package pggeo

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
)

func CreateTables(ctx context.Context, conn *pgx.Conn) error {

	if err := createActivitySummariesTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to create activity summaries table: %w", err)
	}

	if err := createActivityGeometriesTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to create activity geometries table: %w", err)
	}

	if err := createPointSamplesTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to create point samples table: %w", err)
	}

	if err := createFavoriteSegmentsTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to create favorite segments table: %w", err)
	}

	if err := createSegmentActivityMatchesTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to create segment activity matches table: %w", err)
	}

	if err := createHelperFunctions(ctx, conn); err != nil {
		return fmt.Errorf("failed to create helper functions: %w", err)
	}

	return nil
}

func TruncateTables(ctx context.Context, conn *pgx.Conn) error {
	tables := []string{
		"point_samples",
		"activity_geometries",
		"activity_summaries",
		"favorite_segments",
	}

	for _, table := range tables {
		query := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
		if _, err := conn.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to truncate table %s: %w", table, err)
		}
	}

	return nil
}

func DropAndRecreateTables(ctx context.Context, conn *pgx.Conn) error {
	// Drop tables in reverse dependency order
	// Note: segment_activity_matches has foreign keys to both favorite_segments and activity_summaries
	// so it needs to be dropped before those, but CASCADE will handle it anyway
	tables := []string{
		"segment_activity_matches", // Cache table with foreign keys
		"point_samples",            // Depends on activity_summaries
		"activity_geometries",      // Depends on activity_summaries
		"favorite_segments",        // Independent but referenced by segment_activity_matches
		"activity_summaries",       // Base table
	}

	log.Printf("🗑️ Dropping %d tables...", len(tables))
	for _, table := range tables {
		query := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)
		if _, err := conn.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to drop table %s: %w", table, err)
		}
		log.Printf("   ✓ Dropped table: %s", table)
	}

	log.Printf("🔨 Recreating all tables...")
	// Recreate all tables
	if err := CreateTables(ctx, conn); err != nil {
		return err
	}

	log.Printf("✅ All tables dropped and recreated successfully")
	return nil
}

func createActivitySummariesTable(ctx context.Context, conn *pgx.Conn) error {
	query := `
	CREATE TABLE IF NOT EXISTS activity_summaries (
		id BIGINT PRIMARY KEY,
		athlete_id BIGINT NOT NULL,
		name TEXT NOT NULL,
		distance DOUBLE PRECISION NOT NULL,
		moving_time DOUBLE PRECISION NOT NULL,
		elapsed_time DOUBLE PRECISION NOT NULL,
		total_elevation_gain DOUBLE PRECISION NOT NULL,
		type TEXT NOT NULL,
		sport_type TEXT,
		workout_type INTEGER,
		start_date TIMESTAMPTZ NOT NULL,
		utc_offset DOUBLE PRECISION,
		start_lat DOUBLE PRECISION,
		start_lng DOUBLE PRECISION,
		end_lat DOUBLE PRECISION,
		end_lng DOUBLE PRECISION,
		location_city TEXT,
		location_state TEXT,
		location_country TEXT,
		gear_id TEXT,
		average_speed DOUBLE PRECISION,
		max_speed DOUBLE PRECISION,
		average_cadence DOUBLE PRECISION,
		average_watts DOUBLE PRECISION,
		kilojoules DOUBLE PRECISION,
		average_heartrate DOUBLE PRECISION,
		max_heartrate DOUBLE PRECISION,
		max_watts DOUBLE PRECISION,
		suffer_score DOUBLE PRECISION,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW()
	)`

	_, err := conn.Exec(ctx, query)
	if err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_activity_summaries_athlete_id ON activity_summaries (athlete_id)",
		"CREATE INDEX IF NOT EXISTS idx_activity_summaries_start_date ON activity_summaries (start_date)",
		"CREATE INDEX IF NOT EXISTS idx_activity_summaries_type ON activity_summaries (type)",
		"CREATE INDEX IF NOT EXISTS idx_activity_summaries_athlete_start_date ON activity_summaries (athlete_id, start_date)",
		"CREATE INDEX IF NOT EXISTS idx_activity_summaries_athlete_type ON activity_summaries (athlete_id, type)",
		"CREATE INDEX IF NOT EXISTS idx_activity_summaries_location_country ON activity_summaries (location_country)",
	}

	for _, indexQuery := range indexes {
		if _, err := conn.Exec(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

func createActivityGeometriesTable(ctx context.Context, conn *pgx.Conn) error {
	query := `
	CREATE TABLE IF NOT EXISTS activity_geometries (
		activity_id BIGINT PRIMARY KEY REFERENCES activity_summaries(id) ON DELETE CASCADE,
		athlete_id BIGINT NOT NULL,
		route_geog GEOGRAPHY(LINESTRING, 4326) NOT NULL,
		route_bbox_geom    GEOMETRY(POLYGON, 4326)
                     GENERATED ALWAYS AS (ST_Envelope(route_geog::GEOMETRY)) STORED,
		route_geog_simplified GEOGRAPHY(LINESTRING, 4326),
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW(),
	CONSTRAINT activities_route_has_two_points
		CHECK (ST_NPoints(route_geog::GEOMETRY) >= 2)
	)`

	_, err := conn.Exec(ctx, query)
	if err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_activity_geometries_athlete_id ON activity_geometries (athlete_id)",
		"CREATE INDEX IF NOT EXISTS idx_activity_geometries_route_geog ON activity_geometries USING GIST (route_geog)",
		"CREATE INDEX IF NOT EXISTS idx_activity_geometries_bbox ON activity_geometries USING GIST (route_bbox_geom)",
		"CREATE INDEX IF NOT EXISTS idx_activity_geometries_route_geog_simplified ON activity_geometries USING GIST (route_geog_simplified)",
		"CREATE INDEX IF NOT EXISTS idx_activity_geometries_activity_id ON activity_geometries (activity_id)",
	}

	for _, indexQuery := range indexes {
		if _, err := conn.Exec(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create spatial index: %w", err)
		}
	}

	return nil
}

func createPointSamplesTable(ctx context.Context, conn *pgx.Conn) error {
	query := `
	CREATE TABLE IF NOT EXISTS point_samples (
		id BIGSERIAL PRIMARY KEY,
		activity_id BIGINT NOT NULL REFERENCES activity_summaries(id) ON DELETE CASCADE,
		athlete_id BIGINT NOT NULL,
		point_index INTEGER NOT NULL,
		time TIMESTAMPTZ NOT NULL,
		location GEOGRAPHY(POINT, 4326) NOT NULL,
		altitude DOUBLE PRECISION,
		heartrate INTEGER,
		speed DOUBLE PRECISION,
		watts INTEGER,
		cadence INTEGER,
		grade DOUBLE PRECISION,
		moving BOOLEAN,
		cumulative_distance DOUBLE PRECISION,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`

	_, err := conn.Exec(ctx, query)
	if err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_point_samples_athlete_id ON point_samples (athlete_id)",
		"CREATE INDEX IF NOT EXISTS idx_point_samples_activity_id ON point_samples (activity_id)",
		"CREATE INDEX IF NOT EXISTS idx_point_samples_time ON point_samples (time)",
		"CREATE INDEX IF NOT EXISTS idx_point_samples_athlete_activity_time ON point_samples (athlete_id, activity_id, time)",
		"CREATE INDEX IF NOT EXISTS idx_point_samples_location ON point_samples USING GIST (location)",
		"CREATE INDEX IF NOT EXISTS idx_point_samples_activity_point_index ON point_samples (activity_id, point_index)",
		"CREATE INDEX IF NOT EXISTS idx_point_samples_time_range ON point_samples (time) WHERE time IS NOT NULL",
	}

	for _, indexQuery := range indexes {
		if _, err := conn.Exec(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create point samples index: %w", err)
		}
	}

	return nil
}

func createFavoriteSegmentsTable(ctx context.Context, conn *pgx.Conn) error {
	query := `
	CREATE TABLE IF NOT EXISTS favorite_segments (
		id BIGSERIAL PRIMARY KEY,
		athlete_id BIGINT NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		segment_geog GEOGRAPHY(LINESTRING, 4326) NOT NULL,
		segment_bbox_geom GEOMETRY(POLYGON, 4326)
			GENERATED ALWAYS AS (ST_Envelope(segment_geog::GEOMETRY)) STORED,
		segment_geog_simplified GEOGRAPHY(LINESTRING, 4326),
		elevation_gain_m DOUBLE PRECISION,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW(),
		CONSTRAINT segments_has_two_points
			CHECK (ST_NPoints(segment_geog::GEOMETRY) >= 2)
	)`

	_, err := conn.Exec(ctx, query)
	if err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_favorite_segments_athlete_id ON favorite_segments (athlete_id)",
		"CREATE INDEX IF NOT EXISTS idx_favorite_segments_segment_geog ON favorite_segments USING GIST (segment_geog)",
		"CREATE INDEX IF NOT EXISTS idx_favorite_segments_bbox ON favorite_segments USING GIST (segment_bbox_geom)",
		"CREATE INDEX IF NOT EXISTS idx_favorite_segments_segment_geog_simplified ON favorite_segments USING GIST (segment_geog_simplified)",
		"CREATE INDEX IF NOT EXISTS idx_favorite_segments_athlete_name ON favorite_segments (athlete_id, name)",
		"CREATE INDEX IF NOT EXISTS idx_favorite_segments_created_at ON favorite_segments (created_at)",
	}

	for _, indexQuery := range indexes {
		if _, err := conn.Exec(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create segment index: %w", err)
		}
	}

	return nil
}

func createSegmentActivityMatchesTable(ctx context.Context, conn *pgx.Conn) error {
	query := `
	CREATE TABLE IF NOT EXISTS segment_activity_matches (
		segment_id BIGINT NOT NULL REFERENCES favorite_segments(id) ON DELETE CASCADE,
		activity_id BIGINT NOT NULL REFERENCES activity_summaries(id) ON DELETE CASCADE,
		tolerance_meters DOUBLE PRECISION NOT NULL,
		min_distance_m DOUBLE PRECISION NOT NULL,
		overlap_length_m DOUBLE PRECISION NOT NULL,
		overlap_percentage DOUBLE PRECISION NOT NULL,
		start_index INTEGER,
		end_index INTEGER,
		avg_hr DOUBLE PRECISION,
		avg_speed DOUBLE PRECISION,
		distance_m DOUBLE PRECISION,
		elevation_gain_m DOUBLE PRECISION,
		cached_at TIMESTAMPTZ DEFAULT NOW(),
		PRIMARY KEY (segment_id, activity_id, tolerance_meters)
	)`

	_, err := conn.Exec(ctx, query)
	if err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_segment_activity_matches_segment_tolerance ON segment_activity_matches (segment_id, tolerance_meters)",
		"CREATE INDEX IF NOT EXISTS idx_segment_activity_matches_activity_id ON segment_activity_matches (activity_id)",
		"CREATE INDEX IF NOT EXISTS idx_segment_activity_matches_cached_at ON segment_activity_matches (cached_at)",
	}

	for _, indexQuery := range indexes {
		if _, err := conn.Exec(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create segment_activity_matches index: %w", err)
		}
	}

	return nil
}

func createHelperFunctions(ctx context.Context, conn *pgx.Conn) error {
	// First, check if PostGIS is available
	var postgisVersion string
	err := conn.QueryRow(ctx, "SELECT PostGIS_Version()").Scan(&postgisVersion)
	if err != nil {
		log.Printf("⚠️ PostGIS not available, skipping spatial helper functions: %v", err)
		return nil
	}
	log.Printf("✅ PostGIS version: %s", postgisVersion)

	helperQueries := []string{
		// Make route geography from longitude and latitude
		`CREATE OR REPLACE FUNCTION make_route_geog_from_lonlat(
			lon DOUBLE PRECISION[], 
			lat DOUBLE PRECISION[]
		) RETURNS GEOGRAPHY
		LANGUAGE SQL IMMUTABLE STRICT AS
		$$
		SELECT ST_MakeLine(
			ARRAY(
				SELECT ST_SetSRID(ST_MakePoint(lon[i], lat[i]), 4326)
				FROM generate_subscripts(lon,1) AS i
				ORDER BY i
			)
		)::GEOGRAPHY;
		$$;`,
		// Simplify route geography by meters
		`CREATE OR REPLACE FUNCTION simplify_route_geog_meters(
			g GEOGRAPHY, 
			meters DOUBLE PRECISION
		) RETURNS GEOGRAPHY
		LANGUAGE SQL IMMUTABLE STRICT AS
		$$
		SELECT ST_Transform(
			ST_SimplifyPreserveTopology(
				ST_Transform(g::GEOMETRY, 3857),
				GREATEST(meters, 0.0)
			),
			4326
		)::GEOGRAPHY;
		$$;`,
		// Refresh activity simplified
		`CREATE OR REPLACE FUNCTION refresh_activity_simplified(
			p_activity_id BIGINT,
			p_tolerance_meters DOUBLE PRECISION DEFAULT 8.0
		) RETURNS VOID
		LANGUAGE SQL AS
		$$
		UPDATE activity_geometries
		SET route_geog_simplified = simplify_route_geog_meters(route_geog, p_tolerance_meters)
		WHERE activity_id = p_activity_id;
		$$;`,

		// Refresh all simplified
		`CREATE OR REPLACE FUNCTION refresh_all_simplified(
			p_tolerance_meters DOUBLE PRECISION DEFAULT 8.0
		) RETURNS VOID
		LANGUAGE SQL AS
		$$
		UPDATE activity_geometries
		SET route_geog_simplified = simplify_route_geog_meters(route_geog, p_tolerance_meters);
		$$;`,
		// Find activities near
		`CREATE OR REPLACE FUNCTION find_activities_near(
			p_lon DOUBLE PRECISION,
			p_lat DOUBLE PRECISION,
			p_radius_meters DOUBLE PRECISION
			) RETURNS TABLE(activity_id BIGINT, min_dist_m DOUBLE PRECISION)
			LANGUAGE SQL STABLE AS
			$$
			WITH q AS (
				SELECT ST_SetSRID(ST_MakePoint(p_lon, p_lat), 4326)::GEOGRAPHY AS pt,
					p_radius_meters::DOUBLE PRECISION AS r
			)
			SELECT a.activity_id,
					ST_Distance(a.route_geog, q.pt) AS min_dist_m
			FROM activity_geometries a, q
			WHERE ST_DWithin(a.route_geog, q.pt, q.r)
			ORDER BY min_dist_m;
			$$;`,
		// Find activities intersecting line
		`CREATE OR REPLACE FUNCTION find_activities_intersecting_line(
			p_line GEOGRAPHY,              -- input route/segment, GEOGRAPHY(LINESTRING,4326)
			p_tolerance_meters DOUBLE PRECISION DEFAULT 15.0
			)
			RETURNS TABLE (
			activity_id BIGINT,
			min_distance_m DOUBLE PRECISION,
			overlap_length_m DOUBLE PRECISION
			)
			LANGUAGE SQL STABLE AS
			$$
			WITH q AS (
				SELECT p_line AS line, p_tolerance_meters AS tol
			)
			SELECT
				a.activity_id,
				-- Minimum distance between the activity route and the input line
				ST_Distance(a.route_geog, q.line) AS min_distance_m,
				-- Approximate overlapping segment length within tolerance
				ST_Length(
				ST_Intersection(
					ST_Buffer(a.route_geog::geometry, q.tol)::geography,
					q.line
				)
				) AS overlap_length_m
			FROM activity_geometries a, q
			WHERE ST_DWithin(a.route_geog, q.line, q.tol)
			ORDER BY min_distance_m;
			$$;`,

		// Refresh segment simplified
		`CREATE OR REPLACE FUNCTION refresh_segment_simplified(
			p_segment_id BIGINT,
			p_tolerance_meters DOUBLE PRECISION DEFAULT 8.0
			) RETURNS VOID
			LANGUAGE SQL AS
			$$
			UPDATE favorite_segments
				SET segment_geog_simplified = simplify_route_geog_meters(segment_geog, p_tolerance_meters)
			WHERE id = p_segment_id;
			$$;`,

		// Find route parts matching segment
		// Uses geometry-based matching: checks if segment geometry is within tolerance of activity route
		// This allows for deviations along the route and works regardless of point density
		`CREATE OR REPLACE FUNCTION find_route_parts_matching_segment(
			p_segment_id BIGINT,
			p_tolerance_meters DOUBLE PRECISION DEFAULT 15.0
			)
			RETURNS TABLE (
			activity_id BIGINT,
			segment_id BIGINT,
			min_distance_m DOUBLE PRECISION,
			overlap_length_m DOUBLE PRECISION,
			overlap_percentage DOUBLE PRECISION
			)
			LANGUAGE SQL STABLE AS
			$$
			WITH segment_data AS (
				SELECT segment_geog, ST_Length(segment_geog) AS segment_length
				FROM favorite_segments
				WHERE id = p_segment_id
			),
			-- Ensure segment exists
			segment_check AS (
				SELECT COUNT(*) AS cnt FROM segment_data
			),
			-- Initial filter: activities that have any part within tolerance
			candidate_activities AS (
				SELECT DISTINCT a.activity_id
				FROM activity_geometries a
				CROSS JOIN segment_data sd
				CROSS JOIN segment_check sc
				WHERE sc.cnt > 0  -- Only proceed if segment exists
				  AND ST_DWithin(a.route_geog, sd.segment_geog, p_tolerance_meters)
			),
			-- Check if all segment points are within tolerance of the activity route
			-- This ensures the segment geometry matches (allows deviations along route)
			segment_point_checks AS (
				SELECT 
					ca.activity_id,
					sp.point_geog,
					ST_Distance(sp.point_geog, a.route_geog) AS point_dist
				FROM candidate_activities ca
				CROSS JOIN segment_data sd
				CROSS JOIN LATERAL (
					SELECT (ST_DumpPoints(sd.segment_geog::geometry)).geom::geography AS point_geog
				) sp
				INNER JOIN activity_geometries a ON a.activity_id = ca.activity_id
			),
			-- Group by activity and check if all segment points are within tolerance
			activity_geometry_matches AS (
				SELECT 
					spc.activity_id,
					MAX(spc.point_dist) AS max_point_distance,
					COUNT(*) AS segment_point_count,
					COUNT(CASE WHEN spc.point_dist <= p_tolerance_meters THEN 1 END) AS points_within_tolerance
				FROM segment_point_checks spc
				GROUP BY spc.activity_id
				-- All segment points must be within tolerance (allows deviations along route)
				HAVING COUNT(CASE WHEN spc.point_dist <= p_tolerance_meters THEN 1 END) = COUNT(*)
			),
			-- Calculate overlap and metrics for matching activities
			overlap_calc AS (
				SELECT 
					agm.activity_id,
					agm.max_point_distance AS min_distance_m,
					-- Calculate overlap length using intersection with buffer
					-- This gives us the length of the activity route that overlaps with the segment
					ST_Length(
						ST_Intersection(
							ST_Buffer(a.route_geog::geometry, p_tolerance_meters)::geography,
							sd.segment_geog
						)
					) AS overlap_length_m
				FROM activity_geometry_matches agm
				CROSS JOIN segment_data sd
				INNER JOIN activity_geometries a ON a.activity_id = agm.activity_id
			)
			SELECT
				oc.activity_id,
				p_segment_id AS segment_id,
				oc.min_distance_m,
				oc.overlap_length_m,
				CASE 
					WHEN sd.segment_length > 0 THEN
						LEAST((oc.overlap_length_m / sd.segment_length) * 100.0, 100.0)
					ELSE 0.0
				END AS overlap_percentage
			FROM overlap_calc oc
			CROSS JOIN segment_data sd
			WHERE oc.overlap_length_m > 0
			ORDER BY oc.min_distance_m, oc.overlap_length_m DESC;
			$$;`,

		// Find route parts matching segment by name
		`CREATE OR REPLACE FUNCTION find_route_parts_matching_segment_by_name(
			p_segment_name TEXT,
			p_tolerance_meters DOUBLE PRECISION DEFAULT 15.0
			)
			RETURNS TABLE (
			activity_id BIGINT,
			segment_id BIGINT,
			segment_name TEXT,
			min_distance_m DOUBLE PRECISION,
			overlap_length_m DOUBLE PRECISION,
			overlap_percentage DOUBLE PRECISION
			)
			LANGUAGE SQL STABLE AS
			$$
			WITH segment_data AS (
				SELECT id, name, segment_geog, ST_Length(segment_geog) AS segment_length
				FROM favorite_segments
				WHERE name = p_segment_name
			),
			q AS (
				SELECT id, name, segment_geog AS line, segment_length, p_tolerance_meters AS tol
				FROM segment_data
			)
			SELECT
				a.activity_id,
				q.id AS segment_id,
				q.name AS segment_name,
				ST_Distance(a.route_geog, q.line) AS min_distance_m,
				ST_Length(
					ST_Intersection(
						ST_Buffer(a.route_geog::geometry, q.tol)::geography,
						q.line
					)
				) AS overlap_length_m,
				CASE 
					WHEN q.segment_length > 0 THEN
						ST_Length(
							ST_Intersection(
								ST_Buffer(a.route_geog::geometry, q.tol)::geography,
								q.line
							)
						) / q.segment_length * 100.0
					ELSE 0.0
				END AS overlap_percentage
			FROM activity_geometries a, q
			WHERE ST_DWithin(a.route_geog, q.line, q.tol)
			ORDER BY min_distance_m, overlap_percentage DESC;
			$$;`,
		// Find point indices for segment portion in an activity
		`CREATE OR REPLACE FUNCTION find_segment_point_indices(
			p_segment_id BIGINT,
			p_activity_id BIGINT,
			p_athlete_id BIGINT,
			p_tolerance_meters DOUBLE PRECISION DEFAULT 15.0
		)
		RETURNS TABLE (
			start_index INTEGER,
			end_index INTEGER
		)
		LANGUAGE SQL STABLE AS
		$$
		WITH segment_line AS (
			SELECT segment_geog AS line, p_tolerance_meters AS tol
			FROM favorite_segments
			WHERE id = p_segment_id
		),
		activity_points AS (
			SELECT 
				point_index,
				location,
				ST_Distance(location, sl.line) AS dist
			FROM point_samples ps, segment_line sl
			WHERE ps.activity_id = p_activity_id 
			  AND ps.athlete_id = p_athlete_id
			  AND ST_DWithin(ps.location, sl.line, sl.tol)
			ORDER BY point_index
		)
		SELECT 
			MIN(point_index)::INTEGER AS start_index,
			MAX(point_index)::INTEGER AS end_index
		FROM activity_points;
		$$;`,
		// Get segment metrics (distance, elevation gain)
		`CREATE OR REPLACE FUNCTION get_segment_metrics(
			p_segment_id BIGINT
		)
		RETURNS TABLE (
			distance_m DOUBLE PRECISION,
			elevation_gain_m DOUBLE PRECISION
		)
		LANGUAGE SQL STABLE AS
		$$
		SELECT 
			ST_Length(segment_geog) AS distance_m,
			COALESCE(elevation_gain_m, 0.0) AS elevation_gain_m
		FROM favorite_segments
		WHERE id = p_segment_id;
		$$;`,
		// Get activity segment portion metrics
		`CREATE OR REPLACE FUNCTION get_activity_segment_metrics(
			p_segment_id BIGINT,
			p_activity_id BIGINT,
			p_athlete_id BIGINT,
			p_tolerance_meters DOUBLE PRECISION DEFAULT 15.0
		)
		RETURNS TABLE (
			avg_hr DOUBLE PRECISION,
			avg_speed DOUBLE PRECISION,
			distance_m DOUBLE PRECISION,
			elevation_gain_m DOUBLE PRECISION
		)
		LANGUAGE SQL STABLE AS
		$$
		WITH segment_line AS (
			SELECT segment_geog AS line, p_tolerance_meters AS tol
			FROM favorite_segments
			WHERE id = p_segment_id
		),
		segment_points AS (
			SELECT 
				ps.point_index,
				ps.heartrate,
				ps.speed,
				ps.altitude,
				ps.location,
				LAG(ps.altitude) OVER (ORDER BY ps.point_index) AS prev_altitude,
				LAG(ps.location) OVER (ORDER BY ps.point_index) AS prev_location
			FROM point_samples ps, segment_line sl
			WHERE ps.activity_id = p_activity_id 
			  AND ps.athlete_id = p_athlete_id
			  AND ST_DWithin(ps.location, sl.line, sl.tol)
			ORDER BY ps.point_index
		),
		segment_metrics AS (
			SELECT 
				AVG(heartrate) FILTER (WHERE heartrate IS NOT NULL) AS avg_hr,
				AVG(speed) FILTER (WHERE speed IS NOT NULL) AS avg_speed,
				SUM(
					CASE 
						WHEN altitude IS NOT NULL AND prev_altitude IS NOT NULL 
							AND altitude > prev_altitude 
						THEN altitude - prev_altitude 
						ELSE 0 
					END
				) AS elevation_gain,
				SUM(
					CASE 
						WHEN location IS NOT NULL AND prev_location IS NOT NULL
						THEN ST_Distance(location, prev_location)
						ELSE 0
					END
				) AS distance_m
			FROM segment_points
		)
		SELECT 
			COALESCE((SELECT avg_hr FROM segment_metrics), 0.0) AS avg_hr,
			COALESCE((SELECT avg_speed FROM segment_metrics), 0.0) AS avg_speed,
			COALESCE((SELECT distance_m FROM segment_metrics), 0.0) AS distance_m,
			COALESCE((SELECT elevation_gain FROM segment_metrics), 0.0) AS elevation_gain_m
		FROM (SELECT 1) AS dummy;
		$$;`,
	}

	for _, helperQuery := range helperQueries {
		if _, err := conn.Exec(ctx, helperQuery); err != nil {
			return fmt.Errorf("failed to create helper function: %w", err)
		}
	}

	return nil
}

// TableSchema represents the expected schema for a table
type TableSchema struct {
	Name        string
	Columns     []ColumnDef
	Indexes     []string
	Constraints []string
	IsCache     bool // If true, safe to drop/recreate on mismatch
}

// ColumnDef represents a column definition
type ColumnDef struct {
	Name         string
	Type         string
	Nullable     bool
	DefaultValue *string
}

// TableValidationResult represents the result of validating a table
type TableValidationResult struct {
	TableName   string
	Exists      bool
	Matches     bool
	Differences []string
	ActionTaken string
}

// ValidateAndMigrateSchema validates all tables and creates/fixes them as needed
// If forceRebuild is true, tables with schema mismatches will be dropped and recreated
// even if they are not cache tables (WARNING: this will delete all data in those tables)
func ValidateAndMigrateSchema(ctx context.Context, conn *pgx.Conn, forceRebuild bool) error {
	log.Printf("🔍 Validating database schema...")
	if forceRebuild {
		log.Printf("⚠️ Force rebuild mode enabled - mismatched tables will be dropped and recreated")
	}

	expectedSchemas := GetExpectedTableSchemas()
	var results []TableValidationResult

	for _, schema := range expectedSchemas {
		result, err := ValidateTableSchema(ctx, conn, schema)
		if err != nil {
			log.Printf("❌ Error validating table %s: %v", schema.Name, err)
			return fmt.Errorf("failed to validate table %s: %w", schema.Name, err)
		}
		results = append(results, result)

		// Handle missing or mismatched tables
		if !result.Exists {
			log.Printf("📝 Table %s does not exist, creating...", schema.Name)
			if err := createTableBySchema(ctx, conn, schema); err != nil {
				return fmt.Errorf("failed to create table %s: %w", schema.Name, err)
			}
			result.ActionTaken = "created"
			log.Printf("✅ Created table %s", schema.Name)
		} else if !result.Matches {
			log.Printf("⚠️ Table %s schema mismatch detected", schema.Name)
			if len(result.Differences) > 0 {
				for _, diff := range result.Differences {
					log.Printf("   - %s", diff)
				}
			}

			// For cache tables, always drop and recreate
			// For data tables, only drop and recreate if forceRebuild is true
			shouldRebuild := schema.IsCache || forceRebuild

			if shouldRebuild {
				if forceRebuild && !schema.IsCache {
					log.Printf("⚠️ WARNING: Force rebuilding data table %s - ALL DATA WILL BE LOST", schema.Name)
				}
				log.Printf("🔄 Dropping and recreating table %s...", schema.Name)
				dropQuery := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", schema.Name)
				if _, err := conn.Exec(ctx, dropQuery); err != nil {
					return fmt.Errorf("failed to drop table %s: %w", schema.Name, err)
				}
				if err := createTableBySchema(ctx, conn, schema); err != nil {
					return fmt.Errorf("failed to recreate table %s: %w", schema.Name, err)
				}
				result.ActionTaken = "recreated"
				log.Printf("✅ Recreated table %s", schema.Name)
			} else {
				// For data tables without force rebuild, log warning but don't auto-fix
				log.Printf("⚠️ Table %s has schema differences but is not a cache table", schema.Name)
				log.Printf("   Use -force-rebuild flag to rebuild this table (WARNING: will delete all data)")
				result.ActionTaken = "warning"
			}
		} else {
			log.Printf("✅ Table %s schema is valid", schema.Name)
			result.ActionTaken = "valid"
		}
	}

	// Ensure helper functions exist
	if err := createHelperFunctions(ctx, conn); err != nil {
		log.Printf("⚠️ Warning: failed to create helper functions: %v", err)
		// Don't fail on this, as PostGIS might not be available
	}

	// Migrate point_samples table to add cumulative_distance column if it doesn't exist
	if err := migratePointSamplesTable(ctx, conn); err != nil {
		log.Printf("⚠️ Warning: failed to migrate point_samples table: %v", err)
		// Don't fail on this, migration can be done manually
	}

	log.Printf("✅ Schema validation completed")
	return nil
}

// migratePointSamplesTable adds the cumulative_distance column to point_samples if it doesn't exist
func migratePointSamplesTable(ctx context.Context, conn *pgx.Conn) error {
	// Check if column exists
	checkQuery := `
	SELECT COUNT(*) FROM information_schema.columns 
	WHERE table_name = 'point_samples' AND column_name = 'cumulative_distance'
	`
	var count int
	err := conn.QueryRow(ctx, checkQuery).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for cumulative_distance column: %w", err)
	}

	if count == 0 {
		log.Printf("📝 Adding cumulative_distance column to point_samples table...")
		alterQuery := `ALTER TABLE point_samples ADD COLUMN cumulative_distance DOUBLE PRECISION`
		_, err := conn.Exec(ctx, alterQuery)
		if err != nil {
			return fmt.Errorf("failed to add cumulative_distance column: %w", err)
		}
		log.Printf("✅ Added cumulative_distance column to point_samples table")
	}

	return nil
}

// ValidateTableSchema validates a table against expected schema
func ValidateTableSchema(ctx context.Context, conn *pgx.Conn, expected TableSchema) (TableValidationResult, error) {
	result := TableValidationResult{
		TableName:   expected.Name,
		Exists:      false,
		Matches:     false,
		Differences: []string{},
	}

	// Check if table exists
	var exists bool
	checkQuery := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = $1
		)
	`
	err := conn.QueryRow(ctx, checkQuery, expected.Name).Scan(&exists)
	if err != nil {
		return result, fmt.Errorf("failed to check table existence: %w", err)
	}

	result.Exists = exists
	if !exists {
		return result, nil
	}

	// Get actual columns - use pg_catalog for better type information
	columnsQuery := `
		SELECT 
			c.column_name,
			CASE 
				WHEN c.data_type = 'USER-DEFINED' THEN c.udt_name
				ELSE c.data_type
			END as data_type,
			c.is_nullable,
			c.column_default
		FROM information_schema.columns c
		WHERE c.table_schema = 'public' AND c.table_name = $1
		ORDER BY c.ordinal_position
	`
	rows, err := conn.Query(ctx, columnsQuery, expected.Name)
	if err != nil {
		return result, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	actualColumns := make(map[string]ColumnDef)
	for rows.Next() {
		var col ColumnDef
		var nullable string
		var defaultValue *string
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &defaultValue); err != nil {
			return result, fmt.Errorf("failed to scan column: %w", err)
		}
		col.Nullable = nullable == "YES"
		col.DefaultValue = defaultValue
		actualColumns[col.Name] = col
	}

	// Check expected columns
	expectedColumns := make(map[string]ColumnDef)
	for _, col := range expected.Columns {
		expectedColumns[col.Name] = col
	}

	// Compare columns
	for _, expectedCol := range expected.Columns {
		actualCol, ok := actualColumns[expectedCol.Name]
		if !ok {
			result.Differences = append(result.Differences,
				fmt.Sprintf("missing column: %s", expectedCol.Name))
			continue
		}

		// Normalize type for comparison (PostgreSQL has many type aliases)
		expectedType := normalizeType(expectedCol.Type)
		actualType := normalizeType(actualCol.Type)
		if expectedType != actualType {
			result.Differences = append(result.Differences,
				fmt.Sprintf("column %s type mismatch: expected %s, got %s", expectedCol.Name, expectedType, actualType))
		}

		if expectedCol.Nullable != actualCol.Nullable {
			result.Differences = append(result.Differences,
				fmt.Sprintf("column %s nullable mismatch: expected %v, got %v", expectedCol.Name, expectedCol.Nullable, actualCol.Nullable))
		}
	}

	// Check for extra columns (warn but don't fail)
	// First, check if columns are generated columns (we should ignore those if they're not in expected schema)
	generatedColumnsQuery := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' 
		AND table_name = $1
		AND is_generated = 'ALWAYS'
	`
	generatedRows, err := conn.Query(ctx, generatedColumnsQuery, expected.Name)
	generatedCols := make(map[string]bool)
	if err == nil {
		defer generatedRows.Close()
		for generatedRows.Next() {
			var genColName string
			if err := generatedRows.Scan(&genColName); err == nil {
				generatedCols[genColName] = true
			}
		}
	}

	for colName := range actualColumns {
		if _, ok := expectedColumns[colName]; !ok {
			// Don't warn about generated columns that aren't in expected schema
			// (they're auto-created and may vary)
			if !generatedCols[colName] {
				result.Differences = append(result.Differences,
					fmt.Sprintf("extra column: %s (not in expected schema)", colName))
			}
		}
	}

	// Check indexes (simplified - just check if they exist)
	for _, indexName := range expected.Indexes {
		indexQuery := `
			SELECT EXISTS (
				SELECT FROM pg_indexes
				WHERE schemaname = 'public' AND indexname = $1
			)
		`
		var indexExists bool
		if err := conn.QueryRow(ctx, indexQuery, indexName).Scan(&indexExists); err != nil {
			// Log but don't fail
			log.Printf("⚠️ Could not check index %s: %v", indexName, err)
			continue
		}
		if !indexExists {
			result.Differences = append(result.Differences,
				fmt.Sprintf("missing index: %s", indexName))
		}
	}

	result.Matches = len(result.Differences) == 0
	return result, nil
}

// normalizeType normalizes PostgreSQL type names for comparison
func normalizeType(typ string) string {
	typ = strings.ToLower(typ)
	typ = strings.TrimSpace(typ)

	// Handle geography/geometry types - they may have parameters
	if strings.HasPrefix(typ, "geography") || strings.HasPrefix(typ, "geometry") {
		return strings.Split(typ, "(")[0] // Just return base type
	}

	// Handle common type aliases
	typeMap := map[string]string{
		"int8":        "bigint",
		"int4":        "integer",
		"float8":      "double precision",
		"float4":      "real",
		"bool":        "boolean",
		"timestamptz": "timestamp with time zone",
		"timestamp":   "timestamp without time zone",
		"geography":   "geography",
		"geometry":    "geometry",
		"text":        "text",
		"varchar":     "character varying",
		"character":   "character",
	}
	if normalized, ok := typeMap[typ]; ok {
		return normalized
	}
	return typ
}

// GetExpectedTableSchemas returns the expected schemas for all tables
func GetExpectedTableSchemas() []TableSchema {
	return []TableSchema{
		{
			Name:    "activity_summaries",
			IsCache: false,
			Columns: []ColumnDef{
				{Name: "id", Type: "bigint", Nullable: false},
				{Name: "athlete_id", Type: "bigint", Nullable: false},
				{Name: "name", Type: "text", Nullable: false},
				{Name: "distance", Type: "double precision", Nullable: false},
				{Name: "moving_time", Type: "double precision", Nullable: false},
				{Name: "elapsed_time", Type: "double precision", Nullable: false},
				{Name: "total_elevation_gain", Type: "double precision", Nullable: false},
				{Name: "type", Type: "text", Nullable: false},
				{Name: "sport_type", Type: "text", Nullable: true},
				{Name: "workout_type", Type: "integer", Nullable: true},
				{Name: "start_date", Type: "timestamp with time zone", Nullable: false},
				{Name: "utc_offset", Type: "double precision", Nullable: true},
				{Name: "start_lat", Type: "double precision", Nullable: true},
				{Name: "start_lng", Type: "double precision", Nullable: true},
				{Name: "end_lat", Type: "double precision", Nullable: true},
				{Name: "end_lng", Type: "double precision", Nullable: true},
				{Name: "location_city", Type: "text", Nullable: true},
				{Name: "location_state", Type: "text", Nullable: true},
				{Name: "location_country", Type: "text", Nullable: true},
				{Name: "gear_id", Type: "text", Nullable: true},
				{Name: "average_speed", Type: "double precision", Nullable: true},
				{Name: "max_speed", Type: "double precision", Nullable: true},
				{Name: "average_cadence", Type: "double precision", Nullable: true},
				{Name: "average_watts", Type: "double precision", Nullable: true},
				{Name: "kilojoules", Type: "double precision", Nullable: true},
				{Name: "average_heartrate", Type: "double precision", Nullable: true},
				{Name: "max_heartrate", Type: "double precision", Nullable: true},
				{Name: "max_watts", Type: "double precision", Nullable: true},
				{Name: "suffer_score", Type: "double precision", Nullable: true},
				{Name: "created_at", Type: "timestamp with time zone", Nullable: true},
				{Name: "updated_at", Type: "timestamp with time zone", Nullable: true},
			},
			Indexes: []string{
				"idx_activity_summaries_athlete_id",
				"idx_activity_summaries_start_date",
				"idx_activity_summaries_type",
				"idx_activity_summaries_athlete_start_date",
				"idx_activity_summaries_athlete_type",
				"idx_activity_summaries_location_country",
			},
		},
		{
			Name:    "activity_geometries",
			IsCache: false,
			Columns: []ColumnDef{
				{Name: "activity_id", Type: "bigint", Nullable: false},
				{Name: "athlete_id", Type: "bigint", Nullable: false},
				{Name: "route_geog", Type: "geography", Nullable: false},
				{Name: "route_bbox_geom", Type: "geometry", Nullable: true}, // Generated column
				{Name: "route_geog_simplified", Type: "geography", Nullable: true},
				{Name: "created_at", Type: "timestamp with time zone", Nullable: true},
				{Name: "updated_at", Type: "timestamp with time zone", Nullable: true},
			},
			Indexes: []string{
				"idx_activity_geometries_athlete_id",
				"idx_activity_geometries_route_geog",
				"idx_activity_geometries_bbox",
				"idx_activity_geometries_route_geog_simplified",
				"idx_activity_geometries_activity_id",
			},
		},
		{
			Name:    "point_samples",
			IsCache: false,
			Columns: []ColumnDef{
				{Name: "id", Type: "bigint", Nullable: false},
				{Name: "activity_id", Type: "bigint", Nullable: false},
				{Name: "athlete_id", Type: "bigint", Nullable: false},
				{Name: "point_index", Type: "integer", Nullable: false},
				{Name: "time", Type: "timestamp with time zone", Nullable: false},
				{Name: "location", Type: "geography", Nullable: false},
				{Name: "altitude", Type: "double precision", Nullable: true},
				{Name: "heartrate", Type: "integer", Nullable: true},
				{Name: "speed", Type: "double precision", Nullable: true},
				{Name: "watts", Type: "integer", Nullable: true},
				{Name: "cadence", Type: "integer", Nullable: true},
				{Name: "grade", Type: "double precision", Nullable: true},
				{Name: "moving", Type: "boolean", Nullable: true},
				{Name: "cumulative_distance", Type: "double precision", Nullable: true},
				{Name: "created_at", Type: "timestamp with time zone", Nullable: true},
			},
			Indexes: []string{
				"idx_point_samples_athlete_id",
				"idx_point_samples_activity_id",
				"idx_point_samples_time",
				"idx_point_samples_athlete_activity_time",
				"idx_point_samples_location",
				"idx_point_samples_activity_point_index",
				"idx_point_samples_time_range",
			},
		},
		{
			Name:    "favorite_segments",
			IsCache: false,
			Columns: []ColumnDef{
				{Name: "id", Type: "bigint", Nullable: false},
				{Name: "athlete_id", Type: "bigint", Nullable: false},
				{Name: "name", Type: "text", Nullable: false},
				{Name: "description", Type: "text", Nullable: true},
				{Name: "segment_geog", Type: "geography", Nullable: false},
				{Name: "segment_bbox_geom", Type: "geometry", Nullable: true}, // Generated column
				{Name: "segment_geog_simplified", Type: "geography", Nullable: true},
				{Name: "elevation_gain_m", Type: "double precision", Nullable: true},
				{Name: "created_at", Type: "timestamp with time zone", Nullable: true},
				{Name: "updated_at", Type: "timestamp with time zone", Nullable: true},
			},
			Indexes: []string{
				"idx_favorite_segments_athlete_id",
				"idx_favorite_segments_segment_geog",
				"idx_favorite_segments_bbox",
				"idx_favorite_segments_segment_geog_simplified",
				"idx_favorite_segments_athlete_name",
				"idx_favorite_segments_created_at",
			},
		},
		{
			Name:    "segment_activity_matches",
			IsCache: true, // This is a cache table, safe to drop/recreate
			Columns: []ColumnDef{
				{Name: "segment_id", Type: "bigint", Nullable: false},
				{Name: "activity_id", Type: "bigint", Nullable: false},
				{Name: "tolerance_meters", Type: "double precision", Nullable: false},
				{Name: "min_distance_m", Type: "double precision", Nullable: false},
				{Name: "overlap_length_m", Type: "double precision", Nullable: false},
				{Name: "overlap_percentage", Type: "double precision", Nullable: false},
				{Name: "start_index", Type: "integer", Nullable: true},
				{Name: "end_index", Type: "integer", Nullable: true},
				{Name: "avg_hr", Type: "double precision", Nullable: true},
				{Name: "avg_speed", Type: "double precision", Nullable: true},
				{Name: "distance_m", Type: "double precision", Nullable: true},
				{Name: "elevation_gain_m", Type: "double precision", Nullable: true},
				{Name: "cached_at", Type: "timestamp with time zone", Nullable: true},
			},
			Indexes: []string{
				"idx_segment_activity_matches_segment_tolerance",
				"idx_segment_activity_matches_activity_id",
				"idx_segment_activity_matches_cached_at",
			},
		},
	}
}

// createTableBySchema creates a table based on the schema definition
func createTableBySchema(ctx context.Context, conn *pgx.Conn, schema TableSchema) error {
	// This is a simplified version - for full implementation, we'd need to handle
	// all the CREATE TABLE logic. For now, we'll call the existing create functions
	switch schema.Name {
	case "activity_summaries":
		return createActivitySummariesTable(ctx, conn)
	case "activity_geometries":
		return createActivityGeometriesTable(ctx, conn)
	case "point_samples":
		return createPointSamplesTable(ctx, conn)
	case "favorite_segments":
		return createFavoriteSegmentsTable(ctx, conn)
	case "segment_activity_matches":
		return createSegmentActivityMatchesTable(ctx, conn)
	default:
		return fmt.Errorf("unknown table schema: %s", schema.Name)
	}
}
