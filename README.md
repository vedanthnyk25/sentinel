# 🛡️ Sentinel

**A high-concurrency, fault-tolerant ticket reservation engine built in Go.**

Sentinel is designed to solve the **Thundering Herd** problem in ticketing systems — the scenario where thousands of users attempt to purchase a limited number of tickets at the exact same millisecond. Sentinel strictly guarantees data consistency, prevents overselling, and ensures zero database crashes under extreme load through four composable defensive layers.

---

## 📊 Benchmarks

Stress-tested using **Grafana k6** to simulate extreme real-world flash-sale scenarios.

| Metric | Sell-Out Test | Stress Test |
|---|---|---|
| Throughput | 500 RPS (rate-limited) | **4,082 RPS** |
| Concurrent Virtual Users | 1,000 | **4,000** |
| Total Requests | 15,001 | **306,185** |
| p95 Latency | **5 ms** | 1,811 ms |
| Successful Reservations | **100 / 100** | — |
| Oversells | **0** | **0** |
| Server Crashes (5xx) | **0** | **0** |

**Sell-Out Test:** Exactly 100 of 100 available tickets were sold across 15,001 concurrent requests. Every remaining request received a meaningful `409 Sold Out` or `503 Race Condition` — zero unexpected errors.

**Stress Test:** At 4,000 concurrent virtual users across 306,185 requests, the server produced zero crashes. Under extreme load the connection pool queued overflow traffic — p95 latency rose to ~1.8s but the database never went down. **Bend, don't break.**

---

## ⚡ Architecture

Sentinel composes four defensive layers. Each one handles a failure mode the previous cannot.

```
                        ┌─────────────────────────────────────┐
                        │         Go HTTP Server :8080         │
                        │         (go-chi/chi router)          │
                        └──────────────┬──────────────────────┘
                                       │
                    ┌──────────────────▼──────────────────────┐
                    │           JWT Auth Middleware             │
                    └──────────────────┬──────────────────────┘
                                       │
          ┌───────────────┬────────────┴────────────┬─────────────────┐
          │               │                         │                 │
    ┌─────▼─────┐  ┌──────▼──────┐        ┌────────▼───────┐  ┌──────▼──────┐
    │   Auth    │  │   Catalog   │        │  Reservation   │  │   Payment   │
    │  Service  │  │   Service   │        │    Service     │  │   Service   │
    └─────┬─────┘  └──────┬──────┘        └────────┬───────┘  └──────┬──────┘
          │               │                        │                  │
          │               │          ┌─────────────┼──────────┐       │
          │               │          │             │          │       │
    ┌─────▼───────────────▼──┐  ┌────▼────┐  ┌────▼────┐  ┌──▼──────▼────┐
    │       PostgreSQL        │  │  Redis  │  │RabbitMQ │  │    Stripe    │
    │  (sqlc type-safe ORM)  │  │  Cache  │  │  (DLX)  │  │   Checkout   │
    └────────────────────────┘  └─────────┘  └────┬────┘  └─────────────┘
                                                   │
                                          ┌────────▼────────┐
                                          │  Janitor Worker  │
                                          │   (goroutine)    │
                                          └─────────────────┘
```

---

### Layer 1 — Redis Thundering Herd Gate

When a flash sale opens, Redis absorbs the flood before it reaches PostgreSQL. Each reservation attempt atomically decrements an in-memory stock counter (`DECR`). Since Redis is single-threaded, this operation is serialised by design — once the counter hits zero, every subsequent request is rejected at cache speed without touching the database.

```
10,000 requests arrive simultaneously
        ↓
Redis DECR (atomic, microseconds)
        ↓
Requests 1–100:   remainingStock >= 0  →  proceed to PostgreSQL
Requests 101–10,000: remainingStock < 0  →  409 Sold Out (database never touched)
```

### Layer 2 — Optimistic Concurrency Control (OCC)

Requests that pass the Redis gate still race each other at the database level. OCC resolves this without pessimistic row locks. The inventory table carries a `version` column that increments on every successful update. The SQL update asserts `WHERE version = $current_version` — if another transaction committed first, the version has changed, zero rows are affected, and the conflict is detected cleanly.

```sql
UPDATE inventory
SET version         = version + 1,
    available_tickets = available_tickets - 1
WHERE event_id = $1
  AND version   = $2          -- OCC check: fail if someone else got here first
  AND available_tickets >= 1  -- safety net: never go below zero
```

If `rowsAffected = 0`, the service returns `503 Service Unavailable` and the client retries. No locks, no contention, no blocking.

### Layer 3 — Idempotency Shield

Network failures cause clients to retry requests. Without protection, a retry creates a duplicate reservation and charges the user twice. Sentinel requires a client-generated `Idempotency-Key` UUID header on every reservation request. The key is inserted with `ON CONFLICT DO NOTHING` — if it already exists, the entire reservation logic is bypassed.

```sql
INSERT INTO idempotency_keys (user_id, key)
VALUES ($1, $2)
ON CONFLICT (key) DO NOTHING
RETURNING id
-- Returns no rows on conflict → Go detects sql.ErrNoRows → 409 Duplicate Request
```

First request: key inserted, processing continues. Any retry: conflict detected, `409` returned immediately. Zero duplicate reservations regardless of how many retries occur.

### Layer 4 — Janitor Worker (Dead-Letter Exchange)

Unpaid reservations must eventually release their tickets. Rather than polling the database with a cron job, Sentinel uses RabbitMQ's native TTL and Dead-Letter Exchange (DLX) routing. The broker handles expiry and routing entirely — no application-level timers, no polling.

```
POST /reserve succeeds
    │
    └─► publish {reservation_id, event_id} to [reservations.pending]
                    │
                    │  x-message-ttl = 600,000ms (10 minutes)
                    │  x-dead-letter-exchange = "dlx.exchange"
                    │
          ┌─────────▼──────────────────────────────────┐
          │  User pays within 10 min                    │
          │  → Stripe webhook fires                     │
          │  → reservation status = 'confirmed'         │
          │  → Janitor sees 'confirmed', skips rollback │
          └─────────────────────────────────────────────┘
                    │
          ┌─────────▼──────────────────────────────────┐
          │  User does NOT pay                          │
          │  → TTL expires                              │
          │  → RabbitMQ routes to [reservations.expired]│
          │  → Janitor consumes message                 │
          │  → BEGIN DB transaction                     │
          │  → Mark reservation 'expired'               │
          │  → Refund available_tickets in PostgreSQL   │
          │  → COMMIT                                   │
          │  → INCR Redis stock counter                 │
          │  → Ack message                              │
          └─────────────────────────────────────────────┘
```

The Janitor uses manual acknowledgement (`autoAck=false`). On failure it calls `Nack(requeue=true)` — the message is redelivered and retried until it succeeds. **At-least-once processing guaranteed.**

---

## 🛠️ Tech Stack

| Layer | Technology | Purpose |
|---|---|---|
| Language | Go | Concurrent HTTP server, goroutine-based workers |
| Database | PostgreSQL 15 | ACID transactions, inventory, reservations |
| Query Generation | sqlc | Type-safe SQL → Go at compile time, zero ORM overhead |
| Cache | Redis 7 | Atomic stock counter, thundering herd gate |
| Message Broker | RabbitMQ 3 | TTL queue, DLX expiry routing |
| HTTP Router | go-chi/chi | Lightweight routing with middleware support |
| Auth | JWT (HS256) | Stateless authentication, 24-hour tokens |
| Payments | Stripe Checkout | Hosted payment page, webhook confirmation |
| Frontend | Next.js 15 (App Router) | Server actions, TypeScript |
| Containerisation | Docker Compose | Local orchestration of all services |
| Load Testing | Grafana k6 | Stress test scripts |

---

## 🚀 Running Locally

**Prerequisites:** Docker, Go 1.21+

```bash
# 1. Clone the repo
git clone https://github.com/vedanthnyk25/sentinel
cd sentinel

# 2. Copy environment variables
cp .env.example .env
# Fill in your Stripe keys in .env

# 3. Start infrastructure (PostgreSQL, Redis, RabbitMQ)
docker compose up -d

# 4. Wait ~10 seconds for PostgreSQL to initialise
# Verify tables were created
docker exec -it sentinel-postgres psql -U root -d sentinel -c "\dt"

# 5. Run the API
# Redis stock is seeded automatically from PostgreSQL on startup
go run cmd/api/main.go
```

The API runs on `http://localhost:8080`.
The frontend runs on `http://localhost:3000` (`cd sentinel-web && npm install && npm run dev`).
RabbitMQ management UI at `http://localhost:15672` (guest/guest).

### Environment Variables

Copy `.env.example` to `.env` and fill in your values:

```env
DATABASE_URL=postgres://root:secretpassword@localhost:5432/sentinel?sslmode=disable
REDIS_URL=localhost:6379
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
JWT_SECRET=your_jwt_secret_here
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
```

---

## 📡 API Reference

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| POST | `/auth/register` | None | Register a new user |
| POST | `/auth/login` | None | Login — returns JWT token |
| GET | `/events` | None | List all events with inventory |
| GET | `/events/{id}` | None | Single event details |
| POST | `/reserve` | Bearer JWT + Idempotency-Key | Reserve a ticket |
| GET | `/my-reservations` | Bearer JWT | User's reservation history |
| POST | `/checkout` | Bearer JWT | Create Stripe Checkout Session |
| POST | `/webhooks/stripe` | Stripe-Signature | Payment confirmation webhook |

### Key Headers

```
Authorization: Bearer <jwt_token>        # Required on all protected routes
Idempotency-Key: <uuid>                  # Required on POST /reserve
Stripe-Signature: <stripe_sig>           # Sent by Stripe on webhook delivery
```

### Response Codes

| Code | Meaning |
|---|---|
| 201 | Reservation created successfully |
| 409 | Sold out — no tickets remaining |
| 409 | Duplicate request — idempotency key already used |
| 503 | Race condition detected — safe to retry |
| 401 | Missing or invalid JWT token |

---

## 🗄️ Database Schema

```
users                    events                   inventory
─────────────────────    ─────────────────────    ─────────────────────────
id          UUID PK      id          UUID PK      id            UUID PK
email       VARCHAR      name        VARCHAR      event_id      UUID FK (unique)
password_hash VARCHAR    description TEXT         version       INT DEFAULT 0
created_at  TIMESTAMPTZ  date        DATE         available_tickets INT
updated_at  TIMESTAMPTZ  location    VARCHAR        CHECK (>= 0)
                         price       NUMERIC
                         start_time  TIMESTAMPTZ
                         status      ENUM

reservations             idempotency_keys
─────────────────────    ─────────────────────
id          UUID PK      id        UUID PK
user_id     UUID FK      user_id   UUID FK
event_id    UUID FK      key       VARCHAR UNIQUE
status      ENUM         created_at TIMESTAMPTZ
expires_at  TIMESTAMPTZ
created_at  TIMESTAMPTZ
updated_at  TIMESTAMPTZ
```

The `version` column on `inventory` is the foundation of OCC.
The `CHECK (available_tickets >= 0)` constraint is the final safety net — no application bug can oversell past this.

---

## ⚠️ Known Limitations

These are deliberate simplifications for a focused project scope:

**No transactional outbox for RabbitMQ publish.**
If the server crashes after the PostgreSQL commit but before publishing to RabbitMQ, the reservation exists in the database but no expiry message is sent — that ticket stays pending permanently. The correct fix is a transactional outbox pattern (write the pending message inside the same DB transaction, publish asynchronously). Intentionally omitted to avoid a partial implementation of a pattern with its own failure modes.

**Redis is a single point of failure.**
If Redis goes down, all reservation requests fail. A production deployment would use Redis Sentinel or Redis Cluster. On startup, the API seeds Redis stock from PostgreSQL, so a Redis restart is recoverable by restarting the Go server.

**No server-side OCC retry.**
When a race condition is detected (`rowsAffected = 0`), the server returns `503` and delegates retry responsibility to the client. A production system would add a server-side retry loop with exponential backoff.

**Idempotency keys are permanent.**
Keys are never deleted. Over time the table grows unboundedly. A production system would add a TTL column and a background cleanup job, or use Redis with key expiry for idempotency storage.

**No rate limiting.**
The `/auth/login` endpoint has no brute-force protection. A Redis token-bucket rate limiter per IP would be required in production.

---

## 📁 Project Structure

```
sentinel/
├── cmd/
│   └── api/
│       └── main.go              # Entry point — wires all dependencies
├── internal/
│   ├── auth/
│   │   ├── handler.go           # HTTP handlers: /register, /login
│   │   └── service.go           # bcrypt hashing, JWT issuance
│   ├── catalog/
│   │   ├── handler.go           # HTTP handlers: /events, /events/{id}
│   │   └── service.go           # Event listing logic
│   ├── middleware/
│   │   └── auth.go              # JWT validation, injects user_id into context
│   ├── payment/
│   │   ├── handler.go           # HTTP handlers: /checkout, /webhooks/stripe
│   │   └── service.go           # Stripe session creation, webhook verification
│   ├── reservation/
│   │   ├── handler.go           # HTTP handlers: /reserve, /my-reservations
│   │   ├── service.go           # Core reservation logic (all 4 layers)
│   │   └── errors.go            # Typed errors: ErrSoldOut, ErrRaceCond, etc.
│   ├── worker/
│   │   └── janitor.go           # RabbitMQ consumer — expired reservation rollback
│   └── platform/
│       ├── broker/
│       │   └── rabbitmq.go      # DLX topology declaration
│       └── database/
│           ├── *.sql.go         # sqlc-generated type-safe queries
│           ├── models.go        # sqlc-generated struct types
│           └── querier.go       # sqlc-generated interface
├── sql/
│   ├── migrations/
│   │   └── 001_init.sql         # Schema + seed data
│   └── queries/
│       ├── auth.sql             # User queries
│       ├── catalog.sql          # Event queries
│       ├── engine.sql           # Core reservation queries (OCC, idempotency)
│       ├── payment.sql          # Payment status queries
│       └── worker.sql           # Janitor queries
├── sentinel-web/                # Next.js 15 frontend
├── docker-compose.yml           # PostgreSQL, Redis, RabbitMQ
├── sqlc.yaml                    # sqlc configuration
└── benchmark.js                 # k6 load test scripts
```
