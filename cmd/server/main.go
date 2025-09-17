package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func join(nums []int) string {
	if len(nums) == 0 {
		return ""
	}
	s := make([]string, len(nums))
	for i, v := range nums {
		s[i] = strconv.Itoa(v)
	}
	return strings.Join(s, ", ")
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

	staticDir := filepath.FromSlash("web/static/dist")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

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

	mux.HandleFunc("/session/new", func(w http.ResponseWriter, r *http.Request) {
		day := db.NextDay(r.Context(), pool)
		items := plan.Day(day)

		// collect labels that have sets
		var labels []string
		for _, it := range items {
			if it.Kind == "sets" {
				labels = append(labels, it.Label)
			}
		}
		prev, _ := db.PrevLatestByLabels(r.Context(), pool, labels)

		t := mustTpl("web/templates/base.gohtml", "web/templates/session_new.gohtml")
		data := struct {
			Day       int
			Items     []plan.Item
			WorkoutID int64
			Prev      map[string][]int
		}{Day: day, Items: items, WorkoutID: 0, Prev: prev}
		if err := t.ExecuteTemplate(w, "base.gohtml", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	// Save draft: create workout if needed, then persist items
	mux.HandleFunc("/session/save", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		var workoutID int64
		if idStr := r.FormValue("workout_id"); idStr != "" {
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				workoutID = id
			}
		}
		if workoutID == 0 {
			day, _ := strconv.Atoi(r.FormValue("day"))
			const ins = `INSERT INTO workouts(day_num) VALUES ($1) RETURNING id`
			if err := pool.QueryRow(r.Context(), ins, day).Scan(&workoutID); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, `<input type="hidden" id="workout_id" name="workout_id" value="%d" hx-swap-oob="true">`, workoutID)
		}

		// Persist items: for each item index present, delete existing rows for that label then insert current values
		for key := range r.PostForm {
			if !strings.HasPrefix(key, "it_") || !strings.HasSuffix(key, "_kind") {
				continue
			}
			idx := strings.TrimSuffix(strings.TrimPrefix(key, "it_"), "_kind")
			kind := r.PostFormValue("it_" + idx + "_kind")
			label := r.PostFormValue("it_" + idx + "_label")

			// Clear any prior rows for this workout+label to make saves idempotent
			if _, err := pool.Exec(r.Context(), `DELETE FROM workout_items WHERE workout_id=$1 AND label=$2`, workoutID, label); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}

			switch kind {
			case "check":
				checked := r.PostFormValue("c_"+idx) != ""
				_, err := pool.Exec(r.Context(),
					`INSERT INTO workout_items(workout_id,kind,label,checked) VALUES ($1,'check',$2,$3)`,
					workoutID, label, checked,
				)
				if err != nil {
					http.Error(w, "db error", http.StatusInternalServerError)
					return
				}
			case "sets":
				sets, _ := strconv.Atoi(r.PostFormValue("it_" + idx + "_sets"))
				for s := 1; s <= sets; s++ {
					vStr := r.PostFormValue(fmt.Sprintf("s_%s_%d", idx, s))
					if vStr == "" {
						continue
					}
					v, err := strconv.Atoi(vStr)
					if err != nil {
						http.Error(w, "bad number", http.StatusBadRequest)
						return
					}
					_, err = pool.Exec(r.Context(),
						`INSERT INTO workout_items(workout_id,kind,label,set_index,value_int) VALUES ($1,'sets',$2,$3,$4)`,
						workoutID, label, s, v,
					)
					if err != nil {
						http.Error(w, "db error", http.StatusInternalServerError)
						return
					}
				}
			}
		}

		w.Header().Set("HX-Trigger", "saved")
		w.Write([]byte("Saved"))
	})

	// Mark complete: set completed_at and redirect home
	mux.HandleFunc("/session/complete", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		idStr := r.FormValue("workout_id")
		if idStr == "" {
			http.Error(w, "no workout id", http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		if _, err := pool.Exec(r.Context(), `UPDATE workouts SET completed_at=now() WHERE id=$1`, id); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Redirect", "/")
		w.Write([]byte("Completed"))
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
	t = t.Funcs(template.FuncMap{"seq": seq, "join": join})
	return template.Must(t.ParseFiles(files...))
}
