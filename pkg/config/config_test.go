package config_test

import (
	"os"
	"testing"

	"github.com/ruhamabek/vortex/pkg/config"
)

func TestConfig_DevelopmentDefaults(t *testing.T) {
 	os.Clearenv()
	_ = os.Setenv("ENVIRONMENT", "development")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected config to load with dev defaults, got: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("expected default port 8080, got %s", cfg.Port)
	}

	if cfg.MinIO.Endpoint != "localhost:9000" {
		t.Errorf("expected default MinIO endpoint localhost:9000, got %s", cfg.MinIO.Endpoint)
	}
}

func TestConfig_ProductionEnforcement(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("ENVIRONMENT", "production")
 
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected config.Load() to FAIL in production when required secrets are missing, but it succeeded")
	}
}

func TestConfig_EnvironmentOverrides(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("ENVIRONMENT", "production")
	_ = os.Setenv("PORT", "9090")
	_ = os.Setenv("DATABASE_URL", "postgres://custom-db:5432/vortex")
	_ = os.Setenv("MINIO_ENDPOINT", "s3.amazonaws.com")
	_ = os.Setenv("MINIO_ACCESS_KEY", "prod-key")
	_ = os.Setenv("MINIO_SECRET_KEY", "prod-secret")
	_ = os.Setenv("NATS_URL", "nats://prod-nats:4222")
	_ = os.Setenv("REDIS_ADDR", "prod-redis:6379")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %s", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://custom-db:5432/vortex" {
		t.Errorf("expected custom database URL, got %s", cfg.DatabaseURL)
	}
	if cfg.MinIO.Endpoint != "s3.amazonaws.com" {
		t.Errorf("expected custom MinIO endpoint, got %s", cfg.MinIO.Endpoint)
	}
}