package store

import "context"

// TruncateAllForTests empties every application table. Only call it against a disposable database.
func (s *Store) TruncateAllForTests(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `TRUNCATE alert_log, geofence_events, alert_rules, vehicle_geofence_state, vehicle_locations, vehicles, geofences CASCADE`)
	return err
}
