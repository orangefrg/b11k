package strava

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuotaResetUsesReadAndDailyBudgets(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 7, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "200,2000")
	h.Set("X-RateLimit-Usage", "5,100")
	h.Set("X-ReadRateLimit-Limit", "100,1000")
	h.Set("X-ReadRateLimit-Usage", "100,200")
	if got, want := QuotaReset(h, now), time.Date(2026, 9, 21, 10, 15, 1, 0, time.UTC); got != want {
		t.Fatalf("read limit: got %v want %v", got, want)
	}
	h.Set("X-ReadRateLimit-Usage", "100,1000")
	if got, want := QuotaReset(h, now), time.Date(2026, 9, 22, 0, 0, 1, 0, time.UTC); got != want {
		t.Fatalf("daily limit: got %v want %v", got, want)
	}
}
func TestCyclingPageDoesNotStopAtEmptyFilteredPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activities := make([]map[string]any, 200)
		for i := range activities {
			activities[i] = map[string]any{"id": i + 1, "type": "Run", "start_date": "2026-09-21T10:00:00Z"}
		}
		json.NewEncoder(w).Encode(activities)
	}))
	defer server.Close()
	client := NewClient("fixture")
	client.BaseURL = server.URL
	rides, done, err := client.ActivitiesPage(context.Background(), 17, time.Time{}, time.Time{}, 1)
	if err != nil || done || len(rides) != 0 {
		t.Fatalf("rides=%v done=%v err=%v", rides, done, err)
	}
}
func TestDetailKeepsIdentityAndAcceptsAbsentStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/activities/7/streams" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(`{"id":7,"athlete":{"id":17}}`))
	}))
	defer server.Close()
	client := NewClient("fixture")
	client.BaseURL = server.URL
	original := ActivitySummary{ID: 7, AthleteID: 17, Name: "Indoor ride", StartDateTime: time.Now()}
	activity, err := client.Detail(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if activity.Summary != original {
		t.Fatal("summary changed")
	}
	original.AthleteID = 18
	if _, err = client.Detail(context.Background(), original); err == nil {
		t.Fatal("accepted another athlete")
	}
}
func TestUnauthorizedRefreshesOnceWithoutExposingBody(t *testing.T) {
	calls, refreshes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
		w.Write([]byte("private-token"))
	}))
	defer server.Close()
	client := &Client{BaseURL: server.URL, Token: func(_ context.Context, force bool) (string, error) {
		if force {
			refreshes++
		}
		return "fixture", nil
	}}
	_, err := client.Athlete(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.Status != 401 || calls != 2 || refreshes != 1 {
		t.Fatalf("calls=%d refreshes=%d err=%v", calls, refreshes, err)
	}
	if err.Error() != "Strava returned HTTP 401" {
		t.Fatal(err)
	}
}
func TestStreamFailureIsReturnedForRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/activities/7/streams" {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"id":7,"athlete":{"id":17}}`))
	}))
	defer server.Close()
	client := NewClient("fixture")
	client.BaseURL = server.URL
	_, err := client.Detail(context.Background(), ActivitySummary{ID: 7, AthleteID: 17})
	var api *APIError
	if !errors.As(err, &api) || api.Status != 503 {
		t.Fatal(err)
	}
}

func TestExpiredRetryAfterCannotCauseImmediateQuotaRetry(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 7, 0, 0, time.UTC)
	for _, value := range []string{"0", "-1", "Sun, 20 Sep 2026 10:00:00 GMT"} {
		h := http.Header{}
		h.Set("Retry-After", value)
		if !QuotaReset(h, now).IsZero() {
			t.Fatalf("expired Retry-After accepted: %s", value)
		}
	}
}
