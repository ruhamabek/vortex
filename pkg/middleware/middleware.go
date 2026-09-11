package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/ruhamabek/vortex/pkg/logger"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vortex_http_requests_total",
			Help: "Total number of HTTP requests handled",
		},
		[]string{"method", "path", "status"},
	)
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vortex_http_request_duration_seconds",
			Help:    "HTTP request latency distributions in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)
)

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}


func(rw *responseWriterWrapper) WriteHeader(code int){
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		 cid := r.Header.Get("X-Correlation-ID")
		 if cid == "" {
			b := make([]byte, 8)
			_,_ = rand.Read(b)
			cid = fmt.Sprintf("%x", b)
		 }

		 ctx := context.WithValue(r.Context(), logger.CorrelationIDKey, cid)
		 w.Header().Set("X-Correlation-ID", cid)
		 next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		   start := time.Now()
		   ww := &responseWriterWrapper{ResponseWriter: w, statusCode:http.StatusOK}
		   next.ServeHTTP(ww, r)

		   duration := time.Since(start).Seconds()
		   statusStr := strconv.Itoa(ww.statusCode)

		   httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, statusStr).Inc()
		   httpRequestDuration.WithLabelValues(r.Method, r.URL.Path, statusStr).Observe(duration)
	})
}