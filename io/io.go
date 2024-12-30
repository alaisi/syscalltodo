package io

import (
	"syscall"
	"unsafe"

	"github.com/alaisi/syscalltodo/str"
)

func AtExit(fn func()) error {
	sigmask := uint64(1<<(syscall.SIGINT-1)) | (1 << (syscall.SIGTERM - 1))
	signalFdCreate := -1
	fd, _, errno := syscall.Syscall(
		syscall.SYS_SIGNALFD,
		uintptr(signalFdCreate),
		uintptr(unsafe.Pointer(&sigmask)),
		uintptr(8))
	if errno != 0 {
		return syscall.Errno(errno)
	}
	go func() {
		siginfo := make([]byte, 128)
		for {
			_, err := syscall.Read(int(fd), siginfo)
			if err == nil {
				break
			}
			if err != syscall.EINTR {
				return
			}
		}
		fn()
	}()
	return nil
}

func Epoll(fn func(syscall.EpollEvent) error, fds ...int) error {
	epfd, err := syscall.EpollCreate1(0)
	if err != nil {
		return err
	}
	defer syscall.Close(epfd)
	for _, fd := range fds {
		event := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
		err = syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, fd, &event)
		if err != nil {
			return err
		}
	}
	events := make([]syscall.EpollEvent, len(fds))
	defer syscall.Close(epfd)
	for {
		n, err := syscall.EpollWait(epfd, events, -1)
		if err != nil && err != syscall.EINTR {
			return err
		}
		for i := 0; i < n; i++ {
			if events[i].Events != 0 {
				if err := fn(events[i]); err != nil {
					return err
				}
			}
		}
	}
}

func GetEnv(name string) (string, error) {
	env, err := ReadFile("/proc/self/environ")
	if err != nil {
		return "", err
	}
	for _, e := range str.Split(string(env), '\000') {
		eq := str.IndexOf(e, '=')
		if eq > 0 && name == e[0:eq] {
			value := ""
			if eq < len(e)-1 {
				value = e[eq+1:]
			}
			return value, nil
		}
	}
	return "", nil
}

func Listen(port int) (int, error) {
	sockfd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	defer func() {
		if err != nil {
			syscall.Close(sockfd)
		}
	}()
	if err = syscall.SetsockoptInt(
		sockfd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return -1, err
	}
	if err = syscall.SetNonblock(sockfd, true); err != nil {
		return -1, err
	}
	if err = syscall.Bind(
		sockfd, &syscall.SockaddrInet4{Port: port}); err != nil {
		return -1, err
	}
	if err = syscall.Listen(sockfd, 255); err != nil {
		return -1, err
	}
	return sockfd, nil
}

func Connect(addr [4]byte, port int) (int, error) {
	sockfd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	defer func() {
		if err != nil {
			syscall.Close(sockfd)
		}
	}()
	sockaddr := syscall.SockaddrInet4{Addr: addr, Port: port}
	if err = syscall.Connect(sockfd, &sockaddr); err != nil {
		return -1, err
	}
	if err = syscall.SetsockoptInt(
		sockfd, syscall.SOL_TCP, syscall.TCP_NODELAY, 1); err != nil {
		return -1, err
	}
	return sockfd, nil
}

func Println(s string) {
	syscall.Write(1, []byte(s+"\n"))
}

type Reader interface {
	Read(p []byte) (n int, err error)
}
type Writer interface {
	Write(p []byte) (n int, err error)
}

type ByteArrayWriter struct {
	Bytes []byte
}

func NewByteArrayWriter() *ByteArrayWriter {
	return &ByteArrayWriter{make([]byte, 0, 512)}
}

func (writer *ByteArrayWriter) Write(buf []byte) (int, error) {
	writer.Bytes = append(writer.Bytes, buf...)
	return len(buf), nil
}

type ReaderErr string

const EOF ReaderErr = "EOF"

func (err ReaderErr) Error() string {
	return string(err)
}

func Read(fd int, buf []byte) (int, error) {
	for {
		read, err := syscall.Read(fd, buf)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return -1, err
		}
		if read == 0 {
			return -1, EOF
		}
		return read, nil
	}
}

func Write(fd int, buf []byte) (int, error) {
	for {
		n, err := syscall.Write(fd, buf)
		if err == syscall.EINTR {
			continue
		}
		return n, err
	}
}

type fileReader struct {
	fd int
}

func (reader *fileReader) Read(buf []byte) (int, error) {
	return Read(reader.fd, buf)
}

func NewFileReader(fd int) Reader {
	return &fileReader{fd: fd}
}

func ReadFile(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	reader := NewFileReader(fd)
	content := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		read, err := reader.Read(tmp)
		if err == EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		content = append(content, tmp[0:read]...)
	}
	return content, nil
}

type fileWriter struct {
	fd int
}

func (writer *fileWriter) Write(buf []byte) (int, error) {
	return Write(writer.fd, buf)
}

func NewFileWriter(fd int) Writer {
	return &fileWriter{fd}
}

type LineReader struct {
	reader Reader
	buf    []byte
	pos    int
	len    int
}

func NewLineReader(reader Reader) *LineReader {
	return &LineReader{reader: reader, buf: make([]byte, 4096)}
}

func (lr *LineReader) ReadLine() (string, error) {
	start := lr.pos
	for {
		for ; lr.pos < lr.len; lr.pos++ {
			if lr.buf[lr.pos] == '\n' {
				skip := 1
				if lr.pos > 0 && lr.buf[lr.pos-1] == '\r' {
					skip = 2
				}
				lr.pos++
				line := string(lr.buf[start : lr.pos-skip])
				lr.compact()
				return line, nil
			}
		}
		if err := lr.read(); err != nil {
			return "", err
		}
	}
}

func (lr *LineReader) GetTail() []byte {
	tail := lr.buf[0:lr.len]
	lr.len = 0
	return tail
}

func (lr *LineReader) read() error {
	if lr.len == len(lr.buf) {
		lr.buf = append(lr.buf, make([]byte, lr.len)...)
	}
	read, err := lr.reader.Read(lr.buf[lr.len:])
	if err != nil {
		return err
	}
	lr.len += read
	return nil
}

func (reader *LineReader) compact() {
	copy(reader.buf, reader.buf[reader.pos:reader.len])
	reader.len -= reader.pos
	reader.pos = 0
}
