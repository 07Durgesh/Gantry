package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// BuildJob represents the payload sent from the API to the background workers.
type BuildJob struct {
	DeploymentID string    `json:"deployment_id"`
	AppName      string    `json:"app_name"`
	SrcPath      string    `json:"src_path"`
	CreatedAt    time.Time `json:"created_at"`
}

// RedisQueue manages asynchronous job dispatching using Redis lists.
//
// Analogy: The Ticket Wheel at Starbucks.
// Cashiers clip new drink orders onto the wheel (Enqueue).
// Baristas take tickets off the wheel when their hands are free (Dequeue).
type RedisQueue struct {
	client       *redis.Client
	pendingKey   string
	processingKey string
}

// NewRedisQueue initializes a queue connected to Redis.
func NewRedisQueue(addr string) (*RedisQueue, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis at %s: %w", addr, err)
	}

	return &RedisQueue{
		client:        rdb,
		pendingKey:    "gantry:build_queue:pending",
		processingKey: "gantry:build_queue:processing",
	}, nil
}

// Enqueue pushes a new build job onto the pending queue (LPUSH).
func (q *RedisQueue) Enqueue(ctx context.Context, job BuildJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to serialize job: %w", err)
	}

	return q.client.LPush(ctx, q.pendingKey, data).Err()
}

// Dequeue atomically moves a job from pending to processing and returns it (BLMOVE / BRPOPLPUSH).
//
// Technical Nuance (The Drop-Safe Tray):
// If the worker crashes mid-build, the job isn't lost because it still sits
// in the processingKey list, allowing a supervisor to recover stalled jobs.
func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) (*BuildJob, error) {
	// BLMove: Atomically pops from right of pending and pushes to left of processing
	result, err := q.client.BLMove(ctx, q.pendingKey, q.processingKey, "RIGHT", "LEFT", timeout).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Timed out waiting for job
		}
		return nil, err
	}

	var job BuildJob
	if err := json.Unmarshal([]byte(result), &job); err != nil {
		return nil, fmt.Errorf("corrupt job payload in queue: %w", err)
	}

	return &job, nil
}

// Acknowledge removes the job from the processing list once completed successfully.
func (q *RedisQueue) Acknowledge(ctx context.Context, job *BuildJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.client.LRem(ctx, q.processingKey, 1, data).Err()
}

// PublishLog sends a real-time log chunk to anyone listening on this deployment's channel.
func (q *RedisQueue) PublishLog(ctx context.Context, deploymentID string, message string) error {
	channel := fmt.Sprintf("gantry:logs:%s", deploymentID)
	return q.client.Publish(ctx, channel, message).Err()
}

// SubscribeLogs returns a Redis PubSub channel that listens to live logs for a deployment.
func (q *RedisQueue) SubscribeLogs(ctx context.Context, deploymentID string) *redis.PubSub {
	channel := fmt.Sprintf("gantry:logs:%s", deploymentID)
	return q.client.Subscribe(ctx, channel)
}

// Close closes the Redis connection.
func (q *RedisQueue) Close() error {
	return q.client.Close()
}
