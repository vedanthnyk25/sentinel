# 🛡️ Sentinel

**A high-concurrency, fault-tolerant ticket reservation engine built in Go.**

Sentinel is designed to solve the "Thundering Herd" problem in e-commerce and ticketing systems. When thousands of users attempt to purchase a limited number of tickets at the exact same millisecond, Sentinel strictly guarantees data consistency, prevents overselling, and ensures zero database crashes under extreme load.

## ⚡ Architecture & Key Features

* **Optimistic Concurrency Control (OCC):** Utilizes PostgreSQL versioning to detect and prevent race conditions during simultaneous row updates, ensuring absolute inventory accuracy without relying on slow, pessimistic table locks.
* **In-Memory Thundering Herd Filter:** Leverages **Redis** atomic decrements (`DECR`) to instantly reject traffic once tickets are sold out, shielding the primary PostgreSQL database from unnecessary load.
* **Asynchronous State Recovery (The "Janitor"):** Implements a **RabbitMQ Dead-Letter Exchange (DLX)** architecture. Reserved but unpaid tickets are held in a pending queue with a 10-minute TTL. Expired messages trigger a Go worker that autonomously rolls back the database transaction and refunds the Redis stock.
* **Idempotency Shield:** Enforces strict idempotency keys (`ON CONFLICT`) to safely handle network retries, duplicate clicks, and frontend bugs without processing duplicate reservations.
* **Connection Pooling:** Governed PostgreSQL connection limits (`SetMaxOpenConns`) keep the database stable under massive traffic spikes, trading latency for 100% uptime.

## 🛠️ Tech Stack

* **Language:** Go (Golang)
* **Database:** PostgreSQL (with `sqlc` for type-safe query generation)
* **Cache:** Redis
* **Message Broker:** RabbitMQ
* **Routing:** `go-chi/chi`
* **Load Testing:** Grafana k6

## 📊 Benchmarks & Performance

Sentinel was rigorously stress-tested using **Grafana k6** to simulate extreme real-world ticketing scenarios. 

### Test 1: Standard Load (Instant Sell-Out)
*Simulating a high volume of traffic hitting the API to purchase 50 available tickets.*
* **Requests Per Second (RPS):** ~4,800
* **p95 Latency:** 34.91ms
* **Success Rate:** 100% (Exactly 50 tickets secured, remaining requests correctly bounded with `409 Sold Out` or `503 Service Unavailable`).

### Test 2: Stress Test (The Breaking Point)
*Ramping up to 4,000 Concurrent Virtual Users (VUs) to test connection pool saturation and CPU limits.*
* **Total Requests Processed:** 187,260
* **Database Crashes / 500 Errors:** **0**
* **Result:** Under 200% PostgreSQL CPU load, the Go connection pool successfully queued overflow traffic. Max latency temporarily spiked to ~1.2s to prevent database out-of-memory (OOM) failures, proving a "bend but don't break" infrastructure.

