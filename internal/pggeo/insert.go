package pggeo

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"

	"b11k/internal/strava"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		location_city, location_state, location_country, gear_id, gear_name,
		average_speed, max_speed, average_cadence, average_watts,
		kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30
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
		activity.GearName, activity.AverageSpeed, activity.MaxSpeed, activity.AverageCadence, activity.AverageWatts,
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
		speed, watts, cadence, grade, moving, temperature, cumulative_distance
	) VALUES ($1, $2, $3, $4, ST_GeogFromText($5), $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
		var temperature *int

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
		if i < len(activity.TemperatureStream.Data) {
			temperature = &activity.TemperatureStream.Data[i]
		}
		sampleCumulativeDistance := cumulativeDistance
		if i < len(activity.DistanceStream.Data) {
			sampleCumulativeDistance = activity.DistanceStream.Data[i]
		}

		_, err := tx.Exec(ctx, stmt.SQL,
			activity.Summary.ID, activity.Summary.AthleteID, i, activity.TimeStream.Data[i], locationWKT,
			altitude, heartrate, speed, watts, cadence, grade, moving, temperature, sampleCumulativeDistance,
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
type summaryWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func InsertActivitySummaryUpsert(ctx context.Context, conn summaryWriter, activity *strava.ActivitySummary) error {
	query := `
	INSERT INTO activity_summaries (
		id, athlete_id, name, distance, moving_time, elapsed_time, total_elevation_gain,
		type, sport_type, workout_type, start_date, utc_offset,
		start_lat, start_lng, end_lat, end_lng,
		location_city, location_state, location_country, gear_id, gear_name,
		average_speed, max_speed, average_cadence, average_watts,
		kilojoules, average_heartrate, max_heartrate, max_watts, suffer_score
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30
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
		gear_name = EXCLUDED.gear_name,
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
 WHERE activity_summaries.athlete_id = EXCLUDED.athlete_id
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

	tag, err := conn.Exec(ctx, query,
		activity.ID, activity.AthleteID, activity.Name, activity.Distance, activity.MovingTime, activity.ElapsedTime,
		activity.TotalElevationGain, activity.Type, activity.SportType, activity.WorkoutType,
		activity.StartDateTime, activity.UtcOffset, startLat, startLng, endLat, endLng,
		activity.LocationCity, activity.LocationState, activity.LocationCountry, activity.GearID,
		activity.GearName, activity.AverageSpeed, activity.MaxSpeed, activity.AverageCadence, activity.AverageWatts,
		activity.Kilojoules, activity.AverageHeartrate, activity.MaxHeartrate, activity.MaxWatts,
		activity.SufferScore,
	)

	if err == nil && tag.RowsAffected() != 1 {
		return fmt.Errorf("activity belongs to another athlete")
	}
	return err
}

// InsertBikeActivityUpsert inserts or updates a complete bike activity (allows overwriting existing data)
func InsertBikeActivityUpsert(ctx context.Context, conn *pgx.Conn, activity *strava.BikeActivity) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = SaveActivity(ctx, tx, activity); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaveActivity writes all imported parts and completion in the caller's transaction.
func SaveActivity(ctx context.Context, tx pgx.Tx, activity *strava.BikeActivity) error {
	if activity.Summary.ID <= 0 || activity.Summary.AthleteID <= 0 || activity.Summary.StartDateTime.IsZero() {
		return fmt.Errorf("activity summary is incomplete")
	}
	for _, p := range activity.LatLngStream.Data {
		if len(p) != 2 || math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.Abs(p[0]) > 90 || math.Abs(p[1]) > 180 {
			return fmt.Errorf("invalid route coordinate")
		}
	}
	if len(activity.LatLngStream.Data) > 0 && len(activity.TimeStream.Data) != len(activity.LatLngStream.Data) {
		return fmt.Errorf("route and time stream lengths differ")
	}
	if err := InsertActivitySummaryUpsert(ctx, tx, &activity.Summary); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM activity_geometries WHERE activity_id=$1", activity.Summary.ID); err != nil {
		return err
	}
	if len(activity.LatLngStream.Data) >= 2 {
		points := make([]string, len(activity.LatLngStream.Data))
		for i, p := range activity.LatLngStream.Data {
			points[i] = fmt.Sprintf("%.8f %.8f", p[1], p[0])
		}
		_, err := tx.Exec(ctx, `INSERT INTO activity_geometries(activity_id,athlete_id,route_geog,route_geog_simplified)
  VALUES($1,$2,ST_GeogFromText($3),ST_GeogFromText($3))`, activity.Summary.ID, activity.Summary.AthleteID, "LINESTRING("+strings.Join(points, ",")+")")
		if err != nil {
			return err
		}
	}
	if len(activity.TimeStream.Data) == 0 {
		if _, err := tx.Exec(ctx, "DELETE FROM point_samples WHERE activity_id=$1", activity.Summary.ID); err != nil {
			return err
		}
	} else if err := ReplacePointSamples(ctx, tx, activity); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "UPDATE activity_summaries SET sync_version=1 WHERE id=$1 AND athlete_id=$2", activity.Summary.ID, activity.Summary.AthleteID)
	return err
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
type transactionStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

func ReplacePointSamples(ctx context.Context, conn transactionStarter, activity *strava.BikeActivity) error {
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
		speed, watts, cadence, grade, moving, temperature, cumulative_distance
	) VALUES ($1, $2, $3, $4, ST_GeogFromText($5), $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
		var locationWKT *string
		var altitude *float64
		var heartrate *int
		var speed *float64
		var watts *int
		var cadence *int
		var grade *float64
		var moving *bool
		var temperature *int

		// Get location data
		if i < len(activity.LatLngStream.Data) && len(activity.LatLngStream.Data[i]) >= 2 {
			lat := activity.LatLngStream.Data[i][0]
			lng := activity.LatLngStream.Data[i][1]
			point := fmt.Sprintf("POINT(%.8f %.8f)", lng, lat)
			locationWKT = &point // lng, lat for PostGIS

			// Calculate cumulative distance
			if hasPrevPoint {
				cumulativeDistance += haversineDistance(prevLat, prevLng, lat, lng)
			}
			prevLat = lat
			prevLng = lng
			hasPrevPoint = true
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
		if i < len(activity.TemperatureStream.Data) {
			temperature = &activity.TemperatureStream.Data[i]
		}
		sampleCumulativeDistance := cumulativeDistance
		if i < len(activity.DistanceStream.Data) {
			sampleCumulativeDistance = activity.DistanceStream.Data[i]
		}

		_, err := tx.Exec(ctx, stmt.SQL,
			activity.Summary.ID, activity.Summary.AthleteID, i, activity.TimeStream.Data[i], locationWKT,
			altitude, heartrate, speed, watts, cadence, grade, moving, temperature, sampleCumulativeDistance,
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
