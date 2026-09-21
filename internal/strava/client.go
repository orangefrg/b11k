package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIError retains machine-readable failure information without leaking response bodies.
type APIError struct {
	Status  int
	RetryAt time.Time
}

func (e *APIError) Error() string   { return fmt.Sprintf("Strava returned HTTP %d", e.Status) }
func (e *APIError) Temporary() bool { return e.Status == 429 || e.Status >= 500 }

type Client struct {
	BaseURL        string
	HTTP           *http.Client
	Token          func(context.Context, bool) (string, error)
	BeforeRequest  func(context.Context) error
	ObserveHeaders func(context.Context, http.Header) error
}

func NewClient(token string) *Client {
	return &Client{Token: func(context.Context, bool) (string, error) { return token, nil }}
}

// QuotaReset respects both the overall and read-specific application budgets.
func QuotaReset(h http.Header, now time.Time) time.Time {
	var until time.Time
	for _, prefix := range []string{"X-RateLimit", "X-ReadRateLimit"} {
		limits, usages := strings.Split(h.Get(prefix+"-Limit"), ","), strings.Split(h.Get(prefix+"-Usage"), ",")
		if len(limits) != 2 || len(usages) != 2 {
			continue
		}
		for i := 0; i < 2; i++ {
			limit, e1 := strconv.Atoi(strings.TrimSpace(limits[i]))
			used, e2 := strconv.Atoi(strings.TrimSpace(usages[i]))
			if e1 != nil || e2 != nil || limit <= 0 || used < limit {
				continue
			}
			next := now.UTC().Truncate(15 * time.Minute).Add(15*time.Minute + time.Second)
			if i == 1 {
				next = now.UTC().Truncate(24 * time.Hour).Add(24*time.Hour + time.Second)
			}
			if next.After(until) {
				until = next
			}
		}
	}
	if value := h.Get("Retry-After"); value != "" {
		next, e := http.ParseTime(value)
		if seconds, err := strconv.Atoi(value); err == nil {
			next = now.Add(time.Duration(seconds) * time.Second)
			e = nil
		}
		if e == nil && next.After(now) && next.After(until) {
			until = next
		}
	}
	return until
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://www.strava.com/api/v3"
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if c.BeforeRequest != nil {
			if err := c.BeforeRequest(ctx); err != nil {
				return err
			}
		}
		token, err := c.Token(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if c.ObserveHeaders != nil {
			if err := c.ObserveHeaders(ctx, resp.Header); err != nil {
				return err
			}
		}
		if resp.StatusCode == 401 && attempt == 0 {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			retry := QuotaReset(resp.Header, time.Now())
			if resp.StatusCode == 429 && retry.IsZero() {
				retry = time.Now().UTC().Truncate(15 * time.Minute).Add(15*time.Minute + time.Second)
			}
			return &APIError{Status: resp.StatusCode, RetryAt: retry}
		}
		if readErr != nil {
			return readErr
		}
		return json.Unmarshal(body, target)
	}
	return &APIError{Status: 401}
}

func (c *Client) Athlete(ctx context.Context) (*Athlete, error) {
	var athlete Athlete
	err := c.get(ctx, "/athlete", &athlete)
	return &athlete, err
}

// ActivitiesPage returns filtered rides, and a separate last-page flag based on
// the unfiltered page. Empty cycling pages must not truncate an athlete's history.
func (c *Client) ActivitiesPage(ctx context.Context, athleteID int64, start, end time.Time, page int) (ActivitySummaryList, bool, error) {
	q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"200"}}
	if !start.IsZero() {
		q.Set("after", strconv.FormatInt(start.Unix(), 10))
	}
	if !end.IsZero() {
		q.Set("before", strconv.FormatInt(end.Unix(), 10))
	}
	var all ActivitySummaryList
	if err := c.get(ctx, "/athlete/activities?"+q.Encode(), &all); err != nil {
		return nil, false, err
	}
	rides := make(ActivitySummaryList, 0, len(all))
	for _, a := range all {
		if !IsCycling(a) {
			continue
		}
		t, err := time.Parse(time.RFC3339, a.StartDate)
		if err != nil {
			return nil, false, fmt.Errorf("activity %d has invalid start date", a.ID)
		}
		a.StartDateTime = t
		a.AthleteID = athleteID
		rides = append(rides, a)
	}
	return rides, len(all) < 200, nil
}

func IsCycling(a ActivitySummary) bool {
	switch a.Type {
	case "Ride", "VirtualRide", "EBikeRide", "EMountainBikeRide":
		return true
	}
	switch a.SportType {
	case "Ride", "MountainBikeRide", "GravelRide", "VirtualRide", "EBikeRide", "EMountainBikeRide", "Handcycle", "Velomobile":
		return true
	}
	return false
}

func (c *Client) Detail(ctx context.Context, summary ActivitySummary) (*BikeActivity, error) {
	if summary.ID <= 0 || summary.AthleteID <= 0 {
		return nil, fmt.Errorf("activity requires its original summary and athlete")
	}
	var raw json.RawMessage
	if err := c.get(ctx, fmt.Sprintf("/activities/%d", summary.ID), &raw); err != nil {
		return nil, err
	}
	var identity struct {
		ID      int64 `json:"id"`
		Athlete struct {
			ID int64 `json:"id"`
		} `json:"athlete"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		return nil, err
	}
	if identity.ID != summary.ID || identity.Athlete.ID != summary.AthleteID {
		return nil, fmt.Errorf("Strava activity identity mismatch")
	}
	var activity BikeActivity
	if err := json.Unmarshal(raw, &activity); err != nil {
		return nil, err
	}
	activity.Summary = summary
	if activity.Gear != nil && activity.Gear.Name != "" {
		activity.Summary.GearName = &activity.Gear.Name
	}
	q := url.Values{"keys": {strings.Join(activityStreamKeys, ",")}, "key_by_type": {"true"}}
	var streamsBody json.RawMessage
	err := c.get(ctx, fmt.Sprintf("/activities/%d/streams?%s", summary.ID, q.Encode()), &streamsBody)
	if apiErr, ok := err.(*APIError); ok && apiErr.Status == 404 {
		return &activity, nil
	} // existing activity, no recorded streams
	if err != nil {
		return nil, err
	}
	streams, err := decodeRawStravaStreams(streamsBody)
	if err != nil {
		return nil, err
	}
	if err = activity.AddStreams(streams); err != nil {
		return nil, err
	}
	return &activity, nil
}
