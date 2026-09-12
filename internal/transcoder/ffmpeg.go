package transcoder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ProgressCallback func(percent int)

type FFmpegTranscoder struct {
	videoCodec string
}

func NewFFmpegTranscoder() *FFmpegTranscoder {
	codec := "mpeg2video"

	out, err := exec.Command("ffmpeg", "-encoders").CombinedOutput()
	if err == nil && strings.Contains(string(out), "libx264") {
		codec = "libx264"
	}
	return &FFmpegTranscoder{
		videoCodec: codec,
	}
}

func (t *FFmpegTranscoder) TranscodeHLS(
	ctx context.Context,
	inputPath string,
	outputDir string,
	onProgress ProgressCallback,
) error {
	if _, err := os.Stat(inputPath); err != nil {
		return fmt.Errorf("input video does not exist: %v", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	durationSec, _ := t.getVideoDuration(ctx, inputPath)
    
	masterPath := "master.m3u8"
	variantPath := filepath.Join(outputDir, "stream.m3u8")
	segmentPattern := filepath.Join(outputDir, "segment_%03d.ts")

	args := []string{
		"-y",
		"-i", inputPath,
		"-c:v", t.videoCodec,
		"-b:v", "2000k",
		"-c:a", "aac",
		"-b:a", "128k",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", segmentPattern,
		"-master_pl_name", masterPath,
		"-progress", "pipe:1",
		variantPath,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to open stdout: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to open stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}


	doneProgress := make(chan struct{})
	go func(){
		defer close(doneProgress)
		t.parseProgress(stdout, durationSec, onProgress)
	}()

	err = cmd.Wait()
	<-doneProgress

	if err != nil {
		errBytes,_ := io.ReadAll(stderr)
		return fmt.Errorf("ffmpeg execution failed: %v, details: %s", err, string(errBytes))
	}

	if onProgress != nil {
		onProgress(100)
	}

	return nil
}

func(f *FFmpegTranscoder) parseProgress(r io.Reader, durationSec float64, onProgress ProgressCallback){
	if onProgress == nil {
		return
	}

	scanner := bufio.NewScanner(r)
	lastPercent := -1

	for scanner.Scan(){
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if key == "out_time_us" || key == "out_time_ms" {
			valInt, err := strconv.ParseInt(val, 10, 64)
			if err == nil && durationSec > 0 {
				var currentTimeSec float64
				if key == "out_time_us" {
					currentTimeSec = float64(valInt) / 1_000_000.0
				} else {
					currentTimeSec = float64(valInt) / 1_000.0
				}
				percent := int((currentTimeSec / durationSec) * 100)
				if percent > 99 {
					percent = 99
				}
				if percent > lastPercent {
					lastPercent = percent
					onProgress(percent)
				}
			}
		} else if key == "progress" && val == "end" {
			if lastPercent < 100 {
				lastPercent = 100
				onProgress(100)
			}
		}
	}
}
func (t *FFmpegTranscoder) getVideoDuration(ctx context.Context, inputPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(string(out))
	val, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, errors.New("failed to parse duration")
	}
	return val, nil
}