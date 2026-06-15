import http from 'k6/http';
import { check, sleep } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom metrics for clean reporting
const successfulReservations = new Counter('successful_reservations');
const soldOutResponses = new Counter('sold_out_responses');
const racConditionResponses = new Counter('race_condition_responses');
const errorResponses = new Counter('error_responses');
const reservationDuration = new Trend('reservation_duration', true);

// ── PASTE YOUR JWT TOKEN HERE ──────────────────────────────
const JWT_TOKEN = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3NzgzOTY0MjAsInVzZXJfaWQiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEifQ.UQQaScr7zslAlHPFD0W0OdMRofQjIoW3UiUXtxACv38';
const EVENT_ID  = '22222222-2222-2222-2222-222222222222';
// ──────────────────────────────────────────────────────────

export const options = {
    // Constant high load — clean measurement window
    scenarios: {
        flash_sale: {
            executor: 'constant-arrival-rate',
            rate: 500,              // 500 requests per second
            timeUnit: '1s',
            duration: '30s',
            preAllocatedVUs: 1000,  // Pre-warm the VU pool
            maxVUs: 2000,
        },
    },
    thresholds: {
        // Only fail if we get actual 500 errors — not on expected 409/503
        'error_responses': ['count<1'],
        // Measure p95 during sell-out — should be very low
        'reservation_duration': ['p(95)<200'],
    },
};

export default function () {
    const url = 'http://localhost:8080/reserve';

    const payload = JSON.stringify({
        event_id: EVENT_ID,
    });

    const params = {
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${JWT_TOKEN}`,
            'Idempotency-Key': uuidv4(),
        },
    };

    const start = Date.now();
    const res = http.post(url, payload, params);
    const duration = Date.now() - start;

    reservationDuration.add(duration);

    // Track each outcome separately — this is what matters
    switch (res.status) {
        case 201:
            successfulReservations.add(1);
            break;
        case 409:
            soldOutResponses.add(1);
            break;
        case 503:
            racConditionResponses.add(1);
            break;
        default:
            errorResponses.add(1);
            console.log(`Unexpected status: ${res.status} — body: ${res.body}`);
    }

    check(res, {
        'no unexpected errors': (r) => r.status !== 500,
        'auth is working': (r) => r.status !== 401,
        'valid response code': (r) => [201, 409, 503].includes(r.status),
    });
}

export function handleSummary(data) {
    const successful = data.metrics.successful_reservations?.values?.count || 0;
    const soldOut    = data.metrics.sold_out_responses?.values?.count || 0;
    const raceCondition = data.metrics.race_condition_responses?.values?.count || 0;
    const errors     = data.metrics.error_responses?.values?.count || 0;
    const p95        = data.metrics.reservation_duration?.values?.['p(95)'] || 0;
    const rps        = data.metrics.http_reqs?.values?.rate || 0;

    const summary = `
╔══════════════════════════════════════════════════════╗
║         SENTINEL — SELL-OUT TEST RESULTS             ║
╠══════════════════════════════════════════════════════╣
║  Throughput (RPS)        : ${rps.toFixed(0).padEnd(24)}║
║  p95 Latency             : ${(p95.toFixed(2) + ' ms').padEnd(24)}║
╠══════════════════════════════════════════════════════╣
║  Successful Reservations : ${String(successful).padEnd(24)}║
║  Sold Out (409)          : ${String(soldOut).padEnd(24)}║
║  Race Condition (503)    : ${String(raceCondition).padEnd(24)}║
║  Unexpected Errors       : ${String(errors).padEnd(24)}║
╠══════════════════════════════════════════════════════╣
║  Oversell occurred       : ${(successful > 100 ? 'YES ❌' : 'NO ✅').padEnd(24)}║
╚══════════════════════════════════════════════════════╝
`;
    console.log(summary);
    return {
        'stdout': summary,
        'sell_out_results.json': JSON.stringify(data, null, 2),
    };
}
