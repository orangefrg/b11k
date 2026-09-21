package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	syncer "b11k/internal/sync"
	"b11k/internal/testdb"
)

func syncServer(t *testing.T) *server {
	t.Helper()
	conn, connect := testdb.Open(t)
	if err := pggeo.CreateTables(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	box, err := newSecretBox(testTokenEncryptionKey())
	if err != nil {
		t.Fatal(err)
	}
	s := &server{ctx: context.Background(), conn: conn, syncConnect: connect, secretBox: box, mobileSessions: map[string]mobileSession{}}
	for _, token := range []string{"deviceOne", "deviceTwo"} {
		session := mobileSession{SessionToken: token, Token: "expired-access", RefreshToken: "old-refresh", Athlete: &strava.Athlete{ID: 17}, ExpiresAt: time.Now().Add(-time.Hour), SessionExpiresAt: time.Now().Add(time.Hour)}
		if err = s.saveMobileSession(session); err != nil {
			t.Fatal(err)
		}
		s.mobileSessions[token] = session
	}
	return s
}

type refreshTransport func(*http.Request) (*http.Response, error)

func (f refreshTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSyncRefreshSerializesAcrossDevicesAndPersistsRotation(t *testing.T) {
	s := syncServer(t)
	var calls atomic.Int32
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = refreshTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "refresh_token=old-refresh") {
			t.Errorf("unexpected refresh input")
		}
		encoded, _ := json.Marshal(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_at": time.Now().Add(6 * time.Hour).Unix()})
		response := string(encoded)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			token, err := s.syncToken(context.Background(), 17, false)
			if err == nil && token != "new-access" {
				t.Errorf("wrong token")
			}
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh raced: %d calls", calls.Load())
	}
	for _, key := range []string{"deviceOne", "deviceTwo"} {
		session, err := s.loadMobileSession(key)
		if err != nil {
			t.Fatal(err)
		}
		if session.RefreshToken != "new-refresh" || session.Token != "new-access" {
			t.Fatal("credentials diverged")
		}
	}
}

func TestSyncJobAPIRecoversStatusWithoutCallingStrava(t *testing.T) {
	s := syncServer(t)
	request := func(method, path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.handleMobileSyncJobs(w, r)
		return w
	}
	if w := request("GET", "/api/mobile/sync/jobs", ""); w.Code != 401 {
		t.Fatalf("unauthenticated status %d", w.Code)
	}
	// Expired Strava credentials must not hide a saved job behind an auth refresh.
	w := request("POST", "/api/mobile/sync/jobs", "deviceOne")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Job syncer.Job `json:"job"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	id := response.Job.ID
	w = request("POST", "/api/mobile/sync/jobs", "deviceTwo")
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Job.ID != id {
		t.Fatal("second device started duplicate")
	}
	s.mobileSessions["differentAthlete"] = mobileSession{Token: "other", Athlete: &strava.Athlete{ID: 18}, SessionExpiresAt: time.Now().Add(time.Hour)}
	for _, suffix := range []string{"", "/cancel", "/retry"} {
		method := "POST"
		if suffix == "" {
			method = "GET"
		}
		if w = request(method, "/api/mobile/sync/jobs/"+id+suffix, "differentAthlete"); w.Code != 404 {
			t.Fatalf("cross-athlete %s: %d", suffix, w.Code)
		}
	}
	if w = request("POST", "/api/mobile/sync/jobs/"+id+"/cancel", "deviceOne"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request("POST", "/api/mobile/sync/jobs?start=invalid", "deviceOne"); w.Code != 400 {
		t.Fatalf("invalid date %d", w.Code)
	}
}
func TestSyncDateRangeIncludesEndDayAndRejectsInvalidDates(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/mobile/sync/jobs?start=2026-09-20&end=2026-09-20", nil)
	start, end, err := syncTimeframe(r)
	if err != nil || end.Sub(start) != 24*time.Hour {
		t.Fatalf("end date excluded: %v %v %v", start, end, err)
	}
	for _, query := range []string{"start=2026-99-99", "start=2026-09-22&end=2026-09-20"} {
		if _, _, err = syncTimeframe(httptest.NewRequest("POST", "/?"+query, nil)); err == nil {
			t.Fatal("accepted invalid range")
		}
	}
}

func TestMobileRestorationKeepsSavedDataAvailableWhenStravaNeedsReconnect(t *testing.T) {
	s := syncServer(t)
	var calls atomic.Int32
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = refreshTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"Authorization Error"}`))}, nil
	})
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/mobile/me", s.handleMobileMe},
		{"/api/mobile/activities", s.handleMobileActivities},
		{"/api/mobile/sync/jobs", s.handleMobileSyncJobs},
	} {
		r := httptest.NewRequest("GET", route.path, nil)
		r.Header.Set("Authorization", "Bearer deviceOne")
		w := httptest.NewRecorder()
		route.handler(w, r)
		if w.Code != 200 {
			t.Fatalf("restoring %s failed: %d %s", route.path, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("restoring saved data called Strava")
	}
	r := httptest.NewRequest("GET", "/api/mobile/profile", nil)
	r.Header.Set("Authorization", "Bearer deviceOne")
	w := httptest.NewRecorder()
	s.handleMobileProfile(w, r)
	if w.Code != 200 || calls.Load() != 1 {
		t.Fatalf("optional Strava data invalidated profile: %d %s (%d calls)", w.Code, w.Body.String(), calls.Load())
	}
	// A B11K session's own expiry still prevents access, independently of Strava.
	session := s.mobileSessions["deviceOne"]
	session.SessionExpiresAt = time.Now().Add(-time.Minute)
	s.mobileSessions["deviceOne"] = session
	for _, handler := range []http.HandlerFunc{s.handleMobileMe, s.handleMobileActivities, s.handleMobileSyncJobs} {
		w = httptest.NewRecorder()
		handler(w, r)
		if w.Code != 401 {
			t.Fatalf("expired B11K session accepted: %d", w.Code)
		}
	}
}

func TestLegacyMobileEndpointUsesPersistentWorkerAndKeepsResponseShape(t *testing.T) {
	s := syncServer(t)
	// Use the numeric worker-key value as the athlete ID to prove that worker
	// claims and credential locks really occupy separate advisory namespaces.
	session := s.mobileSessions["deviceOne"]
	session.Athlete = &strava.Athlete{ID: int64(syncer.WorkerLock)}
	if err := s.saveMobileSession(session); err != nil {
		t.Fatal(err)
	}
	s.mobileSessions["deviceOne"] = session
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = refreshTransport(func(r *http.Request) (*http.Response, error) {
		var body any
		status := 200
		switch r.URL.Path {
		case "/oauth/token":
			body = map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_at": time.Now().Add(6 * time.Hour).Unix()}
		case "/api/v3/athlete/activities":
			body = []map[string]any{{"id": 77, "type": "Ride", "name": "Legacy client ride", "start_date": "2026-09-20T10:00:00Z"}}
		case "/api/v3/activities/77":
			body = map[string]any{"id": 77, "athlete": map[string]any{"id": int64(syncer.WorkerLock)}}
		case "/api/v3/activities/77/streams":
			status = 404
			body = map[string]any{}
		default:
			t.Errorf("unexpected Strava request: %s", r.URL.Path)
			status = 500
		}
		encoded, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s.ctx = ctx
	done := make(chan error, 1)
	go func() { done <- s.serveSyncJobs() }()
	r := httptest.NewRequest("POST", "/api/mobile/sync", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer deviceOne")
	w := httptest.NewRecorder()
	s.handleMobileSync(w, r)
	cancel()
	<-done
	if w.Code != 200 {
		t.Fatalf("legacy sync failed: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Summary syncer.Summary `json:"summary"`
		Logs    []string       `json:"logs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Summary.Success != 1 || response.Summary.Failed != 0 || len(response.Logs) == 0 {
		t.Fatalf("legacy response changed: %+v", response)
	}
}
