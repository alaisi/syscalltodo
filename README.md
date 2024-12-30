# syscalltodo

  A Golang+Postgres webapp using only Linux syscalls and Go language builtins.

## Packages:

* `http`: HTTP server, implements a subset of Go standard library `net/http` APIs
* `template`: HTML templating, implements a subset of Go standard library `html/template` APIs
* `sql`: Database connectivity, implements a subset of Go standard library `database/sql` APIs
* `pg`: PostgreSQL driver, implementing `sql/driver`

## Running the app:

```bash
$ go build
$ DB_URI=postgresql://username:password@127.0.0.1:5432/dbname ./syscalltodo
$ xdg-open http://localhost:9000
```
