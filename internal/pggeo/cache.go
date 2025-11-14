package pggeo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SegmentActivityCacheEntry represents a cached segment-activity match with metrics
type SegmentActivityCacheEntry struct {
	SegmentID         int64
	ActivityID        int64
	ToleranceMeters   float64
	MinDistanceM      float64
	OverlapLengthM    float64
	OverlapPercentage float64
	StartIndex        *int
	EndIndex          *int
	AvgHR             *float64
	AvgSpeed          *float64
	DistanceM         *float64
	ElevationGainM    *float64
}

// CacheSegmentActivityMatches caches segment-activity match results
// Uses UPSERT to update existing entries or insert new ones, preserving cache for other segments
func CacheSegmentActivityMatches(ctx context.Context, conn *pgx.Conn, segmentID int64, toleranceMeters float64, matches []SegmentMatchResult) error {
	if len(matches) == 0 {
		return nil
	}

	// Get segment length once for all matches (ignore error - will use 0 if query fails)
	var segmentLength float64
	_ = conn.QueryRow(ctx, `
		SELECT ST_Length(segment_geog)
		FROM favorite_segments
		WHERE id = $1
	`, segmentID).Scan(&segmentLength)

	// Insert or update cache entries (UPSERT)
	for _, match := range matches {
		// Calculate overlap percentage if not already set
		overlapPct := match.OverlapPercentage
		if overlapPct == 0 && match.OverlapLengthM > 0 && segmentLength > 0 {
			overlapPct = (match.OverlapLengthM / segmentLength) * 100.0
		}

		_, err := conn.Exec(ctx, `
			INSERT INTO segment_activity_matches 
			(segment_id, activity_id, tolerance_meters, min_distance_m, overlap_length_m, overlap_percentage, cached_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (segment_id, activity_id, tolerance_meters) 
			DO UPDATE SET 
				min_distance_m = EXCLUDED.min_distance_m,
				overlap_length_m = EXCLUDED.overlap_length_m,
				overlap_percentage = EXCLUDED.overlap_percentage,
				cached_at = NOW()
		`, segmentID, match.ActivityID, toleranceMeters, match.MinDistanceM, match.OverlapLengthM, overlapPct)
		if err != nil {
			return fmt.Errorf("failed to cache match: %w", err)
		}
	}

	return nil
}

// CacheSegmentActivityMetrics caches metrics for a segment-activity match
func CacheSegmentActivityMetrics(ctx context.Context, conn *pgx.Conn, segmentID, activityID int64, toleranceMeters float64, startIndex, endIndex int, avgHR, avgSpeed, distanceM, elevationGainM float64) error {
	_, err := conn.Exec(ctx, `
		UPDATE segment_activity_matches
		SET start_index = $1,
			end_index = $2,
			avg_hr = $3,
			avg_speed = $4,
			distance_m = $5,
			elevation_gain_m = $6,
			cached_at = NOW()
		WHERE segment_id = $7 AND activity_id = $8 AND tolerance_meters = $9
	`, startIndex, endIndex, avgHR, avgSpeed, distanceM, elevationGainM, segmentID, activityID, toleranceMeters)
	if err != nil {
		// If update didn't affect any rows, try insert (match might not exist yet)
		_, err = conn.Exec(ctx, `
			INSERT INTO segment_activity_matches 
			(segment_id, activity_id, tolerance_meters, min_distance_m, overlap_length_m, overlap_percentage,
			 start_index, end_index, avg_hr, avg_speed, distance_m, elevation_gain_m, cached_at)
			VALUES ($7, $8, $9, 0, 0, 0, $1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (segment_id, activity_id, tolerance_meters) 
			DO UPDATE SET 
				start_index = EXCLUDED.start_index,
				end_index = EXCLUDED.end_index,
				avg_hr = EXCLUDED.avg_hr,
				avg_speed = EXCLUDED.avg_speed,
				distance_m = EXCLUDED.distance_m,
				elevation_gain_m = EXCLUDED.elevation_gain_m,
				cached_at = NOW()
		`, startIndex, endIndex, avgHR, avgSpeed, distanceM, elevationGainM, segmentID, activityID, toleranceMeters)
		if err != nil {
			return fmt.Errorf("failed to cache metrics: %w", err)
		}
	}
	return nil
}

// GetCachedSegmentActivityMetrics retrieves cached metrics for a segment-activity match
func GetCachedSegmentActivityMetrics(ctx context.Context, conn *pgx.Conn, segmentID, activityID int64, toleranceMeters float64) (*SegmentActivityCacheEntry, error) {
	var entry SegmentActivityCacheEntry
	err := conn.QueryRow(ctx, `
		SELECT segment_id, activity_id, tolerance_meters, min_distance_m, overlap_length_m, overlap_percentage,
			start_index, end_index, avg_hr, avg_speed, distance_m, elevation_gain_m
		FROM segment_activity_matches
		WHERE segment_id = $1 AND activity_id = $2 AND tolerance_meters = $3
	`, segmentID, activityID, toleranceMeters).Scan(
		&entry.SegmentID, &entry.ActivityID, &entry.ToleranceMeters,
		&entry.MinDistanceM, &entry.OverlapLengthM, &entry.OverlapPercentage,
		&entry.StartIndex, &entry.EndIndex, &entry.AvgHR, &entry.AvgSpeed,
		&entry.DistanceM, &entry.ElevationGainM,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cached metrics: %w", err)
	}
	return &entry, nil
}

// InvalidateSegmentCache invalidates cached matches for a segment
func InvalidateSegmentCache(ctx context.Context, conn *pgx.Conn, segmentID int64) error {
	_, err := conn.Exec(ctx, `
		DELETE FROM segment_activity_matches
		WHERE segment_id = $1
	`, segmentID)
	return err
}

// InvalidateActivityCache invalidates cached matches for an activity
func InvalidateActivityCache(ctx context.Context, conn *pgx.Conn, activityID int64) error {
	_, err := conn.Exec(ctx, `
		DELETE FROM segment_activity_matches
		WHERE activity_id = $1
	`, activityID)
	return err
}
