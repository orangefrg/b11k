package strava

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Athlete represents the authenticated athlete profile subset we need
type Athlete struct {
	ID        int64  `json:"id"`
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Profile   string `json:"profile"` // avatar URL
}

// FetchCurrentAthlete retrieves the profile for the current authenticated athlete
func FetchCurrentAthlete(accessToken string) (*Athlete, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", "https://www.strava.com/api/v3/athlete", nil)
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
		return nil, fmt.Errorf("fetch athlete failed: %d: %s", resp.StatusCode, string(body))
	}
	var a Athlete
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
