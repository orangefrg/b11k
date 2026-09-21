package pggeo

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Additive migration: old rows are deliberately unverified and repaired on import.
func EnsureSyncSchema(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
 ALTER TABLE point_samples ALTER COLUMN location DROP NOT NULL;
 ALTER TABLE activity_summaries ADD COLUMN IF NOT EXISTS sync_version INTEGER NOT NULL DEFAULT 0;
 CREATE TABLE IF NOT EXISTS sync_jobs (
  id TEXT PRIMARY KEY, athlete_id BIGINT NOT NULL,
  state TEXT NOT NULL DEFAULT 'queued', phase TEXT NOT NULL DEFAULT 'discovering',
  start_at TIMESTAMPTZ, end_at TIMESTAMPTZ NOT NULL, page INTEGER NOT NULL DEFAULT 1,
  discovery_done BOOLEAN NOT NULL DEFAULT FALSE, attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ
 );
 CREATE UNIQUE INDEX IF NOT EXISTS sync_jobs_one_active_athlete ON sync_jobs(athlete_id)
  WHERE state IN ('queued','running','waiting','needs_auth');
 CREATE INDEX IF NOT EXISTS sync_jobs_due ON sync_jobs(next_attempt_at) WHERE state IN ('queued','running','waiting');
 CREATE TABLE IF NOT EXISTS sync_job_items (
  job_id TEXT NOT NULL REFERENCES sync_jobs(id) ON DELETE CASCADE, activity_id BIGINT NOT NULL,
  summary JSONB NOT NULL, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '', PRIMARY KEY(job_id,activity_id)
 );
 CREATE TABLE IF NOT EXISTS sync_api_budget (id INTEGER PRIMARY KEY CHECK(id=1), blocked_until TIMESTAMPTZ NOT NULL);
 INSERT INTO sync_api_budget VALUES(1,'epoch') ON CONFLICT DO NOTHING;
 `)
	return err
}
