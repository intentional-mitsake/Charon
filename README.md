# Charon

A reverse proxy and load balancer built from scratch in Go.

Charon sits in front of your upstream servers, routes traffic intelligently, and keeps requests moving when things go wrong — without any manual intervention. Built to be understood completely, from the raw TCP connection up.

---

## Goals

- Handle real HTTP and HTTPS traffic across a pool of upstream servers
- Distribute load using least connections so busy servers naturally get fewer requests
- Detect and recover from upstream failures automatically
- Fail fast when an upstream is down rather than letting requests pile up
- Rate limit at the edge before bad traffic reaches your services
- Give full visibility into what's happening through logs, metrics, and a live dashboard
- Accept config changes without restarting

---

## How it works

### TCP & HTTP

Charon accepts raw TCP connections and parses HTTP/1.1 requests at the wire level — method, path, version, headers, body — with no standard library HTTP handling. Responses are parsed and streamed back to the client the same way. Every request gets `X-Forwarded-For` injected with the real client IP, and everything is logged: method, path, status, latency.

### Load balancing

Requests go to whichever upstream has the fewest in-flight connections at that moment. Under skewed load, slow servers naturally attract fewer new requests without any explicit tuning. Ties are broken by round-robin.

The pool is protected by a `sync.RWMutex` so multiple goroutines can select an upstream simultaneously — writes (adding or removing a server) are exclusive, reads aren't. Each upstream tracks its active connection count with `sync/atomic`, and a `defer upstream.Release()` ensures the counter always decrements no matter how the handler exits.

### Health checking & failover

Upstreams are monitored two ways.

**Active checks** run on a background goroutine — a `GET /health` to each upstream on a configurable interval. There's hysteresis built in: 3 consecutive failures to mark an upstream unhealthy, 2 consecutive successes to restore it. This prevents a briefly-flaky server from being yanked out and put back in rotation repeatedly.

When an upstream fails, the check interval backs off exponentially (1s → 2s → 4s → ... capped at 64s), with random jitter added to each delay. This matters in a fleet: without jitter, multiple proxy instances all retry at the same moment and can hammer a recovering server all at once.

**Passive checks** run on real traffic. 5xx responses and connection errors update the same health counters as active checks, so a degraded upstream can be pulled from rotation immediately — no waiting for the next scheduled probe.

All health state per upstream (counters, healthy flag, check interval) lives behind its own mutex, so goroutines managing different upstreams don't contend with each other.

---

## What's next

Connection pooling — reuse TCP connections to upstreams rather than opening a new one per request, with stale connection detection.

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
