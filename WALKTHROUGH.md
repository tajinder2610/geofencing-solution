# Video walkthrough plan (30–45 min)

Target ≈ 38 minutes: **Demo 11 · Code 17 · Gaps 10**. Speak as the author: say *why* you chose things, not just what they do. Keep this open on a second screen.

## Before recording (10-minute checklist)
- Fresh database (`docker compose down -v && docker compose up --build`) so the demo starts empty, plus one terminal ready with `docker compose ps`.
- Browser tabs: app at `localhost:3000` (and the deployed one), `SETUP.md`, a terminal with `wscat -c ws://localhost:8080/ws/alerts`.
- Editor open with these files pinned, in this order: `store/store.go`, `api/vehicles.go`, `api/alerts.go`, `hub/hub.go`, `api/respond.go`, `store/migrations/001_init.sql`, `frontend/src/App.tsx`, `hooks/useAlertSocket.ts`, `components/MapView.tsx`.
- Hit the deployed backend's `/health` to wake it. Close notifications. Zoom the editor font.
- Do one full dry run. Know where the "wow" moment is: the toast + pulsing marker.

---
## Part 1 — App demo (≈ 11 min)
1. **(1 min) Frame it.** One sentence: geofences, vehicles, entry/exit detection, instant alerts. Show the header's **Live** pill and say it is the WebSocket.
2. **(2 min) Geofences tab.** Click **Draw on map**, click 4–5 vertices around a block. Point out the textarea updating in sync, type one coordinate by hand to prove both input paths. Category **Restricted zone** → create. Show the list, the category filter (mention it is a server-side `?category=` query), click to zoom.
3. **(1 min) Vehicles tab.** Register `KA-01-AB-1234`, driver, truck, phone. Show validation: submit a bad phone to display the friendly error, then a duplicate number → 409 message.
4. **(1 min) Alert rules tab.** Geofence = the restricted zone, vehicle = all, **Entry & exit**. Mention pause/delete.
5. **(3 min) The money shot.** Split screen: Vehicles tab + (briefly) `wscat`. Click the map **outside** → send. Click **inside** → send: toast, pulsing red marker, result card "Inside: …", `wscat` prints the JSON. Click inside again: **no second alert** (state diff). Click outside → exit alert. Open **Live alerts** tab: feed with vehicle, geofence + category, event type, time, coordinates.
6. **(1 min) Trail.** Select the vehicle; show the purple path built from `/vehicles/trail/{id}`.
7. **(1 min) History tab.** Filter by vehicle, geofence, date range, change page size, paginate. Mention `total_count`.
8. **(1 min) Docker.** Show `docker compose ps` (db healthy → backend → frontend), `curl localhost:8080/health`, and one `curl` of `POST /vehicles/location` highlighting `time_ns` in the response. Optionally run `./scripts/demo.sh` and watch the UI react.

---
## Part 2 — Implementation deep dive (≈ 17 min)
### 2.1 Layout & architecture (2 min)
`backend/cmd/server` (wiring) → `internal/api` (HTTP) → `internal/store` (SQL) with `hub` (WebSocket), `geo` (pure validation), `ratelimit`, `config`. Draw the diagram from `SETUP.md` §6. Say: handlers never touch SQL; the store never touches HTTP.

### 2.2 Response contract (1 min) — `api/respond.go`
Handlers return `(status, body, error)`; `wrap()` times the call, maps errors to statuses (`errors.As` on `apiError`, `store.ErrNotFound`, …) and `writeJSON` injects `time_ns` last, so it covers serialisation. One place → can't forget it on any endpoint, including errors.

### 2.3 Schema & indexing (2 min) — `001_init.sql`
- `geometry(Polygon,4326)` + **GiST index** on `geofences.geom`; original `[lat,lng]` kept in `coordinates jsonb` so the API returns exactly what was sent.
- `vehicle_locations` is append-only, indexed `(vehicle_id, recorded_at DESC, id DESC)` → "latest position" is an index seek.
- `vehicle_geofence_state` (current membership), `geofence_events` (history), `alert_rules` (unique index with `COALESCE(vehicle_id,'')` so "all vehicles" can't duplicate), `alert_log`.
- Migrations are embedded SQL, idempotent, guarded by a Postgres advisory lock so several instances can boot together.

### 2.4 Point-in-polygon (2 min) — `store.geofencesContaining`, `geo.PolygonWKT`
`ST_Covers(geom, ST_SetSRID(ST_MakePoint(lng, lat), 4326))`. Talk through: (a) axis order trap: API is `[lat,lng]`, PostGIS wants `(lng lat)`, `PolygonWKT` swaps; (b) `ST_Covers` vs `ST_Contains`: boundary counts as inside, tested in the integration test; (c) the GiST index makes it a bounding-box lookup first; (d) `ST_IsValid` rejects bow-ties with a readable 400 (`checkGeometry`).

### 2.5 Entry/exit detection — the core (4 min) — `store.RecordLocation`
Walk it top to bottom:
1. One transaction; `SELECT … FOR UPDATE` on the vehicle row → concurrent updates for the same vehicle are serialised, so two requests can't both see "was outside" and emit duplicate entries.
2. Insert the location (always kept).
3. **Stale check:** if `ts` < latest known timestamp, store it but skip transitions (out-of-order delivery shouldn't flip state backwards).
4. Compute `inside` (spatial query) and `prev` (state table); `entered = inside − prev`, `exited = prev − inside`. Exits processed first (moving between adjacent zones reads naturally).
5. Update state, insert `geofence_events`, commit. Events are returned to the handler.
Mention `UpdateGeofence`: moving a boundary recomputes membership silently (no phantom alerts) — and it is tested.

### 2.6 Alert delivery (3 min) — `api/vehicles.go` → `api/alerts.go` → `hub/hub.go`
- Handler: `go s.dispatchAlerts(...)` **after** commit; HTTP response doesn't wait.
- `dispatchAlerts`: match active rules (vehicle-specific or NULL, `event_type IN (x,'both')`), build the spec's message, `LogAlert`, then `pg_notify`.
- `RunAlertListener` (one per process) `LISTEN`s and calls `hub.Broadcast` → so a client on instance B gets alerts triggered on instance A (`TestAlertFanOutAcrossInstances`). Fallback: if NOTIFY fails, broadcast locally.
- Hub: per-client read pump + write pump, buffered channel (64); a slow client is dropped instead of blocking everyone; ping 25 s / pong deadline 60 s; graceful close on shutdown; `remove()` is idempotent.

### 2.7 Robustness extras (1 min)
Rate limiter (token bucket per IP, `ratelimit.go`, unit-tested with an injectable clock), optional API key (constant-time compare; `?api_key=` for WebSocket because browsers can't set headers), CORS/origin check shared by HTTP and WS, panic-recovery + request logging middleware (with `Hijack` so WebSockets still work), pagination everywhere.

### 2.8 Frontend (2 min)
- `App.tsx` owns the shared map state; panels are small and prop-driven. `hooks/useAppData` caches geofences/vehicles/rules and polls vehicles every 5 s (positions can change from other clients).
- `hooks/useAlertSocket.ts`: reconnect with exponential backoff 1 s → 15 s; handler kept in a ref so re-renders don't reconnect; ignores malformed frames.
- `MapView.tsx`: react-leaflet; click handler switches by mode (`draw`/`pick`/`view`); vehicle markers are `divIcon`s; the marker key includes a nonce to restart the pulse animation.
- Typed API client (`api/client.ts`) turns server `{error, details}` into `ApiError` shown by `ErrorBanner`.

### 2.9 Tests (1 min)
`go test` unit tests + an integration test (needs PostGIS) covering the full lifecycle, boundary points, no duplicate alerts, stale updates, edit/delete cascade, trail, and cross-instance delivery; run with `-race`.

---
## Part 3 — Gaps, improvements, roadmap (≈ 10 min)
Be candid, give estimates (one engineer):

| Gap | Plan | Estimate |
|---|---|---|
| Single shared API key; key visible in the browser bundle | Per-user JWT (login, refresh), per-user WebSocket auth, roles | 1–2 days |
| Alerts go to every connected client; no acknowledge | Per-user/per-geofence subscriptions, alert ack + "unread" persisted, catch-up endpoint using `alert_log` since last seen | 1–1.5 days |
| NOTIFY is best-effort and capped at ~8 kB; lost while a listener is reconnecting | Move fan-out to Redis Streams/NATS with replay, or outbox table + poller | 1–2 days |
| Location ingestion is synchronous per request | Queue (NATS/Kafka) + batch writers, idempotency key per `(vehicle, timestamp)` | 2–3 days |
| `vehicle_locations` grows forever | Time partitioning + retention job; downsample old trails | 1 day |
| History uses offset pagination | Keyset pagination on `(event_time, id)` | 0.5 day |
| `geometry` + planar `ST_Covers` | `geography` or projected SRID for metric buffers / dwell-time rules; antimeridian handling | 1 day |
| Editing a geofence rescans latest positions of *all* vehicles | Restrict by bbox with the GiST index; or async job | 0.5 day |
| Observability | Prometheus metrics (latency, ws clients, notify lag), structured logs, tracing | 1 day |
| Frontend coverage | Component tests + Playwright e2e of the draw→alert flow; marker clustering for thousands of vehicles; vector tiles | 2 days |
| CI/CD | GitHub Actions: vet, tests with PostGIS service, image build/push, preview deploys | 0.5 day |
| Dwell-time / speed / schedule-based rules | Rule engine on top of the state table | 3–4 days |

**Scaling story to thousands of vehicles / geofences:** the hot path is one row lock, one indexed latest-position lookup, one GiST query and a few small inserts → comfortably thousands of updates/second per Postgres node with pooling; scale reads with replicas, writes with partitioning/queued batching, WebSocket tier horizontally (already possible thanks to NOTIFY fan-out), cache geofences for the point-in-polygon prefilter if the table gets huge.

**Edge cases handled:** boundary points, closing-point & range validation, self-intersecting polygons, duplicate vehicle numbers/rules, unknown ids, out-of-order timestamps, concurrent updates, slow WebSocket clients, reconnects.
**Not handled:** antimeridian polygons, polygons with holes, GPS jitter flapping at a boundary (needs hysteresis/min-dwell), clock skew (we trust device timestamps).

## Likely reviewer questions (have answers ready)
- *Why PostGIS instead of ray-casting in Go?* Correct on edge cases, index-backed, one query for N geofences, validation for free.
- *Why a state table instead of comparing with the previous location?* O(1) membership diff, supports overlapping zones, survives restarts, and makes boundary edits safe.
- *How do you avoid duplicate alerts under concurrency?* Row lock on the vehicle inside the transaction.
- *What if the alert goroutine fails?* Event is already in history; alert persisted before publish; client can reconcile via `/violations/history`.
- *Why NOTIFY rather than Redis?* No extra infrastructure for this scale; trade-offs listed above.
- *What would you change first for production?* Auth, alert replay, ingestion queue.
