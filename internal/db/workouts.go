package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func LastCompletedDay(ctx context.Context, pool *pgxpool.Pool) (int, bool, error) {
	const q = `SELECT day_num FROM workouts WHERE completed_at IS NOT NULL ORDER BY completed_at DESC LIMIT 1`
	var day int
	err := pool.QueryRow(ctx, q).Scan(&day)
	if err != nil {
		// no rows or real error: treat both as "none"
		return 0, false, nil
	}
	return day, true, nil
}

func NextDay(ctx context.Context, pool *pgxpool.Pool) int {
	if d, ok, _ := LastCompletedDay(ctx, pool); ok {
		n := (d % 12) + 1
		return n
	}
	return 1
}
