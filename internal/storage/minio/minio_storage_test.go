package minio_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	miniosvc "github.com/ruhamabek/vortex/internal/storage/minio"
)


const (
	testEndpoint  = "localhost:9000"
	testAccessKey = "minioadmin"
	testSecretKey = "minioadminpassword"
	testBucket    = "raw-videos"
)

func setupTestMinIO(t *testing.T) *minio.Client {
	client, err := minio.New(testEndpoint, &minio.Options{
		 Creds: credentials.NewStaticV4(testAccessKey, testSecretKey, ""),
         Secure: false,
	})

	if err != nil {
		t.Fatalf("failed to initialize minio client: %v", err)
	}

	ctx := context.Background()

	exists, err := client.BucketExists(ctx, testBucket)

	if err != nil {
		t.Fatalf("failed to check test bucket: %v", err)
	}

	if !exists {
		if err := client.MakeBucket(ctx, testBucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("Failed to create test bucket %v: %v", testBucket, err)
		}
	}

	return client
}

func TestMinIOStorage_Lifecycle(t *testing.T){
	client := setupTestMinIO(t)
	storage := miniosvc.NewMinIOStorage(client, testBucket)
	ctx := context.Background()

	testKey := "tests/sample_clip.mp4"
	fakeVideoContent := []byte("FAKE_VIDEO_STREAM_BYTES_FOR_TESTING_12345")
	currentLength := int64(len(fakeVideoContent))
   	contentType := "video/mp4"

	t.Cleanup(func() {
		_=storage.DeleteObject(context.Background(), testKey)
	})

	err := storage.PutStorage(ctx, testKey, bytes.NewReader(fakeVideoContent), currentLength, contentType)
	if err != nil {
		t.Fatalf("failed to upload object to minio: %v", err)
	}

	reader, err := storage.GetObject(ctx, testKey)

	if err != nil {
		t.Fatalf("failed to upload onject from minio: %v",err)
	}

	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read downloaded object bytes: %v", err)
	}

	if string(downloaded) != string(fakeVideoContent){
		t.Errorf("content mismatch, expected %v, got %v", fakeVideoContent, downloaded)
	}

	presignedURL, err := storage.PresignedGetURL(ctx, testKey, 15*time.Minute)

	if err != nil {
		t.Fatalf("failed to generate presigned URL: %v", err)
	}

	if !strings.Contains(presignedURL, testKey){
		t.Errorf("expected presigned URL to contain key %v, got %v", testKey, presignedURL)
	}

	if err := storage.DeleteObject(ctx, testKey); err != nil {
		t.Fatalf("failed to delete object: %v", err)
	}
}
