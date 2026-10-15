package api

import (
	"bufio"
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// statusRecorder captures the response status for logging while still supporting WebSocket upgrades.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return s.ResponseWriter.(http.Hijacker).Hijack()
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Microsecond))
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeJSON(w, http.StatusInternalServerError, time.Now(), map[string]any{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// cors allows the configured origins ("*" allows any).
func cors(origins []string, next http.Handler) http.Handler {
	allowAll := slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || slices.Contains(origins, origin)) {
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAPIKey enforces the optional API key. /health is always public. Browsers cannot set
// headers on WebSocket handshakes, so the key may also be passed as ?api_key=.
func requireAPIKey(key string, next http.Handler) http.Handler {
	if key == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-API-Key")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if got == "" {
			got = r.URL.Query().Get("api_key")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeJSON(w, http.StatusUnauthorized, time.Now(), map[string]any{"error": "missing or invalid API key"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit applies the limiter to a single handler, keyed by client IP.
func (s *Server) rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := s.limiter.Allow(s.clientIP(r)); !ok {
			secs := int(wait.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeJSON(w, http.StatusTooManyRequests, time.Now(), map[string]any{
				"error": "rate limit exceeded, retry in " + strconv.Itoa(secs) + "s",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
