// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime settings for the server.
type Config struct {
	Port           string
	DatabaseURL    string
	APIKey         string   // optional; when empty authentication is disabled
	AllowedOrigins []string // CORS / WebSocket origins; "*" allows all
	RateLimitRPS   float64  // sustained requests per second per client on POST /vehicles/location
	RateLimitBurst int
	TrustProxy     bool // honour X-Forwarded-For when identifying clients
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	return Config{
		Port:           get("PORT", "8080"),
		DatabaseURL:    get("DATABASE_URL", "postgres://geo:geo@localhost:5432/geofence?sslmode=disable"),
		APIKey:         os.Getenv("API_KEY"),
		AllowedOrigins: splitList(get("ALLOWED_ORIGINS", "*")),
		RateLimitRPS:   getFloat("RATE_LIMIT_RPS", 50),
		RateLimitBurst: int(getFloat("RATE_LIMIT_BURST", 100)),
		TrustProxy:     get("TRUST_PROXY", "false") == "true",
	}
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
