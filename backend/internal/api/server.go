// Package api exposes the HTTP/WebSocket interface of the geofencing service.
package api

import (
	"context"
	"net/http"
	"slices"
	"time"

	"geofence-backend/internal/config"
	"geofence-backend/internal/hub"
	"geofence-backend/internal/ratelimit"
	"geofence-backend/internal/store"
)

// Server wires the store, real-time hub and HTTP routes together.
type Server struct {
	cfg     config.Config
	store   *store.Store
	hub     *hub.Hub
	limiter *ratelimit.Limiter
}

// NewServer builds a Server. The returned Hub is shared so callers can close it on shutdown.
func NewServer(cfg config.Config, st *store.Store) *Server {
	checkOrigin := func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || slices.Contains(cfg.AllowedOrigins, "*") || slices.Contains(cfg.AllowedOrigins, origin)
	}
	return &Server{
		cfg:     cfg,
		store:   st,
		hub:     hub.New(checkOrigin),
		limiter: ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst),
	}
}

// Hub exposes the WebSocket hub (for graceful shutdown).
func (s *Server) Hub() *hub.Hub { return s.hub }

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.wrap(s.health))

	mux.HandleFunc("POST /geofences", s.wrap(s.createGeofence))
	mux.HandleFunc("GET /geofences", s.wrap(s.listGeofences))
	// Extras beyond the required endpoints, used by the UI.
	mux.HandleFunc("PUT /geofences/{geofence_id}", s.wrap(s.updateGeofence))
	mux.HandleFunc("DELETE /geofences/{geofence_id}", s.wrap(s.deleteGeofence))

	mux.HandleFunc("POST /vehicles", s.wrap(s.createVehicle))
	mux.HandleFunc("GET /vehicles", s.wrap(s.listVehicles))
	mux.HandleFunc("DELETE /vehicles/{vehicle_id}", s.wrap(s.deleteVehicle))
	mux.HandleFunc("DELETE /vehicles/{vehicle_id}/trail", s.wrap(s.clearVehicleTrail))
	mux.HandleFunc("POST /vehicles/location", s.rateLimit(s.wrap(s.updateLocation)))
	mux.HandleFunc("GET /vehicles/location/{vehicle_id}", s.wrap(s.getVehicleLocation))
	mux.HandleFunc("GET /vehicles/trail/{vehicle_id}", s.wrap(s.vehicleTrail))

	mux.HandleFunc("POST /alerts/configure", s.wrap(s.configureAlert))
	mux.HandleFunc("GET /alerts", s.wrap(s.listAlerts))
	mux.HandleFunc("PATCH /alerts/{alert_id}", s.wrap(s.updateAlertStatus))
	mux.HandleFunc("DELETE /alerts/{alert_id}", s.wrap(s.deleteAlert))

	mux.HandleFunc("GET /violations/history", s.wrap(s.violationHistory))

	mux.HandleFunc("GET /ws/alerts", s.hub.ServeWS)

	mux.HandleFunc("/", s.wrap(func(r *http.Request) (int, any, error) {
		return http.StatusNotFound, map[string]any{"error": "route not found"}, nil
	}))

	var h http.Handler = mux
	h = requireAPIKey(s.cfg.APIKey, h)
	h = cors(s.cfg.AllowedOrigins, h)
	h = recoverPanics(h)
	h = logRequests(h)
	return h
}

func (s *Server) health(r *http.Request) (int, any, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		return http.StatusServiceUnavailable, map[string]any{"status": "unavailable"}, nil
	}
	return http.StatusOK, map[string]any{"status": "ok", "ws_clients": s.hub.ClientCount()}, nil
}
