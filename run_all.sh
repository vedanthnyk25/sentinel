#!/bin/bash
set -e

BASE_URL="http://localhost:8080"
EVENT_ID="22222222-2222-2222-2222-222222222222"

echo "========================================"
echo "🛡️  SENTINEL LOAD TESTING SUITE"
echo "========================================"

# 1. Health Check
if ! curl -s --head  --request GET "$BASE_URL/events" | grep "200 OK" > /dev/null; then 
   echo "❌ API is down. Please start the Go server first."
   exit 1
fi

# 2. Authenticate
echo "🔑 Acquiring Auth Token..."
JWT_TOKEN=$(curl -s -X POST "$BASE_URL/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"test@sentinel.com","password":"password123"}' \
    | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$JWT_TOKEN" ]; then
    echo "❌ Login failed. Check your database seeded data."
    exit 1
fi

# Helper function to reset inventory
reset_inventory() {
    local tickets=$1
    echo "🔄 Resetting inventory to ${tickets} tickets..."
    RESET_RES=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE_URL/reset-inventory" \
        -H "Content-Type: application/json" \
        -d "{\"event_id\":\"$EVENT_ID\",\"tickets\":$tickets}")

    if [ "$RESET_RES" -ne 200 ]; then
        echo "❌ Failed to reset inventory. HTTP Status: $RESET_RES"
        exit 1
    fi
}

export JWT_TOKEN="$JWT_TOKEN"

# ---------------------------------------------------------
# EXECUTE TEST SUITE
# ---------------------------------------------------------

echo -e "\n🧪 TEST 1: THE SELL-OUT (Concurrency & Race Conditions)"
reset_inventory 100
k6 run benchmarks/sellout.js

echo -e "\n🧪 TEST 2: THE STRESS TEST (Max Throughput)"
reset_inventory 500000
k6 run benchmarks/stress.js

echo -e "\n🧪 TEST 3: REAL-WORLD LOAD (Mixed Read/Write Traffic)"
reset_inventory 1000
k6 run benchmarks/real_world.js

echo "========================================"
echo "✅ All Benchmarks Completed Successfully."
