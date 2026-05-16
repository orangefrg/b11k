package pggeo

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

type DiscoveredCoverageStatus struct {
	AthleteID            int64      `json:"athlete_id"`
	Enabled              bool       `json:"enabled"`
	Stale                bool       `json:"stale"`
	BuildableActivities  int        `json:"buildable_activities"`
	CachedActivities     int        `json:"cached_activities"`
	RadiusMeters         float64    `json:"radius_meters"`
	SampleDistanceMeters float64    `json:"sample_distance_meters"`
	RebuiltAt            *time.Time `json:"rebuilt_at,omitempty"`
	BBox                 []float64  `json:"bbox,omitempty"`
	Message              string     `json:"message,omitempty"`
}

func RebuildDiscoveredCoverage(ctx context.Context, conn *pgx.Conn, athleteID int64, sampleDistanceMeters, radiusMeters float64) (*DiscoveredCoverageStatus, error) {
	if sampleDistanceMeters <= 0 {
		return nil, fmt.Errorf("sample distance must be positive")
	}
	if radiusMeters <= 0 {
		return nil, fmt.Errorf("reveal radius must be positive")
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin discovered coverage rebuild: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM discovered_activity_buffers WHERE athlete_id = $1`, athleteID); err != nil {
		return nil, fmt.Errorf("clear discovered activity buffers: %w", err)
	}

	insertBuffersQuery := `
	WITH raw AS (
		SELECT
			p.activity_id,
			p.athlete_id,
			p.point_index,
			p.location,
			FLOOR(COALESCE(p.cumulative_distance, 0) / $2)::BIGINT AS bucket,
			ROW_NUMBER() OVER (PARTITION BY p.activity_id ORDER BY p.point_index) AS rn,
			COUNT(*) OVER (PARTITION BY p.activity_id) AS total
		FROM point_samples p
		JOIN activity_summaries s ON s.id = p.activity_id AND s.athlete_id = p.athlete_id
		WHERE p.athlete_id = $1
			AND LOWER(COALESCE(s.type, '') || ' ' || COALESCE(s.sport_type, '')) ~ '(ride|bike|cycling)'
	),
	bucket_first AS (
		SELECT DISTINCT ON (activity_id, bucket)
			activity_id, athlete_id, point_index, location
		FROM raw
		ORDER BY activity_id, bucket, point_index
	),
	endpoints AS (
		SELECT activity_id, athlete_id, point_index, location
		FROM raw
		WHERE rn = 1 OR rn = total
	),
	selected AS (
		SELECT DISTINCT ON (activity_id, point_index)
			activity_id, athlete_id, point_index, location
		FROM (
			SELECT * FROM bucket_first
			UNION ALL
			SELECT * FROM endpoints
		) candidate_points
		ORDER BY activity_id, point_index
	),
	lines AS (
		SELECT
			activity_id,
			athlete_id,
			ST_MakeLine(location::geometry ORDER BY point_index)::geography AS route_geog,
			COUNT(*) AS point_count
		FROM selected
		GROUP BY activity_id, athlete_id
		HAVING COUNT(*) >= 2
	)
	INSERT INTO discovered_activity_buffers (
		activity_id,
		athlete_id,
		sample_distance_m,
		radius_m,
		route_geog,
		buffer_geog,
		updated_at
	)
	SELECT
		activity_id,
		athlete_id,
		$2,
		$3,
		route_geog,
		ST_Multi(ST_Buffer(route_geog, $3)::geometry)::geography,
		NOW()
	FROM lines
	`
	if _, err := tx.Exec(ctx, insertBuffersQuery, athleteID, sampleDistanceMeters, radiusMeters); err != nil {
		return nil, fmt.Errorf("build discovered activity buffers: %w", err)
	}

	upsertCoverageQuery := `
	WITH coverage AS (
		SELECT
			COUNT(*)::INTEGER AS activity_count,
			CASE
				WHEN COUNT(*) = 0 THEN NULL
				ELSE ST_Multi(ST_UnaryUnion(ST_Collect(buffer_geog::geometry)))::geography
			END AS coverage_geog
		FROM discovered_activity_buffers
		WHERE athlete_id = $1
			AND sample_distance_m = $2
			AND radius_m = $3
	)
	INSERT INTO discovered_coverage_cache (
		athlete_id,
		sample_distance_m,
		radius_m,
		activity_count,
		coverage_geog,
		stale,
		rebuilt_at,
		updated_at
	)
	SELECT $1, $2, $3, activity_count, coverage_geog, FALSE, NOW(), NOW()
	FROM coverage
	ON CONFLICT (athlete_id) DO UPDATE SET
		sample_distance_m = EXCLUDED.sample_distance_m,
		radius_m = EXCLUDED.radius_m,
		activity_count = EXCLUDED.activity_count,
		coverage_geog = EXCLUDED.coverage_geog,
		stale = FALSE,
		rebuilt_at = EXCLUDED.rebuilt_at,
		updated_at = NOW()
	`
	if _, err := tx.Exec(ctx, upsertCoverageQuery, athleteID, sampleDistanceMeters, radiusMeters); err != nil {
		return nil, fmt.Errorf("build discovered coverage union: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit discovered coverage rebuild: %w", err)
	}

	return GetDiscoveredCoverageStatus(ctx, conn, athleteID, sampleDistanceMeters, radiusMeters)
}

func MarkDiscoveredCoverageStale(ctx context.Context, conn *pgx.Conn, athleteID int64) error {
	_, err := conn.Exec(ctx, `
		UPDATE discovered_coverage_cache
		SET stale = TRUE, updated_at = NOW()
		WHERE athlete_id = $1
	`, athleteID)
	return err
}

func GetDiscoveredCoverageStatus(ctx context.Context, conn *pgx.Conn, athleteID int64, sampleDistanceMeters, radiusMeters float64) (*DiscoveredCoverageStatus, error) {
	buildable, err := countBuildableBikeActivities(ctx, conn, athleteID)
	if err != nil {
		return nil, err
	}

	status := &DiscoveredCoverageStatus{
		AthleteID:            athleteID,
		Enabled:              true,
		Stale:                true,
		BuildableActivities:  buildable,
		RadiusMeters:         radiusMeters,
		SampleDistanceMeters: sampleDistanceMeters,
		Message:              "Discovered map has not been built yet.",
	}

	var cachedSample, cachedRadius float64
	var stale bool
	var rebuiltAt *time.Time
	var minLng, minLat, maxLng, maxLat *float64
	err = conn.QueryRow(ctx, `
		SELECT
			sample_distance_m,
			radius_m,
			activity_count,
			stale,
			rebuilt_at,
			ST_XMin(coverage_bbox_geom),
			ST_YMin(coverage_bbox_geom),
			ST_XMax(coverage_bbox_geom),
			ST_YMax(coverage_bbox_geom)
		FROM discovered_coverage_cache
		WHERE athlete_id = $1
	`, athleteID).Scan(&cachedSample, &cachedRadius, &status.CachedActivities, &stale, &rebuiltAt, &minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		if err == pgx.ErrNoRows {
			return status, nil
		}
		return nil, fmt.Errorf("load discovered coverage status: %w", err)
	}

	status.RebuiltAt = rebuiltAt
	if minLng != nil && minLat != nil && maxLng != nil && maxLat != nil {
		status.BBox = []float64{*minLng, *minLat, *maxLng, *maxLat}
	}
	paramsChanged := math.Abs(cachedSample-sampleDistanceMeters) > 0.0001 || math.Abs(cachedRadius-radiusMeters) > 0.0001
	countChanged := status.CachedActivities != buildable
	status.Stale = stale || paramsChanged || countChanged
	switch {
	case paramsChanged:
		status.Message = "Discovered map settings changed. Rebuild the map to refresh coverage."
	case countChanged:
		status.Message = "Discovered map is out of sync with your bike activities. Rebuild the map to refresh coverage."
	case stale:
		status.Message = "Discovered map is marked stale. Rebuild the map to refresh coverage."
	default:
		status.Message = ""
	}

	return status, nil
}

func GetDiscoveredFogFeatureCollection(ctx context.Context, conn *pgx.Conn, athleteID int64, minLng, minLat, maxLng, maxLat, sampleDistanceMeters, radiusMeters float64) (string, error) {
	query := `
	WITH viewport AS (
		SELECT ST_MakeEnvelope($2, $3, $4, $5, 4326) AS geom
	),
	coverage AS (
		SELECT coverage_geog::geometry AS geom
		FROM discovered_coverage_cache
		WHERE athlete_id = $1
			AND sample_distance_m = $6
			AND radius_m = $7
			AND stale = FALSE
	),
	fog AS (
		SELECT
			CASE
				WHEN c.geom IS NULL THEN v.geom
				ELSE ST_Difference(v.geom, ST_Intersection(ST_MakeValid(c.geom), v.geom))
			END AS geom
		FROM viewport v
		LEFT JOIN coverage c ON TRUE
	)
	SELECT
		CASE
			WHEN ST_IsEmpty(geom) THEN '{"type":"FeatureCollection","features":[]}'
			ELSE json_build_object(
				'type', 'FeatureCollection',
				'features', json_build_array(json_build_object(
					'type', 'Feature',
					'properties', json_build_object(),
					'geometry', ST_AsGeoJSON(geom)::json
				))
			)::text
		END
	FROM fog
	`

	var featureCollection string
	if err := conn.QueryRow(ctx, query, athleteID, minLng, minLat, maxLng, maxLat, sampleDistanceMeters, radiusMeters).Scan(&featureCollection); err != nil {
		return "", fmt.Errorf("load discovered fog geometry: %w", err)
	}
	return featureCollection, nil
}

func GetDiscoveredCoverageFeatureCollection(ctx context.Context, conn *pgx.Conn, athleteID int64, minLng, minLat, maxLng, maxLat, sampleDistanceMeters, radiusMeters float64) (string, error) {
	query := `
	WITH viewport AS (
		SELECT ST_MakeEnvelope($2, $3, $4, $5, 4326) AS geom
	),
	coverage AS (
		SELECT ST_Intersection(ST_MakeValid(c.coverage_geog::geometry), v.geom) AS geom
		FROM discovered_coverage_cache c
		JOIN viewport v ON c.coverage_bbox_geom && v.geom
		WHERE c.athlete_id = $1
			AND c.sample_distance_m = $6
			AND c.radius_m = $7
			AND c.stale = FALSE
			AND c.coverage_geog IS NOT NULL
	),
	visible AS (
		SELECT ST_Multi(ST_UnaryUnion(ST_Collect(geom))) AS geom
		FROM coverage
		WHERE NOT ST_IsEmpty(geom)
	)
	SELECT
		CASE
			WHEN geom IS NULL OR ST_IsEmpty(geom) THEN '{"type":"FeatureCollection","features":[]}'
			ELSE json_build_object(
				'type', 'FeatureCollection',
				'features', json_build_array(json_build_object(
					'type', 'Feature',
					'properties', json_build_object(),
					'geometry', ST_AsGeoJSON(geom)::json
				))
			)::text
		END
	FROM visible
	`

	var featureCollection string
	if err := conn.QueryRow(ctx, query, athleteID, minLng, minLat, maxLng, maxLat, sampleDistanceMeters, radiusMeters).Scan(&featureCollection); err != nil {
		return "", fmt.Errorf("load discovered coverage geometry: %w", err)
	}
	return featureCollection, nil
}

func countBuildableBikeActivities(ctx context.Context, conn *pgx.Conn, athleteID int64) (int, error) {
	query := `
	SELECT COUNT(*)::INTEGER
	FROM (
		SELECT s.id
		FROM activity_summaries s
		JOIN point_samples p ON p.activity_id = s.id AND p.athlete_id = s.athlete_id
		WHERE s.athlete_id = $1
			AND LOWER(COALESCE(s.type, '') || ' ' || COALESCE(s.sport_type, '')) ~ '(ride|bike|cycling)'
		GROUP BY s.id
		HAVING COUNT(*) >= 2
	) buildable
	`
	var count int
	if err := conn.QueryRow(ctx, query, athleteID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count buildable bike activities: %w", err)
	}
	return count, nil
}
