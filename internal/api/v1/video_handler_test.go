package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/ruhamabek/vortex/internal/api/v1"
	"github.com/ruhamabek/vortex/internal/domain"
	"github.com/ruhamabek/vortex/internal/service"
)

type mockRepo struct {
	videos map[string] *domain.Video
}

func(m *mockRepo) Save(ctx context.Context, v *domain.Video) error {
	m.videos[v.ID] = v
	return nil
}

func(m *mockRepo) FindByID(ctx context.Context, id string)(*domain.Video, error) {
	v, ok := m.videos[id]
	if !ok {
		return nil, domain.ErrVideoNotFound
	}

	return v, nil
}

func (m *mockRepo) Update(ctx context.Context, v *domain.Video) error{
	m.videos[v.ID] = v
	return nil
}

func(m *mockRepo) ListByUserID(ctx context.Context, userID string, limit, offset int)([]*domain.Video, error){
	return nil, nil
}

type mockStorage struct{}

func (m *mockStorage) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := io.Copy(io.Discard, reader)
	return err
}

func(m *mockStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error){
	   return nil, nil
}

func (m *mockStorage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration)(string, error){
	return "https://s3.vortex.dev/stream/" + key, nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	return nil
}

type mockPublisher struct{}

func (m *mockPublisher) PublishVideoUploaded(ctx context.Context, event domain.VideoUploadedEvent) error {
	return nil
}

func setupTestServer()(*v1.VideoHandler, *mockRepo){
	repo := &mockRepo{videos: make(map[string]*domain.Video)}
	storage := &mockStorage{}
	publisher := &mockPublisher{}

	svc := service.NewVideoService(repo, storage, publisher)
    
	handler := v1.NewVideoHandler(svc)
	return handler, repo

}

func TestVideoHandler_Upload_Success(t *testing.T){
	handler, _ := setupTestServer()
   
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("title", "My avocado video")
	part,_ := writer.CreateFormFile("file", "avocado.mp4")
	_,_ = part.Write([]byte("fake raw bytes here"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/video/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", "user-999")
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
	}
    
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if response["title"] != "My avocado video" {
		t.Errorf("expected title 'My Skateboarding Video', got %v", response["title"])
	}

	if response["status"] != "UPLOADED" {
        t.Errorf("expected status 'UPLOADED', got %v", response["status"])
	}

}

func TestVideoHandler_GetByID(t *testing.T){
	handler, repo := setupTestServer()

	video,_ := domain.NewVideo("Test Clip", "test.mp4", 1000, "user-1")
	_= repo.Save(context.Background(), video)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	t.Run("found video returns 200 OK", func(t *testing.T) {
		    req := httptest.NewRequest(http.MethodGet,"/api/v1/videos/"+video.ID, nil)
			rec := httptest.NewRecorder()
			
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK{
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}
	})

	t.Run("missing video returns 404 not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/videos/unknown-id-123", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})
}

func TestVideoHandler_PlaybackURL(t *testing.T) {
	handler, repo := setupTestServer()

	video,_:= domain.NewVideo("Stream video", "stream.mp4", 1000, "user-1")
	_ = video.MarkUploaded("raw/stream.mp4")
	_ = video.StartTranscoding("raw/stream.mp4")
	_ = video.MarkCompleted("hls/user-1/master.m3u8")
	_ = repo.Save(context.Background(), video)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/videos/"+video.ID+"/playback", nil)
    rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if !strings.Contains(resp["playback_url"], "master.m3u8") {
		t.Errorf("expected playback URL to contain master.m3u8, got %s", resp["playback_url"])
	}
}