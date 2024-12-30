package http

import (
	"syscall"

	"github.com/alaisi/syscalltodo/io"
	"github.com/alaisi/syscalltodo/str"
)

type Server struct {
	Addr    string
	Handler Handler
	closefd int
}

const ErrServerClosed = protocolError("Server closed")

func (srv *Server) Close() {
	if srv.closefd > 0 {
		close := []byte{0, 0, 0, 0, 0, 0, 0, 1}
		syscall.Write(srv.closefd, close)
	}
}

func (srv *Server) ListenAndServe() error {
	sockfd, err := io.Listen(str.Atoi(str.Split(srv.Addr, ':')[1]))
	if err != nil {
		return err
	}
	defer syscall.Close(sockfd)
	eventfd, _, errno := syscall.Syscall(
		syscall.SYS_EVENTFD,
		uintptr(0),
		uintptr(0),
		uintptr(0))
	if errno != 0 {
		return syscall.Errno(errno)
	}
	srv.closefd = int(eventfd)
	defer syscall.Close(srv.closefd)
	return io.Epoll(func(event syscall.EpollEvent) error {
		if int(event.Fd) == sockfd {
			if connfd, _, err := syscall.Accept(sockfd); err == nil {
				go srv.serve(connfd)
			}
		} else if int(event.Fd) == srv.closefd {
			return ErrServerClosed
		}
		return nil
	}, sockfd, srv.closefd)
}

func (srv *Server) serve(connfd int) {
	defer syscall.Close(connfd)
	defer func() {
		if r := recover(); r != nil {
			io.Write(2, []byte(str.ToString(r)))
		}
	}()
	if err := configureTimeouts(connfd); err != nil {
		return
	}
	reader := io.NewFileReader(connfd)
	lr := io.NewLineReader(reader)
	writer := io.NewFileWriter(connfd)
	for {
		req, err := readRequest(lr, reader)
		if err != nil {
			if err != io.EOF {
				sendBadRequestResponse(writer)
			}
			break
		}
		res := newHttpResponse(req.Proto)
		srv.Handler.ServeHTTP(res, req)
		if res.status == 0 {
			res.status = 404
		}
		keepAlive := setKeepAlive(res, req)
		sendResponse(res, writer)
		if !keepAlive {
			break
		}
	}
}

func setKeepAlive(res *httpResponse, req *Request) bool {
	connection := (*req.Header)["connection"]
	if len(connection) == 1 {
		value := str.ToLowerAscii(connection[0])
		if req.Proto == "HTTP/1.1" && value != "close" {
			return true
		}
		if req.Proto == "HTTP/1.0" && value == "keep-alive" {
			res.Header().Set("Connection", connection[0])
			return true
		}
	}
	return req.Proto == "HTTP/1.1"
}

func readRequest(lr *io.LineReader, raw io.Reader) (*Request, error) {
	line, err := lr.ReadLine()
	if err != nil {
		return nil, err
	}
	method, path, protocol, err := parseRequestStart(line)
	if err != nil {
		return nil, err
	}
	headers := make(Header)
	var body []byte
	for {
		line, err = lr.ReadLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			body = lr.GetTail()
			break
		}
		header, value, err := parseHeader(line)
		if err != nil {
			return nil, err
		}
		headers[header] = append(headers[header], value)
	}
	req := &Request{
		Method: method, URL: &URL{Path: path}, Header: &headers,
		Proto: protocol, body: body, reader: raw}
	return req, nil
}

func parseRequestStart(line string) (string, string, string, error) {
	parts := str.Split(line, ' ')
	if len(parts) != 3 {
		return "", "", "", protocolError("Invalid request start")
	}
	method := parts[0]
	path := parts[1]
	protocol := parts[2]
	if !(protocol == "HTTP/1.1" || protocol == "HTTP/1.0") ||
		len(path) < 1 || path[0] != '/' {
		return "", "", "", protocolError("Invalid request start")
	}
	return method, path, protocol, nil
}

func parseHeader(line string) (string, string, error) {
	split := str.IndexOf(line, ':')
	if split < 1 || split == len(line)-1 {
		return "", "", protocolError("Invalid header")
	}
	name := str.ToLowerAscii(str.Trim(line[:split]))
	value := str.Trim(line[split+1:])
	return name, value, nil
}

func sendBadRequestResponse(writer io.Writer) {
	res := newHttpResponse("HTTP/1.1")
	res.WriteHeader(400)
	res.Header().Set("Connection", "close")
	sendResponse(res, writer)
}

type Request struct {
	Method string
	URL    *URL
	Header *Header
	Proto  string
	Form   UrlValues
	body   []byte
	reader io.Reader
}
type URL struct {
	Path string
}
type Header map[string][]string

func (header Header) Set(name string, value string) {
	header[name] = []string{value}
}
func (values Header) Get(key string) string {
	v := values[str.ToLowerAscii(key)]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

type UrlValues map[string][]string

func (values UrlValues) Get(key string) string {
	v := values[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

type ResponseWriter interface {
	WriteHeader(status int)
	Header() Header
	Write([]byte) (int, error)
}

type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}
type HandlerFunc func(ResponseWriter, *Request)

func (fn HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) {
	fn(w, r)
}

type protocolError string

func (err protocolError) Error() string {
	return string(err)
}

type httpResponse struct {
	protocol string
	buffer   *io.ByteArrayWriter
	status   int
	header   Header
}

func newHttpResponse(protocol string) *httpResponse {
	return &httpResponse{
		protocol: protocol,
		header:   make(Header),
		buffer:   io.NewByteArrayWriter(),
	}
}

func (res *httpResponse) Header() Header {
	return res.header
}

var statusTexts = map[int]string{
	200: "OK",
	302: "Temporary redirect",
	400: "Bad Request",
	404: "Not Found",
	500: "Internal Server Error",
}

func (res *httpResponse) WriteHeader(status int) {
	res.status = status
}

func (res *httpResponse) Write(body []byte) (int, error) {
	if res.status == 0 {
		res.status = 200
	}
	return res.buffer.Write(body)
}

func sendResponse(res *httpResponse, writer io.Writer) error {
	statusLine := res.protocol + " " +
		str.Itoa(res.status) + " " +
		statusTexts[res.status] + "\r\n"
	if _, err := writer.Write([]byte(statusLine)); err != nil {
		return err
	}
	for header, values := range res.header {
		for _, value := range values {
			headerLine := header + ": " + value + "\r\n"
			if _, err := writer.Write([]byte(headerLine)); err != nil {
				return err
			}
		}
	}
	endHeaders := "Content-Length: " + str.Itoa(len(res.buffer.Bytes)) +
		"\r\n\r\n"
	if _, err := writer.Write([]byte(endHeaders)); err != nil {
		return err
	}
	if _, err := writer.Write(res.buffer.Bytes); err != nil {
		return err
	}
	return nil
}

func Error(res ResponseWriter, body string, code int) {
	res.WriteHeader(code)
	res.Header().Set("Content-Type", "text/plain")
	res.Write([]byte(body))
}

func configureTimeouts(connfd int) error {
	timeout := syscall.Timeval{Sec: 10}
	if err := syscall.SetsockoptTimeval(
		connfd, syscall.SOL_SOCKET,
		syscall.SO_SNDTIMEO, &timeout); err != nil {
		return err
	}
	if err := syscall.SetsockoptInt(
		connfd, syscall.SOL_TCP, syscall.TCP_NODELAY, 1); err != nil {
		return err
	}
	return nil
}

func readBody(req *Request) ([]byte, error) {
	size := str.Atoi(req.Header.Get("content-length"))
	if size < 1 || size > 8192 {
		return []byte{}, nil
	}
	buf := make([]byte, size)
	copy(buf, req.body)
	for read := len(req.body); read < size; {
		n, err := req.reader.Read(buf[read:])
		if err != nil {
			return nil, err
		}
		read += n
	}
	req.body = buf
	return req.body, nil
}

func (req *Request) ParseForm() error {
	if req.Header.Get("content-type") != "application/x-www-form-urlencoded" {
		return protocolError("Unsupported Content-Type")
	}
	body, err := readBody(req)
	if err != nil {
		return err
	}
	req.Form = make(UrlValues, 8)
	for _, field := range str.Split(string(body), '&') {
		eq := str.IndexOf(field, '=')
		k := urlDecode(field[0:eq])
		v := ""
		if eq < len(field)-1 {
			v = urlDecode(field[eq+1:])
		}
		req.Form[k] = append(req.Form[k], v)
	}
	return nil
}

func urlDecode(s string) string {
	encoded := []byte(s)
	decoded := make([]byte, 0, len(encoded))
	for i := 0; i < len(encoded); i++ {
		b := encoded[i]
		if b == '+' {
			decoded = append(decoded, ' ')
		} else if b == '%' && i < len(encoded)-2 {
			c := str.DecodeHex(encoded[i+1 : i+3])
			decoded = append(decoded, c...)
			i += 2
		} else {
			decoded = append(decoded, b)
		}
	}
	return string(decoded)
}
