package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS workouts (
	id			 BIGSERIAL PRIMARY KEY,
	day_num		 INT NOT NULL CHECK (day_num BETWEEN 1 AND 12),
	created_at	 TIMESTAMPTZ NOT NULL DEFAULT now(),
	completed_at TIMESTAMPTZ
);
`

// Migrate ensures tables exist. Call once at startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}
