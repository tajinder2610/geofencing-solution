package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"geofence-backend/internal/config"
	"geofence-backend/internal/store"
)

// Integration test: needs a PostGIS database. Set TEST_DATABASE_URL to run it, e.g.
// TEST_DATABASE_URL=postgres://geo:geo@localhost:5432/geofence_test?sslmode=disable go test ./...
//
// WARNING: the test TRUNCATES all application tables first so results are deterministic;
// point it at a disposable database, never at data you care about.
func newTestServer(t *testing.T) *httptest.Server { return newTestServerOpt(t, true) }

func newTestServerOpt(t *testing.T, truncate bool) *httptest.Server {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if truncate {
		if err := st.TruncateAllForTests(ctx); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { st.Close() })

	cfg := config.Config{AllowedOrigins: []string{"*"}, RateLimitRPS: 1000, RateLimitBurst: 1000}
	srv := NewServer(cfg, st)
	ts := httptest.NewServer(srv.Handler())
	listenCtx, stopListen := context.WithCancel(context.Background())
	go srv.RunAlertListener(listenCtx)
	t.Cleanup(func() { stopListen(); srv.Hub().Close(); ts.Close() })
	time.Sleep(300 * time.Millisecond) // let LISTEN establish
	return ts
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	if _, ok := out["time_ns"].(string); !ok {
		t.Errorf("%s %s: response missing string time_ns: %v", method, path, out)
	}
	return resp.StatusCode, out
}

func TestGeofenceLifecycleEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	suffix := time.Now().Format("150405.000000")

	// Geofence around central San Francisco block.
	status, geo := call(t, ts, "POST", "/geofences", map[string]any{
		"name": "Restricted " + suffix, "category": "restricted_zone",
		"coordinates": [][]float64{{37.7749, -122.4194}, {37.7849, -122.4194}, {37.7849, -122.4094}, {37.7749, -122.4094}, {37.7749, -122.4194}},
	})
	if status != 201 || geo["status"] != "active" {
		t.Fatalf("create geofence: %d %v", status, geo)
	}
	geoID := geo["id"].(string)

	status, veh := call(t, ts, "POST", "/vehicles", map[string]any{
		"vehicle_number": "KA-" + suffix, "driver_name": "John Doe", "vehicle_type": "truck", "phone": "+1234567890",
	})
	if status != 201 {
		t.Fatalf("create vehicle: %d %v", status, veh)
	}
	vehID := veh["id"].(string)

	if status, _ = call(t, ts, "POST", "/vehicles", map[string]any{
		"vehicle_number": strings.ToLower("KA-" + suffix), "driver_name": "X", "vehicle_type": "car", "phone": "+1234567890",
	}); status != 409 {
		t.Errorf("duplicate vehicle number should be 409, got %d", status)
	}

	status, rule := call(t, ts, "POST", "/alerts/configure", map[string]any{"geofence_id": geoID, "event_type": "both"})
	if status != 201 || rule["vehicle_id"] != nil {
		t.Fatalf("configure alert: %d %v", status, rule)
	}

	// Connect a WebSocket client before triggering events.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/alerts"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	time.Sleep(100 * time.Millisecond)

	readAlert := func() map[string]any {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var m map[string]any
		if err := conn.ReadJSON(&m); err != nil {
			t.Fatalf("expected a WebSocket alert: %v", err)
		}
		return m
	}
	base := time.Now().UTC().Truncate(time.Second)
	locate := func(lat, lng float64, at time.Time) map[string]any {
		status, out := call(t, ts, "POST", "/vehicles/location", map[string]any{
			"vehicle_id": vehID, "latitude": lat, "longitude": lng, "timestamp": at.Format(time.RFC3339),
		})
		if status != 200 {
			t.Fatalf("location update: %d %v", status, out)
		}
		return out
	}

	// Outside -> no events.
	out := locate(37.70, -122.50, base)
	if n := len(out["current_geofences"].([]any)); n != 0 {
		t.Fatalf("expected outside, got %v", out["current_geofences"])
	}

	// Entry.
	out = locate(37.7799, -122.4144, base.Add(time.Minute))
	fences := out["current_geofences"].([]any)
	if len(fences) != 1 || fences[0].(map[string]any)["status"] != "inside" {
		t.Fatalf("expected inside one geofence, got %v", fences)
	}
	alert := readAlert()
	if alert["event_type"] != "entry" || alert["geofence"].(map[string]any)["geofence_id"] != geoID ||
		alert["vehicle"].(map[string]any)["vehicle_id"] != vehID {
		t.Fatalf("unexpected entry alert: %v", alert)
	}

	// Staying inside must not repeat the alert.
	locate(37.7801, -122.4150, base.Add(2*time.Minute))

	// Boundary point counts as inside (ST_Covers).
	locate(37.7749, -122.4194, base.Add(3*time.Minute))

	// Exit.
	locate(37.70, -122.50, base.Add(4*time.Minute))
	alert = readAlert()
	if alert["event_type"] != "exit" {
		t.Fatalf("expected exit alert, got %v", alert)
	}

	// An out-of-order (stale) update is stored but must not generate events.
	locate(37.7799, -122.4144, base.Add(30*time.Second))

	// Current location reflects the newest timestamp (outside).
	status, cur := call(t, ts, "GET", "/vehicles/location/"+vehID, nil)
	if status != 200 || len(cur["current_geofences"].([]any)) != 0 {
		t.Fatalf("get location: %d %v", status, cur)
	}

	// History: exactly one entry and one exit.
	status, hist := call(t, ts, "GET", "/violations/history?vehicle_id="+vehID, nil)
	if status != 200 || hist["total_count"].(float64) != 2 {
		t.Fatalf("history: %d %v", status, hist)
	}
	status, hist = call(t, ts, "GET", "/violations/history?vehicle_id="+vehID+"&limit=1&offset=1", nil)
	if status != 200 || len(hist["violations"].([]any)) != 1 || hist["total_count"].(float64) != 2 {
		t.Fatalf("history pagination: %d %v", status, hist)
	}

	// Validation & not-found paths.
	if status, _ = call(t, ts, "POST", "/vehicles/location", map[string]any{"vehicle_id": "veh_nope", "latitude": 1, "longitude": 1, "timestamp": base.Format(time.RFC3339)}); status != 404 {
		t.Errorf("unknown vehicle should be 404, got %d", status)
	}
	if status, _ = call(t, ts, "POST", "/vehicles/location", map[string]any{"vehicle_id": vehID, "latitude": 95, "longitude": 1, "timestamp": "bad"}); status != 400 {
		t.Errorf("bad coordinates should be 400, got %d", status)
	}
	if status, _ = call(t, ts, "POST", "/geofences", map[string]any{
		"name": "bowtie", "category": "toll_zone",
		"coordinates": [][]float64{{0, 0}, {1, 1}, {1, 0}, {0, 1}, {0, 0}},
	}); status != 400 {
		t.Errorf("self-intersecting polygon should be 400, got %d", status)
	}
	if status, _ = call(t, ts, "GET", "/geofences?category=nope", nil); status != 400 {
		t.Errorf("unknown category filter should be 400, got %d", status)
	}

	// Alert rule management + list filters.
	status, list := call(t, ts, "GET", "/alerts?geofence_id="+geoID, nil)
	if status != 200 || len(list["alerts"].([]any)) != 1 {
		t.Fatalf("list alerts: %d %v", status, list)
	}
	ruleID := rule["alert_id"].(string)
	if status, _ = call(t, ts, "PATCH", "/alerts/"+ruleID, map[string]any{"status": "inactive"}); status != 200 {
		t.Errorf("patch alert: %d", status)
	}
	if status, _ = call(t, ts, "DELETE", "/alerts/"+ruleID, nil); status != 200 {
		t.Errorf("delete alert: %d", status)
	}

	// Vehicles list with location enrichment.
	status, vlist := call(t, ts, "GET", "/vehicles?include_location=true", nil)
	if status != 200 || vlist["vehicles"] == nil {
		t.Fatalf("list vehicles: %d %v", status, vlist)
	}
}

func square(lat, lng, size float64) [][]float64 {
	return [][]float64{{lat, lng}, {lat + size, lng}, {lat + size, lng + size}, {lat, lng + size}, {lat, lng}}
}

// An alert produced by instance A must reach a WebSocket client connected to instance B.
func TestAlertFanOutAcrossInstances(t *testing.T) {
	a := newTestServer(t)
	b := newTestServerOpt(t, false)

	_, geo := call(t, a, "POST", "/geofences", map[string]any{"name": "Fanout", "category": "restricted_zone", "coordinates": square(10, 10, 1)})
	_, veh := call(t, a, "POST", "/vehicles", map[string]any{"vehicle_number": "FAN-1", "driver_name": "D", "vehicle_type": "car", "phone": "+1234567890"})
	call(t, a, "POST", "/alerts/configure", map[string]any{"geofence_id": geo["id"], "event_type": "entry"})

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(b.URL, "http")+"/ws/alerts", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Location update goes to instance A; the client is on instance B.
	call(t, a, "POST", "/vehicles/location", map[string]any{"vehicle_id": veh["id"], "latitude": 10.5, "longitude": 10.5, "timestamp": time.Now().UTC().Format(time.RFC3339)})

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg map[string]any
	if err := conn.ReadJSON(&msg); err != nil || msg["event_type"] != "entry" {
		t.Fatalf("client on instance B should receive instance A's alert, got %v err %v", msg, err)
	}
}

func TestGeofenceEditDeleteAndTrail(t *testing.T) {
	ts := newTestServer(t)

	_, geo := call(t, ts, "POST", "/geofences", map[string]any{"name": "Editable", "category": "toll_zone", "coordinates": square(20, 20, 1)})
	geoID := geo["id"].(string)
	_, veh := call(t, ts, "POST", "/vehicles", map[string]any{"vehicle_number": "EDIT-1", "driver_name": "D", "vehicle_type": "van", "phone": "+1234567890"})
	vehID := veh["id"].(string)
	call(t, ts, "POST", "/alerts/configure", map[string]any{"geofence_id": geoID, "event_type": "both"})

	base := time.Now().UTC().Truncate(time.Second)
	locate := func(lat, lng float64, at time.Time) {
		if status, out := call(t, ts, "POST", "/vehicles/location", map[string]any{"vehicle_id": vehID, "latitude": lat, "longitude": lng, "timestamp": at.Format(time.RFC3339)}); status != 200 {
			t.Fatalf("location: %d %v", status, out)
		}
	}
	locate(20.5, 20.5, base) // entry

	// Move the boundary away from the vehicle: renamed, membership recomputed silently.
	status, out := call(t, ts, "PUT", "/geofences/"+geoID, map[string]any{"name": "Moved", "category": "toll_zone", "coordinates": square(40, 40, 1)})
	if status != 200 || out["name"] != "Moved" {
		t.Fatalf("update geofence: %d %v", status, out)
	}
	_, cur := call(t, ts, "GET", "/vehicles/location/"+vehID, nil)
	if len(cur["current_geofences"].([]any)) != 0 {
		t.Fatalf("vehicle should no longer be inside after the edit: %v", cur)
	}
	locate(50, 50, base.Add(time.Minute)) // still outside -> must NOT produce an exit event
	_, hist := call(t, ts, "GET", "/violations/history?vehicle_id="+vehID, nil)
	if hist["total_count"].(float64) != 1 {
		t.Fatalf("editing a boundary must not create events; history = %v", hist)
	}

	// Trail: chronological, includes both points.
	status, trail := call(t, ts, "GET", "/vehicles/trail/"+vehID, nil)
	pts := trail["points"].([]any)
	if status != 200 || len(pts) != 2 || pts[0].(map[string]any)["latitude"].(float64) != 20.5 {
		t.Fatalf("trail: %d %v", status, trail)
	}
	if status, _ = call(t, ts, "GET", "/vehicles/trail/veh_nope", nil); status != 404 {
		t.Errorf("trail for unknown vehicle should be 404, got %d", status)
	}

	// Invalid edits are rejected; unknown ids are 404.
	if status, _ = call(t, ts, "PUT", "/geofences/"+geoID, map[string]any{"name": "x", "category": "nope", "coordinates": square(1, 1, 1)}); status != 400 {
		t.Errorf("invalid update should be 400, got %d", status)
	}
	if status, _ = call(t, ts, "PUT", "/geofences/geo_nope", map[string]any{"name": "x", "category": "toll_zone", "coordinates": square(1, 1, 1)}); status != 404 {
		t.Errorf("update of unknown geofence should be 404, got %d", status)
	}

	// Delete cascades to rules.
	if status, _ = call(t, ts, "DELETE", "/geofences/"+geoID, nil); status != 200 {
		t.Fatalf("delete geofence: %d", status)
	}
	if status, _ = call(t, ts, "DELETE", "/geofences/"+geoID, nil); status != 404 {
		t.Errorf("second delete should be 404, got %d", status)
	}
	_, rules := call(t, ts, "GET", "/alerts", nil)
	if len(rules["alerts"].([]any)) != 0 {
		t.Errorf("alert rules should cascade-delete: %v", rules)
	}
}

func TestVehicleTrailClearAndDelete(t *testing.T) {
	ts := newTestServer(t)
	_, geo := call(t, ts, "POST", "/geofences", map[string]any{
		"name": "Vehicle lifecycle", "category": "restricted_zone", "coordinates": square(10, 10, 1),
	})
	_, vehicle := call(t, ts, "POST", "/vehicles", map[string]any{
		"vehicle_number": "REMOVE-1", "driver_name": "D", "vehicle_type": "car", "phone": "+1234567890",
	})
	vehicleID := vehicle["id"].(string)
	call(t, ts, "POST", "/alerts/configure", map[string]any{
		"geofence_id": geo["id"], "vehicle_id": vehicleID, "event_type": "entry",
	})
	call(t, ts, "POST", "/vehicles/location", map[string]any{
		"vehicle_id": vehicleID, "latitude": 10.5, "longitude": 10.5, "timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	if status, out := call(t, ts, "DELETE", "/vehicles/"+vehicleID+"/trail", nil); status != 200 || out["cleared"] != true {
		t.Fatalf("clear trail: %d %v", status, out)
	}
	if status, out := call(t, ts, "GET", "/vehicles/trail/"+vehicleID, nil); status != 200 || len(out["points"].([]any)) != 0 {
		t.Fatalf("trail after clear: %d %v", status, out)
	}
	if status, out := call(t, ts, "GET", "/vehicles/location/"+vehicleID, nil); status != 200 || out["current_location"] != nil {
		t.Fatalf("current location after clear: %d %v", status, out)
	}
	if _, out := call(t, ts, "GET", "/violations/history?vehicle_id="+vehicleID, nil); out["total_count"].(float64) != 1 {
		t.Fatalf("clear should preserve violation history: %v", out)
	}
	if _, out := call(t, ts, "GET", "/alerts?vehicle_id="+vehicleID, nil); len(out["alerts"].([]any)) != 1 {
		t.Fatalf("clear should preserve vehicle alert rules: %v", out)
	}

	if status, out := call(t, ts, "DELETE", "/vehicles/"+vehicleID, nil); status != 200 || out["deleted"] != true {
		t.Fatalf("delete vehicle: %d %v", status, out)
	}
	if status, _ := call(t, ts, "GET", "/vehicles/location/"+vehicleID, nil); status != 404 {
		t.Errorf("deleted vehicle should return 404, got %d", status)
	}
	if _, out := call(t, ts, "GET", "/violations/history?vehicle_id="+vehicleID, nil); out["total_count"].(float64) != 0 {
		t.Errorf("delete should remove violation history: %v", out)
	}
	if _, out := call(t, ts, "GET", "/alerts?vehicle_id="+vehicleID, nil); len(out["alerts"].([]any)) != 0 {
		t.Errorf("delete should remove vehicle-specific alert rules: %v", out)
	}
	if status, _ := call(t, ts, "DELETE", "/vehicles/"+vehicleID, nil); status != 404 {
		t.Errorf("second delete should return 404, got %d", status)
	}
	if status, _ := call(t, ts, "DELETE", "/vehicles/veh_nope/trail", nil); status != 404 {
		t.Errorf("clear unknown vehicle should return 404, got %d", status)
	}
}

func mustHave(t *testing.T, where string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: missing field %q in %v", where, k, m)
		}
	}
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T (%v)", v, v)
	}
	return m
}

// Verifies every required endpoint returns exactly the fields (and id formats) the assessment spec lists.
func TestSpecResponseShapes(t *testing.T) {
	ts := newTestServer(t)

	status, g := call(t, ts, "POST", "/geofences", map[string]any{
		"name": "Downtown Delivery Zone", "description": "Main delivery area", "category": "delivery_zone",
		"coordinates": [][]float64{{37.7749, -122.4194}, {37.7849, -122.4194}, {37.7849, -122.4094}, {37.7749, -122.4094}, {37.7749, -122.4194}},
	})
	mustHave(t, "POST /geofences", g, "id", "name", "status", "time_ns")
	if status != 201 || !strings.HasPrefix(g["id"].(string), "geo_") || g["status"] != "active" {
		t.Fatalf("POST /geofences: %d %v", status, g)
	}
	geoID := g["id"].(string)

	_, list := call(t, ts, "GET", "/geofences?category=delivery_zone", nil)
	fences := list["geofences"].([]any)
	if len(fences) != 1 {
		t.Fatalf("category filter: %v", list)
	}
	mustHave(t, "GET /geofences item", asMap(t, fences[0]), "id", "name", "description", "coordinates", "category", "created_at")
	if _, other := call(t, ts, "GET", "/geofences?category=toll_zone", nil); len(other["geofences"].([]any)) != 0 {
		t.Error("category filter must exclude other categories")
	}
	if c := asMap(t, fences[0])["coordinates"].([]any); len(c) != 5 || c[0].([]any)[0].(float64) != 37.7749 {
		t.Errorf("coordinates must round-trip as [lat, lng]: %v", c)
	}

	_, v := call(t, ts, "POST", "/vehicles", map[string]any{"vehicle_number": "KA-01-AB-1234", "driver_name": "John Doe", "vehicle_type": "truck", "phone": "+1234567890"})
	mustHave(t, "POST /vehicles", v, "id", "vehicle_number", "status", "time_ns")
	if !strings.HasPrefix(v["id"].(string), "veh_") || v["status"] != "active" {
		t.Fatalf("POST /vehicles: %v", v)
	}
	vehID := v["id"].(string)

	_, vl := call(t, ts, "GET", "/vehicles", nil)
	mustHave(t, "GET /vehicles item", asMap(t, vl["vehicles"].([]any)[0]), "id", "vehicle_number", "driver_name", "vehicle_type", "phone", "status", "created_at")

	_, rule := call(t, ts, "POST", "/alerts/configure", map[string]any{"geofence_id": geoID, "vehicle_id": vehID, "event_type": "entry"})
	mustHave(t, "POST /alerts/configure", rule, "alert_id", "geofence_id", "vehicle_id", "event_type", "status", "time_ns")
	if !strings.HasPrefix(rule["alert_id"].(string), "alert_") || rule["status"] != "active" {
		t.Fatalf("POST /alerts/configure: %v", rule)
	}

	_, al := call(t, ts, "GET", "/alerts?vehicle_id="+vehID, nil)
	mustHave(t, "GET /alerts item", asMap(t, al["alerts"].([]any)[0]), "alert_id", "geofence_id", "geofence_name", "vehicle_id", "vehicle_number", "event_type", "status", "created_at")

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/alerts", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	time.Sleep(100 * time.Millisecond)

	stamp := time.Now().UTC().Truncate(time.Second)
	_, loc := call(t, ts, "POST", "/vehicles/location", map[string]any{"vehicle_id": vehID, "latitude": 37.7799, "longitude": -122.4144, "timestamp": stamp.Format(time.RFC3339)})
	mustHave(t, "POST /vehicles/location", loc, "vehicle_id", "location_updated", "current_geofences", "time_ns")
	if loc["location_updated"] != true {
		t.Errorf("location_updated: %v", loc)
	}
	in := asMap(t, loc["current_geofences"].([]any)[0])
	mustHave(t, "POST /vehicles/location geofence", in, "geofence_id", "geofence_name", "status")
	if in["status"] != "inside" {
		t.Errorf("status should be inside: %v", in)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ws map[string]any
	if err := conn.ReadJSON(&ws); err != nil {
		t.Fatalf("no WebSocket alert: %v", err)
	}
	mustHave(t, "ws message", ws, "event_id", "event_type", "timestamp", "vehicle", "geofence", "location")
	mustHave(t, "ws vehicle", asMap(t, ws["vehicle"]), "vehicle_id", "vehicle_number", "driver_name")
	mustHave(t, "ws geofence", asMap(t, ws["geofence"]), "geofence_id", "geofence_name", "category")
	mustHave(t, "ws location", asMap(t, ws["location"]), "latitude", "longitude")
	if !strings.HasPrefix(ws["event_id"].(string), "evt_") || ws["timestamp"] != stamp.Format(time.RFC3339) {
		t.Errorf("ws event_id/timestamp: %v", ws)
	}

	_, cur := call(t, ts, "GET", "/vehicles/location/"+vehID, nil)
	mustHave(t, "GET /vehicles/location/{id}", cur, "vehicle_id", "vehicle_number", "current_location", "current_geofences", "time_ns")
	mustHave(t, "current_location", asMap(t, cur["current_location"]), "latitude", "longitude", "timestamp")
	mustHave(t, "current geofence", asMap(t, cur["current_geofences"].([]any)[0]), "geofence_id", "geofence_name", "category")

	_, hist := call(t, ts, "GET", "/violations/history?vehicle_id="+vehID+"&geofence_id="+geoID+"&start_date=2000-01-01T00:00:00Z&end_date=2100-01-01T00:00:00Z&limit=10", nil)
	mustHave(t, "GET /violations/history", hist, "violations", "total_count", "time_ns")
	item := asMap(t, hist["violations"].([]any)[0])
	mustHave(t, "violation", item, "id", "vehicle_id", "vehicle_number", "geofence_id", "geofence_name", "event_type", "latitude", "longitude", "timestamp")
	if !strings.HasPrefix(item["id"].(string), "viol_") {
		t.Errorf("violation id format: %v", item["id"])
	}
	if _, h2 := call(t, ts, "GET", "/violations/history?start_date=2100-01-01T00:00:00Z", nil); h2["total_count"].(float64) != 0 {
		t.Error("start_date filter should exclude older events")
	}
	if status, _ := call(t, ts, "GET", "/violations/history?start_date=yesterday", nil); status != 400 {
		t.Errorf("bad date should be 400, got %d", status)
	}
	if _, big := call(t, ts, "GET", "/violations/history?limit=100000", nil); big["limit"].(float64) != 500 {
		t.Errorf("limit must be capped at 500, got %v", big["limit"])
	}
	if _, def := call(t, ts, "GET", "/violations/history", nil); def["limit"].(float64) != 50 {
		t.Errorf("default limit must be 50, got %v", def["limit"])
	}

	// Geofence validation rules from the spec.
	valid := [][]float64{{0, 0}, {0, 1}, {1, 1}, {0, 0}}
	bad := map[string]map[string]any{
		"not closed":   {"name": "n", "category": "toll_zone", "coordinates": [][]float64{{0, 0}, {0, 1}, {1, 1}, {1, 0}}},
		"3 points":     {"name": "n", "category": "toll_zone", "coordinates": [][]float64{{0, 0}, {0, 1}, {0, 0}}},
		"lat 91":       {"name": "n", "category": "toll_zone", "coordinates": [][]float64{{91, 0}, {0, 1}, {1, 1}, {91, 0}}},
		"lng -181":     {"name": "n", "category": "toll_zone", "coordinates": [][]float64{{0, -181}, {0, 1}, {1, 1}, {0, -181}}},
		"bad category": {"name": "n", "category": "park", "coordinates": valid},
		"no name":      {"category": "toll_zone", "coordinates": valid},
		"no coords":    {"name": "n", "category": "toll_zone"},
	}
	for name, body := range bad {
		if status, out := call(t, ts, "POST", "/geofences", body); status != 400 || out["error"] == nil {
			t.Errorf("%s: want 400 with error, got %d %v", name, status, out)
		}
	}
	for _, c := range []string{"delivery_zone", "restricted_zone", "toll_zone", "customer_area"} {
		if status, out := call(t, ts, "POST", "/geofences", map[string]any{"name": "cat " + c, "category": c, "coordinates": valid}); status != 201 {
			t.Errorf("category %s should be accepted: %d %v", c, status, out)
		}
	}
	// Alert config rules: event_type validated, vehicle optional, unknown geofence rejected.
	if status, _ := call(t, ts, "POST", "/alerts/configure", map[string]any{"geofence_id": geoID, "event_type": "sometimes"}); status != 400 {
		t.Errorf("bad event_type should be 400, got %d", status)
	}
	if status, _ := call(t, ts, "POST", "/alerts/configure", map[string]any{"geofence_id": "geo_nope", "event_type": "exit"}); status != 404 {
		t.Errorf("unknown geofence should be 404, got %d", status)
	}
	if status, _ := call(t, ts, "POST", "/alerts/configure", map[string]any{"geofence_id": geoID, "vehicle_id": "veh_nope", "event_type": "exit"}); status != 404 {
		t.Errorf("unknown vehicle should be 404, got %d", status)
	}
	if status, _ := call(t, ts, "POST", "/vehicles", map[string]any{"vehicle_number": "X"}); status != 400 {
		t.Errorf("missing vehicle fields should be 400, got %d", status)
	}
	// time_ns must be a string of digits.
	if s := g["time_ns"].(string); strings.Trim(s, "0123456789") != "" || s == "" {
		t.Errorf("time_ns must be digits-only string, got %q", s)
	}
}
