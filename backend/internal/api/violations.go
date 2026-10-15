package api

import (
	"net/http"
	"time"

	"geofence-backend/internal/store"
)

func (s *Server) violationHistory(r *http.Request) (int, any, error) {
	limit, offset, err := pagination(r, 50, 500)
	if err != nil {
		return 0, nil, err
	}
	start, err := parseTimeParam(r, "start_date")
	if err != nil {
		return 0, nil, err
	}
	end, err := parseTimeParam(r, "end_date")
	if err != nil {
		return 0, nil, err
	}
	if start != nil && end != nil && end.Before(*start) {
		return 0, nil, badRequest("end_date must not be before start_date")
	}

	q := r.URL.Query()
	items, total, err := s.store.ListViolations(r.Context(), storeFilter(q.Get("vehicle_id"), q.Get("geofence_id"), start, end, limit, offset))
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"violations": items, "total_count": total, "limit": limit, "offset": offset}, nil
}

func storeFilter(vehicleID, geofenceID string, start, end *time.Time, limit, offset int) store.ViolationFilter {
	return store.ViolationFilter{VehicleID: vehicleID, GeofenceID: geofenceID, Start: start, End: end, Limit: limit, Offset: offset}
}
