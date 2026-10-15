CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS geofences (
    id          text PRIMARY KEY,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    category    text NOT NULL CHECK (category IN ('delivery_zone', 'restricted_zone', 'toll_zone', 'customer_area')),
    coordinates jsonb NOT NULL,                       -- original [lat, lng] ring, returned verbatim by the API
    geom        geometry(Polygon, 4326) NOT NULL,     -- (lng, lat) polygon used for spatial queries
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS geofences_geom_gix ON geofences USING GIST (geom);
CREATE INDEX IF NOT EXISTS geofences_category_idx ON geofences (category);

CREATE TABLE IF NOT EXISTS vehicles (
    id             text PRIMARY KEY,
    vehicle_number text NOT NULL,
    driver_name    text NOT NULL,
    vehicle_type   text NOT NULL,
    phone          text NOT NULL,
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS vehicles_number_uq ON vehicles (upper(vehicle_number));

-- Append-only location history.
CREATE TABLE IF NOT EXISTS vehicle_locations (
    id          bigserial PRIMARY KEY,
    vehicle_id  text NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    latitude    double precision NOT NULL,
    longitude   double precision NOT NULL,
    recorded_at timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS vehicle_locations_latest_idx ON vehicle_locations (vehicle_id, recorded_at DESC, id DESC);

-- Which geofences each vehicle is currently inside; diffed against each new location to detect entry/exit.
CREATE TABLE IF NOT EXISTS vehicle_geofence_state (
    vehicle_id  text NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    geofence_id text NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    entered_at  timestamptz NOT NULL,
    PRIMARY KEY (vehicle_id, geofence_id)
);

CREATE TABLE IF NOT EXISTS alert_rules (
    id          text PRIMARY KEY,
    geofence_id text NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    vehicle_id  text REFERENCES vehicles (id) ON DELETE CASCADE,  -- NULL = applies to all vehicles
    event_type  text NOT NULL CHECK (event_type IN ('entry', 'exit', 'both')),
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS alert_rules_uq ON alert_rules (geofence_id, COALESCE(vehicle_id, ''), event_type);

-- Every detected entry/exit, whether or not an alert rule matched (powers /violations/history).
CREATE TABLE IF NOT EXISTS geofence_events (
    id          text PRIMARY KEY,
    vehicle_id  text NOT NULL REFERENCES vehicles (id) ON DELETE CASCADE,
    geofence_id text NOT NULL REFERENCES geofences (id) ON DELETE CASCADE,
    event_type  text NOT NULL CHECK (event_type IN ('entry', 'exit')),
    latitude    double precision NOT NULL,
    longitude   double precision NOT NULL,
    event_time  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS geofence_events_time_idx ON geofence_events (event_time DESC, id);
CREATE INDEX IF NOT EXISTS geofence_events_vehicle_idx ON geofence_events (vehicle_id, event_time DESC);
CREATE INDEX IF NOT EXISTS geofence_events_geofence_idx ON geofence_events (geofence_id, event_time DESC);

-- Alerts that were generated (a rule matched) and pushed to real-time clients.
CREATE TABLE IF NOT EXISTS alert_log (
    id               bigserial PRIMARY KEY,
    event_id         text NOT NULL REFERENCES geofence_events (id) ON DELETE CASCADE,
    rule_ids         text[] NOT NULL,
    payload          jsonb NOT NULL,
    clients_notified integer NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS alert_log_event_idx ON alert_log (event_id);
