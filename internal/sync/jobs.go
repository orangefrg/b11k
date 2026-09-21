package sync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	"github.com/jackc/pgx/v5"
)

// Worker claims use the two-integer advisory namespace; athlete locks use bigint.
const WorkerLock int32 = 1
const activeStates = "('queued','running','waiting','needs_auth')"

type Summary struct {
	Total    int `json:"total"`
	Existing int `json:"existing"`
	New      int `json:"new"`
	Success  int `json:"success"`
	Failed   int `json:"failed"`
	Pending  int `json:"pending"`
}
type ItemFailure struct {
	ActivityID int64  `json:"activity_id"`
	Message    string `json:"message"`
}
type Job struct {
	ID               string        `json:"id"`
	AthleteID        int64         `json:"-"`
	State            string        `json:"state"`
	Phase            string        `json:"phase"`
	StartAt          *time.Time    `json:"start_at"`
	EndAt            time.Time     `json:"end_at"`
	Page             int           `json:"-"`
	DiscoveryDone    bool          `json:"discovery_done"`
	Attempts         int           `json:"-"`
	NextAttemptAt    time.Time     `json:"next_attempt_at"`
	Message          string        `json:"message"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	FinishedAt       *time.Time    `json:"finished_at"`
	LastSuccessfulAt *time.Time    `json:"last_successful_at"`
	Summary          Summary       `json:"summary"`
	Failures         []ItemFailure `json:"failures"`
}

func (j *Job) Active() bool {
	return j.State == "queued" || j.State == "running" || j.State == "waiting" || j.State == "needs_auth"
}

type Jobs struct{ Conn *pgx.Conn }

func (s Jobs) Get(ctx context.Context, athlete int64, id string) (*Job, error) {
	j := &Job{Failures: []ItemFailure{}}
	err := s.Conn.QueryRow(ctx, `SELECT id,athlete_id,state,phase,start_at,end_at,page,discovery_done,attempts,next_attempt_at,message,created_at,updated_at,finished_at,
 (SELECT MAX(finished_at) FROM sync_jobs WHERE athlete_id=$1 AND state='complete')
 FROM sync_jobs WHERE athlete_id=$1 AND ($2='' OR id=$2)
 ORDER BY (state IN `+activeStates+`) DESC,created_at DESC LIMIT 1`, athlete, id).Scan(&j.ID, &j.AthleteID, &j.State, &j.Phase, &j.StartAt, &j.EndAt, &j.Page, &j.DiscoveryDone, &j.Attempts, &j.NextAttemptAt, &j.Message, &j.CreatedAt, &j.UpdatedAt, &j.FinishedAt, &j.LastSuccessfulAt)
	if err != nil {
		return nil, err
	}
	err = s.Conn.QueryRow(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE state='existing'),COUNT(*) FILTER(WHERE state='success'),COUNT(*) FILTER(WHERE state='failed'),COUNT(*) FILTER(WHERE state='pending') FROM sync_job_items WHERE job_id=$1`, j.ID).Scan(&j.Summary.Total, &j.Summary.Existing, &j.Summary.Success, &j.Summary.Failed, &j.Summary.Pending)
	if err != nil {
		return nil, err
	}
	j.Summary.New = j.Summary.Total - j.Summary.Existing
	rows, err := s.Conn.Query(ctx, `SELECT activity_id,error FROM sync_job_items WHERE job_id=$1 AND state='failed' ORDER BY activity_id DESC LIMIT 20`, j.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f ItemFailure
		if err = rows.Scan(&f.ActivityID, &f.Message); err != nil {
			return nil, err
		}
		j.Failures = append(j.Failures, f)
	}
	return j, rows.Err()
}

func (s Jobs) Start(ctx context.Context, athlete int64, start, end time.Time) (*Job, error) {
	if athlete <= 0 {
		return nil, fmt.Errorf("invalid athlete")
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if !start.IsZero() && !start.Before(end) {
		return nil, fmt.Errorf("start must precede end")
	}
	tx, err := s.Conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", -athlete); err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, "SELECT id FROM sync_jobs WHERE athlete_id=$1 AND state IN "+activeStates, athlete).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		var bytes [16]byte
		if _, err = rand.Read(bytes[:]); err != nil {
			return nil, err
		}
		id = hex.EncodeToString(bytes[:])
		var first *time.Time
		if !start.IsZero() {
			first = &start
		}
		_, err = tx.Exec(ctx, "INSERT INTO sync_jobs(id,athlete_id,start_at,end_at) VALUES($1,$2,$3,$4)", id, athlete, first, end)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, athlete, id)
}

func (s Jobs) Change(ctx context.Context, athlete int64, id, action string) (*Job, error) {
	tx, err := s.Conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", -athlete); err != nil {
		return nil, err
	}
	var state string
	if err = tx.QueryRow(ctx, "SELECT state FROM sync_jobs WHERE id=$1 AND athlete_id=$2 FOR UPDATE", id, athlete).Scan(&state); err != nil {
		return nil, err
	}
	switch action {
	case "cancel":
		_, err = tx.Exec(ctx, "UPDATE sync_jobs SET state='cancelled',message='Cancelled; imported activities are saved.',finished_at=NOW(),updated_at=NOW() WHERE id=$1 AND state IN "+activeStates, id)
	case "retry":
		var activeID string
		err = tx.QueryRow(ctx, "SELECT id FROM sync_jobs WHERE athlete_id=$1 AND state IN "+activeStates, athlete).Scan(&activeID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if activeID != "" && activeID != id {
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
			return s.Get(ctx, athlete, activeID)
		}
		if state == "failed" || state == "partial" || state == "needs_auth" || state == "cancelled" {
			if _, err = tx.Exec(ctx, "UPDATE sync_job_items SET state='pending',attempts=0,error='' WHERE job_id=$1 AND state='failed'", id); err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, "UPDATE sync_jobs SET state='queued',attempts=0,next_attempt_at=NOW(),message='',finished_at=NULL,updated_at=NOW() WHERE id=$1", id)
		} else {
			err = nil
		}
	default:
		return nil, fmt.Errorf("unknown job action")
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, athlete, id)
}

// A session advisory lock on the worker connection is the claim. PostgreSQL
// releases it on crash/disconnect; a disconnected worker cannot commit a stale
// result. No extra queue service or wall-clock lease renewal is required.
type Worker struct {
	AthleteID  int64 // zero selects any athlete; legacy CLI restricts its credential scope
	Jobs       Jobs
	Client     func(int64) *strava.Client
	Discovered DiscoveredMapConfig
}

func (w Worker) Step(ctx context.Context) (bool, error) {
	var id string
	var athlete int64
	err := w.Jobs.Conn.QueryRow(ctx, `SELECT id,athlete_id FROM sync_jobs WHERE state IN ('queued','running','waiting') AND next_attempt_at<=NOW() AND ($1::bigint=0 OR athlete_id=$1) ORDER BY updated_at,id LIMIT 1`, w.AthleteID).Scan(&id, &athlete)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	j, err := w.Jobs.Get(ctx, athlete, id)
	if err != nil {
		return false, err
	}
	c := w.Client(athlete)
	c.BeforeRequest = func(ctx context.Context) error {
		var blocked time.Time
		if err := w.Jobs.Conn.QueryRow(ctx, "SELECT blocked_until FROM sync_api_budget WHERE id=1").Scan(&blocked); err != nil {
			return err
		}
		if time.Now().Before(blocked) {
			return &strava.APIError{Status: 429, RetryAt: blocked}
		}
		return nil
	}
	c.ObserveHeaders = func(ctx context.Context, h http.Header) error {
		until := strava.QuotaReset(h, time.Now())
		if until.IsZero() {
			return nil
		}
		_, err := w.Jobs.Conn.Exec(ctx, "UPDATE sync_api_budget SET blocked_until=GREATEST(blocked_until,$1) WHERE id=1", until)
		return err
	}
	var activityID int64
	var encoded []byte
	var attempts int
	err = w.Jobs.Conn.QueryRow(ctx, `SELECT activity_id,summary,attempts FROM sync_job_items WHERE job_id=$1 AND state='pending' ORDER BY summary->>'start_date' DESC,activity_id DESC LIMIT 1`, id).Scan(&activityID, &encoded, &attempts)
	if err == nil {
		var summary strava.ActivitySummary
		if err = json.Unmarshal(encoded, &summary); err != nil {
			return true, err
		}
		summary.StartDateTime, err = time.Parse(time.RFC3339, summary.StartDate)
		if err != nil {
			return true, err
		}
		summary.AthleteID = athlete
		activity, fetchErr := c.Detail(ctx, summary)
		if fetchErr != nil {
			return true, w.fail(ctx, j, activityID, attempts, fetchErr)
		}
		tx, err := w.Jobs.Conn.Begin(ctx)
		if err != nil {
			return true, err
		}
		defer tx.Rollback(ctx)
		if active, err := lockActive(ctx, tx, id); err != nil || !active {
			return true, err
		}
		if err = pggeo.SaveActivity(ctx, tx, activity); err != nil {
			tx.Rollback(ctx)
			return true, w.fail(ctx, j, activityID, attempts, err)
		}
		if _, err = tx.Exec(ctx, "UPDATE sync_job_items SET state='success',error='' WHERE job_id=$1 AND activity_id=$2", id, activityID); err != nil {
			return true, err
		}
		if _, err = tx.Exec(ctx, "UPDATE sync_jobs SET state='running',phase='importing',message='',attempts=0,updated_at=NOW(),next_attempt_at=NOW() WHERE id=$1", id); err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return true, err
	}
	if !j.DiscoveryDone {
		start := time.Time{}
		if j.StartAt != nil {
			start = *j.StartAt
		}
		batch, done, fetchErr := c.ActivitiesPage(ctx, athlete, start, j.EndAt, j.Page)
		if fetchErr != nil {
			return true, w.fail(ctx, j, 0, j.Attempts, fetchErr)
		}
		tx, err := w.Jobs.Conn.Begin(ctx)
		if err != nil {
			return true, err
		}
		defer tx.Rollback(ctx)
		if active, err := lockActive(ctx, tx, id); err != nil || !active {
			return true, err
		}
		for _, summary := range batch {
			encoded, err := json.Marshal(summary)
			if err != nil {
				return true, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO sync_job_items(job_id,activity_id,summary,state)
    VALUES($1,$2,$3,CASE WHEN EXISTS(SELECT 1 FROM activity_summaries WHERE id=$2 AND athlete_id=$4 AND sync_version>=1) THEN 'existing' ELSE 'pending' END) ON CONFLICT DO NOTHING`, id, summary.ID, encoded, athlete)
			if err != nil {
				return true, err
			}
		}
		_, err = tx.Exec(ctx, "UPDATE sync_jobs SET page=page+1,discovery_done=$2,state='running',phase='discovering',attempts=0,message='',updated_at=NOW(),next_attempt_at=NOW() WHERE id=$1", id, done)
		if err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	if _, err = w.Jobs.Conn.Exec(ctx, "UPDATE sync_jobs SET phase='finalizing' WHERE id=$1 AND state IN "+activeStates, id); err != nil {
		return true, err
	}
	if w.Discovered.Enabled && j.Summary.Success > 0 {
		if _, err = pggeo.RebuildDiscoveredCoverage(ctx, w.Jobs.Conn, athlete, w.Discovered.SampleDistanceMeters, w.Discovered.RevealRadiusMeters); err != nil {
			return true, w.fail(ctx, j, 0, j.Attempts, fmt.Errorf("map refresh failed: %w", err))
		}
	}
	state, message := "complete", "Sync complete."
	if j.Summary.Failed > 0 {
		state = "partial"
		message = "Some activities need attention. Retry failed items."
	}
	_, err = w.Jobs.Conn.Exec(ctx, "UPDATE sync_jobs SET state=$2,phase='finished',message=$3,finished_at=NOW(),updated_at=NOW() WHERE id=$1 AND state IN "+activeStates, id, state, message)
	return true, err
}

func lockActive(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var state string
	if err := tx.QueryRow(ctx, "SELECT state FROM sync_jobs WHERE id=$1 FOR UPDATE", id).Scan(&state); err != nil {
		return false, err
	}
	return state == "running" || state == "queued" || state == "waiting", nil
}

func (w Worker) fail(ctx context.Context, j *Job, item int64, attempts int, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log.Printf("Sync step failed (job %s, activity %d): %v", j.ID, item, cause)
	state, message := "waiting", "Temporary failure; retrying automatically."
	next := time.Now().Add(time.Duration(1<<min(attempts, 6)) * 5 * time.Second)
	var api *strava.APIError
	quota, permanent := false, false
	if errors.As(cause, &api) {
		quota = api.Status == 429
		if quota {
			message = "Waiting for Strava's request limit to reset."
			next = api.RetryAt
			if next.IsZero() {
				next = time.Now().Add(15 * time.Minute)
			}
		} else if api.Status == 401 {
			state = "needs_auth"
			message = "Reconnect Strava, then retry this sync."
		} else if !api.Temporary() {
			permanent = true
			message = api.Error()
		}
	}
	if !quota && state != "needs_auth" && (permanent || attempts >= 2) {
		state = "failed"
		message = "Could not finish this step. Retry to try again."
		if api != nil {
			message = api.Error()
		}
	}
	tx, err := w.Jobs.Conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if active, err := lockActive(ctx, tx, j.ID); err != nil || !active {
		return err
	}
	if quota {
		if _, err = tx.Exec(ctx, "UPDATE sync_api_budget SET blocked_until=GREATEST(blocked_until,$1) WHERE id=1", next); err != nil {
			return err
		}
	}
	if item != 0 && !quota && state != "needs_auth" {
		itemState := "pending"
		if state == "failed" {
			itemState = "failed"
			state = "running"
			next = time.Now()
		}
		if _, err = tx.Exec(ctx, "UPDATE sync_job_items SET attempts=attempts+1,state=$3,error=$4 WHERE job_id=$1 AND activity_id=$2", j.ID, item, itemState, message); err != nil {
			return err
		}
	}
	increment := 1
	if quota || state == "needs_auth" || item != 0 {
		increment = 0
	}
	_, err = tx.Exec(ctx, "UPDATE sync_jobs SET state=$2,message=$3,next_attempt_at=$4,attempts=attempts+$5,updated_at=NOW() WHERE id=$1", j.ID, state, message, next, increment)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
