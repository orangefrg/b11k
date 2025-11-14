package strava

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type StravaAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

type StravaTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresAt    int64  `json:"expires_at"`
	RefreshToken string `json:"refresh_token"`
}

func NewStravaAuthConfig(clientID, clientSecret, redirectURI string) *StravaAuthConfig {
	return &StravaAuthConfig{ClientID: clientID, ClientSecret: clientSecret, RedirectURI: redirectURI}
}

func generateStravaAuthURL(config StravaAuthConfig) string {
	baseURL := "https://www.strava.com/oauth/authorize"
	params := url.Values{}
	params.Add("client_id", config.ClientID)
	params.Add("redirect_uri", config.RedirectURI)
	params.Add("response_type", "code")
	params.Add("scope", "read,activity:read_all,profile:read_all")
	params.Add("state", "strava_bike_tracker")

	return fmt.Sprintf("%s?%s", baseURL, params.Encode())
}

func exchangeCodeForToken(config StravaAuthConfig, code string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("client_secret", config.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequest("POST", "https://www.strava.com/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp StravaTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

// ExchangeCodeForToken exchanges an authorization code for an access token (exported helper)
func ExchangeCodeForToken(config StravaAuthConfig, code string) (string, error) {
	return exchangeCodeForToken(config, code)
}

// GenerateAuthURL returns the Strava OAuth authorization URL (exported helper)
func GenerateAuthURL(config StravaAuthConfig) string {
	return generateStravaAuthURL(config)
}

func ConsoleLogin(config StravaAuthConfig) (string, error) {
	fmt.Println("Please go to the following URL to login:")
	fmt.Println(generateStravaAuthURL(config))
	fmt.Println("Enter the code:")
	var code string
	fmt.Scanln(&code)
	token, err := exchangeCodeForToken(config, code)
	if err != nil {
		fmt.Println("Error exchanging code for token:", err)
		return "", err
	}

	return token, nil
}
