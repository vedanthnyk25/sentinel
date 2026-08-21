import http from 'k6/http';
import { check, sleep } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const successfulReservations = new Counter('successful_reservations');
const catalogReads = new Counter('catalog_reads');
const readLatency = new Trend('read_latency', true);
const writeLatency = new Trend('write_latency', true);

const JWT_TOKEN = __ENV.JWT_TOKEN;
const EVENT_ID  = '22222222-2222-2222-2222-222222222222';
const BASE_URL  = 'http://localhost:8080';

export const options = {
    discardResponseBodies: true,
    scenarios: {
        catalog_browsing: {
            executor: 'ramping-vus',
            startVUs: 50,
            stages: [
                { duration: '10s', target: 500 },  
                { duration: '20s', target: 1000 }, 
                { duration: '10s', target: 0 },    
            ],
            exec: 'browseCatalog',
        },
        flash_sale_stampede: {
            executor: 'ramping-arrival-rate',
            startTime: '10s',       
            startRate: 100,         
            timeUnit: '1s',
            preAllocatedVUs: 1000,  
            maxVUs: 3000,
            stages: [
                { duration: '5s', target: 1500 },  
                { duration: '10s', target: 2500 }, 
                { duration: '5s', target: 0 },    
            ],
            exec: 'attemptReservation',
        },
    },
};

export function browseCatalog() {
    const start = Date.now();
    http.get(`${BASE_URL}/events`);
    http.get(`${BASE_URL}/events/${EVENT_ID}`);
    readLatency.add(Date.now() - start);
    catalogReads.add(1);
    sleep(Math.random() * 2 + 1); 
}

export function attemptReservation() {
    const payload = JSON.stringify({ event_id: EVENT_ID });
    const params = {
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${JWT_TOKEN}`,
            'Idempotency-Key': uuidv4(),
        },
    };

    const start = Date.now();
    const res = http.post(`${BASE_URL}/reserve`, payload, params);
    writeLatency.add(Date.now() - start);

    if (res.status === 201) {
        successfulReservations.add(1);
    }
}

export function handleSummary(data) {
    const rps = data.metrics.http_reqs?.values?.rate || 0;
    const reads = data.metrics.catalog_reads?.values?.count || 0;
    const success = data.metrics.successful_reservations?.values?.count || 0;
    const readP95 = data.metrics.read_latency?.values?.['p(95)'] || 0;
    const writeP95 = data.metrics.write_latency?.values?.['p(95)'] || 0;

    return {
        'stdout': `\n🌍 REAL-WORLD RESULTS: ${rps.toFixed(0)} Peak RPS | ${reads} Browsing Hits (${readP95.toFixed(1)}ms p95) | ${success} Bookings (${writeP95.toFixed(1)}ms p95)\n`,
    };
}
