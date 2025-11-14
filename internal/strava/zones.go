package strava

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AthleteZones models the Strava athlete zones response
type AthleteZones struct {
	HeartRate HeartRateZones `json:"heart_rate"`
}

// HeartRateZones contains an ordered list of HR zones with min/max bpm
type HeartRateZones struct {
	Zones []HRZone `json:"zones"`
}

// HRZone defines a single heart rate zone boundaries (inclusive)
type HRZone struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// FetchHeartRateZones retrieves the authenticated athlete's heart rate zones using the access token
func FetchHeartRateZones(accessToken string) (*AthleteZones, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", "https://www.strava.com/api/v3/athlete/zones", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch zones: status %d: %s", resp.StatusCode, string(body))
	}

	var zones AthleteZones
	if err := json.Unmarshal(body, &zones); err != nil {
		return nil, err
	}
	return &zones, nil
}
