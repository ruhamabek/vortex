package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"github.com/ruhamabek/vortex/internal/domain"
)

type Transcoder interface {
	TranscodeHLS(ctx context.Context, inputPath, outputDir string, onProgress func(int)) error
}

type TranscoderWorker struct {
	repo       domain.VideoRepository
	storage    domain.ObjectStorage
	tracker    domain.ProgressTracker
	transcoder Transcoder
}

func NewTranscoderWorker(
	repo domain.VideoRepository,
	storage domain.ObjectStorage,
	tracker domain.ProgressTracker,
	transcoder Transcoder,
) *TranscoderWorker {
	return &TranscoderWorker{
		repo:       repo,
		storage:    storage,
		tracker:    tracker,
		transcoder: transcoder,
	}
}


// ProcessVideo orchestrates the complete transcoding lifecycle for a single video event
func (w *TranscoderWorker) ProcessVideo(ctx context.Context, event domain.VideoUploadedEvent) error {

	//1. fecth video from db
	video, err := w.repo.FindByID(ctx, event.VideoID)
	if err != nil {
		return fmt.Errorf("failed to find video %v: %v", event.VideoID, err)
	}

	//2. transition state to TRANSCODING
	if err := video.StartTranscoding(event.SourceURL); err != nil {
		return fmt.Errorf("invalid state transition to transcoding: %v", err)
	}
	if err := w.repo.Update(ctx, video); err != nil {
		return fmt.Errorf("failed to save transcoding state: %w", err)
	}
	if w.tracker != nil {
		_ = w.tracker.SetProgress(ctx, video.ID, 0, "STARTING")
	}

	//3. create isolated temp workspace on disk
	workDir, err := os.MkdirTemp("", "vortex-transcode-*")
	if err != nil {
		return w.handleFailure(ctx, video, fmt.Errorf("failed to create temp work directory: %w", err))
	}
	defer os.RemoveAll(workDir)

	//4. download raw video from MinIO
	rawReader, err := w.storage.GetObject(ctx, event.SourceURL)
	if err != nil {
		return w.handleFailure(ctx, video, fmt.Errorf("failed to get raw video from storage: %v", err))
	}
	defer rawReader.Close()

	localInputPath := filepath.Join(workDir, "input_"+event.OriginalFileName)
	localFile, err := os.Create(localInputPath)
	if err != nil {
		return w.handleFailure(ctx, video, fmt.Errorf("failed to create local temp input file: %w", err))
	}
	if _, err := io.Copy(localFile, rawReader); err != nil {
		_ = localFile.Close()
		return w.handleFailure(ctx, video, fmt.Errorf("failed to write raw video to disk: %w", err))
	}
	_ = localFile.Close()

	//5. transcode using ffmpeg
	hlsOutputDir := filepath.Join(workDir, "hls")
	if err := os.MkdirAll(hlsOutputDir, 0755); err != nil  {
         return w.handleFailure(ctx, video, fmt.Errorf("failed to create hls output dir: %v", err))
	}

	onProgress := func(percent int){
		if w.tracker != nil {
			_ = w.tracker.SetProgress(ctx, video.ID, percent, "TRANSCODING")
		}
	}

	if err := w.transcoder.TranscodeHLS(ctx, localInputPath, hlsOutputDir, onProgress); err != nil {
		return w.handleFailure(ctx, video, fmt.Errorf("transcoding failed: %w", err))
	}

	//6. upload generated hls files to MinIO
	entries, err := os.ReadDir(hlsOutputDir)
	if err != nil {
		return w.handleFailure(ctx, video, fmt.Errorf("failed to read hls output dir: %w", err))
	}

	for _, entry := range entries {
		if entry.IsDir(){
			continue
		}
        
		filePath := filepath.Join(hlsOutputDir, entry.Name())
		f, err := os.Open(filePath)
		if err != nil {
			return w.handleFailure(ctx, video, fmt.Errorf("failed to open segment file %s: %w", entry.Name(), err))
		}
		stat, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return w.handleFailure(ctx, video, fmt.Errorf("failed to stat segment file %s: %w", entry.Name(), err))
		}
		contentType := "application/octet-stream"

		if strings.HasSuffix(entry.Name(), ".m3u8") {
			contentType = "application/vnd.apple.mpegurl"
		} else if strings.HasSuffix(entry.Name(), ".ts") {
			contentType = "video/mp2t"
		}

		destKey := fmt.Sprintf("hls/%s/%s/%s", video.UserID, video.ID, entry.Name())
		if err := w.storage.PutObject(ctx, destKey, f, stat.Size(), contentType); err != nil {
			_ = f.Close()
			return w.handleFailure(ctx, video, fmt.Errorf("failed to upload segment %s: %w", entry.Name(), err))
		}
		_ = f.Close()
	}

	// 7. Transition domain state to COMPLETED
	masterPlaylistKey := fmt.Sprintf("hls/%s/%s/master.m3u8", video.UserID, video.ID)
	if err := video.MarkCompleted(masterPlaylistKey); err != nil {
		return w.handleFailure(ctx, video, fmt.Errorf("failed to transition video to completed: %w", err))
	}
	if err := w.repo.Update(ctx, video); err != nil {
		return fmt.Errorf("failed to save completed state in database: %w", err)
	}
	if w.tracker != nil {
		_ = w.tracker.SetProgress(ctx, video.ID, 100, "COMPLETED")
	}
	return nil
} 


func (w *TranscoderWorker) handleFailure(ctx context.Context, video *domain.Video, origErr error) error {
	_ = video.MarkFailed(origErr.Error())
	_ = w.repo.Update(ctx, video)
	if w.tracker != nil {
		_ = w.tracker.SetProgress(ctx, video.ID, 0, "FAILED")
	}
	return origErr
}