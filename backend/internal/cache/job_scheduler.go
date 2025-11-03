package cache

import (
	"context"
	"log"
	"sync"
	"time"
)

type ScheduleTrigger struct {
	Type      string
	Interval  time.Duration
	Condition func() bool
}

type JobScheduler struct {
	warmer     *CacheWarmer
	workerPool *WorkerPool
	queue      *PriorityQueue
	triggers   map[string]*ScheduleTrigger
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex

	lastRun  time.Time
	nextRun  time.Time
	runCount int64
}

func NewJobScheduler(warmer *CacheWarmer, workerPool *WorkerPool) *JobScheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &JobScheduler{
		warmer:     warmer,
		workerPool: workerPool,
		queue:      NewPriorityQueue(),
		triggers:   make(map[string]*ScheduleTrigger),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (js *JobScheduler) AddIntervalTrigger(name string, interval time.Duration, condition func() bool) {
	js.mu.Lock()
	defer js.mu.Unlock()

	js.triggers[name] = &ScheduleTrigger{
		Type:      "interval",
		Interval:  interval,
		Condition: condition,
	}

	log.Printf("📅 Added interval trigger '%s' with interval %v", name, interval)
}

func (js *JobScheduler) AddEventTrigger(name string, condition func() bool) {
	js.mu.Lock()
	defer js.mu.Unlock()

	js.triggers[name] = &ScheduleTrigger{
		Type:      "event",
		Condition: condition,
	}

	log.Printf("📅 Added event trigger '%s'", name)
}

func (js *JobScheduler) RemoveTrigger(name string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	delete(js.triggers, name)
	log.Printf("📅 Removed trigger '%s'", name)
}

func (js *JobScheduler) ScheduleJob(job WarmupJob) {
	js.queue.Push(job)
	log.Printf("📋 Scheduled job: %s (priority: %d)", job.Key, job.Priority)
}

func (js *JobScheduler) ScheduleJobs(jobs []WarmupJob) {
	for _, job := range jobs {
		js.ScheduleJob(job)
	}
	log.Printf("📋 Scheduled %d jobs", len(jobs))
}

func (js *JobScheduler) Start() {
	js.mu.Lock()
	defer js.mu.Unlock()

	if js.running {
		return
	}

	js.running = true
	js.lastRun = time.Now()

	go js.schedulerLoop()

	log.Printf("📅 Job scheduler started")
}

func (js *JobScheduler) Stop() {
	js.mu.Lock()
	defer js.mu.Unlock()

	if !js.running {
		return
	}

	js.running = false
	js.cancel()

	log.Printf("📅 Job scheduler stopped")
}

func (js *JobScheduler) ProcessScheduledJobs() int {
	if js.queue.Empty() {
		return 0
	}

	processed := 0
	jobs := js.extractJobsBatch()

	if len(jobs) > 0 {
		submitted := js.workerPool.SubmitJobs(jobs)
		processed = submitted

		js.mu.Lock()
		js.runCount++
		js.lastRun = time.Now()
		js.mu.Unlock()

		log.Printf("📤 Processed %d scheduled jobs", processed)
	}

	return processed
}

func (js *JobScheduler) schedulerLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			js.checkTriggers()

		case <-js.ctx.Done():
			return
		}
	}
}

func (js *JobScheduler) checkTriggers() {
	js.mu.RLock()
	triggers := make(map[string]*ScheduleTrigger)
	for k, v := range js.triggers {
		triggers[k] = v
	}
	running := js.running
	js.mu.RUnlock()

	if !running {
		return
	}

	now := time.Now()
	shouldRun := false
	triggerName := ""

	for name, trigger := range triggers {
		switch trigger.Type {
		case "interval":
			if trigger.Interval > 0 && now.Sub(js.lastRun) >= trigger.Interval {
				if trigger.Condition == nil || trigger.Condition() {
					shouldRun = true
					triggerName = name
					break
				}
			}

		case "event":
			if trigger.Condition != nil && trigger.Condition() {
				shouldRun = true
				triggerName = name
				break
			}
		}
	}

	if shouldRun {
		log.Printf("🔥 Trigger '%s' activated, processing scheduled jobs", triggerName)
		js.ProcessScheduledJobs()
	}
}

func (js *JobScheduler) extractJobsBatch() []WarmupJob {
	var jobs []WarmupJob
	batchSize := 10

	if js.workerPool != nil {
		stats := js.workerPool.GetStats()
		if capacity, ok := stats["queue_capacity"].(int); ok {
			batchSize = capacity
		}
	}

	for i := 0; i < batchSize && !js.queue.Empty(); i++ {
		if job, ok := js.queue.Pop(); ok {
			jobs = append(jobs, job)
		}
	}

	return jobs
}

func (js *JobScheduler) GetStats() map[string]interface{} {
	js.mu.RLock()
	defer js.mu.RUnlock()

	triggerCount := len(js.triggers)
	queueLength := js.queue.Len()

	stats := map[string]interface{}{
		"running":       js.running,
		"trigger_count": triggerCount,
		"queue_length":  queueLength,
		"last_run":      js.lastRun.Format(time.RFC3339),
		"run_count":     js.runCount,
	}

	if !js.nextRun.IsZero() {
		stats["next_run"] = js.nextRun.Format(time.RFC3339)
	}

	return stats
}

func (js *JobScheduler) GetTriggers() map[string]ScheduleTrigger {
	js.mu.RLock()
	defer js.mu.RUnlock()

	triggers := make(map[string]ScheduleTrigger)
	for name, trigger := range js.triggers {
		triggers[name] = *trigger
	}

	return triggers
}

func (js *JobScheduler) IsRunning() bool {
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.running
}

func (js *JobScheduler) ClearQueue() {
	cleared := js.queue.Len()
	js.queue.Clear()
	log.Printf("🗑️ Cleared %d jobs from scheduler queue", cleared)
}
