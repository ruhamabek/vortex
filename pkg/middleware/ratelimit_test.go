package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ruhamabek/vortex/pkg/middleware"
)

func setupTestRedis(t *testing.T) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to connect to test redis: %v", err)
	}

	t.Cleanup(func() {
		client.FlushDB(context.Background())
		client.Close()
	})

	return client
}

func TestRateLimiter_SlidingWindow(t *testing.T) {
	client := setupTestRedis(t)

	// Allow max 3 requests per 2-second window
	limit := int64(3)
	window := 2 * time.Second
	limiter := middleware.NewRedisRateLimiter(client, limit, window)

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := limiter.RateLimit(dummyHandler)

	// 1. First 3 requests for user-alpha should SUCCEED
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest("POST", "/upload", nil)
		req.Header.Set("X-User-ID", "user-alpha")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected HTTP 200, got %d", i, rec.Code)
		}
	}

	// 2. 4th request for user-alpha should be BLOCKED with HTTP 429
	req4 := httptest.NewRequest("POST", "/upload", nil)
	req4.Header.Set("X-User-ID", "user-alpha")
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)

	if rec4.Code != http.StatusTooManyRequests {
		t.Fatalf("expected HTTP 429 Too Many Requests, got %d", rec4.Code)
	}

	if rec4.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429 response")
	}

	// 3. Independent user: user-beta should not be affected by user-alpha's limit
	reqBeta := httptest.NewRequest("POST", "/upload", nil)
	reqBeta.Header.Set("X-User-ID", "user-beta")
	recBeta := httptest.NewRecorder()
	handler.ServeHTTP(recBeta, reqBeta)

	if recBeta.Code != http.StatusOK {
		t.Fatalf("expected user-beta to succeed with HTTP 200, got %d", recBeta.Code)
	}
}