package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"geofence-backend/internal/geo"
	"geofence-backend/internal/store"
)

var phonePattern = regexp.MustCompile(`^\+?[0-9][0-9\s\-().]{5,19}$`)

type createVehicleRequest struct {
	VehicleNumber string `json:"vehicle_number"`
	DriverName    string `json:"driver_name"`
	VehicleType   string `json:"vehicle_type"`
	Phone         string `json:"phone"`
}

func (s *Server) createVehicle(r *http.Request) (int, any, error) {
	var req createVehicleRequest
	if err := decodeJSON(r, &req); err != nil {
		return 0, nil, err
	}
	req.VehicleNumber = strings.TrimSpace(req.VehicleNumber)
	req.DriverName = strings.TrimSpace(req.DriverName)
	req.VehicleType = strings.ToLower(strings.TrimSpace(req.VehicleType))
	req.Phone = strings.TrimSpace(req.Phone)

	var problems []string
	if req.VehicleNumber == "" {
		problems = append(problems, "vehicle_number is required")
	}
	if req.DriverName == "" {
		problems = append(problems, "driver_name is required")
	}
	if req.VehicleType == "" {
		problems = append(problems, "vehicle_type is required")
	}
	if req.Phone == "" {
		problems = append(problems, "phone is required")
	} else if !phonePattern.MatchString(req.Phone) {
		problems = append(problems, "phone must be a valid phone number, e.g. +1234567890")
	}
	if len(problems) > 0 {
		return 0, nil, badRequest("validation failed", problems...)
	}

	v := &store.Vehicle{VehicleNumber: req.VehicleNumber, DriverName: req.DriverName, VehicleType: req.VehicleType, Phone: req.Phone}
	if err := s.store.CreateVehicle(r.Context(), v); err != nil {
		if err == store.ErrConflict {
			return 0, nil, &apiError{Status: http.StatusConflict, Message: "a vehicle with number " + req.VehicleNumber + " is already registered"}
		}
		return 0, nil, err
	}
	return http.StatusCreated, map[string]any{"id": v.ID, "vehicle_number": v.VehicleNumber, "status": v.Status}, nil
}

func (s *Server) listVehicles(r *http.Request) (int, any, error) {
	limit, offset, err := pagination(r, 100, 1000)
	if err != nil {
		return 0, nil, err
	}
	withLocation := r.URL.Query().Get("include_location") == "true"
	items, total, err := s.store.ListVehicles(r.Context(), withLocation, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"vehicles": items, "total_count": total, "limit": limit, "offset": offset}, nil
}

func (s *Server) deleteVehicle(r *http.Request) (int, any, error) {
	id := r.PathValue("vehicle_id")
	if err := s.store.DeleteVehicle(r.Context(), id); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"deleted": true}, nil
}

func (s *Server) clearVehicleTrail(r *http.Request) (int, any, error) {
	id := r.PathValue("vehicle_id")
	if err := s.store.ClearVehicleTrail(r.Context(), id); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"cleared": true}, nil
}

type updateLocationRequest struct {
	VehicleID string   `json:"vehicle_id"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Timestamp string   `json:"timestamp"`
}

func (s *Server) updateLocation(r *http.Request) (int, any, error) {
	var req updateLocationRequest
	if err := decodeJSON(r, &req); err != nil {
		return 0, nil, err
	}

	var problems []string
	if strings.TrimSpace(req.VehicleID) == "" {
		problems = append(problems, "vehicle_id is required")
	}
	if req.Latitude == nil {
		problems = append(problems, "latitude is required")
	} else if !geo.ValidLatitude(*req.Latitude) {
		problems = append(problems, "latitude must be between -90 and 90")
	}
	if req.Longitude == nil {
		problems = append(problems, "longitude is required")
	} else if !geo.ValidLongitude(*req.Longitude) {
		problems = append(problems, "longitude must be between -180 and 180")
	}
	var ts time.Time
	if req.Timestamp == "" {
		problems = append(problems, "timestamp is required")
	} else if t, err := time.Parse(time.RFC3339, req.Timestamp); err != nil {
		problems = append(problems, "timestamp must be ISO 8601 / RFC 3339, e.g. 2025-01-15T10:35:00Z")
	} else {
		ts = t.UTC()
	}
	if len(problems) > 0 {
		return 0, nil, badRequest("validation failed", problems...)
	}

	res, err := s.store.RecordLocation(r.Context(), req.VehicleID, *req.Latitude, *req.Longitude, ts)
	if err != nil {
		return 0, nil, err
	}
	if len(res.Events) > 0 {
		// Alert delivery must not delay the HTTP response.
		go s.dispatchAlerts(res.Vehicle, res.Events)
	}
	return http.StatusOK, map[string]any{
		"vehicle_id":        res.Vehicle.ID,
		"location_updated":  true,
		"current_geofences": res.Current,
	}, nil
}

func (s *Server) getVehicleLocation(r *http.Request) (int, any, error) {
	id := r.PathValue("vehicle_id")
	v, loc, fences, err := s.store.CurrentStatus(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	for i := range fences {
		fences[i].Status = "" // the GET response exposes category only
	}
	return http.StatusOK, map[string]any{
		"vehicle_id":        v.ID,
		"vehicle_number":    v.VehicleNumber,
		"current_location":  loc, // null until the first location update
		"current_geofences": fences,
	}, nil
}

// vehicleTrail returns recent positions for drawing a path (extra endpoint). ?limit defaults to 100, max 1000.
func (s *Server) vehicleTrail(r *http.Request) (int, any, error) {
	limit, _, err := pagination(r, 100, 1000)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("vehicle_id")
	points, err := s.store.Trail(r.Context(), id, limit)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"vehicle_id": id, "points": points}, nil
}
