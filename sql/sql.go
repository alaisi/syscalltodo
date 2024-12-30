package sql

import (
	"github.com/alaisi/syscalltodo/sql/driver"
	"github.com/alaisi/syscalltodo/str"
)

var drivers map[string]driver.Driver = make(map[string]driver.Driver)

func Register(name string, driver driver.Driver) {
	drivers[name] = driver
}

type DB struct {
	newConnection func() (driver.Conn, error)
	maxSize       int
	size          int
	sizeLock      chan any
	pool          chan driver.Conn
}

func Open(driverName string, dataSourceName string) (*DB, error) {
	drv := drivers[driverName]
	if drv == nil {
		return nil, dbError("No driver available for: " + driverName)
	}
	db := &DB{
		maxSize:  1,
		pool:     make(chan driver.Conn, 1),
		sizeLock: make(chan any, 1),
		newConnection: func() (driver.Conn, error) {
			return drv.Open(dataSourceName)
		},
	}
	db.sizeLock <- struct{}{}
	return db, nil
}

func (db *DB) SetMaxOpenConns(max int) {
	db.maxSize = max
	db.pool = make(chan driver.Conn, max)
}

func (db *DB) Close() error {
	for {
		locked := <-db.sizeLock
		size := db.size
		db.sizeLock <- locked
		if size == 0 {
			return nil
		}
		conn := <-db.pool
		db.destroyConnection(conn)
	}
}

func (db *DB) Exec(query string, args ...any) (Result, error) {
	conn, err := db.getConnection()
	if err != nil {
		return nil, err
	}
	defer db.releaseConnection(conn)
	stmt, err := conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := stmt.Exec(toDriverValues(args))
	if err != nil {
		return nil, err
	}
	return &result{res}, nil
}

func (db *DB) Query(query string, args ...any) (*Rows, error) {
	conn, err := db.getConnection()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			db.releaseConnection(conn)
		}
	}()
	stmt, err := conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rs, err := stmt.Query(toDriverValues(args))
	if err != nil {
		return nil, err
	}
	values := make([]driver.Value, len(rs.Columns()))
	return &Rows{db, conn, rs, values}, nil
}

func (db *DB) Begin() (*Tx, error) {
	conn, err := db.getConnection()
	if err != nil {
		return nil, err
	}
	tx, err := conn.Begin()
	if err != nil {
		db.releaseConnection(conn)
		return nil, err
	}
	return &Tx{db, conn, tx}, nil
}

type Tx struct {
	db   *DB
	conn driver.Conn
	tx   driver.Tx
}

func (tx *Tx) Exec(query string, args ...any) (Result, error) {
	stmt, err := tx.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := stmt.Exec(toDriverValues(args))
	if err != nil {
		return nil, err
	}
	return &result{res}, nil
}

func (tx *Tx) Query(query string, args ...any) (*Rows, error) {
	stmt, err := tx.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rs, err := stmt.Query(toDriverValues(args))
	if err != nil {
		return nil, err
	}
	values := make([]driver.Value, len(rs.Columns()))
	return &Rows{nil, nil, rs, values}, nil

}

func (tx *Tx) Commit() error {
	err := tx.tx.Commit()
	tx.db.releaseConnection(tx.conn)
	return err
}

func (tx *Tx) Rollback() error {
	err := tx.tx.Rollback()
	tx.db.releaseConnection(tx.conn)
	return err
}

type Rows struct {
	db     *DB
	conn   driver.Conn
	rs     driver.Rows
	values []driver.Value
}

func (rows *Rows) Next() bool {
	err := rows.rs.Next(rows.values)
	return err == nil
}

func (rows *Rows) Scan(dest ...any) error {
	for i := 0; i < len(dest) && i < len(rows.values); i++ {
		if d, isString := dest[i].(*string); isString {
			*d = str.ToString(rows.values[i])
		} else if d, isInt64 := dest[i].(*int64); isInt64 {
			*d = int64(str.Atoi(str.ToString(rows.values[i])))
		} else if d, isBool := dest[i].(*bool); isBool {
			*d = str.ToString(rows.values[i]) == "t"
		} else {
			return dbError("Unsupported scan target type at " + str.Itoa(i))
		}
	}
	return nil
}

func (rows *Rows) Close() error {
	err := rows.rs.Close()
	if rows.conn != nil {
		rows.db.releaseConnection(rows.conn)
	}
	return err
}

type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

type result struct {
	r driver.Result
}

func (result *result) LastInsertId() (int64, error) {
	return result.r.LastInsertId()
}

func (result *result) RowsAffected() (int64, error) {
	return result.r.RowsAffected()
}

func (db *DB) getConnection() (driver.Conn, error) {
	for i := 0; i < db.maxSize; i++ {
		conn, err := db.checkoutConnection()
		if err != nil {
			return nil, err
		}
		if db.validateConnection(conn) {
			return conn, nil
		}
		db.destroyConnection(conn)
	}
	return nil, dbError("No db connection available")
}

func (db *DB) checkoutConnection() (driver.Conn, error) {
	select {
	case conn := <-db.pool:
		return conn, nil
	default:
		locked := <-db.sizeLock
		if db.size < db.maxSize {
			db.size++
			db.sizeLock <- locked
			conn, err := db.newConnection()
			if err != nil {
				locked = <-db.sizeLock
				db.size--
				db.sizeLock <- locked
				return nil, err
			}
			return conn, nil

		}
		db.sizeLock <- locked
		conn := <-db.pool
		return conn, nil
	}
}

func (db *DB) releaseConnection(conn driver.Conn) {
	if !db.validateConnection(conn) {
		db.destroyConnection(conn)
		return
	}
	db.pool <- conn
}

func (db *DB) destroyConnection(conn driver.Conn) {
	conn.Close()
	locked := <-db.sizeLock
	db.size--
	db.sizeLock <- locked
}

func (db *DB) validateConnection(conn driver.Conn) bool {
	if validator, ok := conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

func toDriverValues(args []any) []driver.Value {
	params := make([]driver.Value, len(args))
	for i, p := range args {
		params[i] = p
	}
	return params
}

type dbError string

func (err dbError) Error() string {
	return string(err)
}
