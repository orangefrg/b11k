package pggeo

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"b11k/internal/strava"

	"github.com/jackc/pgx/v5"
)

// GetActivityByID retrieves an activity summary by ID
func GetActivityByID(ctx context.Context, conn *pgx.Conn, athleteID, activityID int64) (*strava.ActivitySummary, error) {
	query := `
	SELECT id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		   type, sport_type, workout_type, start_date, utc_offset,
		   start_lat, start_lng, end_lat, end_lng,
		   location_city, location_state, location_country, gear_id,
		   average_speed, max_speed, average_cadence, average_watts,
		   kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	FROM activity_summaries
	WHERE athlete_id = $1 AND id = $2
	`

	row := conn.QueryRow(ctx, query, athleteID, activityID)

	var activity strava.ActivitySummary
	var startLat, startLng, endLat, endLng *float64
	var locationCity, locationState *string
	var workoutType *int

	err := row.Scan(
		&activity.ID, &activity.AthleteID, &activity.Name, &activity.Distance, &activity.MovingTime, &activity.ElapsedTime,
		&activity.TotalElevationGain, &activity.Type, &activity.SportType, &workoutType,
		&activity.StartDateTime, &activity.UtcOffset, &startLat, &startLng, &endLat, &endLng,
		&locationCity, &locationState, &activity.LocationCountry, &activity.GearID,
		&activity.AverageSpeed, &activity.MaxSpeed, &activity.AverageCadence, &activity.AverageWatts,
		&activity.Kilojoules, &activity.AverageHeartrate, &activity.MaxHeartrate, &activity.MaxWatts,
		&activity.SufferScore,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("activity with ID %d not found", activityID)
		}
		return nil, fmt.Errorf("failed to scan activity: %w", err)
	}

	// Set optional fields
	activity.WorkoutType = workoutType
	if startLat != nil && startLng != nil {
		activity.StartLatLng = &[]float64{*startLat, *startLng}
	}
	if endLat != nil && endLng != nil {
		activity.EndLatLng = &[]float64{*endLat, *endLng}
	}
	activity.LocationCity = locationCity
	activity.LocationState = locationState

	return &activity, nil
}

// GetActivitiesByDateRange retrieves activities within a date range for a specific athlete
func GetActivitiesByDateRange(ctx context.Context, conn *pgx.Conn, athleteID int64, startDate, endDate time.Time) ([]strava.ActivitySummary, error) {
	query := `
	SELECT id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		   type, sport_type, workout_type, start_date, utc_offset,
		   start_lat, start_lng, end_lat, end_lng,
		   location_city, location_state, location_country, gear_id,
		   average_speed, max_speed, average_cadence, average_watts,
		   kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	FROM activity_summaries
	WHERE athlete_id = $1 AND start_date >= $2 AND start_date <= $3
	ORDER BY start_date DESC
	`

	rows, err := conn.Query(ctx, query, athleteID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query activities: %w", err)
	}
	defer rows.Close()

	var activities []strava.ActivitySummary
	for rows.Next() {
		var activity strava.ActivitySummary
		var startLat, startLng, endLat, endLng *float64
		var locationCity, locationState *string
		var workoutType *int

		err := rows.Scan(
			&activity.ID, &activity.AthleteID, &activity.Name, &activity.Distance, &activity.MovingTime, &activity.ElapsedTime,
			&activity.TotalElevationGain, &activity.Type, &activity.SportType, &workoutType,
			&activity.StartDateTime, &activity.UtcOffset, &startLat, &startLng, &endLat, &endLng,
			&locationCity, &locationState, &activity.LocationCountry, &activity.GearID,
			&activity.AverageSpeed, &activity.MaxSpeed, &activity.AverageCadence, &activity.AverageWatts,
			&activity.Kilojoules, &activity.AverageHeartrate, &activity.MaxHeartrate, &activity.MaxWatts,
			&activity.SufferScore,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}

		// Set optional fields
		activity.WorkoutType = workoutType
		if startLat != nil && startLng != nil {
			activity.StartLatLng = &[]float64{*startLat, *startLng}
		}
		if endLat != nil && endLng != nil {
			activity.EndLatLng = &[]float64{*endLat, *endLng}
		}
		activity.LocationCity = locationCity
		activity.LocationState = locationState

		activities = append(activities, activity)
	}

	return activities, rows.Err()
}

// GetAllActivities retrieves all activities for a specific athlete ordered by start date descending
func GetAllActivities(ctx context.Context, conn *pgx.Conn, athleteID int64) ([]strava.ActivitySummary, error) {
	query := `
	SELECT id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		   type, sport_type, workout_type, start_date, utc_offset,
		   start_lat, start_lng, end_lat, end_lng,
		   location_city, location_state, location_country, gear_id,
		   average_speed, max_speed, average_cadence, average_watts,
		   kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	FROM activity_summaries
	WHERE athlete_id = $1
	ORDER BY start_date DESC
	`

	rows, err := conn.Query(ctx, query, athleteID)
	if err != nil {
		return nil, fmt.Errorf("failed to query activities: %w", err)
	}
	defer rows.Close()

	var activities []strava.ActivitySummary
	for rows.Next() {
		var activity strava.ActivitySummary
		var startLat, startLng, endLat, endLng *float64
		var locationCity, locationState *string
		var workoutType *int

		err := rows.Scan(
			&activity.ID, &activity.AthleteID, &activity.Name, &activity.Distance, &activity.MovingTime, &activity.ElapsedTime,
			&activity.TotalElevationGain, &activity.Type, &activity.SportType, &workoutType,
			&activity.StartDateTime, &activity.UtcOffset, &startLat, &startLng, &endLat, &endLng,
			&locationCity, &locationState, &activity.LocationCountry, &activity.GearID,
			&activity.AverageSpeed, &activity.MaxSpeed, &activity.AverageCadence, &activity.AverageWatts,
			&activity.Kilojoules, &activity.AverageHeartrate, &activity.MaxHeartrate, &activity.MaxWatts,
			&activity.SufferScore,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}

		// Set optional fields
		activity.WorkoutType = workoutType
		if startLat != nil && startLng != nil {
			activity.StartLatLng = &[]float64{*startLat, *startLng}
		}
		if endLat != nil && endLng != nil {
			activity.EndLatLng = &[]float64{*endLat, *endLng}
		}
		activity.LocationCity = locationCity
		activity.LocationState = locationState

		activities = append(activities, activity)
	}

	return activities, rows.Err()
}

// GetActivitiesInBoundingBox retrieves activities that intersect with a bounding box
func GetActivitiesInBoundingBox(ctx context.Context, conn *pgx.Conn, minLat, minLng, maxLat, maxLng float64) ([]strava.ActivitySummary, error) {
	query := `
	SELECT s.id, s.name, s.distance, s.moving_time, s.elapsed_time, s.total_elevation_gain,
		   s.type, s.sport_type, s.workout_type, s.start_date, s.utc_offset,
		   s.start_lat, s.start_lng, s.end_lat, s.end_lng,
		   s.location_city, s.location_state, s.location_country, s.gear_id,
		   s.average_speed, s.max_speed, s.average_cadence, s.average_watts,
		   s.kilojoules, s.average_heartrate, s.max_heartrate, s.max_watts, s.suffer_score
	FROM activity_summaries s
	JOIN activity_geometries g ON s.id = g.activity_id
	WHERE g.route_bbox_geom && ST_MakeEnvelope($1, $2, $3, $4, 4326)
	ORDER BY s.start_date DESC
	`

	rows, err := conn.Query(ctx, query, minLng, minLat, maxLng, maxLat)
	if err != nil {
		return nil, fmt.Errorf("failed to query activities in bounding box: %w", err)
	}
	defer rows.Close()

	var activities []strava.ActivitySummary
	for rows.Next() {
		var activity strava.ActivitySummary
		var startLat, startLng, endLat, endLng *float64
		var locationCity, locationState *string
		var workoutType *int

		err := rows.Scan(
			&activity.ID, &activity.AthleteID, &activity.Name, &activity.Distance, &activity.MovingTime, &activity.ElapsedTime,
			&activity.TotalElevationGain, &activity.Type, &activity.SportType, &workoutType,
			&activity.StartDateTime, &activity.UtcOffset, &startLat, &startLng, &endLat, &endLng,
			&locationCity, &locationState, &activity.LocationCountry, &activity.GearID,
			&activity.AverageSpeed, &activity.MaxSpeed, &activity.AverageCadence, &activity.AverageWatts,
			&activity.Kilojoules, &activity.AverageHeartrate, &activity.MaxHeartrate, &activity.MaxWatts,
			&activity.SufferScore,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}

		// Set optional fields
		activity.WorkoutType = workoutType
		if startLat != nil && startLng != nil {
			activity.StartLatLng = &[]float64{*startLat, *startLng}
		}
		if endLat != nil && endLng != nil {
			activity.EndLatLng = &[]float64{*endLat, *endLng}
		}
		activity.LocationCity = locationCity
		activity.LocationState = locationState

		activities = append(activities, activity)
	}

	return activities, rows.Err()
}

// GetPointSamplesForActivity retrieves all point samples for a specific activity
func GetPointSamplesForActivity(ctx context.Context, conn *pgx.Conn, athleteID, activityID int64) ([]PointSample, error) {
	query := `
	SELECT id, activity_id, athlete_id, point_index, time, 
		   ST_Y(location::geometry) as lat, ST_X(location::geometry) as lng,
		   altitude, heartrate, speed, watts, cadence, grade, moving, cumulative_distance
	FROM point_samples
	WHERE athlete_id = $1 AND activity_id = $2
	ORDER BY point_index
	`

	rows, err := conn.Query(ctx, query, athleteID, activityID)
	if err != nil {
		return nil, fmt.Errorf("failed to query point samples: %w", err)
	}
	defer rows.Close()

	var samples []PointSample
	for rows.Next() {
		var sample PointSample
		err := rows.Scan(
			&sample.ID, &sample.ActivityID, &sample.AthleteID, &sample.PointIndex, &sample.Time,
			&sample.Lat, &sample.Lng, &sample.Altitude, &sample.Heartrate,
			&sample.Speed, &sample.Watts, &sample.Cadence, &sample.Grade, &sample.Moving,
			&sample.CumulativeDistance,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan point sample: %w", err)
		}

		samples = append(samples, sample)
	}

	return samples, rows.Err()
}

// GraphDataPoint represents a single data point in a graph time series
type GraphDataPoint struct {
	Time     time.Time `json:"time"`
	Value    float64   `json:"value"`
	Zone     *int      `json:"zone,omitempty"`     // HR zone (1-5) if applicable
	Distance *float64  `json:"distance,omitempty"` // Cumulative distance in meters
}

// GraphData represents graph data for multiple metrics
type GraphData struct {
	Speed     []GraphDataPoint `json:"speed,omitempty"`
	Heartrate []GraphDataPoint `json:"heartrate,omitempty"`
	Height    []GraphDataPoint `json:"height,omitempty"`
	Cadence   []GraphDataPoint `json:"cadence,omitempty"`
}

// GetGraphDataForActivity retrieves graph data for specified metrics for an activity
func GetGraphDataForActivity(ctx context.Context, conn *pgx.Conn, athleteID, activityID int64, metrics []string, includeZones bool, hrZones *strava.HeartRateZones) (*GraphData, error) {
	samples, err := GetPointSamplesForActivity(ctx, conn, athleteID, activityID)
	if err != nil {
		return nil, err
	}

	result := &GraphData{}
	metricMap := make(map[string]bool)
	for _, m := range metrics {
		metricMap[m] = true
	}

	for _, sample := range samples {
		if metricMap["speed"] && sample.Speed != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: *sample.Speed,
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Speed = append(result.Speed, point)
		}
		if metricMap["heartrate"] && sample.Heartrate != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: float64(*sample.Heartrate),
			}
			if includeZones && hrZones != nil {
				zone := calculateHRZone(*sample.Heartrate, hrZones)
				point.Zone = &zone
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Heartrate = append(result.Heartrate, point)
		}
		if metricMap["height"] && sample.Altitude != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: *sample.Altitude,
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Height = append(result.Height, point)
		}
		if metricMap["cadence"] && sample.Cadence != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: float64(*sample.Cadence),
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Cadence = append(result.Cadence, point)
		}
	}

	return result, nil
}

// GetGraphDataForSegmentInActivity retrieves graph data for a segment portion of an activity
func GetGraphDataForSegmentInActivity(ctx context.Context, conn *pgx.Conn, athleteID, activityID, segmentID int64, metrics []string, includeZones bool, hrZones *strava.HeartRateZones) (*GraphData, error) {
	// First, get the segment's start and end indices in the activity
	var startIndex, endIndex int
	query := `SELECT * FROM find_segment_point_indices($1, $2, $3, $4)`
	err := conn.QueryRow(ctx, query, segmentID, activityID, athleteID, 15.0).Scan(&startIndex, &endIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to find segment indices: %w", err)
	}

	// Get all point samples for the activity
	samples, err := GetPointSamplesForActivity(ctx, conn, athleteID, activityID)
	if err != nil {
		return nil, err
	}

	// Filter samples to segment range
	segmentSamples := []PointSample{}
	for _, sample := range samples {
		if sample.PointIndex >= startIndex && sample.PointIndex <= endIndex {
			segmentSamples = append(segmentSamples, sample)
		}
	}

	result := &GraphData{}
	metricMap := make(map[string]bool)
	for _, m := range metrics {
		metricMap[m] = true
	}

	for _, sample := range segmentSamples {
		if metricMap["speed"] && sample.Speed != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: *sample.Speed,
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Speed = append(result.Speed, point)
		}
		if metricMap["heartrate"] && sample.Heartrate != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: float64(*sample.Heartrate),
			}
			if includeZones && hrZones != nil {
				zone := calculateHRZone(*sample.Heartrate, hrZones)
				point.Zone = &zone
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Heartrate = append(result.Heartrate, point)
		}
		if metricMap["height"] && sample.Altitude != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: *sample.Altitude,
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Height = append(result.Height, point)
		}
		if metricMap["cadence"] && sample.Cadence != nil {
			point := GraphDataPoint{
				Time:  sample.Time,
				Value: float64(*sample.Cadence),
			}
			if sample.CumulativeDistance != nil {
				point.Distance = sample.CumulativeDistance
			}
			result.Cadence = append(result.Cadence, point)
		}
	}

	return result, nil
}

// calculateHRZone determines which HR zone (1-5) a heart rate value falls into
func calculateHRZone(hr int, zones *strava.HeartRateZones) int {
	if zones == nil || len(zones.Zones) == 0 {
		return 0
	}
	for i, zone := range zones.Zones {
		if hr >= zone.Min && hr <= zone.Max {
			return i + 1 // Zones are 1-indexed
		}
	}
	// If HR is above all zones, return the highest zone
	if len(zones.Zones) > 0 {
		return len(zones.Zones)
	}
	return 0
}

// PointSample represents a single point sample from an activity
type PointSample struct {
	ID                int64     `json:"id"`
	ActivityID        int64     `json:"activity_id"`
	AthleteID         int64     `json:"athlete_id"`
	PointIndex        int       `json:"point_index"`
	Time              time.Time `json:"time"`
	Lat               float64   `json:"lat"`
	Lng               float64   `json:"lng"`
	Altitude          *float64  `json:"altitude,omitempty"`
	Heartrate         *int      `json:"heartrate,omitempty"`
	Speed             *float64  `json:"speed,omitempty"`
	Watts             *int      `json:"watts,omitempty"`
	Cadence           *int      `json:"cadence,omitempty"`
	Grade             *float64  `json:"grade,omitempty"`
	Moving            *bool     `json:"moving,omitempty"`
	CumulativeDistance *float64  `json:"cumulative_distance,omitempty"`
}

// ActivityNearResult represents the result of finding activities near a point
type ActivityNearResult struct {
	ActivityID int64   `json:"activity_id"`
	MinDistM   float64 `json:"min_dist_m"`
}

// ActivityIntersectionResult represents the result of finding activities intersecting a line
type ActivityIntersectionResult struct {
	ActivityID     int64   `json:"activity_id"`
	MinDistanceM   float64 `json:"min_distance_m"`
	OverlapLengthM float64 `json:"overlap_length_m"`
}

// FindActivitiesNear finds activities within a specified radius of a point
func FindActivitiesNear(ctx context.Context, conn *pgx.Conn, lon, lat, radiusMeters float64) ([]ActivityNearResult, error) {
	query := `SELECT * FROM find_activities_near($1, $2, $3)`

	rows, err := conn.Query(ctx, query, lon, lat, radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("failed to find activities near point: %w", err)
	}
	defer rows.Close()

	var results []ActivityNearResult
	for rows.Next() {
		var result ActivityNearResult
		err := rows.Scan(&result.ActivityID, &result.MinDistM)
		if err != nil {
			return nil, fmt.Errorf("failed to scan activity near result: %w", err)
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// FindActivitiesIntersectingLine finds activities that intersect with a given line
func FindActivitiesIntersectingLine(ctx context.Context, conn *pgx.Conn, lineWKT string, toleranceMeters float64) ([]ActivityIntersectionResult, error) {
	query := `SELECT * FROM find_activities_intersecting_line(ST_GeogFromText($1), $2)`

	rows, err := conn.Query(ctx, query, lineWKT, toleranceMeters)
	if err != nil {
		return nil, fmt.Errorf("failed to find activities intersecting line: %w", err)
	}
	defer rows.Close()

	var results []ActivityIntersectionResult
	for rows.Next() {
		var result ActivityIntersectionResult
		err := rows.Scan(&result.ActivityID, &result.MinDistanceM, &result.OverlapLengthM)
		if err != nil {
			return nil, fmt.Errorf("failed to scan activity intersection result: %w", err)
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// RefreshActivitySimplified refreshes the simplified geometry for a specific activity
func RefreshActivitySimplified(ctx context.Context, conn *pgx.Conn, activityID int64, toleranceMeters float64) error {
	query := `SELECT refresh_activity_simplified($1, $2)`
	_, err := conn.Exec(ctx, query, activityID, toleranceMeters)
	return err
}

// RefreshAllSimplified refreshes the simplified geometry for all activities
func RefreshAllSimplified(ctx context.Context, conn *pgx.Conn, toleranceMeters float64) error {
	query := `SELECT refresh_all_simplified($1)`
	_, err := conn.Exec(ctx, query, toleranceMeters)
	return err
}

// ActivityExists checks if an activity with the given ID already exists in the database
func ActivityExists(ctx context.Context, conn *pgx.Conn, activityID int64) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM activity_summaries WHERE id = $1)`
	var exists bool
	err := conn.QueryRow(ctx, query, activityID).Scan(&exists)
	return exists, err
}

// ActivitiesExist checks which activities from a list already exist in the database
func ActivitiesExist(ctx context.Context, conn *pgx.Conn, activityIDs []int64) (map[int64]bool, error) {
	if len(activityIDs) == 0 {
		return make(map[int64]bool), nil
	}

	// Build the query with placeholders
	placeholders := make([]string, len(activityIDs))
	args := make([]interface{}, len(activityIDs))
	for i, id := range activityIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, true as exists 
		FROM activity_summaries 
		WHERE id IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing activities: %w", err)
	}
	defer rows.Close()

	existsMap := make(map[int64]bool)
	for rows.Next() {
		var id int64
		var exists bool
		if err := rows.Scan(&id, &exists); err != nil {
			return nil, fmt.Errorf("failed to scan activity existence: %w", err)
		}
		existsMap[id] = true
	}

	// Fill in false for activities that don't exist
	for _, id := range activityIDs {
		if !existsMap[id] {
			existsMap[id] = false
		}
	}

	return existsMap, rows.Err()
}

// GetExistingActivityIDs returns a set of activity IDs that already exist in the database
func GetExistingActivityIDs(ctx context.Context, conn *pgx.Conn, activityIDs []int64) (map[int64]struct{}, error) {
	existsMap, err := ActivitiesExist(ctx, conn, activityIDs)
	if err != nil {
		return nil, err
	}

	existingIDs := make(map[int64]struct{})
	for id, exists := range existsMap {
		if exists {
			existingIDs[id] = struct{}{}
		}
	}

	return existingIDs, nil
}

// ActivitiesExistWithLogging checks which activities from a list exist in the database with logging
func ActivitiesExistWithLogging(ctx context.Context, conn *pgx.Conn, activityIDs []int64) (map[int64]bool, error) {
	log.Printf("🔍 Checking existence of %d activities in database", len(activityIDs))

	existsMap, err := ActivitiesExist(ctx, conn, activityIDs)
	if err != nil {
		log.Printf("❌ Error checking activities existence: %v", err)
		return nil, fmt.Errorf("failed to check activities existence: %w", err)
	}

	existingCount := 0
	for _, exists := range existsMap {
		if exists {
			existingCount++
		}
	}

	log.Printf("📊 Found %d existing activities out of %d checked", existingCount, len(activityIDs))

	return existsMap, nil
}

// SegmentActivityMatch represents a cached match between a segment and activity
type SegmentActivityMatch struct {
	SegmentID         int64   `json:"segment_id"`
	ActivityID        int64   `json:"activity_id"`
	ToleranceMeters   float64 `json:"tolerance_meters"`
	MinDistanceM      float64 `json:"min_distance_m"`
	OverlapLengthM    float64 `json:"overlap_length_m"`
	OverlapPercentage float64 `json:"overlap_percentage"`
	CachedAt          string  `json:"cached_at"`
}

// ActivityWithMatch represents an activity with its match metadata
type ActivityWithMatch struct {
	strava.ActivitySummary
	MinDistanceM       float64  `json:"min_distance_m"`
	OverlapLengthM     float64  `json:"overlap_length_m"`
	OverlapPercentage  float64  `json:"overlap_percentage"`
	StartDateFormatted string   `json:"start_date_formatted"`             // Formatted date for display
	SegmentAvgHR       *float64 `json:"segment_avg_hr,omitempty"`         // Segment-specific avg HR
	SegmentAvgSpeed    *float64 `json:"segment_avg_speed,omitempty"`      // Segment-specific avg speed
	SegmentDistance    *float64 `json:"segment_distance,omitempty"`       // Segment-specific distance
	SegmentElevation   *float64 `json:"segment_elevation_gain,omitempty"` // Segment-specific elevation gain
}

// GetActivitiesForSegment retrieves activities matching a segment, using cache when available
// It also loads segment-specific metrics for sorting
func GetActivitiesForSegment(ctx context.Context, conn *pgx.Conn, athleteID, segmentID int64, toleranceMeters float64, sortBy string, forceRefresh bool) ([]ActivityWithMatch, error) {
	// Check cache first (unless force refresh)
	if !forceRefresh {
		cached, err := getCachedSegmentMatches(ctx, conn, segmentID, toleranceMeters)
		if err == nil && len(cached) > 0 {
			// Check if cache is recent (within last hour)
			var latestCacheTime time.Time
			if err := conn.QueryRow(ctx, `
				SELECT MAX(cached_at::text::timestamptz)
				FROM segment_activity_matches
				WHERE segment_id = $1 AND tolerance_meters = $2
			`, segmentID, toleranceMeters).Scan(&latestCacheTime); err == nil {
				if time.Since(latestCacheTime) < time.Hour {
					// Use cached results (with tolerance for loading segment metrics)
					return getActivitiesWithMatchesWithTolerance(ctx, conn, athleteID, cached, sortBy, segmentID, toleranceMeters)
				}
			}
		}
	}

	// Cache miss or stale - run spatial query and cache results
	matches, err := FindRoutePartsMatchingSegment(ctx, conn, segmentID, toleranceMeters)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching activities: %w", err)
	}

	// Cache the results
	if err := CacheSegmentActivityMatches(ctx, conn, segmentID, toleranceMeters, matches); err != nil {
		// Log but don't fail - cache is optional
		log.Printf("⚠️ Failed to cache segment matches: %v", err)
	}

	// Convert to ActivityWithMatch (with tolerance for loading segment metrics)
	return getActivitiesWithMatchesWithTolerance(ctx, conn, athleteID, matches, sortBy, segmentID, toleranceMeters)
}

// getCachedSegmentMatches retrieves cached matches from the database
func getCachedSegmentMatches(ctx context.Context, conn *pgx.Conn, segmentID int64, toleranceMeters float64) ([]SegmentMatchResult, error) {
	query := `
	SELECT activity_id, segment_id, min_distance_m, overlap_length_m, overlap_percentage
	FROM segment_activity_matches
	WHERE segment_id = $1 AND tolerance_meters = $2
	ORDER BY min_distance_m, overlap_percentage DESC
	`

	rows, err := conn.Query(ctx, query, segmentID, toleranceMeters)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SegmentMatchResult
	for rows.Next() {
		var result SegmentMatchResult
		err := rows.Scan(
			&result.ActivityID, &result.SegmentID,
			&result.MinDistanceM, &result.OverlapLengthM, &result.OverlapPercentage,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// getActivitiesWithMatchesWithTolerance retrieves activity summaries and combines with match metadata and segment metrics
func getActivitiesWithMatchesWithTolerance(ctx context.Context, conn *pgx.Conn, athleteID int64, matches []SegmentMatchResult, sortBy string, segmentID int64, toleranceMeters float64) ([]ActivityWithMatch, error) {
	if len(matches) == 0 {
		return []ActivityWithMatch{}, nil
	}

	// Extract activity IDs
	activityIDs := make([]int64, len(matches))
	matchMap := make(map[int64]SegmentMatchResult)
	for i, match := range matches {
		activityIDs[i] = match.ActivityID
		matchMap[match.ActivityID] = match
	}

	// Get activity summaries
	activities, err := GetActivitiesByIDs(ctx, conn, athleteID, activityIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get activities: %w", err)
	}

	// Load segment-specific metrics from cache
	cachedMetricsMap := make(map[int64]*SegmentActivityCacheEntry)
	for _, activityID := range activityIDs {
		cached, err := GetCachedSegmentActivityMetrics(ctx, conn, segmentID, activityID, toleranceMeters)
		if err == nil && cached != nil {
			cachedMetricsMap[activityID] = cached
		}
	}

	// Combine with match metadata and segment metrics
	result := make([]ActivityWithMatch, 0, len(activities))
	for _, activity := range activities {
		match, ok := matchMap[activity.ID]
		if !ok {
			continue // Skip if match not found (shouldn't happen)
		}

		awm := ActivityWithMatch{
			ActivitySummary:    activity,
			MinDistanceM:       match.MinDistanceM,
			OverlapLengthM:     match.OverlapLengthM,
			OverlapPercentage:  match.OverlapPercentage,
			StartDateFormatted: activity.StartDateTime.Format(time.RFC3339),
		}

		// Add segment-specific metrics if available
		if cached, ok := cachedMetricsMap[activity.ID]; ok {
			awm.SegmentAvgHR = cached.AvgHR
			awm.SegmentAvgSpeed = cached.AvgSpeed
			awm.SegmentDistance = cached.DistanceM
			awm.SegmentElevation = cached.ElevationGainM
		}

		result = append(result, awm)
	}

	// Apply sorting
	sortActivitiesWithMatches(result, sortBy)

	return result, nil
}

// sortActivitiesWithMatches sorts activities by the specified criteria
// Uses segment-specific metrics when available, falls back to whole activity metrics
func sortActivitiesWithMatches(activities []ActivityWithMatch, sortBy string) {
	switch sortBy {
	case "avg_hr":
		sort.Slice(activities, func(i, j int) bool {
			// Prefer segment-specific HR, fall back to whole activity HR
			hrI := 0.0
			if activities[i].SegmentAvgHR != nil {
				hrI = *activities[i].SegmentAvgHR
			} else if activities[i].AverageHeartrate > 0 {
				hrI = activities[i].AverageHeartrate
			}
			hrJ := 0.0
			if activities[j].SegmentAvgHR != nil {
				hrJ = *activities[j].SegmentAvgHR
			} else if activities[j].AverageHeartrate > 0 {
				hrJ = activities[j].AverageHeartrate
			}
			return hrI > hrJ // Descending
		})
	case "avg_speed":
		sort.Slice(activities, func(i, j int) bool {
			// Prefer segment-specific speed, fall back to whole activity speed
			speedI := 0.0
			if activities[i].SegmentAvgSpeed != nil {
				speedI = *activities[i].SegmentAvgSpeed
			} else if activities[i].AverageSpeed > 0 {
				speedI = activities[i].AverageSpeed
			}
			speedJ := 0.0
			if activities[j].SegmentAvgSpeed != nil {
				speedJ = *activities[j].SegmentAvgSpeed
			} else if activities[j].AverageSpeed > 0 {
				speedJ = activities[j].AverageSpeed
			}
			return speedI > speedJ // Descending
		})
	case "total_time":
		sort.Slice(activities, func(i, j int) bool {
			return activities[i].ElapsedTime > activities[j].ElapsedTime // Descending
		})
	case "date":
		sort.Slice(activities, func(i, j int) bool {
			return activities[i].StartDateTime.After(activities[j].StartDateTime) // Descending (newest first)
		})
	default:
		// Default: sort by min_distance_m (best match first)
		sort.Slice(activities, func(i, j int) bool {
			return activities[i].MinDistanceM < activities[j].MinDistanceM
		})
	}
}

// GetActivitiesByIDs retrieves activities by their IDs
func GetActivitiesByIDs(ctx context.Context, conn *pgx.Conn, athleteID int64, activityIDs []int64) ([]strava.ActivitySummary, error) {
	if len(activityIDs) == 0 {
		return []strava.ActivitySummary{}, nil
	}

	query := `
	SELECT id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		   type, sport_type, workout_type, start_date, utc_offset,
		   start_lat, start_lng, end_lat, end_lng,
		   location_city, location_state, location_country, gear_id,
		   average_speed, max_speed, average_cadence, average_watts,
		   kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	FROM activity_summaries
	WHERE athlete_id = $1 AND id = ANY($2)
	`

	rows, err := conn.Query(ctx, query, athleteID, activityIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query activities: %w", err)
	}
	defer rows.Close()

	var activities []strava.ActivitySummary
	for rows.Next() {
		var activity strava.ActivitySummary
		var startLat, startLng, endLat, endLng *float64
		var locationCity, locationState *string
		var workoutType *int

		err := rows.Scan(
			&activity.ID, &activity.AthleteID, &activity.Name, &activity.Distance, &activity.MovingTime, &activity.ElapsedTime,
			&activity.TotalElevationGain, &activity.Type, &activity.SportType, &workoutType,
			&activity.StartDateTime, &activity.UtcOffset, &startLat, &startLng, &endLat, &endLng,
			&locationCity, &locationState, &activity.LocationCountry, &activity.GearID,
			&activity.AverageSpeed, &activity.MaxSpeed, &activity.AverageCadence, &activity.AverageWatts,
			&activity.Kilojoules, &activity.AverageHeartrate, &activity.MaxHeartrate, &activity.MaxWatts,
			&activity.SufferScore,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}

		activity.WorkoutType = workoutType
		if startLat != nil && startLng != nil {
			activity.StartLatLng = &[]float64{*startLat, *startLng}
		}
		if endLat != nil && endLng != nil {
			activity.EndLatLng = &[]float64{*endLat, *endLng}
		}
		activity.LocationCity = locationCity
		activity.LocationState = locationState

		activities = append(activities, activity)
	}

	return activities, rows.Err()
}
