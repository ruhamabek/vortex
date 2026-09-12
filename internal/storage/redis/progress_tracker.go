package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ruhamabek/vortex/internal/domain"
)


type RedisProgressTracker struct {
	client *redis.Client
	ttl time.Duration
}


type ProgressUpdate struct {
	VideoID   string `json:"video_id"`
	Percent   int    `json:"percent"`
	Stage     string `json:"stage"`
	UpdatedAt string `json:"updated_at"`
}

func NewRedisProgressTracker(client *redis.Client, ttl time.Duration) *RedisProgressTracker{
	return &RedisProgressTracker{
		client: client,
		ttl: ttl,
	}
}

func (r *RedisProgressTracker) key(videoID string) string {
	return "vortex:progress:" + videoID
}

func (r *RedisProgressTracker) channel(videoID string) string {
	return "vortex:channel:progress:" + videoID
}

func (r *RedisProgressTracker) SetProgress(ctx context.Context, videoID string, progressPercent int, stage string) error {
	key := r.key(videoID)
	nowStr := time.Now().UTC().Format(time.RFC3339)

	update := ProgressUpdate{
		VideoID:   videoID,
		Percent:   progressPercent,
		Stage:     stage,
		UpdatedAt: nowStr,
	}

	payload, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal progress update: %w", err)
	}

	data := map[string]any{
		"percent":    progressPercent,
		"stage":      stage,
		"updated_at": nowStr,
	}

	pipe := r.client.Pipeline()
	pipe.HSet(ctx, key, data)
	pipe.Expire(ctx, key, r.ttl)
	pipe.Publish(ctx, r.channel(videoID), payload)  

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save progress to redis: %v", err)
	}

	return nil
}

func (r *RedisProgressTracker) GetProgress(ctx context.Context, videoID string)(int, string, error){
	key := r.key(videoID)

	data, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get progress from redis: %v", err)
	}

	if len(data) == 0 {
		return 0, "", domain.ErrProgressNotFound
	}

	percentStr, ok := data["percent"]

	if !ok {
		return 0, "", domain.ErrProgressNotFound
	}

	percent, err := strconv.Atoi(percentStr)

	if err != nil {
		return 0, "", fmt.Errorf("corrupt percent in redis: %v", err)
	}

	stage := data["stage"]

	return percent, stage, nil
}

func (r *RedisProgressTracker) SubscribeProgress(ctx context.Context, videoID string) (<-chan ProgressUpdate, func(), error) {
	pubsub := r.client.Subscribe(ctx, r.channel(videoID))
 	_, err := pubsub.Receive(ctx)
	if err != nil {
		_ = pubsub.Close()
		return nil, nil, fmt.Errorf("failed to connect to redis pubsub: %w", err)
	}
	outChan := make(chan ProgressUpdate, 16)
	subChannel := pubsub.Channel()
	subCtx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(outChan)
		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-subChannel:
				if !ok {
					return
				}
				var update ProgressUpdate
				if err := json.Unmarshal([]byte(msg.Payload), &update); err == nil {
					select {
					case outChan <- update:
					case <-subCtx.Done():
						return
					}
				}
			}
		}
	}()
	stop := func() {
		cancel()
		_ = pubsub.Close()
	}
	return outChan, stop, nil
}