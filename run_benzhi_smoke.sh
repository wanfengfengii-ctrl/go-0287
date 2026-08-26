#!/usr/bin/env bash
#
# run_benzhi_smoke.sh — deterministic offline smoke test for the
# steel-fireproofing evidence-closure backend. It builds the server, starts it
# on a local ephemeral port against a temporary SQLite database, probes the
# liveness/readiness endpoints and a real lock API, then tears everything
# down. It never touches the network and never shells out to `go test`.
#
set -euo pipefail

WORKDIR="$(mktemp -d)"
BIN="${WORKDIR}/server"
DB="${WORKDIR}/smoke.db"
PORT="${PORT:-18080}"
ADDR="127.0.0.1:${PORT}"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT

echo "building server"
go build -o "${BIN}" ./cmd/server

echo "starting server on ${ADDR}"
DB_PATH="${DB}" ADDR="${ADDR}" "${BIN}" &
SERVER_PID=$!

# Wait deterministically for readiness (bounded, no external network).
ready=""
for _ in $(seq 1 100); do
  ready="$(curl -s "http://${ADDR}/health/ready" 2>/dev/null || true)"
  if [[ "${ready}" == *'"status":"ready"'* ]]; then
    break
  fi
  sleep 0.05
done

if [[ "${ready}" != *'"status":"ready"'* ]]; then
  echo "server did not become ready; last response: ${ready}" >&2
  exit 1
fi

# Liveness probe — capture the response, do not pipe into grep.
live="$(curl -s "http://${ADDR}/health/live")"
if [[ "${live}" != *'"status":"alive"'* ]]; then
  echo "liveness probe failed: ${live}" >&2
  exit 1
fi

# Real public API: lock a task and assert the computed integer results.
lock_body='{
  "task_id": "smoke-1",
  "floor": "A",
  "fire_compartment": "F1",
  "catalog_version": 1,
  "rule_summary": "public integer section-factor and thickness rules v1",
  "material_proof_summary": "mp-v1",
  "equipment_calibration_summary": "cal-v1",
  "primer_compatibility": {"primer-a": {"coating-x": true}},
  "members": [{
    "id": "beam-1", "floor": "A", "fire_compartment": "F1", "type": "beam",
    "section": {"height_mm": 400, "width_mm": 200}, "fire_rating_minutes": 60,
    "material_batch_ids": ["coating-x"], "primer_batch_id": "primer-a",
    "spray_zones": [{"id": "zone-1"}],
    "exposed_faces": [{"id": "face-1", "member_id": "beam-1", "spray_zone_id": "zone-1",
      "boundary": {"min_x": 0, "min_y": 0, "max_x": 1000, "max_y": 1000},
      "grid_points": [{"id": "p1", "face_id": "face-1", "point": {"x": 100, "y": 100}, "spray_zone_id": "zone-1"}]}]
  }],
  "coat_plan": {"coat_count": 3, "wet_film_per_coat_um": 200, "open_time_ticks": 10,
    "curing_window_ticks": 20, "min_dry_film_um": 37500000, "max_dispersion_um": 10000000,
    "min_bond_strength_kpa": 500000}
}'

lock="$(curl -s -X POST "http://${ADDR}/v1/tasks/smoke-1/lock" -H 'Content-Type: application/json' -d "${lock_body}")"
if [[ "${lock}" != *'"generation":1'* ]]; then
  echo "lock API failed: ${lock}" >&2
  exit 1
fi
if [[ "${lock}" != *'15000000'* ]]; then
  echo "expected section factor 15000000 in lock response: ${lock}" >&2
  exit 1
fi

echo "smoke test passed"
