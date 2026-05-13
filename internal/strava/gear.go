package strava

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchGear retrieves a Strava gear object by ID.
func FetchGear(accessToken, gearID string) (*Gear, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", "https://www.strava.com/api/v3/gear/"+gearID, nil)
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
		return nil, fmt.Errorf("failed to fetch gear %s: status %d: %s", gearID, resp.StatusCode, string(body))
	}

	var gear Gear
	if err := json.Unmarshal(body, &gear); err != nil {
		return nil, err
	}
	return &gear, nil
}
