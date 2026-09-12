package v1_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/gorilla/websocket"
	v1 "github.com/ruhamabek/vortex/internal/api/v1"
	"github.com/ruhamabek/vortex/internal/storage/redis"
)
type mockProgressSubscriber struct {
	progress   map[string]int
	stages     map[string]string
	updateChan chan redis.ProgressUpdate
}
func (m *mockProgressSubscriber) GetProgress(ctx context.Context, videoID string) (int, string, error) {
	return m.progress[videoID], m.stages[videoID], nil
}
func (m *mockProgressSubscriber) SubscribeProgress(ctx context.Context, videoID string) (<-chan redis.ProgressUpdate, func(), error) {
	return m.updateChan, func() {}, nil
}

func TestWSHandler_ProgressStreaming(t *testing.T) {
	updateChan := make(chan redis.ProgressUpdate, 10)
	mockSubscriber := &mockProgressSubscriber{
		progress:   map[string]int{"vid-123": 10},
		stages:     map[string]string{"vid-123": "STARTING"},
		updateChan: updateChan,
	}

	wsHandler := v1.NewWSHandler(mockSubscriber)
	mux := http.NewServeMux()
	wsHandler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/videos/vid-123/progress"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v",err)
		return
	}
	defer conn.Close()

	//1. first message = initial state
	var firstMsg redis.ProgressUpdate
	if err := conn.ReadJSON(&firstMsg); err != nil {
		t.Fatalf("failed to read initial message: %v", err)
	}
	if firstMsg.Percent != 10 || firstMsg.Stage != "STARTING" {
		t.Errorf("expected initial message 10%% STARTING, got %d%% %s", firstMsg.Percent, firstMsg.Stage)
	}
    
	// 2. Stream an update = 50% TRANSCODING
	updateChan <- redis.ProgressUpdate{
		VideoID: "vid-123",
		Percent: 50,
		Stage:   "TRANSCODING",
	}
	var secondMsg redis.ProgressUpdate
	if err := conn.ReadJSON(&secondMsg); err != nil {
		t.Fatalf("failed to read second message: %v", err)
	}
	if secondMsg.Percent != 50 || secondMsg.Stage != "TRANSCODING" {
		t.Errorf("expected 50%% TRANSCODING, got %d%% %s", secondMsg.Percent, secondMsg.Stage)
	}

	// 3. Stream = 100% COMPLETED
	updateChan <- redis.ProgressUpdate{
		VideoID: "vid-123",
		Percent: 100,
		Stage:   "COMPLETED",
	}

	var thirdMsg redis.ProgressUpdate
	if err := conn.ReadJSON(&thirdMsg); err != nil {
		t.Fatalf("failed to read third message: %v", err)
	}
	if thirdMsg.Percent != 100 || thirdMsg.Stage != "COMPLETED" {
		t.Errorf("expected 100%% COMPLETED, got %d%% %s", thirdMsg.Percent, thirdMsg.Stage)
	}
	// 4. Server close connection = COMPLETED
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Error("expected websocket to close after completion, but read succeeded")
	}
}
