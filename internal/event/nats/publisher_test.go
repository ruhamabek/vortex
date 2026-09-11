package nats_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"
     natssvc "github.com/ruhamabek/vortex/internal/event/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/ruhamabek/vortex/internal/domain"
)

const (
	testNatsURL    = "nats://localhost:4222"
	testStreamName = "VORTEX_TEST"
)

func setupTestNATS(t *testing.T)(*nats.Conn, jetstream.JetStream){
	nc, err := nats.Connect(testNatsURL)
	if err != nil {
		t.Fatalf("failed to connect to test nats: %v", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		t.Fatalf("failed to create jetstream context: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      testStreamName,
		Subjects:  []string{"videos.>"},
		Storage:   jetstream.MemoryStorage, 
		Retention: jetstream.InterestPolicy,
	})

	if err != nil {
		nc.Close()
		t.Fatalf("failed to create test stream: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()
		_=js.DeleteStream(cleanupCtx, testStreamName)
		nc.Close()
	})

	return nc, js
}

func TestNATSEventPublisher_PublishVideoUploaded(t *testing.T){
	_, js := setupTestNATS(t)
	publisher := natssvc.NewNATSEventPublisher(js)
	ctx := context.Background()

	event := domain.VideoUploadedEvent{
		VideoID: "vid-99-abc",
		UserID: "user_42",
		SourceURL: "raw/user-42/clip.mp4",
		OriginalFileName: "clip.mp4",
		OriginalSize: 1024 * 1024 * 50,
		UploadedAt: time.Now().UTC(),
	}
    
	// 1. create consumer to listen for the event
	consumer, err := js.CreateOrUpdateConsumer(ctx, testStreamName, jetstream.ConsumerConfig{
		Name: "test-worker-consumer",
		FilterSubject: "videos.uploaded",
		AckPolicy: jetstream.AckAllPolicy,
	})
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}

	// 2. publish the event
	if err := publisher.PublishVideoUploaded(ctx, event); err != nil {
		t.Fatalf("failed to publidh video uploaded event: %v", err)
	}

	//3. fetch the message from the queue and verify
	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("failed to fetch message from streamm: %v", err)
	}

	var receivedMsg jetstream.Msg
	for msg := range msgs.Messages(){
		receivedMsg = msg
		break
	}

	if receivedMsg == nil {
		t.Fatalf("expected to receive published messagem but got none")
	}
	defer receivedMsg.Ack()

	var receivedEvent domain.VideoUploadedEvent
	if err := json.Unmarshal(receivedMsg.Data(), &receivedEvent); err != nil{
		t.Fatalf("failed to unmarshal received event: %v", err)
	}

	if receivedEvent.VideoID != event.VideoID {
		t.Errorf("expected videoID %v, got %v", event.VideoID, receivedEvent.VideoID)
	}

	if receivedEvent.SourceURL != event.SourceURL {
		t.Errorf("expected sourceURL %v, got %v", event.SourceURL, receivedEvent.SourceURL)
	}

}