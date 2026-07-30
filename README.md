# Charon

A reverse proxy and load balancer built from scratch in Go.

Charon sits in front of your upstream servers, routes traffic intelligently, and keeps requests moving when things go wrong — without any manual intervention. It's built to be understood completely, from the raw TCP connection up.

---

## Goals

- Handle real HTTP and HTTPS traffic across a pool of upstream servers
- Route using least connections so load distributes naturally, not mechanically
- Detect and recover from upstream failures automatically
- Fail fast when an upstream is down rather than letting requests pile up
- Rate limit at the edge before bad traffic reaches your services
- Give full visibility into what's happening — logs, metrics, and a live dashboard
- Accept configuration changes without restarting

---

## What's built

### Phase 1 — TCP Proxy & HTTP Parsing ✓

Charon accepts raw TCP connections, parses HTTP/1.1 requests at the wire level (method, path, version, headers, body), and forwards them to an upstream server. The response is parsed and streamed back to the client.

- TCP listener on a configurable port
- HTTP request and response parser built on `bufio` — no standard library HTTP handling
- `X-Forwarded-For` header injection with the real client IP
- Full request logging: method, path, status code, latency

### Phase 2 — Least Connections Load Balancing ✓

Traffic is distributed across a pool of upstream servers using the least connections algorithm — requests go to whichever upstream has the fewest in-flight connections at that moment. Under skewed load, slow servers naturally receive fewer new requests without any explicit configuration.

- Thread-safe active connection counter per upstream using `sync/atomic`
- `sync.RWMutex` on the pool slice — multiple goroutines can select simultaneously, writes are exclusive
- Round-robin tiebreaker when multiple upstreams are tied on connection count
- `defer upstream.Release()` ensures the counter always decrements regardless of how the handler exits

### Phase 3 — Health Checking & Automatic Failover ✓

Unhealthy upstreams are detected and removed from rotation automatically. Recovery is detected and they're restored.

**Active health checks** — a background goroutine sends `GET /health` to each upstream on a configurable interval. Results go through a threshold: 3 consecutive failures to mark unhealthy, 2 consecutive successes to restore. This hysteresis prevents flapping.

**Exponential backoff with jitter** — when an upstream fails, the check interval doubles each time (1s → 2s → 4s → ... → 64s cap). Random jitter is added to each delay so multiple proxies in a fleet don't retry in lockstep and cause a thundering herd against a recovering server.

**Passive health checks** — real traffic is also monitored. 5xx responses and connection errors on actual requests update the same health counters, so an upstream can be marked unhealthy from real traffic without waiting for a synthetic check.

**Per-upstream mutex** — all health state (counters, healthy flag, check interval) is protected by a mutex on each `Upstream` struct. Goroutines for different upstreams don't contend with each other.

---

## Upcoming

| Phase | What |
|-------|------|
| 4 | Connection pooling — reuse TCP connections to upstreams, stale connection detection |

---

## Running

```bash
# start proxy + 3 backends
docker compose up

# configure via environment variables
PORT=80                              # proxy listen port
UPSTREAM_ADDRS=host:port,host:port   # comma-separated upstream addresses
HC_INTERVAL=15s                      # health check interval
```

---

*Built as a deep-dive into how reverse proxies actually work.*
