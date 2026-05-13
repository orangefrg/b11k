package pggeo

import (
	"context"
	"fmt"
	"log"
	"strings"

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
	ElevationLossM        *float64 `json:"elevation_loss_m,omitempty"`
	NetElevationM         *float64 `json:"net_elevation_m,omitempty"`
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

// SegmentDashboardSummary is a compact, presentation-ready segment overview.
type SegmentDashboardSummary struct {
	ID            int64
	Name          string
	Description   *string
	CreatedAt     string
	DistanceLabel string
	NetRiseLabel  string
	AscentLabel   string
	SlopeLabel    string
	Direction     string
	DirectionKey  string
	Attempts      int
	MinTimeLabel  string
	MaxTimeLabel  string
	MinHRLabel    string
	MaxHRLabel    string
	SortBestTime  float64
	SortWorstTime float64
	SortAttempts  int
	SortMinHR     float64
	SortMaxHR     float64
	SortSlope     float64
	SortAscent    float64
	SortDirection string
	SortName      string
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
	var elevationLoss *float64
	var netElevation *float64
	if len(pointSamples) > 0 {
		totalGain := 0.0
		totalLoss := 0.0
		for i := 1; i < len(pointSamples); i++ {
			if pointSamples[i].Altitude != nil && pointSamples[i-1].Altitude != nil {
				diff := *pointSamples[i].Altitude - *pointSamples[i-1].Altitude
				if diff > 0 {
					totalGain += diff
				} else if diff < 0 {
					totalLoss += -diff
				}
			}
		}
		if totalGain > 0 {
			elevationGain = &totalGain
		}
		if totalLoss > 0 {
			elevationLoss = &totalLoss
		}
		if pointSamples[0].Altitude != nil && pointSamples[len(pointSamples)-1].Altitude != nil {
			net := *pointSamples[len(pointSamples)-1].Altitude - *pointSamples[0].Altitude
			netElevation = &net
		}
	}

	query := `
	INSERT INTO favorite_segments (athlete_id, name, description, segment_geog, elevation_gain_m, elevation_loss_m, net_elevation_m)
	VALUES ($1, $2, $3, make_route_geog_from_lonlat($4, $5), $6, $7, $8)
	RETURNING id, athlete_id, name, description, 
		ST_AsText(segment_geog::geometry) as segment_geog,
		ST_AsText(segment_geog_simplified::geometry) as segment_geog_simplified,
		elevation_gain_m, elevation_loss_m, net_elevation_m,
		created_at::text, updated_at::text
	`

	var segment FavoriteSegment
	var desc *string
	if description != "" {
		desc = &description
	}

	err := conn.QueryRow(ctx, query, athleteID, name, desc, lons, lats, elevationGain, elevationLoss, netElevation).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM, &segment.ElevationLossM, &segment.NetElevationM,
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
		elevation_gain_m, elevation_loss_m, net_elevation_m,
		created_at::text, updated_at::text
	FROM favorite_segments
	WHERE id = $1
	`

	var segment FavoriteSegment
	err := conn.QueryRow(ctx, query, segmentID).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM, &segment.ElevationLossM, &segment.NetElevationM,
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
		elevation_gain_m, elevation_loss_m, net_elevation_m,
		created_at::text, updated_at::text
	FROM favorite_segments
	WHERE athlete_id = $1 AND name = $2
	`

	var segment FavoriteSegment
	err := conn.QueryRow(ctx, query, athleteID, name).Scan(
		&segment.ID, &segment.AthleteID, &segment.Name, &segment.Description,
		&segment.SegmentGeog, &segment.SegmentGeogSimplified,
		&segment.ElevationGainM, &segment.ElevationLossM, &segment.NetElevationM,
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
		elevation_gain_m, elevation_loss_m, net_elevation_m,
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
			&segment.ElevationGainM, &segment.ElevationLossM, &segment.NetElevationM,
			&segment.CreatedAt, &segment.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan favorite segment: %w", err)
		}
		segments = append(segments, segment)
	}

	return segments, rows.Err()
}

// ListSegmentDashboardSummaries retrieves dashboard-ready summaries for all favorite segments.
func ListSegmentDashboardSummaries(ctx context.Context, conn *pgx.Conn, athleteID int64, toleranceMeters float64) ([]SegmentDashboardSummary, error) {
	segments, err := ListFavoriteSegments(ctx, conn, athleteID)
	if err != nil {
		return nil, err
	}

	summaries := make([]SegmentDashboardSummary, 0, len(segments))
	for _, segment := range segments {
		summary := SegmentDashboardSummary{
			ID:            segment.ID,
			Name:          segment.Name,
			Description:   segment.Description,
			CreatedAt:     segment.CreatedAt,
			NetRiseLabel:  "n/a",
			AscentLabel:   "n/a",
			SlopeLabel:    "n/a",
			Direction:     "Unknown",
			DirectionKey:  "unknown",
			MinTimeLabel:  "n/a",
			MaxTimeLabel:  "n/a",
			MinHRLabel:    "n/a",
			MaxHRLabel:    "n/a",
			SortBestTime:  1e12,
			SortWorstTime: 0,
			SortMinHR:     1e12,
			SortMaxHR:     0,
			SortSlope:     0,
			SortAscent:    0,
			SortDirection: "unknown",
			SortName:      strings.ToLower(segment.Name),
		}

		var distanceM float64
		if err := conn.QueryRow(ctx, `SELECT ST_Length(segment_geog) FROM favorite_segments WHERE id = $1`, segment.ID).Scan(&distanceM); err == nil {
			summary.DistanceLabel = formatDistanceLabel(distanceM)
		} else {
			summary.DistanceLabel = "n/a"
		}

		if segment.ElevationGainM != nil {
			summary.AscentLabel = fmt.Sprintf("%.0f m", *segment.ElevationGainM)
			summary.SortAscent = *segment.ElevationGainM
		}
		if segment.NetElevationM != nil {
			summary.NetRiseLabel = fmt.Sprintf("%+.0f m", *segment.NetElevationM)
			if distanceM > 0 {
				slope := *segment.NetElevationM / distanceM
				summary.SortSlope = slope
				summary.SlopeLabel = fmt.Sprintf("%+.1f%%", slope*100)
				switch {
				case slope > 0.02:
					summary.Direction = "Uphill"
					summary.DirectionKey = "uphill"
				case slope < -0.02:
					summary.Direction = "Downhill"
					summary.DirectionKey = "downhill"
				default:
					summary.Direction = "Flat"
					summary.DirectionKey = "flat"
				}
			}
		}
		summary.SortDirection = summary.DirectionKey

		efforts, err := GetActivitiesForSegment(ctx, conn, athleteID, segment.ID, toleranceMeters, "total_time", false)
		if err != nil {
			log.Printf("⚠️ Failed to summarize segment %d: %v", segment.ID, err)
			summaries = append(summaries, summary)
			continue
		}

		summary.Attempts = len(efforts)
		summary.SortAttempts = len(efforts)
		for _, effort := range efforts {
			if effort.SegmentElapsedSecs != nil && *effort.SegmentElapsedSecs > 0 {
				seconds := *effort.SegmentElapsedSecs
				if seconds < summary.SortBestTime {
					summary.SortBestTime = seconds
					summary.MinTimeLabel = formatDurationLabel(seconds)
				}
				if seconds > summary.SortWorstTime {
					summary.SortWorstTime = seconds
					summary.MaxTimeLabel = formatDurationLabel(seconds)
				}
			}
			if effort.SegmentAvgHR != nil && *effort.SegmentAvgHR > 0 {
				hr := *effort.SegmentAvgHR
				if hr < summary.SortMinHR {
					summary.SortMinHR = hr
					summary.MinHRLabel = fmt.Sprintf("%.0f bpm", hr)
				}
				if hr > summary.SortMaxHR {
					summary.SortMaxHR = hr
					summary.MaxHRLabel = fmt.Sprintf("%.0f bpm", hr)
				}
			}
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func formatDistanceLabel(meters float64) string {
	if meters >= 1000 {
		return fmt.Sprintf("%.2f km", meters/1000)
	}
	return fmt.Sprintf("%.0f m", meters)
}

func formatDurationLabel(seconds float64) string {
	total := int(seconds + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
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
		elevation_gain_m, elevation_loss_m, net_elevation_m,
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
		&segment.ElevationGainM, &segment.ElevationLossM, &segment.NetElevationM,
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
