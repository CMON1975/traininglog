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

func loadLoc() *time.Location {
	if loc, err := time.LoadLocation("America/Vancouver"); err == nil {
		return loc
	}
	return time.Local
}

type DayCell struct {
	Date      time.Time
	InMonth   bool
	Completed bool
}

func monthFromQuery(r *http.Request, loc *time.Location) (time.Time, string) {
	ym := r.URL.Query().Get("ym") // "YYYY-MM"
	now := time.Now().In(loc)
	if len(ym) == 7 {
		if t, err := time.ParseInLocation("2006-01", ym, loc); err == nil {
			return t, ym
		}
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc), now.Format("2006-01")
}

func monthBounds(t time.Time) (time.Time, time.Time) {
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	end := start.AddDate(0, 1, 0)
	return start, end
}

func buildCells(loc *time.Location, month time.Time, completed map[string]bool) []DayCell {
	startOfMonth := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, loc)
	weekday := int(startOfMonth.Weekday()) // 0=Sun
	gridStart := startOfMonth.AddDate(0, 0, -weekday)
	cells := make([]DayCell, 42) // 6 weeks
	for i := 0; i < 42; i++ {
		d := gridStart.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		cells[i] = DayCell{
			Date:      d,
			InMonth:   d.Month() == month.Month(),
			Completed: completed[key],
		}
	}
	return cells
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

	mux.HandleFunc("/calendar", func(w http.ResponseWriter, r *http.Request) {
		loc := loadLoc()
		month, _ := monthFromQuery(r, loc)
		startLocal, endLocal := monthBounds(month)

		// query range in UTC to match timestamptz
		startUTC := startLocal.UTC()
		endUTC := endLocal.UTC()

		rows, err := pool.Query(r.Context(),
			`SELECT completed_at FROM workouts
			 WHERE completed_at IS NOT NULL
			   AND completed_at >= $1 AND completed_at < $2`,
			startUTC, endUTC)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		completed := map[string]bool{}
		for rows.Next() {
			var ts time.Time
			if err := rows.Scan(&ts); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
			key := ts.In(loc).Format("2006-01-02")
			completed[key] = true
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		prev := month.AddDate(0, -1, 0).Format("2006-01")
		next := month.AddDate(0, 1, 0).Format("2006-01")
		title := month.Format("January 2006")

		data := struct {
			Title  string
			PrevYM string
			NextYM string
			Cells  []DayCell
		}{
			Title:  title,
			PrevYM: prev,
			NextYM: next,
			Cells:  buildCells(loc, month, completed),
		}

		// full page or HTMX swap target
		files := []string{
			filepath.FromSlash("web/templates/base.gohtml"),
			filepath.FromSlash("web/templates/calendar.gohtml"),
		}
		t := mustTpl(files...)
		if r.Header.Get("HX-Request") == "true" {
			// return only the calendar div for HTMX
			if err := t.ExecuteTemplate(w, "calendar.gohtml", data); err != nil {
				http.Error(w, "template error", http.StatusInternalServerError)
				return
			}
			return
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
		today := time.Now().In(loadLoc()).Format("2006-01-02")
		data := struct {
			Day       int
			Items     []plan.Item
			WorkoutID int64
			Prev      map[string][]int
			Today     string
		}{Day: day, Items: items, WorkoutID: 0, Prev: prev, Today: today}
		if err := t.ExecuteTemplate(w, "base.gohtml", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	// Save draft: create workout if needed, then persist metadata and items
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
			// send hidden id back so subsequent posts include it
			fmt.Fprintf(w, `<input type="hidden" id="workout_id" name="workout_id" value="%d" hx-swap-oob="outerHTML">`, workoutID)
		}

		// Metadata after workout exists
		if d := r.FormValue("session_date"); d != "" {
			if t, err := time.ParseInLocation("2006-01-02", d, loadLoc()); err == nil {
				_, _ = pool.Exec(r.Context(), `UPDATE workouts SET session_date=$2 WHERE id=$1`, workoutID, t)
			}
		}
		if bw := r.FormValue("body_weight_kg"); bw != "" {
			_, _ = pool.Exec(r.Context(), `UPDATE workouts SET body_weight_kg=$2 WHERE id=$1`, workoutID, bw)
		}

		// Persist items idempotently
		for key := range r.PostForm {
			if !strings.HasPrefix(key, "it_") || !strings.HasSuffix(key, "_kind") {
				continue
			}
			idx := strings.TrimSuffix(strings.TrimPrefix(key, "it_"), "_kind")
			kind := r.PostFormValue("it_" + idx + "_kind")
			label := r.PostFormValue("it_" + idx + "_label")

			if _, err := pool.Exec(r.Context(), `DELETE FROM workout_items WHERE workout_id=$1 AND label=$2`, workoutID, label); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}

			switch kind {
			case "check":
				checked := r.PostFormValue("c_"+idx) != ""
				if _, err := pool.Exec(r.Context(),
					`INSERT INTO workout_items(workout_id,kind,label,checked) VALUES ($1,'check',$2,$3)`,
					workoutID, label, checked,
				); err != nil {
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
					if _, err := pool.Exec(r.Context(),
						`INSERT INTO workout_items(workout_id,kind,label,set_index,value_int) VALUES ($1,'sets',$2,$3,$4)`,
						workoutID, label, s, v,
					); err != nil {
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

		ctx := r.Context()
		tx, err := pool.Begin(ctx)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		// ensure workout exists
		var workoutID int64
		if idStr := r.FormValue("workout_id"); idStr != "" {
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				workoutID = id
			}
		}
		if workoutID == 0 {
			day, _ := strconv.Atoi(r.FormValue("day"))
			if err := tx.QueryRow(ctx, `INSERT INTO workouts(day_num) VALUES ($1) RETURNING id`, day).Scan(&workoutID); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
		}

		// metadata (date, body weight)
		if d := r.FormValue("session_date"); d != "" {
			if t, err := time.ParseInLocation("2006-01-02", d, loadLoc()); err == nil {
				if _, err := tx.Exec(ctx, `UPDATE workouts SET session_date=$2 WHERE id=$1`, workoutID, t); err != nil {
					http.Error(w, "db error", http.StatusInternalServerError)
					return
				}
			}
		}
		if bw := r.FormValue("body_weight_kg"); bw != "" {
			if _, err := tx.Exec(ctx, `UPDATE workouts SET body_weight_kg=$2 WHERE id=$1`, workoutID, bw); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
		}

		// persist items idempotently (same logic as save)
		for key := range r.PostForm {
			if !strings.HasPrefix(key, "it_") || !strings.HasSuffix(key, "_kind") {
				continue
			}
			idx := strings.TrimSuffix(strings.TrimPrefix(key, "it_"), "_kind")
			kind := r.PostFormValue("it_" + idx + "_kind")
			label := r.PostFormValue("it_" + idx + "_label")

			if _, err := tx.Exec(ctx, `DELETE FROM workout_items WHERE workout_id=$1 AND label=$2`, workoutID, label); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}

			switch kind {
			case "check":
				checked := r.PostFormValue("c_"+idx) != ""
				if _, err := tx.Exec(ctx,
					`INSERT INTO workout_items(workout_id,kind,label,checked) VALUES ($1,'check',$2,$3)`,
					workoutID, label, checked,
				); err != nil {
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
					if _, err := tx.Exec(ctx,
						`INSERT INTO workout_items(workout_id,kind,label,set_index,value_int) VALUES ($1,'sets',$2,$3,$4)`,
						workoutID, label, s, v,
					); err != nil {
						http.Error(w, "db error", http.StatusInternalServerError)
						return
					}
				}
			}
		}

		// complete
		tag, err := tx.Exec(ctx, `UPDATE workouts SET completed_at=now() WHERE id=$1`, workoutID)
		if err != nil || tag.RowsAffected() != 1 {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(ctx); err != nil {
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
