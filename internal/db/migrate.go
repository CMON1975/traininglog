package db

import (
	"context"
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
func Migrate(ctx context.Context, pool PgxPool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}

// PgxPool is the small interface we need (pgxpool.Pool satisfies it)
type PgxPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (ct interface{ RowsAffected() int64 }, err error)
}
