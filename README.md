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
 
*Built as a deep-dive into how reverse proxies actually work.*
 
