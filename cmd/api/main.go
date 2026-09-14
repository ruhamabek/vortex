package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/inngest/inngestgo"
	goredis "github.com/redis/go-redis/v9"
	v1 "github.com/ruhamabek/vortex/internal/api/v1"
	natssvc "github.com/ruhamabek/vortex/internal/event/nats"
	"github.com/ruhamabek/vortex/internal/service"
	miniosvc "github.com/ruhamabek/vortex/internal/storage/minio"
	pgsvc "github.com/ruhamabek/vortex/internal/storage/postgres"
	redissvc "github.com/ruhamabek/vortex/internal/storage/redis"
	"github.com/ruhamabek/vortex/internal/workflow"
	"github.com/ruhamabek/vortex/pkg/config"
	"github.com/ruhamabek/vortex/pkg/logger"
	"github.com/ruhamabek/vortex/pkg/middleware"
	"github.com/ruhamabek/vortex/pkg/telemetry"
)

func main() {
 	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize Structured Logger
	log, err := logger.Init(cfg.Environment)
	if err != nil {
		fmt.Printf("failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	// 3. Initialize OpenTelemetry Tracer
	ctx := context.Background()
	tp, err := telemetry.InitTracer(ctx, "vortex-api", cfg.TempoEndpoint)
	if err != nil {
		log.Warn("failed to initialize tracer", zap.Error(err))
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = tp.Shutdown(shutdownCtx)
		}()
		log.Info("opentelemetry tracer initialized", zap.String("tempo", cfg.TempoEndpoint))
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 4. Connect to PostgreSQL
	log.Info("connecting to postgres...", zap.String("url", cfg.DatabaseURL))
	dbPool, err := pgxpool.New(ctxTimeout, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("unable to connect to database", zap.Error(err))
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctxTimeout); err != nil {
		log.Fatal("failed to ping database", zap.Error(err))
	}
	log.Info("connected to postgres successfully")

	// 5. Connect to MinIO Object Storage
	log.Info("connecting to minio...", zap.String("endpoint", cfg.MinIO.Endpoint))
	minioClient, err := minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})
	if err != nil {
		log.Fatal("failed to initialize minio client", zap.Error(err))
	}

	exists, err := minioClient.BucketExists(ctxTimeout, cfg.MinIO.Bucket)
	if err != nil {
		log.Fatal("failed to check minio bucket", zap.Error(err))
	}
	if !exists {
		if err := minioClient.MakeBucket(ctxTimeout, cfg.MinIO.Bucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatal("failed to create minio bucket", zap.Error(err))
		}
	}
	log.Info("connected to minio successfully", zap.String("bucket", cfg.MinIO.Bucket))

	// 6. Connect to NATS JetStream
	log.Info("connecting to nats...", zap.String("url", cfg.NATSURL))
	nc, err := natsgo.Connect(cfg.NATSURL)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal("failed to initialize jetstream context", zap.Error(err))
	}

	// Ensure JetStream Stream "VORTEX" exists
	_, err = js.CreateOrUpdateStream(ctxTimeout, jetstream.StreamConfig{
		Name:      "VORTEX",
		Subjects:  []string{"videos.>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.InterestPolicy,
	})
	if err != nil {
		log.Fatal("failed to initialize jetstream stream", zap.Error(err))
	}
	log.Info("connected to nats jetstream successfully")

	// 7. Connect to Redis
	log.Info("connecting to redis...", zap.String("addr", cfg.RedisAddr))
	redisClient := goredis.NewClient(&goredis.Options{
		Addr: cfg.RedisAddr,
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctxTimeout).Err(); err != nil {
		log.Fatal("failed to ping redis", zap.Error(err))
	}
	log.Info("connected to redis successfully")

	// 8. Rate Limiter: Max 5 requests per 1-minute sliding window per User/IP
	rateLimiter := middleware.NewRedisRateLimiter(redisClient, 5, 1*time.Minute)

	// 9. Instantiate Repositories & Application Services
	videoRepo := pgsvc.NewVideoRepository(dbPool)
	objectStorage := miniosvc.NewMinIOStorage(minioClient, cfg.MinIO.Bucket)
	eventPublisher := natssvc.NewNATSEventPublisher(js)

	videoService := service.NewVideoService(videoRepo, objectStorage, eventPublisher)
	videoHandler := v1.NewVideoHandler(videoService)

	progressTracker := redissvc.NewRedisProgressTracker(redisClient, 24*time.Hour)
	wsHandler := v1.NewWSHandler(progressTracker)

	// 10. Inngest Durable Workflow Client
	inngestClient, err := inngestgo.NewClient(inngestgo.ClientOpts{
		AppID: "vortex",
		Dev:   new(true),
	})
	if err != nil {
		log.Fatal("failed to initialize inngest client", zap.Error(err))
	}

	_, err = workflow.NewPostProcessingFunction(inngestClient, objectStorage, nil)
	if err != nil {
		log.Fatal("failed to register inngest workflow function", zap.Error(err))
	}
	log.Info("inngest workflow registered successfully")

	// 11. Setup HTTP Mux & Observability Routes
	mux := http.NewServeMux()

	// Inngest Endpoint
	mux.Handle("/api/inngest", inngestClient.Serve())

	// OpenAPI Spec & Swagger UI
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "api/openapi.yaml")
	})

	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Vortex API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => {
      SwaggerUIBundle({
        url: '/openapi.yaml',
        dom_id: '#swagger-ui',
      });
    };
  </script>
</body>
</html>`
		_, _ = w.Write([]byte(html))
	})

	// Health Check & Prometheus Metrics
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	// Application Routes
	videoHandler.RegisterRoutes(mux)
	wsHandler.RegisterRoutes(mux)

	// Wrap Mux with Middleware Chain: Trace -> CorrelationID -> Logging -> RateLimiter -> ServeMux
	handlerWithMiddleware := telemetry.HTTPTraceMiddleware("vortex-api")(
		middleware.CORS(middleware.CorrelationID(middleware.Logging(rateLimiter.RateLimit(mux)))),
	)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handlerWithMiddleware,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 12. Start HTTP Server in background goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("vortex api server listening", zap.String("port", cfg.Port))
		serverErrors <- server.ListenAndServe()
	}()

	// 13. Graceful Shutdown listener
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("server error", zap.Error(err))
		}
	case sig := <-shutdown:
		log.Info("shutdown signal received, stopping gracefully...", zap.String("signal", sig.String()))

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown failed, forcing close", zap.Error(err))
			_ = server.Close()
		}
		log.Info("server shutdown complete")
	}
}