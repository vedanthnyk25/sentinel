import http from 'k6/http';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const serverErrors = new Counter('server_errors');
const reservationDuration = new Trend('reservation_duration', true);

const JWT_TOKEN = __ENV.JWT_TOKEN;
const EVENT_ID  = '22222222-2222-2222-2222-222222222222';

export const options = {
    scenarios: {
        stress: {
            executor: 'ramping-arrival-rate',
            startRate: 500,
            timeUnit: '1s',
            preAllocatedVUs: 2000, 
            maxVUs: 5000,          
            stages: [
                { target: 1000, duration: '10s' }, 
                { target: 3000, duration: '15s' }, 
                { target: 5000, duration: '15s' }, 
                { target: 0,    duration: '5s' }, 
            ],
        },
    },
    thresholds: {
        'server_errors': ['count<1'],
    },
};

export default function () {
    const res = http.post('http://localhost:8080/reserve', 
        JSON.stringify({ event_id: EVENT_ID }), 
        {
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${JWT_TOKEN}`,
                'Idempotency-Key': uuidv4(),
            }
        }
    );

    reservationDuration.add(res.timings.duration);
    if (res.status === 500 || res.status === 0) {
        serverErrors.add(1);
    }
}

export function handleSummary(data) {
    const rps = data.metrics.http_reqs?.values?.rate || 0;
    const p95 = data.metrics.reservation_duration?.values?.['p(95)'] || 0;
    const errors = data.metrics.server_errors?.values?.count || 0;

    return {
        'stdout': `\n🚀 STRESS RESULTS: Peak RPS: ${rps.toFixed(0)} | p95 Latency: ${p95.toFixed(2)}ms | Crashes: ${errors}\n`,
    };
}
