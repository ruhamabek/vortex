package worker_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
    "github.com/ruhamabek/vortex/internal/worker"
	"github.com/ruhamabek/vortex/internal/domain"
)


type mockRepo struct {
	videos map[string]*domain.Video
}

func (m *mockRepo) Save(ctx context.Context, v *domain.Video)error {
	m.videos[v.ID] = v
	return nil 
}

func (m *mockRepo) FindByID(ctx context.Context, id string)(*domain.Video, error) {
	v, ok := m.videos[id]
	if !ok {
		return nil, domain.ErrVideoNotFound
	}

	return v, nil
}

func (m *mockRepo) Update(ctx context.Context, v *domain.Video) error {
	m.videos[v.ID] = v
	return nil
}

func (m *mockRepo) ListByUserID(ctx context.Context, userID string, lmit, offset int)([]*domain.Video, error){
	return nil, nil
}

type mockStorage struct {
	objects map[string][]byte
}

func (m *mockStorage) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.objects[key] = data
	return nil
}


func (m *mockStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStorage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "https://storage.vortex.dev/" + key, nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

type mockProgressTracker struct {
	progress map[string]int
	stages   map[string]string
}

func(m *mockProgressTracker) SetProgress(ctx context.Context, videoID string, progressPercent int, stage string) error {
	m.progress[videoID] = progressPercent
	m.stages[videoID] = stage
	return nil
}

func(m *mockProgressTracker) GetProgress(ctx context.Context, videoID string)(int, string, error){
	return m.progress[videoID], m.stages[videoID], nil
}

type mockTranscoder struct {
	shouldFail bool
}

func (m *mockTranscoder) TranscodeHLS(ctx context.Context, inputPAth, outputDir string, onProgress func(int)) error {
	if m.shouldFail {
		return errors.New("ffmpeg error: invalid stream")
	}

	if onProgress != nil {
		onProgress(25)
		onProgress(50)
		onProgress(100)
	}

	_= os.WriteFile(filepath.Join(outputDir, "master.m3u8"), []byte("#EXTM3U\n#EXT-X-STREAM-INF..."), 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "segment_000.ts"), []byte("DUMMY_TS_BYTES"), 0644)
	return nil
}

func TestTranscoderWorker_ProcessVideo_Sucess(t *testing.T){
	repo := &mockRepo{videos: make(map[string]*domain.Video)}
	storage := &mockStorage{objects: make(map[string][]byte)}
	tracker := &mockProgressTracker{progress: make(map[string]int), stages: make(map[string]string)}
	transcoder := &mockTranscoder{shouldFail: false}

	video,_:=domain.NewVideo("Summer Vacation ended :*(", "clip.mp4", 1000, "user-42")
	_ = video.MarkUploaded("raw/user-42/" + video.ID + "_clip.mp4")
	_ = repo.Save(context.Background(), video)

	storage.objects[video.SourceURL] = []byte("RAW_MP4_FILE_BYTES")

	w := worker.NewTranscoderWorker(repo, storage, tracker, transcoder)

	event := domain.VideoUploadedEvent{
		VideoID:          video.ID,
		UserID:           video.UserID,
		SourceURL:        video.SourceURL,
		OriginalFileName: video.OriginalFileName,
		OriginalSize:     video.OriginalSize,
		UploadedAt:       video.UpdatedAt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := w.ProcessVideo(ctx, event)
	if err != nil {
		t.Fatalf("expected ProcessVideo to succeed, got: %v", err)
	}
	
	savedVideo, _ := repo.FindByID(ctx, video.ID)
	if savedVideo.Status != domain.VideoStatusCompleted {
		t.Errorf("expected video status to be COMPLETED, got %v", savedVideo.Status)
	}
	if !strings.Contains(savedVideo.MasterPlaylistURL, "master.m3u8") {
		t.Errorf("expected MasterPlaylistURL to contain 'master.m3u8', got %v", savedVideo.MasterPlaylistURL)
	}
 
	if len(storage.objects) < 2 {
		t.Errorf("expected master.m3u8 and .ts files uploaded to storage, found %d items", len(storage.objects))
	}
 
	progress, stage, _ := tracker.GetProgress(ctx, video.ID)
	if progress != 100 || stage != "COMPLETED" {
		t.Errorf("expected progress 100 and stage COMPLETED, got %d and %s", progress, stage)
	}
}


func TestTranscoderWorker_ProcessVideo_TranscodeFailure(t *testing.T) {
	repo := &mockRepo{videos: make(map[string]*domain.Video)}
	storage := &mockStorage{objects: make(map[string][]byte)}
	tracker := &mockProgressTracker{progress: make(map[string]int), stages: make(map[string]string)}
	transcoder := &mockTranscoder{shouldFail: true}

	video, _ := domain.NewVideo("Broken Video", "corrupt.mp4", 1000, "user-42")
	_ = video.MarkUploaded("raw/user-42/" + video.ID + "_corrupt.mp4")
	_ = repo.Save(context.Background(), video)
	storage.objects[video.SourceURL] = []byte("CORRUPT_BYTES")

	w := worker.NewTranscoderWorker(repo, storage, tracker, transcoder)
	event := domain.VideoUploadedEvent{
		VideoID:          video.ID,
		UserID:           video.UserID,
		SourceURL:        video.SourceURL,
		OriginalFileName: video.OriginalFileName,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := w.ProcessVideo(ctx, event)
	if err == nil {
		t.Fatal("expected ProcessVideo to fail, got nil")
	}

	savedVideo, _ := repo.FindByID(ctx, video.ID)
	if savedVideo.Status != domain.VideoStatusFailed {
		t.Errorf("expected video status to be FAILED, got %v", savedVideo.Status)
	}

	if savedVideo.ErrorMessage == "" {
		t.Error("expected error message to be recorded on video")
	}
}
