#!/bin/bash
set -e

BASE_URL="http://localhost:8080"
EVENT_ID="22222222-2222-2222-2222-222222222222"

reset_inventory() {
    local tickets=$1

    echo "Resetting inventory to ${tickets} tickets..."

    curl -s -X POST "$BASE_URL/reset-inventory" \
        -H "Content-Type: application/json" \
        -d "{\"event_id\":\"$EVENT_ID\",\"tickets\":$tickets}" >/dev/null
}

login() {
    echo "Logging in..."

    JWT_TOKEN=$(curl -s -X POST "$BASE_URL/auth/login" \
        -H "Content-Type: application/json" \
        -d '{"email":"test@sentinel.com","password":"password123"}' \
        | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$JWT_TOKEN" ]; then
        echo "❌ Login failed"
        exit 1
    fi

    echo "✅ JWT acquired"
}

############################################
# Login once
############################################

login

############################################
# SELL-OUT TEST
############################################

echo
echo "========================================"
echo "Running Sell-Out Benchmark"
echo "========================================"

reset_inventory 100

JWT_TOKEN="$JWT_TOKEN" k6 run benchmark_sellout.js

############################################
# STRESS TEST
############################################

echo
echo "========================================"
echo "Running Stress Benchmark"
echo "========================================"

reset_inventory 100000

JWT_TOKEN="$JWT_TOKEN" k6 run benchmark_stress.js

echo
echo "========================================"
echo "All benchmarks completed."
echo "========================================"
