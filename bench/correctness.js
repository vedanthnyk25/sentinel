// Correctness under contention: fire far more requests than tickets exist,
// against a small fixed inventory, and prove no overselling / no crashes.
// This is the test whose numbers you'd actually cite for "handles race
// conditions correctly" — the throughput test is a separate claim.
import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const success = new Counter('reservation_success');
const soldOut = new Counter('reservation_sold_out');
const raceCond = new Counter('reservation_race_cond');
const unexpected = new Counter('reservation_unexpected');
const latency = new Trend('reservation_latency', true);

const JWT_TOKEN = __ENV.JWT_TOKEN;
const EVENT_ID = __ENV.EVENT_ID || '22222222-2222-2222-2222-222222222222';
const TICKETS = parseInt(__ENV.TICKETS || '100', 10);
const LABEL = __ENV.LABEL || 'run';
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    contention: {
      executor: 'constant-arrival-rate',
      rate: parseInt(__ENV.RATE || '2000', 10),
      timeUnit: '1s',
      duration: __ENV.DURATION || '10s',
      // Generous headroom so the load generator itself is never the
      // bottleneck — if it still can't keep up, dropped_iterations catches it.
      preAllocatedVUs: 2000,
      maxVUs: 4000,
    },
  },
  thresholds: {
    // Zero real server errors, ever.
    'reservation_unexpected': ['count<1'],
    // Never accept more than TICKETS successes — the actual oversell guard.
    'reservation_success': [`count<=${TICKETS}`],
    // If this fails, the LOAD GENERATOR couldn't keep up with `rate` and
    // every number in this run is suspect — not a server-side result.
    'dropped_iterations': ['count<1'],
  },
};

export default function () {
  const payload = JSON.stringify({ event_id: EVENT_ID });
  const params = {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${JWT_TOKEN}`,
      'Idempotency-Key': uuidv4(),
    },
    timeout: '10s',
  };

  const start = Date.now();
  const res = http.post(`${BASE_URL}/reserve`, payload, params);
  latency.add(Date.now() - start);

  switch (res.status) {
    case 201:
      success.add(1);
      break;
    case 409:
      soldOut.add(1);
      break;
    case 503:
      raceCond.add(1);
      break;
    default:
      unexpected.add(1);
      console.log(`unexpected status ${res.status}: ${res.body}`);
  }

  check(res, {
    'no 5xx/0 (except expected 503)': (r) => r.status !== 500 && r.status !== 0,
    'auth accepted': (r) => r.status !== 401,
  });
}

export function handleSummary(data) {
  const out = {
    label: LABEL,
    test: 'correctness',
    tickets: TICKETS,
    requested_rate: parseInt(__ENV.RATE || '2000', 10),
    achieved_rps: data.metrics.http_reqs?.values?.rate || 0,
    dropped_iterations: data.metrics.dropped_iterations?.values?.count || 0,
    success: data.metrics.reservation_success?.values?.count || 0,
    sold_out: data.metrics.reservation_sold_out?.values?.count || 0,
    race_cond: data.metrics.reservation_race_cond?.values?.count || 0,
    unexpected: data.metrics.reservation_unexpected?.values?.count || 0,
    p50_ms: data.metrics.reservation_latency?.values?.med || 0,
    p95_ms: data.metrics.reservation_latency?.values?.['p(95)'] || 0,
    p99_ms: data.metrics.reservation_latency?.values?.['p(99)'] || 0,
    max_ms: data.metrics.reservation_latency?.values?.max || 0,
  };

  const oversold = out.success > TICKETS;
  
  // Your custom output format
  const summary = `\nSELL-OUT RESULTS: ${out.success} Sold | ${out.unexpected} Errors | Oversold: ${oversold ? 'YES ❌' : 'NO ✅'}\n`;
  console.log(summary);

  // Keep JSON export intact so verify_postgres.sh and compare.py don't crash
  return {
    stdout: summary,
    [`results/correctness_${LABEL}.json`]: JSON.stringify(out, null, 2),
  };
}
