package transcoder_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruhamabek/vortex/internal/transcoder"
)

func generateSyntheticVideo(t *testing.T, outputPath string){
	t.Helper()
	cmd := exec.Command(
    "ffmpeg", "-y",
    "-f", "lavfi", "-i", "testsrc=duration=2:size=640x360:rate=24",
    "-f", "lavfi", "-i", "sine=frequency=1000:duration=2",
    "-c:v", "mpeg4", "-c:a", "aac",
    "-pix_fmt", "yuv420p",
    outputPath,
    )
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate synthetic video: %v\nOutput: %s", err, string(out))
	}
}

func TestFFmpegTranscoder_TranscodeHLS_Success(t *testing.T){
	tmpDir := t.TempDir()
	inputVideo := filepath.Join(tmpDir, "sample_input.mp4")
	outputDir := filepath.Join(tmpDir, "hls_output")

	generateSyntheticVideo(t, inputVideo)

	engine := transcoder.NewFFmpegTranscoder()

	var progressReports []int
	onProgress := func(percent int){
		progressReports = append(progressReports, percent)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	err := engine.TranscodeHLS(ctx, inputVideo, outputDir, onProgress)

	if err != nil {
		t.Fatalf("expected transcoding to succeed, got %v", err)
	}

	masterPath := filepath.Join(outputDir, "master.m3u8")
	if _, err := os.Stat(masterPath); os.IsNotExist(err){
		t.Fatalf("expected master.m3u8 to exist at %v", masterPath)
	}
	

	masterContent, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatalf("failed to read master.m3u8: %v", err)
	}
	if len(masterContent) == 0 {
		t.Fatalf("master.m3u8 is empty")
	}

	files, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}

	var foundTS bool
	for _,f := range files {
		if filepath.Ext(f.Name()) == ".ts" {
			foundTS = true
			break
		}
	}
	if !foundTS {
		t.Fatalf("expected at least one .ts segment in output dir")
	}


	if len(progressReports) == 0 {
		t.Fatal("expected progress callback to be called at least once")
	}
	lastProgress := progressReports[len(progressReports) - 1]
	if lastProgress != 100 {
		t.Errorf("expected final progress to be 100, got %d", lastProgress)
	}
}

func TestFFmpegTranscoder_NonExistentInput(t *testing.T) {
	tmpDir := t.TempDir()

	outputDir := filepath.Join(tmpDir, "hls_output")
	engine := transcoder.NewFFmpegTranscoder()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.TranscodeHLS(ctx, "/path/does/not/exist.mp4", outputDir, nil)
	if err == nil {
		t.Fatal("expected error for non-existent input file, got nil")
	}
}