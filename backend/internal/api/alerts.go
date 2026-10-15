package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"geofence-backend/internal/store"
)

// alertChannel is the Postgres NOTIFY channel used to fan alerts out across instances.
const alertChannel = "geofence_alerts"

var eventTypes = []string{"entry", "exit", "both"}

type configureAlertRequest struct {
	GeofenceID string  `json:"geofence_id"`
	VehicleID  *string `json:"vehicle_id"`
	EventType  string  `json:"event_type"`
}

func (s *Server) configureAlert(r *http.Request) (int, any, error) {
	var req configureAlertRequest
	if err := decodeJSON(r, &req); err != nil {
		return 0, nil, err
	}
	var problems []string
	if strings.TrimSpace(req.GeofenceID) == "" {
		problems = append(problems, "geofence_id is required")
	}
	if !slices.Contains(eventTypes, req.EventType) {
		problems = append(problems, "event_type must be one of: entry, exit, both")
	}
	if req.VehicleID != nil && strings.TrimSpace(*req.VehicleID) == "" {
		req.VehicleID = nil // empty string means "all vehicles"
	}
	if len(problems) > 0 {
		return 0, nil, badRequest("validation failed", problems...)
	}

	rule, err := s.store.CreateAlertRule(r.Context(), req.GeofenceID, req.VehicleID, req.EventType)
	switch err {
	case nil:
	case store.ErrNotFound:
		return 0, nil, &apiError{Status: http.StatusNotFound, Message: "geofence or vehicle not found"}
	case store.ErrConflict:
		return 0, nil, &apiError{Status: http.StatusConflict, Message: "an identical alert rule already exists"}
	default:
		return 0, nil, err
	}
	return http.StatusCreated, map[string]any{
		"alert_id":    rule.ID,
		"geofence_id": rule.GeofenceID,
		"vehicle_id":  rule.VehicleID,
		"event_type":  rule.EventType,
		"status":      rule.Status,
	}, nil
}

func (s *Server) listAlerts(r *http.Request) (int, any, error) {
	limit, offset, err := pagination(r, 100, 1000)
	if err != nil {
		return 0, nil, err
	}
	q := r.URL.Query()
	items, total, err := s.store.ListAlertRules(r.Context(), q.Get("geofence_id"), q.Get("vehicle_id"), limit, offset)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"alerts": items, "total_count": total, "limit": limit, "offset": offset}, nil
}

func (s *Server) updateAlertStatus(r *http.Request) (int, any, error) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return 0, nil, err
	}
	if req.Status != "active" && req.Status != "inactive" {
		return 0, nil, badRequest("status must be 'active' or 'inactive'")
	}
	rule, err := s.store.SetAlertRuleStatus(r.Context(), r.PathValue("alert_id"), req.Status)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, rule, nil
}

func (s *Server) deleteAlert(r *http.Request) (int, any, error) {
	if err := s.store.DeleteAlertRule(r.Context(), r.PathValue("alert_id")); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"deleted": true}, nil
}

// alertMessage is the WebSocket payload pushed to clients.
type alertMessage struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Vehicle   struct {
		VehicleID     string `json:"vehicle_id"`
		VehicleNumber string `json:"vehicle_number"`
		DriverName    string `json:"driver_name"`
	} `json:"vehicle"`
	Geofence struct {
		GeofenceID   string `json:"geofence_id"`
		GeofenceName string `json:"geofence_name"`
		Category     string `json:"category"`
	} `json:"geofence"`
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
}

// dispatchAlerts runs after the HTTP response: for each detected event it finds matching
// active rules, persists an alert record and broadcasts it to connected clients.
func (s *Server) dispatchAlerts(v store.Vehicle, events []store.GeofenceEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, ev := range events {
		ruleIDs, err := s.store.MatchingRuleIDs(ctx, ev.Geofence.GeofenceID, v.ID, ev.Type)
		if err != nil {
			log.Printf("alerts: match rules for %s: %v", ev.ID, err)
			continue
		}
		if len(ruleIDs) == 0 {
			continue
		}

		var msg alertMessage
		msg.EventID, msg.EventType, msg.Timestamp = ev.ID, ev.Type, ev.Timestamp
		msg.Vehicle.VehicleID, msg.Vehicle.VehicleNumber, msg.Vehicle.DriverName = v.ID, v.VehicleNumber, v.DriverName
		msg.Geofence.GeofenceID, msg.Geofence.GeofenceName, msg.Geofence.Category =
			ev.Geofence.GeofenceID, ev.Geofence.GeofenceName, ev.Geofence.Category
		msg.Location.Latitude, msg.Location.Longitude = ev.Latitude, ev.Longitude

		payload, err := json.Marshal(msg)
		if err != nil {
			log.Printf("alerts: marshal %s: %v", ev.ID, err)
			continue
		}
		if err := s.store.LogAlert(ctx, ev.ID, ruleIDs, payload); err != nil {
			log.Printf("alerts: persist %s: %v", ev.ID, err)
		}
		// Publish through Postgres so clients connected to *any* backend instance receive it.
		// If publishing fails, at least serve the clients connected to this instance.
		if err := s.store.Notify(ctx, alertChannel, string(payload)); err != nil {
			log.Printf("alerts: notify %s: %v (falling back to local broadcast)", ev.ID, err)
			s.hub.Broadcast(payload)
		}
	}
}

// RunAlertListener subscribes to alert notifications and relays each to this instance's WebSocket
// clients. It blocks until ctx is cancelled and should be started once per process.
func (s *Server) RunAlertListener(ctx context.Context) {
	for ctx.Err() == nil {
		err := s.store.Listen(ctx, alertChannel, func(payload string) {
			s.hub.Broadcast([]byte(payload))
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("alerts: listener stopped: %v (retrying)", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}
