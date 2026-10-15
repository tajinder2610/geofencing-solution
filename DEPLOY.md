# Deployment guide (Neon + Render + Vercel)

This is one concrete path that satisfies the brief (containerised backend, frontend on Vercel/Netlify/Cloudflare, backend image on Docker Hub). Any Docker host and any PostGIS-enabled Postgres works the same way. Dashboard labels change over time; if a button name differs, look for the equivalent.

Order matters because each piece needs the previous one's URL: **database → backend image → backend service → frontend → back to the backend to lock down CORS.**

## 0. Before you start
- Accounts: GitHub, Docker Hub, Neon, Render, Vercel (all have free tiers; check current limits yourself).
- Local sanity check first: `docker compose up --build`, then `./scripts/demo.sh`. Do not debug deployment problems that also exist locally.

## 1. Database – Neon (PostGIS)
1. Create a project and database. Neon supports PostGIS; the backend runs `CREATE EXTENSION IF NOT EXISTS postgis` itself on boot.
2. Copy the connection string. **Use the *direct* (non-pooled) connection**, i.e. the host *without* `-pooler`.
   Why: the backend uses `LISTEN/NOTIFY` (alert fan-out) and a session advisory lock (migrations). Both break on transaction-pooled connections (PgBouncer).
3. The string looks like `postgres://USER:PASSWORD@ep-xxxx.region.aws.neon.tech/neondb?sslmode=require`. Keep it for step 3.
4. Note: the long-lived `LISTEN` connection keeps the Neon compute from auto-suspending while the backend runs. That is expected; it also means no cold-start delay on alerts.

## 2. Backend image → Docker Hub
Build for **linux/amd64** (important if you are on an Apple Silicon Mac, otherwise most hosts fail with "exec format error"):
```bash
cd backend
docker login
docker buildx build --platform linux/amd64 -t <dockerhub-user>/geofence-backend:latest --push .
```
Check it at `https://hub.docker.com/r/<dockerhub-user>/geofence-backend`. Add the link to `SETUP.md`. If the repo is private on Docker Hub, the host needs registry credentials; making the *image* public is simplest (the *source repo* must stay private per the brief).

## 3. Backend – Render (Docker image)
1. New → **Web Service** → *Existing image from a registry* → `docker.io/<dockerhub-user>/geofence-backend:latest`.
2. Environment variables:

| Variable | Value |
|---|---|
| `DATABASE_URL` | the Neon **direct** string from step 1 |
| `ALLOWED_ORIGINS` | `*` for now (tightened in step 5) |
| `TRUST_PROXY` | `true` (Render sits behind a proxy; makes the rate limiter see real client IPs) |
| `API_KEY` | optional. If set, the frontend needs the same value as `VITE_API_KEY` and it becomes visible in the browser bundle |

   `PORT` is injected by the platform and the server already reads it.
3. Health check path: `/health`.
4. Wait for the deploy, then verify:
```bash
curl https://<backend-host>/health              # {"status":"ok",...}
./scripts/demo.sh https://<backend-host>        # add API_KEY=... if you enabled it
npx wscat -c wss://<backend-host>/ws/alerts     # in a second terminal while the demo runs
```
Free web services on most hosts sleep when idle; the first request after a pause is slow and open WebSockets are dropped (the UI reconnects automatically). **Hit `/health` a minute before recording or submitting.**

## 4. Frontend – Vercel
1. Import the GitHub repo. **Root Directory:** `frontend`. Framework preset: Vite (build `npm run build`, output `dist`; `vercel.json` already contains the SPA rewrite).
2. Environment variables (these are baked in at build time, so redeploy after changing them):
   - `VITE_API_URL` = `https://<backend-host>` (no trailing slash)
   - `VITE_API_KEY` only if the backend has `API_KEY`
3. Deploy, open the URL. The header pill should say **Live**. Over HTTPS the socket automatically uses `wss://`.
   (Netlify: `netlify.toml` is included. Cloudflare Pages: build `npm run build`, output `dist`, same env vars.)

## 5. Lock down CORS
Set the backend's `ALLOWED_ORIGINS` to your exact frontend origin, e.g. `https://geofence-xyz.vercel.app` (comma-separate several; preview URLs need adding too), and redeploy/restart. This also restricts which sites can open the alert WebSocket.

## 6. Final verification checklist
- [ ] `GET /health` is ok on the public URL.
- [ ] UI loads without a red "Could not load data" banner; header shows **Live**.
- [ ] Draw a geofence → register vehicle → add alert rule → click map to move the vehicle in and out → toast + feed entry + history row.
- [ ] Refresh the page: data persists. Restart the backend service: data persists and the UI reconnects.
- [ ] `docker run` of your Docker Hub image works on a clean machine (with a `DATABASE_URL`).
- [ ] Links added to `SETUP.md`; repo private; the six reviewers added as collaborators.

## Troubleshooting
| Symptom | Likely cause / fix |
|---|---|
| Browser console: CORS error | `ALLOWED_ORIGINS` doesn't include the exact frontend origin (scheme + host, no trailing slash) |
| UI banner "Cannot reach the server" | wrong `VITE_API_URL`, or you forgot to redeploy the frontend after changing it |
| Header stuck on "Reconnecting…" | backend asleep, `http://` backend used from an `https://` page (mixed content), or `API_KEY` mismatch |
| Backend crash: `permission denied to create extension "postgis"` | the DB role can't create extensions; enable PostGIS once as an admin role or use a PostGIS-capable provider |
| Backend log: `pg_advisory_lock` / `LISTEN` errors, alerts never arrive | you used a pooled connection string; switch to the direct one |
| `exec format error` on the host | image built for arm64; rebuild with `--platform linux/amd64` |
| 429 responses during load | per-IP limit on location updates; raise `RATE_LIMIT_RPS/BURST` or confirm `TRUST_PROXY=true` |
| Frontend 404 on refresh of a sub-route | missing SPA rewrite (`vercel.json` / `netlify.toml` handle it) |
