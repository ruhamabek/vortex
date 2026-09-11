package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ruhamabek/vortex/internal/domain"
)

// UploadVideoInput encapsulates all parameters required to upload a video
type UploadVideoInput struct {
	Title            string
	OriginalFileName string
	OriginalSize     int64
	UserID           string
	ContentReader    io.Reader
	ContentType      string
}
// VideoService orchestrates the video lifecycle business workflows
type VideoService struct {
	repo      domain.VideoRepository
	storage   domain.ObjectStorage
	publisher domain.EventPublisher
}
// NewVideoService creates a new VideoService with its injected dependencies
func NewVideoService(
	repo domain.VideoRepository,
	storage domain.ObjectStorage,
	publisher domain.EventPublisher,
) *VideoService {
	return &VideoService{
		repo:      repo,
		storage:   storage,
		publisher: publisher,
	}
}

// UploadVideo coordinates creating the video, storing the file in S3, and notifying NATS

func (s *VideoService) UploadVideo(ctx context.Context, input UploadVideoInput)(*domain.Video, error){
	
	// 1. Create the domain entity (starts in PENDING state)
	video, err := domain.NewVideo(input.Title, input.OriginalFileName, input.OriginalSize, input.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid video data: %v", err)
	}

	// 2. Save initial PENDING record in the database
	if err := s.repo.Save(ctx, video); err != nil {
		  return nil, fmt.Errorf("failed to save initial video record: %v", err)
	}
	
    // 3. Define the destination S3 storage path: raw/{userID}/{videoID}_{filename}   
	storageKey :=  fmt.Sprintf("raw/%s/%s_%s", video.UserID, video.ID, video.OriginalFileName)
    
	// 4. Stream the video file directly into S3 (MinIO)
	err = s.storage.PutObject(ctx, storageKey, input.ContentReader, input.OriginalSize, input.ContentType)
    
	// 5. Update domain state to UPLOADED
	if err != nil {
		_ = video.MarkFailed(fmt.Sprintf("storage upload failed: %v", err))
		_= s.repo.Update(ctx, video)
		return nil, fmt.Errorf("failed to upload video to storage: %v", err)
	}

	
	if err := video.MarkUploaded(storageKey); err != nil {
		return nil, fmt.Errorf("failed to transition video state: %v", err)
	}

	// 6. Update database with UPLOADED status
	if err := s.repo.Update(ctx, video); err != nil {
		return nil, fmt.Errorf("failed to update video recorded: %v", err)
	}

    // 7. Publish event to NATS JetStream so Transcoder Workers can start!
	event := domain.VideoUploadedEvent{
			VideoID:          video.ID,
			UserID:           video.UserID,
			SourceURL:        video.SourceURL,
			OriginalFileName: video.OriginalFileName,
			OriginalSize:     video.OriginalSize,
			UploadedAt:       video.UpdatedAt,
		}
	if err := s.publisher.PublishVideoUploaded(ctx, event); err != nil {
		return nil, fmt.Errorf("failed to publish video uploaded event: %w", err)
	}
	return video, nil
}


func (s *VideoService) GetVideo(ctx context.Context, id string)(*domain.Video, error){
	return s.repo.FindByID(ctx, id)
}

func (s *VideoService) ListUserVideo(ctx context.Context, userID string, limit, offset int)([]*domain.Video, error){
	if limit <= 0 || limit > 100 {
	   limit = 10
	}

	if offset < 0 {
		offset = 0
	}

	return s.repo.ListByUserID(ctx, userID, limit, offset)
}

func (s *VideoService) GetPlaybackURL(ctx context.Context, videoID string)(string, error){
	video, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return "",err
	}

	if video.Status != domain.VideoStatusCompleted {
		return "", errors.New("video is not ready for playback: status is " + string(video.Status))
	}

	return s.storage.PresignedGetURL(ctx, video.MasterPlaylistURL, 2*time.Hour)
}