#!/usr/bin/env bash
# Shared helpers sourced by run.sh and verify_postgres.sh. Not run directly.

BASE_URL="${BASE_URL:-http://localhost:8080}"
TEST_EMAIL="test@sentinel.com"
TEST_PASSWORD="password123"

# Matches docker-compose.yml — override via env if your setup differs.
DOCKER_PG_CONTAINER="${DOCKER_PG_CONTAINER:-sentinel-postgres}"
DOCKER_PG_USER="${DOCKER_PG_USER:-root}"
DOCKER_PG_DB="${DOCKER_PG_DB:-sentinel}"
DOCKER_REDIS_CONTAINER="${DOCKER_REDIS_CONTAINER:-sentinel-redis}"

load_env() {
  local envfile
  envfile="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.env"
  if [ -f "$envfile" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$envfile"
    set +a
  fi
}

login_or_register() {
  echo "Authenticating..." >&2
  JWT_TOKEN=$(curl -sf -X POST "$BASE_URL/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$TEST_EMAIL\",\"password\":\"$TEST_PASSWORD\"}" \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null || true)

  if [ -z "$JWT_TOKEN" ]; then
    echo "Login failed, attempting registration..." >&2
    curl -sf -X POST "$BASE_URL/auth/register" \
      -H "Content-Type: application/json" \
      -d "{\"email\":\"$TEST_EMAIL\",\"password\":\"$TEST_PASSWORD\"}" > /dev/null || true

    JWT_TOKEN=$(curl -sf -X POST "$BASE_URL/auth/login" \
      -H "Content-Type: application/json" \
      -d "{\"email\":\"$TEST_EMAIL\",\"password\":\"$TEST_PASSWORD\"}" \
      | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null || true)
  fi

  if [ -z "$JWT_TOKEN" ]; then
    echo "FATAL: could not obtain a JWT. Is the API running and the DB seeded?" >&2
    exit 1
  fi
  export JWT_TOKEN
  echo "Auth OK" >&2
}

reset_inventory() {
  local event_id="$1" tickets="$2"
  local code
  code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE_URL/reset-inventory" \
    -H "Content-Type: application/json" \
    -d "{\"event_id\":\"$event_id\",\"tickets\":$tickets}")
  if [ "$code" != "200" ]; then
    echo "FATAL: reset-inventory for $event_id failed (HTTP $code)" >&2
    exit 1
  fi
  echo "Reset $event_id -> $tickets tickets" >&2
}

# --- psql / redis-cli, with automatic docker-exec fallback -----------------
have_psql() {
  command -v psql > /dev/null 2>&1 && [ -n "${POSTGRES_CONNECTION_STRING:-}" ]
}

have_docker_pg() {
  command -v docker > /dev/null 2>&1 && \
    docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$DOCKER_PG_CONTAINER"
}

psql_exec() {
  local sql="$1"
  if have_psql; then
    psql "$POSTGRES_CONNECTION_STRING" -tA -c "$sql"
  elif have_docker_pg; then
    docker exec -i "$DOCKER_PG_CONTAINER" psql -U "$DOCKER_PG_USER" -d "$DOCKER_PG_DB" -tA -c "$sql"
  else
    return 1
  fi
}

have_redis_cli() {
  command -v redis-cli > /dev/null 2>&1
}

have_docker_redis() {
  command -v docker > /dev/null 2>&1 && \
    docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$DOCKER_REDIS_CONTAINER"
}

redis_cli_exec() {
  if have_redis_cli; then
    redis-cli "$@"
  elif have_docker_redis; then
    docker exec -i "$DOCKER_REDIS_CONTAINER" redis-cli "$@"
  else
    return 1
  fi
}

# Waits until the Redis stream + its consumer-group PEL are fully drained,
# i.e. the SyncWorker has finished writing everything to Postgres.
#
# FIXED: previously accumulated "waited" with `bc` in 0.5 steps, which
# produces values like ".5" — bash's `-lt` can't parse that as an integer,
# so the loop condition errored out on the very first check and the whole
# wait collapsed to ~0.5s instead of the intended timeout. This version
# only ever does integer arithmetic (whole-second sleeps), so it can't hit
# that class of bug again.
wait_for_drain() {
  local timeout_s="${1:-30}" waited_s=0
  if ! redis_cli_exec PING > /dev/null 2>&1; then
    echo "WARN: no redis-cli (host or docker) available — sleeping 5s instead, less reliable." >&2
    sleep 5
    return
  fi
  echo -n "Waiting for stream to drain..." >&2
  while [ "$waited_s" -lt "$timeout_s" ]; do
    local len pending
    len=$(redis_cli_exec XLEN reservations:stream 2>/dev/null | tr -d '[:space:]')
    pending=$(redis_cli_exec XPENDING reservations:stream sync_group 2>/dev/null | head -1 | tr -d '[:space:]')
    if [ "$len" = "0" ] || [ "$pending" = "0" ]; then
      echo " drained (waited ${waited_s}s)." >&2
      return
    fi
    sleep 1
    waited_s=$((waited_s + 1))
    echo -n "." >&2
  done
  echo " WARN: drain wait timed out after ${timeout_s}s — correctness check may race the worker." >&2
}
