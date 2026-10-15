# Geofencing & Real-time Alert System

Go REST API + WebSocket alerts, PostgreSQL/PostGIS storage, React (Vite + TypeScript + Leaflet) dashboard.

- **GitHub repository:** https://github.com/tajinder2610/geofencing-solution
- **Docker Hub image:** https://hub.docker.com/r/tajinder26/geofence-backend
- **Demo video:** _add your link here (Loom / YouTube unlisted / Drive shared with the reviewers)_
- **Live frontend:** _add URL_ · **Live backend:** _add URL_

```
.
├── backend/              Go service (cmd/server, internal/{api,store,hub,geo,ratelimit,config}); deps vendored
├── frontend/             React app (Vite, TypeScript, react-leaflet)
├── DEPLOY.md             Step-by-step Neon + Render + Vercel deployment
├── WALKTHROUGH.md        Plan and talking points for the video
├── scripts/demo.sh       Walks a vehicle through a restricted zone (entry + exit)
└── docker-compose.yml    PostGIS + backend + frontend
```

## 1. Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Docker + Docker Compose v2 | recent | easiest way to run everything |
| Go | 1.22+ | running/testing the backend without Docker |
| Node.js | 20+ (22 recommended) | running the frontend without Docker |
| PostgreSQL + PostGIS | 14+ / PostGIS 3 | only when not using Docker |

## 2. Run with Docker Compose (recommended)

```bash
docker compose up --build
```

| Service | URL |
|---|---|
| Frontend | http://localhost:3000 |
| Backend API | http://localhost:8080 (health: `/health`) |
| PostGIS | `localhost:5432` (user/pass/db: `geo` / `geo` / `geofence`) |

The backend waits for the database and applies its schema automatically on boot (idempotent migrations embedded in the binary). Backend dependencies are vendored, so the Go image builds without network access to the Go module proxy.

Stop with `docker compose down` (add `-v` to wipe the database).

## 3. Run locally without Docker

**Database** – any PostGIS-enabled Postgres, e.g.:
```bash
docker run -d --name geo-db -p 5432:5432 -e POSTGRES_USER=geo -e POSTGRES_PASSWORD=geo -e POSTGRES_DB=geofence postgis/postgis:16-3.4-alpine
```

**Backend**
```bash
cd backend
export DATABASE_URL='postgres://geo:geo@localhost:5432/geofence?sslmode=disable'
go run ./cmd/server            # listens on :8080
```

**Frontend**
```bash
cd frontend
cp .env.example .env           # VITE_API_URL=http://localhost:8080
npm install                    # skip if node_modules is already present
npm run dev                    # http://localhost:5173
```

### Backend configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DATABASE_URL` | `postgres://geo:geo@localhost:5432/geofence?sslmode=disable` | Postgres connection string |
| `ALLOWED_ORIGINS` | `*` | Comma-separated CORS / WebSocket origins. Set to your frontend URL in production |
| `API_KEY` | _(empty = auth off)_ | When set, every request needs `X-API-Key` (WebSocket: `?api_key=`). `/health` stays public |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `50` / `100` | Per-client-IP token bucket on `POST /vehicles/location` (429 + `Retry-After`) |
| `TRUST_PROXY` | `false` | Use `X-Forwarded-For` as the client IP (enable behind a trusted reverse proxy) |

Frontend (build-time, `frontend/.env`): `VITE_API_URL`, optional `VITE_API_KEY`, optional `VITE_WS_URL`.

## 4. API testing guide

Every response includes `"time_ns"` (handler execution time as a string of nanoseconds). Errors look like
`{"error": "validation failed", "details": ["…"], "time_ns": "…"}` with status 400 / 404 / 409 / 429.
Add `-H "X-API-Key: $API_KEY"` to each call if you enabled `API_KEY`.

Fastest path – run the scripted scenario while the UI is open:
```bash
./scripts/demo.sh            # or ./scripts/demo.sh https://your-backend.example.com
```

Manual walkthrough:

```bash
API=http://localhost:8080

# 1. Create a geofence (coordinates are [latitude, longitude]; ring must be closed, ≥4 points)
curl -s -X POST $API/geofences -H 'Content-Type: application/json' -d '{
  "name": "Downtown Delivery Zone",
  "description": "Main delivery area for downtown customers",
  "coordinates": [[37.7749,-122.4194],[37.7849,-122.4194],[37.7849,-122.4094],[37.7749,-122.4094],[37.7749,-122.4194]],
  "category": "restricted_zone"
}'
# → {"id":"geo_…","name":"Downtown Delivery Zone","status":"active","time_ns":"…"}

# 2. List geofences (optional ?category=delivery_zone|restricted_zone|toll_zone|customer_area, ?limit, ?offset)
curl -s "$API/geofences?category=restricted_zone"

# 3. Register a vehicle
curl -s -X POST $API/vehicles -H 'Content-Type: application/json' -d '{
  "vehicle_number":"KA-01-AB-1234","driver_name":"John Doe","vehicle_type":"truck","phone":"+1234567890"}'

# 4. List vehicles (?include_location=true adds current_location + current_geofences)
curl -s "$API/vehicles"

# 5. Configure an alert rule (vehicle_id optional = all vehicles; event_type: entry | exit | both)
curl -s -X POST $API/alerts/configure -H 'Content-Type: application/json' -d '{
  "geofence_id":"geo_…","vehicle_id":"veh_…","event_type":"entry"}'

# 6. Send a location update – this is what triggers entry/exit detection and live alerts
curl -s -X POST $API/vehicles/location -H 'Content-Type: application/json' -d '{
  "vehicle_id":"veh_…","latitude":37.7799,"longitude":-122.4144,"timestamp":"2025-01-15T10:35:00Z"}'

# 7. Current location + geofences of one vehicle
curl -s $API/vehicles/location/veh_…

# 8. Alert rules (filters: ?geofence_id=…&vehicle_id=…)
curl -s "$API/alerts"

# 9. Entry/exit history (filters: vehicle_id, geofence_id, start_date, end_date, limit ≤ 500 (default 50), offset)
curl -s "$API/violations/history?vehicle_id=veh_…&start_date=2025-01-01T00:00:00Z&limit=100"
```

Extra endpoints (not required by the spec, used by the UI):

| Endpoint | Purpose |
|---|---|
| `PUT /geofences/{geofence_id}` | Replace a geofence (same body as create). Vehicle membership is recomputed silently, so editing never fires alerts |
| `DELETE /geofences/{geofence_id}` | Delete a geofence; its alert rules, state and history cascade |
| `GET /vehicles/trail/{vehicle_id}?limit=100` | Recent positions (oldest → newest) for drawing a path |
| `PATCH /alerts/{alert_id}` | `{"status":"active"\|"inactive"}` pause/resume a rule |
| `DELETE /alerts/{alert_id}` | Delete a rule |

**Watching the WebSocket from a terminal:** `npx wscat -c ws://localhost:8080/ws/alerts`, then send a location update that crosses a boundary.

Alert message (`/ws/alerts`):
```json
{"event_id":"evt_…","event_type":"entry","timestamp":"2025-01-15T10:35:00Z",
 "vehicle":{"vehicle_id":"veh_…","vehicle_number":"KA-01-AB-1234","driver_name":"John Doe"},
 "geofence":{"geofence_id":"geo_…","geofence_name":"Downtown Delivery Zone","category":"restricted_zone"},
 "location":{"latitude":37.7799,"longitude":-122.4144}}
```

### Automated tests
```bash
cd backend
go test ./...                                  # unit tests (geo validation, rate limiter, WebSocket hub)
# Integration test against a *disposable* PostGIS database (it TRUNCATES all tables):
TEST_DATABASE_URL='postgres://geo:geo@localhost:5432/geofence_test?sslmode=disable' go test -race ./...
cd ../frontend && npm test                     # 12 tests: coordinate parsing + app smoke tests (jsdom, mocked API/WebSocket)
```

## 5. Frontend usage guide

Edit and delete buttons on each geofence reuse the create form. Selecting a vehicle on the map draws its recent trail.

The header shows a **Live / Reconnecting** indicator for the WebSocket and a badge on *Live alerts* when new alerts arrive while you are on another tab.

1. **Geofences** – press **Draw on map** and click to place vertices (Undo / Clear to adjust), or type/paste `lat, lng` lines – both stay in sync. Choose a category, **Create geofence**. The list can be filtered by category (server-side query); click an item to zoom to it.
2. **Vehicles** – register a vehicle, select it, then **click the map** (or type coordinates) and choose a timestamp, then **Send location update**. The result card shows which geofences it is now inside; markers turn red inside restricted zones.
3. **Alert rules** – pick a geofence, optionally a vehicle (default: all), and entry / exit / both. Rules can be paused, resumed or deleted.
4. **Live alerts** – incoming events appear as toasts, pulse the vehicle marker, and accumulate in the feed with vehicle, geofence + category, event type, timestamp and coordinates.
5. **History** – filter by vehicle, geofence and date range; paginate with a page-size selector. It refreshes itself when a live alert arrives.

## 6. Architecture overview

```
 React (Vite) ──REST──▶ Go net/http ──▶ PostgreSQL + PostGIS
      ▲                    │  POST /vehicles/location
      └──── WebSocket ◀────┘  (async goroutine → hub.Broadcast)
```

- **Point-in-polygon:** polygons are stored as `geometry(Polygon, 4326)` with a **GiST index**; membership is `ST_Covers(geom, ST_MakePoint(lng, lat))` (boundary counts as inside). The API accepts `[lat, lng]` pairs (per spec) and swaps to PostGIS's `(lng lat)` order when building WKT. PostGIS also rejects invalid shapes (e.g. self-intersecting “bow-ties”) with a 400.
- **Entry/exit detection:** `vehicle_geofence_state` holds which geofences each vehicle is currently in. Each location update is diffed against it inside one transaction that first locks the vehicle row (`SELECT … FOR UPDATE`), so concurrent updates for a vehicle can't emit duplicate events. Every transition is written to `geofence_events` (the history table). Updates whose timestamp is older than the latest known location are stored but do not change state (out-of-order delivery).
- **Alerts:** after the transaction commits, a goroutine matches events against active `alert_rules` (vehicle-specific or all, entry/exit/both), builds the message, records it in `alert_log` and publishes it with Postgres `NOTIFY`. Every backend instance `LISTEN`s and relays notifications to its own WebSocket clients, so alerts reach clients on **any** instance. If publishing fails the local clients are still served. The HTTP response never waits for any of this.
- **WebSocket hub:** gorilla/websocket; one read + one write goroutine per client, buffered send queue (slow clients are dropped instead of blocking others), ping/pong keep-alive, graceful close on shutdown. The frontend reconnects with exponential backoff (1 s → 15 s).
- **Responses:** handlers return `(status, body, error)`; one wrapper times the call, maps errors to statuses, and injects `time_ns`.
- **Schema:** `geofences`, `vehicles`, `vehicle_locations` (append-only, indexed `(vehicle_id, recorded_at DESC)`), `vehicle_geofence_state`, `alert_rules` (unique per geofence/vehicle/event), `geofence_events`, `alert_log`.
- **Bonus items included:** rate limiting on location updates, optional API-key auth, pagination on all list endpoints (`limit`/`offset` + `total_count`), unit + integration tests.

## 7. Deployment

**Backend image → Docker Hub**
```bash
cd backend
docker build -t <dockerhub-user>/geofence-backend:latest .
docker login && docker push <dockerhub-user>/geofence-backend:latest
```
Build with `--platform linux/amd64` if you are on Apple Silicon. See **[DEPLOY.md](DEPLOY.md)** for a full Neon + Render + Vercel walkthrough. Run it on any Docker host (Render, Railway, Fly.io, a VM…) with `DATABASE_URL` pointing at a managed Postgres (use the **direct, non-pooled** connection string: `LISTEN/NOTIFY` does not work through PgBouncer) **with the PostGIS extension available** (e.g. Neon, Supabase, Railway/Render Postgres, or a `postgis/postgis` container), `ALLOWED_ORIGINS=https://<your-frontend-domain>`, and ideally `API_KEY`. The migration runs `CREATE EXTENSION IF NOT EXISTS postgis`, which needs a role allowed to create extensions on first boot.

**Frontend → Vercel / Netlify / Cloudflare Pages**
- Root directory `frontend`, build command `npm run build`, output `dist`.
- Set env var `VITE_API_URL=https://<your-backend>` (and `VITE_API_KEY` if you enabled auth; note it is embedded in the client bundle). Over HTTPS the WebSocket automatically uses `wss://`.
- `vercel.json` / `netlify.toml` already contain the SPA rewrite.

## 8. Known limitations / roadmap (useful for the video's third section)

- Auth is a single shared API key; production would use per-user JWTs and keep keys out of the browser bundle.
- Alert fan-out uses Postgres `NOTIFY`: best-effort (~8 kB payload cap, notifications are lost while a listener reconnects). Needs a *direct* (non-pooled) DB connection. Next step would be Redis Streams/NATS with replay.
- Alerts are broadcast to all connected clients (no per-user subscriptions or acknowledgement); delivery is best-effort — missed alerts are still in `/violations/history`.
- No map clustering for thousands of vehicles; the trail shows only the last 100 points.
- Polygons crossing the antimeridian aren't supported. A vehicle already inside a geofence created later only gets an entry event on its next location update.
- Scale-out ideas: partition `vehicle_locations` by time, batch/queue location ingestion (Kafka/NATS), cache hot geofences, `geography` type for metric queries.
