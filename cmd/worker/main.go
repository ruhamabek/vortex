package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	goredis "github.com/redis/go-redis/v9"
	"github.com/ruhamabek/vortex/internal/domain"
	miniosvc "github.com/ruhamabek/vortex/internal/storage/minio"
	pgsvc "github.com/ruhamabek/vortex/internal/storage/postgres"
	redissvc "github.com/ruhamabek/vortex/internal/storage/redis"
	"github.com/ruhamabek/vortex/internal/transcoder"
	"github.com/ruhamabek/vortex/internal/worker"
	"github.com/ruhamabek/vortex/pkg/logger"
	"go.uber.org/zap"
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

	dbURL := getEnv("DATABASE_URL", "postgres://vortex:vortex_secret_password@localhost:5432/vortex_db?sslmode=disable")
	minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	minioAccessKey := getEnv("MINIO_ACCESS_KEY", "minioadmin")
	minioSecretKey := getEnv("MINIO_SECRET_KEY", "minioadminpassword")
	minioBucket := getEnv("MINIO_BUCKET", "raw-videos")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// 1. PostgreSQL Connection
	log.Info("connecting to postgres...", zap.String("url", dbURL))
	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer dbPool.Close()
	if err := dbPool.Ping(ctx); err != nil {
		log.Fatal("failed to ping postgres", zap.Error(err))
	}

	// 2. MinIO S3 Connection
	log.Info("connecting to minio...", zap.String("endpoint", minioEndpoint))
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatal("failed to initialize minio client", zap.Error(err))
	}

	// 3. Redis Connection
	log.Info("connecting to redis...", zap.String("addr", redisAddr))
	redisClient := goredis.NewClient(&goredis.Options{
		Addr: redisAddr,
	})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatal("failed to ping redis", zap.Error(err))
	}

	// 4. NATS JetStream Connection
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
    
	// check JetStream Stream "VORTEX" exists
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      "VORTEX",
		Subjects:  []string{"videos.>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.InterestPolicy,
	})
	if err != nil {
		log.Fatal("failed to initialize jetstream stream", zap.Error(err))
	}

	// 5. Create / Join Durable JetStream Consumer
	consumer, err := js.CreateOrUpdateConsumer(ctx, "VORTEX", jetstream.ConsumerConfig{
		Durable:       "transcoder-worker",
		FilterSubject: "videos.uploaded",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    3,
		AckWait:       30 * time.Minute, 
	})
    if err != nil {
		log.Fatal("failed to create durable jetstream consumer", zap.Error(err))
	}

	// 6. Instantiate Dependencies & Transcoder Worker
	videoRepo := pgsvc.NewVideoRepository(dbPool)
	objectStorage := miniosvc.NewMinIOStorage(minioClient, minioBucket)
	progressTracker := redissvc.NewRedisProgressTracker(redisClient, 24*time.Hour)
	ffmpegEngine := transcoder.NewFFmpegTranscoder()
	transcoderWorker := worker.NewTranscoderWorker(videoRepo, objectStorage, progressTracker, ffmpegEngine)

	inngestClient, err := inngestgo.NewClient(inngestgo.ClientOpts{
		AppID: "vortex",
		Dev:   inngestgo.BoolPtr(true), 
	})
	if err != nil {
		log.Fatal("failed to initialize inngest client in worker", zap.Error(err))
	}

	//7. start consuming NATS messages
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		var event domain.VideoUploadedEvent
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			log.Error("failed to unmarshal video uploaded event", zap.Error(err))
			_=msg.Term()
			return
		}
        
		log.Info("received video transcoding job",
			zap.String("video_id", event.VideoID),
			zap.String("user_id", event.UserID),
			zap.String("filename", event.OriginalFileName),
		)

		jobCtx, jobCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer jobCancel()

		if err := transcoderWorker.ProcessVideo(jobCtx, event); err != nil {
			log.Error("transcoding job failed",
			zap.String("video_id", event.VideoID),
			zap.Error(err),
			)
			_ = msg.Term()
			return
		}
	   log.Info("transcoding job completed successfully", zap.String("video_id", event.VideoID))
       _ = msg.Ack()

	   	// Trigger Inngest Post-Processing Workflow
		_, _ = inngestClient.Send(jobCtx, map[string]any{
			"name": "video/transcoding.completed",
			"data": map[string]any{
				"video_id":            event.VideoID,
				"user_id":             event.UserID,
				"source_url":          event.SourceURL,
				"master_playlist_url": fmt.Sprintf("hls/%s/%s/master.m3u8", event.UserID, event.VideoID),
				"original_file_name":  event.OriginalFileName,
			},
		})
	})

	if err != nil {
		log.Fatal("failed to start consuming messages", zap.Error(err))
	}
	defer consumeCtx.Stop()

    log.Info("vortex transcoder worker running and waiting for jobs...")

	// 8. Wait for OS Shutdown Signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	sig := <-shutdown
	log.Info("shutdown signal received, stopping worker gracefully...", zap.String("signal", sig.String()))
	consumeCtx.Stop()
	log.Info("worker exited cleanly")
}
