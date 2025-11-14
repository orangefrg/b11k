package pggeo

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
)

// FavoriteSegment represents a favorite segment
type FavoriteSegment struct {
	ID                    int64    `json:"id"`
	AthleteID             int64    `json:"athlete_id"`
	Name                  string   `json:"name"`
	Description           *string  `json:"description,omitempty"`
	SegmentGeog           string   `json:"segment_geog"` // WKT representation
	SegmentGeogSimplified *string  `json:"segment_geog_simplified,omitempty"`
	ElevationGainM        *float64 `json:"elevation_gain_m,omitempty"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
}

// SegmentMatchResult represents the result of finding route parts matching a segment
type SegmentMatchResult struct {
	ActivityID        int64   `json:"activity_id"`
	SegmentID         int64   `json:"segment_id"`
	SegmentName       *string `json:"segment_name,omitempty"`
	MinDistanceM      float64 `json:"min_distance_m"`
	OverlapLengthM    float64 `json:"overlap_length_m"`
	OverlapPercentage float64 `json:"overlap_percentage"`
}

// InsertFavoriteSegment inserts a new favorite segment
// If pointSamples is provided, elevation gain will be calculated from them
func InsertFavoriteSegment(ctx context.Context, conn *pgx.Conn, athleteID int64, name, description string, latLngData [][]float64, pointSamples []PointSample) (*FavoriteSegment, error) {
	if len(latLngData) < 2 {
		return nil, fmt.Errorf("need at least 2 points to create a linestring")
	}

	// Extract longitude and latitude arrays for the helper function
	lons := make([]float64, len(latLngData))
	lats := make([]float64, len(latLngData))

	for i, point := range latLngData {
		lons[i] = point[1] // longitude
		lats[i] = point[0] // latitude
	}

	// Calculate elevation gain from point samples if available
	var elevationGain *float64
	if len(pointSamples) > 0 {
		totalGain := 0.0
		for i := 1; i < len(pointSamples); i++ {
			if pointSamples[i].Altitude != nil && pointSamples[i-1].Altitude != nil {
				diff := *pointSamples[i].Altitude - *pointSamples[i-1].Altitude
				if diff > 0 {
					totalGain += diff
				}
			}
		}
		if totalGain > 0 {
			elevationGain = &totalGain
		}
	}

	query := `
	INSERT INTO favorite_segments (athlete_id, name, description, segment_geog, elevation_gain_m)
	VALUES ($1, $2, $3, make_route_geog_from_lonlat($4, $5), $6)
	RETURNING id, athlete_id, name, description, 
		ST_AsText(segment_geog::geometry) as segment_geog,
		ST_AsText(segment_geog_simplified::geometry) as segment_geog_simplified,
		elevation_gain_m,
		created_at::text, updated_at::text
	`

	var segment FavoriteSegment
	var desc *string
	if description != "" {
		desc = &description
	}

	err := conn.QueryRow(ctx, query, athleteID, name, desc, lons, lats, elevationGain).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM,
		&segment.CreatedAt, &segment.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to insert favorite segment: %w", err)
	}

	// Refresh the simplified segment with default tolerance
	refreshQuery := `SELECT refresh_segment_simplified($1)`
	_, err = conn.Exec(ctx, refreshQuery, segment.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh simplified segment: %w", err)
	}

	return &segment, nil
}

// GetFavoriteSegment retrieves a favorite segment by ID
func GetFavoriteSegment(ctx context.Context, conn *pgx.Conn, segmentID int64) (*FavoriteSegment, error) {
	query := `
	SELECT id, athlete_id, name, description,
		ST_AsText(segment_geog::geometry) as segment_geog,
		ST_AsText(segment_geog_simplified::geometry) as segment_geog_simplified,
		elevation_gain_m,
		created_at::text, updated_at::text
	FROM favorite_segments
	WHERE id = $1
	`

	var segment FavoriteSegment
	err := conn.QueryRow(ctx, query, segmentID).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM,
		&segment.CreatedAt, &segment.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("segment with ID %d not found", segmentID)
		}
		return nil, fmt.Errorf("failed to get favorite segment: %w", err)
	}

	return &segment, nil
}

// GetFavoriteSegmentByName retrieves a favorite segment by name for a specific athlete
func GetFavoriteSegmentByName(ctx context.Context, conn *pgx.Conn, athleteID int64, name string) (*FavoriteSegment, error) {
	query := `
	SELECT id, athlete_id, name, description,
		ST_AsText(segment_geog::geometry) as segment_geog,
		ST_AsText(segment_geog_simplified::geometry) as segment_geog_simplified,
		elevation_gain_m,
		created_at::text, updated_at::text
	FROM favorite_segments
	WHERE athlete_id = $1 AND name = $2
	`

	var segment FavoriteSegment
	err := conn.QueryRow(ctx, query, athleteID, name).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM,
		&segment.CreatedAt, &segment.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("segment with name '%s' not found", name)
		}
		return nil, fmt.Errorf("failed to get favorite segment: %w", err)
	}

	return &segment, nil
}

// ListFavoriteSegments retrieves all favorite segments for a specific athlete
func ListFavoriteSegments(ctx context.Context, conn *pgx.Conn, athleteID int64) ([]FavoriteSegment, error) {
	query := `
	SELECT id, athlete_id, name, description,
		ST_AsText(segment_geog::geometry) as segment_geog,
		ST_AsText(segment_geog_simplified::geometry) as segment_geog_simplified,
		elevation_gain_m,
		created_at::text, updated_at::text
	FROM favorite_segments
	WHERE athlete_id = $1
	ORDER BY name
	`

	rows, err := conn.Query(ctx, query, athleteID)
	if err != nil {
		return nil, fmt.Errorf("failed to list favorite segments: %w", err)
	}
	defer rows.Close()

	var segments []FavoriteSegment
	for rows.Next() {
		var segment FavoriteSegment
		err := rows.Scan(
			&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
			&segment.SegmentGeog, &segment.SegmentGeogSimplified,
			&segment.ElevationGainM,
			&segment.CreatedAt, &segment.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan favorite segment: %w", err)
		}
		segments = append(segments, segment)
	}

	return segments, rows.Err()
}

// UpdateFavoriteSegment updates an existing favorite segment and invalidates its cache
func UpdateFavoriteSegment(ctx context.Context, conn *pgx.Conn, segmentID int64, name, description string, latLngData [][]float64) (*FavoriteSegment, error) {
	if len(latLngData) < 2 {
		return nil, fmt.Errorf("need at least 2 points to create a linestring")
	}

	// Extract longitude and latitude arrays for the helper function
	lons := make([]float64, len(latLngData))
	lats := make([]float64, len(latLngData))

	for i, point := range latLngData {
		lons[i] = point[1] // longitude
		lats[i] = point[0] // latitude
	}

	query := `
	UPDATE favorite_segments 
	SET name = $2, description = $3, segment_geog = make_route_geog_from_lonlat($4, $5), updated_at = NOW()
	WHERE id = $1
	RETURNING id, athlete_id, name, description,
		ST_AsText(segment_geog::geometry) as segment_geog,
		ST_AsText(segment_geog_simplified::geometry) as segment_geog_simplified,
		elevation_gain_m,
		created_at::text, updated_at::text
	`

	var segment FavoriteSegment
	var desc *string
	if description != "" {
		desc = &description
	}

	err := conn.QueryRow(ctx, query, segmentID, name, desc, lons, lats).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM,
		&segment.CreatedAt, &segment.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("segment with ID %d not found", segmentID)
		}
		return nil, fmt.Errorf("failed to update favorite segment: %w", err)
	}

	// Refresh the simplified segment with default tolerance
	refreshQuery := `SELECT refresh_segment_simplified($1)`
	_, err = conn.Exec(ctx, refreshQuery, segment.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh simplified segment: %w", err)
	}

	// Invalidate cache since segment geometry changed
	if err := InvalidateSegmentCache(ctx, conn, segmentID); err != nil {
		log.Printf("⚠️ Failed to invalidate cache for segment %d: %v", segmentID, err)
		// Continue even if cache invalidation fails
	}

	return &segment, nil
}

// DeleteFavoriteSegment deletes a favorite segment and invalidates its cache
func DeleteFavoriteSegment(ctx context.Context, conn *pgx.Conn, segmentID int64) error {
	// Invalidate cache before deleting segment (CASCADE will handle it, but we do it explicitly for clarity)
	if err := InvalidateSegmentCache(ctx, conn, segmentID); err != nil {
		log.Printf("⚠️ Failed to invalidate cache for segment %d: %v", segmentID, err)
		// Continue with deletion even if cache invalidation fails
	}

	query := `DELETE FROM favorite_segments WHERE id = $1`
	result, err := conn.Exec(ctx, query, segmentID)
	if err != nil {
		return fmt.Errorf("failed to delete favorite segment: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("segment with ID %d not found", segmentID)
	}

	return nil
}

// FindRoutePartsMatchingSegment finds route parts from activities that match a segment
func FindRoutePartsMatchingSegment(ctx context.Context, conn *pgx.Conn, segmentID int64, toleranceMeters float64) ([]SegmentMatchResult, error) {
	query := `SELECT * FROM find_route_parts_matching_segment($1, $2)`

	rows, err := conn.Query(ctx, query, segmentID, toleranceMeters)
	if err != nil {
		return nil, fmt.Errorf("failed to find route parts matching segment: %w", err)
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
			return nil, fmt.Errorf("failed to scan segment match result: %w", err)
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// FindRoutePartsMatchingSegmentByName finds route parts from activities that match a segment by name
func FindRoutePartsMatchingSegmentByName(ctx context.Context, conn *pgx.Conn, segmentName string, toleranceMeters float64) ([]SegmentMatchResult, error) {
	query := `SELECT * FROM find_route_parts_matching_segment_by_name($1, $2)`

	rows, err := conn.Query(ctx, query, segmentName, toleranceMeters)
	if err != nil {
		return nil, fmt.Errorf("failed to find route parts matching segment by name: %w", err)
	}
	defer rows.Close()

	var results []SegmentMatchResult
	for rows.Next() {
		var result SegmentMatchResult
		err := rows.Scan(
			&result.ActivityID, &result.SegmentID, &result.SegmentName,
			&result.MinDistanceM, &result.OverlapLengthM, &result.OverlapPercentage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan segment match result: %w", err)
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// RefreshSegmentSimplified refreshes the simplified geometry for a specific segment
func RefreshSegmentSimplified(ctx context.Context, conn *pgx.Conn, segmentID int64, toleranceMeters float64) error {
	query := `SELECT refresh_segment_simplified($1, $2)`
	_, err := conn.Exec(ctx, query, segmentID, toleranceMeters)
	return err
}
