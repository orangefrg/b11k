package sync

import (
	"context"
	"fmt"
	"log"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
)

// SyncConfig holds configuration for the sync process
type SyncConfig struct {
	StravaAccessToken string
	DatabaseConfig    DatabaseConfig
	Timeframe         TimeframeConfig
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

// SyncActivitiesFromStrava is the main orchestration function that:
// 1. Fetches activities from Strava within the specified timeframe
// 2. Checks which activities already exist in the database
// 3. Fetches details and streams for new activities
// 4. Saves new activities to the database
// 5. Logs all major steps and errors
// If progressCallback is provided, it will be called to report progress
func SyncActivitiesFromStrava(ctx context.Context, config SyncConfig, progressCallback ProgressCallback) (*SyncResult, error) {
	startTime := time.Now()
	log.Printf("🚀 Starting Strava activity sync process")
	log.Printf("📅 Timeframe: %s to %s",
		config.Timeframe.StartTime.Format("2006-01-02 15:04:05"),
		config.Timeframe.EndTime.Format("2006-01-02 15:04:05"))

	result := &SyncResult{
		FailedActivities: make([]int64, 0),
		Errors:           make([]error, 0),
	}

	// Step 1: Connect to database
	log.Printf("🔌 Connecting to database...")
	conn, err := pggeo.Connect(ctx, config.DatabaseConfig.User, config.DatabaseConfig.Password,
		config.DatabaseConfig.Host, config.DatabaseConfig.Port, config.DatabaseConfig.Database)
	if err != nil {
		log.Printf("❌ Failed to connect to database: %v", err)
		return result, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close(ctx)
	log.Printf("✅ Successfully connected to database")

	// Step 2: Get current athlete info
	log.Printf("👤 Fetching current athlete info...")
	athlete, err := strava.FetchCurrentAthlete(config.StravaAccessToken)
	if err != nil {
		log.Printf("❌ Failed to fetch athlete info: %v", err)
		return result, fmt.Errorf("failed to fetch athlete info: %w", err)
	}
	log.Printf("✅ Found athlete: %s %s (ID: %d)", athlete.FirstName, athlete.LastName, athlete.ID)

	// Step 3: Fetch activities from Strava
	if progressCallback != nil {
		progressCallback("fetching_activities", 0, 0, "Fetching activities from Strava...")
	}
	log.Printf("📡 Fetching activities from Strava...")
	bikeActivities, err := strava.FetchBikeActivities(config.StravaAccessToken,
		config.Timeframe.StartTime, config.Timeframe.EndTime)
	if err != nil {
		log.Printf("❌ Failed to fetch activities from Strava: %v", err)
		result.Errors = append(result.Errors, fmt.Errorf("failed to fetch activities: %w", err))
		return result, fmt.Errorf("failed to fetch activities from Strava: %w", err)
	}

	// Set athlete_id for all activities
	for i := range bikeActivities {
		bikeActivities[i].AthleteID = athlete.ID
	}

	result.TotalActivitiesFound = len(bikeActivities)

	if progressCallback != nil {
		progressCallback("fetching_activities", len(bikeActivities), len(bikeActivities), fmt.Sprintf("Found %d activities", len(bikeActivities)))
	}

	if len(bikeActivities) == 0 {
		log.Printf("ℹ️ No bike activities found in the specified timeframe")
		result.ProcessingTime = time.Since(startTime)
		return result, nil
	} else {
		log.Printf("✅ Found %d bike activities from Strava", len(bikeActivities))
	}

	for _, activity := range bikeActivities {
		log.Printf("Activity: %s", activity.ToString())
	}

	// Step 4: Check which activities already exist in database
	log.Printf("🔍 Checking which activities already exist in database...")
	activityIDs := make([]int64, len(bikeActivities))
	for i, activity := range bikeActivities {
		activityIDs[i] = activity.ID
	}

	existsMap, err := pggeo.ActivitiesExistWithLogging(ctx, conn, activityIDs)
	if err != nil {
		log.Printf("❌ Failed to check existing activities: %v", err)
		result.Errors = append(result.Errors, fmt.Errorf("failed to check existing activities: %w", err))
		return result, fmt.Errorf("failed to check existing activities: %w", err)
	}

	// Count existing and new activities
	var newActivities strava.ActivitySummaryList
	for _, activity := range bikeActivities {
		if exists, ok := existsMap[activity.ID]; ok && exists {
			result.ExistingActivities++
		} else {
			newActivities = append(newActivities, activity)
			result.NewActivities++
		}
	}

	log.Printf("📊 Activity status: %d existing, %d new", result.ExistingActivities, result.NewActivities)

	if len(newActivities) == 0 {
		log.Printf("ℹ️ All activities already exist in database")
		result.ProcessingTime = time.Since(startTime)
		return result, nil
	}

	// Step 4: Fetch detailed activities and streams for new activities
	if progressCallback != nil {
		progressCallback("fetching_details", 0, len(newActivities), fmt.Sprintf("Fetching details for %d activities...", len(newActivities)))
	}
	log.Printf("📋 Fetching detailed information for %d new activities...", len(newActivities))

	// Fetch detailed activities with progress tracking
	detailedActivities, err := fetchDetailedActivitiesWithProgress(newActivities, config.StravaAccessToken, progressCallback)
	if err != nil {
		log.Printf("❌ Failed to fetch detailed activities: %v", err)
		result.Errors = append(result.Errors, fmt.Errorf("failed to fetch detailed activities: %w", err))
		// Continue with partial results if some activities were fetched successfully
	}

	if progressCallback != nil && len(detailedActivities) > 0 {
		progressCallback("fetching_details", len(detailedActivities), len(newActivities), fmt.Sprintf("Fetched details for %d activities", len(detailedActivities)))
	}

	// Step 5: Save new activities to database
	if progressCallback != nil {
		progressCallback("saving", 0, len(detailedActivities), fmt.Sprintf("Saving %d activities to database...", len(detailedActivities)))
	}
	log.Printf("💾 Saving %d new activities to database...", len(detailedActivities))
	for i, detailedActivity := range detailedActivities {
		activityID := detailedActivity.Summary.ID
		activityName := detailedActivity.Summary.Name
		log.Printf("💾 Saving activity %d/%d: %d (%s)", i+1, len(detailedActivities), activityID, activityName)

		if err := pggeo.InsertBikeActivityWithLogging(ctx, conn, &detailedActivity); err != nil {
			log.Printf("❌ Failed to save activity %d: %v", activityID, err)
			result.FailedActivities = append(result.FailedActivities, activityID)
			result.Errors = append(result.Errors, fmt.Errorf("failed to save activity %d: %w", activityID, err))
			if progressCallback != nil {
				progressCallback("saving", i+1, len(detailedActivities), fmt.Sprintf("Failed to save: %s", activityName))
			}
			continue
		}

		result.SuccessfullyProcessed++
		log.Printf("✅ Successfully saved activity %d", activityID)
		if progressCallback != nil {
			progressCallback("saving", i+1, len(detailedActivities), fmt.Sprintf("Saved: %s", activityName))
		}
	}

	// Final summary
	result.ProcessingTime = time.Since(startTime)
	log.Printf("🎉 Sync process completed!")
	log.Printf("📊 Final results:")
	log.Printf("   - Total activities found: %d", result.TotalActivitiesFound)
	log.Printf("   - Existing activities: %d", result.ExistingActivities)
	log.Printf("   - New activities: %d", result.NewActivities)
	log.Printf("   - Successfully processed: %d", result.SuccessfullyProcessed)
	log.Printf("   - Failed activities: %d", len(result.FailedActivities))
	log.Printf("   - Processing time: %v", result.ProcessingTime)

	if len(result.FailedActivities) > 0 {
		log.Printf("❌ Failed activity IDs: %v", result.FailedActivities)
	}

	if len(result.Errors) > 0 {
		log.Printf("⚠️ Total errors encountered: %d", len(result.Errors))
	}

	return result, nil
}

// fetchDetailedActivitiesWithProgress fetches detailed activities with progress tracking
func fetchDetailedActivitiesWithProgress(activities strava.ActivitySummaryList, accessToken string, progressCallback ProgressCallback) (strava.BikeActivityList, error) {
	var detailedActivities strava.BikeActivityList
	total := len(activities)

	// Fetch activities one by one to track progress
	for i, activity := range activities {
		// Create a single-item list and fetch it
		singleActivityList := strava.ActivitySummaryList{activity}
		results, err := singleActivityList.GetDetailedActivities(accessToken)
		if err != nil {
			log.Printf("⚠️ Failed to fetch details for activity %d: %v", activity.ID, err)
			// Continue with next activity
			if progressCallback != nil {
				progressCallback("fetching_details", i+1, total, fmt.Sprintf("Failed: %s", activity.Name))
			}
			continue
		}

		if len(results) > 0 {
			detailedActivities = append(detailedActivities, results[0])
		}

		// Report progress
		if progressCallback != nil {
			progressCallback("fetching_details", i+1, total, fmt.Sprintf("Fetched: %s", activity.Name))
		}
	}

	return detailedActivities, nil
}

// SyncActivitiesFromStravaWithRetry performs the sync with retry logic for failed activities
func SyncActivitiesFromStravaWithRetry(ctx context.Context, config SyncConfig, maxRetries int, progressCallback ProgressCallback) (*SyncResult, error) {
	log.Printf("🔄 Starting sync with retry logic (max retries: %d)", maxRetries)

	// Initial sync
	result, err := SyncActivitiesFromStrava(ctx, config, progressCallback)
	if err != nil {
		return result, err
	}

	// If no failed activities, we're done
	if len(result.FailedActivities) == 0 {
		return result, nil
	}

	// Retry failed activities
	for attempt := 1; attempt <= maxRetries && len(result.FailedActivities) > 0; attempt++ {
		log.Printf("🔄 Retry attempt %d for %d failed activities", attempt, len(result.FailedActivities))

		// Get connection for retry
		conn, err := pggeo.Connect(ctx, config.DatabaseConfig.User, config.DatabaseConfig.Password,
			config.DatabaseConfig.Host, config.DatabaseConfig.Port, config.DatabaseConfig.Database)
		if err != nil {
			log.Printf("❌ Failed to connect to database for retry: %v", err)
			break
		}

		// Process failed activities
		var stillFailed []int64
		for _, activityID := range result.FailedActivities {
			log.Printf("🔄 Retrying activity %d", activityID)

			// Fetch single activity details using existing function
			activities := strava.ActivitySummaryList{{ID: activityID}}
			detailedActivities, err := activities.GetDetailedActivities(config.StravaAccessToken)
			if err != nil || len(detailedActivities) == 0 {
				log.Printf("❌ Retry failed for activity %d: %v", activityID, err)
				stillFailed = append(stillFailed, activityID)
				continue
			}

			// Save to database
			if err := pggeo.InsertBikeActivityWithLogging(ctx, conn, &detailedActivities[0]); err != nil {
				log.Printf("❌ Retry save failed for activity %d: %v", activityID, err)
				stillFailed = append(stillFailed, activityID)
				continue
			}

			log.Printf("✅ Retry successful for activity %d", activityID)
			result.SuccessfullyProcessed++
		}

		conn.Close(ctx)
		result.FailedActivities = stillFailed

		if len(stillFailed) == 0 {
			log.Printf("✅ All activities successfully processed after retry")
			break
		}

		// Wait before next retry
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	return result, nil
}
