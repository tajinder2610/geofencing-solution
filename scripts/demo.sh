#!/usr/bin/env bash
# Walks a vehicle into and out of a restricted geofence so you can watch the live alert in the UI.
# Usage: ./scripts/demo.sh [API_URL]     (set API_KEY if the backend requires one)
set -euo pipefail

API="${1:-http://localhost:8080}"
AUTH=()
[[ -n "${API_KEY:-}" ]] && AUTH=(-H "X-API-Key: ${API_KEY}")
SUFFIX="$(date +%s)"

post() { curl -sS -X POST "$API$1" -H 'Content-Type: application/json' "${AUTH[@]}" -d "$2"; }
field() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

echo "1) Creating restricted geofence…"
GEO=$(post /geofences '{
  "name": "Demo Restricted Zone '"$SUFFIX"'",
  "description": "Created by scripts/demo.sh",
  "category": "restricted_zone",
  "coordinates": [[37.7749,-122.4194],[37.7849,-122.4194],[37.7849,-122.4094],[37.7749,-122.4094],[37.7749,-122.4194]]
}')
echo "$GEO"; GEO_ID=$(echo "$GEO" | field id)

echo "2) Registering vehicle…"
VEH=$(post /vehicles '{"vehicle_number":"DEMO-'"$SUFFIX"'","driver_name":"Demo Driver","vehicle_type":"truck","phone":"+1234567890"}')
echo "$VEH"; VEH_ID=$(echo "$VEH" | field id)

echo "3) Alerting on entry and exit for all vehicles…"
post /alerts/configure '{"geofence_id":"'"$GEO_ID"'","event_type":"both"}'; echo

move() {
  echo "-> $1"
  post /vehicles/location '{"vehicle_id":"'"$VEH_ID"'","latitude":'"$2"',"longitude":'"$3"',"timestamp":"'"$(now)"'"}'; echo
  sleep "${4:-2}"
}
move "outside the zone"            37.7600 -122.4300
move "ENTERING the restricted zone" 37.7799 -122.4144
move "still inside (no new alert)"  37.7810 -122.4130
move "EXITING the restricted zone"  37.7600 -122.4300 0

echo "Done. Check the Live alerts tab and: curl '$API/violations/history?vehicle_id=$VEH_ID'"
