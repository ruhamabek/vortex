package domain


import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidTitle           = errors.New("video title cannot be empty")
	ErrInvalidFileSize        = errors.New("video file size must be greater than 0")
	ErrFileTooLarge           = errors.New("video file size exceeds maximum limit of 2GB")
	ErrInvalidUserID          = errors.New("user ID cannot be empty")
	ErrInvalidStateTransition = errors.New("invalid video state transition")
	ErrVideoNotFound          = errors.New("video not found")
)

const MaxVideoSizeBytes int64 = 2 * 1024 * 1024 * 1024

type VideoStatus string

const (
	VideoStatusPending     VideoStatus = "PENDING"
	VideoStatusUploaded    VideoStatus = "UPLOADED"
	VideoStatusTranscoding VideoStatus = "TRANSCODING"
	VideoStatusCompleted   VideoStatus = "COMPLETED"
	VideoStatusFailed      VideoStatus = "FAILED"
)

type Video struct {
	ID                string      `json:"id"`
	Title             string      `json:"title"`
	OriginalFileName  string      `json:"original_file_name"`
	OriginalSize      int64       `json:"original_size"`
	UserID            string      `json:"user_id"`
	Status            VideoStatus `json:"status"`
	SourceURL         string      `json:"source_url,omitempty"`
	MasterPlaylistURL string      `json:"master_playlist_url,omitempty"`
	ErrorMessage      string      `json:"error_message,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

func NewVideo(title, originalFileName string, sizeBytes int64, userID string)(*Video, error){
	if title == ""{
		return nil, ErrInvalidTitle
	}

	if sizeBytes == 0 {
		return nil, ErrInvalidFileSize
	}

     if sizeBytes > MaxVideoSizeBytes {
		 return nil, ErrFileTooLarge
	 }

	 if userID == ""{
		return nil, ErrInvalidUserID
	 }

	 id, err := generateUUID()

	 if err != nil {
		return nil, fmt.Errorf("failed to generate video id: %v", err)
	 }

	 now := time.Now().UTC()

	 return &Video{
		ID: id,
		Title: title,
		OriginalFileName: originalFileName,
		OriginalSize: sizeBytes,
		UserID: userID,
		Status: VideoStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	 }, nil
}

func (v *Video) MarkUploaded(sourceURL string) error {
	if v.Status != VideoStatusPending {
		return ErrInvalidStateTransition
	}
	v.Status = VideoStatusUploaded
	v.SourceURL = sourceURL
	v.UpdatedAt = time.Now().UTC()
	return nil
}

func (v *Video) StartTranscoding(sourceURL string) error{
	if v.Status != VideoStatusUploaded {
		return ErrInvalidStateTransition
	}

	v.Status = VideoStatusTranscoding
	v.UpdatedAt = time.Now().UTC()
	return nil
}

func (v *Video) MarkCompleted(masterPlaylistURL string) error {
	if v.Status != VideoStatusTranscoding {
		return ErrInvalidStateTransition
	}
	v.Status = VideoStatusCompleted
	v.MasterPlaylistURL = masterPlaylistURL
	v.UpdatedAt = time.Now().UTC()
	return nil
}

func (v *Video) MarkFailed(reason string) error {
	if v.Status == VideoStatusCompleted {
		return ErrInvalidStateTransition
	}
	v.Status = VideoStatusFailed
	v.ErrorMessage = reason
	v.UpdatedAt = time.Now().UTC()
	return nil
}

func generateUUID() (string, error) {
	var uuid [16]byte
	_, err := rand.Read(uuid[:])
	if err != nil {
		return "", err
	}

	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}