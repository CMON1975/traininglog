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

CREATE TABLE IF NOT EXISTS workout_items (
	id		 	BIGSERIAL PRIMARY KEY,
	workout_id	BIGINT NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
	kind		TEXT NOT NULL CHECK (kind IN ('check', 'sets')),
	label		TEXT NOT NULL,
	set_index	INT,
	value_int	INT,
	checked		BOOLEAN,
	created_at	TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workout_items_workout_id ON workout_items(workout_id);
CREATE INDEX IF NOT EXISTS idx_workout_items_label ON workout_items(label);
`

// Migrate ensures tables exist. Call once at startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}
