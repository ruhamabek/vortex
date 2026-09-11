package redis

import (
	"context"
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

func NewRedisProgressTracker(client *redis.Client, ttl time.Duration) *RedisProgressTracker{
	return &RedisProgressTracker{
		client: client,
		ttl: ttl,
	}
}

func (r *RedisProgressTracker) key(videoID string) string {
	return "vortex:progress:" + videoID
}

func (r *RedisProgressTracker) SetProgress(ctx context.Context, videoID string, progressPercent int, stage string) error {
	key := r.key(videoID)

	data := map[string]any{
		  "percent": progressPercent,
		  "stage": stage,
		  "updated_at": time.Now().UTC().Format(time.RFC3339),
	}

	pipe := r.client.Pipeline()
	pipe.HSet(ctx, key, data)
	pipe.Expire(ctx, key, r.ttl)

	_,err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save progress to redis:%v", err)
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