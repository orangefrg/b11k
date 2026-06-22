package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"b11k/internal/pggeo"

	"github.com/jackc/pgx/v5"
)

type mobileSegmentSummary struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	CreatedAt     string  `json:"created_at"`
	DistanceLabel string  `json:"distance_label"`
	NetRiseLabel  string  `json:"net_rise_label"`
	AscentLabel   string  `json:"ascent_label"`
	SlopeLabel    string  `json:"slope_label"`
	Direction     string  `json:"direction"`
	DirectionKey  string  `json:"direction_key"`
	Attempts      int     `json:"attempts"`
	MinTimeLabel  string  `json:"min_time_label"`
	MaxTimeLabel  string  `json:"max_time_label"`
	MinHRLabel    string  `json:"min_hr_label"`
	MaxHRLabel    string  `json:"max_hr_label"`
}

type mobileSegment struct {
	ID             int64                  `json:"id"`
	Name           string                 `json:"name"`
	Description    *string                `json:"description,omitempty"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
	DistanceMeters *float64               `json:"distance_meters,omitempty"`
	ElevationGainM *float64               `json:"elevation_gain_m,omitempty"`
	ElevationLossM *float64               `json:"elevation_loss_m,omitempty"`
	NetElevationM  *float64               `json:"net_elevation_m,omitempty"`
	SlopePercent   *float64               `json:"slope_percent,omitempty"`
	Direction      string                 `json:"direction"`
	DirectionKey   string                 `json:"direction_key"`
	Geometry       *mobileSegmentGeometry `json:"geometry,omitempty"`
	SegmentGeog    string                 `json:"segment_geog,omitempty"`
	SimplifiedGeog *string                `json:"segment_geog_simplified,omitempty"`
}

type mobileSegmentEffort struct {
	Activity           mobileActivity `json:"activity"`
	MinDistanceM       float64        `json:"min_distance_m"`
	OverlapLengthM     float64        `json:"overlap_length_m"`
	OverlapPercentage  float64        `json:"overlap_percentage"`
	SegmentAvgHR       *float64       `json:"segment_avg_hr,omitempty"`
	SegmentAvgSpeed    *float64       `json:"segment_avg_speed,omitempty"`
	SegmentDistance    *float64       `json:"segment_distance,omitempty"`
	SegmentElevation   *float64       `json:"segment_elevation_gain,omitempty"`
	SegmentElapsedSecs *float64       `json:"segment_elapsed_seconds,omitempty"`
}

type mobileSegmentEffortDetail struct {
	SegmentID  int64                      `json:"segment_id"`
	ActivityID int64                      `json:"activity_id"`
	Tolerance  float64                    `json:"tolerance"`
	StartIndex int                        `json:"start_index"`
	EndIndex   int                        `json:"end_index"`
	Activity   mobileActivity             `json:"activity"`
	Metrics    mobileSegmentEffortMetrics `json:"metrics"`
	Points     []mobileRoutePoint         `json:"points"`
}

type mobileSegmentEffortMetrics struct {
	AvgHR          float64 `json:"avg_hr"`
	AvgSpeed       float64 `json:"avg_speed"`
	Distance       float64 `json:"distance"`
	ElevationGain  float64 `json:"elevation_gain"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
}

type mobileSegmentGeometry struct {
	Type        string         `json:"type"`
	Coordinates [][]float64    `json:"coordinates"`
	Points      []mobileLatLng `json:"points"`
}

type mobileLatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type mobileSegmentCreateRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	ActivityID  int64          `json:"activity_id"`
	StartIndex  int            `json:"start_index"`
	EndIndex    int            `json:"end_index"`
	Points      []mobileLatLng `json:"points"`
	Coordinates [][]float64    `json:"coordinates"`
	LatLng      [][]float64    `json:"lat_lng"`
	LatLngData  [][]float64    `json:"lat_lng_data"`
}

type mobileSegmentUpdateRequest struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Points      []mobileLatLng `json:"points"`
	Coordinates [][]float64    `json:"coordinates"`
	LatLng      [][]float64    `json:"lat_lng"`
	LatLngData  [][]float64    `json:"lat_lng_data"`
}

func (s *server) handleMobileSegments(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	scope := s.mobileScopeFromSession(session)
	if scope.AthleteID == 0 {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	pathText := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mobile/segments"), "/")
	if pathText != "" {
		if unescaped, err := url.PathUnescape(pathText); err == nil {
			pathText = unescaped
		}
		s.handleMobileSegmentPath(w, r, scope, pathText)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleMobileSegmentsList(w, r, scope)
	case http.MethodPost:
		s.handleMobileSegmentCreate(w, r, scope)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleMobileSegmentsList(w http.ResponseWriter, r *http.Request, scope athleteScope) {
	tolerance := floatQueryValue(r, "tolerance", 15.0)
	summaries, err := s.listSegmentDashboardSummaries(scope.AthleteID, tolerance)
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	segments := mobileSegmentSummariesFromDashboard(summaries)
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		segments = filterMobileSegmentSummaries(segments, q)
	}
	writeJSON(w, map[string]interface{}{
		"count":    len(segments),
		"segments": segments,
	})
}

func (s *server) handleMobileSegmentCreate(w http.ResponseWriter, r *http.Request, scope athleteScope) {
	var req mobileSegmentCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	latLngData, hasPoints, err := mobileLatLngData(req.Points, req.Coordinates, req.LatLng, req.LatLngData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var segment *pggeo.FavoriteSegment
	if hasPoints {
		segment, err = s.createFavoriteSegmentFromPoints(scope.AthleteID, name, req.Description, latLngData)
	} else {
		if req.ActivityID <= 0 {
			http.Error(w, "activity_id is required when points are not provided", http.StatusBadRequest)
			return
		}
		if req.StartIndex < 0 || req.EndIndex < 0 || req.StartIndex >= req.EndIndex {
			http.Error(w, "invalid start_index or end_index", http.StatusBadRequest)
			return
		}
		segment, err = s.createFavoriteSegmentFromActivityRange(scope.AthleteID, req.ActivityID, name, req.Description, req.StartIndex, req.EndIndex)
	}
	if err != nil {
		s.handleMobileSegmentMutationError(w, r, err)
		return
	}

	response, err := s.mobileSegmentFromFavorite(scope.AthleteID, segment, true)
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"segment": response})
}

func (s *server) handleMobileSegmentPath(w http.ResponseWriter, r *http.Request, scope athleteScope, pathText string) {
	parts := strings.Split(pathText, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	segmentID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid segment id", http.StatusBadRequest)
		return
	}
	segment, err := s.getOwnedFavoriteSegment(scope.AthleteID, segmentID)
	if err != nil {
		s.handleOwnedMobileSegmentError(w, r, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if len(parts) == 1 {
			response, err := s.mobileSegmentFromFavorite(scope.AthleteID, segment, true)
			if err != nil {
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]interface{}{"segment": response})
			return
		}
		if len(parts) == 2 && parts[1] == "geometry" {
			geometry, err := s.mobileSegmentGeometry(scope.AthleteID, segmentID)
			if err != nil {
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]interface{}{
				"segment_id": segmentID,
				"geometry":   geometry,
			})
			return
		}
		if len(parts) == 2 && parts[1] == "activities" {
			s.handleMobileSegmentActivities(w, r, scope, segmentID)
			return
		}
		if len(parts) == 3 && parts[1] == "activities" {
			activityID, err := strconv.ParseInt(parts[2], 10, 64)
			if err != nil {
				http.Error(w, "invalid activity id", http.StatusBadRequest)
				return
			}
			s.handleMobileSegmentActivityDetail(w, r, scope, segmentID, activityID)
			return
		}
		http.NotFound(w, r)
	case http.MethodPut, http.MethodPatch:
		if len(parts) != 1 {
			http.NotFound(w, r)
			return
		}
		s.handleMobileSegmentUpdate(w, r, scope, segment)
	case http.MethodDelete:
		if len(parts) != 1 {
			http.NotFound(w, r)
			return
		}
		if err := s.withDB(func(conn *pgx.Conn) error {
			return pggeo.DeleteFavoriteSegment(s.ctx, conn, segmentID)
		}); err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleMobileSegmentActivityDetail(w http.ResponseWriter, r *http.Request, scope athleteScope, segmentID, activityID int64) {
	tolerance := floatQueryValue(r, "tolerance", 15.0)

	var activity *pggeo.ActivityWithMatch
	err := s.withDB(func(conn *pgx.Conn) error {
		efforts, dbErr := pggeo.GetActivitiesForSegment(s.ctx, conn, scope.AthleteID, segmentID, tolerance, "total_time", false)
		if dbErr != nil {
			return dbErr
		}
		for i := range efforts {
			if efforts[i].ID == activityID {
				activity = &efforts[i]
				return nil
			}
		}
		return pgx.ErrNoRows
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "segment effort not found", http.StatusNotFound)
			return
		}
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}

	detail, err := s.mobileSegmentEffortDetail(scope.AthleteID, segmentID, activityID, tolerance, *activity)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "segment effort not found", http.StatusNotFound)
			return
		}
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, detail)
}

func (s *server) handleMobileSegmentActivities(w http.ResponseWriter, r *http.Request, scope athleteScope, segmentID int64) {
	tolerance := floatQueryValue(r, "tolerance", 15.0)
	sortBy := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortBy == "" {
		sortBy = "total_time"
	}
	forceRefresh := r.URL.Query().Get("refresh") == "true"

	var activities []pggeo.ActivityWithMatch
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activities, dbErr = pggeo.GetActivitiesForSegment(s.ctx, conn, scope.AthleteID, segmentID, tolerance, sortBy, forceRefresh)
		return dbErr
	})
	if err != nil {
		log.Printf("❌ Failed to load mobile activities for segment %d: %v", segmentID, err)
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"segment_id": segmentID,
		"count":      len(activities),
		"tolerance":  tolerance,
		"sort":       sortBy,
		"activities": mobileSegmentEffortsFromActivities(activities),
	})
}

func (s *server) mobileSegmentEffortDetail(athleteID, segmentID, activityID int64, tolerance float64, activity pggeo.ActivityWithMatch) (mobileSegmentEffortDetail, error) {
	startIndex, endIndex, metrics, err := s.mobileSegmentEffortMetrics(athleteID, segmentID, activityID, tolerance)
	if err != nil {
		return mobileSegmentEffortDetail{}, err
	}

	var samples []pggeo.PointSample
	err = s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		samples, dbErr = pggeo.GetPointSamplesForActivity(s.ctx, conn, athleteID, activityID)
		return dbErr
	})
	if err != nil {
		return mobileSegmentEffortDetail{}, err
	}
	segmentSamples := pointSamplesInIndexRange(samples, startIndex, endIndex)
	if len(segmentSamples) == 0 {
		return mobileSegmentEffortDetail{}, pgx.ErrNoRows
	}

	return mobileSegmentEffortDetail{
		SegmentID:  segmentID,
		ActivityID: activityID,
		Tolerance:  tolerance,
		StartIndex: startIndex,
		EndIndex:   endIndex,
		Activity:   mobileActivityFromSummary(activity.ActivitySummary),
		Metrics:    metrics,
		Points:     mobileRoutePointsFromSamples(segmentSamples),
	}, nil
}

func (s *server) mobileSegmentEffortMetrics(athleteID, segmentID, activityID int64, tolerance float64) (int, int, mobileSegmentEffortMetrics, error) {
	var cached *pggeo.SegmentActivityCacheEntry
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		cached, dbErr = pggeo.GetCachedSegmentActivityMetrics(s.ctx, conn, segmentID, activityID, tolerance)
		return dbErr
	})
	if err != nil {
		return 0, 0, mobileSegmentEffortMetrics{}, err
	}
	if cached != nil && cached.StartIndex != nil && cached.EndIndex != nil &&
		cached.AvgHR != nil && cached.AvgSpeed != nil && cached.DistanceM != nil &&
		cached.ElevationGainM != nil && cached.ElapsedSeconds != nil {
		return *cached.StartIndex, *cached.EndIndex, mobileSegmentEffortMetrics{
			AvgHR:          *cached.AvgHR,
			AvgSpeed:       *cached.AvgSpeed,
			Distance:       *cached.DistanceM,
			ElevationGain:  *cached.ElevationGainM,
			ElapsedSeconds: *cached.ElapsedSeconds,
		}, nil
	}

	var startIndex, endIndex int
	var avgHR, avgSpeed, distanceM, elevationGainM, elapsedSeconds float64
	err = s.withDB(func(conn *pgx.Conn) error {
		if err := conn.QueryRow(s.ctx,
			`SELECT * FROM find_segment_point_indices($1, $2, $3, $4)`,
			segmentID, activityID, athleteID, tolerance,
		).Scan(&startIndex, &endIndex); err != nil {
			return err
		}
		if err := conn.QueryRow(s.ctx,
			`SELECT * FROM get_activity_segment_metrics($1, $2, $3, $4)`,
			segmentID, activityID, athleteID, tolerance,
		).Scan(&avgHR, &avgSpeed, &distanceM, &elevationGainM, &elapsedSeconds); err != nil {
			return err
		}
		return pggeo.CacheSegmentActivityMetrics(s.ctx, conn, segmentID, activityID, tolerance, startIndex, endIndex, avgHR, avgSpeed, distanceM, elevationGainM, elapsedSeconds)
	})
	if err != nil {
		return 0, 0, mobileSegmentEffortMetrics{}, err
	}

	return startIndex, endIndex, mobileSegmentEffortMetrics{
		AvgHR:          avgHR,
		AvgSpeed:       avgSpeed,
		Distance:       distanceM,
		ElevationGain:  elevationGainM,
		ElapsedSeconds: elapsedSeconds,
	}, nil
}

func (s *server) handleMobileSegmentUpdate(w http.ResponseWriter, r *http.Request, scope athleteScope, segment *pggeo.FavoriteSegment) {
	var req mobileSegmentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	name := segment.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	description := ""
	if segment.Description != nil {
		description = *segment.Description
	}
	if req.Description != nil {
		description = *req.Description
	}

	latLngData, hasPoints, err := mobileLatLngData(req.Points, req.Coordinates, req.LatLng, req.LatLngData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !hasPoints {
		geometry, err := s.mobileSegmentGeometry(scope.AthleteID, segment.ID)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		latLngData = latLngDataFromMobilePoints(geometry.Points)
	}

	updated, err := s.updateOwnedFavoriteSegment(scope.AthleteID, segment.ID, name, description, latLngData)
	if err != nil {
		s.handleMobileSegmentMutationError(w, r, err)
		return
	}

	response, err := s.mobileSegmentFromFavorite(scope.AthleteID, updated, true)
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"segment": response})
}

func pointSamplesInIndexRange(samples []pggeo.PointSample, startIndex, endIndex int) []pggeo.PointSample {
	result := make([]pggeo.PointSample, 0, max(0, endIndex-startIndex+1))
	for _, sample := range samples {
		if sample.PointIndex >= startIndex && sample.PointIndex <= endIndex {
			result = append(result, sample)
		}
	}
	return result
}

func mobileSegmentEffortsFromActivities(activities []pggeo.ActivityWithMatch) []mobileSegmentEffort {
	result := make([]mobileSegmentEffort, 0, len(activities))
	for _, activity := range activities {
		result = append(result, mobileSegmentEffort{
			Activity:           mobileActivityFromSummary(activity.ActivitySummary),
			MinDistanceM:       activity.MinDistanceM,
			OverlapLengthM:     activity.OverlapLengthM,
			OverlapPercentage:  activity.OverlapPercentage,
			SegmentAvgHR:       activity.SegmentAvgHR,
			SegmentAvgSpeed:    activity.SegmentAvgSpeed,
			SegmentDistance:    activity.SegmentDistance,
			SegmentElevation:   activity.SegmentElevation,
			SegmentElapsedSecs: activity.SegmentElapsedSecs,
		})
	}
	return result
}

func (s *server) mobileSegmentFromFavorite(athleteID int64, segment *pggeo.FavoriteSegment, includeGeometry bool) (mobileSegment, error) {
	distance, err := s.segmentDistanceMeters(athleteID, segment.ID)
	if err != nil {
		return mobileSegment{}, err
	}

	result := mobileSegment{
		ID:             segment.ID,
		Name:           segment.Name,
		Description:    segment.Description,
		CreatedAt:      segment.CreatedAt,
		UpdatedAt:      segment.UpdatedAt,
		DistanceMeters: &distance,
		ElevationGainM: segment.ElevationGainM,
		ElevationLossM: segment.ElevationLossM,
		NetElevationM:  segment.NetElevationM,
		SegmentGeog:    segment.SegmentGeog,
		SimplifiedGeog: segment.SegmentGeogSimplified,
		Direction:      "Unknown",
		DirectionKey:   "unknown",
	}

	if segment.NetElevationM != nil && distance > 0 {
		slope := (*segment.NetElevationM / distance) * 100
		result.SlopePercent = &slope
		switch {
		case slope > 2:
			result.Direction = "Uphill"
			result.DirectionKey = "uphill"
		case slope < -2:
			result.Direction = "Downhill"
			result.DirectionKey = "downhill"
		default:
			result.Direction = "Flat"
			result.DirectionKey = "flat"
		}
	}

	if includeGeometry {
		geometry, err := s.mobileSegmentGeometry(athleteID, segment.ID)
		if err != nil {
			return mobileSegment{}, err
		}
		result.Geometry = &geometry
	}

	return result, nil
}

func (s *server) mobileSegmentGeometry(athleteID, segmentID int64) (mobileSegmentGeometry, error) {
	var geoJSONText string
	err := s.withDB(func(conn *pgx.Conn) error {
		return conn.QueryRow(s.ctx, `
			SELECT ST_AsGeoJSON(segment_geog::geometry)
			FROM favorite_segments
			WHERE id = $1 AND athlete_id = $2
		`, segmentID, athleteID).Scan(&geoJSONText)
	})
	if err != nil {
		return mobileSegmentGeometry{}, err
	}
	return parseMobileSegmentGeometry(geoJSONText)
}

func (s *server) segmentDistanceMeters(athleteID, segmentID int64) (float64, error) {
	var distance float64
	err := s.withDB(func(conn *pgx.Conn) error {
		return conn.QueryRow(s.ctx, `
			SELECT ST_Length(segment_geog)
			FROM favorite_segments
			WHERE id = $1 AND athlete_id = $2
		`, segmentID, athleteID).Scan(&distance)
	})
	return distance, err
}

func mobileSegmentSummariesFromDashboard(summaries []pggeo.SegmentDashboardSummary) []mobileSegmentSummary {
	result := make([]mobileSegmentSummary, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, mobileSegmentSummary{
			ID:            summary.ID,
			Name:          summary.Name,
			Description:   summary.Description,
			CreatedAt:     summary.CreatedAt,
			DistanceLabel: summary.DistanceLabel,
			NetRiseLabel:  summary.NetRiseLabel,
			AscentLabel:   summary.AscentLabel,
			SlopeLabel:    summary.SlopeLabel,
			Direction:     summary.Direction,
			DirectionKey:  summary.DirectionKey,
			Attempts:      summary.Attempts,
			MinTimeLabel:  summary.MinTimeLabel,
			MaxTimeLabel:  summary.MaxTimeLabel,
			MinHRLabel:    summary.MinHRLabel,
			MaxHRLabel:    summary.MaxHRLabel,
		})
	}
	return result
}

func filterMobileSegmentSummaries(segments []mobileSegmentSummary, query string) []mobileSegmentSummary {
	filtered := make([]mobileSegmentSummary, 0, len(segments))
	for _, segment := range segments {
		description := ""
		if segment.Description != nil {
			description = *segment.Description
		}
		haystack := strings.ToLower(segment.Name + " " + description + " " + segment.Direction + " " + segment.DirectionKey)
		if strings.Contains(haystack, query) {
			filtered = append(filtered, segment)
		}
	}
	return filtered
}

func mobileLatLngData(points []mobileLatLng, coordinates, latLng, latLngData [][]float64) ([][]float64, bool, error) {
	switch {
	case len(points) > 0:
		data := latLngDataFromMobilePoints(points)
		return data, true, validateLatLngData(data)
	case len(latLng) > 0:
		return copyLatLngPairs(latLng, false)
	case len(latLngData) > 0:
		return copyLatLngPairs(latLngData, false)
	case len(coordinates) > 0:
		return copyLatLngPairs(coordinates, true)
	default:
		return nil, false, nil
	}
}

func latLngDataFromMobilePoints(points []mobileLatLng) [][]float64 {
	data := make([][]float64, 0, len(points))
	for _, point := range points {
		data = append(data, []float64{point.Lat, point.Lng})
	}
	return data
}

func copyLatLngPairs(values [][]float64, geoJSONOrder bool) ([][]float64, bool, error) {
	data := make([][]float64, 0, len(values))
	for _, value := range values {
		if len(value) < 2 {
			return nil, true, fmt.Errorf("each point must include two coordinates")
		}
		if geoJSONOrder {
			data = append(data, []float64{value[1], value[0]})
		} else {
			data = append(data, []float64{value[0], value[1]})
		}
	}
	return data, true, validateLatLngData(data)
}

func validateLatLngData(data [][]float64) error {
	if len(data) < 2 {
		return fmt.Errorf("at least two points are required")
	}
	for _, point := range data {
		if len(point) < 2 {
			return fmt.Errorf("each point must include latitude and longitude")
		}
		if err := validateLatLng(point[0], point[1]); err != nil {
			return err
		}
	}
	return nil
}

func validateLatLng(lat, lng float64) error {
	if math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) {
		return fmt.Errorf("coordinates must be finite")
	}
	if lat < -90 || lat > 90 {
		return fmt.Errorf("latitude must be between -90 and 90")
	}
	if lng < -180 || lng > 180 {
		return fmt.Errorf("longitude must be between -180 and 180")
	}
	return nil
}

func parseMobileSegmentGeometry(geoJSONText string) (mobileSegmentGeometry, error) {
	var raw struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(geoJSONText), &raw); err != nil {
		return mobileSegmentGeometry{}, fmt.Errorf("parse segment geometry: %w", err)
	}
	if !strings.EqualFold(raw.Type, "LineString") {
		return mobileSegmentGeometry{}, fmt.Errorf("segment geometry must be LineString, got %s", raw.Type)
	}

	var coordinates [][]float64
	if err := json.Unmarshal(raw.Coordinates, &coordinates); err != nil {
		return mobileSegmentGeometry{}, fmt.Errorf("parse segment coordinates: %w", err)
	}
	if len(coordinates) < 2 {
		return mobileSegmentGeometry{}, fmt.Errorf("segment geometry has fewer than two points")
	}

	points := make([]mobileLatLng, 0, len(coordinates))
	for _, coordinate := range coordinates {
		if len(coordinate) < 2 {
			return mobileSegmentGeometry{}, fmt.Errorf("segment coordinate must include longitude and latitude")
		}
		lng := coordinate[0]
		lat := coordinate[1]
		if err := validateLatLng(lat, lng); err != nil {
			return mobileSegmentGeometry{}, err
		}
		points = append(points, mobileLatLng{Lat: lat, Lng: lng})
	}

	return mobileSegmentGeometry{
		Type:        "LineString",
		Coordinates: coordinates,
		Points:      points,
	}, nil
}

func floatQueryValue(r *http.Request, name string, fallback float64) float64 {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
		return fallback
	}
	return parsed
}

func (s *server) handleOwnedMobileSegmentError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errForbidden) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "not found") {
		http.Error(w, "segment not found", http.StatusNotFound)
		return
	}
	s.handleDBPageError(w, r, err, http.StatusInternalServerError)
}

func (s *server) handleMobileSegmentMutationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errSegmentIndexOutOfRange) {
		http.Error(w, "index out of range", http.StatusBadRequest)
		return
	}
	if errors.Is(err, errActivitySamplesMissing) {
		s.handleDBPageError(w, r, err, http.StatusNotFound)
		return
	}
	s.handleOwnedMobileSegmentError(w, r, err)
}
