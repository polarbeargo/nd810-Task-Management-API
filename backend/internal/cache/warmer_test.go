package cache

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)


type testCache struct {
	data sync.Map
}

func newTestCache() *testCache {
	return &testCache{}
}

func (tc *testCache) Set(key string, value interface{}, ttl time.Duration) error {
	tc.data.Store(key, value)
	return nil
}

func (tc *testCache) Get(key string, dest interface{}) error {
	if value, exists := tc.data.Load(key); exists {
		
		if ptr, ok := dest.(*interface{}); ok {
			*ptr = value
			return nil
		}
	}
	return nil
}

func (tc *testCache) GetValue(key string) (interface{}, bool) {
	return tc.data.Load(key)
}

func (tc *testCache) Delete(key string) error {
	tc.data.Delete(key)
	return nil
}

func (tc *testCache) DeletePattern(pattern string) error {
	return nil
}

func (tc *testCache) Exists(key string) (bool, error) {
	_, exists := tc.data.Load(key)
	return exists, nil
}

func (tc *testCache) Stats() map[string]interface{} {
	return map[string]interface{}{"type": "test"}
}

func (tc *testCache) Health() error {
	return nil
}

func (tc *testCache) Close() error {
	return nil
}

func TestCacheWarmerBasicFunctionality(t *testing.T) {
	
	cache := newTestCache()

	strategy := &WarmupStrategy{
		BatchSize:      5,
		ConcurrentJobs: 2,
		WarmupInterval: 0, 
	}

	warmer := NewCacheWarmer(cache, strategy)
	defer warmer.Stop()

	
	job := WarmupJob{
		Key:      "test_key",
		Data:     "test_value",
		TTL:      time.Hour,
		Priority: 1,
	}

	warmer.AddWarmupJob(job)

	
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	warmer.WarmCacheManually(ctx)

	
	time.Sleep(100 * time.Millisecond)

	
	value, exists := cache.GetValue("test_key")
	if !exists {
		t.Error("Expected key to be warmed in cache")
	}
	if value != "test_value" {
		t.Errorf("Expected 'test_value', got %v", value)
	}
}

func TestCacheWarmerPriority(t *testing.T) {
	cache := newTestCache()

	strategy := &WarmupStrategy{
		BatchSize:      1, 
		ConcurrentJobs: 1,
		WarmupInterval: 0,
	}

	warmer := NewCacheWarmer(cache, strategy)
	defer warmer.Stop()

	
	jobs := []WarmupJob{
		{
			Key:      "low",
			Data:     "value_low",
			TTL:      time.Hour,
			Priority: 1, 
		},
		{
			Key:      "high",
			Data:     "value_high",
			TTL:      time.Hour,
			Priority: 10, 
		},
		{
			Key:      "critical",
			Data:     "value_critical",
			TTL:      time.Hour,
			Priority: 20, 
		},
	}

	
	for _, job := range jobs {
		warmer.AddWarmupJob(job)
	}

	
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	warmer.WarmCacheManually(ctx)

	
	time.Sleep(100 * time.Millisecond)

	
	for _, job := range jobs {
		value, exists := cache.GetValue(job.Key)
		if !exists {
			t.Errorf("Expected key %s to be in cache", job.Key)
		}
		if value != job.Data {
			t.Errorf("Expected %s for key %s, got %v", job.Data, job.Key, value)
		}
	}
}

func TestCacheWarmerBatching(t *testing.T) {
	cache := newTestCache()

	strategy := &WarmupStrategy{
		BatchSize:      2, 
		ConcurrentJobs: 2,
		WarmupInterval: 0,
	}

	warmer := NewCacheWarmer(cache, strategy)
	defer warmer.Stop()

	
	for i := 0; i < 5; i++ {
		job := WarmupJob{
			Key:      "key_" + strconv.Itoa(i),
			Data:     "value_" + strconv.Itoa(i),
			TTL:      time.Hour,
			Priority: 1,
		}
		warmer.AddWarmupJob(job)
	}

	
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	warmer.WarmCacheManually(ctx)

	
	time.Sleep(200 * time.Millisecond)

	
	for i := 0; i < 5; i++ {
		key := "key_" + strconv.Itoa(i)
		expected := "value_" + strconv.Itoa(i)

		value, exists := cache.GetValue(key)
		if !exists {
			t.Errorf("Expected key %s to be in cache", key)
		}
		if value != expected {
			t.Errorf("Expected %s for key %s, got %v", expected, key, value)
		}
	}
}

func TestCacheWarmerStop(t *testing.T) {
	cache := newTestCache()

	strategy := &WarmupStrategy{
		BatchSize:      5,
		ConcurrentJobs: 2,
		WarmupInterval: 100 * time.Millisecond, 
	}

	warmer := NewCacheWarmer(cache, strategy)

	
	job := WarmupJob{
		Key:      "test_key",
		Data:     "test_value",
		TTL:      time.Hour,
		Priority: 1,
	}

	warmer.AddWarmupJob(job)

	
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	warmer.Start(ctx)

	
	time.Sleep(50 * time.Millisecond)

	
	warmer.Stop()

	
	stats := warmer.GetStats()
	running, ok := stats["running"].(bool)
	if !ok || running {
		t.Error("Expected warmer to be stopped")
	}
}

func TestCacheWarmerStats(t *testing.T) {
	cache := newTestCache()

	strategy := &WarmupStrategy{
		BatchSize:      10,
		ConcurrentJobs: 3,
		WarmupInterval: 5 * time.Minute,
	}

	warmer := NewCacheWarmer(cache, strategy)
	defer warmer.Stop()

	
	for i := 0; i < 5; i++ {
		job := WarmupJob{
			Key:      "key_" + strconv.Itoa(i),
			Data:     "value_" + strconv.Itoa(i),
			TTL:      time.Hour,
			Priority: 1,
		}
		warmer.AddWarmupJob(job)
	}

	
	stats := warmer.GetStats()

	
	if totalJobs, ok := stats["total_jobs"].(int); !ok || totalJobs != 5 {
		t.Errorf("Expected 5 total jobs, got %v", stats["total_jobs"])
	}

	if batchSize, ok := stats["batch_size"].(int); !ok || batchSize != 10 {
		t.Errorf("Expected batch size 10, got %v", stats["batch_size"])
	}

	if concurrentJobs, ok := stats["concurrent_jobs"].(int); !ok || concurrentJobs != 3 {
		t.Errorf("Expected 3 concurrent jobs, got %v", stats["concurrent_jobs"])
	}
}
