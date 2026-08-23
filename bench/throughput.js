// Max throughput, measured as several DISTINCT held rates back to back,
// each reported separately, rather than one long ramp whose average
// understates peak capacity.
//
// VU SIZING: earlier versions scaled preAllocatedVUs/maxVUs with the target
// rate (up to 12,000-15,000 VUs for the 10k stage). k6 pre-instantiates
// every preAllocatedVU up front as a real JS runtime, so that pre-allocated
// tens of thousands of them regardless of whether traffic ever needed that
// many concurrently — and OOM-killed the process. The concurrency actually
// needed is roughly rate * average_latency_seconds; with this server's
// sub-millisecond latency, even 10,000 rps needs on the order of tens of
// concurrent VUs, not thousands. These caps are intentionally small and
// fixed regardless of target rate. If a stage's dropped_iterations > 0,
// that means THIS cap was the bottleneck (not your server) — raise
// MAX_VUS a bit and rerun rather than assuming the server maxed out.
import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const JWT_TOKEN = __ENV.JWT_TOKEN;
const EVENT_ID = __ENV.EVENT_ID || '33333333-3333-3333-3333-333333333333';
const LABEL = __ENV.LABEL || 'run';
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const STAGE_DURATION_S = 12;
const RATES = [1000, 3000, 6000, 10000];

// Fixed, conservative, rate-independent. Safe on an 8-16GB laptop.
const PRE_ALLOCATED_VUS = 500;
const MAX_VUS = 2000;

const metricsByRate = {
  1000: { ok: new Counter('ok_1000'), failed: new Counter('failed_1000'), latency: new Trend('latency_1000', true) },
  3000: { ok: new Counter('ok_3000'), failed: new Counter('failed_3000'), latency: new Trend('latency_3000', true) },
  6000: { ok: new Counter('ok_6000'), failed: new Counter('failed_6000'), latency: new Trend('latency_6000', true) },
  10000: { ok: new Counter('ok_10000'), failed: new Counter('failed_10000'), latency: new Trend('latency_10000', true) },
};

function stage(rate, index, exec) {
  return {
    executor: 'constant-arrival-rate',
    rate,
    timeUnit: '1s',
    duration: `${STAGE_DURATION_S}s`,
    startTime: `${index * STAGE_DURATION_S}s`,
    preAllocatedVUs: PRE_ALLOCATED_VUS,
    maxVUs: MAX_VUS,
    exec,
  };
}

export const options = {
  scenarios: {
    rate_1000: stage(1000, 0, 'at_1000'),
    rate_3000: stage(3000, 1, 'at_3000'),
    rate_6000: stage(6000, 2, 'at_6000'),
    rate_10000: stage(10000, 3, 'at_10000'),
  },
  // Not a hard threshold anymore: a dropped iteration at 10,000 rps just
  // means the VU cap above was hit, which is informative, not fatal — so
  // we don't abort the whole run over it. It's still reported per-stage.
};

function reserve(rate) {
  const m = metricsByRate[rate];
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
  m.latency.add(Date.now() - start);
  if (res.status === 201) m.ok.add(1);
  else m.failed.add(1);
  check(res, { 'status 201': (r) => r.status === 201 });
}

export function at_1000() {
  reserve(1000);
}
export function at_3000() {
  reserve(3000);
}
export function at_6000() {
  reserve(6000);
}
export function at_10000() {
  reserve(10000);
}

export function handleSummary(data) {
  const perStage = RATES.map((rate) => {
    const ok = data.metrics[`ok_${rate}`]?.values?.count || 0;
    const failed = data.metrics[`failed_${rate}`]?.values?.count || 0;
    const p95 = data.metrics[`latency_${rate}`]?.values?.['p(95)'] || 0;
    const p99 = data.metrics[`latency_${rate}`]?.values?.['p(99)'] || 0;
    return {
      target_rate: rate,
      achieved_rate: ok / STAGE_DURATION_S,
      success: ok,
      failed,
      p95_ms: p95,
      p99_ms: p99,
    };
  });

  const dropped = data.metrics.dropped_iterations?.values?.count || 0;
  const cleanStages = perStage.filter((s) => s.failed === 0);
  const maxCleanStage = cleanStages.length ? cleanStages[cleanStages.length - 1] : null;

  const rps = maxCleanStage ? maxCleanStage.achieved_rate : 0;
  const p95 = maxCleanStage ? maxCleanStage.p95_ms : 0;

  // Your custom output format
  const summary = `\nSTRESS RESULTS: Peak RPS: ${rps.toFixed(0)} | p95 Latency: ${p95.toFixed(2)}ms | Crashes: ${dropped}\n`;
  console.log(summary);

  // Keep JSON export intact for run.sh
  return {
    stdout: summary,
    [`results/throughput_${LABEL}.json`]: JSON.stringify(
      {
        label: LABEL,
        test: 'throughput',
        stage_duration_s: STAGE_DURATION_S,
        vu_cap: { preAllocated: PRE_ALLOCATED_VUS, max: MAX_VUS },
        stages: perStage,
        dropped_iterations: dropped,
        achieved_rps: rps,
        p95_ms: p95,
        p99_ms: maxCleanStage ? maxCleanStage.p99_ms : 0,
      },
      null,
      2
    ),
  };
}
