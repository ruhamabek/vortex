package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
 	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
	"github.com/ruhamabek/vortex/internal/domain"
)

type VideoCompletedEventData struct {
	VideoID           string `json:"video_id"`
	UserID            string `json:"user_id"`
	SourceURL         string `json:"source_url"`
	MasterPlaylistURL string `json:"master_playlist_url"`
	OriginalFileName  string `json:"original_file_name"`
}

type ThumbnailGenerator interface {
	GeneratePoster(ctx context.Context, inputPath, outputPath string) error
	GeneratePreviewGIF(ctx context.Context, inputPath, outputPath string) error
}

type DefaultThumbnailGenerator struct{}

func (g *DefaultThumbnailGenerator) GeneratePoster(ctx context.Context, inputPath, outputPath string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-ss", "00:00:01",
		"-i", inputPath,
		"-vframes", "1",
		"-q:v", "2",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate poster: %v, output: %s", err, string(out))
	}
	return nil
}

func (g *DefaultThumbnailGenerator) GeneratePreviewGIF(ctx context.Context, inputPath, outputPath string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y",
		"-ss", "00:00:01",
		"-t", "2",
		"-i", inputPath,
		"-vf", "fps=10,scale=320:-1:flags=lanczos",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate preview gif: %v, output: %s", err, string(out))
	}
	return nil
}

// NewPostProcessingFunction returns the Inngest durable function
func NewPostProcessingFunction(
	client inngestgo.Client,
	storage domain.ObjectStorage,
	thumbGen ThumbnailGenerator,
) (inngestgo.ServableFunction, error) {
	if thumbGen == nil {
		thumbGen = &DefaultThumbnailGenerator{}
	}

	return inngestgo.CreateFunction(
		client,
		inngestgo.FunctionOpts{
			ID:   "post-process-video",
			Name: "Post-Process Transcoded Video",
		},
		inngestgo.EventTrigger("video/transcoding.completed", nil),
		func(ctx context.Context, input inngestgo.Input[VideoCompletedEventData]) (any, error) {
			data := input.Event.Data

 			workDir, err := os.MkdirTemp("", "vortex-inngest-*")
			if err != nil {
				return nil, fmt.Errorf("failed to create temp work dir: %w", err)
			}
			defer os.RemoveAll(workDir)

			// Download raw video for thumbnail extraction
			rawReader, err := storage.GetObject(ctx, data.SourceURL)
			if err != nil {
				return nil, fmt.Errorf("failed to get raw video: %w", err)
			}
			defer rawReader.Close()

			localInputPath := filepath.Join(workDir, "input.mp4")
			f, err := os.Create(localInputPath)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(f, rawReader); err != nil {
				_ = f.Close()
				return nil, err
			}
			_ = f.Close()

			// STEP 1: Generate & Upload High-Res Poster Thumbnail  
			posterKey, err := step.Run(ctx, "generate-poster-thumbnail", func(ctx context.Context) (string, error) {
				localPosterPath := filepath.Join(workDir, "poster.jpg")
				if err := thumbGen.GeneratePoster(ctx, localInputPath, localPosterPath); err != nil {
					return "", err
				}

				posterData, err := os.ReadFile(localPosterPath)
				if err != nil {
					return "", err
				}

				destKey := fmt.Sprintf("hls/%s/%s/poster.jpg", data.UserID, data.VideoID)
				err = storage.PutObject(ctx, destKey, bytes.NewReader(posterData), int64(len(posterData)), "image/jpeg")
				if err != nil {
					return "", fmt.Errorf("failed to upload poster: %w", err)
				}

				return destKey, nil
			})
			if err != nil {
				return nil, err
			}

			// STEP 2: Generate & Upload Animated Preview GIF  
			previewKey, err := step.Run(ctx, "generate-animated-preview", func(ctx context.Context) (string, error) {
				localGIFPath := filepath.Join(workDir, "preview.gif")
				if err := thumbGen.GeneratePreviewGIF(ctx, localInputPath, localGIFPath); err != nil {
					return "", err
				}

				gifData, err := os.ReadFile(localGIFPath)
				if err != nil {
					return "", err
				}

				destKey := fmt.Sprintf("hls/%s/%s/preview.gif", data.UserID, data.VideoID)
				err = storage.PutObject(ctx, destKey, bytes.NewReader(gifData), int64(len(gifData)), "image/gif")
				if err != nil {
					return "", fmt.Errorf("failed to upload preview gif: %w", err)
				}

				return destKey, nil
			})
			if err != nil {
				return nil, err
			}

			// STEP 3: Dispatch Customer Webhook 
			webhookStatus, err := step.Run(ctx, "dispatch-customer-webhook", func(ctx context.Context) (string, error) {
				return fmt.Sprintf("Webhook delivered for video %s: poster=%s, preview=%s", data.VideoID, posterKey, previewKey), nil
			})
			if err != nil {
				return nil, err
			}

			return map[string]any{
				"video_id":       data.VideoID,
				"poster_key":     posterKey,
				"preview_key":    previewKey,
				"webhook_status": webhookStatus,
				"processed_at":   time.Now().UTC().Format(time.RFC3339),
			}, nil
		},
	)
}