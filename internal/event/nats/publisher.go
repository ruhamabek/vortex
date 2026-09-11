package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/ruhamabek/vortex/internal/domain"
)

const (
	SubjectVideoUploaded = "videos.uploaded"
)

type NATSEventPublisher struct {
	js jetstream.JetStream
}

func NewNATSEventPublisher(js jetstream.JetStream) *NATSEventPublisher{
	return &NATSEventPublisher{
		js: js,
	}
}

func (p *NATSEventPublisher) PublishVideoUploaded(ctx context.Context, event domain.VideoUploadedEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal video uploaded event: %v", err)
	}

	_, err = p.js.Publish(ctx, SubjectVideoUploaded, data, jetstream.WithMsgID(event.VideoID))
	if err != nil {
		return fmt.Errorf("failed top publish event to subject %s: %v", SubjectVideoUploaded, err)
	}

	return nil
}