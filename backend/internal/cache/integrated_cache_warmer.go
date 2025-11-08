package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type IntegratedCacheWarmer struct {
	cache       Cache
	redisClient *redis.Client
	strategy    *WarmupStrategy
	mu          sync.RWMutex
	running     bool
	stopCh      chan struct{}

	distributedWorkers []*DistributedCacheWorker
	localWorkerPool    *WorkerPool
	jobRouter          *JobRouter

	scheduler     *JobScheduler
	priorityQueue *PriorityQueue

	processedJobs   int64
	failedJobs      int64
	retryJobs       int64
	distributedJobs int64
	localJobs       int64
}

type JobRouter struct {
	redisAvailable   bool
	localCapacity    int
	distributedQueue string
	mu               sync.RWMutex
}

type DistributedCacheWorker struct {
	id          string
	warmer      *IntegratedCacheWarmer
	redisClient *redis.Client
	stopCh      chan struct{}
	wg          sync.WaitGroup
	queueName   string
}

type CacheJobType string

const (
	CacheJobWarmup     CacheJobType = "cache_warmup"
	CacheJobBatch      CacheJobType = "cache_batch"
	CacheJobEviction   CacheJobType = "cache_eviction"
	CacheJobValidation CacheJobType = "cache_validation"
	CacheJobScheduled  CacheJobType = "cache_scheduled"
)

type DistributedCacheJob struct {
	ID        string                 `json:"id"`
	Type      CacheJobType           `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Attempts  int                    `json:"attempts"`
	MaxTries  int                    `json:"max_tries"`
	CreatedAt time.Time              `json:"created_at"`
	ProcessAt time.Time              `json:"process_at"`
	Priority  int                    `json:"priority"`
}

type JobRoutingStrategy struct {
	PreferDistributed    bool          
	LocalFallback        bool          
	DistributedThreshold int           
	MaxLocalJobs         int           
	RoutingTimeout       time.Duration 
}

func NewIntegratedCacheWarmer(cache Cache, redisClient *redis.Client, strategy *WarmupStrategy) *IntegratedCacheWarmer {
	if strategy == nil {
		strategy = &WarmupStrategy{
			BatchSize:      10,
			ConcurrentJobs: 3,
			WarmupInterval: 5 * time.Minute,
			UseWorkerPool:  true,
			UseScheduler:   true,
		}
	}

	icw := &IntegratedCacheWarmer{
		cache:       cache,
		redisClient: redisClient,
		strategy:    strategy,
		stopCh:      make(chan struct{}),
	}

	icw.jobRouter = &JobRouter{
		redisAvailable:   redisClient != nil,
		localCapacity:    strategy.ConcurrentJobs,
		distributedQueue: "cache_jobs",
	}

	icw.localWorkerPool = NewWorkerPool(strategy.ConcurrentJobs, cache)

	if strategy.UseScheduler {
		
		legacyWarmer := &CacheWarmer{
			cache:    cache,
			strategy: strategy,
		}
		icw.scheduler = NewJobScheduler(legacyWarmer, icw.localWorkerPool)
	}
	icw.priorityQueue = NewPriorityQueue()

	if redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Printf("Redis unavailable, falling back to local-only mode: %v", err)
			icw.jobRouter.redisAvailable = false
		}
	}

	log.Printf("Integrated cache warmer initialized (Redis: %v)", icw.jobRouter.redisAvailable)
	return icw
}

func (icw *IntegratedCacheWarmer) Start() error {
	icw.mu.Lock()
	defer icw.mu.Unlock()

	if icw.running {
		return fmt.Errorf("integrated cache warmer is already running")
	}

	icw.running = true

	icw.localWorkerPool.Start()

	if icw.jobRouter.redisAvailable {
		for i := 0; i < icw.strategy.ConcurrentJobs; i++ {
			worker := &DistributedCacheWorker{
				id:          fmt.Sprintf("distributed-worker-%d", i),
				warmer:      icw,
				redisClient: icw.redisClient,
				stopCh:      make(chan struct{}),
				queueName:   icw.jobRouter.distributedQueue,
			}
			icw.distributedWorkers = append(icw.distributedWorkers, worker)
			go worker.start()
		}
		log.Printf("Started %d distributed cache workers", len(icw.distributedWorkers))
	}

	if icw.scheduler != nil {
		icw.scheduler.Start()
	}

	log.Printf("Integrated cache warmer started (distributed workers: %d, local workers: %d)",
		len(icw.distributedWorkers), icw.strategy.ConcurrentJobs)
	return nil
}

func (icw *IntegratedCacheWarmer) Stop() error {
	icw.mu.Lock()
	defer icw.mu.Unlock()

	if !icw.running {
		return nil
	}

	close(icw.stopCh)

	for _, worker := range icw.distributedWorkers {
		close(worker.stopCh)
		worker.wg.Wait()
	}

	icw.localWorkerPool.Stop()

	if icw.scheduler != nil {
		icw.scheduler.Stop()
	}

	icw.running = false
	log.Println("Integrated cache warmer stopped")
	return nil
}

func (icw *IntegratedCacheWarmer) EnqueueWarmupJob(key string, data interface{}, ttl time.Duration, priority int) error {
	job := DistributedCacheJob{
		ID:   fmt.Sprintf("%d", time.Now().UnixNano()),
		Type: CacheJobWarmup,
		Payload: map[string]interface{}{
			"key":  key,
			"data": data,
			"ttl":  ttl.String(),
		},
		MaxTries:  3,
		CreatedAt: time.Now(),
		ProcessAt: time.Now(),
		Priority:  priority,
	}

	return icw.routeJob(job)
}

func (icw *IntegratedCacheWarmer) EnqueueBatchWarmupJob(keys []string, data interface{}, priority int) error {
	job := DistributedCacheJob{
		ID:   fmt.Sprintf("batch-%d", time.Now().UnixNano()),
		Type: CacheJobBatch,
		Payload: map[string]interface{}{
			"keys": keys,
			"data": data,
		},
		MaxTries:  3,
		CreatedAt: time.Now(),
		ProcessAt: time.Now(),
		Priority:  priority,
	}

	return icw.routeJob(job)
}

func (icw *IntegratedCacheWarmer) EnqueueScheduledWarmup(key string, data interface{}, ttl time.Duration, processAt time.Time, priority int) error {
	job := DistributedCacheJob{
		ID:   fmt.Sprintf("scheduled-%d", time.Now().UnixNano()),
		Type: CacheJobScheduled,
		Payload: map[string]interface{}{
			"key":  key,
			"data": data,
			"ttl":  ttl.String(),
		},
		MaxTries:  3,
		CreatedAt: time.Now(),
		ProcessAt: processAt,
		Priority:  priority,
	}

	if icw.jobRouter.redisAvailable {
		return icw.enqueueToRedis(job)
	}

	log.Printf("Redis unavailable for scheduled job, executing immediately: %s", key)
	return icw.enqueueToLocal(job)
}

func (icw *IntegratedCacheWarmer) routeJob(job DistributedCacheJob) error {
	icw.jobRouter.mu.RLock()
	defer icw.jobRouter.mu.RUnlock()

	switch {
	case !icw.jobRouter.redisAvailable:
		
		icw.localJobs++
		return icw.enqueueToLocal(job)

	case job.Type == CacheJobScheduled:
		
		icw.distributedJobs++
		return icw.enqueueToRedis(job)

	case job.Priority == 1:
		
		icw.localJobs++
		return icw.enqueueToLocal(job)

	case len(icw.localWorkerPool.jobCh) > icw.strategy.ConcurrentJobs:
		
		icw.distributedJobs++
		return icw.enqueueToRedis(job)

	default:
		
		icw.localJobs++
		return icw.enqueueToLocal(job)
	}
}

func (icw *IntegratedCacheWarmer) enqueueToRedis(job DistributedCacheJob) error {
	jobData, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	ctx := context.Background()

	if job.ProcessAt.After(time.Now()) {
		
		score := float64(job.ProcessAt.Unix())
		return icw.redisClient.ZAdd(ctx, icw.jobRouter.distributedQueue+"_delayed", redis.Z{
			Score:  score,
			Member: string(jobData),
		}).Err()
	}

	score := float64(time.Now().Unix()) - float64(job.Priority) 
	return icw.redisClient.ZAdd(ctx, icw.jobRouter.distributedQueue, redis.Z{
		Score:  score,
		Member: string(jobData),
	}).Err()
}

func (icw *IntegratedCacheWarmer) enqueueToLocal(job DistributedCacheJob) error {
	
	warmupJob := WarmupJob{
		Key:      fmt.Sprintf("%v", job.Payload["key"]),
		Data:     job.Payload["data"],
		Priority: job.Priority,
		TTL:      1 * time.Hour, 
	}

	if ttlStr, ok := job.Payload["ttl"].(string); ok {
		if ttl, err := time.ParseDuration(ttlStr); err == nil {
			warmupJob.TTL = ttl
		}
	}

	if job.Type == CacheJobBatch {
		if keys, ok := job.Payload["keys"].([]interface{}); ok {
			for _, keyInterface := range keys {
				if key, ok := keyInterface.(string); ok {
					batchJob := warmupJob
					batchJob.Key = key
					if !icw.localWorkerPool.SubmitJob(batchJob) {
						return fmt.Errorf("failed to submit batch job for key: %s", key)
					}
				}
			}
			return nil
		}
	}

	if !icw.localWorkerPool.SubmitJob(warmupJob) {
		return fmt.Errorf("failed to submit job to local worker pool")
	}

	return nil
}

func (dcw *DistributedCacheWorker) start() {
	dcw.wg.Add(1)
	defer dcw.wg.Done()

	log.Printf("Distributed cache worker %s started", dcw.id)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-dcw.stopCh:
			log.Printf("Distributed cache worker %s stopped", dcw.id)
			return
		case <-ticker.C:
			dcw.processJobs()
		}
	}
}

func (dcw *DistributedCacheWorker) processJobs() {
	ctx := context.Background()

	dcw.moveDelayedJobs(ctx)

	dcw.processImmediateJobs(ctx)
}

func (dcw *DistributedCacheWorker) moveDelayedJobs(ctx context.Context) {
	now := float64(time.Now().Unix())
	delayedQueue := dcw.queueName + "_delayed"

	jobs, err := dcw.redisClient.ZRangeByScore(ctx, delayedQueue, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   fmt.Sprintf("%f", now),
		Count: 10,
	}).Result()

	if err != nil || len(jobs) == 0 {
		return
	}

	for _, jobData := range jobs {
		var job DistributedCacheJob
		if err := json.Unmarshal([]byte(jobData), &job); err == nil {
			
			dcw.redisClient.ZRem(ctx, delayedQueue, jobData)

			score := float64(time.Now().Unix()) - float64(job.Priority)
			dcw.redisClient.ZAdd(ctx, dcw.queueName, redis.Z{
				Score:  score,
				Member: jobData,
			})
		}
	}
}

func (dcw *DistributedCacheWorker) processImmediateJobs(ctx context.Context) {
	
	result, err := dcw.redisClient.ZPopMin(ctx, dcw.queueName).Result()
	if err != nil || len(result) == 0 {
		return
	}

	var job DistributedCacheJob
	if err := json.Unmarshal([]byte(result[0].Member.(string)), &job); err != nil {
		log.Printf("Worker %s: failed to unmarshal job: %v", dcw.id, err)
		return
	}

	dcw.executeJob(ctx, &job)
}

func (dcw *DistributedCacheWorker) executeJob(ctx context.Context, job *DistributedCacheJob) {
	job.Attempts++

	var err error
	switch job.Type {
	case CacheJobWarmup, CacheJobScheduled:
		err = dcw.handleWarmupJob(ctx, job)
	case CacheJobBatch:
		err = dcw.handleBatchJob(ctx, job)
	case CacheJobValidation:
		err = dcw.handleValidationJob(ctx, job)
	case CacheJobEviction:
		err = dcw.handleEvictionJob(ctx, job)
	default:
		err = fmt.Errorf("unknown job type: %s", job.Type)
	}

	if err != nil {
		log.Printf("Worker %s: job %s failed (attempt %d/%d): %v",
			dcw.id, job.ID, job.Attempts, job.MaxTries, err)

		dcw.warmer.failedJobs++

		if job.Attempts < job.MaxTries {
			
			retryDelay := time.Duration(job.Attempts) * time.Second
			job.ProcessAt = time.Now().Add(retryDelay)

			jobData, _ := json.Marshal(job)
			dcw.redisClient.ZAdd(ctx, dcw.queueName+"_delayed", redis.Z{
				Score:  float64(job.ProcessAt.Unix()),
				Member: string(jobData),
			})

			dcw.warmer.retryJobs++
		} else {
			
			jobData, _ := json.Marshal(job)
			dcw.redisClient.LPush(ctx, dcw.queueName+"_dlq", string(jobData))
		}
	} else {
		log.Printf("Worker %s: job %s completed successfully", dcw.id, job.ID)
		dcw.warmer.processedJobs++
	}
}

func (dcw *DistributedCacheWorker) handleWarmupJob(ctx context.Context, job *DistributedCacheJob) error {
	key, ok := job.Payload["key"].(string)
	if !ok {
		return fmt.Errorf("invalid key in warmup job payload")
	}

	data := job.Payload["data"]
	ttl := 1 * time.Hour 

	if ttlStr, ok := job.Payload["ttl"].(string); ok {
		if parsedTTL, err := time.ParseDuration(ttlStr); err == nil {
			ttl = parsedTTL
		}
	}

	log.Printf("Distributed worker processing warmup job for key: %s", key)
	return dcw.warmer.cache.Set(key, data, ttl)
}

func (dcw *DistributedCacheWorker) handleBatchJob(ctx context.Context, job *DistributedCacheJob) error {
	keys, ok := job.Payload["keys"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid keys in batch job payload")
	}

	data := job.Payload["data"]
	ttl := 1 * time.Hour

	log.Printf("Distributed worker processing batch warmup for %d keys", len(keys))

	for _, keyInterface := range keys {
		if key, ok := keyInterface.(string); ok {
			if err := dcw.warmer.cache.Set(key, data, ttl); err != nil {
				log.Printf("Failed to warmup key %s: %v", key, err)
			}
		}
	}

	return nil
}

func (dcw *DistributedCacheWorker) handleValidationJob(ctx context.Context, job *DistributedCacheJob) error {
	key, ok := job.Payload["key"].(string)
	if !ok {
		return fmt.Errorf("invalid key in validation job payload")
	}

	exists, err := dcw.warmer.cache.Exists(key)
	if err != nil || !exists {
		
		return dcw.warmer.EnqueueWarmupJob(key, job.Payload["data"], 1*time.Hour, job.Priority)
	}

	return nil
}

func (dcw *DistributedCacheWorker) handleEvictionJob(ctx context.Context, job *DistributedCacheJob) error {
	key, ok := job.Payload["key"].(string)
	if !ok {
		return fmt.Errorf("invalid key in eviction job payload")
	}

	log.Printf("Distributed worker processing eviction job for key: %s", key)
	return dcw.warmer.cache.Delete(key)
}

func (icw *IntegratedCacheWarmer) GetMetrics() map[string]interface{} {
	icw.mu.RLock()
	defer icw.mu.RUnlock()

	metrics := map[string]interface{}{
		"processed_jobs":      icw.processedJobs,
		"failed_jobs":         icw.failedJobs,
		"retry_jobs":          icw.retryJobs,
		"distributed_jobs":    icw.distributedJobs,
		"local_jobs":          icw.localJobs,
		"redis_available":     icw.jobRouter.redisAvailable,
		"distributed_workers": len(icw.distributedWorkers),
		"system_type":         "integrated_redis_local",
	}

	if localStats := icw.localWorkerPool.GetStats(); localStats != nil {
		metrics["local_worker_stats"] = localStats
	}

	return metrics
}

func (icw *IntegratedCacheWarmer) GetQueueSizes() (map[string]int64, error) {
	sizes := make(map[string]int64)

	sizes["local"] = int64(len(icw.localWorkerPool.jobCh))

	if icw.jobRouter.redisAvailable {
		ctx := context.Background()

		if immediate, err := icw.redisClient.ZCard(ctx, icw.jobRouter.distributedQueue).Result(); err == nil {
			sizes["redis_immediate"] = immediate
		}

		if delayed, err := icw.redisClient.ZCard(ctx, icw.jobRouter.distributedQueue+"_delayed").Result(); err == nil {
			sizes["redis_delayed"] = delayed
		}

		if dlq, err := icw.redisClient.LLen(ctx, icw.jobRouter.distributedQueue+"_dlq").Result(); err == nil {
			sizes["redis_dead_letter"] = dlq
		}
	}

	return sizes, nil
}

func (icw *IntegratedCacheWarmer) WarmupCache() error {
	
	for _, job := range icw.strategy.Jobs {
		if err := icw.EnqueueWarmupJob(job.Key, job.Data, job.TTL, job.Priority); err != nil {
			log.Printf("Failed to enqueue warmup job for key %s: %v", job.Key, err)
		}
	}
	return nil
}

func (icw *IntegratedCacheWarmer) IsRunning() bool {
	icw.mu.RLock()
	defer icw.mu.RUnlock()
	return icw.running
}
