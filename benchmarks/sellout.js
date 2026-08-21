import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { Counter, Trend } from 'k6/metrics';

const successfulReservations = new Counter('successful_reservations');
const soldOutResponses = new Counter('sold_out_responses');
const errorResponses = new Counter('error_responses');
const reservationDuration = new Trend('reservation_duration', true);

const JWT_TOKEN = __ENV.JWT_TOKEN;
const EVENT_ID  = '22222222-2222-2222-2222-222222222222';

export const options = {
    scenarios: {
        flash_sale: {
            executor: 'constant-arrival-rate',
            rate: 1000,             
            timeUnit: '1s',
            duration: '15s',
            preAllocatedVUs: 1500,  
        },
    },
    thresholds: {
        'error_responses': ['count<1'],
        'successful_reservations': ['count<=100'], // Hard SLA against overselling
    },
};

export default function () {
    const payload = JSON.stringify({ event_id: EVENT_ID });
    const params = {
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${JWT_TOKEN}`,
            'Idempotency-Key': uuidv4(),
        },
        timeout: '10s',
    };

    const start = Date.now();
    const res = http.post('http://localhost:8080/reserve', payload, params);
    reservationDuration.add(Date.now() - start);

    switch (res.status) {
        case 201: successfulReservations.add(1); break;
        case 409: soldOutResponses.add(1); break;
        default: errorResponses.add(1); break;
    }
}

export function handleSummary(data) {
    const success = data.metrics.successful_reservations?.values?.count || 0;
    const errors = data.metrics.error_responses?.values?.count || 0;
    
    return {
        'stdout': `\n🎯 SELL-OUT RESULTS: ${success} Sold | ${errors} Errors | Oversold: ${success > 100 ? 'YES ❌' : 'NO ✅'}\n`,
    };
}
