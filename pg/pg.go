package pg

import (
	"syscall"

	"github.com/alaisi/syscalltodo/io"
	"github.com/alaisi/syscalltodo/sql"
	"github.com/alaisi/syscalltodo/sql/driver"
	"github.com/alaisi/syscalltodo/str"
)

var (
	_ driver.Driver = pgDriver{}
	_ driver.Conn   = pgConn{}
	_ driver.Tx     = pgConn{}
	_ driver.Stmt   = pgStmt{}
	_ driver.Rows   = pgRows{}
	_ driver.Result = pgRows{}
)

func init() {
	sql.Register("postgres", pgDriver{})
}

type pgDriver struct {
}

func (d pgDriver) Open(connStr string) (driver.Conn, error) {
	spec, err := parseConnectionSpec(connStr)
	if err != nil {
		return nil, err
	}
	sockfd, err := io.Connect(spec.ip, spec.port)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			syscall.Close(sockfd)
		}
	}()
	stream := &pgStream{sockfd, &packet{buffer: make([]byte, 0, 4096)}, true}
	if err = authenticate(stream, spec.db, spec.user, spec.password); err != nil {
		return nil, err
	}
	return &pgConn{stream}, nil
}

type pgConn struct {
	stream *pgStream
}

func (conn pgConn) Prepare(query string) (driver.Stmt, error) {
	return &pgStmt{query, &conn}, nil
}

func (conn pgConn) Close() error {
	conn.stream.send(writeTerminate())
	return syscall.Close(conn.stream.sockfd)
}

func (conn pgConn) Begin() (driver.Tx, error) {
	st, _ := conn.Prepare("BEGIN")
	if _, err := st.Exec([]driver.Value{}); err != nil {
		return nil, err
	}
	return &conn, nil
}

func (conn pgConn) Commit() error {
	st, _ := conn.Prepare("COMMIT")
	_, err := st.Exec([]driver.Value{})
	return err
}

func (conn pgConn) Rollback() error {
	st, _ := conn.Prepare("ROLLBACK")
	_, err := st.Exec([]driver.Value{})
	return err
}

func (conn pgConn) IsValid() bool {
	return conn.stream.valid
}

type pgStmt struct {
	query string
	conn  *pgConn
}

func (p pgStmt) Exec(args []driver.Value) (driver.Result, error) {
	rows, err := p.Query(args)
	if err != nil {
		return nil, err
	}
	return rows.(pgRows), nil
}

func (p pgStmt) NumInput() int {
	return -1
}

func (p pgStmt) Query(args []driver.Value) (driver.Rows, error) {
	req := buildQueryMessages(p.query, args)
	if err := p.conn.stream.send(req); err != nil {
		return nil, err
	}
	msgs, err := p.conn.stream.recv(false)
	if err != nil {
		return nil, err
	}
	var desc *rowDescription
	var rows []*dataRow
	for _, msg := range msgs {
		switch msg.cmd {
		case 'E':
			return nil, readError(msg.packet)
		case 'T':
			desc = readRowDescription(msg.packet)
			rows = make([]*dataRow, 0, desc.cols)
		case 'D':
			rows = append(rows, readDataRow(msg.packet))
		case 'C':
			complete := readCommandComplete(msg.packet)
			return pgRows{&pgRowsCursor{desc, complete, rows}}, nil
		}
	}
	p.conn.stream.valid = false
	return nil, Error{Severity: "FATAL", Message: "Protocol error"}
}

func buildQueryMessages(query string, args []driver.Value) []byte {
	if len(args) == 0 {
		return writeQuery(query)
	}
	params := make([]*[]byte, len(args))
	for i, p := range args {
		if p != nil {
			encoded := []byte(str.ToString(p))
			params[i] = &encoded
		} else {
			params[i] = nil
		}
	}
	req := writeParse(query)
	req = append(req, writeBind(params)...)
	req = append(req, writeDescribe()...)
	req = append(req, writeExecute()...)
	req = append(req, writeClose()...)
	return append(req, writeSync()...)
}

func (p pgStmt) Close() error {
	return nil
}

type pgRowsCursor struct {
	desc     *rowDescription
	complete *commandComplete
	rows     []*dataRow
}
type pgRows struct {
	cursor *pgRowsCursor
}

func (r pgRows) Close() error {
	return nil
}

func (r pgRows) Columns() []string {
	return r.cursor.desc.names
}

func (r pgRows) Next(dest []driver.Value) error {
	if len(r.cursor.rows) == 0 {
		return io.EOF
	}
	row := r.cursor.rows[0]
	for i := 0; i < len(dest); i++ {
		value := row.values[i]
		if value != nil {
			dest[i] = *value
		} else {
			dest[i] = nil
		}
	}
	r.cursor.rows = r.cursor.rows[1:]
	return nil
}

func (r pgRows) RowsAffected() (int64, error) {
	words := str.Split(r.cursor.complete.tag, ' ')
	if len(words) == 0 {
		return 0, nil
	}
	return str.Atol(words[len(words)-1]), nil
}

func (r pgRows) LastInsertId() (int64, error) {
	return -1, Error{
		Severity: "FATAL",
		Message:  "Not supported, use RETURNING in query instead"}
}

type Error struct {
	Severity string
	Code     string
	Message  string
	Detail   string
}

func (e Error) Error() string {
	s := e.Severity + ": " + e.Message + ", code=" + e.Code
	if e.Detail != "" {
		s += ", detail=" + e.Detail
	}
	return s
}

type connSpec struct {
	ip       [4]byte
	port     int
	db       string
	user     string
	password string
}

var invalidConnectionSpec = Error{
	Severity: "FATAL",
	Message:  "Invalid connection spec",
	Detail:   "Use 'postgresql://<user>:<password>@<ip>:<port>/<database>'"}

func parseConnectionSpec(connStr string) (*connSpec, error) {
	parts := str.Split(connStr, '/')
	if len(parts) != 4 || parts[0] != "postgresql:" {
		return nil, invalidConnectionSpec
	}
	db := parts[3]
	parts = str.Split(parts[2], '@')
	if len(parts) != 2 {
		return nil, invalidConnectionSpec
	}
	auth := str.Split(parts[0], ':')
	if len(parts) != 2 {
		return nil, invalidConnectionSpec
	}
	parts = str.Split(parts[1], ':')
	if len(parts) != 2 {
		return nil, invalidConnectionSpec
	}
	port := str.Atoi(parts[1])
	parts = str.Split(parts[0], '.')
	if len(parts) != 4 || port < 1 {
		return nil, invalidConnectionSpec
	}
	ip := [4]byte{}
	for i := 0; i < len(ip); i++ {
		ip[i] = byte(str.Atoi(parts[i]))
	}
	return &connSpec{ip, port, db, auth[0], auth[1]}, nil
}

func authenticate(stream *pgStream, db string, user string, password string) error {
	if err := stream.send(writeStartup(db, user)); err != nil {
		return err
	}
	res, err := stream.recv(true)
	if err != nil {
		return err
	}
	for _, msg := range res {
		switch msg.cmd {
		case 'E':
			return readError(msg.packet)
		case 'R':
			switch msg.packet.readUint32() {
			case 0:
				return nil
			case 10:
				return saslAuthenticate(stream, msg.packet, password)
			default:
				return Error{Severity: "FATAL", Message: "Authentication error"}
			}
		}
	}
	return Error{Severity: "FATAL", Message: "Protocol error on authentication"}
}

func saslAuthenticate(stream *pgStream, methods *packet, password string) error {
	if !isScramSha256(methods) {
		return Error{Severity: "FATAL", Message: "Unsupported SASL method"}
	}
	clientFirst, err := scramBuildClientFirst()
	if err != nil {
		return err
	}
	serverFirst, err := saslSendClientFirst(stream, clientFirst)
	if err != nil {
		return err
	}
	saltedPassword := scramHashPassword(password, serverFirst)
	clientFinal := scramBuildClientFinal(clientFirst, saltedPassword, serverFirst)
	if clientFinal == nil {
		return Error{Severity: "FATAL", Message: "Invalid SASL response"}
	}
	serverFinal, err := saslSendClientFinal(stream, clientFinal)
	if err != nil {
		return err
	}
	if !scramAuthenticateServer(
		clientFirst, serverFirst, saltedPassword, clientFinal, serverFinal) {
		return Error{
			Severity: "FATAL",
			Message:  "Server authentication failed"}
	}
	return nil
}

func isScramSha256(methods *packet) bool {
	for m := methods.readString(); m != ""; m = methods.readString() {
		if m == "SCRAM-SHA-256" {
			return true
		}
	}
	return false
}

func saslSendClientFirst(stream *pgStream, clientFirst []byte) ([]byte, error) {
	request := writeSaslInitialResponse(clientFirst)
	return saslExchange(stream, request, 11)
}

func saslSendClientFinal(stream *pgStream, clientFinal []byte) ([]byte, error) {
	request := writeSaslClientFinal(clientFinal)
	return saslExchange(stream, request, 12)
}

func saslExchange(stream *pgStream, req []byte, nextState int) ([]byte, error) {
	if err := stream.send(req); err != nil {
		return nil, err
	}
	res, err := stream.recv(true)
	if err != nil {
		return nil, err
	}
	for _, msg := range res {
		switch msg.cmd {
		case 'E':
			return nil, readError(msg.packet)
		case 'R':
			if msg.packet.readUint32() != uint32(nextState) {
				return nil, Error{
					Severity: "FATAL",
					Message:  "Authentication error"}
			}
			return msg.packet.readBytes(msg.packet.available()), nil
		}
	}
	return nil, Error{
		Severity: "FATAL",
		Message:  "Protocol error on SASL authentication"}
}
