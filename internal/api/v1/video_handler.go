package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ruhamabek/vortex/internal/domain"
	"github.com/ruhamabek/vortex/internal/service"
)

type VideoHandler struct {
	videoService *service.VideoService
}

func NewVideoHandler(videoService *service.VideoService) *VideoHandler{
	return &VideoHandler{
		videoService: videoService,
	}
}

func (h *VideoHandler) RegisterRoutes (mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/video/upload", h.Upload)
	mux.HandleFunc("POST /api/v1/videos/upload", h.Upload) 
	mux.HandleFunc("GET /api/v1/videos/{id}", h.GetByID)
	mux.HandleFunc("GET /api/v1/videos/{id}/playback", h.GetPlaybackURL)
}

func (h *VideoHandler) Upload(w http.ResponseWriter, r *http.Request) {
	  r.Body = http.MaxBytesReader(w, r.Body, domain.MaxVideoSizeBytes)

	  if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse multipart form: " + err.Error(), http.StatusBadRequest)
		return
	  }
	  defer r.MultipartForm.RemoveAll()

	  title := r.FormValue("title")
	  if title == ""{
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	  }

	  file, header, err := r.FormFile("file")

	  if err != nil {
			http.Error(w, "video file is required under form field 'file'", http.StatusBadRequest)
			return
		}
	   defer file.Close()

	   userID := r.Header.Get("X-User-ID")
	   if userID == ""{
		userID = "anonymous"
	   }

	   contentType := header.Header.Get("Content-Type")
	   if contentType == ""{
		    contentType = "video/mp4"
	   }

	   input := service.UploadVideoInput{
			Title:            title,
			OriginalFileName: header.Filename,
			OriginalSize:     header.Size,
			UserID:           userID,
			ContentReader:    file,
			ContentType:      contentType,
		}

	   video, err := h.videoService.UploadVideo(r.Context(), input)
	   if err != nil {
		  http.Error(w, err.Error(), http.StatusInternalServerError)
		  return
	   }

	   w.Header().Set("Content-Type", "application/json")
	   w.WriteHeader(http.StatusCreated)
	   _ = json.NewEncoder(w).Encode(video)
}

func(h *VideoHandler) GetByID(w http.ResponseWriter, r *http.Request){
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing video id", http.StatusBadRequest)
		return
	}

	video, err := h.videoService.GetVideo(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrVideoNotFound){
			http.Error(w, "video not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(video)
}

func (h *VideoHandler) GetPlaybackURL(w http.ResponseWriter, r *http.Request){
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing video id", http.StatusBadRequest)
        return
	}
    
	url, err := h.videoService.GetPlaybackURL(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrVideoNotFound) {
            http.Error(w, "video not found", http.StatusNotFound)
            return
        }
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
	}

	w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "playback_url": url,
    })
}