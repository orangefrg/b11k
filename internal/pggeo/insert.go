package pggeo

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"

	"b11k/internal/strava"

	"github.com/jackc/pgx/v5"
)

// haversineDistance calculates the distance between two points using the Haversine formula
// Returns distance in meters
func haversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000 // Earth radius in meters
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// InsertActivitySummary inserts an activity summary into the database
// Returns an error if the activity already exists
func InsertActivitySummary(ctx context.Context, conn *pgx.Conn, activity *strava.ActivitySummary) error {
	// Check if activity already exists
	exists, err := ActivityExists(ctx, conn, activity.ID)
	if err != nil {
		return fmt.Errorf("failed to check if activity exists: %w", err)
	}
	if exists {
		return fmt.Errorf("activity with ID %d already exists", activity.ID)
	}
	query := `
	INSERT INTO activity_summaries (
		id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		type, sport_type, workout_type, start_date, utc_offset,
		start_lat, start_lng, end_lat, end_lng,
		location_city, location_state, location_country, gear_id,
		average_speed, max_speed, average_cadence, average_watts,
		kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29
	)`

	var startLat, startLng, endLat, endLng *float64
	if activity.StartLatLng != nil && len(*activity.StartLatLng) >= 2 {
		startLat = &(*activity.StartLatLng)[0]
		startLng = &(*activity.StartLatLng)[1]
	}
	if activity.EndLatLng != nil && len(*activity.EndLatLng) >= 2 {
		endLat = &(*activity.EndLatLng)[0]
		endLng = &(*activity.EndLatLng)[1]
	}

	_, err = conn.Exec(ctx, query,
		activity.ID, activity.AthleteID, activity.Name, activity.Distance, activity.MovingTime, activity.ElapsedTime,
		activity.TotalElevationGain, activity.Type, activity.SportType, activity.WorkoutType,
		activity.StartDateTime, activity.UtcOffset, startLat, startLng, endLat, endLng,
		activity.LocationCity, activity.LocationState, activity.LocationCountry, activity.GearID,
		activity.AverageSpeed, activity.MaxSpeed, activity.AverageCadence, activity.AverageWatts,
		activity.Kilojoules, activity.AverageHeartrate, activity.MaxHeartrate, activity.MaxWatts,
		activity.SufferScore,
	)

	return err
}

// InsertActivityGeometry inserts activity geometry data using the new schema
// Returns an error if the activity doesn't exist in activity_summaries
func InsertActivityGeometry(ctx context.Context, conn *pgx.Conn, athleteID, activityID int64, latLngData [][]float64) error {
	// Check if activity exists in summaries table
	exists, err := ActivityExists(ctx, conn, activityID)
	if err != nil {
		return fmt.Errorf("failed to check if activity exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("activity with ID %d does not exist in activity_summaries", activityID)
	}
	if len(latLngData) < 2 {
		return fmt.Errorf("need at least 2 points to create a linestring")
	}

	// Extract longitude and latitude arrays for the helper function
	lons := make([]float64, len(latLngData))
	lats := make([]float64, len(latLngData))

	for i, point := range latLngData {
		lons[i] = point[1] // longitude
		lats[i] = point[0] // latitude
	}

	// Try to use the helper function first, fallback to direct PostGIS if not available
	query := `
	INSERT INTO activity_geometries (activity_id, athlete_id, route_geog)
	VALUES ($1, $2, make_route_geog_from_lonlat($3, $4))
	`

	_, err = conn.Exec(ctx, query, activityID, athleteID, lons, lats)
	if err != nil {
		// If helper function doesn't exist, try direct PostGIS approach
		log.Printf("⚠️ Helper function failed, trying direct PostGIS approach: %v", err)

		// Create a simple linestring from the coordinates
		points := make([]string, len(latLngData))
		for i, coord := range latLngData {
			if len(coord) >= 2 {
				points[i] = fmt.Sprintf("%.8f %.8f", coord[0], coord[1]) // lng lat
			}
		}

		linestringWKT := fmt.Sprintf("LINESTRING(%s)", strings.Join(points, ","))
		fallbackQuery := `
		INSERT INTO activity_geometries (activity_id, athlete_id, route_geog)
		VALUES ($1, $2, ST_GeogFromText($3))
		`

		_, err = conn.Exec(ctx, fallbackQuery, activityID, athleteID, linestringWKT)
		if err != nil {
			return fmt.Errorf("both helper function and direct PostGIS approach failed: %w", err)
		}
	}

	// Refresh the simplified route with default tolerance (if helper function exists)
	refreshQuery := `SELECT refresh_activity_simplified($1)`
	_, err = conn.Exec(ctx, refreshQuery, activityID)
	if err != nil {
		// If helper function doesn't exist, skip the refresh (not critical)
		log.Printf("⚠️ Warning: Could not refresh simplified geometry for activity %d: %v", activityID, err)
	}
	return nil
}

// InsertPointSamples inserts point samples for an activity
// Returns an error if the activity doesn't exist in activity_summaries
func InsertPointSamples(ctx context.Context, conn *pgx.Conn, activity *strava.BikeActivity) error {
	// Check if activity exists in summaries table
	exists, err := ActivityExists(ctx, conn, activity.Summary.ID)
	if err != nil {
		return fmt.Errorf("failed to check if activity exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("activity with ID %d does not exist in activity_summaries", activity.Summary.ID)
	}
	if len(activity.TimeStream.Data) == 0 {
		return fmt.Errorf("no time stream data available")
	}

	// Start a transaction for batch insert
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Prepare the insert statement
	query := `
	INSERT INTO point_samples (
		activity_id, athlete_id, point_index, time, location, altitude, heartrate,
		speed, watts, cadence, grade, moving, cumulative_distance
	) VALUES ($1, $2, $3, $4, ST_GeogFromText($5), $6, $7, $8, $9, $10, $11, $12, $13)
	`

	stmt, err := tx.Prepare(ctx, "insert_point_samples", query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	// Calculate cumulative distance as we iterate
	var cumulativeDistance float64
	var prevLat, prevLng float64
	hasPrevPoint := false

	for i := 0; i < len(activity.TimeStream.Data); i++ {
		var locationWKT string
		var altitude *float64
		var heartrate *int
		var speed *float64
		var watts *int
		var cadence *int
		var grade *float64
		var moving *bool

		if i < len(activity.LatLngStream.Data) && len(activity.LatLngStream.Data[i]) >= 2 {
			lat := activity.LatLngStream.Data[i][0]
			lng := activity.LatLngStream.Data[i][1]
			locationWKT = fmt.Sprintf("POINT(%.8f %.8f)", lng, lat) // lng, lat for PostGIS
			
			// Calculate cumulative distance
			if hasPrevPoint {
				cumulativeDistance += haversineDistance(prevLat, prevLng, lat, lng)
			}
			prevLat = lat
			prevLng = lng
			hasPrevPoint = true
		} else {
			continue // Skip points without location data
		}

		if i < len(activity.AltitudeStream.Data) {
			altitude = &activity.AltitudeStream.Data[i]
		}
		if i < len(activity.HeartrateStream.Data) {
			heartrate = &activity.HeartrateStream.Data[i]
		}
		if i < len(activity.SpeedStream.Data) {
			speed = &activity.SpeedStream.Data[i]
		}
		if i < len(activity.WattsStream.Data) {
			watts = &activity.WattsStream.Data[i]
		}
		if i < len(activity.CadenceStream.Data) {
			cadence = &activity.CadenceStream.Data[i]
		}
		if i < len(activity.GradeStream.Data) {
			grade = &activity.GradeStream.Data[i]
		}
		if i < len(activity.MovingStream.Data) {
			moving = &activity.MovingStream.Data[i]
		}

		_, err := tx.Exec(ctx, stmt.SQL,
			activity.Summary.ID, activity.Summary.AthleteID, i, activity.TimeStream.Data[i], locationWKT,
			altitude, heartrate, speed, watts, cadence, grade, moving, cumulativeDistance,
		)
		if err != nil {
			return fmt.Errorf("failed to insert point sample %d: %w", i, err)
		}
	}

	return tx.Commit(ctx)
}

// InsertBikeActivity inserts a complete bike activity (summary, geometry, and points)
// Returns an error if the activity already exists
func InsertBikeActivity(ctx context.Context, conn *pgx.Conn, activity *strava.BikeActivity) error {
	// Insert activity summary
	if err := InsertActivitySummary(ctx, conn, &activity.Summary); err != nil {
		return fmt.Errorf("failed to insert activity summary: %w", err)
	}

	// Insert activity geometry if we have lat/lng data
	if len(activity.LatLngStream.Data) > 0 {
		if err := InsertActivityGeometry(ctx, conn, activity.Summary.AthleteID, activity.Summary.ID, activity.LatLngStream.Data); err != nil {
			return fmt.Errorf("failed to insert activity geometry: %w", err)
		}
	}

	// Insert point samples
	if err := InsertPointSamples(ctx, conn, activity); err != nil {
		return fmt.Errorf("failed to insert point samples: %w", err)
	}

	return nil
}

// InsertActivitySummaryUpsert inserts or updates an activity summary (allows overwriting existing data)
func InsertActivitySummaryUpsert(ctx context.Context, conn *pgx.Conn, activity *strava.ActivitySummary) error {
	query := `
	INSERT INTO activity_summaries (
		id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		type, sport_type, workout_type, start_date, utc_offset,
		start_lat, start_lng, end_lat, end_lng,
		location_city, location_state, location_country, gear_id,
		average_speed, max_speed, average_cadence, average_watts,
		kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29
	) ON CONFLICT (id) DO UPDATE SET
		athlete_id = EXCLUDED.athlete_id,
		name = EXCLUDED.name,
		distance = EXCLUDED.distance,
		moving_time = EXCLUDED.moving_time,
		elapsed_time = EXCLUDED.elapsed_time,
		total_elevation_gain = EXCLUDED.total_elevation_gain,
		type = EXCLUDED.type,
		sport_type = EXCLUDED.sport_type,
		workout_type = EXCLUDED.workout_type,
		start_date = EXCLUDED.start_date,
		utc_offset = EXCLUDED.utc_offset,
		start_lat = EXCLUDED.start_lat,
		start_lng = EXCLUDED.start_lng,
		end_lat = EXCLUDED.end_lat,
		end_lng = EXCLUDED.end_lng,
		location_city = EXCLUDED.location_city,
		location_state = EXCLUDED.location_state,
		location_country = EXCLUDED.location_country,
		gear_id = EXCLUDED.gear_id,
		average_speed = EXCLUDED.average_speed,
		max_speed = EXCLUDED.max_speed,
		average_cadence = EXCLUDED.average_cadence,
		average_watts = EXCLUDED.average_watts,
		kilojoules = EXCLUDED.kilojoules,
		average_heartrate = EXCLUDED.average_heartrate,
		max_heartrate = EXCLUDED.max_heartrate,
		max_watts = EXCLUDED.max_watts,
		suffer_score = EXCLUDED.suffer_score,
		updated_at = NOW()
	`

	var startLat, startLng, endLat, endLng *float64
	if activity.StartLatLng != nil && len(*activity.StartLatLng) >= 2 {
		startLat = &(*activity.StartLatLng)[0]
		startLng = &(*activity.StartLatLng)[1]
	}
	if activity.EndLatLng != nil && len(*activity.EndLatLng) >= 2 {
		endLat = &(*activity.EndLatLng)[0]
		endLng = &(*activity.EndLatLng)[1]
	}

	_, err := conn.Exec(ctx, query,
		activity.ID, activity.AthleteID, activity.Name, activity.Distance, activity.MovingTime, activity.ElapsedTime,
		activity.TotalElevationGain, activity.Type, activity.SportType, activity.WorkoutType,
		activity.StartDateTime, activity.UtcOffset, startLat, startLng, endLat, endLng,
		activity.LocationCity, activity.LocationState, activity.LocationCountry, activity.GearID,
		activity.AverageSpeed, activity.MaxSpeed, activity.AverageCadence, activity.AverageWatts,
		activity.Kilojoules, activity.AverageHeartrate, activity.MaxHeartrate, activity.MaxWatts,
		activity.SufferScore,
	)

	return err
}

// InsertBikeActivityUpsert inserts or updates a complete bike activity (allows overwriting existing data)
func InsertBikeActivityUpsert(ctx context.Context, conn *pgx.Conn, activity *strava.BikeActivity) error {
	// Insert/update activity summary
	if err := InsertActivitySummaryUpsert(ctx, conn, &activity.Summary); err != nil {
		return fmt.Errorf("failed to upsert activity summary: %w", err)
	}

	// Insert/update activity geometry if we have lat/lng data
	if len(activity.LatLngStream.Data) > 0 {
		if err := InsertActivityGeometryUpsert(ctx, conn, activity.Summary.AthleteID, activity.Summary.ID, activity.LatLngStream.Data); err != nil {
			return fmt.Errorf("failed to upsert activity geometry: %w", err)
		}
	}

	// Delete existing point samples and insert new ones
	if err := ReplacePointSamples(ctx, conn, activity); err != nil {
		return fmt.Errorf("failed to replace point samples: %w", err)
	}

	return nil
}

// InsertActivityGeometryUpsert inserts or updates activity geometry data
func InsertActivityGeometryUpsert(ctx context.Context, conn *pgx.Conn, athleteID, activityID int64, latLngData [][]float64) error {
	if len(latLngData) < 2 {
		return fmt.Errorf("need at least 2 points to create a linestring")
	}

	// Extract longitude and latitude arrays for the helper function
	lons := make([]float64, len(latLngData))
	lats := make([]float64, len(latLngData))

	for i, point := range latLngData {
		lons[i] = point[1] // longitude
		lats[i] = point[0] // latitude
	}

	query := `
	INSERT INTO activity_geometries (activity_id, athlete_id, route_geog)
	VALUES ($1, $2, make_route_geog_from_lonlat($3, $4))
	ON CONFLICT (activity_id) DO UPDATE SET
		athlete_id = EXCLUDED.athlete_id,
		route_geog = EXCLUDED.route_geog,
		updated_at = NOW()
	`

	_, err := conn.Exec(ctx, query, activityID, athleteID, lons, lats)
	if err != nil {
		// If helper function doesn't exist, try direct PostGIS approach
		log.Printf("⚠️ Helper function failed, trying direct PostGIS approach: %v", err)

		// Create a simple linestring from the coordinates
		points := make([]string, len(latLngData))
		for i, coord := range latLngData {
			if len(coord) >= 2 {
				points[i] = fmt.Sprintf("%.8f %.8f", coord[0], coord[1]) // lng lat
			}
		}

		linestringWKT := fmt.Sprintf("LINESTRING(%s)", strings.Join(points, ","))
		fallbackQuery := `
		INSERT INTO activity_geometries (activity_id, athlete_id, route_geog)
		VALUES ($1, $2, ST_GeogFromText($3))
		ON CONFLICT (activity_id) DO UPDATE SET
			athlete_id = EXCLUDED.athlete_id,
			route_geog = EXCLUDED.route_geog,
			updated_at = NOW()
		`

		_, err = conn.Exec(ctx, fallbackQuery, activityID, athleteID, linestringWKT)
		if err != nil {
			return fmt.Errorf("both helper function and direct PostGIS approach failed: %w", err)
		}
	}

	// Refresh the simplified route with default tolerance (if helper function exists)
	refreshQuery := `SELECT refresh_activity_simplified($1)`
	_, err = conn.Exec(ctx, refreshQuery, activityID)
	if err != nil {
		// If helper function doesn't exist, skip the refresh (not critical)
		log.Printf("⚠️ Warning: Could not refresh simplified geometry for activity %d: %v", activityID, err)
	}
	return nil
}

// ReplacePointSamples deletes existing point samples and inserts new ones
func ReplacePointSamples(ctx context.Context, conn *pgx.Conn, activity *strava.BikeActivity) error {
	if len(activity.TimeStream.Data) == 0 {
		return fmt.Errorf("no time stream data available")
	}

	// Start a transaction for batch operations
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing point samples
	deleteQuery := `DELETE FROM point_samples WHERE activity_id = $1`
	_, err = tx.Exec(ctx, deleteQuery, activity.Summary.ID)
	if err != nil {
		return fmt.Errorf("failed to delete existing point samples: %w", err)
	}

	// Prepare the insert statement
	insertQuery := `
	INSERT INTO point_samples (
		activity_id, athlete_id, point_index, time, location, altitude, heartrate,
		speed, watts, cadence, grade, moving, cumulative_distance
	) VALUES ($1, $2, $3, $4, ST_GeogFromText($5), $6, $7, $8, $9, $10, $11, $12, $13)
	`

	stmt, err := tx.Prepare(ctx, "replace_point_samples", insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	// Calculate cumulative distance as we iterate
	var cumulativeDistance float64
	var prevLat, prevLng float64
	hasPrevPoint := false

	// Insert each point
	for i := 0; i < len(activity.TimeStream.Data); i++ {
		var locationWKT string
		var altitude *float64
		var heartrate *int
		var speed *float64
		var watts *int
		var cadence *int
		var grade *float64
		var moving *bool

		// Get location data
		if i < len(activity.LatLngStream.Data) && len(activity.LatLngStream.Data[i]) >= 2 {
			lat := activity.LatLngStream.Data[i][0]
			lng := activity.LatLngStream.Data[i][1]
			locationWKT = fmt.Sprintf("POINT(%.8f %.8f)", lng, lat) // lng, lat for PostGIS
			
			// Calculate cumulative distance
			if hasPrevPoint {
				cumulativeDistance += haversineDistance(prevLat, prevLng, lat, lng)
			}
			prevLat = lat
			prevLng = lng
			hasPrevPoint = true
		} else {
			continue // Skip points without location data
		}

		// Get optional sensor data
		if i < len(activity.AltitudeStream.Data) {
			altitude = &activity.AltitudeStream.Data[i]
		}
		if i < len(activity.HeartrateStream.Data) {
			heartrate = &activity.HeartrateStream.Data[i]
		}
		if i < len(activity.SpeedStream.Data) {
			speed = &activity.SpeedStream.Data[i]
		}
		if i < len(activity.WattsStream.Data) {
			watts = &activity.WattsStream.Data[i]
		}
		if i < len(activity.CadenceStream.Data) {
			cadence = &activity.CadenceStream.Data[i]
		}
		if i < len(activity.GradeStream.Data) {
			grade = &activity.GradeStream.Data[i]
		}
		if i < len(activity.MovingStream.Data) {
			moving = &activity.MovingStream.Data[i]
		}

		_, err := tx.Exec(ctx, stmt.SQL,
			activity.Summary.ID, activity.Summary.AthleteID, i, activity.TimeStream.Data[i], locationWKT,
			altitude, heartrate, speed, watts, cadence, grade, moving, cumulativeDistance,
		)
		if err != nil {
			return fmt.Errorf("failed to insert point sample %d: %w", i, err)
		}
	}

	return tx.Commit(ctx)
}

// InsertBikeActivityWithLogging inserts a complete bike activity with logging
func InsertBikeActivityWithLogging(ctx context.Context, conn *pgx.Conn, activity *strava.BikeActivity) error {
	log.Printf("🚴 Starting to save complete bike activity %d (%s)", activity.Summary.ID, activity.Summary.Name)

	err := InsertBikeActivityUpsert(ctx, conn, activity)
	if err != nil {
		log.Printf("❌ Error saving activity %d: %v", activity.Summary.ID, err)
		return fmt.Errorf("failed to save bike activity: %w", err)
	}

	log.Printf("✅ Successfully saved complete bike activity %d", activity.Summary.ID)
	return nil
}
