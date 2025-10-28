package cache

import (
	"context"
	"log"
	"sync"
	"time"
)

type JobResult struct {
	Job      WarmupJob
	Error    error
	Duration time.Duration
}

type WorkerPool struct {
	workers  int
	jobCh    chan WarmupJob
	resultCh chan JobResult
	cache    Cache
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  bool
	mu       sync.RWMutex

	jobsProcessed int64
	totalDuration time.Duration
	errors        int64
}

func NewWorkerPool(workers int, cache Cache) *WorkerPool {
	if workers <= 0 {
		workers = 3
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		workers:  workers,
		jobCh:    make(chan WarmupJob, workers*2),
		resultCh: make(chan JobResult, workers),
		cache:    cache,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (wp *WorkerPool) Start() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.running {
		return
	}

	wp.running = true

	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	go wp.resultCollector()

	log.Printf("🏃 Worker pool started with %d workers", wp.workers)
}

func (wp *WorkerPool) Stop() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.running {
		return
	}

	wp.running = false

	close(wp.jobCh)

	wp.wg.Wait()

	wp.cancel()
	close(wp.resultCh)

	log.Printf("🛑 Worker pool stopped")
}

func (wp *WorkerPool) SubmitJob(job WarmupJob) bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.running {
		return false
	}

	select {
	case wp.jobCh <- job:
		return true
	case <-wp.ctx.Done():
		return false
	default:
		log.Printf("⚠️ Worker pool queue full, dropping job: %s", job.Key)
		return false
	}
}

func (wp *WorkerPool) SubmitJobs(jobs []WarmupJob) int {
	submitted := 0
	for _, job := range jobs {
		if wp.SubmitJob(job) {
			submitted++
		}
	}
	return submitted
}

func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	log.Printf("🔧 Worker %d started", id)

	for {
		select {
		case job, ok := <-wp.jobCh:
			if !ok {
				log.Printf("🔧 Worker %d stopping", id)
				return
			}

			result := wp.processJob(job)

			select {
			case wp.resultCh <- result:
			case <-wp.ctx.Done():
				return
			default:
			}

		case <-wp.ctx.Done():
			log.Printf("🔧 Worker %d cancelled", id)
			return
		}
	}
}

func (wp *WorkerPool) processJob(job WarmupJob) JobResult {
	start := time.Now()

	err := wp.cache.Set(job.Key, job.Data, job.TTL)
	duration := time.Since(start)

	if err != nil {
		log.Printf("❌ Failed to warm cache key %s: %v", job.Key, err)
	} else {
		log.Printf("✅ Warmed cache key %s (priority: %d, duration: %v)", job.Key, job.Priority, duration)
	}

	return JobResult{
		Job:      job,
		Error:    err,
		Duration: duration,
	}
}

func (wp *WorkerPool) resultCollector() {
	for {
		select {
		case result, ok := <-wp.resultCh:
			if !ok {
				return
			}

			wp.mu.Lock()
			wp.jobsProcessed++
			wp.totalDuration += result.Duration
			if result.Error != nil {
				wp.errors++
			}
			wp.mu.Unlock()

		case <-wp.ctx.Done():
			return
		}
	}
}

func (wp *WorkerPool) GetStats() map[string]interface{} {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	avgDuration := time.Duration(0)
	if wp.jobsProcessed > 0 {
		avgDuration = wp.totalDuration / time.Duration(wp.jobsProcessed)
	}

	return map[string]interface{}{
		"workers":        wp.workers,
		"running":        wp.running,
		"jobs_processed": wp.jobsProcessed,
		"total_errors":   wp.errors,
		"avg_duration":   avgDuration.String(),
		"total_duration": wp.totalDuration.String(),
		"queue_length":   len(wp.jobCh),
		"queue_capacity": cap(wp.jobCh),
	}
}

func (wp *WorkerPool) IsRunning() bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.running
}

func (wp *WorkerPool) Resize(newSize int) {
	if newSize <= 0 {
		return
	}

	wasRunning := wp.IsRunning()
	if wasRunning {
		wp.Stop()
	}

	wp.workers = newSize
	wp.jobCh = make(chan WarmupJob, newSize*2)
	wp.resultCh = make(chan JobResult, newSize)

	if wasRunning {
		wp.Start()
	}

	log.Printf("🔄 Worker pool resized to %d workers", newSize)
}
