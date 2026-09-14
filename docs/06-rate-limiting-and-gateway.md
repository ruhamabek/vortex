# Rate Limiting and Edge Gateway

This module explains our two-tiered defense-in-depth security model: Caddy reverse proxy at the perimeter and Redis atomic sliding window rate limiting in the application.

---

## Defense-in-Depth Architecture

```
[ External Request ]
        │
        ▼
┌───────────────────────────────┐
│     Tier 1: Caddy Gateway     │
│  - Blocks IP volumetric spam  │ ◄── Drops unauthenticated floods before Go wakes up
│  - Enforces 500MB max payload │
└──────────────┬────────────────┘
               │ (Passed)
               ▼
┌───────────────────────────────┐
│     Tier 2: Go + Redis        │
│  - Tracks User ID quota       │ ◄── "Max 5 uploads/min per authenticated account"
│  - Sliding Window (ZSET)      │
└──────────────┬────────────────┘
               │ (Passed)
               ▼
       [ Business Logic ]
```

---

## Tier 1: Caddy Edge Gateway

Caddy runs as the entrypoint deployment in Kubernetes (caddy-gateway). It uses a declarative ConfigMap (Caddyfile) mounted as a read-only volume:

- Unbuffered Uploads: Allows client streams to flow straight through to Go without being cached on disk first.
- Max Payload Size: request_body { max_size 500MB } prevents disk-filling denial of service.
- Protocol Routing: Routes HTTP/1.1 and WebSockets to port 8080, while proxying application/grpc calls using HTTP/2 Cleartext (h2c://vortex-grpc:50055).

---

## Tier 2: Redis Sliding Window Rate Limiter

Fixed window counters (e.g. 5 requests per calendar minute) suffer from the "boundary burst" flaw: an attacker can send 5 requests at 12:00:59 and 5 requests at 12:01:01, executing 10 operations in 2 seconds.

Vortex implements an atomic Sliding Window Log using Redis Sorted Sets (ZSET):

### Atomic Pipeline (pkg/middleware/ratelimit.go)
When a request arrives:
1. Identify the caller using X-User-ID (fallback to IP address if unauthenticated).
2. Calculate current time: now = time.Now().UnixMilli().
3. Execute atomic Redis transaction pipeline:
   - ZREMRANGEBYSCORE key 0 (now - windowMs): Evict timestamps older than 60 seconds.
   - ZCARD key: Count remaining requests within the sliding window.
4. If count >= limit:
   - Return HTTP 429 Too Many Requests.
   - Send Retry-After: 60 and X-RateLimit-Remaining: 0.
5. If count < limit:
   - ZADD key now now:nanosecond: Record the new request.
   - EXPIRE key (window * 2): Refresh key time-to-live.
   - Forward request to handler with decremented X-RateLimit-Remaining.

### Internal Operational Exemption:
To prevent Kubernetes liveness/readiness probes or Prometheus scrapers from ever being throttled, /healthz and /metrics paths automatically bypass rate limiting.
