package main

import (
	"github.com/alaisi/syscalltodo/http"
	"github.com/alaisi/syscalltodo/io"
	_ "github.com/alaisi/syscalltodo/pg"
	"github.com/alaisi/syscalltodo/slog"
	"github.com/alaisi/syscalltodo/sql"
	"github.com/alaisi/syscalltodo/str"
	"github.com/alaisi/syscalltodo/template"
)

func main() {
	dbUri, err := io.GetEnv("DB_URI")
	if err != nil || dbUri == "" {
		slog.Error("DB_URI env variable required")
		return
	}
	db, err := sql.Open("postgres", dbUri)
	if err != nil {
		slog.Error("Db connection failed: " + err.Error())
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(25)

	if err := migrateDb(db); err != nil {
		slog.Error("Db schema migration failed: " + err.Error())
		return
	}

	addr := "0.0.0.0:9000"
	server := http.Server{Addr: addr, Handler: errorMiddleware(routes(db))}
	slog.Info("Starting server on http://" + addr)
	io.AtExit(func() {
		server.Close()
	})
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("Server failed: " + err.Error())
	}
	slog.Info("Server stopped")
}

func migrateDb(db *sql.DB) error {
	sql, err := io.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	_, err = db.Exec(string(sql))
	return err
}

func routes(db *sql.DB) http.HandlerFunc {
	index := indexHandler(db)
	addTodo := addTodoHandler(db)
	toggleTodo := toggleTodoHandler(db)
	return func(res http.ResponseWriter, req *http.Request) {
		switch req.Method + " " + req.URL.Path {
		case "GET /":
			index(res, req)
		case "POST /":
			addTodo(res, req)
		case "POST /toggle":
			toggleTodo(res, req)
		default:
			http.Error(res, "NOT_FOUND", 404)
		}
	}
}

func errorMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				http.Error(res, str.ToString(r), 500)
			}
		}()
		next(res, req)
	}
}

func indexHandler(db *sql.DB) http.HandlerFunc {
	var indexHtml = template.Must(template.ParseFiles("main.html"))
	return func(res http.ResponseWriter, req *http.Request) {
		todos, err := getTodos(db)
		if err != nil {
			http.Error(res, err.Error(), 500)
			return
		}
		res.Header().Set("Content-Type", "text/html;charset=utf-8")
		indexHtml.Execute(res, map[string]any{
			"todos": todos,
		})
	}
}

func addTodoHandler(db *sql.DB) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			http.Error(res, err.Error(), 500)
			return
		}
		task := req.Form.Get("task")
		if err := insertTodo(db, task); err != nil {
			http.Error(res, err.Error(), 500)
			return
		}
		res.Header().Set("Location", "/")
		res.WriteHeader(302)
	}
}

func toggleTodoHandler(db *sql.DB) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			http.Error(res, err.Error(), 500)
			return
		}
		id := str.Atol(req.Form.Get("id"))
		found, err := updateTodoDone(db, id)
		if err != nil {
			http.Error(res, err.Error(), 500)
			return
		}
		if !found {
			http.Error(res, "NOT_FOUND", 404)
			return
		}
		res.Header().Set("Location", "/")
		res.WriteHeader(302)
	}
}

func getTodos(db *sql.DB) ([]map[string]any, error) {
	rows, err := db.Query(`
		select id, task, done from todos
		order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	todos := []map[string]any{}
	for rows.Next() {
		var id int64
		var task string
		var done bool
		if err := rows.Scan(&id, &task, &done); err != nil {
			return nil, err
		}
		todos = append(todos, *NewTodo().Id(id).Task(task).Done(done))
	}
	return todos, nil
}

func insertTodo(db *sql.DB, task string) error {
	_, err := db.Exec(
		"insert into todos (task) values ($1)", task)
	return err
}

func updateTodoDone(db *sql.DB, id int64) (bool, error) {
	res, err := db.Exec(`
		update todos set done = not(done)
		where id = $1`, id)
	if err != nil {
		return false, err
	}
	updated, _ := res.RowsAffected()
	return updated > 0, err
}

type Todo map[string]any

func NewTodo() *Todo {
	return &Todo{"done": false}
}
func (t *Todo) Id(id int64) *Todo {
	(*t)["id"] = id
	return t
}
func (t *Todo) Task(task string) *Todo {
	(*t)["task"] = task
	return t
}
func (t *Todo) Done(done bool) *Todo {
	(*t)["done"] = done
	return t
}
