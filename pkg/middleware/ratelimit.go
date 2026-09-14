package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisRateLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
}

// NewRedisRateLimiter initializes a Sliding Window rate limiter backed by Redis.
func NewRedisRateLimiter(client *redis.Client, limit int64, window time.Duration) *RedisRateLimiter {
	return &RedisRateLimiter{
		client: client,
		limit:  limit,
		window: window,
	}
}

// RateLimit wraps an http.Handler with sliding window rate limiting.
func (rl *RedisRateLimiter) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		// User ID takes precedence, fallback to IP address
		identifier := r.Header.Get("X-User-ID")
		if identifier == "" {
			identifier = extractIP(r)
		}

		key := fmt.Sprintf("vortex:ratelimit:%s", identifier)
		now := time.Now()
		nowMs := now.UnixMilli()
		windowMs := rl.window.Milliseconds()
		clearBeforeMs := nowMs - windowMs

		ctx := r.Context()

		// Clean expired entries and count current requests
		pipe := rl.client.TxPipeline()
		pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(clearBeforeMs, 10))
		countCmd := pipe.ZCard(ctx, key)
		_, err := pipe.Exec(ctx)
		if err != nil && err != redis.Nil {
			// if Redis has a transient issue, don't break user traffic
			next.ServeHTTP(w, r)
			return
		}

		currentCount := countCmd.Val()

		// Set standard RFC rate-limit headers
		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(rl.limit, 10))

		// Check if threshold is breached
		if currentCount >= rl.limit {
			retryAfter := int(rl.window.Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded, please retry later"}`))
			return
		}

 		pipe = rl.client.TxPipeline()
		pipe.ZAdd(ctx, key, redis.Z{
			Score:  float64(nowMs),
			Member: fmt.Sprintf("%d:%d", nowMs, now.Nanosecond()),
		})
		pipe.Expire(ctx, key, rl.window*2)
		_, _ = pipe.Exec(ctx)

		remaining := rl.limit - (currentCount + 1)
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

		next.ServeHTTP(w, r)
	})
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}