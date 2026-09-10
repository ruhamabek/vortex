package domain_test

import (
	"testing"

	"github.com/ruhamabek/vortex/internal/domain"
)

func TestNewVideo_Valid(t *testing.T){
	  title := "my first action movie"
	  originalFileName := "raw_footage.mov"
	  var sizeBytes int64 = 1024 * 1024 * 250
	  userID := "user-123"

      video, err := domain.NewVideo(title, originalFileName, sizeBytes, userID)
   
      if err != nil {
		  t.Fatalf("expected no error and got %v", err)
	  }

	  if video.ID == ""{
		t.Errorf("expected a video ID to be generated got nothing")
	  }

	  if video.Status != domain.VideoStatusPending {
		t.Errorf("expected status %v, got %v", domain.VideoStatusPending, video.Status)
	  }

	  if video.Title != title {
		t.Errorf("expected title %v, got %v", title, video.Title)
	  }

}

func TestNewVideo_ValidationErrors(t *testing.T) {
	tests := []struct {
		name             string
		title            string
		originalFilename string
		sizeBytes        int64
		userID           string
		expectedErr      error
	}{
		{
			name:             "empty title",
			title:            "",
			originalFilename: "clip.mp4",
			sizeBytes:        1000,
			userID:           "user-1",
			expectedErr:      domain.ErrInvalidTitle,
		},
		{
			name:             "zero file size",
			title:            "Clip",
			originalFilename: "clip.mp4",
			sizeBytes:        0,
			userID:           "user-1",
			expectedErr:      domain.ErrInvalidFileSize,
		},
		{
			name:             "file size exceeds 2GB maximum limit",
			title:            "Huge Movie",
			originalFilename: "huge.mp4",
			sizeBytes:        domain.MaxVideoSizeBytes + 1,
			userID:           "user-1",
			expectedErr:      domain.ErrFileTooLarge,
		},
		{
			name:             "empty user ID",
			title:            "Clip",
			originalFilename: "clip.mp4",
			sizeBytes:        1000,
			userID:           "",
			expectedErr:      domain.ErrInvalidUserID,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewVideo(tc.title, tc.originalFilename, tc.sizeBytes, tc.userID)
			if err != tc.expectedErr {
				t.Fatalf("expected error %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

func TestVideo_StateTransitions(t *testing.T){
	t.Run("valid full happpy path lifecycle", func(t *testing.T) {
		video, err := domain.NewVideo("testing", "test.mp4", 5000, "user-1")

		if err != nil {
			t.Fatalf("Failed to create new video: %v", err)
		}
       
	   //1. Pending -> Uploaded
       storagePath := "raw/user-1/test.mp4"

	   if err := video.MarkUploaded(storagePath); err != nil {
		     t.Fatalf("expected valid upload phase transtion: %v", err)
	   }

	   if video.Status != domain.VideoStatusUploaded {
		    t.Errorf("expected status %v, got %v", domain.VideoStatusUploaded, video.Status)
	   }

	   if video.SourceURL != storagePath {
		  t.Errorf("expected sourceurl %v, got %v", storagePath, video.SourceURL)
	   }

	   //2. Uploaded -> Transcoding      
	   if err := video.StartTranscoding(video.SourceURL); err != nil {
	       t.Fatalf("expected valid transcoding transition, got: %v", err)
	   }

	   if video.Status != domain.VideoStatusTranscoding {
		   t.Errorf("expected status %v, got %v", domain.VideoStatusTranscoding, video.Status)
	   }

	   // 3. Transcoding -> Completed
	   masterPlaylistURL := "hls/user-1/master.m3u8"

	   if err := video.MarkCompleted(masterPlaylistURL); err != nil {
		   t.Fatalf("expected valid completion transtion, got: %v", err)
	   }

	   if video.Status != domain.VideoStatusCompleted{
		   t.Errorf("expected status %s, got %s", domain.VideoStatusCompleted, video.Status)
	   }

	   if video.MasterPlaylistURL != masterPlaylistURL {
		  t.Errorf("expected playlisst URL %v, got %v", masterPlaylistURL, video.MasterPlaylistURL )
	   }

	})

	t.Run("failure can occur from transcoding state", func(t *testing.T) {
		   video, _ := domain.NewVideo("testing", "test.mp4", 5000, "user-1")
		   _ = video.MarkUploaded("raw/test.mp4")
		   _ = video.StartTranscoding("raw/test.mp4")

		   failReason := "ffmpeg: corrupt input bitstream"
				if err := video.MarkFailed(failReason); err != nil {
					t.Fatalf("expected valid failure transition, got %v", err)
				}
				if video.Status != domain.VideoStatusFailed {
					t.Errorf("expected status %s, got %s", domain.VideoStatusFailed, video.Status)
				}
				if video.ErrorMessage != failReason {
					t.Errorf("expected error reason %s, got %s", failReason, video.ErrorMessage)
				}
	})
}
