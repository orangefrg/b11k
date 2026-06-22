package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	"b11k/internal/sync"

	"github.com/jackc/pgx/v5"
)

const (
	mobileAuthDeniedMessage = "Strava authorization was not completed."
	mobileAuthFailedMessage = "Strava login could not be completed. Return to B11K and try again."
)

func (s *server) handleMobileAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.StravaClientID == "" || s.cfg.StravaClientSecret == "" {
		http.Error(w, "Strava client is not configured", http.StatusServiceUnavailable)
		return
	}

	state, err := randomURLToken(24)
	if err != nil {
		http.Error(w, "failed to create auth state", http.StatusInternalServerError)
		return
	}

	s.mobileMu.Lock()
	s.mobileAuthStates[state] = time.Now().Add(10 * time.Minute)
	s.mobileMu.Unlock()

	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.IOSRedirectURI)
	writeJSON(w, map[string]string{
		"state":        state,
		"redirect_uri": s.cfg.IOSRedirectURI,
		"app_auth_url": strava.GenerateMobileAppAuthURL(*authCfg, state),
		"web_auth_url": strava.GenerateMobileAuthURL(*authCfg, state),
	})
}

func (s *server) handleMobileAuthExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Code  string `json:"code"`
		State string `json:"state"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.State) == "" {
		http.Error(w, "code and state are required", http.StatusBadRequest)
		return
	}
	if !s.consumeMobileAuthState(req.State) {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.IOSRedirectURI)
	tokenResp, err := strava.ExchangeCodeForTokenResponse(*authCfg, req.Code)
	if err != nil {
		log.Printf("mobile token exchange failed: %v", err)
		http.Error(w, mobileAuthFailedMessage, http.StatusBadGateway)
		return
	}
	athlete, err := strava.FetchCurrentAthlete(tokenResp.AccessToken)
	if err != nil {
		log.Printf("mobile athlete fetch failed: %v", err)
		http.Error(w, mobileAuthFailedMessage, http.StatusBadGateway)
		return
	}

	session, err := s.createMobileSession(tokenResp, athlete)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"session_token": session.SessionToken,
		"athlete":       athlete,
	})
}

func (s *server) handleMobileAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	accessError := strings.TrimSpace(r.URL.Query().Get("error"))
	if state == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	if accessError != "" {
		if !s.consumeMobileAuthState(state) {
			s.renderMobileAuthCallbackPage(w, "This login link expired.", "Return to B11K and start Strava login again.")
			return
		}
		s.storeMobileAuthResult(state, mobileAuthResult{
			Error:     mobileAuthDeniedMessage,
			ExpiresAt: time.Now().Add(10 * time.Minute),
		})
		s.renderMobileAuthCallbackPage(w, mobileAuthDeniedMessage, "Return to B11K and start Strava login again.")
		return
	}
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	if !s.consumeMobileAuthState(state) {
		s.storeMobileAuthResult(state, mobileAuthResult{
			Error:     "invalid or expired state",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		})
		s.renderMobileAuthCallbackPage(w, "This login link expired.", "Return to B11K and start Strava login again.")
		return
	}

	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.IOSRedirectURI)
	tokenResp, err := strava.ExchangeCodeForTokenResponse(*authCfg, code)
	if err != nil {
		log.Printf("mobile token exchange failed: %v", err)
		s.storeMobileAuthResult(state, mobileAuthResult{
			Error:     mobileAuthFailedMessage,
			ExpiresAt: time.Now().Add(10 * time.Minute),
		})
		s.renderMobileAuthCallbackPage(w, "B11K could not finish Strava login.", mobileAuthFailedMessage)
		return
	}
	athlete, err := strava.FetchCurrentAthlete(tokenResp.AccessToken)
	if err != nil {
		log.Printf("mobile athlete fetch failed: %v", err)
		s.storeMobileAuthResult(state, mobileAuthResult{
			Error:     mobileAuthFailedMessage,
			ExpiresAt: time.Now().Add(10 * time.Minute),
		})
		s.renderMobileAuthCallbackPage(w, "B11K could not load your Strava profile.", mobileAuthFailedMessage)
		return
	}

	session, err := s.createMobileSession(tokenResp, athlete)
	if err != nil {
		msg := "failed to create session"
		s.storeMobileAuthResult(state, mobileAuthResult{
			Error:     msg,
			ExpiresAt: time.Now().Add(10 * time.Minute),
		})
		s.renderMobileAuthCallbackPage(w, "B11K could not create a mobile session.", msg)
		return
	}

	s.mobileMu.Lock()
	s.mobileAuthResults[state] = mobileAuthResult{
		SessionToken: session.SessionToken,
		Athlete:      athlete,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}
	s.mobileMu.Unlock()

	s.renderMobileAuthCallbackPage(w, "Strava connected.", "Return to B11K and tap Check Login.")
}

func (s *server) handleMobileAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		http.Error(w, "state is required", http.StatusBadRequest)
		return
	}

	s.mobileMu.Lock()
	result, ok := s.mobileAuthResults[state]
	if ok && time.Now().After(result.ExpiresAt) {
		delete(s.mobileAuthResults, state)
		ok = false
	}
	if ok && result.SessionToken != "" {
		delete(s.mobileAuthResults, state)
	}
	s.mobileMu.Unlock()

	if !ok {
		writeJSON(w, map[string]interface{}{
			"status": "pending",
		})
		return
	}
	if result.Error != "" {
		writeJSON(w, map[string]interface{}{
			"status": "error",
			"error":  result.Error,
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"status":        "ready",
		"session_token": result.SessionToken,
		"athlete":       result.Athlete,
	})
}

func (s *server) handleMobileMe(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]interface{}{
		"athlete": session.Athlete,
	})
}

func (s *server) handleMobileActivities(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if pathText := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mobile/activities"), "/"); pathText != "" {
		s.handleMobileActivityPath(w, r, session, pathText)
		return
	}

	var activities []strava.ActivitySummary
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activities, dbErr = pggeo.GetAllActivities(s.ctx, conn, session.Athlete.ID)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}

	activities = filterMobileActivities(activities, r)

	page := intQueryValue(r, "page", 1)
	perPage := intQueryValue(r, "per_page", 100)
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 100
	}
	if perPage > 200 {
		perPage = 200
	}
	start := (page - 1) * perPage
	end := start + perPage
	pagedActivities := []strava.ActivitySummary{}
	if start < len(activities) {
		if end > len(activities) {
			end = len(activities)
		}
		pagedActivities = activities[start:end]
	}

	writeJSON(w, map[string]interface{}{
		"count":      len(activities),
		"page":       page,
		"per_page":   perPage,
		"has_more":   end < len(activities),
		"activities": mobileActivitiesFromSummaries(pagedActivities),
	})
}

func (s *server) handleMobileActivityPath(w http.ResponseWriter, r *http.Request, session mobileSession, pathText string) {
	if unescaped, err := url.PathUnescape(pathText); err == nil {
		pathText = unescaped
	}
	parts := strings.Split(pathText, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "route" {
		s.handleMobileActivityRoute(w, r, session, parts[0])
		return
	}
	if len(parts) == 1 {
		s.handleMobileActivity(w, r, session, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *server) handleMobileActivity(w http.ResponseWriter, r *http.Request, session mobileSession, idText string) {
	activityID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.Error(w, "invalid activity id", http.StatusBadRequest)
		return
	}

	var activity *strava.ActivitySummary
	err = s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activity, dbErr = pggeo.GetActivityByID(s.ctx, conn, session.Athlete.ID, activityID)
		return dbErr
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "activity not found", http.StatusNotFound)
			return
		}
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"activity": mobileActivityFromSummary(*activity),
	})
}

func (s *server) handleMobileActivityRoute(w http.ResponseWriter, r *http.Request, session mobileSession, idText string) {
	activityID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.Error(w, "invalid activity id", http.StatusBadRequest)
		return
	}

	var samples []pggeo.PointSample
	err = s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		samples, dbErr = pggeo.GetPointSamplesForActivity(s.ctx, conn, session.Athlete.ID, activityID)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	source := "point_samples"
	if len(samples) == 0 {
		err = s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			samples, dbErr = pggeo.GetRoutePointsForActivity(s.ctx, conn, session.Athlete.ID, activityID)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		source = "activity_geometries"
		if len(samples) == 0 {
			source = "none"
		}
	}

	writeJSON(w, map[string]interface{}{
		"activity_id": activityID,
		"count":       len(samples),
		"source":      source,
		"points":      mobileRoutePointsFromSamples(samples),
	})
}

func (s *server) handleMobileSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}

	startTime, endTime := mobileSyncTimeframeFromRequest(r)

	logs := make([]string, 0, 32)
	progressCallback := func(phase string, current, total int, message string) {
		if total > 0 {
			logs = append(logs, fmt.Sprintf("%s: %s (%d/%d)", phase, message, current, total))
			return
		}
		logs = append(logs, fmt.Sprintf("%s: %s", phase, message))
	}

	result, err := sync.SyncActivitiesFromStravaWithRetry(s.ctx, s.mobileSyncConfig(session, startTime, endTime), 3, progressCallback)
	if err != nil {
		http.Error(w, fmt.Sprintf("sync failed: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, map[string]interface{}{
		"summary": mobileSyncSummary(result),
		"logs":    logs,
	})
}

func mobileSyncTimeframeFromRequest(r *http.Request) (time.Time, time.Time) {
	q := r.URL.Query()
	var startTime time.Time
	var endTime time.Time
	if startStr := q.Get("start"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			startTime = t
		}
	}
	if endStr := q.Get("end"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			endTime = t
		}
	}
	return startTime, endTime
}

func (s *server) mobileSyncConfig(session mobileSession, startTime, endTime time.Time) sync.SyncConfig {
	return sync.SyncConfig{
		StravaAccessToken: session.Token,
		DatabaseConfig: sync.DatabaseConfig{
			Host:     s.cfg.PGIP,
			Port:     s.cfg.PGPort,
			User:     s.cfg.PGUser,
			Password: s.cfg.PGPassword,
			Database: s.cfg.PGDatabase,
		},
		Timeframe: sync.TimeframeConfig{
			StartTime: startTime,
			EndTime:   endTime,
		},
		DiscoveredMap: sync.DiscoveredMapConfig{
			Enabled:              s.cfg.DiscoveredMapEnabled,
			RevealRadiusMeters:   s.cfg.DiscoveredRevealRadiusMeters,
			SampleDistanceMeters: s.cfg.DiscoveredSampleDistanceMeters,
		},
	}
}

func mobileSyncSummary(result *sync.SyncResult) map[string]int {
	return map[string]int{
		"total":    result.TotalActivitiesFound,
		"existing": result.ExistingActivities,
		"new":      result.NewActivities,
		"success":  result.SuccessfullyProcessed,
		"failed":   len(result.FailedActivities),
	}
}

type mobileStorageStats struct {
	Activities                 int `json:"activities"`
	ActivityGeometries         int `json:"activity_geometries"`
	PointSamples               int `json:"point_samples"`
	ActivitiesWithPointSamples int `json:"activities_with_point_samples"`
	ActivitiesWithGeometry     int `json:"activities_with_geometry"`
}

func (s *server) mobileStorageStats(athleteID int64) (mobileStorageStats, error) {
	var stats mobileStorageStats
	err := s.withDB(func(conn *pgx.Conn) error {
		return conn.QueryRow(s.ctx, `
			SELECT
				(SELECT COUNT(*) FROM activity_summaries WHERE athlete_id = $1),
				(SELECT COUNT(*) FROM activity_geometries WHERE athlete_id = $1),
				(SELECT COUNT(*) FROM point_samples WHERE athlete_id = $1),
				(SELECT COUNT(DISTINCT activity_id) FROM point_samples WHERE athlete_id = $1),
				(SELECT COUNT(DISTINCT activity_id) FROM activity_geometries WHERE athlete_id = $1)
		`, athleteID).Scan(
			&stats.Activities,
			&stats.ActivityGeometries,
			&stats.PointSamples,
			&stats.ActivitiesWithPointSamples,
			&stats.ActivitiesWithGeometry,
		)
	})
	return stats, err
}

type mobileActivity struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Type               string     `json:"type"`
	SportType          string     `json:"sport_type"`
	StartDate          string     `json:"start_date"`
	Distance           float64    `json:"distance"`
	MovingTime         float64    `json:"moving_time"`
	ElapsedTime        float64    `json:"elapsed_time"`
	TotalElevationGain float64    `json:"total_elevation_gain"`
	LocationCity       *string    `json:"location_city"`
	LocationState      *string    `json:"location_state"`
	LocationCountry    *string    `json:"location_country"`
	GearID             string     `json:"gear_id"`
	GearName           *string    `json:"gear_name,omitempty"`
	AverageSpeed       float64    `json:"average_speed"`
	MaxSpeed           float64    `json:"max_speed"`
	AverageCadence     float64    `json:"average_cadence"`
	AverageWatts       float64    `json:"average_watts"`
	Kilojoules         float64    `json:"kilojoules"`
	AverageHeartrate   float64    `json:"average_heartrate"`
	MaxHeartrate       float64    `json:"max_heartrate"`
	MaxWatts           float64    `json:"max_watts"`
	SufferScore        float64    `json:"suffer_score"`
	StartLatLng        *[]float64 `json:"start_latlng"`
	EndLatLng          *[]float64 `json:"end_latlng"`
}

type mobileRoutePoint struct {
	Index              int      `json:"index"`
	Lat                float64  `json:"lat"`
	Lng                float64  `json:"lng"`
	Altitude           *float64 `json:"altitude,omitempty"`
	Heartrate          *int     `json:"heartrate,omitempty"`
	Speed              *float64 `json:"speed,omitempty"`
	Watts              *int     `json:"watts,omitempty"`
	Cadence            *int     `json:"cadence,omitempty"`
	Grade              *float64 `json:"grade,omitempty"`
	Moving             *bool    `json:"moving,omitempty"`
	Temperature        *int     `json:"temperature,omitempty"`
	CumulativeDistance *float64 `json:"cumulative_distance,omitempty"`
}

func filterMobileActivities(activities []strava.ActivitySummary, r *http.Request) []strava.ActivitySummary {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	sport := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sport")))
	if query == "" && sport == "" {
		return activities
	}

	filtered := make([]strava.ActivitySummary, 0, len(activities))
	for _, activity := range activities {
		if sport != "" {
			activitySport := strings.ToLower(firstNonEmpty(activity.SportType, activity.Type))
			if activitySport != sport && !strings.Contains(activitySport, sport) {
				continue
			}
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				activity.Name,
				activity.Type,
				activity.SportType,
				stringPtrValue(activity.LocationCity),
				stringPtrValue(activity.LocationState),
				stringPtrValue(activity.LocationCountry),
				activity.GearID,
				stringPtrValue(activity.GearName),
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, activity)
	}
	return filtered
}

func mobileActivitiesFromSummaries(activities []strava.ActivitySummary) []mobileActivity {
	result := make([]mobileActivity, 0, len(activities))
	for _, activity := range activities {
		result = append(result, mobileActivityFromSummary(activity))
	}
	return result
}

func mobileActivityFromSummary(activity strava.ActivitySummary) mobileActivity {
	startDate := activity.StartDate
	if !activity.StartDateTime.IsZero() {
		startDate = activity.StartDateTime.Format(time.RFC3339)
	}
	return mobileActivity{
		ID:                 activity.ID,
		Name:               activity.Name,
		Type:               activity.Type,
		SportType:          activity.SportType,
		StartDate:          startDate,
		Distance:           activity.Distance,
		MovingTime:         activity.MovingTime,
		ElapsedTime:        activity.ElapsedTime,
		TotalElevationGain: activity.TotalElevationGain,
		LocationCity:       activity.LocationCity,
		LocationState:      activity.LocationState,
		LocationCountry:    activity.LocationCountry,
		GearID:             activity.GearID,
		GearName:           activity.GearName,
		AverageSpeed:       activity.AverageSpeed,
		MaxSpeed:           activity.MaxSpeed,
		AverageCadence:     activity.AverageCadence,
		AverageWatts:       activity.AverageWatts,
		Kilojoules:         activity.Kilojoules,
		AverageHeartrate:   activity.AverageHeartrate,
		MaxHeartrate:       activity.MaxHeartrate,
		MaxWatts:           activity.MaxWatts,
		SufferScore:        activity.SufferScore,
		StartLatLng:        activity.StartLatLng,
		EndLatLng:          activity.EndLatLng,
	}
}

func mobileRoutePointsFromSamples(samples []pggeo.PointSample) []mobileRoutePoint {
	points := make([]mobileRoutePoint, 0, len(samples))
	for _, sample := range samples {
		points = append(points, mobileRoutePoint{
			Index:              sample.PointIndex,
			Lat:                sample.Lat,
			Lng:                sample.Lng,
			Altitude:           sample.Altitude,
			Heartrate:          sample.Heartrate,
			Speed:              sample.Speed,
			Watts:              sample.Watts,
			Cadence:            sample.Cadence,
			Grade:              sample.Grade,
			Moving:             sample.Moving,
			Temperature:        sample.Temperature,
			CumulativeDistance: sample.CumulativeDistance,
		})
	}
	return points
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intQueryValue(r *http.Request, name string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (s *server) createMobileSession(tokenResp *strava.StravaTokenResponse, athlete *strava.Athlete) (mobileSession, error) {
	sessionToken, err := randomURLToken(32)
	if err != nil {
		return mobileSession{}, err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" || strings.TrimSpace(tokenResp.RefreshToken) == "" {
		return mobileSession{}, fmt.Errorf("Strava did not return complete token metadata")
	}
	session := mobileSession{
		SessionToken:     sessionToken,
		Token:            tokenResp.AccessToken,
		RefreshToken:     tokenResp.RefreshToken,
		ExpiresAt:        stravaTokenExpiry(tokenResp.ExpiresAt),
		SessionExpiresAt: time.Now().Add(mobileSessionLifetime),
		Athlete:          athlete,
		CreatedAt:        time.Now(),
	}
	if err := s.saveMobileSession(session); err != nil {
		return mobileSession{}, err
	}

	s.mobileMu.Lock()
	s.mobileSessions[sessionToken] = session
	s.mobileMu.Unlock()

	return session, nil
}

func (s *server) saveMobileSession(session mobileSession) error {
	if session.SessionExpiresAt.IsZero() {
		session.SessionExpiresAt = time.Now().Add(mobileSessionLifetime)
	}
	accessToken, err := s.encryptSecret(session.Token)
	if err != nil {
		return err
	}
	refreshToken, err := s.encryptSecret(session.RefreshToken)
	if err != nil {
		return err
	}
	return s.withDB(func(conn *pgx.Conn) error {
		_, err := conn.Exec(s.ctx, `
			INSERT INTO mobile_app_sessions (
				session_token, athlete_id, athlete_firstname, athlete_lastname, athlete_profile,
				strava_access_token, strava_refresh_token, strava_expires_at, session_expires_at,
				created_at, updated_at, last_seen_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), NOW())
			ON CONFLICT (session_token) DO UPDATE SET
				athlete_id = EXCLUDED.athlete_id,
				athlete_firstname = EXCLUDED.athlete_firstname,
				athlete_lastname = EXCLUDED.athlete_lastname,
				athlete_profile = EXCLUDED.athlete_profile,
				strava_access_token = EXCLUDED.strava_access_token,
				strava_refresh_token = EXCLUDED.strava_refresh_token,
				strava_expires_at = EXCLUDED.strava_expires_at,
				session_expires_at = EXCLUDED.session_expires_at,
				updated_at = NOW(),
				last_seen_at = NOW()
		`, mobileSessionStorageKey(session.SessionToken), session.Athlete.ID, session.Athlete.FirstName, session.Athlete.LastName, session.Athlete.Profile,
			accessToken, refreshToken, session.ExpiresAt, session.SessionExpiresAt)
		return err
	})
}

func (s *server) loadMobileSession(sessionToken string) (mobileSession, error) {
	session, err := s.loadMobileSessionByStorageKey(sessionToken, mobileSessionStorageKey(sessionToken))
	if err == nil {
		return session, nil
	}
	if err != pgx.ErrNoRows {
		return mobileSession{}, err
	}

	session, err = s.loadMobileSessionByStorageKey(sessionToken, sessionToken)
	if err != nil {
		return mobileSession{}, err
	}
	if saveErr := s.saveMobileSession(session); saveErr == nil {
		_ = s.deleteMobileSessionStorageKey(sessionToken)
	}
	return session, nil
}

func (s *server) loadMobileSessionByStorageKey(rawSessionToken, storageKey string) (mobileSession, error) {
	var session mobileSession
	var athlete strava.Athlete
	var storedAccessToken, storedRefreshToken string
	err := s.withDB(func(conn *pgx.Conn) error {
		return conn.QueryRow(s.ctx, `
			SELECT session_token, athlete_id, athlete_firstname, athlete_lastname, athlete_profile,
			       strava_access_token, strava_refresh_token, strava_expires_at, session_expires_at, created_at
			FROM mobile_app_sessions
			WHERE session_token = $1
		`, storageKey).Scan(
			&session.SessionToken,
			&athlete.ID,
			&athlete.FirstName,
			&athlete.LastName,
			&athlete.Profile,
			&storedAccessToken,
			&storedRefreshToken,
			&session.ExpiresAt,
			&session.SessionExpiresAt,
			&session.CreatedAt,
		)
	})
	if err != nil {
		return mobileSession{}, err
	}
	session.Token, err = s.decryptSecret(storedAccessToken)
	if err != nil {
		return mobileSession{}, err
	}
	session.RefreshToken, err = s.decryptSecret(storedRefreshToken)
	if err != nil {
		return mobileSession{}, err
	}
	session.SessionToken = rawSessionToken
	if session.SessionExpiresAt.IsZero() {
		session.SessionExpiresAt = session.CreatedAt.Add(mobileSessionLifetime)
	}
	session.Athlete = &athlete
	if s.secretBox != nil && (!isEncryptedSecret(storedAccessToken) || !isEncryptedSecret(storedRefreshToken)) {
		if err := s.saveMobileSession(session); err != nil {
			return mobileSession{}, err
		}
	}
	return session, nil
}

func (s *server) refreshMobileSessionIfNeeded(session mobileSession) (mobileSession, error) {
	if !session.SessionExpiresAt.IsZero() && time.Now().After(session.SessionExpiresAt) {
		_ = s.deleteMobileSession(session.SessionToken)
		return mobileSession{}, fmt.Errorf("session expired")
	}
	if session.Token != "" && time.Until(session.ExpiresAt) > 2*time.Minute {
		_ = s.touchMobileSession(session.SessionToken)
		return session, nil
	}
	if strings.TrimSpace(session.RefreshToken) == "" {
		return mobileSession{}, fmt.Errorf("missing Strava refresh token")
	}

	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.IOSRedirectURI)
	tokenResp, err := strava.RefreshAccessToken(*authCfg, session.RefreshToken)
	if err != nil {
		return mobileSession{}, err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return mobileSession{}, fmt.Errorf("Strava did not return an access token")
	}
	session.Token = tokenResp.AccessToken
	if strings.TrimSpace(tokenResp.RefreshToken) != "" {
		session.RefreshToken = tokenResp.RefreshToken
	}
	session.ExpiresAt = stravaTokenExpiry(tokenResp.ExpiresAt)
	if err := s.saveMobileSession(session); err != nil {
		return mobileSession{}, err
	}
	return session, nil
}

func (s *server) touchMobileSession(sessionToken string) error {
	return s.withDB(func(conn *pgx.Conn) error {
		_, err := conn.Exec(s.ctx, `
			UPDATE mobile_app_sessions
			SET last_seen_at = NOW()
			WHERE session_token = $1 OR session_token = $2
		`, mobileSessionStorageKey(sessionToken), sessionToken)
		return err
	})
}

func (s *server) deleteMobileSession(sessionToken string) error {
	return s.withDB(func(conn *pgx.Conn) error {
		_, err := conn.Exec(s.ctx, `
			DELETE FROM mobile_app_sessions
			WHERE session_token = $1 OR session_token = $2
		`, mobileSessionStorageKey(sessionToken), sessionToken)
		return err
	})
}

func (s *server) deleteMobileSessionStorageKey(storageKey string) error {
	return s.withDB(func(conn *pgx.Conn) error {
		_, err := conn.Exec(s.ctx, `DELETE FROM mobile_app_sessions WHERE session_token = $1`, storageKey)
		return err
	})
}

func mobileSessionStorageKey(sessionToken string) string {
	sum := sha256.Sum256([]byte(sessionToken))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stravaTokenExpiry(expiresAt int64) time.Time {
	if expiresAt <= 0 {
		return time.Now().Add(6 * time.Hour)
	}
	return time.Unix(expiresAt, 0)
}

func (s *server) consumeMobileAuthState(state string) bool {
	s.mobileMu.Lock()
	defer s.mobileMu.Unlock()

	expiresAt, ok := s.mobileAuthStates[state]
	if !ok {
		return false
	}
	delete(s.mobileAuthStates, state)
	return time.Now().Before(expiresAt)
}

func (s *server) storeMobileAuthResult(state string, result mobileAuthResult) {
	s.mobileMu.Lock()
	s.mobileAuthResults[state] = result
	delete(s.mobileAuthStates, state)
	s.mobileMu.Unlock()
}

func (s *server) renderMobileAuthCallbackPage(w http.ResponseWriter, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>B11K</title>
  <style>
    :root { color-scheme: dark; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #0d1117; color: #eef2f5; font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    main { width: min(520px, calc(100vw - 40px)); border: 1px solid #2b3442; border-radius: 8px; background: #151a22; padding: 28px; }
    h1 { margin: 0 0 10px; font-size: 28px; }
    p { margin: 0; color: rgba(238,242,245,.72); }
  </style>
</head>
<body>
  <main>
    <h1>%s</h1>
    <p>%s</p>
  </main>
</body>
</html>`, templateEscape(title), templateEscape(detail))
}

func (s *server) mobileSessionFromRequest(w http.ResponseWriter, r *http.Request) (mobileSession, bool) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return mobileSession{}, false
	}

	sessionToken := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if !isPlausibleMobileBearerToken(sessionToken) {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return mobileSession{}, false
	}
	s.mobileMu.Lock()
	session, ok := s.mobileSessions[sessionToken]
	s.mobileMu.Unlock()

	if !ok {
		var err error
		session, err = s.loadMobileSession(sessionToken)
		if err != nil {
			if err != pgx.ErrNoRows {
				log.Printf("⚠️ Mobile session lookup failed: %v", err)
				http.Error(w, "session lookup failed", http.StatusInternalServerError)
				return mobileSession{}, false
			}
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return mobileSession{}, false
		}
	}

	session, err := s.refreshMobileSessionIfNeeded(session)
	if err != nil {
		log.Printf("⚠️ Mobile session refresh failed: %v", err)
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return mobileSession{}, false
	}

	if session.Token == "" || session.Athlete == nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return mobileSession{}, false
	}

	s.mobileMu.Lock()
	s.mobileSessions[sessionToken] = session
	s.mobileMu.Unlock()

	return session, true
}

func isPlausibleMobileBearerToken(token string) bool {
	if token == "" || len(token) > 128 {
		return false
	}
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func templateEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return value
}

func randomURLToken(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
