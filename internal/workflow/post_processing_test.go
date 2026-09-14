package workflow_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/ruhamabek/vortex/internal/workflow"
)

type mockStorage struct{}

func (m *mockStorage) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	return nil
}

func (m *mockStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockStorage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	return nil
}

type mockThumbnailGenerator struct {
	posterGenerated  bool
	previewGenerated bool
}

func (m *mockThumbnailGenerator) GeneratePoster(ctx context.Context, inputPath, outputPath string) error {
	m.posterGenerated = true
	return nil
}

func (m *mockThumbnailGenerator) GeneratePreviewGIF(ctx context.Context, inputPath, outputPath string) error {
	m.previewGenerated = true
	return nil
}

func TestPostProcessingFunction_Registration(t *testing.T) {
	client, err := inngestgo.NewClient(inngestgo.ClientOpts{
		AppID: "vortex-test",
	})
	if err != nil {
		t.Fatalf("failed to create inngest client: %v", err)
	}

	storage := &mockStorage{}
	mockGen := &mockThumbnailGenerator{}

	fn, err := workflow.NewPostProcessingFunction(client, storage, mockGen)
	if err != nil {
		t.Fatalf("expected NewPostProcessingFunction to succeed, got: %v", err)
	}

	if fn == nil {
		t.Fatal("expected servable function to be non-nil")
	}

	if fn.Name() != "Post-Process Transcoded Video" {
		t.Errorf("expected function name 'Post-Process Transcoded Video', got '%s'", fn.Name())
	}
}