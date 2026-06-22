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
	return generateStravaAuthURLWithOptions(config, "https://www.strava.com/oauth/authorize", "strava_bike_tracker")
}

func generateStravaAuthURLWithOptions(config StravaAuthConfig, baseURL, state string) string {
	params := url.Values{}
	params.Add("client_id", config.ClientID)
	params.Add("redirect_uri", config.RedirectURI)
	params.Add("response_type", "code")
	params.Add("approval_prompt", "auto")
	params.Add("scope", "read,activity:read_all,profile:read_all")
	if state != "" {
		params.Add("state", state)
	}

	return fmt.Sprintf("%s?%s", baseURL, params.Encode())
}

func exchangeCodeForToken(config StravaAuthConfig, code string) (string, error) {
	tokenResp, err := exchangeCodeForTokenResponse(config, code)
	if err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}

func exchangeCodeForTokenResponse(config StravaAuthConfig, code string) (*StravaTokenResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("client_secret", config.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	// Include redirect_uri - Strava requires it to match exactly what was used in authorization
	if config.RedirectURI != "" {
		data.Set("redirect_uri", config.RedirectURI)
	}

	req, err := http.NewRequest("POST", "https://www.strava.com/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		// Provide more detailed error for SSL/TLS issues
		if strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "tls") {
			return nil, fmt.Errorf("SSL/TLS error connecting to Strava API: %w. This may indicate missing CA certificates or network issues", err)
		}
		return nil, fmt.Errorf("failed to connect to Strava API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp StravaTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

// ExchangeCodeForToken exchanges an authorization code for an access token (exported helper)
func ExchangeCodeForToken(config StravaAuthConfig, code string) (string, error) {
	return exchangeCodeForToken(config, code)
}

// ExchangeCodeForTokenResponse exchanges an authorization code for full token metadata.
func ExchangeCodeForTokenResponse(config StravaAuthConfig, code string) (*StravaTokenResponse, error) {
	return exchangeCodeForTokenResponse(config, code)
}

// RefreshAccessToken refreshes an expired Strava access token.
func RefreshAccessToken(config StravaAuthConfig, refreshToken string) (*StravaTokenResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("client_secret", config.ClientSecret)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequest("POST", "https://www.strava.com/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Strava API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp StravaTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

// GenerateAuthURL returns the Strava OAuth authorization URL (exported helper)
func GenerateAuthURL(config StravaAuthConfig) string {
	return generateStravaAuthURL(config)
}

// GenerateMobileAuthURL returns a Strava mobile OAuth URL for iOS/Android flows.
func GenerateMobileAuthURL(config StravaAuthConfig, state string) string {
	return generateStravaAuthURLWithOptions(config, "https://www.strava.com/oauth/mobile/authorize", state)
}

// GenerateMobileAppAuthURL returns a URL that opens the Strava iOS app when installed.
func GenerateMobileAppAuthURL(config StravaAuthConfig, state string) string {
	return generateStravaAuthURLWithOptions(config, "strava://oauth/mobile/authorize", state)
}

func ConsoleLogin(config StravaAuthConfig) (string, error) {
	fmt.Println("Please go to the following URL to login:")
	fmt.Println(generateStravaAuthURL(config))
	fmt.Println("Enter the code:")
	var code string
	if _, err := fmt.Scanln(&code); err != nil {
		return "", fmt.Errorf("read authorization code: %w", err)
	}
	token, err := exchangeCodeForToken(config, code)
	if err != nil {
		fmt.Println("Error exchanging code for token:", err)
		return "", err
	}

	return token, nil
}
