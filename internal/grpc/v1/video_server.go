package v1


import (
	"context"
	"errors"
	"time"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/ruhamabek/vortex/internal/domain"
	"github.com/ruhamabek/vortex/internal/service"
	videov1 "github.com/ruhamabek/vortex/proto/gen/video/v1"
)
type VideoServer struct {
	videov1.UnimplementedVideoServiceServer
	service *service.VideoService
}
func NewVideoServer(svc *service.VideoService) *VideoServer {
	return &VideoServer{
		service: svc,
	}
}

func (s *VideoServer) GetVideo(ctx context.Context, req *videov1.GetVideoRequest)(*videov1.GetVideoResponse, error){
	if req.GetVideoId() == "" {
		return nil, status.Error(codes.InvalidArgument, "video_id is required")
	}

	video, err := s.service.GetVideo(ctx, req.GetVideoId())
	if err != nil {
		if errors.Is(err, domain.ErrVideoNotFound){
			  return nil, status.Error(codes.NotFound, "video not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get video: %v", err)
	}

	return &videov1.GetVideoResponse{
		Video: toProtoVideo(video),
	}, nil
}

func (s *VideoServer) GetPlaybackURL(ctx context.Context, req *videov1.GetPlaybackURLRequest)(*videov1.GetPlaybackURLResponse, error){
	if req.GetVideoId() == ""{
		return nil, status.Error(codes.InvalidArgument, "video_id is required")
	}

	url, err := s.service.GetPlaybackURL(ctx, req.GetVideoId())
	if err != nil {
		if errors.Is(err, domain.ErrVideoNotFound){
			return nil, status.Error(codes.NotFound, "video not found")
		}
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get playback URL: %v", err)
	}

	return &videov1.GetPlaybackURLResponse{
		PlaybackUrl: url,
	}, nil
}

// streamReader adapts gRPC chunk streaming into a standard io.Reader
type streamReader struct {
	stream videov1.VideoService_UploadVideoServer
	buf    []byte
}

func(r *streamReader) Read(p []byte)(n int, err error){
	if len(r.buf) == 0 {
	req, err := r.stream.Recv()
	if err != nil {
		return 0, err // returns io.EOF when client finishes sending
	}
	chunk := req.GetChunk()
	if len(chunk) == 0 {
		return 0, nil
	}
	r.buf = chunk
    }
	n = copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (s *VideoServer) UploadVideo(stream videov1.VideoService_UploadVideoServer) error{
     req, err := stream.Recv()
	 if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive upload metadata: %v", err)
	 }

	 meta := req.GetMetadata()
	 if meta == nil {
		return status.Error(codes.InvalidArgument, "first message in stream must contain upload metadata")
	 }

	 contentType := meta.GetContentType()
	 if contentType == ""{
		contentType = "video/mp4"
	 }

	 reader := &streamReader{stream: stream}
	 input := service.UploadVideoInput{
		Title:            meta.GetTitle(),
		OriginalFileName: meta.GetOriginalFileName(),
		OriginalSize:     meta.GetFileSize(),
		UserID:           meta.GetUserId(),
		ContentReader:    reader,
		ContentType:      contentType,
	}

	video, err := s.service.UploadVideo(stream.Context(), input)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to upload video: %v", err)
	}
	
	return stream.SendAndClose(&videov1.UploadVideoResponse{
		Video: toProtoVideo(video),
	})
}

func toProtoVideo(v *domain.Video) *videov1.Video {
	var status videov1.VideoStatus
	switch v.Status {
	case domain.VideoStatusPending:
		status = videov1.VideoStatus_VIDEO_STATUS_PENDING
	case domain.VideoStatusUploaded:
		status = videov1.VideoStatus_VIDEO_STATUS_UPLOADED
	case domain.VideoStatusTranscoding:
		status = videov1.VideoStatus_VIDEO_STATUS_TRANSCODING
	case domain.VideoStatusCompleted:
		status = videov1.VideoStatus_VIDEO_STATUS_COMPLETED
	case domain.VideoStatusFailed:
		status = videov1.VideoStatus_VIDEO_STATUS_FAILED
	default:
		status = videov1.VideoStatus_VIDEO_STATUS_UNSPECIFIED
	}
	return &videov1.Video{
		Id:                v.ID,
		Title:             v.Title,
		OriginalFileName:  v.OriginalFileName,
		OriginalSize:      v.OriginalSize,
		UserId:            v.UserID,
		Status:            status,
		SourceUrl:         v.SourceURL,
		MasterPlaylistUrl: v.MasterPlaylistURL,
		ErrorMessage:      v.ErrorMessage,
		CreatedAt:         v.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         v.UpdatedAt.Format(time.RFC3339),
	}
}