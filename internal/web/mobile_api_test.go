package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
)

func TestMobileSegmentsRequireBearerToken(t *testing.T) {
	s := &server{}
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/segments", nil)
	rec := httptest.NewRecorder()

	s.handleMobileSegments(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileProfileRequiresBearerToken(t *testing.T) {
	s := &server{}
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/profile", nil)
	rec := httptest.NewRecorder()

	s.handleMobileProfile(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileLogoutRequiresBearerToken(t *testing.T) {
	s := &server{}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/logout", nil)
	rec := httptest.NewRecorder()

	s.handleMobileLogout(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestProfileDataJSONShape(t *testing.T) {
	data, err := json.Marshal(profileData{
		TotalBikeKM:     123.4,
		TotalActivities: 7,
		BikeStats: []profileBikeStat{{
			GearID:     "b123",
			Label:      "Road Bike",
			DistanceKM: 123.4,
			Activities: 7,
		}},
		BestMonth: profilePeriodStat{Label: "June 2026", Activities: 5},
		BestYear:  profilePeriodStat{Label: "2026", Activities: 7},
		HRZones:   []profileHRZone{{Label: "Z1", Range: "100-120 bpm"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"total_bike_km", "total_activities", "bike_stats", "best_month", "best_year", "hr_zones"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("profile payload missing %q: %s", key, string(data))
		}
	}
}

func TestMobileDiscoveredDisabledReturnsNotFoundBeforeAuth(t *testing.T) {
	s := &server{cfg: Config{DiscoveredMapEnabled: false}}
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/discovered/status", nil)
	rec := httptest.NewRecorder()

	s.handleMobileDiscovered(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMobileLatLngDataAcceptsPointShapes(t *testing.T) {
	data, hasPoints, err := mobileLatLngData(
		[]mobileLatLng{{Lat: 1, Lng: 2}, {Lat: 3, Lng: 4}},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPoints || len(data) != 2 || data[0][0] != 1 || data[0][1] != 2 {
		t.Fatalf("mobile point payload parsed as %#v, has=%v", data, hasPoints)
	}

	data, hasPoints, err = mobileLatLngData(nil, [][]float64{{2, 1}, {4, 3}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPoints || len(data) != 2 || data[0][0] != 1 || data[0][1] != 2 {
		t.Fatalf("GeoJSON coordinate payload parsed as %#v, has=%v", data, hasPoints)
	}

	data, hasPoints, err = mobileLatLngData(nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasPoints || data != nil {
		t.Fatalf("empty payload parsed as %#v, has=%v", data, hasPoints)
	}
}

func TestMobileLatLngDataRejectsInvalidCoordinates(t *testing.T) {
	tests := []struct {
		name        string
		points      []mobileLatLng
		coordinates [][]float64
		latLng      [][]float64
	}{
		{name: "one point", points: []mobileLatLng{{Lat: 1, Lng: 2}}},
		{name: "bad latitude", points: []mobileLatLng{{Lat: 91, Lng: 2}, {Lat: 3, Lng: 4}}},
		{name: "bad longitude", points: []mobileLatLng{{Lat: 1, Lng: -181}, {Lat: 3, Lng: 4}}},
		{name: "short array", latLng: [][]float64{{1}, {3, 4}}},
		{name: "bad GeoJSON latitude", coordinates: [][]float64{{2, 91}, {4, 3}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := mobileLatLngData(tt.points, tt.coordinates, tt.latLng, nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseMobileSegmentGeometry(t *testing.T) {
	geometry, err := parseMobileSegmentGeometry(`{"type":"LineString","coordinates":[[20.1,44.8],[20.2,44.9]]}`)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Type != "LineString" || len(geometry.Points) != 2 {
		t.Fatalf("unexpected geometry: %#v", geometry)
	}
	if geometry.Points[0].Lat != 44.8 || geometry.Points[0].Lng != 20.1 {
		t.Fatalf("first point = %#v", geometry.Points[0])
	}

	if _, err := parseMobileSegmentGeometry(`{"type":"Point","coordinates":[20.1,44.8]}`); err == nil {
		t.Fatal("non-LineString geometry should be rejected")
	}
	if _, err := parseMobileSegmentGeometry(`{"type":"LineString","coordinates":[[20.1,91],[20.2,44.9]]}`); err == nil {
		t.Fatal("invalid coordinates should be rejected")
	}
}

func TestMobileSegmentEffortsFromActivities(t *testing.T) {
	hr := 142.0
	speed := 8.2
	distance := 1234.5
	elevation := 44.0
	elapsed := 321.0

	efforts := mobileSegmentEffortsFromActivities([]pggeo.ActivityWithMatch{{
		ActivitySummary: strava.ActivitySummary{
			ID:        123,
			Name:      "Hill Repeats",
			Distance:  5000,
			SportType: "Ride",
		},
		MinDistanceM:       3.4,
		OverlapLengthM:     1200,
		OverlapPercentage:  96.5,
		SegmentAvgHR:       &hr,
		SegmentAvgSpeed:    &speed,
		SegmentDistance:    &distance,
		SegmentElevation:   &elevation,
		SegmentElapsedSecs: &elapsed,
	}})

	if len(efforts) != 1 {
		t.Fatalf("len = %d, want 1", len(efforts))
	}
	if efforts[0].Activity.ID != 123 || efforts[0].Activity.Name != "Hill Repeats" {
		t.Fatalf("unexpected activity: %#v", efforts[0].Activity)
	}
	if efforts[0].SegmentElapsedSecs == nil || *efforts[0].SegmentElapsedSecs != elapsed {
		t.Fatalf("segment elapsed = %#v, want %v", efforts[0].SegmentElapsedSecs, elapsed)
	}
	if efforts[0].OverlapPercentage != 96.5 {
		t.Fatalf("overlap = %v, want 96.5", efforts[0].OverlapPercentage)
	}
}

func TestPointSamplesInIndexRange(t *testing.T) {
	samples := []pggeo.PointSample{
		{PointIndex: 2},
		{PointIndex: 3},
		{PointIndex: 4},
		{PointIndex: 5},
	}
	filtered := pointSamplesInIndexRange(samples, 3, 4)
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
	if filtered[0].PointIndex != 3 || filtered[1].PointIndex != 4 {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestMobileDiscoveredRebuildHasSeparateRateLimit(t *testing.T) {
	s := &server{rateLimits: map[string]rateLimitEntry{}}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/discovered/rebuild", strings.NewReader(""))
	req.RemoteAddr = localRemote

	for i := 0; i < 6; i++ {
		if !s.allowRequestRate(httptest.NewRecorder(), req) {
			t.Fatalf("request %d was unexpectedly rate limited", i+1)
		}
	}
	rec := httptest.NewRecorder()
	if s.allowRequestRate(rec, req) {
		t.Fatal("seventh discovered rebuild request should be rate limited")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}
