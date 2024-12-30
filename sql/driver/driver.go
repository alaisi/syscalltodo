package driver

type Driver interface {
	Open(name string) (Conn, error)
}

type Conn interface {
	Prepare(query string) (Stmt, error)
	Close() error
	Begin() (Tx, error)
}

type Tx interface {
	Commit() error
	Rollback() error
}

type Stmt interface {
	Close() error
	NumInput() int
	Query(args []Value) (Rows, error)
	Exec(args []Value) (Result, error)
}

type Value any

type Rows interface {
	Columns() []string
	Close() error
	Next(dest []Value) error
}

type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

type Validator interface {
	IsValid() bool
}
