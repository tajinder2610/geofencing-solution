package api

import (
	"net/http"
	"slices"
	"strings"

	"geofence-backend/internal/geo"
	"geofence-backend/internal/store"
)

var geofenceCategories = []string{"delivery_zone", "restricted_zone", "toll_zone", "customer_area"}

type createGeofenceRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Coordinates [][]float64 `json:"coordinates"`
	Category    string      `json:"category"`
}

// parseGeofence decodes and validates a create/update body.
func parseGeofence(r *http.Request) (*store.Geofence, error) {
	var req createGeofenceRequest
	if err := decodeJSON(r, &req); err != nil {
		return nil, err
	}
	var problems []string
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		problems = append(problems, "name is required")
	}
	if !slices.Contains(geofenceCategories, req.Category) {
		problems = append(problems, "category must be one of: "+strings.Join(geofenceCategories, ", "))
	}
	problems = append(problems, geo.ValidatePolygon(req.Coordinates)...)
	if len(problems) > 0 {
		return nil, badRequest("validation failed", problems...)
	}
	return &store.Geofence{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		Coordinates: req.Coordinates,
		Category:    req.Category,
	}, nil
}

func (s *Server) createGeofence(r *http.Request) (int, any, error) {
	g, err := parseGeofence(r)
	if err != nil {
		return 0, nil, err
	}
	if err := s.store.CreateGeofence(r.Context(), g); err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, map[string]any{"id": g.ID, "name": g.Name, "status": "active"}, nil
}

// updateGeofence replaces a geofence (extra endpoint, used by the UI's edit action).
func (s *Server) updateGeofence(r *http.Request) (int, any, error) {
	g, err := parseGeofence(r)
	if err != nil {
		return 0, nil, err
	}
	g.ID = r.PathValue("geofence_id")
	if err := s.store.UpdateGeofence(r.Context(), g); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, g, nil
}

// deleteGeofence removes a geofence along with its alert rules and history (extra endpoint).
func (s *Server) deleteGeofence(r *http.Request) (int, any, error) {
	if err := s.store.DeleteGeofence(r.Context(), r.PathValue("geofence_id")); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"deleted": true}, nil
}

func (s *Server) listGeofences(r *http.Request) (int, any, error) {
	category := r.URL.Query().Get("category")
	if category != "" && !slices.Contains(geofenceCategories, category) {
		return 0, nil, badRequest("category must be one of: " + strings.Join(geofenceCategories, ", "))
	}
	limit, offset, err := pagination(r, 100, 1000)
	if err != nil {
		return 0, nil, err
	}
	items, total, err := s.store.ListGeofences(r.Context(), category, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"geofences": items, "total_count": total, "limit": limit, "offset": offset}, nil
}
