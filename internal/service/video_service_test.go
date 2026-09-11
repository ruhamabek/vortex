package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ruhamabek/vortex/internal/domain"
	"github.com/ruhamabek/vortex/internal/service"
)

type MockVideoRepo struct {
	videos map[string]*domain.Video
	saveErr error
	updateErr error
}

func newMockVideoRepo()*MockVideoRepo{
	return &MockVideoRepo{
		videos: make(map[string]*domain.Video),
	}
}

func(m *MockVideoRepo) Save(ctx context.Context, v *domain.Video) error{
	if m.saveErr != nil {
		return m.saveErr
	}
	m.videos[v.ID] = v
	return nil
}

func (m *MockVideoRepo) FindByID(ctx context.Context, id string)(*domain.Video, error){
	v, ok := m.videos[id]
	if !ok {
		return nil, domain.ErrVideoNotFound
	}
	return v, nil
}

func (m *MockVideoRepo) Update(ctx context.Context, v *domain.Video) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.videos[v.ID] = v
	return nil 
}

func (m *MockVideoRepo) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.Video, error){
     var list[]*domain.Video
	 for _, v := range m.videos {
		if v.UserID == userID{
			list = append(list, v)
		}
	 }

	 return list, nil
}

type MockObjectStorage struct{
	putErr error
}

func (m *MockObjectStorage) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error{
	return m.putErr
}

func (m *MockObjectStorage) GetObject(ctx context.Context, key string)(io.ReadCloser, error){
	return nil, nil
}

func (m *MockObjectStorage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration)(string, error){
	return "https://s3.vortex.dev/" + key, nil
}

func (m *MockObjectStorage) DeleteObject(ctx context.Context, key string) error{
	return nil
}

type MockEventPublisher struct {
	publishedEvents []domain.VideoUploadedEvent
	publishErr      error
}

func (m *MockEventPublisher) PublishVideoUploaded(ctx context.Context, event domain.VideoUploadedEvent) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}



// Test 1: The Happy Path (Everything succeeds)
func TestVideoService_UploadVideo_Success(t *testing.T) {
	repo := newMockVideoRepo()
	storage := &MockObjectStorage{} // Normal storage (no error)
	publisher := &MockEventPublisher{}

	svc := service.NewVideoService(repo, storage, publisher)
	ctx := context.Background()

	input := service.UploadVideoInput{
		Title:            "Epic Mountain Biking",
		OriginalFileName: "biking.mp4",
		OriginalSize:     1024 * 1024 * 20, // 20 MB
		UserID:           "user-42",
		ContentReader:    bytes.NewReader([]byte("fake video stream bytes")),
		ContentType:      "video/mp4",
	}

	video, err := svc.UploadVideo(ctx, input)
	if err != nil {
		t.Fatalf("expected successful upload, got: %v", err)
	}

	// 1. Verify video returned is UPLOADED
	if video.Status != domain.VideoStatusUploaded {
		t.Errorf("expected status UPLOADED, got %s", video.Status)
	}

	// 2. Verify stored in repository
	saved, _ := repo.FindByID(ctx, video.ID)
	if saved == nil || saved.Status != domain.VideoStatusUploaded {
		t.Errorf("expected video saved in repo with status UPLOADED")
	}

	// 3. Verify NATS event was published!
	if len(publisher.publishedEvents) != 1 {
		t.Fatalf("expected exactly 1 event published, got %d", len(publisher.publishedEvents))
	}
	if publisher.publishedEvents[0].VideoID != video.ID {
		t.Errorf("expected event video ID %s, got %s", video.ID, publisher.publishedEvents[0].VideoID)
	}
}

// Test 2: The Failure Path (S3 fails, video marked FAILED, no NATS event)
func TestVideoService_UploadVideo_StorageFailure(t *testing.T) {
	repo := newMockVideoRepo()
	// Storage that purposefully returns an error!
	storage := &MockObjectStorage{putErr: errors.New("s3 connection timeout")}
	publisher := &MockEventPublisher{}

	svc := service.NewVideoService(repo, storage, publisher)
	ctx := context.Background()

	input := service.UploadVideoInput{
		Title:            "Failed Video",
		OriginalFileName: "clip.mp4",
		OriginalSize:     5000,
		UserID:           "user-42",
		ContentReader:    bytes.NewReader([]byte("bytes")),
		ContentType:      "video/mp4",
	}

	_, err := svc.UploadVideo(ctx, input)
	if err == nil {
		t.Fatalf("expected upload to fail when storage fails, got nil")
	}

	// Verify NATS event was never published
	if len(publisher.publishedEvents) != 0 {
		t.Errorf("expected 0 events published on failure, got %d", len(publisher.publishedEvents))
	}

	// Verify video was marked FAILED in repository
	for _, v := range repo.videos {
		if v.Status != domain.VideoStatusFailed {
			t.Errorf("expected video to be marked FAILED in repo, got %s", v.Status)
		}
	}
}