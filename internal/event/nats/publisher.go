package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/ruhamabek/vortex/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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

	msg := &nats.Msg{
		Subject: SubjectVideoUploaded,
		Data:    data,
		Header:  make(nats.Header),
	}

	msg.Header.Set("Nats-Msg-Id", event.VideoID)

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))

		_, err = p.js.PublishMsg(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to publish event to subject %s: %w", SubjectVideoUploaded, err)
	}
	return nil
}