package web

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
)

func mobileCall(s *server, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	switch {
	case strings.HasPrefix(path, "/api/mobile/activities"):
		s.handleMobileActivities(w, r)
	case strings.HasPrefix(path, "/api/mobile/segments"):
		s.handleMobileSegments(w, r)
	case strings.HasPrefix(path, "/api/mobile/discovered"):
		s.handleMobileDiscovered(w, r)
	case path == "/api/mobile/logout":
		s.handleMobileLogout(w, r)
	case path == "/api/mobile/me":
		s.handleMobileMe(w, r)
	}
	return w
}

func savedRide(t *testing.T, s *server, id, athlete int64, name string) {
	t.Helper()
	start := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC).Add(time.Duration(id) * time.Hour)
	a := strava.BikeActivity{Summary: strava.ActivitySummary{ID: id, AthleteID: athlete, Name: name, Type: "Ride", SportType: "Ride", StartDateTime: start}}
	a.TimeStream.Data = []time.Time{start, start.Add(time.Minute), start.Add(2 * time.Minute)}
	a.LatLngStream.Data = [][]float64{{44.8, 20.4}, {44.801, 20.401}, {44.802, 20.402}}
	a.HeartrateStream.Data = []int{120, 130, 140}
	if err := pggeo.InsertBikeActivityUpsert(s.ctx, s.conn, &a); err != nil {
		t.Fatal(err)
	}
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestMobileActivityLibraryPaginationSearchAndIsolation(t *testing.T) {
	s := syncServer(t)
	savedRide(t, s, 1, 17, "River loop")
	savedRide(t, s, 2, 17, "Morning hills")
	savedRide(t, s, 3, 18, "Other athlete private ride")
	type page struct {
		Count      int
		HasMore    bool `json:"has_more"`
		Activities []mobileActivity
	}
	first := decodeBody[page](t, mobileCall(s, "GET", "/api/mobile/activities?per_page=1", "deviceOne", ""), 200)
	if first.Count != 2 || !first.HasMore || len(first.Activities) != 1 || first.Activities[0].ID != 2 {
		t.Fatalf("bad first page: %+v", first)
	}
	second := decodeBody[page](t, mobileCall(s, "GET", "/api/mobile/activities?per_page=1&page=2", "deviceOne", ""), 200)
	if second.HasMore || len(second.Activities) != 1 || second.Activities[0].ID != 1 {
		t.Fatalf("bad second page: %+v", second)
	}
	search := decodeBody[page](t, mobileCall(s, "GET", "/api/mobile/activities?q=RIVER&sport=ride", "deviceOne", ""), 200)
	if search.Count != 1 || len(search.Activities) != 1 || search.Activities[0].ID != 1 {
		t.Fatalf("bad search: %+v", search)
	}
	for _, pageNo := range []string{"100", "9223372036854775807"} {
		empty := decodeBody[page](t, mobileCall(s, "GET", "/api/mobile/activities?page="+pageNo, "deviceOne", ""), 200)
		if empty.HasMore || len(empty.Activities) != 0 {
			t.Fatalf("past-end page: %+v", empty)
		}
	}
	if w := mobileCall(s, "GET", "/api/mobile/activities/3", "deviceOne", ""); w.Code != 404 {
		t.Fatalf("other athlete detail exposed: %d", w.Code)
	}
	type route struct {
		Source string
		Points []mobileRoutePoint
	}
	own := decodeBody[route](t, mobileCall(s, "GET", "/api/mobile/activities/1/route", "deviceOne", ""), 200)
	if own.Source != "point_samples" || len(own.Points) != 3 || own.Points[0].Lat != 44.8 || own.Points[0].Heartrate == nil || *own.Points[0].Heartrate != 120 {
		t.Fatalf("lost route samples: %+v", own)
	}
	other := decodeBody[route](t, mobileCall(s, "GET", "/api/mobile/activities/3/route", "deviceOne", ""), 200)
	if len(other.Points) != 0 {
		t.Fatal("other athlete route exposed")
	}
	if _, err := s.conn.Exec(s.ctx, "DELETE FROM point_samples WHERE activity_id=1"); err != nil {
		t.Fatal(err)
	}
	fallback := decodeBody[route](t, mobileCall(s, "GET", "/api/mobile/activities/1/route", "deviceOne", ""), 200)
	if fallback.Source != "activity_geometries" || len(fallback.Points) != 3 {
		t.Fatalf("legacy route fallback failed: %+v", fallback)
	}
}

func TestMobileSegmentLifecycleProtectsOwnershipAndValidatesEdits(t *testing.T) {
	s := syncServer(t)
	s.mobileSessions["otherDevice"] = mobileSession{Token: "other", Athlete: &strava.Athlete{ID: 18}, SessionExpiresAt: time.Now().Add(time.Hour)}
	type result struct {
		Segment struct {
			ID                int64
			Name, Description string
		}
	}
	created := decodeBody[result](t, mobileCall(s, "POST", "/api/mobile/segments", "deviceOne", `{"name":"River","description":"Easy","points":[{"lat":44.8,"lng":20.4},{"lat":44.801,"lng":20.401}]}`), 200)
	path := fmt.Sprintf("/api/mobile/segments/%d", created.Segment.ID)
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		if w := mobileCall(s, method, path, "otherDevice", `{"name":"Stolen"}`); w.Code != 403 {
			t.Fatalf("cross-athlete %s: %d %s", method, w.Code, w.Body.String())
		}
	}
	updated := decodeBody[result](t, mobileCall(s, "PATCH", path, "deviceOne", `{"name":"River loop","description":"Updated"}`), 200)
	if updated.Segment.Name != "River loop" || updated.Segment.Description != "Updated" {
		t.Fatalf("edit lost: %+v", updated)
	}
	if w := mobileCall(s, "PATCH", path, "deviceOne", `{"points":[{"lat":91,"lng":20},{"lat":44,"lng":20}]}`); w.Code != 400 {
		t.Fatalf("invalid geometry accepted: %d", w.Code)
	}
	preserved := decodeBody[result](t, mobileCall(s, "GET", path, "deviceOne", ""), 200)
	if preserved.Segment.Name != "River loop" {
		t.Fatal("rejected edit changed saved segment")
	}
	list := decodeBody[struct{ Count int }](t, mobileCall(s, "GET", "/api/mobile/segments?q=RIVER", "deviceOne", ""), 200)
	if list.Count != 1 {
		t.Fatalf("segment missing from filtered list: %+v", list)
	}
	if w := mobileCall(s, "DELETE", path, "deviceOne", ""); w.Code != 204 {
		t.Fatalf("delete failed: %d %s", w.Code, w.Body.String())
	}
	if w := mobileCall(s, "GET", path, "deviceOne", ""); w.Code != 404 {
		t.Fatal("deleted segment still readable")
	}
}

func TestMobileSegmentCannotBeCreatedFromAnotherAthletesRide(t *testing.T) {
	s := syncServer(t)
	savedRide(t, s, 71, 17, "Own ride")
	savedRide(t, s, 72, 18, "Private ride")
	for _, id := range []int{72, 999} {
		w := mobileCall(s, "POST", "/api/mobile/segments", "deviceOne", fmt.Sprintf(`{"name":"Private segment","activity_id":%d,"start_index":0,"end_index":2}`, id))
		if w.Code != 400 {
			t.Fatalf("unowned activity %d: %d %s", id, w.Code, w.Body.String())
		}
	}
	var count int
	if err := s.conn.QueryRow(s.ctx, "SELECT COUNT(*) FROM favorite_segments").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected creation left saved segments: %d %v", count, err)
	}
	w := mobileCall(s, "POST", "/api/mobile/segments", "deviceOne", `{"name":"Own segment","activity_id":71,"start_index":0,"end_index":2}`)
	if w.Code != 200 {
		t.Fatalf("own route segment rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestMobileLogoutRevokesPersistedSessionButKeepsOtherDevice(t *testing.T) {
	s := syncServer(t)
	if w := mobileCall(s, "POST", "/api/mobile/logout", "deviceOne", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// Force subsequent lookups through PostgreSQL, as after a server restart.
	s.mobileSessions = map[string]mobileSession{}
	if w := mobileCall(s, "GET", "/api/mobile/me", "deviceOne", ""); w.Code != 401 {
		t.Fatalf("logged out bearer accepted: %d", w.Code)
	}
	if w := mobileCall(s, "GET", "/api/mobile/me", "deviceTwo", ""); w.Code != 200 {
		t.Fatalf("other device revoked: %d", w.Code)
	}
}

func TestMobileDiscoveredCoverageRebuildAndIsolation(t *testing.T) {
	s := syncServer(t)
	s.cfg.DiscoveredMapEnabled = true
	s.cfg.DiscoveredSampleDistanceMeters = 50
	s.cfg.DiscoveredRevealRadiusMeters = 100
	s.mobileSessions["otherDevice"] = mobileSession{Token: "other", Athlete: &strava.Athlete{ID: 18}, SessionExpiresAt: time.Now().Add(time.Hour)}
	savedRide(t, s, 81, 17, "Covered ride")
	before := decodeBody[pggeo.DiscoveredCoverageStatus](t, mobileCall(s, "GET", "/api/mobile/discovered/status", "deviceOne", ""), 200)
	if !before.Stale {
		t.Fatal("unbuilt coverage should be stale")
	}
	after := decodeBody[pggeo.DiscoveredCoverageStatus](t, mobileCall(s, "POST", "/api/mobile/discovered/rebuild", "deviceOne", ""), 200)
	if after.Stale || after.CachedActivities != 1 {
		t.Fatalf("rebuild did not catch up: %+v", after)
	}
	type collection struct {
		Type     string
		Features []json.RawMessage
	}
	path := "/api/mobile/discovered/coverage?bbox=20,44,21,45"
	own := decodeBody[collection](t, mobileCall(s, "GET", path, "deviceOne", ""), 200)
	if own.Type != "FeatureCollection" || len(own.Features) == 0 {
		t.Fatal("missing coverage geometry")
	}
	other := decodeBody[collection](t, mobileCall(s, "GET", path, "otherDevice", ""), 200)
	if len(other.Features) != 0 {
		t.Fatal("coverage leaked between athletes")
	}
	for _, bbox := range []string{"NaN,44,21,45", "21,44,20,45", "20,44,21,91"} {
		if w := mobileCall(s, "GET", "/api/mobile/discovered/fog?bbox="+bbox, "deviceOne", ""); w.Code != 400 {
			t.Fatalf("invalid bbox accepted: %s %d", bbox, w.Code)
		}
	}
}
