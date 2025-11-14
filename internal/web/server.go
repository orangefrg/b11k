package web

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	syncpkg "sync"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	"b11k/internal/sync"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	StravaClientID     string
	StravaClientSecret string
	StravaRedirectURI  string
	MapboxToken        string
	PGIP               string
	PGPort             string
	PGUser             string
	PGPassword         string
	PGDatabase         string
	WebPort            string
}

type server struct {
	ctx    context.Context
	cfg    Config
	conn   *pgx.Conn
	connMu syncpkg.Mutex // Mutex to serialize database access (single connection)
	tmpl   *template.Template
	token  string
	user   *strava.Athlete
}

const stravaTokenCookieName = "strava_token"

func RunServer(ctx context.Context, cfg Config) {
	log.Printf("🌐 Starting web server on port %s", cfg.WebPort)

	conn, err := pggeo.Connect(ctx, cfg.PGUser, cfg.PGPassword, cfg.PGIP, cfg.PGPort, cfg.PGDatabase)
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	// Validate and migrate schema (forceRebuild=false for normal server startup)
	if err := pggeo.ValidateAndMigrateSchema(ctx, conn, false); err != nil {
		log.Fatalf("Error validating/migrating database schema: %v", err)
	}

	// Parse templates from disk
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"mul":  func(a, b float64) float64 { return a * b },
		"kcal": func(kj float64) float64 { return kj * 0.239006 },
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
		"hasActivity": func(data interface{}) bool {
			if data == nil {
				return false
			}
			v := reflect.ValueOf(data)
			if v.Kind() == reflect.Ptr {
				v = v.Elem()
			}
			if v.Kind() != reflect.Struct {
				return false
			}
			return v.FieldByName("Activity").IsValid()
		},
	}).ParseFiles(
		filepath.FromSlash("web/templates/index.html"),
		filepath.FromSlash("web/templates/activity.html"),
		filepath.FromSlash("web/templates/segments.html"),
		filepath.FromSlash("web/templates/segment.html"),
		filepath.FromSlash("web/templates/partials/topbar.html"),
		filepath.FromSlash("web/templates/partials/map.html"),
		filepath.FromSlash("web/templates/partials/graph.html"),
		filepath.FromSlash("web/templates/partials/activity_sidebar.html"),
		filepath.FromSlash("web/templates/partials/segment_sidebar.html"),
	)
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	s := &server{ctx: ctx, cfg: cfg, conn: conn, tmpl: tmpl}

	// Routes
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/strava/", s.handleStravaHome)
	http.HandleFunc("/strava/login", s.handleStravaLogin)
	http.HandleFunc("/activity/", s.handleActivity)
	http.HandleFunc("/api/activities", s.handleActivitiesAPI)
	http.HandleFunc("/api/activities/", s.handleActivityPointsAPI)
	http.HandleFunc("/strava/callback", s.handleStravaCallback)
	http.HandleFunc("/strava/logout", s.handleStravaLogout)
	http.HandleFunc("/api/hrzones", s.handleHRZones)
	http.HandleFunc("/strava/sync", s.handleStravaSyncSSE)
	http.HandleFunc("/api/segments", s.handleSegmentsAPI)
	http.HandleFunc("/api/segments/", s.handleSegmentAPI)
	http.HandleFunc("/segments", s.handleSegmentsPage)
	http.HandleFunc("/segment/", s.handleSegmentPage)

	// static
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.FromSlash("web/static")))))

	addr := ":" + strings.TrimPrefix(cfg.WebPort, ":")
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Check for token in cookie if not in memory
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	s.renderActivitiesPageWithReq(w, r)
}

func (s *server) handleStravaHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/strava/" {
		http.NotFound(w, r)
		return
	}
	// Check for token in cookie if not in memory
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	s.renderActivitiesPageWithReq(w, r)
}

func (s *server) renderActivitiesPageWithReq(w http.ResponseWriter, r *http.Request) {
	// pagination params
	page := 1
	perPage := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if n, err := strconv.Atoi(pp); err == nil && n > 0 && n <= 100 {
			perPage = n
		}
	}
	// Get all activities for the current athlete (no date restriction)
	var activities []strava.ActivitySummary
	var err error
	if s.user != nil {
		activities, err = pggeo.GetAllActivities(s.ctx, s.conn, s.user.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// ensure token from cookie and athlete profile loaded for header
	if s.token == "" {
		// this function doesn't have *http.Request*, but callers already populated s.token via cookie where possible
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}

	// paginate in-memory for now
	total := len(activities)
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	startIdx := (page - 1) * perPage
	endIdx := startIdx + perPage
	if endIdx > total {
		endIdx = total
	}
	pageItems := activities
	if total > 0 {
		pageItems = activities[startIdx:endIdx]
	}
	data := struct {
		Activities   []strava.ActivitySummary
		ShowLoginCTA bool
		Authorized   bool
		Athlete      *strava.Athlete
		CurrentPage  int
		TotalPages   int
		HasNext      bool
		HasPrev      bool
		PerPage      int
	}{
		Activities:   pageItems,
		ShowLoginCTA: s.token == "" && s.cfg.StravaClientID != "",
		Authorized:   s.token != "",
		Athlete:      s.user,
		CurrentPage:  page,
		TotalPages:   totalPages,
		HasNext:      page < totalPages,
		HasPrev:      page > 1,
		PerPage:      perPage,
	}
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) handleStravaLogin(w http.ResponseWriter, r *http.Request) {
	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.StravaRedirectURI)
	loginURL := strava.GenerateAuthURL(*authCfg)
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (s *server) handleActivity(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/activity/")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}
	activityID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Load athlete from cookie token if available
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}

	// Check if user is authenticated
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	activity, err := pggeo.GetActivityByID(s.ctx, s.conn, s.user.ID, activityID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	data := struct {
		Activity     strava.ActivitySummary
		MapboxToken  string
		Athlete      *strava.Athlete
		ShowLoginCTA bool
		Authorized   bool
	}{
		Activity:     *activity,
		MapboxToken:  s.cfg.MapboxToken,
		Athlete:      s.user,
		ShowLoginCTA: s.token == "" && s.cfg.StravaClientID != "",
		Authorized:   s.token != "",
	}
	if err := s.tmpl.ExecuteTemplate(w, "activity.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) handleActivitiesAPI(w http.ResponseWriter, r *http.Request) {
	// Check if user is authenticated
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	end := time.Now()
	start := end.AddDate(0, 0, -180)
	activities, err := pggeo.GetActivitiesByDateRange(s.ctx, s.conn, s.user.ID, start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, activities)
}

// handleStravaSyncSSE starts a sync and streams progress logs using Server-Sent Events
func (s *server) handleStravaSyncSSE(w http.ResponseWriter, r *http.Request) {
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.token == "" {
		http.Error(w, "not authorized with Strava", http.StatusUnauthorized)
		return
	}

	// Parse timeframe from query (?start=YYYY-MM-DD&end=YYYY-MM-DD)
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// helper to send a line
	send := func(event, data string) {
		if event != "" {
			_, _ = w.Write([]byte("event: " + event + "\n"))
		}
		_, _ = w.Write([]byte("data: " + data + "\n\n"))
		flusher.Flush()
	}

	send("log", "Starting sync...")

	cfg := sync.SyncConfig{
		StravaAccessToken: s.token,
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
	}

	// Create progress callback that sends SSE events
	progressCallback := func(phase string, current, total int, message string) {
		progressData := struct {
			Phase   string `json:"phase"`
			Current int    `json:"current"`
			Total   int    `json:"total"`
			Message string `json:"message"`
		}{
			Phase:   phase,
			Current: current,
			Total:   total,
			Message: message,
		}
		progressJSON, _ := json.Marshal(progressData)
		send("progress", string(progressJSON))
	}

	// Run sync synchronously; for large syncs consider goroutine + channels
	result, err := sync.SyncActivitiesFromStravaWithRetry(s.ctx, cfg, 3, progressCallback)
	if err != nil {
		send("error", "Sync failed: "+err.Error())
		return
	}

	// Summarize
	summary := struct {
		Total    int `json:"total"`
		Existing int `json:"existing"`
		New      int `json:"new"`
		Success  int `json:"success"`
		Failed   int `json:"failed"`
	}{result.TotalActivitiesFound, result.ExistingActivities, result.NewActivities, result.SuccessfullyProcessed, len(result.FailedActivities)}

	b, _ := json.Marshal(summary)
	send("summary", string(b))
	send("done", "ok")
}

func (s *server) handleActivityPointsAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/activities/"), "/")
	if len(parts) < 1 {
		http.NotFound(w, r)
		return
	}

	activityID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Check for token in cookie if not in memory
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}

	// Check if user is authenticated
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Handle graph endpoint
	if len(parts) == 2 && parts[1] == "graph" {
		metricsStr := r.URL.Query().Get("metrics")
		if metricsStr == "" {
			http.Error(w, "metrics parameter required", http.StatusBadRequest)
			return
		}
		metrics := strings.Split(metricsStr, ",")
		for i := range metrics {
			metrics[i] = strings.TrimSpace(metrics[i])
		}

		includeZones := r.URL.Query().Get("include_zones") == "true"

		var hrZones *strava.HeartRateZones
		if includeZones {
			zones, err := strava.FetchHeartRateZones(s.token)
			if err == nil && zones != nil {
				hrZones = &zones.HeartRate
			}
		}

		graphData, err := pggeo.GetGraphDataForActivity(s.ctx, s.conn, s.user.ID, activityID, metrics, includeZones, hrZones)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, graphData)
		return
	}

	// Handle points endpoint
	if len(parts) == 2 && parts[1] == "points" {
		samples, err := pggeo.GetPointSamplesForActivity(s.ctx, s.conn, s.user.ID, activityID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, samples)
		return
	}

	http.NotFound(w, r)
}

func (s *server) handleHRZones(w http.ResponseWriter, r *http.Request) {
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.token == "" {
		http.Error(w, "not authorized", http.StatusUnauthorized)
		return
	}
	zones, err := strava.FetchHeartRateZones(s.token)
	if err != nil {
		// Some athletes may not have HR zones configured or API could deny access.
		// Return empty zones with 200 so the UI can degrade gracefully.
		log.Printf("hr zones fetch error: %v", err)
		writeJSON(w, &strava.AthleteZones{HeartRate: strava.HeartRateZones{Zones: []strava.HRZone{}}})
		return
	}
	writeJSON(w, zones)
}

func (s *server) handleStravaCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.StravaRedirectURI)
	tok, err := strava.ExchangeCodeForToken(*authCfg, code)
	if err != nil {
		log.Printf("token exchange error: %v", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	s.token = tok

	// Set secure HTTP-only cookie with token
	http.SetCookie(w, &http.Cookie{
		Name:     stravaTokenCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   60 * 60 * 24 * 30, // 30 days
	})

	// Preload athlete profile for header display
	if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
		s.user = a
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<html><body><h3>Strava authorized ✅</h3><p>You can close this tab.</p><p><a href='/'>&larr; Back to activities</a></p></body></html>"))
}

func (s *server) handleStravaLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the token from memory
	s.token = ""

	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stravaTokenCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1, // Expire immediately
	})

	// Redirect to home page
	http.Redirect(w, r, "/", http.StatusFound)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// handleSegmentsAPI handles GET /api/segments and POST /api/segments
func (s *server) handleSegmentsAPI(w http.ResponseWriter, r *http.Request) {
	// Check authentication
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "GET":
		segments, err := pggeo.ListFavoriteSegments(s.ctx, s.conn, s.user.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, segments)
	case "POST":
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			ActivityID  int64  `json:"activity_id"`
			StartIndex  int    `json:"start_index"`
			EndIndex    int    `json:"end_index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if req.StartIndex < 0 || req.EndIndex < 0 || req.StartIndex >= req.EndIndex {
			http.Error(w, "invalid start_index or end_index", http.StatusBadRequest)
			return
		}

		// Get point samples for the activity
		samples, err := pggeo.GetPointSamplesForActivity(s.ctx, s.conn, s.user.ID, req.ActivityID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		if req.StartIndex >= len(samples) || req.EndIndex > len(samples) {
			http.Error(w, "index out of range", http.StatusBadRequest)
			return
		}

		// Extract points between start and end indices
		latLngData := make([][]float64, 0, req.EndIndex-req.StartIndex)
		segmentSamples := make([]pggeo.PointSample, 0, req.EndIndex-req.StartIndex)
		for i := req.StartIndex; i < req.EndIndex; i++ {
			latLngData = append(latLngData, []float64{samples[i].Lat, samples[i].Lng})
			segmentSamples = append(segmentSamples, samples[i])
		}

		segment, err := pggeo.InsertFavoriteSegment(s.ctx, s.conn, s.user.ID, req.Name, req.Description, latLngData, segmentSamples)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, segment)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSegmentAPI handles GET /api/segments/:id and DELETE /api/segments/:id
func (s *server) handleSegmentAPI(w http.ResponseWriter, r *http.Request) {
	// Extract segment ID from path
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/segments/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "segment ID required", http.StatusBadRequest)
		return
	}

	segmentID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid segment ID", http.StatusBadRequest)
		return
	}

	// Check authentication
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "GET":
		// Handle GET /api/segments/:id/graph
		if len(parts) > 1 && parts[1] == "graph" {
			activityIDStr := r.URL.Query().Get("activity_id")
			if activityIDStr == "" {
				http.Error(w, "activity_id parameter required", http.StatusBadRequest)
				return
			}
			activityID, err := strconv.ParseInt(activityIDStr, 10, 64)
			if err != nil {
				http.Error(w, "invalid activity_id", http.StatusBadRequest)
				return
			}

			metricsStr := r.URL.Query().Get("metrics")
			if metricsStr == "" {
				http.Error(w, "metrics parameter required", http.StatusBadRequest)
				return
			}
			metrics := strings.Split(metricsStr, ",")
			for i := range metrics {
				metrics[i] = strings.TrimSpace(metrics[i])
			}

			includeZones := r.URL.Query().Get("include_zones") == "true"

			var hrZones *strava.HeartRateZones
			if includeZones {
				zones, err := strava.FetchHeartRateZones(s.token)
				if err == nil && zones != nil {
					hrZones = &zones.HeartRate
				}
			}

			graphData, err := pggeo.GetGraphDataForSegmentInActivity(s.ctx, s.conn, s.user.ID, activityID, segmentID, metrics, includeZones, hrZones)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, graphData)
			return
		}
		// Handle GET /api/segments/:id/metrics
		if len(parts) > 1 && parts[1] == "metrics" {
			s.connMu.Lock()
			query := `SELECT * FROM get_segment_metrics($1)`
			var distanceM, elevationGainM float64
			err := s.conn.QueryRow(s.ctx, query, segmentID).Scan(&distanceM, &elevationGainM)
			s.connMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]float64{
				"distance":       distanceM,
				"elevation_gain": elevationGainM,
			})
			return
		}
		// Handle GET /api/segments/:id/activity/:activityId/indices
		if len(parts) > 2 && parts[1] == "activity" && parts[3] == "indices" {
			activityID, err := strconv.ParseInt(parts[2], 10, 64)
			if err != nil {
				http.Error(w, "invalid activity ID", http.StatusBadRequest)
				return
			}
			tolerance := 15.0
			if tolStr := r.URL.Query().Get("tolerance"); tolStr != "" {
				if tol, err := strconv.ParseFloat(tolStr, 64); err == nil {
					tolerance = tol
				}
			}

			// Check cache first (with mutex)
			s.connMu.Lock()
			cached, err := pggeo.GetCachedSegmentActivityMetrics(s.ctx, s.conn, segmentID, activityID, tolerance)
			s.connMu.Unlock()
			if err == nil && cached != nil && cached.StartIndex != nil && cached.EndIndex != nil {
				writeJSON(w, map[string]int{
					"start_index": *cached.StartIndex,
					"end_index":   *cached.EndIndex,
				})
				return
			}

			// Calculate if not cached (with mutex)
			s.connMu.Lock()
			query := `SELECT * FROM find_segment_point_indices($1, $2, $3, $4)`
			var startIndex, endIndex int
			err = s.conn.QueryRow(s.ctx, query, segmentID, activityID, s.user.ID, tolerance).Scan(&startIndex, &endIndex)
			s.connMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Cache the result (metrics will be cached separately)
			writeJSON(w, map[string]int{
				"start_index": startIndex,
				"end_index":   endIndex,
			})
			return
		}
		// Handle GET /api/segments/:id/activity/:activityId/metrics
		if len(parts) > 2 && parts[1] == "activity" && parts[3] == "metrics" {
			activityID, err := strconv.ParseInt(parts[2], 10, 64)
			if err != nil {
				http.Error(w, "invalid activity ID", http.StatusBadRequest)
				return
			}
			tolerance := 15.0
			if tolStr := r.URL.Query().Get("tolerance"); tolStr != "" {
				if tol, err := strconv.ParseFloat(tolStr, 64); err == nil {
					tolerance = tol
				}
			}

			// Check cache first (with mutex)
			s.connMu.Lock()
			cached, err := pggeo.GetCachedSegmentActivityMetrics(s.ctx, s.conn, segmentID, activityID, tolerance)
			s.connMu.Unlock()
			if err == nil && cached != nil && cached.AvgHR != nil && cached.AvgSpeed != nil {
				distance := 0.0
				if cached.DistanceM != nil {
					distance = *cached.DistanceM
				}
				elevationGain := 0.0
				if cached.ElevationGainM != nil {
					elevationGain = *cached.ElevationGainM
				}
				writeJSON(w, map[string]float64{
					"avg_hr":         *cached.AvgHR,
					"avg_speed":      *cached.AvgSpeed,
					"distance":       distance,
					"elevation_gain": elevationGain,
				})
				return
			}

			// Calculate if not cached (with mutex)
			s.connMu.Lock()
			query := `SELECT * FROM get_activity_segment_metrics($1, $2, $3, $4)`
			var avgHR, avgSpeed, distanceM, elevationGainM float64
			err = s.conn.QueryRow(s.ctx, query, segmentID, activityID, s.user.ID, tolerance).Scan(&avgHR, &avgSpeed, &distanceM, &elevationGainM)
			s.connMu.Unlock()
			if err != nil {
				// If no rows returned (no matching points), return zeros
				writeJSON(w, map[string]float64{
					"avg_hr":         0,
					"avg_speed":      0,
					"distance":       0,
					"elevation_gain": 0,
				})
				return
			}

			// Get indices for caching (with mutex)
			s.connMu.Lock()
			var startIndex, endIndex int
			idxQuery := `SELECT * FROM find_segment_point_indices($1, $2, $3, $4)`
			if err := s.conn.QueryRow(s.ctx, idxQuery, segmentID, activityID, s.user.ID, tolerance).Scan(&startIndex, &endIndex); err == nil {
				s.connMu.Unlock()
				// Cache the metrics (with mutex)
				s.connMu.Lock()
				pggeo.CacheSegmentActivityMetrics(s.ctx, s.conn, segmentID, activityID, tolerance, startIndex, endIndex, avgHR, avgSpeed, distanceM, elevationGainM)
				s.connMu.Unlock()
			} else {
				s.connMu.Unlock()
			}

			writeJSON(w, map[string]float64{
				"avg_hr":         avgHR,
				"avg_speed":      avgSpeed,
				"distance":       distanceM,
				"elevation_gain": elevationGainM,
			})
			return
		}
		// Handle GET /api/segments/:id/activities
		if len(parts) > 1 && parts[1] == "activities" {
			// Parse query parameters
			tolerance := 15.0 // default
			if tolStr := r.URL.Query().Get("tolerance"); tolStr != "" {
				if tol, err := strconv.ParseFloat(tolStr, 64); err == nil {
					tolerance = tol
				}
			}
			forceRefresh := r.URL.Query().Get("refresh") == "true"
			sortBy := r.URL.Query().Get("sort")
			if sortBy == "" {
				sortBy = "distance" // default
			}

			activities, err := pggeo.GetActivitiesForSegment(s.ctx, s.conn, s.user.ID, segmentID, tolerance, sortBy, forceRefresh)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, activities)
			return
		}
		// Regular GET /api/segments/:id
		segment, err := pggeo.GetFavoriteSegment(s.ctx, s.conn, segmentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Verify ownership
		if segment.AthleteID != s.user.ID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		writeJSON(w, segment)
	case "DELETE":
		// Verify ownership before deleting
		segment, err := pggeo.GetFavoriteSegment(s.ctx, s.conn, segmentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if segment.AthleteID != s.user.ID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		err = pggeo.DeleteFavoriteSegment(s.ctx, s.conn, segmentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSegmentsPage handles GET /segments - renders the segments list page
func (s *server) handleSegmentsPage(w http.ResponseWriter, r *http.Request) {
	// Check authentication
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	segments, err := pggeo.ListFavoriteSegments(s.ctx, s.conn, s.user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Segments     []pggeo.FavoriteSegment
		Athlete      *strava.Athlete
		ShowLoginCTA bool
		Authorized   bool
	}{
		Segments:     segments,
		Athlete:      s.user,
		ShowLoginCTA: s.token == "" && s.cfg.StravaClientID != "",
		Authorized:   s.token != "",
	}

	if err := s.tmpl.ExecuteTemplate(w, "segments.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleSegmentPage handles GET /segment/:id - renders the segment detail page
func (s *server) handleSegmentPage(w http.ResponseWriter, r *http.Request) {
	// Extract segment ID from path
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/segment/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "segment ID required", http.StatusBadRequest)
		return
	}

	segmentID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid segment ID", http.StatusBadRequest)
		return
	}

	// Check authentication
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		}
	}
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get segment
	segment, err := pggeo.GetFavoriteSegment(s.ctx, s.conn, segmentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Verify ownership
	if segment.AthleteID != s.user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	data := struct {
		Segment      *pggeo.FavoriteSegment
		Athlete      *strava.Athlete
		ShowLoginCTA bool
		Authorized   bool
		MapboxToken  string
	}{
		Segment:      segment,
		Athlete:      s.user,
		ShowLoginCTA: s.token == "" && s.cfg.StravaClientID != "",
		Authorized:   s.token != "",
		MapboxToken:  s.cfg.MapboxToken,
	}

	if err := s.tmpl.ExecuteTemplate(w, "segment.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
