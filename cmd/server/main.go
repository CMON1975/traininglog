package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"traininglog/internal/db"
)

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

	dbStatus := "down"
	if err := db.Ping(ctx, pool); err == nil {
		dbStatus = "ok"
	}

	mux := http.NewServeMux()

	// static assets
	staticDir := filepath.FromSlash("web/static/dist")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t := template.Must(template.ParseFiles(
			filepath.FromSlash("web/templates/base.gohtml"),
			filepath.FromSlash("web/templates/index.gohtml"),
		))
		data := struct {
			DBStatus string
		}{DBStatus: dbStatus}
		if err := t.ExecuteTemplate(w, "base.gohtml", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Println("listening on http://127.0.0.1:8080")
	log.Fatal(srv.ListenAndServe())
}
