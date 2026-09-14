package config

import (
	"fmt"
	"os"
	"strings"
)

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type Config struct {
	Environment   string
	Port          string
	GRPCPort      string
	DatabaseURL   string
	MinIO         MinIOConfig
	NATSURL       string
	RedisAddr     string
	TempoEndpoint string
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

// Load reads all environment variables into a strongly-typed Config struct.
// In development, it provides sensible local defaults for developer convenience.
// In production, it strictly enforces that required credentials and URLs are set.
func Load() (*Config, error) {
	env := getEnv("ENVIRONMENT", "development")

	cfg := &Config{
		Environment: env,
		Port:        getEnv("PORT", "8080"),
		GRPCPort:    getEnv("GRPC_PORT", "50055"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", ""),
			AccessKey: getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey: getEnv("MINIO_SECRET_KEY", ""),
			Bucket:    getEnv("MINIO_BUCKET", "raw-videos"),
			UseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",
		},
		NATSURL:       getEnv("NATS_URL", ""),
		RedisAddr:     getEnv("REDIS_ADDR", ""),
		TempoEndpoint: getEnv("TEMPO_ENDPOINT", "localhost:4317"),
	}

 	if env == "production" {
		var missing []string
		if cfg.DatabaseURL == "" {
			missing = append(missing, "DATABASE_URL")
		}
		if cfg.MinIO.Endpoint == "" {
			missing = append(missing, "MINIO_ENDPOINT")
		}
		if cfg.MinIO.AccessKey == "" {
			missing = append(missing, "MINIO_ACCESS_KEY")
		}
		if cfg.MinIO.SecretKey == "" {
			missing = append(missing, "MINIO_SECRET_KEY")
		}
		if cfg.NATSURL == "" {
			missing = append(missing, "NATS_URL")
		}
		if cfg.RedisAddr == "" {
			missing = append(missing, "REDIS_ADDR")
		}

		if len(missing) > 0 {
			return nil, fmt.Errorf("production config error: missing required environment variables: %s", strings.Join(missing, ", "))
		}
	} else {
 		if cfg.DatabaseURL == "" {
			cfg.DatabaseURL = "postgres://vortex:vortex_secret_password@localhost:5432/vortex_db?sslmode=disable"
		}
		if cfg.MinIO.Endpoint == "" {
			cfg.MinIO.Endpoint = "localhost:9000"
		}
		if cfg.MinIO.AccessKey == "" {
			cfg.MinIO.AccessKey = "minioadmin"
		}
		if cfg.MinIO.SecretKey == "" {
			cfg.MinIO.SecretKey = "minioadminpassword"
		}
		if cfg.NATSURL == "" {
			cfg.NATSURL = "nats://localhost:4222"
		}
		if cfg.RedisAddr == "" {
			cfg.RedisAddr = "localhost:6379"
		}
	}

	return cfg, nil
}