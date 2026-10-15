package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"geofence-backend/internal/store"
)

// handlerFunc is a handler that returns its result instead of writing it, so every response
// is built (and timed) in exactly one place.
type handlerFunc func(r *http.Request) (status int, body any, err error)

// apiError is an error carrying an HTTP status and optional per-field details.
type apiError struct {
	Status  int
	Message string
	Details []string
}

func (e *apiError) Error() string { return e.Message }

func badRequest(msg string, details ...string) *apiError {
	return &apiError{Status: http.StatusBadRequest, Message: msg, Details: details}
}

// wrap times the handler and writes the JSON response, always including "time_ns".
func (s *Server) wrap(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status, body, err := h(r)
		if err != nil {
			status, body = s.errorResponse(r, err)
		}
		writeJSON(w, status, start, body)
	}
}

func (s *Server) errorResponse(r *http.Request, err error) (int, any) {
	var ae *apiError
	var ve *store.ValidationError
	switch {
	case errors.As(err, &ae):
		out := map[string]any{"error": ae.Message}
		if len(ae.Details) > 0 {
			out["details"] = ae.Details
		}
		return ae.Status, out
	case errors.As(err, &ve):
		return http.StatusBadRequest, map[string]any{"error": ve.Msg}
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, map[string]any{"error": "resource not found"}
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict, map[string]any{"error": "resource already exists"}
	default:
		log.Printf("api: %s %s: %v", r.Method, r.URL.Path, err)
		return http.StatusInternalServerError, map[string]any{"error": "internal server error"}
	}
}

// writeJSON serialises body, merges in time_ns measured since start, and writes it.
func writeJSON(w http.ResponseWriter, status int, start time.Time, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		log.Printf("api: marshal response: %v", err)
		status, raw = http.StatusInternalServerError, []byte(`{"error":"internal server error"}`)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		fields = map[string]json.RawMessage{"data": raw}
	}
	// The elapsed time is captured last so it covers handler work plus serialisation.
	fields["time_ns"] = json.RawMessage(strconv.Quote(strconv.FormatInt(time.Since(start).Nanoseconds(), 10)))

	out, _ := json.Marshal(fields)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

// decodeJSON reads a size-limited JSON body into dst.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 5<<20))
	if err := dec.Decode(dst); err != nil {
		return badRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

// pagination parses ?limit and ?offset, applying defaults and an upper bound.
func pagination(r *http.Request, defLimit, maxLimit int) (limit, offset int, err error) {
	limit, offset = defLimit, 0
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 {
			return 0, 0, badRequest("limit must be a positive integer")
		}
		if limit > maxLimit {
			limit = maxLimit
		}
	}
	if v := q.Get("offset"); v != "" {
		if offset, err = strconv.Atoi(v); err != nil || offset < 0 {
			return 0, 0, badRequest("offset must be a non-negative integer")
		}
	}
	return limit, offset, nil
}

func parseTimeParam(r *http.Request, name string) (*time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, badRequest(name + " must be an ISO 8601 / RFC 3339 timestamp, e.g. 2025-01-01T00:00:00Z")
	}
	return &t, nil
}
