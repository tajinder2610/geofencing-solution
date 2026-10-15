package store

import (
	"errors"
	"time"
)

// Sentinel errors mapped to HTTP statuses by the API layer.
var (
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("resource already exists")
)

// ValidationError reports invalid caller input detected inside the store (e.g. invalid geometry).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

type Geofence struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Coordinates [][]float64 `json:"coordinates"`
	Category    string      `json:"category"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Vehicle struct {
	ID            string    `json:"id"`
	VehicleNumber string    `json:"vehicle_number"`
	DriverName    string    `json:"driver_name"`
	VehicleType   string    `json:"vehicle_type"`
	Phone         string    `json:"phone"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type Location struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Timestamp time.Time `json:"timestamp"`
}

// GeofenceRef is a lightweight reference to a geofence.
type GeofenceRef struct {
	GeofenceID   string `json:"geofence_id"`
	GeofenceName string `json:"geofence_name"`
	Category     string `json:"category,omitempty"`
	Status       string `json:"status,omitempty"`
}

// VehicleWithLocation is a vehicle optionally enriched with its latest position.
type VehicleWithLocation struct {
	Vehicle
	CurrentLocation  *Location     `json:"current_location,omitempty"`
	CurrentGeofences []GeofenceRef `json:"current_geofences,omitempty"`
}

type AlertRule struct {
	ID            string    `json:"alert_id"`
	GeofenceID    string    `json:"geofence_id"`
	GeofenceName  string    `json:"geofence_name"`
	VehicleID     *string   `json:"vehicle_id"`
	VehicleNumber *string   `json:"vehicle_number"`
	EventType     string    `json:"event_type"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// GeofenceEvent is a detected entry or exit.
type GeofenceEvent struct {
	ID        string
	Type      string // "entry" | "exit"
	Geofence  GeofenceRef
	Latitude  float64
	Longitude float64
	Timestamp time.Time
}

// LocationResult is the outcome of recording a location update.
type LocationResult struct {
	Vehicle Vehicle
	Current []GeofenceRef
	Events  []GeofenceEvent
	Stale   bool // the update was older than the latest known location; stored but no transitions evaluated
}

type Violation struct {
	ID            string    `json:"id"`
	VehicleID     string    `json:"vehicle_id"`
	VehicleNumber string    `json:"vehicle_number"`
	GeofenceID    string    `json:"geofence_id"`
	GeofenceName  string    `json:"geofence_name"`
	Category      string    `json:"category"`
	EventType     string    `json:"event_type"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	Timestamp     time.Time `json:"timestamp"`
}

type ViolationFilter struct {
	VehicleID  string
	GeofenceID string
	Start      *time.Time
	End        *time.Time
	Limit      int
	Offset     int
}
