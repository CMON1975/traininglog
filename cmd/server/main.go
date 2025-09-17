package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"traininglog/internal/db"
	"traininglog/internal/plan"
)

func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i + 1
	}
	return s
}

func main() {
	if os.Getenv("DATABASE_URL") == "" {
		log.Fatal("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	dbStatus := "down"
	if err := db.Ping(ctx, pool); err == nil {
		dbStatus = "ok"
	}

	mux := http.NewServeMux()

	// static assets
	staticDir := filepath.FromSlash("web/static/dist")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// home
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t := mustTpl("web/templates/base.gohtml", "web/templates/index.gohtml")
		data := struct {
			DBStatus string
			NextDay  int
		}{
			DBStatus: dbStatus,
			NextDay:  db.NextDay(r.Context(), pool),
		}
		if err := t.ExecuteTemplate(w, "base.gohtml", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	// new session form
	mux.HandleFunc("/session/new", func(w http.ResponseWriter, r *http.Request) {
		day := db.NextDay(r.Context(), pool)
		items := plan.Day(day)
		t := mustTpl("web/templates/base.gohtml", "web/templates/session_new.gohtml")
		t = t.Funcs(template.FuncMap{"seq": seq})
		data := struct {
			Day   int
			Items []plan.Item
		}{Day: day, Items: items}
		if err := t.ExecuteTemplate(w, "base.gohtml", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	// save draft (only records a workout row for now)
	mux.HandleFunc("/session/save", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		day, _ := strconv.Atoi(r.FormValue("day"))
		const ins = `INSERT INTO workouts(day_num) VALUES ($1)`
		if _, err := pool.Exec(r.Context(), ins, day); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "saved")
		w.Write([]byte("Saved draft"))
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Println("listening on http://127.0.0.1:8080")
	log.Fatal(srv.ListenAndServe())
}

func mustTpl(files ...string) *template.Template {
	t := template.New(filepath.Base(files[0]))
	t = t.Funcs(template.FuncMap{"seq": seq})
	return template.Must(t.ParseFiles(files...))
}
