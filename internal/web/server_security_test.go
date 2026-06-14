package web

import (
	"net/http"
	"testing"
)

func TestRequestGateSeparatesPublicAPIHost(t *testing.T) {
	s := &server{
		cfg: Config{
			PublicAPIHost: "api.b11k.example.com",
		},
	}

	tests := []struct {
		name string
		host string
		path string
		want bool
	}{
		{name: "api host allows mobile API", host: "api.b11k.example.com", path: "/api/mobile/me", want: true},
		{name: "api host blocks web UI", host: "api.b11k.example.com", path: "/profile", want: false},
		{name: "web host blocks mobile API", host: "b11k.example.com", path: "/api/mobile/me", want: false},
		{name: "web host allows mobile auth callback", host: "b11k.example.com", path: "/api/mobile/auth/callback", want: true},
		{name: "web host blocks adjacent mobile auth session", host: "b11k.example.com", path: "/api/mobile/auth/session", want: false},
		{name: "web host allows web UI", host: "b11k.example.com", path: "/profile", want: true},
		{name: "lan host allows mobile API during development", host: "10.0.0.42:8080", path: "/api/mobile/me", want: true},
		{name: "localhost allows web UI during development", host: "localhost:8080", path: "/profile", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://"+tt.host+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.isRequestAllowed(req); got != tt.want {
				t.Fatalf("isRequestAllowed(%q, %q) = %v, want %v", tt.host, tt.path, got, tt.want)
			}
		})
	}
}

func TestRequestGateRequiresHTTPSOnPublicConfiguredHosts(t *testing.T) {
	s := &server{
		cfg: Config{
			WebProtocol:   "https",
			PublicAPIHost: "api.b11k.example.com",
		},
	}

	publicReq, err := http.NewRequest(http.MethodGet, "http://api.b11k.example.com/api/mobile/auth/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.isPublicRequestTransportAllowed(publicReq) {
		t.Fatal("public HTTP request was allowed without forwarded HTTPS")
	}

	publicReq.Header.Set("X-Forwarded-Proto", "https")
	if !s.isPublicRequestTransportAllowed(publicReq) {
		t.Fatal("public HTTPS request was rejected")
	}

	localReq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/mobile/auth/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.isPublicRequestTransportAllowed(localReq) {
		t.Fatal("local HTTP request should remain allowed")
	}
}

func TestBrowserOriginMobileAPIRequestsAreRejected(t *testing.T) {
	s := &server{}

	req, err := http.NewRequest(http.MethodPost, "https://api.b11k.example.com/api/mobile/sync", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	if !s.isDisallowedBrowserMobileAPIRequest(req) {
		t.Fatal("browser-origin mobile API request was not rejected")
	}

	callback, err := http.NewRequest(http.MethodGet, "https://b11k.example.com/api/mobile/auth/callback", nil)
	if err != nil {
		t.Fatal(err)
	}
	callback.Header.Set("Origin", "https://www.strava.com")
	if s.isDisallowedBrowserMobileAPIRequest(callback) {
		t.Fatal("mobile auth callback should not be rejected by origin guard")
	}
}

func TestRequestGateKeepsDevAPIPrivate(t *testing.T) {
	tests := []struct {
		name      string
		enableDev bool
		host      string
		wantAllow bool
	}{
		{name: "disabled on LAN", enableDev: false, host: "10.0.0.42:8080", wantAllow: false},
		{name: "enabled on LAN", enableDev: true, host: "10.0.0.42:8080", wantAllow: true},
		{name: "enabled still blocked on public host", enableDev: true, host: "api.b11k.example.com", wantAllow: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{
				cfg: Config{
					PublicAPIHost: "api.b11k.example.com",
					EnableDevAPI:  tt.enableDev,
				},
			}
			req, err := http.NewRequest(http.MethodPost, "http://"+tt.host+"/api/mobile/dev/rebuild-sync", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.isRequestAllowed(req); got != tt.wantAllow {
				t.Fatalf("isRequestAllowed(dev, host=%q, enable=%v) = %v, want %v", tt.host, tt.enableDev, got, tt.wantAllow)
			}
		})
	}
}

func TestMobileSessionStorageKeyHashesRawTokens(t *testing.T) {
	raw := "session-secret"
	key := mobileSessionStorageKey(raw)
	if key == raw {
		t.Fatal("session storage key must not equal raw token")
	}
	if len(key) != len("sha256:")+64 {
		t.Fatalf("unexpected storage key length: %d", len(key))
	}
	if key != mobileSessionStorageKey(raw) {
		t.Fatal("session storage key must be stable")
	}
}

func TestRateLimitBlocksAfterLimit(t *testing.T) {
	s := &server{rateLimits: map[string]rateLimitEntry{}}
	for i := 0; i < 2; i++ {
		ok, _ := s.consumeRateLimit("client:bucket", 2, 60_000_000_000)
		if !ok {
			t.Fatalf("request %d was unexpectedly rate limited", i+1)
		}
	}
	ok, _ := s.consumeRateLimit("client:bucket", 2, 60_000_000_000)
	if ok {
		t.Fatal("third request should be rate limited")
	}
}
