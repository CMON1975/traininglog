package main

import (
	"context"
	"encoding/csv"
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

type listRow struct {
	ID        int64
	DayNum    int
	Date      string
	Completed bool
}

type checkRow struct {
	Label   string
	Checked bool
}
type setRow struct {
	Label  string
	Values []int
}

type DayCell struct {
	Date      time.Time
	InMonth   bool
	Completed bool
	Count     int
}

func loadLoc() *time.Location {
	if loc, err := time.LoadLocation("America/Vancouver"); err == nil {
		return loc
	}
	return time.Local
}

func monthFromQuery(r *http.Request, loc *time.Location) (time.Time, string) {
	ym := r.URL.Query().Get("ym")
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

func buildCells(loc *time.Location, month time.Time, counts map[string]int) []DayCell {
	startOfMonth := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, loc)
	weekday := int(startOfMonth.Weekday()) // 0=Sun
	gridStart := startOfMonth.AddDate(0, 0, -weekday)
	cells := make([]DayCell, 42)
	for i := 0; i < 42; i++ {
		d := gridStart.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		c := counts[key]
		cells[i] = DayCell{
			Date:      d,
			InMonth:   d.Month() == month.Month(),
			Completed: c > 0,
			Count:     c,
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
		startUTC, endUTC := startLocal.UTC(), endLocal.UTC()

		type row struct {
			SessionDate *time.Time
			CompletedAt time.Time
		}

		rows, err := pool.Query(r.Context(),
			`SELECT session_date, completed_at
			FROM workouts
			WHERE completed_at IS NOT NULL
				AND (
					(session_date IS NOT NULL AND session_date >= $1 AND session_date < $2)
				OR (session_date IS NULL   AND completed_at >= $3 AND completed_at < $4)
					)`,
			startLocal, endLocal, startUTC, endUTC)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		counts := map[string]int{}
		for rows.Next() {
			var sd *time.Time
			var ct time.Time
			if err := rows.Scan(&sd, &ct); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
			var key string
			if sd != nil {
				key = sd.In(loc).Format("2006-01-02")
			} else {
				key = ct.In(loc).Format("2006-01-02")
			}
			counts[key]++
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
			Cells:  buildCells(loc, month, counts),
		}

		files := []string{
			filepath.FromSlash("web/templates/base.gohtml"),
			filepath.FromSlash("web/templates/calendar.gohtml"),
		}
		t := mustTpl(files...)
		if r.Header.Get("HX-Request") == "true" {
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

	// CSV export: one row per workout item (checks and sets). Includes workout metadata.
	mux.HandleFunc("/export.csv", func(w http.ResponseWriter, r *http.Request) {
		type row struct {
			WID          int64
			DayNum       int
			SessionDate  *time.Time
			BodyWeightKg *float64
			CompletedAt  *time.Time
			Kind         *string
			Label        *string
			SetIndex     *int32
			ValueInt     *int32
			Checked      *bool
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="traininglog_export.csv"`)

		writer := csv.NewWriter(w)
		defer writer.Flush()

		_ = writer.Write([]string{
			"workout_id", "day_num", "session_date", "body_weight_kg", "completed_at",
			"kind", "label", "set_index", "value_int", "checked",
		})

		q := `
SELECT
  w.id,
  w.day_num,
  w.session_date,
  w.body_weight_kg,
  w.completed_at,
  wi.kind,
  wi.label,
  wi.set_index,
  wi.value_int,
  wi.checked
FROM workouts w
LEFT JOIN workout_items wi ON wi.workout_id = w.id
ORDER BY w.id DESC, wi.label NULLS LAST, wi.set_index NULLS LAST;
`
		rows, err := pool.Query(r.Context(), q)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var rr row
			if err := rows.Scan(
				&rr.WID,
				&rr.DayNum,
				&rr.SessionDate,
				&rr.BodyWeightKg,
				&rr.CompletedAt,
				&rr.Kind,
				&rr.Label,
				&rr.SetIndex,
				&rr.ValueInt,
				&rr.Checked,
			); err != nil {
				http.Error(w, "db scan error", http.StatusInternalServerError)
				return
			}

			sd := ""
			if rr.SessionDate != nil {
				sd = rr.SessionDate.In(loadLoc()).Format("2006-01-02")
			}
			bw := ""
			if rr.BodyWeightKg != nil {
				bw = fmt.Sprintf("%.2f", *rr.BodyWeightKg)
			}
			ca := ""
			if rr.CompletedAt != nil {
				ca = rr.CompletedAt.In(loadLoc()).Format(time.RFC3339)
			}
			kind := ""
			if rr.Kind != nil {
				kind = *rr.Kind
			}
			label := ""
			if rr.Label != nil {
				label = *rr.Label
			}
			si := ""
			if rr.SetIndex != nil {
				si = strconv.Itoa(int(*rr.SetIndex))
			}
			vi := ""
			if rr.ValueInt != nil {
				vi = strconv.Itoa(int(*rr.ValueInt))
			}
			ch := ""
			if rr.Checked != nil {
				if *rr.Checked {
					ch = "true"
				} else {
					ch = "false"
				}
			}

			if err := writer.Write([]string{
				strconv.FormatInt(rr.WID, 10),
				strconv.Itoa(rr.DayNum),
				sd, bw, ca,
				kind, label, si, vi, ch,
			}); err != nil {
				http.Error(w, "csv write error", http.StatusInternalServerError)
				return
			}
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "db rows error", http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		loc := loadLoc()
		rows, err := pool.Query(r.Context(), `
	SELECT id, day_num, session_date, completed_at
	FROM workouts
	ORDER BY COALESCE(session_date, (completed_at AT TIME ZONE 'UTC' AT TIME ZONE $1)::date) DESC NULLS LAST, id DESC
	LIMIT 100
	`, loc.String())
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var out []listRow
		for rows.Next() {
			var id int64
			var day int
			var sd *time.Time
			var ct *time.Time
			if err := rows.Scan(&id, &day, &sd, &ct); err != nil {
				http.Error(w, "db scan error", http.StatusInternalServerError)
				return
			}
			var dstr string
			if sd != nil {
				dstr = sd.In(loc).Format("2006-01-02")
			} else if ct != nil {
				dstr = ct.In(loc).Format("2006-01-02")
			}
			out = append(out, listRow{
				ID:        id,
				DayNum:    day,
				Date:      dstr,
				Completed: ct != nil,
			})
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "db rows error", http.StatusInternalServerError)
			return
		}

		t := mustTpl("web/templates/base.gohtml", "web/templates/sessions.gohtml")
		if err := t.ExecuteTemplate(w, "base.gohtml", struct{ Rows []listRow }{Rows: out}); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc("/sessions/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/sessions/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
		loc := loadLoc()

		var day int
		var sd *time.Time
		var bw *float64
		var ct *time.Time
		err = pool.QueryRow(r.Context(),
			`SELECT day_num, session_date, body_weight_kg, completed_at FROM workouts WHERE id=$1`, id).
			Scan(&day, &sd, &bw, &ct)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		type itemRow struct {
			Kind     string
			Label    string
			SetIndex *int32
			ValueInt *int32
			Checked  *bool
		}
		rows, err := pool.Query(r.Context(), `
	SELECT kind, label, set_index, value_int, checked
	FROM workout_items
	WHERE workout_id=$1
	ORDER BY kind, label, set_index
	`, id)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var checks []checkRow
		tmpSets := map[string][]int{}
		for rows.Next() {
			var k string
			var lbl string
			var si *int32
			var vi *int32
			var ch *bool
			if err := rows.Scan(&k, &lbl, &si, &vi, &ch); err != nil {
				http.Error(w, "db scan error", http.StatusInternalServerError)
				return
			}
			if k == "check" {
				checked := ch != nil && *ch
				checks = append(checks, checkRow{Label: lbl, Checked: checked})
			} else if k == "sets" && si != nil && vi != nil {
				tmpSets[lbl] = append(tmpSets[lbl], int(*vi))
			}
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "db rows error", http.StatusInternalServerError)
			return
		}
		var sets []setRow
		for lbl, vals := range tmpSets {
			sets = append(sets, setRow{Label: lbl, Values: vals})
		}

		var dateStr string
		if sd != nil {
			dateStr = sd.In(loc).Format("2006-01-02")
		} else if ct != nil {
			dateStr = ct.In(loc).Format("2006-01-02")
		}
		var bwStr string
		if bw != nil {
			bwStr = fmt.Sprintf("%.2f", *bw)
		}

		data := struct {
			W struct {
				ID         int64
				DayNum     int
				Date       string
				Completed  bool
				BodyWeight string
			}
			Checks []checkRow
			Sets   []setRow
		}{}
		data.W.ID = id
		data.W.DayNum = day
		data.W.Date = dateStr
		data.W.Completed = ct != nil
		data.W.BodyWeight = bwStr
		data.Checks = checks
		data.Sets = sets

		t := mustTpl("web/templates/base.gohtml", "web/templates/session_show.gohtml")
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

func mustTpl(files ...string) *template.Template {
	t := template.New(filepath.Base(files[0]))
	t = t.Funcs(template.FuncMap{"seq": seq, "join": join})
	return template.Must(t.ParseFiles(files...))
}
