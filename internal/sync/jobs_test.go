package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	"b11k/internal/testdb"
	"github.com/jackc/pgx/v5"
)

func fixture(t *testing.T) (Jobs, func(context.Context) (*pgx.Conn, error)) {
	t.Helper()
	conn, connect := testdb.Open(t)
	if err := pggeo.CreateTables(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	return Jobs{Conn: conn}, connect
}
func mockStrava(t *testing.T, streamStatus *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/athlete/activities":
			w.Write([]byte(`[{"id":77,"name":"Test ride","type":"Ride","start_date":"2026-09-20T10:00:00Z","distance":5000}]`))
		case r.URL.Path == "/activities/77":
			w.Write([]byte(`{"id":77,"athlete":{"id":17}}`))
		case r.URL.Path == "/activities/77/streams":
			if *streamStatus != 200 {
				w.WriteHeader(*streamStatus)
				return
			}
			w.Write([]byte(`{"time":{"data":[0,1]},"latlng":{"data":[[44.8,20.4],[44.81,20.41]]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
func workerFor(jobs Jobs, server *httptest.Server) Worker {
	return Worker{Jobs: jobs, Client: func(int64) *strava.Client { c := strava.NewClient("fixture"); c.BaseURL = server.URL; return c }}
}
func step(t *testing.T, w Worker) {
	t.Helper()
	if _, err := w.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func load(t *testing.T, s Jobs, j *Job) *Job {
	t.Helper()
	got, err := s.Get(context.Background(), j.AthleteID, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDurableImportFailureRetryAndRestart(t *testing.T) {
	s, connect := fixture(t)
	ctx := context.Background()
	status := 503
	server := mockStrava(t, &status)
	job, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	w := workerFor(s, server)
	step(t, w)
	step(t, w)
	j := load(t, s, job)
	if j.State != "waiting" || j.Summary.Pending != 1 || j.Summary.Success != 0 {
		t.Fatalf("failure lost: %+v", j)
	}
	// Recreate the worker and its connection, just as a restarted process would.
	conn, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	w = workerFor(Jobs{Conn: conn}, server)
	status = 200
	s.Conn.Exec(ctx, "UPDATE sync_jobs SET next_attempt_at=NOW() WHERE id=$1", job.ID)
	step(t, w)
	step(t, w)
	j = load(t, s, job)
	if j.State != "complete" || j.Summary.Success != 1 || j.Summary.Failed != 0 {
		t.Fatalf("not recovered: %+v", j)
	}
	var athlete int64
	var name string
	var count int
	if err = s.Conn.QueryRow(ctx, "SELECT athlete_id,name FROM activity_summaries WHERE id=77").Scan(&athlete, &name); err != nil {
		t.Fatal(err)
	}
	if athlete != 17 || name != "Test ride" {
		t.Fatalf("lost metadata: %d %s", athlete, name)
	}
	s.Conn.QueryRow(ctx, "SELECT COUNT(*) FROM point_samples WHERE activity_id=77").Scan(&count)
	if count != 2 {
		t.Fatalf("samples %d", count)
	}
	next, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	step(t, w)
	step(t, w)
	if got := load(t, s, next); got.Summary.Existing != 1 || got.Summary.Success != 0 {
		t.Fatalf("duplicated complete import: %+v", got)
	}
}
func TestFailedItemsAreVisibleAndManualRetryRepairsThem(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	status := 503
	server := mockStrava(t, &status)
	w := workerFor(s, server)
	job, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	step(t, w)
	for i := 0; i < 3; i++ {
		s.Conn.Exec(ctx, "UPDATE sync_jobs SET next_attempt_at=NOW() WHERE id=$1", job.ID)
		step(t, w)
	}
	step(t, w)
	got := load(t, s, job)
	if got.State != "partial" || got.Summary.Failed != 1 || len(got.Failures) != 1 {
		t.Fatalf("false success: %+v", got)
	}
	if _, err = s.Change(ctx, 17, job.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	status = 404
	step(t, w)
	step(t, w)
	if got = load(t, s, job); got.State != "complete" || got.Summary.Success != 1 {
		t.Fatalf("indoor import not repaired: %+v", got)
	}
}
func TestConcurrentStartsAndAthleteIsolation(t *testing.T) {
	s, connect := fixture(t)
	ctx := context.Background()
	other, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(ctx)
	jobs := make(chan *Job, 2)
	errs := make(chan error, 2)
	for _, store := range []Jobs{s, {Conn: other}} {
		go func(store Jobs) { j, e := store.Start(ctx, 17, time.Time{}, time.Time{}); jobs <- j; errs <- e }(store)
	}
	a, b := <-jobs, <-jobs
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
	if a.ID != b.ID {
		t.Fatal("duplicate jobs")
	}
	if _, err = s.Get(ctx, 18, a.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-athlete read: %v", err)
	}
	if _, err = s.Change(ctx, 18, a.ID, "cancel"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-athlete cancel: %v", err)
	}
	if _, err = s.Change(ctx, 18, a.ID, "retry"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-athlete retry: %v", err)
	}
}
func TestCancellationDuringDownloadCannotCommitOrResurrect(t *testing.T) {
	s, connect := fixture(t)
	ctx := context.Background()
	other, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(ctx)
	j, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := (Jobs{Conn: other}).Change(ctx, 17, j.ID, "cancel")
		if err != nil {
			t.Error(err)
		}
		w.Write([]byte(`[{"id":77,"name":"Test","type":"Ride","start_date":"2026-09-20T10:00:00Z"}]`))
	}))
	defer server.Close()
	step(t, workerFor(s, server))
	if got := load(t, s, j); got.State != "cancelled" || got.Summary.Total != 0 {
		t.Fatalf("cancel was overwritten: %+v", got)
	}
}
func TestQuotasDoNotExhaustRetriesAndSurviveRestart(t *testing.T) {
	s, connect := fixture(t)
	ctx := context.Background()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-ReadRateLimit-Limit", "100,1000")
		w.Header().Set("X-ReadRateLimit-Usage", "100,1000")
		w.WriteHeader(429)
	}))
	defer server.Close()
	j, _ := s.Start(ctx, 17, time.Time{}, time.Time{})
	step(t, workerFor(s, server))
	got := load(t, s, j)
	if got.State != "waiting" || got.Attempts != 0 || !got.NextAttemptAt.After(time.Now()) {
		t.Fatalf("bad quota state: %+v", got)
	}
	_, err := s.Start(ctx, 18, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	step(t, workerFor(Jobs{Conn: conn}, server))
	if calls != 1 {
		t.Fatal("application-wide quota was not shared")
	}
}
func TestAtomicWriteRollsBackAndProtectsOwnership(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	a := strava.BikeActivity{Summary: strava.ActivitySummary{ID: 77, AthleteID: 17, Name: "Original", StartDateTime: time.Now(), Type: "Ride"}}
	if err := pggeo.InsertBikeActivityUpsert(ctx, s.Conn, &a); err != nil {
		t.Fatal(err)
	}
	// Fail after the summary write, inside the samples write.
	_, err := s.Conn.Exec(ctx, `CREATE FUNCTION reject_test_sample() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected write failure'; END $$;
 CREATE TRIGGER reject_test_sample BEFORE INSERT ON point_samples FOR EACH ROW EXECUTE FUNCTION reject_test_sample()`)
	if err != nil {
		t.Fatal(err)
	}
	a.Summary.Name = "Changed"
	a.TimeStream.Data = []time.Time{time.Now(), time.Now()}
	a.LatLngStream.Data = [][]float64{{44.8, 20.4}, {44.81, 20.41}}
	if err = pggeo.InsertBikeActivityUpsert(ctx, s.Conn, &a); err == nil {
		t.Fatal("expected injected failure")
	}
	var name string
	s.Conn.QueryRow(ctx, "SELECT name FROM activity_summaries WHERE id=77").Scan(&name)
	if name != "Original" {
		t.Fatal("partial summary committed")
	}
	a.Summary.AthleteID = 18
	if err = pggeo.InsertBikeActivityUpsert(ctx, s.Conn, &a); err == nil || !strings.Contains(err.Error(), "another athlete") {
		t.Fatalf("ownership changed: %v", err)
	}
}
func TestLegacyRowsAreRepairedRatherThanSkipped(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	status := 200
	server := mockStrava(t, &status)
	a := strava.ActivitySummary{ID: 77, AthleteID: 17, Name: "Incomplete", StartDateTime: time.Now(), Type: "Ride"}
	if err := pggeo.InsertActivitySummaryUpsert(ctx, s.Conn, &a); err != nil {
		t.Fatal(err)
	}
	job, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	w := workerFor(s, server)
	step(t, w)
	step(t, w)
	step(t, w)
	if got := load(t, s, job); got.Summary.Success != 1 || got.Summary.Existing != 0 {
		t.Fatalf("incomplete import skipped: %+v", got)
	}
}
func TestAllHistoryPaginationHasNoSilentHundredPageLimit(t *testing.T) {
	// A job resumes persisted pagination beyond the previous arbitrary cap.
	s, _ := fixture(t)
	ctx := context.Background()
	job, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	s.Conn.Exec(ctx, "UPDATE sync_jobs SET page=101 WHERE id=$1", job.ID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "101" {
			t.Error("lost page checkpoint")
		}
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()
	w := workerFor(s, server)
	step(t, w)
	step(t, w)
	if got := load(t, s, job); got.State != "complete" {
		t.Fatal(fmt.Sprintf("%+v", got))
	}
}

func TestIndoorSensorsPersistWithoutInventingCoordinates(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	a := strava.BikeActivity{Summary: strava.ActivitySummary{ID: 80, AthleteID: 17, Name: "Indoor", StartDateTime: time.Now(), Type: "VirtualRide"}}
	a.TimeStream.Data = []time.Time{time.Now(), time.Now().Add(time.Second)}
	a.WattsStream.Data = []int{150, 160}
	if err := pggeo.InsertBikeActivityUpsert(ctx, s.Conn, &a); err != nil {
		t.Fatal(err)
	}
	var count int
	var sum int
	if err := s.Conn.QueryRow(ctx, "SELECT COUNT(*),SUM(watts) FROM point_samples WHERE activity_id=80 AND location IS NULL").Scan(&count, &sum); err != nil {
		t.Fatal(err)
	}
	if count != 2 || sum != 310 {
		t.Fatalf("sensor samples lost: %d %d", count, sum)
	}
	route, err := pggeo.GetPointSamplesForActivity(ctx, s.Conn, 17, 80)
	if err != nil || len(route) != 0 {
		t.Fatalf("invented indoor route: %v %v", route, err)
	}
	if _, err = pggeo.RebuildDiscoveredCoverage(ctx, s.Conn, 17, 50, 100); err != nil {
		t.Fatal(err)
	}
}
func TestMapFinalizationRetriesWithoutDownloadingActivities(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	status := 200
	server := mockStrava(t, &status)
	j, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	w := workerFor(s, server)
	w.Discovered = DiscoveredMapConfig{Enabled: true, SampleDistanceMeters: 50, RevealRadiusMeters: 0}
	step(t, w)
	step(t, w)
	step(t, w)
	if got := load(t, s, j); got.State != "waiting" || got.Phase != "finalizing" || got.Summary.Success != 1 {
		t.Fatalf("wrong finalization state: %+v", got)
	}
	w.Discovered.RevealRadiusMeters = 100
	w.Client = func(int64) *strava.Client {
		return &strava.Client{Token: func(context.Context, bool) (string, error) {
			t.Error("redownload during map recovery")
			return "", fmt.Errorf("unexpected request")
		}}
	}
	s.Conn.Exec(ctx, "UPDATE sync_jobs SET next_attempt_at=NOW() WHERE id=$1", j.ID)
	step(t, w)
	if got := load(t, s, j); got.State != "complete" {
		t.Fatalf("map did not recover: %+v", got)
	}
}
func TestWorkerClaimReleasesOnConnectionLoss(t *testing.T) {
	_, connect := fixture(t)
	ctx := context.Background()
	a, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	var held bool
	if err = a.QueryRow(ctx, "SELECT pg_try_advisory_lock(48110, $1::integer)", WorkerLock).Scan(&held); err != nil || !held {
		t.Fatal("could not claim worker")
	}
	if err = b.QueryRow(ctx, "SELECT pg_try_advisory_lock(48110, $1::integer)", WorkerLock).Scan(&held); err != nil || held {
		t.Fatal("two workers can import at once")
	}
	a.Close(ctx)
	claimCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err = b.Exec(claimCtx, "SELECT pg_advisory_lock(48110, $1::integer)", WorkerLock); err != nil {
		t.Fatalf("worker claim did not recover after disconnect: %v", err)
	}
}
func TestAuthorizationFailurePausesUntilExplicitRetry(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	j, err := s.Start(ctx, 17, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Jobs: s, Client: func(int64) *strava.Client {
		return &strava.Client{Token: func(context.Context, bool) (string, error) { return "", &strava.APIError{Status: 401} }}
	}}
	step(t, w)
	got := load(t, s, j)
	if got.State != "needs_auth" || got.Attempts != 0 {
		t.Fatalf("auth failure should pause: %+v", got)
	}
	if worked, err := w.Step(ctx); err != nil || worked {
		t.Fatal("paused job kept retrying")
	}
	if _, err = s.Change(ctx, 17, j.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	if got = load(t, s, j); got.State != "queued" {
		t.Fatal("could not resume after reconnect")
	}
}

func TestSyncMigrationPreservesLegacyActivitiesAndIsRepeatable(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	a := strava.ActivitySummary{ID: 90, AthleteID: 17, Name: "Existing ride", StartDateTime: time.Now(), Type: "Ride"}
	if err := pggeo.InsertActivitySummaryUpsert(ctx, s.Conn, &a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Conn.Exec(ctx, "ALTER TABLE activity_summaries DROP COLUMN sync_version; ALTER TABLE point_samples ALTER COLUMN location SET NOT NULL"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := pggeo.EnsureSyncSchema(ctx, s.Conn); err != nil {
			t.Fatal(err)
		}
	}
	var name string
	var version int
	if err := s.Conn.QueryRow(ctx, "SELECT name,sync_version FROM activity_summaries WHERE id=90").Scan(&name, &version); err != nil {
		t.Fatal(err)
	}
	if name != "Existing ride" || version != 0 {
		t.Fatal("migration lost existing data or falsely marked it complete")
	}
}
