import http from 'k6/http';
import { check } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

export const options = {
    // This turns the dial up slowly to find the breaking point
    stages: [
        { duration: '10s', target: 500 },  // Ramp up to 500 concurrent users
        { duration: '15s', target: 2000 }, // Push it to 2,000 users (Danger Zone)
        { duration: '10s', target: 4000 }, // Try to break the server
        { duration: '5s', target: 0 },     // Scale down
    ],
    // Let's set a strict threshold: if p95 goes over 500ms, the engine is officially "overloaded"
    thresholds: {
        http_req_duration: ['p(95)<500'], 
    },
};

export default function () {
    const url = 'http://localhost:8080/reserve';
    
    const payload = JSON.stringify({
        user_id: '11111111-1111-1111-1111-111111111111',
        event_id: '22222222-2222-2222-2222-222222222222',
    });

    const params = {
        headers: {
            'Content-Type': 'application/json',
            'Idempotency-Key': uuidv4(), 
        },
    };

    const res = http.post(url, payload, params);

    check(res, {
        'is status 201, 409, or 503': (r) => [201, 409, 503].includes(r.status),
        'no 500 internal server errors': (r) => r.status !== 500,
    });
}
