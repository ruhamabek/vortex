package domain

import (
	"context"
	"io"
	"time"
)

type VideoRepository interface {
	Save(ctx context.Context, video *Video) error
	FindByID(ctx context.Context, id string)(*Video, error)
    Update(ctx context.Context, video *Video) error
	ListByUserID(ctx context.Context, userID string, limit, offset string)([]*Video, error)
}

type ObjectStorage interface{
	 PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string)
	 GetObject(ctx context.Context, key string)(io.ReadCloser, error)
	 PresignedGetURL(ctx context.Context, key string, expiry time.Time)(string, error)
	 DeleteObject(ctx context.Context, key string) error
}

type VideoUploadedEvent struct {
	VideoID          string    `json:"video_id"`
	UserID           string    `json:"user_id"`
	SourceURL        string    `json:"source_url"`
	OriginalFileName string    `json:"original_file_name"`
	OriginalSize     int64     `json:"original_size"`
	UploadedAt       time.Time `json:"uploaded_at"`
}

type EventPublisher interface {
	PublishVideoUploaded(ctx context.Context, event VideoUploadedEvent) error
}

type ProgressTracker interface {
	SetProgress(ctx context.Context, videoID string, progressPercent int, stage string) error
	GetProgress(ctx context.Context, videoID string) (progressPercent int, stage string, err error)
}

