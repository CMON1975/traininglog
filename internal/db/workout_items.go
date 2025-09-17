package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PrevLatestByLabels returns the most recent completed values for each label.
// Values are ordered by set_index (e.g., [10, 6, 3, 0]).
func PrevLatestByLabels(ctx context.Context, pool *pgxpool.Pool, labels []string) (map[string][]int, error) {
	out := make(map[string][]int)
	if len(labels) == 0 {
		return out, nil
	}
	const q = `
WITH latest AS (
	SELECT wi.label, max(w.completed_at) AS maxc
	FROM workout_items wi
	JOIN workouts w ON w.id = wi.workout_id
	WHERE w.completed_at IS NOT NULL
		AND wi.kind = 'sets'
		AND wi.label = ANY($1)
	GROUP BY wi.label
)
SELECT wi.label, wi.set_index, wi.value_int
FROM workout_items wi
JOIN workouts w ON w.id = wi.workout_id
JOIN latest l ON l.label = wi.label AND w.completed_at = l.maxc
ORDER BY wi.label, wi.set_index;
`
	rows, err := pool.Query(ctx, q, labels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rec struct {
		label string
		idx   int
		val   int
	}
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.label, &r.idx, &r.val); err != nil {
			return nil, err
		}
		out[r.label] = append(out[r.label], r.val)
	}
	return out, rows.Err()
}
