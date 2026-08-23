#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"
mkdir -p results

source lib.sh
load_env

# Hardcode the label so the JS files and verify_postgres.sh don't break
LABEL="current"
CORRECTNESS_EVENT="22222222-2222-2222-2222-222222222222"
CORRECTNESS_TICKETS=100
THROUGHPUT_EVENT="33333333-3333-3333-3333-333333333333"
THROUGHPUT_TICKETS=5000000

echo "========================================"
echo " SENTINEL BENCHMARK SUITE"
echo " commit: $(git -C .. rev-parse --short HEAD 2>/dev/null || echo unknown)"
echo "========================================"

if ! curl -sf -o /dev/null "$BASE_URL/events"; then
  echo "FATAL: API not reachable at $BASE_URL. Start the server first." >&2
  exit 1
fi

login_or_register

echo ""
echo "---- [1/2] Correctness / no-oversell test ----"
reset_inventory "$CORRECTNESS_EVENT" "$CORRECTNESS_TICKETS"
JWT_TOKEN="$JWT_TOKEN" EVENT_ID="$CORRECTNESS_EVENT" TICKETS="$CORRECTNESS_TICKETS" \
  LABEL="$LABEL" BASE_URL="$BASE_URL" RATE=2000 DURATION=10s \
  k6 run correctness.js

echo "---- Waiting for async worker to drain before checking Postgres ----"
wait_for_drain 30

echo "---- Verifying against Postgres (source of truth) ----"
CORRECTNESS_STATUS=0
bash verify_postgres.sh "$CORRECTNESS_EVENT" "$CORRECTNESS_TICKETS" \
  "results/correctness_${LABEL}.json" "$LABEL" || CORRECTNESS_STATUS=$?

echo ""
echo "---- [2/2] Max throughput test ----"
reset_inventory "$THROUGHPUT_EVENT" "$THROUGHPUT_TICKETS"
JWT_TOKEN="$JWT_TOKEN" EVENT_ID="$THROUGHPUT_EVENT" \
  LABEL="$LABEL" BASE_URL="$BASE_URL" \
  k6 run throughput.js

echo ""
echo "========================================"
if [ "$CORRECTNESS_STATUS" -ne 0 ]; then
  echo " RESULT: FAIL — Postgres correctness check failed. See output above."
  echo "========================================"
  exit 1
fi
echo " RESULT: PASS"
echo " Throughput RPS : $(python3 -c "import json;print(json.load(open('results/throughput_${LABEL}.json'))['achieved_rps'])")"
echo "========================================"
