package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"geofence-backend/internal/config"
	"geofence-backend/internal/ratelimit"
)

var ok200 = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func TestCORSPreflightAllowsEveryMethodTheUIUses(t *testing.T) {
	h := cors([]string{"https://app.example.com"}, ok200)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		req := httptest.NewRequest(http.MethodOptions, "/geofences/geo_1", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", method)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s preflight: status %d", method, rec.Code)
		}
		allowed := rec.Header().Get("Access-Control-Allow-Methods")
		if !contains(allowed, method) {
			t.Errorf("preflight must allow %s, got %q", method, allowed)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
			t.Errorf("allowed origin not echoed: %q", got)
		}
	}

	req := httptest.NewRequest(http.MethodOptions, "/geofences", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin must not receive CORS headers")
	}
}

func contains(list, item string) bool {
	for _, part := range splitComma(list) {
		if part == item {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ',' {
			out = append(out, cur)
			cur = ""
		} else if c != ' ' {
			cur += string(c)
		}
	}
	return append(out, cur)
}

func TestAPIKeyMiddleware(t *testing.T) {
	h := requireAPIKey("secret", ok200)
	cases := []struct {
		name   string
		method string
		path   string
		header map[string]string
		want   int
	}{
		{"no key", "GET", "/geofences", nil, 401},
		{"wrong key", "GET", "/geofences", map[string]string{"X-API-Key": "nope"}, 401},
		{"header key", "GET", "/geofences", map[string]string{"X-API-Key": "secret"}, 200},
		{"bearer key", "GET", "/geofences", map[string]string{"Authorization": "Bearer secret"}, 200},
		{"query key (websocket)", "GET", "/ws/alerts?api_key=secret", nil, 200},
		{"health is public", "GET", "/health", nil, 200},
		{"preflight is public", "OPTIONS", "/geofences", nil, 200},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		for k, v := range tc.header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	// Auth disabled when no key configured.
	rec := httptest.NewRecorder()
	requireAPIKey("", ok200).ServeHTTP(rec, httptest.NewRequest("GET", "/geofences", nil))
	if rec.Code != 200 {
		t.Errorf("auth should be off without a key, got %d", rec.Code)
	}
}

func TestRateLimitReturns429WithRetryAfter(t *testing.T) {
	s := &Server{cfg: config.Config{}, limiter: ratelimit.New(1, 2)}
	h := s.rateLimit(ok200)
	var codes []int
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("POST", "/vehicles/location", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
		if rec.Code == 429 && rec.Header().Get("Retry-After") == "" {
			t.Error("429 must carry Retry-After")
		}
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 || codes[3] != 429 {
		t.Fatalf("expected burst of 2 then 429s, got %v", codes)
	}
	// A different client has its own budget.
	req := httptest.NewRequest("POST", "/vehicles/location", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("other client should not be limited, got %d", rec.Code)
	}
}

func TestClientIPHonoursTrustProxyOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.9:555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := (&Server{cfg: config.Config{TrustProxy: false}}).clientIP(req); got != "10.0.0.9" {
		t.Errorf("must ignore X-Forwarded-For when proxy is not trusted, got %s", got)
	}
	if got := (&Server{cfg: config.Config{TrustProxy: true}}).clientIP(req); got != "203.0.113.7" {
		t.Errorf("should use first X-Forwarded-For hop when trusted, got %s", got)
	}
}
