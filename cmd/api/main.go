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
	"github.com/ruhamabek/vortex/pkg/logger"
	"github.com/ruhamabek/vortex/pkg/middleware"
	"github.com/ruhamabek/vortex/pkg/telemetry"
)

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func main() {
	env := getEnv("ENVIRONMENT", "development")
	log, err := logger.Init(env)
	if err != nil {
		fmt.Printf("failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()
     
	ctx := context.Background()
	tempoEndpoint := getEnv("TEMPO_ENDPOINT", "localhost:4317")
	tp, err := telemetry.InitTracer(ctx, "vortex-api", tempoEndpoint)
	if err != nil {
		log.Warn("failed to initialize tracer", zap.Error(err))
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = tp.Shutdown(shutdownCtx)
		}()
		log.Info("opentelemetry tracer initialized", zap.String("tempo", tempoEndpoint))
	}

	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "postgres://vortex:vortex_secret_password@localhost:5432/vortex_db?sslmode=disable")
	minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	minioAccessKey := getEnv("MINIO_ACCESS_KEY", "minioadmin")
	minioSecretKey := getEnv("MINIO_SECRET_KEY", "minioadminpassword")
	minioBucket := getEnv("MINIO_BUCKET", "raw-videos")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
   	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. PostgreSQL 
	log.Info("connecting to postgres...", zap.String("url", dbURL))
	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal("unable to connect to database", zap.Error(err))
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctx); err != nil {
		log.Fatal("failed to ping database", zap.Error(err))
	}
	log.Info("connected to postgres successfully")

	// 2. MinIO 
	log.Info("connecting to minio...", zap.String("endpoint", minioEndpoint))
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatal("failed to initialize minio client", zap.Error(err))
	}

	exists, err := minioClient.BucketExists(ctx, minioBucket)
	if err != nil {
		log.Fatal("failed to check minio bucket", zap.Error(err))
	}
	if !exists {
		if err := minioClient.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatal("failed to create minio bucket", zap.Error(err))
		}
	}
	log.Info("connected to minio successfully", zap.String("bucket", minioBucket))

	// 3. NATS JetStream  
	log.Info("connecting to nats...", zap.String("url", natsURL))
	nc, err := natsgo.Connect(natsURL)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal("failed to initialize jetstream context", zap.Error(err))
	}

	// Ensure JetStream Stream "VORTEX" exists
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "VORTEX",
		Subjects:  []string{"videos.>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.InterestPolicy,
	})
	if err != nil {
		log.Fatal("failed to initialize jetstream stream", zap.Error(err))
	}
	log.Info("connected to nats jetstream successfully")
    
	//redis setup
	log.Info("connecting to redis...", zap.String("addr", redisAddr))
	redisClient := goredis.NewClient(&goredis.Options{
		Addr: redisAddr,
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatal("failed to ping redis", zap.Error(err))
	}
	log.Info("connected to redis successfully")

	// 4. Instantiate Repositories & Services
	videoRepo := pgsvc.NewVideoRepository(dbPool)
	objectStorage := miniosvc.NewMinIOStorage(minioClient, minioBucket)
	eventPublisher := natssvc.NewNATSEventPublisher(js)

	videoService := service.NewVideoService(videoRepo, objectStorage, eventPublisher)
	videoHandler := v1.NewVideoHandler(videoService)

	progressTracker := redissvc.NewRedisProgressTracker(redisClient, 24*time.Hour)
	wsHandler := v1.NewWSHandler(progressTracker)
    
	// 4b. Inngest Durable Workflow Client
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

	// 5. Setup HTTP Mux & Observability Routes
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

	// Wrap Mux with Middleware Chain: Trace -> CorrelationID -> Logging -> ServeMux
	handlerWithMiddleware := telemetry.HTTPTraceMiddleware("vortex-api")(
		middleware.CORS(middleware.CorrelationID(middleware.Logging(mux))),
	)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handlerWithMiddleware,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 6. Start HTTP Server in background goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("vortex api server listening", zap.String("port", port))
		serverErrors <- server.ListenAndServe()
	}()

	// 7. Graceful Shutdown listener
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