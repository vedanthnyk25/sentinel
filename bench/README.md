# Sentinel benchmark suite

Replaces `benchmark_baseline.js` / `benchmark_sellout.js` / `benchmark_stress.js` /
`benchmarks/*.js` / `run_all.sh` / `test.sh` with one canonical, runnable suite.
Delete or archive the old ones — having two copies of the same test with
different parameters is the reason "2400 rps" currently isn't reproducible.

## Requirements
- k6 (`brew install k6` / see k6.io)
- `psql` and `redis-cli` on PATH (for the Postgres correctness check and drain wait)
- API, Postgres, Redis, RabbitMQ all running locally
- `.env` in the repo root with `POSTGRES_CONNECTION_STRING` set

## Usage

```bash
# capture a baseline before your change
git checkout <before-commit-or-branch>
./bench/run.sh before

# apply your change, restart the server, then
git checkout <after-commit-or-branch>
./bench/run.sh after

# diff them
python3 bench/compare.py before after
```

Each run does three things, in order:
1. **Correctness test** — 2000 req/s against 100 tickets for 10s. Verifies
   no oversell over HTTP, then *waits for the async worker to drain* and
   re-verifies the actual row count in Postgres — not just the HTTP status
   codes k6 saw. This is the check your current suite is missing: since the
   design is eventually consistent (Redis decides, Postgres catches up),
   "no oversell" has to be checked against the source of truth, not inferred.
2. **Throughput test** — ramps 200 -> 5000 req/s against effectively
   unlimited inventory, isolating raw request-handling capacity from
   sold-out noise.
3. **Consolidated summary** — `results/summary_<label>.json`, tagged with
   the git commit hash, so a number can always be traced back to the exact
   code that produced it.

## Why `dropped_iterations` matters
Every k6 script here fails its threshold if `dropped_iterations > 0`. This
metric means k6 itself couldn't spawn enough VUs to hit the requested rate —
i.e. **the load generator was the bottleneck, not your server**. On a single
laptop running k6 + Go + Postgres + Redis + RabbitMQ simultaneously, this is
a real risk. If you see this warning, the RPS number in that run understates
(or just doesn't reflect) your server's actual ceiling — rerun with a lower
target rate, or run k6 with `--compatibility-mode` on a separate core budget,
or note it explicitly rather than citing the number as-is.

## Resource monitoring (run alongside, not automated here)
While a run is in progress, in another terminal:
```bash
htop                                              # CPU split across k6/go/postgres/redis
watch -n1 'redis-cli info clients | head -5'      # redis connection/command load
watch -n1 "psql \$POSTGRES_CONNECTION_STRING -c 'select count(*) from pg_stat_activity'"
```
If you want to cite a specific bottleneck explanation (e.g. "removing the
synchronous Postgres transaction from the hot path is what fixed the p95"),
capture one screenshot/log of this during a `before` run and one during an
`after` run — that's the evidence that makes the interview story defensible
beyond "the number went up."
