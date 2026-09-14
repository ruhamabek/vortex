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
	grpcsvc "github.com/ruhamabek/vortex/internal/grpc/v1"
	natssvc "github.com/ruhamabek/vortex/internal/event/nats"
	"github.com/ruhamabek/vortex/internal/service"
	miniosvc "github.com/ruhamabek/vortex/internal/storage/minio"
	pgsvc "github.com/ruhamabek/vortex/internal/storage/postgres"
	"github.com/ruhamabek/vortex/pkg/logger"
	videov1 "github.com/ruhamabek/vortex/proto/gen/video/v1"
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
	port := getEnv("GRPC_PORT", "50055")
	dbURL := getEnv("DATABASE_URL", "postgres://vortex:vortex_secret_password@localhost:5432/vortex_db?sslmode=disable")
	minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	minioAccessKey := getEnv("MINIO_ACCESS_KEY", "minioadmin")
	minioSecretKey := getEnv("MINIO_SECRET_KEY", "minioadminpassword")
	minioBucket := getEnv("MINIO_BUCKET", "raw-videos")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// 1. Connect to PostgreSQL
	log.Info("connecting to postgres...", zap.String("url", dbURL))
	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal("unable to connect to postgres", zap.Error(err))
	}
	defer dbPool.Close()
	if err := dbPool.Ping(ctx); err != nil {
		log.Fatal("failed to ping postgres", zap.Error(err))
	}
	// 2. Connect to MinIO
	log.Info("connecting to minio...", zap.String("endpoint", minioEndpoint))
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatal("failed to initialize minio client", zap.Error(err))
	}
	// 3. Connect to NATS JetStream
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
	// 4. Instantiate Service & gRPC Server
	videoRepo := pgsvc.NewVideoRepository(dbPool)
	objectStorage := miniosvc.NewMinIOStorage(minioClient, minioBucket)
	eventPublisher := natssvc.NewNATSEventPublisher(js)
	videoService := service.NewVideoService(videoRepo, objectStorage, eventPublisher)
	videoServer := grpcsvc.NewVideoServer(videoService)

	grpcServer := grpc.NewServer()
	videov1.RegisterVideoServiceServer(grpcServer, videoServer)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal("failed to listen on tcp port", zap.String("port", port), zap.Error(err))
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Info("vortex grpc server listening", zap.String("port", port))
		serverErrors <- grpcServer.Serve(lis)
	}()

 	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		log.Fatal("grpc server error", zap.Error(err))
	case sig := <-shutdown:
		log.Info("shutdown signal received, stopping grpc server...", zap.String("signal", sig.String()))
		grpcServer.GracefulStop()
		log.Info("grpc server stopped cleanly")
	}

}