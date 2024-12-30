package pg

import "github.com/alaisi/syscalltodo/io"

type msg struct {
	cmd    byte
	packet *packet
}

type pgStream struct {
	sockfd  int
	backlog *packet
	valid   bool
}

func (s *pgStream) send(req []byte) error {
	_, err := io.Write(s.sockfd, req)
	s.valid = err == nil
	return err
}

func (s *pgStream) recv(stopOnError bool) ([]*msg, error) {
	msgs := make([]*msg, 0, 16)
	buf := make([]byte, 4096)
	for ready := false; !ready; {
		n, err := io.Read(s.sockfd, buf)
		if err != nil {
			s.valid = false
			return nil, err
		}
		s.backlog.writeByte(buf[0:n]...)
		for s.backlog.available() >= 5 {
			cmd := s.backlog.readByte()
			size := int(s.backlog.readUint32()) - 4
			if s.backlog.available() < size {
				s.backlog.read(-5)
				break
			}
			msg := &msg{cmd, &packet{buffer: s.backlog.readBytes(size)}}
			msgs = append(msgs, msg)
			if isResponseReady(msg, stopOnError) {
				ready = true
			}
		}
		s.backlog.compact()
	}
	return msgs, nil
}

func isResponseReady(msg *msg, stopOnError bool) bool {
	if msg.cmd == 'Z' || (stopOnError && msg.cmd == 'E') {
		return true
	}
	if msg.cmd == 'R' {
		state := msg.packet.readUint32()
		msg.packet.read(-4)
		return state != 0 && state != 12
	}
	return false
}

func writeStartup(db string, user string) []byte {
	p := &packet{buffer: make([]byte, 0, 96)}
	p.writeByte(0, 0, 0, 0, 0, 3, 0, 0)
	for k, v := range map[string]string{
		"client_encoding": "UTF-8",
		"database":        db,
		"user":            user,
	} {
		p.writeString(k)
		p.writeString(v)
	}
	p.writeByte(0)
	setUint32(p.buffer, 0, uint32(len(p.buffer)))
	return p.buffer
}

func writeQuery(sql string) []byte {
	p := &packet{buffer: make([]byte, 0, 6+len(sql))}
	p.writeByte('Q', 0, 0, 0, 0)
	p.writeString(sql)
	return p.toBytes()
}

func writeTerminate() []byte {
	return []byte{'X', 0, 0, 0, 4}
}

func writeParse(sql string) []byte {
	p := &packet{buffer: make([]byte, 0, len(sql)+9)}
	p.writeByte('P', 0, 0, 0, 0)
	p.writeByte(0)
	p.writeString(sql)
	p.writeUint16(0)
	return p.toBytes()
}

func writeBind(params []*[]byte) []byte {
	p := &packet{buffer: make([]byte, 0, 64)}
	p.writeByte('B', 0, 0, 0, 0)
	p.writeByte(0)
	p.writeByte(0)
	p.writeUint16(0)
	p.writeUint16(uint16(len(params)))
	for _, param := range params {
		if param != nil {
			p.writeUint32(uint32(len(*param)))
			p.writeByte(*param...)
		} else {
			sqlNull := -1
			p.writeUint32(uint32(sqlNull))
		}
	}
	p.writeUint16(0)
	return p.toBytes()
}

func writeDescribe() []byte {
	return []byte{'D', 0, 0, 0, 6, 'S', 0}
}

func writeExecute() []byte {
	return []byte{'E', 0, 0, 0, 9, 0, 0, 0, 0, 0}
}

func writeClose() []byte {
	return []byte{'C', 0, 0, 0, 6, 'S', 0}
}

func writeSync() []byte {
	return []byte{'S', 0, 0, 0, 4}
}

func writeSaslInitialResponse(scramClientFirst []byte) []byte {
	p := &packet{buffer: make([]byte, 0, 64)}
	p.writeByte('p', 0, 0, 0, 0)
	p.writeString("SCRAM-SHA-256")
	p.writeUint32(uint32(len(scramClientFirst)))
	p.writeByte(scramClientFirst...)
	return p.toBytes()
}

func writeSaslClientFinal(scramClientFinal []byte) []byte {
	p := &packet{buffer: make([]byte, 0, 64)}
	p.writeByte('p', 0, 0, 0, 0)
	p.writeByte(scramClientFinal...)
	return p.toBytes()
}

func readError(p *packet) Error {
	var severity, code, message, detail string
	for f := p.readByte(); f != 0; f = p.readByte() {
		value := p.readString()
		switch f {
		case 'V':
			severity = value
		case 'C':
			code = value
		case 'M':
			message = value
		case 'D':
			detail = value
		}
	}
	return Error{severity, code, message, detail}
}

type rowDescription struct {
	cols  uint16
	names []string
	oids  []uint32
}

func readRowDescription(p *packet) *rowDescription {
	cols := p.readUint16()
	names := make([]string, 0, cols)
	oids := make([]uint32, 0, cols)
	for i := uint16(0); i < cols; i++ {
		names = append(names, p.readString())
		p.read(6)
		oids = append(oids, p.readUint32())
		p.read(8)
	}
	return &rowDescription{cols, names, oids}
}

type dataRow struct {
	values []*[]byte
}

func readDataRow(p *packet) *dataRow {
	cols := p.readUint16()
	values := make([]*[]byte, 0, cols)
	for i := uint16(0); i < cols; i++ {
		len := p.readInt32()
		if len == -1 {
			values = append(values, nil)
		} else {
			value := p.readBytes(int(len))
			values = append(values, &value)
		}
	}
	return &dataRow{values}
}

type commandComplete struct {
	tag string
}

func readCommandComplete(p *packet) *commandComplete {
	return &commandComplete{p.readString()}
}

type packet struct {
	buffer []byte
	pos    int
}

func (p *packet) writeUint32(v uint32) {
	p.buffer = append(
		p.buffer,
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func (p *packet) writeUint16(v uint16) {
	p.buffer = append(
		p.buffer,
		byte(v>>8), byte(v))
}

func (p *packet) writeByte(b ...byte) {
	p.buffer = append(p.buffer, b...)
}

func (p *packet) writeString(s string) {
	p.buffer = append(p.buffer, []byte(s)...)
	p.buffer = append(p.buffer, 0)
}

func (p *packet) toBytes() []byte {
	setUint32(p.buffer, 1, uint32(len(p.buffer)-1))
	return p.buffer
}

func (p *packet) read(n int) {
	p.pos += n
}

func (p *packet) readByte() byte {
	b := p.buffer[p.pos]
	p.pos++
	return b
}

func (p *packet) readBytes(len int) []byte {
	bytes := p.buffer[p.pos : p.pos+len]
	p.pos += len
	return bytes
}

func (p *packet) readUint32() uint32 {
	return (uint32(p.readByte()) << 24) +
		(uint32(p.readByte()) << 16) +
		(uint32(p.readByte()) << 8) +
		uint32(p.readByte())
}

func (p *packet) readInt32() int32 {
	return (int32(p.readByte()) << 24) +
		(int32(p.readByte()) << 16) +
		(int32(p.readByte()) << 8) +
		int32(p.readByte())
}

func (p *packet) readUint16() uint16 {
	return (uint16(p.readByte()) << 8) + uint16(p.readByte())
}

func (p *packet) readString() string {
	for i := 0; ; i++ {
		if p.buffer[p.pos+i] == 0 {
			str := string(p.buffer[p.pos : p.pos+i])
			p.pos += i + 1
			return str
		}
	}
}

func (p *packet) available() int {
	return len(p.buffer) - p.pos
}

func (p *packet) compact() {
	p.buffer = p.buffer[p.pos:len(p.buffer)]
	p.pos = 0
}

func setUint32(b []byte, pos uint, v uint32) {
	b[pos] = byte(v >> 24)
	b[pos+1] = byte(v >> 16)
	b[pos+2] = byte(v >> 8)
	b[pos+3] = byte(v)
}
