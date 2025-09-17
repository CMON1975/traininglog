package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
-- Ensure base tables exist (new installs get the full schema)
CREATE TABLE IF NOT EXISTS workouts (
  id             BIGSERIAL PRIMARY KEY,
  day_num        INT NOT NULL CHECK (day_num BETWEEN 1 AND 12),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at   TIMESTAMPTZ,
  session_date   DATE,
  body_weight_kg NUMERIC(6,2)
);

CREATE TABLE IF NOT EXISTS workout_items (
  id         BIGSERIAL PRIMARY KEY,
  workout_id BIGINT NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
  kind       TEXT   NOT NULL CHECK (kind IN ('check','sets')),
  label      TEXT   NOT NULL,
  set_index  INT,
  value_int  INT,
  checked    BOOLEAN,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill columns for existing installs
ALTER TABLE workouts ADD COLUMN IF NOT EXISTS session_date DATE;
ALTER TABLE workouts ADD COLUMN IF NOT EXISTS body_weight_kg NUMERIC(6,2);

-- Indexes (safe to create now that columns exist)
CREATE INDEX IF NOT EXISTS idx_workout_items_workout_id ON workout_items(workout_id);
CREATE INDEX IF NOT EXISTS idx_workout_items_label ON workout_items(label);
CREATE INDEX IF NOT EXISTS idx_workouts_session_date ON workouts(session_date);
`

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}
