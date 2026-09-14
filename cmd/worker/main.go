package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	internalgrpc "github.com/ruhamabek/vortex/internal/grpc/v1"
	natssvc "github.com/ruhamabek/vortex/internal/event/nats"
	"github.com/ruhamabek/vortex/internal/service"
	miniosvc "github.com/ruhamabek/vortex/internal/storage/minio"
	pgsvc "github.com/ruhamabek/vortex/internal/storage/postgres"
	"github.com/ruhamabek/vortex/pkg/config"
	"github.com/ruhamabek/vortex/pkg/logger"
	videov1 "github.com/ruhamabek/vortex/proto/gen/video/v1"
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

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 3. Connect to PostgreSQL
	log.Info("connecting to postgres...", zap.String("url", cfg.DatabaseURL))
	dbPool, err := pgxpool.New(ctxTimeout, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("unable to connect to postgres", zap.Error(err))
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctxTimeout); err != nil {
		log.Fatal("failed to ping postgres", zap.Error(err))
	}

	// 4. Connect to MinIO
	log.Info("connecting to minio...", zap.String("endpoint", cfg.MinIO.Endpoint))
	minioClient, err := minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})
	if err != nil {
		log.Fatal("failed to initialize minio client", zap.Error(err))
	}

	// 5. Connect to NATS JetStream
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

	// 6. Instantiate Domain Services
	videoRepo := pgsvc.NewVideoRepository(dbPool)
	objectStorage := miniosvc.NewMinIOStorage(minioClient, cfg.MinIO.Bucket)
	eventPublisher := natssvc.NewNATSEventPublisher(js)
	videoService := service.NewVideoService(videoRepo, objectStorage, eventPublisher)

	// 7. Start gRPC Server
	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Fatal("failed to listen on gRPC port", zap.Error(err))
	}

	grpcServer := grpc.NewServer()
	videoServer := internalgrpc.NewVideoServer(videoService)
	videov1.RegisterVideoServiceServer(grpcServer, videoServer)
	reflection.Register(grpcServer)

	serverErrors := make(chan error, 1)
	go func() {
		log.Info("vortex grpc server listening", zap.String("port", cfg.GRPCPort))
		serverErrors <- grpcServer.Serve(lis)
	}()

	// 8. Graceful Shutdown listener
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatal("gRPC server error", zap.Error(err))
	case sig := <-shutdown:
		log.Info("shutdown signal received, stopping gRPC server gracefully...", zap.String("signal", sig.String()))
		grpcServer.GracefulStop()
		log.Info("gRPC server shutdown complete")
	}
}