package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestWorker(t *testing.T) (*Worker, *redis.Client, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	config := WorkerConfig{
		RedisClient:  client,
		Concurrency:  2,
		PollInterval: time.Millisecond * 100,
		Queues:       []string{"test_queue", "retry_queue"},
	}

	worker := NewWorker(config)
	return worker, client, mr
}

func TestNewWorker(t *testing.T) {
	worker, _, mr := setupTestWorker(t)
	defer mr.Close()

	if worker == nil {
		t.Error("Expected worker to be created")
	}

	if worker.client == nil {
		t.Error("Expected Redis client to be set")
	}

	if len(worker.handlers) != 0 {
		t.Error("Expected empty handlers map initially")
	}

	if len(worker.queues) != 2 {
		t.Errorf("Expected 2 queues, got %d", len(worker.queues))
	}

	if worker.ctx == nil {
		t.Error("Expected context to be initialized")
	}
}

func TestWorker_RegisterHandler(t *testing.T) {
	worker, _, mr := setupTestWorker(t)
	defer mr.Close()

	handler := func(ctx context.Context, job *Job) error {
		return nil
	}

	worker.RegisterHandler(JobTypeEmailNotification, handler)

	if len(worker.handlers) != 1 {
		t.Errorf("Expected 1 handler, got %d", len(worker.handlers))
	}

	if _, exists := worker.handlers[JobTypeEmailNotification]; !exists {
		t.Error("Expected handler to be registered for JobTypeEmailNotification")
	}
}

func TestWorker_StartAndStop(t *testing.T) {
	worker, _, mr := setupTestWorker(t)
	defer mr.Close()

	worker.Start(1)

	time.Sleep(time.Millisecond * 50)

	worker.Stop()

	select {
	case <-worker.ctx.Done():
	default:
		t.Error("Expected context to be cancelled after stop")
	}
}

func TestWorker_ProcessJob_Success(t *testing.T) {
	worker, client, mr := setupTestWorker(t)
	defer mr.Close()

	jobProcessed := false
	var processedJob *Job

	handler := func(ctx context.Context, job *Job) error {
		jobProcessed = true
		processedJob = job
		return nil
	}

	worker.RegisterHandler(JobTypeEmailNotification, handler)

	job := &Job{
		ID:        "test-job-1",
		Type:      JobTypeEmailNotification,
		Payload:   map[string]interface{}{"email": "test@example.com"},
		Attempts:  0,
		MaxTries:  3,
		CreatedAt: time.Now(),
		ProcessAt: time.Now(),
	}

	jobData, _ := json.Marshal(job)
	err := client.RPush(context.Background(), "test_queue", jobData).Err()
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	err = worker.processNextJob()
	if err != nil {
		t.Fatalf("Failed to process job: %v", err)
	}

	if !jobProcessed {
		t.Error("Expected job to be processed")
	}

	if processedJob == nil {
		t.Error("Expected processed job to be captured")
	} else {
		if processedJob.ID != job.ID {
			t.Errorf("Expected job ID %s, got %s", job.ID, processedJob.ID)
		}
		if processedJob.Type != job.Type {
			t.Errorf("Expected job type %s, got %s", job.Type, processedJob.Type)
		}
	}
}

func TestWorker_ProcessJob_NoHandler(t *testing.T) {
	worker, client, mr := setupTestWorker(t)
	defer mr.Close()

	job := &Job{
		ID:        "test-job-2",
		Type:      JobTypeDataExport, 
		Payload:   map[string]interface{}{},
		Attempts:  0,
		MaxTries:  3,
		CreatedAt: time.Now(),
		ProcessAt: time.Now(),
	}

	jobData, _ := json.Marshal(job)
	err := client.RPush(context.Background(), "test_queue", jobData).Err()
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	err = worker.processNextJob()
	if err == nil {
		t.Error("Expected error when processing job without handler")
	}
}

func TestWorker_ProcessJob_HandlerError(t *testing.T) {
	worker, client, mr := setupTestWorker(t)
	defer mr.Close()

	handlerCallCount := 0
	handler := func(ctx context.Context, job *Job) error {
		handlerCallCount++
		return errors.New("handler failed")
	}

	worker.RegisterHandler(JobTypeTaskReminder, handler)

	job := &Job{
		ID:        "test-job-3",
		Type:      JobTypeTaskReminder,
		Payload:   map[string]interface{}{},
		Attempts:  0,
		MaxTries:  2,
		CreatedAt: time.Now(),
		ProcessAt: time.Now(),
	}

	jobData, _ := json.Marshal(job)
	err := client.RPush(context.Background(), "test_queue", jobData).Err()
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	err = worker.processNextJob()
	if err != nil {
		t.Fatalf("Unexpected error during job processing: %v", err)
	}

	if handlerCallCount != 1 {
		t.Errorf("Expected handler to be called once, got %d", handlerCallCount)
	}

	retryQueueLength, err := client.LLen(context.Background(), "retry_queue").Result()
	if err != nil {
		t.Fatalf("Failed to check retry queue length: %v", err)
	}

	if retryQueueLength != 1 {
		t.Errorf("Expected 1 job in retry queue, got %d", retryQueueLength)
	}
}

func TestWorker_ProcessJob_MaxAttemptsReached(t *testing.T) {
	worker, client, mr := setupTestWorker(t)
	defer mr.Close()

	handler := func(ctx context.Context, job *Job) error {
		return errors.New("persistent failure")
	}

	worker.RegisterHandler(JobTypeCleanup, handler)

	job := &Job{
		ID:        "test-job-4",
		Type:      JobTypeCleanup,
		Payload:   map[string]interface{}{},
		Attempts:  2, 
		MaxTries:  2,
		CreatedAt: time.Now(),
		ProcessAt: time.Now(),
	}

	jobData, _ := json.Marshal(job)
	err := client.RPush(context.Background(), "test_queue", jobData).Err()
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	err = worker.processNextJob()
	if err != nil {
		t.Fatalf("Unexpected error during job processing: %v", err)
	}

	deadQueueLength, err := client.LLen(context.Background(), "dead_queue").Result()
	if err != nil {
		t.Fatalf("Failed to check dead queue length: %v", err)
	}

	if deadQueueLength != 1 {
		t.Errorf("Expected 1 job in dead queue, got %d", deadQueueLength)
	}
}

func TestWorker_ProcessJob_FutureProcessTime(t *testing.T) {
	worker, client, mr := setupTestWorker(t)
	defer mr.Close()

	job := &Job{
		ID:        "test-job-5",
		Type:      JobTypeEmailNotification,
		Payload:   map[string]interface{}{},
		Attempts:  0,
		MaxTries:  3,
		CreatedAt: time.Now(),
		ProcessAt: time.Now().Add(time.Hour), 
	}

	jobData, _ := json.Marshal(job)
	err := client.RPush(context.Background(), "test_queue", jobData).Err()
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	err = worker.processNextJob()
	if err != nil {
		t.Fatalf("Unexpected error during job processing: %v", err)
	}

	queueLength, err := client.LLen(context.Background(), "test_queue").Result()
	if err != nil {
		t.Fatalf("Failed to check queue length: %v", err)
	}

	if queueLength != 1 {
		t.Errorf("Expected 1 job back in queue, got %d", queueLength)
	}
}

func TestWorker_ProcessNextJob_EmptyQueue(t *testing.T) {
	worker, _, mr := setupTestWorker(t)
	defer mr.Close()

	err := worker.processNextJob()
	if err != nil {
		t.Errorf("Expected no error with empty queue, got: %v", err)
	}
}

func TestWorker_ProcessNextJob_InvalidJSON(t *testing.T) {
	worker, client, mr := setupTestWorker(t)
	defer mr.Close()

	err := client.RPush(context.Background(), "test_queue", "invalid-json").Err()
	if err != nil {
		t.Fatalf("Failed to enqueue invalid data: %v", err)
	}

	err = worker.processNextJob()
	if err == nil {
		t.Error("Expected error when processing invalid JSON")
	}
}

func TestJobQueue_NewJobQueue(t *testing.T) {
	_, client, mr := setupTestWorker(t)
	defer mr.Close()

	queue := NewJobQueue(client)

	if queue == nil {
		t.Error("Expected job queue to be created")
	}

	if queue.client != client {
		t.Error("Expected Redis client to be set")
	}
}

func TestJobQueue_Enqueue(t *testing.T) {
	_, client, mr := setupTestWorker(t)
	defer mr.Close()

	queue := NewJobQueue(client)

	payload := map[string]interface{}{
		"email": "test@example.com",
		"name":  "Test User",
	}

	err := queue.Enqueue("test_queue", JobTypeEmailNotification, payload)
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	queueLength, err := client.LLen(context.Background(), "test_queue").Result()
	if err != nil {
		t.Fatalf("Failed to check queue length: %v", err)
	}

	if queueLength != 1 {
		t.Errorf("Expected 1 job in queue, got %d", queueLength)
	}

	jobData, err := client.LPop(context.Background(), "test_queue").Result()
	if err != nil {
		t.Fatalf("Failed to pop job: %v", err)
	}

	var job Job
	err = json.Unmarshal([]byte(jobData), &job)
	if err != nil {
		t.Fatalf("Failed to unmarshal job: %v", err)
	}

	if job.Type != JobTypeEmailNotification {
		t.Errorf("Expected job type %s, got %s", JobTypeEmailNotification, job.Type)
	}

	if job.Payload["email"] != payload["email"] {
		t.Errorf("Expected email %s, got %s", payload["email"], job.Payload["email"])
	}

	if job.MaxTries != 3 {
		t.Errorf("Expected MaxTries 3, got %d", job.MaxTries)
	}
}

func TestJobQueue_EnqueueAt(t *testing.T) {
	_, client, mr := setupTestWorker(t)
	defer mr.Close()

	queue := NewJobQueue(client)

	payload := map[string]interface{}{"message": "delayed job"}
	processAt := time.Now().Add(time.Hour)

	err := queue.EnqueueAt("test_queue", JobTypeTaskReminder, payload, processAt)
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	jobData, err := client.LPop(context.Background(), "test_queue").Result()
	if err != nil {
		t.Fatalf("Failed to pop job: %v", err)
	}

	var job Job
	err = json.Unmarshal([]byte(jobData), &job)
	if err != nil {
		t.Fatalf("Failed to unmarshal job: %v", err)
	}

	if job.ProcessAt.Unix() != processAt.Unix() {
		t.Errorf("Expected ProcessAt %v, got %v", processAt, job.ProcessAt)
	}
}

func TestJobQueue_GetQueueSize(t *testing.T) {
	_, client, mr := setupTestWorker(t)
	defer mr.Close()

	queue := NewJobQueue(client)

	size, err := queue.GetQueueSize("test_queue")
	if err != nil {
		t.Fatalf("Failed to get queue size: %v", err)
	}

	if size != 0 {
		t.Errorf("Expected queue size 0, got %d", size)
	}

	for i := 0; i < 3; i++ {
		err := queue.Enqueue("test_queue", JobTypeEmailNotification, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Failed to enqueue job %d: %v", i, err)
		}
	}

	size, err = queue.GetQueueSize("test_queue")
	if err != nil {
		t.Fatalf("Failed to get queue size: %v", err)
	}

	if size != 3 {
		t.Errorf("Expected queue size 3, got %d", size)
	}
}

func TestJobTypes(t *testing.T) {
	tests := []struct {
		jobType  JobType
		expected string
	}{
		{JobTypeEmailNotification, "email_notification"},
		{JobTypeTaskReminder, "task_reminder"},
		{JobTypeDataExport, "data_export"},
		{JobTypeCleanup, "cleanup"},
	}

	for _, tt := range tests {
		if string(tt.jobType) != tt.expected {
			t.Errorf("Expected job type %s, got %s", tt.expected, string(tt.jobType))
		}
	}
}

func BenchmarkWorker_ProcessJob(b *testing.B) {
	mr := miniredis.RunT(&testing.T{})
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	config := WorkerConfig{
		RedisClient:  client,
		Concurrency:  1,
		PollInterval: time.Millisecond,
		Queues:       []string{"bench_queue"},
	}

	worker := NewWorker(config)
	worker.RegisterHandler(JobTypeEmailNotification, func(ctx context.Context, job *Job) error {
		return nil
	})

	queue := NewJobQueue(client)
	for i := 0; i < b.N; i++ {
		payload := map[string]interface{}{"id": i}
		err := queue.Enqueue("bench_queue", JobTypeEmailNotification, payload)
		if err != nil {
			b.Fatalf("Failed to enqueue job: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := worker.processNextJob()
		if err != nil {
			b.Fatalf("Failed to process job: %v", err)
		}
	}
}

func BenchmarkJobQueue_Enqueue(b *testing.B) {
	mr := miniredis.RunT(&testing.T{})
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(client)
	payload := map[string]interface{}{"test": "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := queue.Enqueue("bench_queue", JobTypeEmailNotification, payload)
		if err != nil {
			b.Fatalf("Failed to enqueue job: %v", err)
		}
	}
}
