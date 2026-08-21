import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const serverErrors   = new Counter('server_errors');
const expectedErrors = new Counter('expected_errors');
const reservationDuration = new Trend('reservation_duration', true);

const JWT_TOKEN = __ENV.JWT_TOKEN;
const EVENT_ID  = '22222222-2222-2222-2222-222222222222';

export const options = {
    scenarios: {
        stress_test: {
            // Forces k6 to fire exact RPS, avoiding the coordinated omission trap
            executor: 'ramping-arrival-rate',
            startRate: 500,
            timeUnit: '1s',
            preAllocatedVUs: 1000, // Pre-warm connection pool
            maxVUs: 6000,          // High ceiling so k6 doesn't choke if latency spikes
            stages: [
                { target: 1000, duration: '15s' }, // Ramp up to 1k RPS
                { target: 2500, duration: '20s' }, // Ramp to 2.5k RPS
                { target: 5000, duration: '20s' }, // Ramp to maximum stress (5k RPS)
                { target: 5000, duration: '15s' }, // Hold at maximum
                { target: 0,    duration: '10s' }, // Cool down
            ],
        },
    },
    thresholds: {
        // The thesis: zero 500 errors no matter what
        'server_errors': ['count<1'],
        // SLA: 95% of requests MUST complete in under 2 seconds (2000ms)
        'reservation_duration': ['p(95)<2000'],
    },
};

export default function () {
    const res = http.post(
        'http://localhost:8080/reserve',
        JSON.stringify({ event_id: EVENT_ID }),
        {
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${JWT_TOKEN}`,
                'Idempotency-Key': uuidv4(),
            },
            timeout: '30s',
        }
    );

    reservationDuration.add(res.timings.duration);

    if (res.status === 500 || res.status === 0) {
        serverErrors.add(1);
        console.error(`FAILED REQUEST: status=${res.status} error=${res.error} body=${res.body}`);
    } else if (res.status === 409 || res.status === 503) {
        expectedErrors.add(1);
    }

    check(res, {
        'server did not crash': (r) => r.status !== 500 && r.status !== 0,
        'auth working':         (r) => r.status !== 401,
    });
}

export function handleSummary(data) {
    const crashes  = data.metrics.server_errors?.values?.count || 0;
    const total    = data.metrics.http_reqs?.values?.count || 0;
    const p95      = data.metrics.reservation_duration?.values?.['p(95)'] || 0;
    const p99      = data.metrics.reservation_duration?.values?.['p(99)'] || 0;
    const maxLat   = data.metrics.reservation_duration?.values?.max || 0;
    const rps      = data.metrics.http_reqs?.values?.rate || 0;

    const summary = `
╔══════════════════════════════════════════════════════╗
║         SENTINEL — STRESS TEST RESULTS               ║
╠══════════════════════════════════════════════════════╣
║  Total Requests          : ${String(total).padEnd(24)}║
║  Peak Throughput (RPS)   : ${rps.toFixed(0).padEnd(24)}║
╠══════════════════════════════════════════════════════╣
║  p95 Latency             : ${(p95.toFixed(0) + ' ms').padEnd(24)}║
║  p99 Latency             : ${(p99.toFixed(0) + ' ms').padEnd(24)}║
║  Max Latency             : ${(maxLat.toFixed(0) + ' ms').padEnd(24)}║
╠══════════════════════════════════════════════════════╣
║  Server Crashes (500/0)  : ${String(crashes).padEnd(24)}║
║  Result                  : ${(crashes === 0 && p95 < 2000 ? 'BEND NOT BREAK ✅' : 'FAILED SLA/CRASH ❌').padEnd(24)}║
╚══════════════════════════════════════════════════════╝
`;
    console.log(summary);
    return {
        'stdout': summary,
        'stress_test_results.json': JSON.stringify(data, null, 2),
    };
}
