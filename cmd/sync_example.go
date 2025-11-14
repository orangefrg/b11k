package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"b11k/internal/sync"
)

// ExampleSyncActivities demonstrates how to use the sync functionality
// with different timeframes and configurations
func ExampleSyncActivities() {
	// Example configuration - replace with your actual values
	syncConfig := sync.SyncConfig{
		StravaAccessToken: "your_strava_access_token_here",
		DatabaseConfig: sync.DatabaseConfig{
			Host:     "localhost",
			Port:     "5432",
			User:     "your_db_user",
			Password: "your_db_password",
			Database: "your_db_name",
		},
		Timeframe: sync.TimeframeConfig{
			StartTime: time.Now().AddDate(0, 0, -7), // Last 7 days
			EndTime:   time.Time{},                  // No end time (current)
		},
	}

	// Perform sync with retry logic (no progress callback for CLI)
	ctx := context.Background()
	result, err := sync.SyncActivitiesFromStravaWithRetry(ctx, syncConfig, 3, nil)
	if err != nil {
		log.Fatalf("Sync failed: %v", err)
	}

	// Print detailed results
	fmt.Printf("Sync Results:\n")
	fmt.Printf("  Total activities found: %d\n", result.TotalActivitiesFound)
	fmt.Printf("  Existing activities: %d\n", result.ExistingActivities)
	fmt.Printf("  New activities: %d\n", result.NewActivities)
	fmt.Printf("  Successfully processed: %d\n", result.SuccessfullyProcessed)
	fmt.Printf("  Failed activities: %d\n", len(result.FailedActivities))
	fmt.Printf("  Processing time: %v\n", result.ProcessingTime)

	if len(result.FailedActivities) > 0 {
		fmt.Printf("Failed activity IDs: %v\n", result.FailedActivities)
	}
}

// ExampleSyncSpecificTimeframe demonstrates syncing activities for a specific date range
func ExampleSyncSpecificTimeframe() {
	// Sync activities for a specific week
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC)

	syncConfig := sync.SyncConfig{
		StravaAccessToken: "your_strava_access_token_here",
		DatabaseConfig: sync.DatabaseConfig{
			Host:     "localhost",
			Port:     "5432",
			User:     "your_db_user",
			Password: "your_db_password",
			Database: "your_db_name",
		},
		Timeframe: sync.TimeframeConfig{
			StartTime: startDate,
			EndTime:   endDate,
		},
	}

	ctx := context.Background()
	result, err := sync.SyncActivitiesFromStrava(ctx, syncConfig, nil)
	if err != nil {
		log.Fatalf("Sync failed: %v", err)
	}

	fmt.Printf("Synced activities for %s to %s:\n",
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	fmt.Printf("  New activities: %d\n", result.NewActivities)
	fmt.Printf("  Successfully processed: %d\n", result.SuccessfullyProcessed)
}
