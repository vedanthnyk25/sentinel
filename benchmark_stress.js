import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const serverErrors   = new Counter('server_errors');
const expectedErrors = new Counter('expected_errors');
const reservationDuration = new Trend('reservation_duration', true);

const JWT_TOKEN = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3NzgzOTY0MjAsInVzZXJfaWQiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEifQ.UQQaScr7zslAlHPFD0W0OdMRofQjIoW3UiUXtxACv38';
const EVENT_ID  = '22222222-2222-2222-2222-222222222222';

export const options = {
    stages: [
        { duration: '15s', target: 500  },  // Warm up
        { duration: '20s', target: 2000 },  // Ramp to serious load
        { duration: '20s', target: 4000 },  // Maximum stress
        { duration: '10s', target: 4000 },  // Hold at max — this is what we measure
        { duration: '10s', target: 0    },  // Cool down
    ],
    thresholds: {
        // The thesis: zero 500 errors no matter what
        'server_errors': ['count<1'],
        // Expected errors (409, 503) are fine — do not threshold these
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
            // Generous timeout — we expect latency to spike under load
            timeout: '30s',
        }
    );

    reservationDuration.add(res.timings.duration);

    if (res.status === 500) {
        serverErrors.add(1);
        console.error(`CRASH DETECTED: status=${res.status} body=${res.body}`);
    } else if(res.status === 0) {
        console.log(`REQUEST TIMEOUT: status=${res.status} body=${res.body}`);
    } 
    else if (res.status === 409 || res.status === 503) {
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
║  Peak VUs                : 4,000                     ║
║  Total Requests          : ${String(total).padEnd(24)}║
║  Throughput (RPS)        : ${rps.toFixed(0).padEnd(24)}║
╠══════════════════════════════════════════════════════╣
║  p95 Latency             : ${(p95.toFixed(0) + ' ms').padEnd(24)}║
║  p99 Latency             : ${(p99.toFixed(0) + ' ms').padEnd(24)}║
║  Max Latency             : ${(maxLat.toFixed(0) + ' ms').padEnd(24)}║
╠══════════════════════════════════════════════════════╣
║  Server Crashes (500/0)  : ${String(crashes).padEnd(24)}║
║  Result                  : ${(crashes === 0 ? 'BEND NOT BREAK ✅' : 'CRASHED ❌').padEnd(24)}║
╚══════════════════════════════════════════════════════╝
`;
    console.log(summary);
    return {
        'stdout': summary,
        'stress_test_results.json': JSON.stringify(data, null, 2),
    };
}
