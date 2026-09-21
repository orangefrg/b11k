package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	syncer "b11k/internal/sync"
	"github.com/jackc/pgx/v5"
)

func (s *server) syncConnection(ctx context.Context) (*pgx.Conn, error) {
	if s.syncConnect != nil {
		return s.syncConnect(ctx)
	}
	return pggeo.Connect(ctx, s.cfg.PGUser, s.cfg.PGPassword, s.cfg.PGIP, s.cfg.PGPort, s.cfg.PGDatabase)
}

// Serialize refresh across workers/devices/processes and update all sessions for
// this athlete together. Jobs never persist bearer tokens in their payloads.
func (s *server) syncToken(ctx context.Context, athlete int64, force bool) (string, error) {
	conn, err := s.syncConnection(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", athlete); err != nil {
		return "", err
	}
	var access, refresh string
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT strava_access_token,strava_refresh_token,strava_expires_at FROM mobile_app_sessions
 WHERE athlete_id=$1 AND session_expires_at>NOW() ORDER BY strava_expires_at DESC,updated_at DESC LIMIT 1`, athlete).Scan(&access, &refresh, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &strava.APIError{Status: 401}
	}
	if err != nil {
		return "", err
	}
	access, err = s.decryptSecret(access)
	if err != nil {
		return "", err
	}
	if force || time.Until(expires) < 2*time.Minute {
		refresh, err = s.decryptSecret(refresh)
		if err != nil {
			return "", err
		}
		config := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.IOSRedirectURI)
		tokens, err := strava.RefreshAccessToken(*config, refresh)
		if err != nil {
			return "", err
		}
		if tokens.AccessToken == "" {
			return "", &strava.APIError{Status: 401}
		}
		access = tokens.AccessToken
		expires = stravaTokenExpiry(tokens.ExpiresAt)
		if tokens.RefreshToken != "" {
			refresh = tokens.RefreshToken
		}
		encryptedAccess, err := s.encryptSecret(access)
		if err != nil {
			return "", err
		}
		encryptedRefresh, err := s.encryptSecret(refresh)
		if err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `UPDATE mobile_app_sessions SET strava_access_token=$2,strava_refresh_token=$3,strava_expires_at=$4,updated_at=NOW() WHERE athlete_id=$1`, athlete, encryptedAccess, encryptedRefresh, expires); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	s.mobileMu.Lock()
	for key, session := range s.mobileSessions {
		if session.Athlete != nil && session.Athlete.ID == athlete {
			session.Token = access
			session.ExpiresAt = expires
			s.mobileSessions[key] = session
		}
	}
	s.mobileMu.Unlock()
	return access, nil
}

func (s *server) runSyncWorker() {
	for s.ctx.Err() == nil {
		if err := s.serveSyncJobs(); err != nil && s.ctx.Err() == nil {
			log.Printf("Sync worker disconnected; pending work is saved: %v", err)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
func (s *server) serveSyncJobs() error {
	conn, err := s.syncConnection(s.ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	worker := syncer.Worker{Jobs: syncer.Jobs{Conn: conn}, Discovered: syncer.DiscoveredMapConfig{Enabled: s.cfg.DiscoveredMapEnabled, RevealRadiusMeters: s.cfg.DiscoveredRevealRadiusMeters, SampleDistanceMeters: s.cfg.DiscoveredSampleDistanceMeters}, Client: func(athlete int64) *strava.Client {
		return &strava.Client{Token: func(ctx context.Context, force bool) (string, error) { return s.syncToken(ctx, athlete, force) }}
	}}
	for s.ctx.Err() == nil {
		var leader bool
		if err = conn.QueryRow(s.ctx, "SELECT pg_try_advisory_lock(48110, $1::integer)", syncer.WorkerLock).Scan(&leader); err != nil {
			return err
		}
		didWork := false
		if leader {
			didWork, err = worker.Step(s.ctx)
			_, unlockErr := conn.Exec(s.ctx, "SELECT pg_advisory_unlock(48110, $1::integer)", syncer.WorkerLock)
			if err != nil {
				return err
			}
			if unlockErr != nil {
				return unlockErr
			}
		}

		pause := 100 * time.Millisecond
		if !didWork {
			pause = time.Second
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-time.After(pause):
		}
	}
	return s.ctx.Err()
}

func syncTimeframe(r *http.Request) (time.Time, time.Time, error) {
	var start, end time.Time
	for key, target := range map[string]*time.Time{"start": &start, "end": &end} {
		value := r.URL.Query().Get(key)
		if value == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return start, end, fmt.Errorf("invalid %s date", key)
		}
		if key == "end" {
			parsed = parsed.AddDate(0, 0, 1)
		} // date range includes the entire end date (UTC)
		*target = parsed
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return start, end, fmt.Errorf("start must not follow end")
	}
	return start, end, nil
}

func (s *server) handleMobileSyncJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mobile/sync/jobs"), "/")
	parts := strings.Split(path, "/")
	id, action := parts[0], ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	if id != "" && (len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "") {
		http.NotFound(w, r)
		return
	}
	var start, end time.Time
	if r.Method == http.MethodPost && id == "" {
		var err error
		start, end, err = syncTimeframe(r)
		effectiveEnd := end
		if effectiveEnd.IsZero() {
			effectiveEnd = time.Now()
		}
		if err != nil || (!start.IsZero() && !start.Before(effectiveEnd)) {
			http.Error(w, "Choose a valid date range.", http.StatusBadRequest)
			return
		}
	}
	var job *syncer.Job
	err := s.withDB(func(conn *pgx.Conn) error {
		jobs := syncer.Jobs{Conn: conn}
		var err error
		switch {
		case r.Method == http.MethodGet && action == "":
			job, err = jobs.Get(r.Context(), session.Athlete.ID, id)
		case r.Method == http.MethodPost && id == "":
			job, err = jobs.Start(r.Context(), session.Athlete.ID, start, end)
		case r.Method == http.MethodPost && (action == "cancel" || action == "retry"):
			job, err = jobs.Change(r.Context(), session.Athlete.ID, id, action)
		default:
			return pgx.ErrNoRows
		}
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if id == "" && r.Method == http.MethodGet {
			writeJSON(w, map[string]any{"job": nil})
			return
		}
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("Sync job request failed: %v", err)
		http.Error(w, "Sync is temporarily unavailable. Please try again.", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodPost && id == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
	}
	writeJSON(w, map[string]any{"job": job})
}

func (s *server) startSyncJob(ctx context.Context, athlete int64, start, end time.Time) (*syncer.Job, error) {
	var job *syncer.Job
	err := s.withDB(func(conn *pgx.Conn) error {
		var err error
		job, err = (syncer.Jobs{Conn: conn}).Start(ctx, athlete, start, end)
		return err
	})
	return job, err
}
func (s *server) waitSyncJob(ctx context.Context, athlete int64, id string, progress func(*syncer.Job)) (*syncer.Job, error) {
	for {
		var job *syncer.Job
		err := s.withDB(func(conn *pgx.Conn) error {
			var err error
			job, err = (syncer.Jobs{Conn: conn}).Get(ctx, athlete, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		if progress != nil {
			progress(job)
		}
		if !job.Active() || job.State == "needs_auth" {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
