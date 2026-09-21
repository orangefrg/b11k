package sync

import (
	"context"
	"fmt"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
)

// SyncConfig holds configuration for the sync process
type SyncConfig struct {
	StravaAccessToken string
	DatabaseConfig    DatabaseConfig
	Timeframe         TimeframeConfig
	DiscoveredMap     DiscoveredMapConfig
}

type DiscoveredMapConfig struct {
	Enabled              bool
	RevealRadiusMeters   float64
	SampleDistanceMeters float64
}

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// TimeframeConfig holds timeframe configuration for fetching activities
type TimeframeConfig struct {
	StartTime time.Time
	EndTime   time.Time
}

// SyncResult holds the results of a sync operation
type SyncResult struct {
	TotalActivitiesFound  int
	ExistingActivities    int
	NewActivities         int
	SuccessfullyProcessed int
	FailedActivities      []int64
	ProcessingTime        time.Duration
	Errors                []error
}

// ProgressCallback is called to report sync progress
// phase: "fetching_activities", "fetching_details", "saving"
// current: current item being processed
// total: total items to process
// message: optional message describing current operation
type ProgressCallback func(phase string, current, total int, message string)

// Legacy CLI/web entry points share the durable importer. Only the worker
// holding PostgreSQL's advisory lock can import or finalize activities.
func SyncActivitiesFromStrava(ctx context.Context, config SyncConfig, progress ProgressCallback) (*SyncResult, error) {
	started := time.Now()
	result := &SyncResult{FailedActivities: []int64{}, Errors: []error{}}
	client := strava.NewClient(config.StravaAccessToken)
	athlete, err := client.Athlete(ctx)
	if err != nil {
		return result, err
	}
	conn, err := pggeo.Connect(ctx, config.DatabaseConfig.User, config.DatabaseConfig.Password, config.DatabaseConfig.Host, config.DatabaseConfig.Port, config.DatabaseConfig.Database)
	if err != nil {
		return result, err
	}
	defer conn.Close(context.Background())
	if err = pggeo.EnsureSyncSchema(ctx, conn); err != nil {
		return result, err
	}
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock(48110, $1::integer)", WorkerLock); err != nil {
		return result, err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(48110, $1::integer)", WorkerLock)
	jobs := Jobs{Conn: conn}
	job, err := jobs.Start(ctx, athlete.ID, config.Timeframe.StartTime, config.Timeframe.EndTime)
	if err != nil {
		return result, err
	}
	if job.State == "needs_auth" {
		job, err = jobs.Change(ctx, athlete.ID, job.ID, "retry")
		if err != nil {
			return result, err
		}
	}
	worker := Worker{Jobs: jobs, AthleteID: athlete.ID, Client: func(int64) *strava.Client { return strava.NewClient(config.StravaAccessToken) }, Discovered: config.DiscoveredMap}
	for job.Active() && job.State != "needs_auth" {
		worked, err := worker.Step(ctx)
		if err != nil {
			return result, err
		}
		job, err = jobs.Get(ctx, athlete.ID, job.ID)
		if err != nil {
			return result, err
		}
		if progress != nil {
			progress(job.Phase, job.Summary.Success+job.Summary.Existing, job.Summary.Total, job.Message)
		}
		if !worked {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	result.TotalActivitiesFound = job.Summary.Total
	result.ExistingActivities = job.Summary.Existing
	result.NewActivities = job.Summary.New
	result.SuccessfullyProcessed = job.Summary.Success
	result.ProcessingTime = time.Since(started)
	rows, err := conn.Query(ctx, "SELECT activity_id FROM sync_job_items WHERE job_id=$1 AND state='failed'", job.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return result, err
		}
		result.FailedActivities = append(result.FailedActivities, id)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if job.State != "complete" {
		return result, fmt.Errorf("sync %s: %s", job.State, job.Message)
	}
	return result, nil
}

func SyncActivitiesFromStravaWithRetry(ctx context.Context, config SyncConfig, _ int, progress ProgressCallback) (*SyncResult, error) {
	return SyncActivitiesFromStrava(ctx, config, progress)
}
