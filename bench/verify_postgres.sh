#!/usr/bin/env bash
# Compares what k6 observed over HTTP against what actually landed in
# Postgres once the async SyncWorker has drained the Redis stream.
#
# Exit codes: 0 = verified-and-passed, OR couldn't verify (see stdout for
# which). 1 = verified AND found a real correctness violation. run.sh treats
# these differently — only exit 1 fails the whole suite.
set -uo pipefail  # not -e: we want to fall through to a clear SKIP message

EVENT_ID="$1"
TICKETS="$2"
K6_SUMMARY_JSON="$3"   # e.g. results/correctness_after.json
LABEL="${4:-run}"

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$DIR/lib.sh"
load_env

if ! have_psql && ! have_docker_pg; then
  echo ""
  echo "SKIP: no psql binary and no reachable '$DOCKER_PG_CONTAINER' container."
  echo "      Correctness was checked at the HTTP layer only (k6's own oversold"
  echo "      check) — this does NOT verify the async write actually landed in"
  echo "      Postgres. Install psql, or run docker-compose so the container"
  echo "      name matches, to get the real check."
  echo ""
  exit 0
fi

CONFIRMED_COUNT=$(psql_exec "SELECT count(*) FROM reservations WHERE event_id = '$EVENT_ID' AND status != 'expired';" | tr -d '[:space:]')
REMAINING=$(psql_exec "SELECT available_tickets FROM inventory WHERE event_id = '$EVENT_ID';" | tr -d '[:space:]')

if [ -z "$CONFIRMED_COUNT" ]; then
  echo "SKIP: psql/docker exec ran but returned nothing — check DB connectivity manually."
  exit 0
fi

HTTP_SUCCESS=$(python3 -c "import json; print(json.load(open('$K6_SUMMARY_JSON'))['success'])")

echo ""
echo "POSTGRES VERIFICATION [$LABEL]"
echo "  HTTP 201s reported by k6        : $HTTP_SUCCESS"
echo "  Confirmed rows in Postgres      : $CONFIRMED_COUNT"
echo "  Postgres available_tickets left : $REMAINING (started at $TICKETS)"

FAIL=0

if [ "$CONFIRMED_COUNT" -gt "$TICKETS" ]; then
  echo "  OVERSOLD IN POSTGRES: $CONFIRMED_COUNT > $TICKETS  <-- FAIL"
  FAIL=1
fi

if [ "$CONFIRMED_COUNT" -ne "$HTTP_SUCCESS" ]; then
  echo "  MISMATCH: k6 saw $HTTP_SUCCESS successes but only $CONFIRMED_COUNT rows exist."
  echo "  -> a reservation was accepted (Redis) but never landed in Postgres."
  echo "     Expected to be 0 in a clean run with no worker crashes; non-zero"
  echo "     here means the crash-recovery path isn't fully closing the gap."
  FAIL=1
fi

EXPECTED_REMAINING=$((TICKETS - CONFIRMED_COUNT))
if [ -n "$REMAINING" ] && [ "$REMAINING" -ne "$EXPECTED_REMAINING" ]; then
  echo "  NOTE: Postgres available_tickets ($REMAINING) != tickets - confirmed ($EXPECTED_REMAINING)."
  echo "        Informational only — Redis, not this column, is the source of"
  echo "        truth for admission in this design."
fi

if [ "$FAIL" -eq 0 ]; then
  echo "  RESULT: PASS (verified against Postgres)"
else
  echo "  RESULT: FAIL"
fi
echo ""

exit $FAIL
