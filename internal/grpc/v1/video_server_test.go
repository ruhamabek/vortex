package v1_test


import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
	"google.golang.org/grpc"
 	"google.golang.org/grpc/credentials/insecure"
 	"google.golang.org/grpc/test/bufconn"
	"github.com/ruhamabek/vortex/internal/domain"
	grpcsvc "github.com/ruhamabek/vortex/internal/grpc/v1"
	"github.com/ruhamabek/vortex/internal/service"
	videov1 "github.com/ruhamabek/vortex/proto/gen/video/v1"
)

const bufSize = 1024 * 1024

type mockRepo struct{
	videos map[string]*domain.Video
}

func (m *mockRepo) Save(ctx context.Context, v *domain.Video) error {
	m.videos[v.ID] = v
	return nil
}

func (m *mockRepo) FindByID(ctx context.Context, id string)(*domain.Video, error){
	v, ok := m.videos[id]
	if !ok {
		return nil, domain.ErrVideoNotFound
	}

	return v, nil
}

func (m *mockRepo) Update(ctx context.Context, v *domain.Video) error {
	m.videos[v.ID] = v
	return nil
}
func (m *mockRepo) ListByUserID(ctx context.Context, userID string, limit, offset int)([]*domain.Video, error){
	return nil, nil
}

type mockStorage struct {
	objects map[string][]byte
}

func (m *mockStorage) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.objects[key] = data
	return nil
}

func (m *mockStorage) GetObject(ctx context.Context, key string)(io.ReadCloser, error){
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStorage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration)(string, error){
	return "https://storage.vortex.dev/" + key, nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

type mockPublisher struct{}

func (m *mockPublisher) PublishVideoUploaded(ctx context.Context, event domain.VideoUploadedEvent) error {
	return nil
}

func setupGRPCTestServer(t *testing.T) (videov1.VideoServiceClient, *mockRepo, *mockStorage){
	t.Helper()

	lis := bufconn.Listen(bufSize)
	baseServer := grpc.NewServer()

	repo := &mockRepo{videos: make(map[string]*domain.Video)}
	storage := &mockStorage{objects: make(map[string][]byte)}
	publisher := &mockPublisher{}

	videoService := service.NewVideoService(repo, storage, publisher)
	server := grpcsvc.NewVideoServer(videoService)

	videov1.RegisterVideoServiceServer(baseServer, server)

	go func ()  {
		 if err := baseServer.Serve(lis); err != nil {
			return
		 }
	}()

	t.Cleanup(func ()  {
		baseServer.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough://bufnet", grpc.WithContextDialer(
		func (context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return  videov1.NewVideoServiceClient(conn), repo, storage
}

func TestVideoServer_GetVideo_Sucess(t *testing.T){
	client, repo, _ := setupGRPCTestServer(t)

	video, _ := domain.NewVideo("test", "test.mp4", 5000, "user-123")
	_ = repo.Save(context.Background(), video)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.GetVideo(ctx, &videov1.GetVideoRequest{VideoId: video.ID})
    if err != nil{
		t.Fatalf("excpected to succeed, got %v", err)
	}

	if resp.Video.Id != video.ID {
		t.Errorf("expected video id %v, got %v", video.ID, resp.Video.Id)
	}
	if resp.Video.Title != "test" {
		t.Errorf("expected title 'Mountain Drone Shot', got %s", resp.Video.Title)
	}
	if resp.Video.Status != videov1.VideoStatus_VIDEO_STATUS_PENDING {
		t.Errorf("expected status PENDING, got %v", resp.Video.Status)
	}
}


func TestVideoServer_UploadVideo_StreamingSuccess(t *testing.T) {
	client, _, storage := setupGRPCTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.UploadVideo(ctx)
	if err != nil {
		t.Fatalf("failed to open upload stream: %v", err)
	}

	// message 1: metadata
	err = stream.Send(&videov1.UploadVideoRequest{
		Payload: &videov1.UploadVideoRequest_Metadata{
			Metadata: &videov1.UploadMetadata{
				Title:            "Skate Park Kickflip",
				OriginalFileName: "skate.mp4",
				UserId:           "user-skater",
				ContentType:      "video/mp4",
				FileSize:         30, 
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to send metadata: %v", err)
	}

	// message 2: First binary chunk (15 bytes)
	err = stream.Send(&videov1.UploadVideoRequest{
		Payload: &videov1.UploadVideoRequest_Chunk{
			Chunk: []byte("PART_ONE_BYTES_"),
		},
	})

	if err != nil {
		t.Fatalf("failed to send chunk 1: %v", err)
	}

	// message 3: Second binary chunk (15 bytes)
	err = stream.Send(&videov1.UploadVideoRequest{
		Payload: &videov1.UploadVideoRequest_Chunk{
			Chunk: []byte("PART_TWO_BYTES_"),
		},
	})
	if err != nil {
		t.Fatalf("failed to send chunk 2: %v", err)
	}

	// Close stream and receive the created Video
	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("failed to close and receive upload response: %v", err)
	}


	if resp.Video.Title != "Skate Park Kickflip" {
		t.Errorf("expected title 'Skate Park Kickflip', got %s", resp.Video.Title)
	}
	if resp.Video.Status != videov1.VideoStatus_VIDEO_STATUS_UPLOADED {
		t.Errorf("expected status UPLOADED, got %v", resp.Video.Status)
	}

	//verify

	var totalStorageBytes []byte
	for _, data := range storage.objects{
		totalStorageBytes = append(totalStorageBytes, data...)
	}
	if string(totalStorageBytes) != "PART_ONE_BYTES_PART_TWO_BYTES_" {
		t.Errorf("expected storage payload 'PART_ONE_BYTES_PART_TWO_BYTES_', got %s", string(totalStorageBytes))
	}
}