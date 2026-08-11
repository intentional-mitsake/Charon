# Charon

A reverse proxy and load balancer built from scratch in Go.

Charon sits in front of your upstream servers, routes traffic intelligently, and keeps requests moving when things go wrong — without any manual intervention. Built to be understood completely, from the raw TCP connection up.

---

## What it does

Charon sits in front of a pool of upstream servers and handles everything a production proxy needs:

- Accepts raw TCP connections and parses HTTP/1.1 at the wire level
- Routes traffic using **least connections** so slow servers naturally receive fewer requests
- Maintains a **connection pool** per upstream, reusing TCP connections across requests
- Monitors upstream health with **active and passive health checks**, removing dead servers automatically
- Applies **exponential backoff with jitter** when retrying unhealthy upstreams
- Enforces **per-IP rate limiting** using a token bucket algorithm
- Trips a **per-upstream circuit breaker** that fails fast on known-bad upstreams without waiting for a dial
- Injects `X-Forwarded-For` headers and logs structured request telemetry

---

## Architecture

```
client
  │
  ▼
TCP listener (port 80)
  │
  ├─ Rate limiter (per-IP token bucket)
  │
  ├─ Request parser (method, path, headers, body)
  │
  ├─ Upstream selector (least connections + round-robin tiebreak)
  │
  ├─ Circuit breaker (CLOSED / OPEN / HALF_OPEN per upstream)
  │
  ├─ Connection pool (reuse idle TCP connections)
  │
  ├─ Request forwarder → upstream server
  │
  └─ Response parser → client
       │
       └─ Passive health update (5xx → mark upstream degraded)

Background:
  └─ Health checker (active GET /health, per-upstream backoff)
```

---

## What's done

### 1 — TCP Proxy & HTTP Parsing
- TCP listener on a configurable port
- HTTP/1.1 parser from raw bytes: method, path, version, headers, body via `Content-Length`
- `X-Forwarded-For` injection with real client IP
- Structured request logging: method, path, status, latency

### 2 — Least Connections Load Balancing
- Thread-safe active connection counter per upstream using `sync/atomic`
- Least connections selection with `sync.RWMutex` on the pool slice
- Round-robin tiebreaker when multiple upstreams are tied
- `defer upstream.Release()` guarantees counter always decrements

### 3 — Health Checking & Automatic Failover
- **Active checks**: background goroutine sends `GET /health` on a configurable interval
- **Passive checks**: 5xx responses and connection errors on real traffic update the same health counters
- Hysteresis: 3 consecutive failures to mark unhealthy, 2 consecutive passes to restore
- Exponential backoff with jitter on failed upstreams (1s → 2s → 4s → 64s cap)
- Per-upstream mutex prevents goroutines for different upstreams from contending

### 4 — Connection Pooling
- `chan net.Conn` per upstream as a lock-free idle connection pool
- Non-blocking `select` on both pull and push — never blocks the request path
- `Connection: keep-alive` header check before returning connections to pool
- Error paths explicitly close connections — broken connections never re-enter the pool

### 5 — Rate Limiting
- Token bucket algorithm: configurable capacity and fill rate
- Per-IP buckets stored in `sync.Map` — optimised for read-heavy workloads
- Lazy refill on each request — no background goroutine needed
- Denied connections receive `429 Too Many Requests` with `Retry-After` header and are closed immediately

### 6 — Circuit Breaker
- Per-upstream state machine with three states: **CLOSED** (normal), **OPEN** (fail fast), **HALF_OPEN** (probe)
- Runs inline in `HandleConnection` on every request — not a background process
- CLOSED → OPEN after N consecutive real-traffic failures; returns `503` immediately without dialing
- OPEN → HALF_OPEN after a cooldown timer expires
- HALF_OPEN lets exactly one probe request through using `atomic.Bool` with `CompareAndSwap` — concurrent requests are blocked until the probe resolves
- Probe success → CLOSED immediately (no threshold, `BeingChecked` already enforces one probe)
- Probe failure → OPEN with reset timer
- Separate failure counters from health check counters — health checks and the circuit breaker track different signals and must not interfere

---

## Proving it works

**Least connections under skewed load**

Added `DELAY_REQ=300ms` to backend-3, then ran `hey -n 200 -c 20`:

| Backend | Run 1 | Run 2 |
|---------|-------|-------|
| backend-1 (fast) | 78 | 67 |
| backend-2 (fast) | 78 | 67 |
| backend-3 (+300ms) | 7 | 2 |

The slow backend received ~5% of traffic instead of the 33% it would get under round-robin. No configuration change — the algorithm adapted automatically to real load.

**Connection pooling**

Under concurrent load (`hey -c 20`), pool hits appear in logs alongside dials — connections returned from one request are reused by the next wave. Under sequential browser traffic the pool stays empty because requests don't overlap.

**Health check recovery**

Killed backend-2 mid-run. After 3 consecutive active check failures, it was removed from rotation. Restarted it. After 2 consecutive passes it was restored. All other traffic continued uninterrupted throughout.

**Circuit breaker**

Killed backend-2 mid-run. After the failure threshold was crossed on live traffic, the breaker opened — subsequent requests to that upstream returned `503` immediately without dialing. After the cooldown, one probe request was let through; on recovery it closed the breaker and normal traffic resumed.

---

**Environment variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `80` | Proxy listen port |
| `UPSTREAM_ADDRS` | `127.0.0.1:8848,...` | Comma-separated upstream addresses |
| `HC_INTERVAL` | `15s` | Health check interval |

---

## Key implementation decisions

**Why `chan net.Conn` for the pool**
Buffered channels are inherently thread-safe — no mutex needed for pool operations. Non-blocking `select` with `default` handles both empty pool (dial fresh) and full pool (discard) without ever blocking the request goroutine.

**Why token bucket over alternatives**
Fixed window counters allow 2x burst at window boundaries. Leaky bucket queues excess requests, growing memory under attack. Sliding window logs store every timestamp. Token bucket allows legitimate bursts up to capacity, rejects excess immediately, and is O(1) per request with constant memory.

**Why least connections over round-robin**
Round-robin distributes by count, not by load. A slow upstream accumulates connections at the same rate as fast ones. Least connections naturally routes around slow servers — the slow server's connection count grows, making it less likely to be selected. Self-tuning, no configuration required.

**Why `sync/atomic` for `ActiveConns` but mutex for health state**
`ActiveConns` is a single integer updated on every request — atomic is a single CPU instruction with no scheduler involvement. Health state involves multiple fields (`Healthy`, `SuccessiveFails`, `SuccessivePasses`, `CheckInterval`) that must change together as one unit — mutex is the correct primitive.

**Why the circuit breaker is separate from health checking**
`Healthy=false` excludes an upstream from `SelectUpstream` entirely — it won't be chosen at all. The circuit breaker operates on upstreams that *are* selected but may be mid-failure. Health checks are slow and deliberate (timer-driven, debounced with hysteresis). The circuit breaker is fast and per-request (real traffic, trips immediately). Both are needed.

**Why `CompareAndSwap` for HALF_OPEN probing**
Plain `if u.CBState == HALF_OPEN` doesn't work — multiple goroutines can all read the state simultaneously and all get through. `atomic.Bool.CompareAndSwap(false, true)` is a single CPU instruction; only the one goroutine that wins the swap gets to probe. All others return `503` until the probe resolves and `BeingChecked` is cleared.

---

## Running

```bash
# start proxy + 3 backends
docker compose up
```

```bash
PORT=80                              # proxy listen port
UPSTREAM_ADDRS=host:port,host:port   # comma-separated upstream addresses
HC_INTERVAL=15s                      # health check interval
```

---

*Built as a deep-dive into how reverse proxies actually work.*
