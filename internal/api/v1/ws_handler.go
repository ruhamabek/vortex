package v1

import (
	"context"
	"net/http"
	"github.com/gorilla/websocket"
	"github.com/ruhamabek/vortex/internal/storage/redis"
)

type ProgressSubscriber interface {
	GetProgress(ctx context.Context, videoID string) (int, string, error)
	SubscribeProgress(ctx context.Context, videoID string) (<-chan redis.ProgressUpdate, func(), error)
}

type WSHandler struct {
	tracker  ProgressSubscriber
	upgrader websocket.Upgrader
}

func NewWSHandler(tracker ProgressSubscriber) *WSHandler {
	return &WSHandler{
		tracker: tracker,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true 
			},
		},
	}
}

// RegisterRoutes registers the WebSocket endpoint 
func (h *WSHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws/videos/{id}/progress", h.HandleProgressWS)
}

func (h *WSHandler) HandleProgressWS(w http.ResponseWriter, r *http.Request){
	videoID := r.PathValue("id")
	if videoID == ""{
		http.Error(w, "missing video id", http.StatusBadRequest)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer conn.Close()

	// 1. Send immediate initial state if video has already started
	percent, stage, err := h.tracker.GetProgress(r.Context(), videoID)
	if err == nil {
		initialUpdate := redis.ProgressUpdate{
			VideoID: videoID,
			Percent: percent,
			Stage: stage,
		}
		if err := conn.WriteJSON(initialUpdate); err != nil {
			return
		}

		if stage == "COMPLETED" || stage == "FAILED" {
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "finished"))
			return
		}
	}

	// 2. Subscribe to real-time Redis Pub/Sub stream
	updates, stop, err := h.tracker.SubscribeProgress(r.Context(), videoID)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": "failed to subscribe to progress"})
		return
	}
	defer stop()

	// 3. Detect client disconnect  
	clientClosed := make(chan struct{})
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(clientClosed)
				return
			}
		}
	}()

	// 4. Stream live progress frames to the WebSocket
	for {
		select {
		case <-clientClosed:
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := conn.WriteJSON(update); err != nil {
				return
			}
			// When finished, send close frame and terminate
			if update.Stage == "COMPLETED" || update.Stage == "FAILED" {
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "finished"))
				return
			}
		}
	}
}