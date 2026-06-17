package web

import (
	"net/http"
	"testing"
)

const (
	localRemote   = "127.0.0.1:45678"
	privateRemote = "10.0.0.7:45678"
	publicRemote  = "198.51.100.8:45678"
)

func newHostRequest(t *testing.T, method, host, path, remoteAddr string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+host+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = remoteAddr
	return req
}

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
			remote := publicRemote
			if tt.host == "10.0.0.42:8080" {
				remote = privateRemote
			}
			if tt.host == "localhost:8080" {
				remote = localRemote
			}
			req := newHostRequest(t, http.MethodGet, tt.host, tt.path, remote)
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

	publicReq := newHostRequest(t, http.MethodGet, "api.b11k.example.com", "/api/mobile/auth/start", localRemote)
	if s.isPublicRequestTransportAllowed(publicReq) {
		t.Fatal("public HTTP request was allowed without forwarded HTTPS")
	}

	publicReq.Header.Set("X-Forwarded-Proto", "https")
	if !s.isPublicRequestTransportAllowed(publicReq) {
		t.Fatal("public HTTPS request was rejected")
	}

	localReq := newHostRequest(t, http.MethodGet, "127.0.0.1:8080", "/api/mobile/auth/start", localRemote)
	if !s.isPublicRequestTransportAllowed(localReq) {
		t.Fatal("local HTTP request should remain allowed")
	}

	spoofedPublicReq := newHostRequest(t, http.MethodGet, "api.b11k.example.com", "/api/mobile/auth/start", publicRemote)
	spoofedPublicReq.Header.Set("X-Forwarded-Proto", "https")
	if s.isPublicRequestTransportAllowed(spoofedPublicReq) {
		t.Fatal("direct public request should not be able to spoof forwarded HTTPS")
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
			remote := publicRemote
			if tt.host == "10.0.0.42:8080" {
				remote = privateRemote
			}
			req := newHostRequest(t, http.MethodPost, tt.host, "/api/mobile/dev/rebuild-sync", remote)
			if got := s.isRequestAllowed(req); got != tt.wantAllow {
				t.Fatalf("isRequestAllowed(dev, host=%q, enable=%v) = %v, want %v", tt.host, tt.enableDev, got, tt.wantAllow)
			}
		})
	}
}

func TestRequestGateRejectsUnknownPublicHostWhenWebHostConfigured(t *testing.T) {
	s := &server{
		cfg: Config{
			PublicAPIHost: "api.b11k.example.com",
			WebHost:       "b11k.example.com",
		},
	}

	req := newHostRequest(t, http.MethodGet, "unexpected.example.com", "/profile", publicRemote)
	if s.isRequestAllowed(req) {
		t.Fatal("unknown public host should be rejected when web_host is configured")
	}

	webReq := newHostRequest(t, http.MethodGet, "b11k.example.com", "/profile", publicRemote)
	if !s.isRequestAllowed(webReq) {
		t.Fatal("configured web host should remain allowed")
	}
}

func TestForwardedHeadersOnlyTrustedFromLocalPeer(t *testing.T) {
	localProxyReq := newHostRequest(t, http.MethodGet, "127.0.0.1:8080", "/api/mobile/me", localRemote)
	localProxyReq.Header.Set("X-Forwarded-Host", "api.b11k.example.com")
	localProxyReq.Header.Set("X-Forwarded-For", "203.0.113.44")
	localProxyReq.Header.Set("X-Forwarded-Proto", "https")

	s := &server{cfg: Config{WebProtocol: "https", PublicAPIHost: "api.b11k.example.com"}}
	if got := requestHost(localProxyReq); got != "api.b11k.example.com" {
		t.Fatalf("requestHost through trusted proxy = %q", got)
	}
	if got := clientIP(localProxyReq); got != "203.0.113.44" {
		t.Fatalf("clientIP through trusted proxy = %q", got)
	}
	if !s.isHTTPSRequest(localProxyReq) {
		t.Fatal("trusted forwarded HTTPS should be accepted")
	}

	publicReq := newHostRequest(t, http.MethodGet, "api.b11k.example.com", "/api/mobile/me", publicRemote)
	publicReq.Header.Set("X-Forwarded-Host", "b11k.example.com")
	publicReq.Header.Set("X-Forwarded-For", "203.0.113.44")
	publicReq.Header.Set("X-Forwarded-Proto", "https")
	if got := requestHost(publicReq); got != "api.b11k.example.com" {
		t.Fatalf("direct public request host = %q", got)
	}
	if got := clientIP(publicReq); got != "198.51.100.8" {
		t.Fatalf("direct public clientIP should ignore spoofed XFF, got %q", got)
	}
	if s.isHTTPSRequest(publicReq) {
		t.Fatal("direct public request should not be able to spoof HTTPS")
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

func TestParseBBoxRejectsInvalidCoordinates(t *testing.T) {
	tests := []string{
		"",
		"0,0,0,1",
		"181,0,182,1",
		"0,-91,1,0",
		"NaN,0,1,1",
		"0,0,+Inf,1",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, _, _, _, ok := parseBBox(raw); ok {
				t.Fatalf("parseBBox(%q) unexpectedly succeeded", raw)
			}
		})
	}

	if minLng, minLat, maxLng, maxLat, ok := parseBBox("-1,2,3,4"); !ok || minLng != -1 || minLat != 2 || maxLng != 3 || maxLat != 4 {
		t.Fatalf("valid bbox parsed as (%v, %v, %v, %v, %v)", minLng, minLat, maxLng, maxLat, ok)
	}
}

func TestMobileBearerTokenFormat(t *testing.T) {
	if !isPlausibleMobileBearerToken("abc_DEF-123") {
		t.Fatal("expected URL-safe bearer token to be accepted")
	}
	if isPlausibleMobileBearerToken("") {
		t.Fatal("empty token should be rejected")
	}
	if isPlausibleMobileBearerToken("abc.def") {
		t.Fatal("unexpected token punctuation should be rejected")
	}
	if isPlausibleMobileBearerToken("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz") {
		t.Fatal("oversized token should be rejected")
	}
}
