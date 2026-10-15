// Package store is the PostgreSQL/PostGIS persistence layer.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/lib/pq"

	"geofence-backend/internal/geo"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database handle.
type Store struct {
	db  *sql.DB
	url string
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// pointExpr builds a PostGIS point from ($lng, $lat) parameter positions.
const pointExpr = "ST_SetSRID(ST_MakePoint(%s, %s), 4326)"

// Open connects to the database, retrying until it is reachable or ctx expires.
func Open(ctx context.Context, url string) (*Store, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	for {
		if err = db.PingContext(ctx); err == nil {
			return &Store{db: db, url: url}, nil
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, fmt.Errorf("database not reachable: %w", err)
		case <-time.After(time.Second):
		}
	}
}

// Migrate applies the embedded SQL migrations. All statements are idempotent.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Serialise concurrent starters (e.g. several replicas booting together).
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(727274)"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(727274)")

	for _, e := range entries {
		sqlText, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, string(sqlText)); err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                   { return s.db.Close() }

func newID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503"
}

// ---------------------------------------------------------------- geofences

// checkGeometry asks PostGIS whether the ring forms a valid polygon (rejects e.g. self-intersections).
func (s *Store) checkGeometry(ctx context.Context, wkt string) error {
	var valid bool
	var reason string
	if err := s.db.QueryRowContext(ctx,
		`SELECT ST_IsValid(ST_GeomFromText($1, 4326)), ST_IsValidReason(ST_GeomFromText($1, 4326))`, wkt,
	).Scan(&valid, &reason); err != nil {
		return &ValidationError{Msg: "coordinates do not form a valid polygon"}
	}
	if !valid {
		return &ValidationError{Msg: "coordinates do not form a valid polygon: " + reason}
	}
	return nil
}

// CreateGeofence validates the polygon with PostGIS and stores it.
func (s *Store) CreateGeofence(ctx context.Context, g *Geofence) error {
	wkt := geo.PolygonWKT(g.Coordinates)
	if err := s.checkGeometry(ctx, wkt); err != nil {
		return err
	}
	g.ID = newID("geo")
	coordsJSON, err := json.Marshal(g.Coordinates)
	if err != nil {
		return err
	}
	return s.db.QueryRowContext(ctx, `
		INSERT INTO geofences (id, name, description, category, coordinates, geom)
		VALUES ($1, $2, $3, $4, $5, ST_GeomFromText($6, 4326))
		RETURNING created_at`,
		g.ID, g.Name, g.Description, g.Category, coordsJSON, wkt,
	).Scan(&g.CreatedAt)
}

// UpdateGeofence replaces a geofence's attributes and boundary (g.ID selects the row).
// Vehicle membership for this geofence is recomputed from each vehicle's latest position
// *without* emitting entry/exit events, so editing a boundary never fires spurious alerts.
func (s *Store) UpdateGeofence(ctx context.Context, g *Geofence) error {
	wkt := geo.PolygonWKT(g.Coordinates)
	if err := s.checkGeometry(ctx, wkt); err != nil {
		return err
	}
	coordsJSON, err := json.Marshal(g.Coordinates)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `
		UPDATE geofences SET name = $2, description = $3, category = $4, coordinates = $5, geom = ST_GeomFromText($6, 4326)
		WHERE id = $1 RETURNING created_at`,
		g.ID, g.Name, g.Description, g.Category, coordsJSON, wkt,
	).Scan(&g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vehicle_geofence_state WHERE geofence_id = $1`, g.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO vehicle_geofence_state (vehicle_id, geofence_id, entered_at)
		SELECT l.vehicle_id, g.id, l.recorded_at
		FROM (SELECT DISTINCT ON (vehicle_id) vehicle_id, latitude, longitude, recorded_at
		      FROM vehicle_locations ORDER BY vehicle_id, recorded_at DESC, id DESC) l
		JOIN geofences g ON g.id = $1 AND ST_Covers(g.geom, `+fmt.Sprintf(pointExpr, "l.longitude", "l.latitude")+`)`, g.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteGeofence removes a geofence; its rules, state and history cascade.
func (s *Store) DeleteGeofence(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM geofences WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGeofences returns geofences, optionally filtered by category, plus the unpaginated total.
func (s *Store) ListGeofences(ctx context.Context, category string, limit, offset int) ([]Geofence, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM geofences WHERE ($1 = '' OR category = $1)`, category).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, category, coordinates, created_at
		FROM geofences WHERE ($1 = '' OR category = $1)
		ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`, category, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]Geofence, 0)
	for rows.Next() {
		var g Geofence
		var raw []byte
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Category, &raw, &g.CreatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(raw, &g.Coordinates); err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------- vehicles

func (s *Store) CreateVehicle(ctx context.Context, v *Vehicle) error {
	v.ID = newID("veh")
	v.Status = "active"
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO vehicles (id, vehicle_number, driver_name, vehicle_type, phone, status)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`,
		v.ID, v.VehicleNumber, v.DriverName, v.VehicleType, v.Phone, v.Status,
	).Scan(&v.CreatedAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetVehicle(ctx context.Context, id string) (*Vehicle, error) {
	return scanVehicle(s.db.QueryRowContext(ctx, vehicleCols+` WHERE id = $1`, id))
}

const vehicleCols = `SELECT id, vehicle_number, driver_name, vehicle_type, phone, status, created_at FROM vehicles`

func scanVehicle(row *sql.Row) (*Vehicle, error) {
	var v Vehicle
	err := row.Scan(&v.ID, &v.VehicleNumber, &v.DriverName, &v.VehicleType, &v.Phone, &v.Status, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &v, err
}

// ListVehicles returns vehicles; with withLocation each carries its latest position and containing geofences.
func (s *Store) ListVehicles(ctx context.Context, withLocation bool, limit, offset int) ([]VehicleWithLocation, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vehicles`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, vehicleCols+` ORDER BY created_at DESC, id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]VehicleWithLocation, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var v VehicleWithLocation
		if err := rows.Scan(&v.ID, &v.VehicleNumber, &v.DriverName, &v.VehicleType, &v.Phone, &v.Status, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
		ids = append(ids, v.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if !withLocation || len(out) == 0 {
		return out, total, nil
	}

	locs, err := s.latestLocations(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	fences, err := s.currentGeofencesFor(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		if l, ok := locs[out[i].ID]; ok {
			l := l
			out[i].CurrentLocation = &l
			out[i].CurrentGeofences = fences[out[i].ID]
		}
	}
	return out, total, nil
}

func (s *Store) latestLocations(ctx context.Context, vehicleIDs []string) (map[string]Location, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.vid, l.latitude, l.longitude, l.recorded_at
		FROM unnest($1::text[]) AS v(vid)
		CROSS JOIN LATERAL (
			SELECT latitude, longitude, recorded_at FROM vehicle_locations
			WHERE vehicle_id = v.vid ORDER BY recorded_at DESC, id DESC LIMIT 1) l`, pq.Array(vehicleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]Location)
	for rows.Next() {
		var id string
		var l Location
		if err := rows.Scan(&id, &l.Latitude, &l.Longitude, &l.Timestamp); err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, rows.Err()
}

// currentGeofencesFor evaluates the latest position of each vehicle against all geofences in one query.
func (s *Store) currentGeofencesFor(ctx context.Context, vehicleIDs []string) (map[string][]GeofenceRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.vid, g.id, g.name, g.category
		FROM unnest($1::text[]) AS v(vid)
		CROSS JOIN LATERAL (
			SELECT latitude, longitude FROM vehicle_locations
			WHERE vehicle_id = v.vid ORDER BY recorded_at DESC, id DESC LIMIT 1) l
		JOIN geofences g ON ST_Covers(g.geom, `+fmt.Sprintf(pointExpr, "l.longitude", "l.latitude")+`)
		ORDER BY g.name`, pq.Array(vehicleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]GeofenceRef)
	for rows.Next() {
		var vid string
		var r GeofenceRef
		if err := rows.Scan(&vid, &r.GeofenceID, &r.GeofenceName, &r.Category); err != nil {
			return nil, err
		}
		out[vid] = append(out[vid], r)
	}
	return out, rows.Err()
}

// CurrentStatus returns a vehicle with its latest location (nil if none) and containing geofences.
func (s *Store) CurrentStatus(ctx context.Context, vehicleID string) (*Vehicle, *Location, []GeofenceRef, error) {
	v, err := s.GetVehicle(ctx, vehicleID)
	if err != nil {
		return nil, nil, nil, err
	}
	locs, err := s.latestLocations(ctx, []string{vehicleID})
	if err != nil {
		return nil, nil, nil, err
	}
	loc, ok := locs[vehicleID]
	if !ok {
		return v, nil, []GeofenceRef{}, nil
	}
	fences, err := s.currentGeofencesFor(ctx, []string{vehicleID})
	if err != nil {
		return nil, nil, nil, err
	}
	current := fences[vehicleID]
	if current == nil {
		current = []GeofenceRef{}
	}
	return v, &loc, current, nil
}

// ---------------------------------------------------------------- locations

// RecordLocation stores a position and detects geofence entry/exit by diffing against the
// vehicle's previously known state. The vehicle row is locked so concurrent updates for the
// same vehicle are serialised and cannot emit duplicate events.
func (s *Store) RecordLocation(ctx context.Context, vehicleID string, lat, lng float64, ts time.Time) (*LocationResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var v Vehicle
	err = tx.QueryRowContext(ctx, vehicleCols+` WHERE id = $1 FOR UPDATE`, vehicleID).
		Scan(&v.ID, &v.VehicleNumber, &v.DriverName, &v.VehicleType, &v.Phone, &v.Status, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var latest sql.NullTime
	if err := tx.QueryRowContext(ctx,
		`SELECT max(recorded_at) FROM vehicle_locations WHERE vehicle_id = $1`, vehicleID).Scan(&latest); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO vehicle_locations (vehicle_id, latitude, longitude, recorded_at) VALUES ($1, $2, $3, $4)`,
		vehicleID, lat, lng, ts); err != nil {
		return nil, err
	}

	res := &LocationResult{Vehicle: v, Events: []GeofenceEvent{}}

	// Out-of-order update: keep it in history but leave the state machine untouched.
	if latest.Valid && ts.Before(latest.Time) {
		res.Stale = true
		if res.Current, err = stateRefs(ctx, tx, vehicleID); err != nil {
			return nil, err
		}
		return res, tx.Commit()
	}

	inside, err := geofencesContaining(ctx, tx, lat, lng)
	if err != nil {
		return nil, err
	}
	prev, err := stateRefs(ctx, tx, vehicleID)
	if err != nil {
		return nil, err
	}

	insideSet := make(map[string]GeofenceRef, len(inside))
	for _, r := range inside {
		insideSet[r.GeofenceID] = r
	}
	prevSet := make(map[string]GeofenceRef, len(prev))
	for _, r := range prev {
		prevSet[r.GeofenceID] = r
	}

	var exited, entered []GeofenceRef
	for id, r := range prevSet {
		if _, still := insideSet[id]; !still {
			exited = append(exited, r)
		}
	}
	for id, r := range insideSet {
		if _, was := prevSet[id]; !was {
			entered = append(entered, r)
		}
	}
	sortRefs(exited)
	sortRefs(entered)

	for _, r := range exited {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM vehicle_geofence_state WHERE vehicle_id = $1 AND geofence_id = $2`, vehicleID, r.GeofenceID); err != nil {
			return nil, err
		}
		if err := insertEvent(ctx, tx, res, vehicleID, "exit", r, lat, lng, ts); err != nil {
			return nil, err
		}
	}
	for _, r := range entered {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO vehicle_geofence_state (vehicle_id, geofence_id, entered_at) VALUES ($1, $2, $3)
			 ON CONFLICT DO NOTHING`, vehicleID, r.GeofenceID, ts); err != nil {
			return nil, err
		}
		if err := insertEvent(ctx, tx, res, vehicleID, "entry", r, lat, lng, ts); err != nil {
			return nil, err
		}
	}

	res.Current = inside
	return res, tx.Commit()
}

func insertEvent(ctx context.Context, q querier, res *LocationResult, vehicleID, typ string, g GeofenceRef, lat, lng float64, ts time.Time) error {
	ev := GeofenceEvent{ID: newID("evt"), Type: typ, Geofence: g, Latitude: lat, Longitude: lng, Timestamp: ts}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO geofence_events (id, vehicle_id, geofence_id, event_type, latitude, longitude, event_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ev.ID, vehicleID, g.GeofenceID, typ, lat, lng, ts); err != nil {
		return err
	}
	res.Events = append(res.Events, ev)
	return nil
}

func sortRefs(refs []GeofenceRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].GeofenceName != refs[j].GeofenceName {
			return refs[i].GeofenceName < refs[j].GeofenceName
		}
		return refs[i].GeofenceID < refs[j].GeofenceID
	})
}

// geofencesContaining runs the spatial point-in-polygon query (uses the GiST index; boundary counts as inside).
func geofencesContaining(ctx context.Context, q querier, lat, lng float64) ([]GeofenceRef, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, name, category FROM geofences WHERE ST_Covers(geom, `+fmt.Sprintf(pointExpr, "$1", "$2")+`) ORDER BY name, id`,
		lng, lat)
	if err != nil {
		return nil, err
	}
	return scanRefs(rows, "inside")
}

// stateRefs returns the geofences a vehicle is recorded as being inside.
func stateRefs(ctx context.Context, q querier, vehicleID string) ([]GeofenceRef, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT g.id, g.name, g.category FROM vehicle_geofence_state s
		JOIN geofences g ON g.id = s.geofence_id WHERE s.vehicle_id = $1 ORDER BY g.name, g.id`, vehicleID)
	if err != nil {
		return nil, err
	}
	return scanRefs(rows, "inside")
}

func scanRefs(rows *sql.Rows, status string) ([]GeofenceRef, error) {
	defer rows.Close()
	out := make([]GeofenceRef, 0)
	for rows.Next() {
		var r GeofenceRef
		if err := rows.Scan(&r.GeofenceID, &r.GeofenceName, &r.Category); err != nil {
			return nil, err
		}
		r.Status = status
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- alert rules

func (s *Store) CreateAlertRule(ctx context.Context, geofenceID string, vehicleID *string, eventType string) (*AlertRule, error) {
	id := newID("alert")
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_rules (id, geofence_id, vehicle_id, event_type) VALUES ($1, $2, $3, $4)`,
		id, geofenceID, vehicleID, eventType)
	switch {
	case isUniqueViolation(err):
		return nil, ErrConflict
	case isForeignKeyViolation(err):
		return nil, ErrNotFound
	case err != nil:
		return nil, err
	}
	return s.GetAlertRule(ctx, id)
}

const ruleSelect = `
	SELECT r.id, r.geofence_id, g.name, r.vehicle_id, v.vehicle_number, r.event_type, r.status, r.created_at
	FROM alert_rules r
	JOIN geofences g ON g.id = r.geofence_id
	LEFT JOIN vehicles v ON v.id = r.vehicle_id`

func scanRule(sc interface{ Scan(...any) error }) (*AlertRule, error) {
	var r AlertRule
	err := sc.Scan(&r.ID, &r.GeofenceID, &r.GeofenceName, &r.VehicleID, &r.VehicleNumber, &r.EventType, &r.Status, &r.CreatedAt)
	return &r, err
}

func (s *Store) GetAlertRule(ctx context.Context, id string) (*AlertRule, error) {
	r, err := scanRule(s.db.QueryRowContext(ctx, ruleSelect+` WHERE r.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (s *Store) ListAlertRules(ctx context.Context, geofenceID, vehicleID string, limit, offset int) ([]AlertRule, int, error) {
	const where = ` WHERE ($1 = '' OR r.geofence_id = $1) AND ($2 = '' OR r.vehicle_id = $2)`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM alert_rules r`+where, geofenceID, vehicleID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, ruleSelect+where+` ORDER BY r.created_at DESC, r.id LIMIT $3 OFFSET $4`,
		geofenceID, vehicleID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AlertRule, 0)
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

func (s *Store) SetAlertRuleStatus(ctx context.Context, id, status string) (*AlertRule, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alert_rules SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetAlertRule(ctx, id)
}

func (s *Store) DeleteAlertRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MatchingRuleIDs returns active rules that apply to this vehicle/geofence/event combination.
func (s *Store) MatchingRuleIDs(ctx context.Context, geofenceID, vehicleID, eventType string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM alert_rules
		WHERE geofence_id = $1 AND status = 'active'
		  AND (vehicle_id IS NULL OR vehicle_id = $2)
		  AND event_type IN ($3, 'both')
		ORDER BY id`, geofenceID, vehicleID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LogAlert persists a generated alert.
func (s *Store) LogAlert(ctx context.Context, eventID string, ruleIDs []string, payload []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_log (event_id, rule_ids, payload) VALUES ($1, $2, $3)`,
		eventID, pq.Array(ruleIDs), payload)
	return err
}

// ---------------------------------------------------------------- history

// ListViolations returns entry/exit history, newest first, plus the total matching count.
func (s *Store) ListViolations(ctx context.Context, f ViolationFilter) ([]Violation, int, error) {
	const where = `
		WHERE ($1 = '' OR e.vehicle_id = $1)
		  AND ($2 = '' OR e.geofence_id = $2)
		  AND ($3::timestamptz IS NULL OR e.event_time >= $3)
		  AND ($4::timestamptz IS NULL OR e.event_time <= $4)`
	args := []any{f.VehicleID, f.GeofenceID, f.Start, f.End}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM geofence_events e`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.vehicle_id, v.vehicle_number, e.geofence_id, g.name, g.category,
		       e.event_type, e.latitude, e.longitude, e.event_time
		FROM geofence_events e
		JOIN vehicles v ON v.id = e.vehicle_id
		JOIN geofences g ON g.id = e.geofence_id`+where+`
		ORDER BY e.event_time DESC, e.id DESC LIMIT $5 OFFSET $6`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]Violation, 0)
	for rows.Next() {
		var v Violation
		var eventID string
		if err := rows.Scan(&eventID, &v.VehicleID, &v.VehicleNumber, &v.GeofenceID, &v.GeofenceName, &v.Category,
			&v.EventType, &v.Latitude, &v.Longitude, &v.Timestamp); err != nil {
			return nil, 0, err
		}
		v.ID = "viol_" + eventID[len("evt_"):]
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------- trail

// Trail returns a vehicle's most recent positions in chronological order.
func (s *Store) Trail(ctx context.Context, vehicleID string, limit int) ([]Location, error) {
	if _, err := s.GetVehicle(ctx, vehicleID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT latitude, longitude, recorded_at FROM (
			SELECT latitude, longitude, recorded_at, id FROM vehicle_locations
			WHERE vehicle_id = $1 ORDER BY recorded_at DESC, id DESC LIMIT $2) t
		ORDER BY recorded_at ASC, id ASC`, vehicleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Location, 0)
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.Latitude, &l.Longitude, &l.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- cross-instance fan-out

// Notify publishes payload on a Postgres NOTIFY channel (payload limit is ~8 kB).
func (s *Store) Notify(ctx context.Context, channel, payload string) error {
	_, err := s.db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, channel, payload)
	return err
}

// Listen blocks, calling handler for every NOTIFY on channel, until ctx is cancelled.
// The connection is re-established automatically; notifications sent while it was down are lost
// (delivery is best-effort — everything is still in the history tables).
func (s *Store) Listen(ctx context.Context, channel string, handler func(payload string)) error {
	l := pq.NewListener(s.url, time.Second, 30*time.Second, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("store: listener event %d: %v", ev, err)
		}
	})
	defer l.Close()
	if err := l.Listen(channel); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case n := <-l.Notify:
			if n != nil { // nil signals a reconnect
				handler(n.Extra)
			}
		case <-time.After(60 * time.Second):
			go l.Ping() // keep-alive; surfaces dead connections
		}
	}
}
