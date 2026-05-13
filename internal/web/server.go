package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	StravaClientID      string
	StravaClientSecret  string
	StravaRedirectURI   string
	MapboxToken         string
	PGIP                string
	PGPort              string
	PGUser              string
	PGPassword          string
	PGDatabase          string
	WebHost             string
	WebPort             string
	WebProtocol         string
	DevReloadTemplates  bool
	MobileActivityOrder string
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

	tmpl, err := parseTemplates()
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	s := &server{ctx: ctx, cfg: cfg, conn: conn, tmpl: tmpl}
	if cfg.DevReloadTemplates {
		log.Printf("🔁 Dev template reload enabled")
	}

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
	http.HandleFunc("/profile", s.handleProfilePage)

	// static
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.FromSlash("web/static")))))

	addr := ":" + strings.TrimPrefix(cfg.WebPort, ":")
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"mul":  func(a, b float64) float64 { return a * b },
		"kcal": func(kj float64) float64 { return kj * 0.239006 },
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
		"asset": func(path string) string {
			return cacheBustedAsset(path)
		},
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
		filepath.FromSlash("web/templates/profile.html"),
		filepath.FromSlash("web/templates/partials/topbar.html"),
		filepath.FromSlash("web/templates/partials/map.html"),
		filepath.FromSlash("web/templates/partials/graph.html"),
		filepath.FromSlash("web/templates/partials/color_controls.html"),
		filepath.FromSlash("web/templates/partials/activity_sidebar.html"),
		filepath.FromSlash("web/templates/partials/segment_sidebar.html"),
	)
}

func cacheBustedAsset(path string) string {
	if !strings.HasPrefix(path, "/static/") {
		return path
	}
	localPath := filepath.FromSlash(strings.TrimPrefix(path, "/static/"))
	info, err := os.Stat(filepath.Join("web", "static", localPath))
	if err != nil {
		return path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%sv=%d", path, separator, info.ModTime().Unix())
}

func (s *server) executeTemplate(w http.ResponseWriter, name string, data interface{}) error {
	tmpl := s.tmpl
	if s.cfg.DevReloadTemplates {
		reloaded, err := parseTemplates()
		if err != nil {
			log.Printf("template reload error: %v", err)
			return err
		}
		tmpl = reloaded
	}
	return tmpl.ExecuteTemplate(w, name, data)
}

func (s *server) withDB(op func(*pgx.Conn) error) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	err := op(s.conn)
	if err == nil {
		return nil
	}
	if !isRecoverableDBError(err) {
		return err
	}

	log.Printf("⚠️ Database connection looked busy/stale, reconnecting: %v", err)
	if recErr := s.reconnectDBLocked(); recErr != nil {
		return fmt.Errorf("database recovery failed after %v: %w", err, recErr)
	}

	if retryErr := op(s.conn); retryErr != nil {
		return retryErr
	}
	log.Printf("✅ Database connection recovered")
	return nil
}

func (s *server) reconnectDBLocked() error {
	if s.conn != nil {
		_ = s.conn.Close(context.Background())
	}

	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()

	conn, err := pggeo.Connect(ctx, s.cfg.PGUser, s.cfg.PGPassword, s.cfg.PGIP, s.cfg.PGPort, s.cfg.PGDatabase)
	if err != nil {
		return err
	}
	if err := pggeo.ValidateAndMigrateSchema(ctx, conn, false); err != nil {
		_ = conn.Close(context.Background())
		return err
	}
	s.conn = conn
	return nil
}

func isRecoverableDBError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	recoverableFragments := []string{
		"conn busy",
		"failed to deallocate cached statement",
		"conn closed",
		"closed connection",
		"connection reset",
		"broken pipe",
	}
	for _, fragment := range recoverableFragments {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func (s *server) renderDatabaseBusy(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("⚠️ Database still recovering for %s: %v", r.URL.Path, err)
	w.Header().Set("Retry-After", "2")
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":               "Database is recovering. Please retry shortly.",
			"retry_after_seconds": 2,
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="refresh" content="2">
  <title>B11K is recovering</title>
  <style>
    :root { color-scheme: dark; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background: #0d1117;
      color: #eef2f5;
      font: 16px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(520px, calc(100vw - 40px));
      border: 1px solid #2b3442;
      border-radius: 8px;
      background: #151a22;
      padding: 28px;
      box-shadow: 0 18px 60px rgba(0,0,0,.32);
    }
    h1 { margin: 0 0 10px; font-size: clamp(28px, 7vw, 42px); line-height: 1.1; }
    p { margin: 0 0 14px; color: rgba(238,242,245,.72); }
    .bar {
      height: 4px;
      overflow: hidden;
      border-radius: 999px;
      background: #253043;
    }
    .bar::before {
      content: "";
      display: block;
      width: 38%;
      height: 100%;
      border-radius: inherit;
      background: #9bd3ff;
      animation: busy 1.1s ease-in-out infinite alternate;
    }
    @keyframes busy { from { transform: translateX(0); } to { transform: translateX(164%); } }
  </style>
</head>
<body>
  <main>
    <h1>Reconnecting database</h1>
    <p>The database connection got busy, so B11K is reconnecting. This page will retry automatically in a moment.</p>
    <div class="bar" aria-hidden="true"></div>
  </main>
</body>
</html>`))
}

func (s *server) handleDBPageError(w http.ResponseWriter, r *http.Request, err error, fallbackStatus int) {
	if isRecoverableDBError(err) {
		s.renderDatabaseBusy(w, r, err)
		return
	}
	http.Error(w, err.Error(), fallbackStatus)
}

func (s *server) ensureSessionFromRequest(r *http.Request) {
	if s.token == "" {
		if cookie, err := r.Cookie(stravaTokenCookieName); err == nil {
			s.token = cookie.Value
		}
	}
	if s.user == nil && s.token != "" {
		if a, err := strava.FetchCurrentAthlete(s.token); err == nil {
			s.user = a
		} else {
			log.Printf("⚠️ Failed to fetch current athlete: %v", err)
		}
	}
}

func (s *server) enrichGearNames(activities []strava.ActivitySummary) []strava.ActivitySummary {
	if s.token == "" || s.user == nil {
		return activities
	}

	seen := make(map[string]*string)
	for i := range activities {
		gearID := strings.TrimSpace(activities[i].GearID)
		if gearID == "" || activities[i].GearName != nil {
			continue
		}
		if cached, ok := seen[gearID]; ok {
			activities[i].GearName = cached
			continue
		}

		gear, err := strava.FetchGear(s.token, gearID)
		if err != nil || gear == nil || strings.TrimSpace(gear.Name) == "" {
			if err != nil {
				log.Printf("⚠️ Failed to fetch gear %s: %v", gearID, err)
			}
			seen[gearID] = nil
			continue
		}

		name := strings.TrimSpace(gear.Name)
		activities[i].GearName = &name
		seen[gearID] = &name
		if err := s.withDB(func(conn *pgx.Conn) error {
			return pggeo.UpdateGearNameForGearID(s.ctx, conn, s.user.ID, gearID, name)
		}); err != nil {
			log.Printf("⚠️ Failed to cache gear name for %s: %v", gearID, err)
		}
	}
	return activities
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Check for token in cookie if not in memory
	s.ensureSessionFromRequest(r)
	s.renderActivitiesPageWithReq(w, r)
}

func (s *server) handleStravaHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/strava/" {
		http.NotFound(w, r)
		return
	}
	// Check for token in cookie if not in memory
	s.ensureSessionFromRequest(r)
	s.renderActivitiesPageWithReq(w, r)
}

func (s *server) renderActivitiesPageWithReq(w http.ResponseWriter, r *http.Request) {
	s.ensureSessionFromRequest(r)

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
		err = s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			activities, dbErr = pggeo.GetAllActivities(s.ctx, conn, s.user.ID)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		activities = s.enrichGearNames(activities)
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
	if err := s.executeTemplate(w, "index.html", data); err != nil {
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

	var activity *strava.ActivitySummary
	err = s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activity, dbErr = pggeo.GetActivityByID(s.ctx, conn, s.user.ID, activityID)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusNotFound)
		return
	}
	enriched := s.enrichGearNames([]strava.ActivitySummary{*activity})
	if len(enriched) > 0 {
		activity = &enriched[0]
	}

	var activityHRZones []pggeo.HRZoneDistribution
	if s.token != "" {
		if zones, err := strava.FetchHeartRateZones(s.token); err == nil && zones != nil {
			err = s.withDB(func(conn *pgx.Conn) error {
				var dbErr error
				activityHRZones, dbErr = pggeo.GetHRZoneDistributionForActivity(s.ctx, conn, s.user.ID, activityID, &zones.HeartRate)
				return dbErr
			})
			if err != nil {
				log.Printf("⚠️ Failed to calculate activity HR zones for %d: %v", activityID, err)
			}
		}
	}
	data := struct {
		Activity            strava.ActivitySummary
		ActivityHRZones     []pggeo.HRZoneDistribution
		MapboxToken         string
		Athlete             *strava.Athlete
		ShowLoginCTA        bool
		Authorized          bool
		MobileActivityOrder string
	}{
		Activity:            *activity,
		ActivityHRZones:     activityHRZones,
		MapboxToken:         s.cfg.MapboxToken,
		Athlete:             s.user,
		ShowLoginCTA:        s.token == "" && s.cfg.StravaClientID != "",
		Authorized:          s.token != "",
		MobileActivityOrder: s.cfg.MobileActivityOrder,
	}
	if err := s.executeTemplate(w, "activity.html", data); err != nil {
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
	var activities []strava.ActivitySummary
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activities, dbErr = pggeo.GetActivitiesByDateRange(s.ctx, conn, s.user.ID, start, end)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	activities = s.enrichGearNames(activities)
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

		var graphData *pggeo.GraphData
		err := s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			graphData, dbErr = pggeo.GetGraphDataForActivity(s.ctx, conn, s.user.ID, activityID, metrics, includeZones, hrZones)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, graphData)
		return
	}

	// Handle points endpoint
	if len(parts) == 2 && parts[1] == "points" {
		var samples []pggeo.PointSample
		err := s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			samples, dbErr = pggeo.GetPointSamplesForActivity(s.ctx, conn, s.user.ID, activityID)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
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

	// Log the callback for debugging
	log.Printf("🔐 Strava callback received from: %s", r.RemoteAddr)
	log.Printf("📋 Using redirect URI: %s", s.cfg.StravaRedirectURI)

	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.StravaRedirectURI)
	tok, err := strava.ExchangeCodeForToken(*authCfg, code)
	if err != nil {
		log.Printf("❌ Token exchange error: %v", err)
		log.Printf("💡 Check that your Strava app's redirect URI matches: %s", s.cfg.StravaRedirectURI)
		http.Error(w, fmt.Sprintf("token exchange failed: %v", err), http.StatusBadGateway)
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
		var segments []pggeo.FavoriteSegment
		err := s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			segments, dbErr = pggeo.ListFavoriteSegments(s.ctx, conn, s.user.ID)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
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
		var samples []pggeo.PointSample
		err := s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			samples, dbErr = pggeo.GetPointSamplesForActivity(s.ctx, conn, s.user.ID, req.ActivityID)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusNotFound)
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

		var segment *pggeo.FavoriteSegment
		err = s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			segment, dbErr = pggeo.InsertFavoriteSegment(s.ctx, conn, s.user.ID, req.Name, req.Description, latLngData, segmentSamples)
			return dbErr
		})
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
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

			var graphData *pggeo.GraphData
			err = s.withDB(func(conn *pgx.Conn) error {
				var dbErr error
				graphData, dbErr = pggeo.GetGraphDataForSegmentInActivity(s.ctx, conn, s.user.ID, activityID, segmentID, metrics, includeZones, hrZones)
				return dbErr
			})
			if err != nil {
				log.Printf("❌ Failed to load segment graph data for segment %d activity %d: %v", segmentID, activityID, err)
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
				return
			}
			writeJSON(w, graphData)
			return
		}
		// Handle GET /api/segments/:id/metrics
		if len(parts) > 1 && parts[1] == "metrics" {
			query := `SELECT * FROM get_segment_metrics($1)`
			var distanceM, elevationGainM float64
			err := s.withDB(func(conn *pgx.Conn) error {
				return conn.QueryRow(s.ctx, query, segmentID).Scan(&distanceM, &elevationGainM)
			})
			if err != nil {
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
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
			var cached *pggeo.SegmentActivityCacheEntry
			err = s.withDB(func(conn *pgx.Conn) error {
				var dbErr error
				cached, dbErr = pggeo.GetCachedSegmentActivityMetrics(s.ctx, conn, segmentID, activityID, tolerance)
				return dbErr
			})
			if err == nil && cached != nil && cached.StartIndex != nil && cached.EndIndex != nil {
				writeJSON(w, map[string]int{
					"start_index": *cached.StartIndex,
					"end_index":   *cached.EndIndex,
				})
				return
			}

			// Calculate if not cached (with mutex)
			query := `SELECT * FROM find_segment_point_indices($1, $2, $3, $4)`
			var startIndex, endIndex int
			err = s.withDB(func(conn *pgx.Conn) error {
				return conn.QueryRow(s.ctx, query, segmentID, activityID, s.user.ID, tolerance).Scan(&startIndex, &endIndex)
			})
			if err != nil {
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
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
			var cached *pggeo.SegmentActivityCacheEntry
			err = s.withDB(func(conn *pgx.Conn) error {
				var dbErr error
				cached, dbErr = pggeo.GetCachedSegmentActivityMetrics(s.ctx, conn, segmentID, activityID, tolerance)
				return dbErr
			})
			if err == nil && cached != nil && cached.AvgHR != nil && cached.AvgSpeed != nil {
				distance := 0.0
				if cached.DistanceM != nil {
					distance = *cached.DistanceM
				}
				elevationGain := 0.0
				if cached.ElevationGainM != nil {
					elevationGain = *cached.ElevationGainM
				}
				elapsedSeconds := 0.0
				if cached.ElapsedSeconds != nil {
					elapsedSeconds = *cached.ElapsedSeconds
				}
				writeJSON(w, map[string]float64{
					"avg_hr":          *cached.AvgHR,
					"avg_speed":       *cached.AvgSpeed,
					"distance":        distance,
					"elevation_gain":  elevationGain,
					"elapsed_seconds": elapsedSeconds,
				})
				return
			}

			// Calculate if not cached (with mutex)
			query := `SELECT * FROM get_activity_segment_metrics($1, $2, $3, $4)`
			var avgHR, avgSpeed, distanceM, elevationGainM, elapsedSeconds float64
			err = s.withDB(func(conn *pgx.Conn) error {
				return conn.QueryRow(s.ctx, query, segmentID, activityID, s.user.ID, tolerance).Scan(&avgHR, &avgSpeed, &distanceM, &elevationGainM, &elapsedSeconds)
			})
			if err != nil {
				// If no rows returned (no matching points), return zeros
				writeJSON(w, map[string]float64{
					"avg_hr":          0,
					"avg_speed":       0,
					"distance":        0,
					"elevation_gain":  0,
					"elapsed_seconds": 0,
				})
				return
			}

			// Get indices for caching (with mutex)
			var startIndex, endIndex int
			idxQuery := `SELECT * FROM find_segment_point_indices($1, $2, $3, $4)`
			_ = s.withDB(func(conn *pgx.Conn) error {
				if err := conn.QueryRow(s.ctx, idxQuery, segmentID, activityID, s.user.ID, tolerance).Scan(&startIndex, &endIndex); err != nil {
					return err
				}
				return pggeo.CacheSegmentActivityMetrics(s.ctx, conn, segmentID, activityID, tolerance, startIndex, endIndex, avgHR, avgSpeed, distanceM, elevationGainM, elapsedSeconds)
			})

			writeJSON(w, map[string]float64{
				"avg_hr":          avgHR,
				"avg_speed":       avgSpeed,
				"distance":        distanceM,
				"elevation_gain":  elevationGainM,
				"elapsed_seconds": elapsedSeconds,
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

			var activities []pggeo.ActivityWithMatch
			err := s.withDB(func(conn *pgx.Conn) error {
				var dbErr error
				activities, dbErr = pggeo.GetActivitiesForSegment(s.ctx, conn, s.user.ID, segmentID, tolerance, sortBy, forceRefresh)
				return dbErr
			})
			if err != nil {
				log.Printf("❌ Failed to load activities for segment %d: %v", segmentID, err)
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
				return
			}
			if s.token != "" {
				if zones, err := strava.FetchHeartRateZones(s.token); err == nil && zones != nil {
					for i := range activities {
						activityID := activities[i].ID
						zoneErr := s.withDB(func(conn *pgx.Conn) error {
							var dbErr error
							activities[i].SegmentHRZones, dbErr = pggeo.GetHRZoneDistributionForSegmentInActivity(s.ctx, conn, s.user.ID, activityID, segmentID, tolerance, &zones.HeartRate)
							return dbErr
						})
						if zoneErr != nil {
							log.Printf("⚠️ Failed to calculate segment HR zones for segment %d activity %d: %v", segmentID, activityID, zoneErr)
						}
					}
				} else if err != nil {
					log.Printf("⚠️ Failed to fetch HR zones for segment efforts: %v", err)
				}
			}
			writeJSON(w, activities)
			return
		}
		// Regular GET /api/segments/:id
		var segment *pggeo.FavoriteSegment
		err = s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			segment, dbErr = pggeo.GetFavoriteSegment(s.ctx, conn, segmentID)
			return dbErr
		})
		if err != nil {
			log.Printf("❌ Failed to load segment %d: %v", segmentID, err)
			s.handleDBPageError(w, r, err, http.StatusNotFound)
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
		var segment *pggeo.FavoriteSegment
		err = s.withDB(func(conn *pgx.Conn) error {
			var dbErr error
			segment, dbErr = pggeo.GetFavoriteSegment(s.ctx, conn, segmentID)
			return dbErr
		})
		if err != nil {
			log.Printf("❌ Failed to load segment %d for delete: %v", segmentID, err)
			s.handleDBPageError(w, r, err, http.StatusNotFound)
			return
		}
		if segment.AthleteID != s.user.ID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		err = s.withDB(func(conn *pgx.Conn) error {
			return pggeo.DeleteFavoriteSegment(s.ctx, conn, segmentID)
		})
		if err != nil {
			log.Printf("❌ Failed to delete segment %d: %v", segmentID, err)
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
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

	var segments []pggeo.SegmentDashboardSummary
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segments, dbErr = pggeo.ListSegmentDashboardSummaries(s.ctx, conn, s.user.ID, 15.0)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}

	data := struct {
		Segments     []pggeo.SegmentDashboardSummary
		Athlete      *strava.Athlete
		ShowLoginCTA bool
		Authorized   bool
	}{
		Segments:     segments,
		Athlete:      s.user,
		ShowLoginCTA: s.token == "" && s.cfg.StravaClientID != "",
		Authorized:   s.token != "",
	}

	if err := s.executeTemplate(w, "segments.html", data); err != nil {
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
	var segment *pggeo.FavoriteSegment
	err = s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segment, dbErr = pggeo.GetFavoriteSegment(s.ctx, conn, segmentID)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusNotFound)
		return
	}

	// Verify ownership
	if segment.AthleteID != s.user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	data := struct {
		Segment             *pggeo.FavoriteSegment
		Athlete             *strava.Athlete
		ShowLoginCTA        bool
		Authorized          bool
		MapboxToken         string
		MobileActivityOrder string
	}{
		Segment:             segment,
		Athlete:             s.user,
		ShowLoginCTA:        s.token == "" && s.cfg.StravaClientID != "",
		Authorized:          s.token != "",
		MapboxToken:         s.cfg.MapboxToken,
		MobileActivityOrder: s.cfg.MobileActivityOrder,
	}

	if err := s.executeTemplate(w, "segment.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type profileBikeStat struct {
	GearID     string
	Label      string
	DistanceKM float64
	Activities int
}

type profilePeriodStat struct {
	Label      string
	Activities int
}

type profileHRZone struct {
	Label string
	Range string
}

func (s *server) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	s.ensureSessionFromRequest(r)
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var activities []strava.ActivitySummary
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activities, dbErr = pggeo.GetAllActivities(s.ctx, conn, s.user.ID)
		return dbErr
	})
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	activities = s.enrichGearNames(activities)

	var zones []profileHRZone
	var zonesError string
	if s.token != "" {
		athleteZones, err := strava.FetchHeartRateZones(s.token)
		if err != nil {
			zonesError = err.Error()
		} else if athleteZones != nil {
			for i, zone := range athleteZones.HeartRate.Zones {
				zones = append(zones, profileHRZone{
					Label: fmt.Sprintf("Z%d", i+1),
					Range: formatHRZoneRange(zone),
				})
			}
		}
	}

	bikeStats, totalBikeKM := buildBikeStats(activities)
	bestMonth, bestYear := findBusiestPeriods(activities)

	data := struct {
		Athlete           *strava.Athlete
		ShowLoginCTA      bool
		Authorized        bool
		HRZones           []profileHRZone
		HRZonesError      string
		TotalBikeKM       float64
		TotalActivities   int
		BikeStats         []profileBikeStat
		BestMonth         profilePeriodStat
		BestYear          profilePeriodStat
		HasRecordedRides  bool
		HasRecordedMonths bool
	}{
		Athlete:           s.user,
		ShowLoginCTA:      s.token == "" && s.cfg.StravaClientID != "",
		Authorized:        s.token != "",
		HRZones:           zones,
		HRZonesError:      zonesError,
		TotalBikeKM:       totalBikeKM,
		TotalActivities:   len(activities),
		BikeStats:         bikeStats,
		BestMonth:         bestMonth,
		BestYear:          bestYear,
		HasRecordedRides:  len(bikeStats) > 0,
		HasRecordedMonths: bestMonth.Activities > 0 || bestYear.Activities > 0,
	}

	if err := s.executeTemplate(w, "profile.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func buildBikeStats(activities []strava.ActivitySummary) ([]profileBikeStat, float64) {
	byGear := make(map[string]*profileBikeStat)
	totalKM := 0.0
	for _, activity := range activities {
		if !isBikeActivity(activity) {
			continue
		}
		gearID := strings.TrimSpace(activity.GearID)
		if gearID == "" {
			gearID = "unknown"
		}
		stat := byGear[gearID]
		if stat == nil {
			label := gearID
			if gearID == "unknown" {
				label = "No bike recorded"
			} else if activity.GearName != nil && strings.TrimSpace(*activity.GearName) != "" {
				label = strings.TrimSpace(*activity.GearName)
			}
			stat = &profileBikeStat{GearID: gearID, Label: label}
			byGear[gearID] = stat
		} else if stat.Label == gearID && activity.GearName != nil && strings.TrimSpace(*activity.GearName) != "" {
			stat.Label = strings.TrimSpace(*activity.GearName)
		}
		distanceKM := activity.Distance / 1000
		stat.DistanceKM += distanceKM
		stat.Activities++
		totalKM += distanceKM
	}

	stats := make([]profileBikeStat, 0, len(byGear))
	for _, stat := range byGear {
		stats = append(stats, *stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].DistanceKM > stats[j].DistanceKM
	})
	return stats, totalKM
}

func isBikeActivity(activity strava.ActivitySummary) bool {
	kind := strings.ToLower(activity.Type + " " + activity.SportType)
	return strings.Contains(kind, "ride") || strings.Contains(kind, "bike") || strings.Contains(kind, "cycling")
}

func findBusiestPeriods(activities []strava.ActivitySummary) (profilePeriodStat, profilePeriodStat) {
	months := make(map[string]int)
	years := make(map[string]int)
	monthLabels := make(map[string]string)

	for _, activity := range activities {
		if activity.StartDateTime.IsZero() {
			continue
		}
		monthKey := activity.StartDateTime.Format("2006-01")
		yearKey := activity.StartDateTime.Format("2006")
		months[monthKey]++
		years[yearKey]++
		monthLabels[monthKey] = activity.StartDateTime.Format("January 2006")
	}

	bestMonth := profilePeriodStat{}
	bestMonthKey := ""
	for key, count := range months {
		if count > bestMonth.Activities || (count == bestMonth.Activities && key > bestMonthKey) {
			bestMonth = profilePeriodStat{Label: monthLabels[key], Activities: count}
			bestMonthKey = key
		}
	}

	bestYear := profilePeriodStat{}
	bestYearKey := ""
	for key, count := range years {
		if count > bestYear.Activities || (count == bestYear.Activities && key > bestYearKey) {
			bestYear = profilePeriodStat{Label: key, Activities: count}
			bestYearKey = key
		}
	}

	return bestMonth, bestYear
}

func formatHRZoneRange(zone strava.HRZone) string {
	switch {
	case zone.Min > 0 && zone.Max > 0:
		return fmt.Sprintf("%d-%d bpm", zone.Min, zone.Max)
	case zone.Min > 0:
		return fmt.Sprintf("%d+ bpm", zone.Min)
	case zone.Max > 0:
		return fmt.Sprintf("up to %d bpm", zone.Max)
	default:
		return "not set"
	}
}
