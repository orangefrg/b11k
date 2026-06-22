package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"math"
	"net"
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
	StravaClientID                 string
	StravaClientSecret             string
	StravaRedirectURI              string
	IOSRedirectURI                 string
	PGIP                           string
	PGPort                         string
	PGUser                         string
	PGPassword                     string
	PGDatabase                     string
	WebHost                        string
	PublicAPIHost                  string
	WebPort                        string
	WebProtocol                    string
	TokenEncryptionKey             string
	DevReloadTemplates             bool
	MobileActivityOrder            string
	DiscoveredMapEnabled           bool
	DiscoveredRevealRadiusMeters   float64
	DiscoveredSampleDistanceMeters float64
}

type server struct {
	ctx    context.Context
	cfg    Config
	conn   *pgx.Conn
	connMu syncpkg.Mutex // Mutex to serialize database access (single connection)
	tmpl   *template.Template
	token  string
	user   *strava.Athlete

	mobileMu          syncpkg.Mutex
	mobileSessions    map[string]mobileSession
	mobileAuthStates  map[string]time.Time
	mobileAuthResults map[string]mobileAuthResult
	rateMu            syncpkg.Mutex
	rateLimits        map[string]rateLimitEntry
	secretBox         *secretBox
}

const stravaTokenCookieName = "strava_token" // #nosec G101 -- cookie name only; not a credential value.
const mobileSessionLifetime = 90 * 24 * time.Hour

type mobileSession struct {
	SessionToken     string
	Token            string
	RefreshToken     string
	ExpiresAt        time.Time
	SessionExpiresAt time.Time
	Athlete          *strava.Athlete
	CreatedAt        time.Time
}

type mobileAuthResult struct {
	SessionToken string
	Athlete      *strava.Athlete
	Error        string
	ExpiresAt    time.Time
}

type rateLimitEntry struct {
	Count   int
	ResetAt time.Time
}

func RunServer(ctx context.Context, cfg Config) {
	log.Printf("🌐 Starting web server on port %s", cfg.WebPort)

	secretBox, err := newSecretBox(cfg.TokenEncryptionKey)
	if err != nil {
		log.Fatalf("Invalid token encryption key: %v", err)
	}
	if requiresTokenEncryption(cfg) && secretBox == nil {
		log.Fatalf("B11K_TOKEN_ENCRYPTION_KEY is required when exposing the mobile API over public HTTPS")
	}

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

	s := &server{
		ctx:               ctx,
		cfg:               cfg,
		conn:              conn,
		tmpl:              tmpl,
		mobileSessions:    make(map[string]mobileSession),
		mobileAuthStates:  make(map[string]time.Time),
		mobileAuthResults: make(map[string]mobileAuthResult),
		rateLimits:        make(map[string]rateLimitEntry),
		secretBox:         secretBox,
	}
	if cfg.DevReloadTemplates {
		log.Printf("🔁 Dev template reload enabled")
	}
	if secretBox != nil {
		log.Printf("🔒 Strava token encryption at rest enabled")
	}
	if cfg.PublicAPIHost != "" {
		log.Printf("🔐 Public API host configured: %s", cfg.PublicAPIHost)
	}

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/strava/", s.handleStravaHome)
	mux.HandleFunc("/strava/login", s.handleStravaLogin)
	mux.HandleFunc("/activity/", s.handleActivity)
	mux.HandleFunc("/api/activities", s.handleActivitiesAPI)
	mux.HandleFunc("/api/activities/", s.handleActivityPointsAPI)
	mux.HandleFunc("/strava/callback", s.handleStravaCallback)
	mux.HandleFunc("/strava/logout", s.handleStravaLogout)
	mux.HandleFunc("/api/hrzones", s.handleHRZones)
	mux.HandleFunc("/api/mobile/auth/start", s.handleMobileAuthStart)
	mux.HandleFunc("/api/mobile/auth/exchange", s.handleMobileAuthExchange)
	mux.HandleFunc("/api/mobile/auth/callback", s.handleMobileAuthCallback)
	mux.HandleFunc("/api/mobile/auth/session", s.handleMobileAuthSession)
	mux.HandleFunc("/api/mobile/me", s.handleMobileMe)
	mux.HandleFunc("/api/mobile/profile", s.handleMobileProfile)
	mux.HandleFunc("/api/mobile/logout", s.handleMobileLogout)
	mux.HandleFunc("/api/mobile/sync", s.handleMobileSync)
	mux.HandleFunc("/api/mobile/activities", s.handleMobileActivities)
	mux.HandleFunc("/api/mobile/activities/", s.handleMobileActivities)
	mux.HandleFunc("/api/mobile/segments", s.handleMobileSegments)
	mux.HandleFunc("/api/mobile/segments/", s.handleMobileSegments)
	mux.HandleFunc("/strava/sync", s.handleStravaSyncSSE)
	mux.HandleFunc("/api/segments", s.handleSegmentsAPI)
	mux.HandleFunc("/api/segments/", s.handleSegmentAPI)
	mux.HandleFunc("/segments", s.handleSegmentsPage)
	mux.HandleFunc("/segment/", s.handleSegmentPage)
	mux.HandleFunc("/profile", s.handleProfilePage)
	if cfg.DiscoveredMapEnabled {
		mux.HandleFunc("/api/mobile/discovered/", s.handleMobileDiscovered)
		mux.HandleFunc("/discovered", s.handleDiscoveredPage)
		mux.HandleFunc("/api/discovered/", s.handleDiscoveredAPI)
	}

	// static
	mux.Handle("/static/", http.StripPrefix("/static/", s.staticFileServer()))

	addr := ":" + strings.TrimPrefix(cfg.WebPort, ":")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.securityMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func (s *server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")

		if !s.isRequestAllowed(r) {
			http.NotFound(w, r)
			return
		}
		if !s.isPublicRequestTransportAllowed(r) {
			http.Error(w, "HTTPS is required", http.StatusForbidden)
			return
		}
		if s.isDisallowedBrowserMobileAPIRequest(r) {
			http.Error(w, "browser origins are not allowed for mobile API", http.StatusForbidden)
			return
		}
		if !s.allowRequestRate(w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/mobile/") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			}
		}
		if s.isHTTPSRequest(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) staticFileServer() http.Handler {
	files := http.FileServer(http.Dir(filepath.FromSlash("web/static")))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.DevReloadTemplates || isLocalOrPrivateRequest(r) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *server) isRequestAllowed(r *http.Request) bool {
	path := r.URL.Path
	if path == "/api/mobile/auth/callback" {
		return true
	}
	apiHost := normalizeHost(s.cfg.PublicAPIHost)
	webHost := normalizeHost(s.cfg.WebHost)
	if apiHost == "" && webHost == "" {
		return true
	}
	host := requestHost(r)
	if host == "" {
		return true
	}
	if isLocalOrPrivateHost(host) {
		return isLocalOrPrivateRequest(r)
	}
	if apiHost != "" && host == apiHost {
		return strings.HasPrefix(path, "/api/mobile/")
	}
	if strings.HasPrefix(path, "/api/mobile/") {
		return false
	}
	if webHost != "" && !isLocalOrPrivateHost(webHost) && host != webHost {
		return false
	}
	return true
}

func (s *server) isPublicRequestTransportAllowed(r *http.Request) bool {
	if isLocalOrPrivateRequest(r) {
		return true
	}
	if s.cfg.WebProtocol != "https" {
		return true
	}
	return s.isHTTPSRequest(r)
}

func (s *server) isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !forwardedHeadersTrusted(r) {
		return false
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	if strings.Contains(strings.ToLower(r.Header.Get("CF-Visitor")), `"scheme":"https"`) {
		return true
	}
	return false
}

func (s *server) isDisallowedBrowserMobileAPIRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/mobile/") {
		return false
	}
	if r.URL.Path == "/api/mobile/auth/callback" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	return origin != ""
}

func (s *server) allowRequestRate(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/mobile/") && path != "/strava/login" && path != "/strava/callback" {
		return true
	}

	limit := 180
	window := time.Minute
	bucket := "mobile"
	switch {
	case strings.HasPrefix(path, "/api/mobile/auth/"):
		limit = 40
		bucket = "mobile-auth"
	case path == "/api/mobile/sync":
		limit = 12
		window = time.Hour
		bucket = "mobile-sync"
	case path == "/api/mobile/discovered/rebuild":
		limit = 6
		window = time.Hour
		bucket = "mobile-discovered-rebuild"
	case path == "/api/mobile/discovered/fog" || path == "/api/mobile/discovered/coverage":
		limit = 120
		bucket = "mobile-map"
	case path == "/strava/login" || path == "/strava/callback":
		limit = 40
		bucket = "web-auth"
	}

	key := clientIP(r) + ":" + bucket
	ok, retryAfter := s.consumeRateLimit(key, limit, window)
	if ok {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	return false
}

func (s *server) consumeRateLimit(key string, limit int, window time.Duration) (bool, time.Duration) {
	now := time.Now()
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	if s.rateLimits == nil {
		s.rateLimits = make(map[string]rateLimitEntry)
	}
	entry := s.rateLimits[key]
	if entry.ResetAt.IsZero() || now.After(entry.ResetAt) {
		s.rateLimits[key] = rateLimitEntry{Count: 1, ResetAt: now.Add(window)}
		s.pruneRateLimitsLocked(now)
		return true, 0
	}
	if entry.Count >= limit {
		return false, time.Until(entry.ResetAt)
	}
	entry.Count++
	s.rateLimits[key] = entry
	return true, 0
}

func (s *server) pruneRateLimitsLocked(now time.Time) {
	if len(s.rateLimits) < 1000 {
		return
	}
	for key, entry := range s.rateLimits {
		if now.After(entry.ResetAt) {
			delete(s.rateLimits, key)
		}
	}
}

func requestHost(r *http.Request) string {
	host := normalizeHost(r.Host)
	if host != "" && !isLocalOrPrivateHost(host) {
		return host
	}
	if forwardedHeadersTrusted(r) {
		if forwardedHost := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			return normalizeHost(forwardedHost)
		}
	}
	return host
}

func clientIP(r *http.Request) string {
	if forwardedHeadersTrusted(r) {
		if value := firstHeaderValue(r.Header.Get("CF-Connecting-IP")); value != "" {
			return value
		}
		if value := firstHeaderValue(r.Header.Get("X-Forwarded-For")); value != "" {
			return value
		}
	}
	return remoteHost(r)
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func firstHeaderValue(value string) string {
	return strings.TrimSpace(strings.Split(strings.TrimSpace(value), ",")[0])
}

func safeLogText(value string) string {
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\r", "\\r")
}

func forwardedHeadersTrusted(r *http.Request) bool {
	remote := remoteHost(r)
	return remote != "" && isLocalOrPrivateHost(remote)
}

func isLocalOrPrivateRequest(r *http.Request) bool {
	remote := remoteHost(r)
	return remote != "" && isLocalOrPrivateHost(requestHost(r)) && isLocalOrPrivateHost(remote)
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	} else if strings.HasPrefix(host, "[") {
		host = strings.Trim(host, "[]")
	} else if strings.Count(host, ":") == 1 {
		host = strings.Split(host, ":")[0]
	}
	return strings.Trim(host, "[]")
}

func isLocalOrPrivateHost(host string) bool {
	host = normalizeHost(host)
	if host == "localhost" || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
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
		filepath.FromSlash("web/templates/discovered.html"),
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
	return fmt.Sprintf("%s%sv=%d", path, separator, info.ModTime().UnixNano())
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
	// #nosec G706 -- request path is escaped before logging.
	log.Printf("⚠️ Database still recovering for %s: %v", safeLogText(r.URL.Path), err)
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
		Activities           []strava.ActivitySummary
		ShowLoginCTA         bool
		Authorized           bool
		Athlete              *strava.Athlete
		CurrentPage          int
		TotalPages           int
		HasNext              bool
		HasPrev              bool
		PerPage              int
		DiscoveredMapEnabled bool
	}{
		Activities:           pageItems,
		ShowLoginCTA:         s.token == "" && s.cfg.StravaClientID != "",
		Authorized:           s.token != "",
		Athlete:              s.user,
		CurrentPage:          page,
		TotalPages:           totalPages,
		HasNext:              page < totalPages,
		HasPrev:              page > 1,
		PerPage:              perPage,
		DiscoveredMapEnabled: s.cfg.DiscoveredMapEnabled,
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
		Activity             strava.ActivitySummary
		ActivityHRZones      []pggeo.HRZoneDistribution
		Athlete              *strava.Athlete
		ShowLoginCTA         bool
		Authorized           bool
		MobileActivityOrder  string
		DiscoveredMapEnabled bool
	}{
		Activity:             *activity,
		ActivityHRZones:      activityHRZones,
		Athlete:              s.user,
		ShowLoginCTA:         s.token == "" && s.cfg.StravaClientID != "",
		Authorized:           s.token != "",
		MobileActivityOrder:  s.cfg.MobileActivityOrder,
		DiscoveredMapEnabled: s.cfg.DiscoveredMapEnabled,
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
		DiscoveredMap: sync.DiscoveredMapConfig{
			Enabled:              s.cfg.DiscoveredMapEnabled,
			RevealRadiusMeters:   s.cfg.DiscoveredRevealRadiusMeters,
			SampleDistanceMeters: s.cfg.DiscoveredSampleDistanceMeters,
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

	authCfg := strava.NewStravaAuthConfig(s.cfg.StravaClientID, s.cfg.StravaClientSecret, s.cfg.StravaRedirectURI)
	tok, err := strava.ExchangeCodeForToken(*authCfg, code)
	if err != nil {
		log.Printf("❌ Token exchange error: %v", err)
		log.Printf("💡 Check that your Strava app's redirect URI matches: %s", s.cfg.StravaRedirectURI)
		http.Error(w, "Strava login could not be completed. Check the server logs for details.", http.StatusBadGateway)
		return
	}
	s.token = tok

	// #nosec G124 -- local HTTP needs an insecure cookie; production HTTPS requests set Secure.
	http.SetCookie(w, &http.Cookie{
		Name:     stravaTokenCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(r),
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

	// #nosec G124 -- local HTTP needs an insecure cookie; production HTTPS requests set Secure.
	http.SetCookie(w, &http.Cookie{
		Name:     stravaTokenCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(r),
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

func (s *server) secureCookies(r *http.Request) bool {
	return s.cfg.WebProtocol == "https" || s.isHTTPSRequest(r)
}

func (s *server) handleDiscoveredPage(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DiscoveredMapEnabled {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/discovered" {
		http.NotFound(w, r)
		return
	}
	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}

	data := struct {
		Athlete                        *strava.Athlete
		ShowLoginCTA                   bool
		Authorized                     bool
		DiscoveredMapEnabled           bool
		DiscoveredRevealRadiusMeters   float64
		DiscoveredSampleDistanceMeters float64
	}{
		Athlete:                        scope.Athlete,
		ShowLoginCTA:                   scope.StravaToken == "" && s.cfg.StravaClientID != "",
		Authorized:                     scope.StravaToken != "",
		DiscoveredMapEnabled:           s.cfg.DiscoveredMapEnabled,
		DiscoveredRevealRadiusMeters:   s.cfg.DiscoveredRevealRadiusMeters,
		DiscoveredSampleDistanceMeters: s.cfg.DiscoveredSampleDistanceMeters,
	}

	if err := s.executeTemplate(w, "discovered.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) handleDiscoveredAPI(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DiscoveredMapEnabled {
		http.NotFound(w, r)
		return
	}
	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}

	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/discovered/"), "/")
	switch action {
	case "status":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status, err := s.discoveredCoverageStatus(scope.AthleteID)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, status)
	case "rebuild":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status, err := s.rebuildDiscoveredCoverage(scope.AthleteID)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, status)
	case "fog":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		minLng, minLat, maxLng, maxLat, ok := parseBBox(r.URL.Query().Get("bbox"))
		if !ok {
			http.Error(w, "bbox must be minLng,minLat,maxLng,maxLat", http.StatusBadRequest)
			return
		}
		featureCollection, err := s.discoveredFogFeatureCollection(scope.AthleteID, minLng, minLat, maxLng, maxLat)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(featureCollection))
	case "coverage":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		minLng, minLat, maxLng, maxLat, ok := parseBBox(r.URL.Query().Get("bbox"))
		if !ok {
			http.Error(w, "bbox must be minLng,minLat,maxLng,maxLat", http.StatusBadRequest)
			return
		}
		featureCollection, err := s.discoveredCoverageFeatureCollection(scope.AthleteID, minLng, minLat, maxLng, maxLat)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(featureCollection))
	default:
		http.NotFound(w, r)
	}
}

func parseBBox(raw string) (float64, float64, float64, float64, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	values := make([]float64, 4)
	for i, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, 0, 0, 0, false
		}
		values[i] = value
	}
	if values[0] < -180 || values[0] > 180 || values[2] < -180 || values[2] > 180 {
		return 0, 0, 0, 0, false
	}
	if values[1] < -90 || values[1] > 90 || values[3] < -90 || values[3] > 90 {
		return 0, 0, 0, 0, false
	}
	if values[0] >= values[2] || values[1] >= values[3] {
		return 0, 0, 0, 0, false
	}
	return values[0], values[1], values[2], values[3], true
}

// handleSegmentsAPI handles GET /api/segments and POST /api/segments
func (s *server) handleSegmentsAPI(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case "GET":
		segments, err := s.listFavoriteSegments(scope.AthleteID)
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

		segment, err := s.createFavoriteSegmentFromActivityRange(scope.AthleteID, req.ActivityID, req.Name, req.Description, req.StartIndex, req.EndIndex)
		if err != nil {
			if errors.Is(err, errSegmentIndexOutOfRange) {
				http.Error(w, "index out of range", http.StatusBadRequest)
				return
			}
			if errors.Is(err, errActivitySamplesMissing) {
				s.handleDBPageError(w, r, err, http.StatusNotFound)
				return
			}
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

	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}

	segment, err := s.getOwnedFavoriteSegment(scope.AthleteID, segmentID)
	if err != nil {
		if errors.Is(err, errForbidden) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		log.Printf("❌ Failed to load segment %d: %v", segmentID, err)
		s.handleDBPageError(w, r, err, http.StatusNotFound)
		return
	}

	switch r.Method {
	case "GET":
		// Handle GET /api/segments/:id/graph
		if len(parts) == 2 && parts[1] == "graph" {
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
				zones, err := strava.FetchHeartRateZones(scope.StravaToken)
				if err == nil && zones != nil {
					hrZones = &zones.HeartRate
				}
			}

			var graphData *pggeo.GraphData
			err = s.withDB(func(conn *pgx.Conn) error {
				var dbErr error
				graphData, dbErr = pggeo.GetGraphDataForSegmentInActivity(s.ctx, conn, scope.AthleteID, activityID, segmentID, metrics, includeZones, hrZones)
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
		if len(parts) == 2 && parts[1] == "metrics" {
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
		if len(parts) == 4 && parts[1] == "activity" && parts[3] == "indices" {
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
				return conn.QueryRow(s.ctx, query, segmentID, activityID, scope.AthleteID, tolerance).Scan(&startIndex, &endIndex)
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
		if len(parts) == 4 && parts[1] == "activity" && parts[3] == "metrics" {
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
				return conn.QueryRow(s.ctx, query, segmentID, activityID, scope.AthleteID, tolerance).Scan(&avgHR, &avgSpeed, &distanceM, &elevationGainM, &elapsedSeconds)
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
				if err := conn.QueryRow(s.ctx, idxQuery, segmentID, activityID, scope.AthleteID, tolerance).Scan(&startIndex, &endIndex); err != nil {
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
		if len(parts) == 2 && parts[1] == "activities" {
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
				activities, dbErr = pggeo.GetActivitiesForSegment(s.ctx, conn, scope.AthleteID, segmentID, tolerance, sortBy, forceRefresh)
				return dbErr
			})
			if err != nil {
				log.Printf("❌ Failed to load activities for segment %d: %v", segmentID, err)
				s.handleDBPageError(w, r, err, http.StatusInternalServerError)
				return
			}
			if scope.StravaToken != "" {
				if zones, err := strava.FetchHeartRateZones(scope.StravaToken); err == nil && zones != nil {
					for i := range activities {
						activityID := activities[i].ID
						zoneErr := s.withDB(func(conn *pgx.Conn) error {
							var dbErr error
							activities[i].SegmentHRZones, dbErr = pggeo.GetHRZoneDistributionForSegmentInActivity(s.ctx, conn, scope.AthleteID, activityID, segmentID, tolerance, &zones.HeartRate)
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
		if len(parts) != 1 {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, segment)
	case "DELETE":
		if len(parts) != 1 {
			http.NotFound(w, r)
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
	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}

	segments, err := s.listSegmentDashboardSummaries(scope.AthleteID, 15.0)
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}

	data := struct {
		Segments             []pggeo.SegmentDashboardSummary
		Athlete              *strava.Athlete
		ShowLoginCTA         bool
		Authorized           bool
		DiscoveredMapEnabled bool
	}{
		Segments:             segments,
		Athlete:              scope.Athlete,
		ShowLoginCTA:         scope.StravaToken == "" && s.cfg.StravaClientID != "",
		Authorized:           scope.StravaToken != "",
		DiscoveredMapEnabled: s.cfg.DiscoveredMapEnabled,
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

	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}

	segment, err := s.getOwnedFavoriteSegment(scope.AthleteID, segmentID)
	if err != nil {
		if errors.Is(err, errForbidden) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		s.handleDBPageError(w, r, err, http.StatusNotFound)
		return
	}

	data := struct {
		Segment              *pggeo.FavoriteSegment
		Athlete              *strava.Athlete
		ShowLoginCTA         bool
		Authorized           bool
		MobileActivityOrder  string
		DiscoveredMapEnabled bool
	}{
		Segment:              segment,
		Athlete:              scope.Athlete,
		ShowLoginCTA:         scope.StravaToken == "" && s.cfg.StravaClientID != "",
		Authorized:           scope.StravaToken != "",
		MobileActivityOrder:  s.cfg.MobileActivityOrder,
		DiscoveredMapEnabled: s.cfg.DiscoveredMapEnabled,
	}

	if err := s.executeTemplate(w, "segment.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type profileBikeStat struct {
	GearID     string  `json:"gear_id"`
	Label      string  `json:"label"`
	DistanceKM float64 `json:"distance_km"`
	Activities int     `json:"activities"`
}

type profilePeriodStat struct {
	Label      string `json:"label"`
	Activities int    `json:"activities"`
}

type profileHRZone struct {
	Label string `json:"label"`
	Range string `json:"range"`
}

type profileData struct {
	Athlete              *strava.Athlete   `json:"athlete"`
	ShowLoginCTA         bool              `json:"show_login_cta"`
	Authorized           bool              `json:"authorized"`
	HRZones              []profileHRZone   `json:"hr_zones"`
	HRZonesError         string            `json:"hr_zones_error,omitempty"`
	TotalBikeKM          float64           `json:"total_bike_km"`
	TotalActivities      int               `json:"total_activities"`
	BikeStats            []profileBikeStat `json:"bike_stats"`
	BestMonth            profilePeriodStat `json:"best_month"`
	BestYear             profilePeriodStat `json:"best_year"`
	HasRecordedRides     bool              `json:"has_recorded_rides"`
	HasRecordedMonths    bool              `json:"has_recorded_months"`
	DiscoveredMapEnabled bool              `json:"discovered_map_enabled"`
}

func (s *server) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.webScopeFromRequest(w, r)
	if !ok {
		return
	}
	data, err := s.buildProfileData(scope)
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}

	if err := s.executeTemplate(w, "profile.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) buildProfileData(scope athleteScope) (profileData, error) {
	if scope.AthleteID == 0 || scope.Athlete == nil {
		return profileData{}, fmt.Errorf("profile requires an authenticated athlete")
	}

	var activities []strava.ActivitySummary
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		activities, dbErr = pggeo.GetAllActivities(s.ctx, conn, scope.AthleteID)
		return dbErr
	})
	if err != nil {
		return profileData{}, err
	}
	activities = s.enrichGearNames(activities)

	zones, zonesError := buildProfileHRZones(scope.StravaToken)
	bikeStats, totalBikeKM := buildBikeStats(activities)
	bestMonth, bestYear := findBusiestPeriods(activities)

	return profileData{
		Athlete:              scope.Athlete,
		ShowLoginCTA:         scope.StravaToken == "" && s.cfg.StravaClientID != "",
		Authorized:           scope.StravaToken != "",
		HRZones:              zones,
		HRZonesError:         zonesError,
		TotalBikeKM:          totalBikeKM,
		TotalActivities:      len(activities),
		BikeStats:            bikeStats,
		BestMonth:            bestMonth,
		BestYear:             bestYear,
		HasRecordedRides:     len(bikeStats) > 0,
		HasRecordedMonths:    bestMonth.Activities > 0 || bestYear.Activities > 0,
		DiscoveredMapEnabled: s.cfg.DiscoveredMapEnabled,
	}, nil
}

func buildProfileHRZones(token string) ([]profileHRZone, string) {
	var zones []profileHRZone
	var zonesError string
	if token != "" {
		athleteZones, err := strava.FetchHeartRateZones(token)
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
	return zones, zonesError
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
