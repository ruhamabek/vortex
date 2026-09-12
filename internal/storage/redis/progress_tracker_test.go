package redis_test

import (
	"context"
	"testing"
	"time"
    "github.com/ruhamabek/vortex/internal/domain"
	"github.com/redis/go-redis/v9"
	redissvc "github.com/ruhamabek/vortex/internal/storage/redis"
)


const testRedisAddr = "localhost:6379"


func setupTestRedis(t *testing.T) *redis.Client{
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		 t.Fatalf("failed to connect to test redis: %v", err)
	}

	t.Cleanup(func() {
		client.FlushDB(context.Background())
		client.Close()
	})

	return client
}

func TestRedisProgressTracker_Lifecycle(t *testing.T){
	client := setupTestRedis(t)
	ttl := 1 *time.Hour
	tracker := redissvc.NewRedisProgressTracker(client, ttl)
	ctx := context.Background()

	videoID := "vid-test-12345"

	_,_,err := tracker.GetProgress(ctx, videoID)

	if err != domain.ErrProgressNotFound{
		t.Errorf("expected ErrProgressNotFound, got %v", err)
	}

	if err := tracker.SetProgress(ctx, videoID, 25, "Encoding 360p"); err != nil {
		t.Fatalf("failed to set progress:%v", err)
	}

	percent, stage, err := tracker.GetProgress(ctx, videoID)

	if err != nil {
		t.Fatalf("failed to get progress: %v", err)
	}

	if percent != 25{
		t.Errorf("expected percent 25, got %v", percent)
	}

	if stage != "Encoding 360p" {
		t.Errorf("expected stage 'Encoding 360p', got %v", stage)
	}
    
	if err := tracker.SetProgress(ctx, videoID, 100, "Completed"); err != nil {
			t.Fatalf("failed to update progress: %v", err)
	}

	percent, stage, err = tracker.GetProgress(ctx, videoID)
	if err != nil {
		t.Fatalf("failed to get updated progress: %v", err)
	}

	if percent != 100 {
		t.Errorf("expected percent 100, got %d", percent)
	}

	if stage != "Completed" {
		t.Errorf("expected stage 'Completed', got '%s'", stage)
	}
}

func TestRedisProgressTracker_PubSub(t *testing.T){
	client := setupTestRedis(t)
	tracker := redissvc.NewRedisProgressTracker(client, 10*time.Minute)
	videoID := "test-pubsub-video-123"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updateChan, stopSub, err := tracker.SubscribeProgress(ctx, videoID)
	if err != nil {
		t.Fatalf("failed to subscribe to progress: %v", err)
	}
	defer stopSub()

	time.Sleep(50 * time.Millisecond)

	err = tracker.SetProgress(ctx, videoID, 45, "TRANSCODING")
	if err != nil {
		t.Fatalf("failed to set progress: %v", err)
	}

	select {
	case update := <-updateChan:
		  if update.Percent != 45 || update.Stage != "TRANSCODING" {
			     t.Errorf("expected 45%% TRANSCODING, got %d%% %s", update.Percent, update.Stage)
		  }
	case <-time.After(2 * time.Second):
		  t.Fatal("timed out waiting for progress update from pub/sub channel")
	}
}