package cache

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

type Cache interface {
	Set(key string, value interface{}, ttl time.Duration) error
	Get(key string, dest interface{}) error
	Delete(key string) error
	DeletePattern(pattern string) error
	Exists(key string) (bool, error)
	Stats() map[string]interface{}
	Health() error
	Close() error
}

type MultiLevelCache struct {
	l1             *MemoryCache
	l2             *RedisCache
	metrics        *CacheMetrics
	circuitBreaker *CircuitBreaker
	warmer         *CacheWarmer
}

func NewMultiLevelCache(redisCache *RedisCache) *MultiLevelCache {
	mlc := &MultiLevelCache{
		l1:             NewMemoryCache(),
		l2:             redisCache,
		metrics:        NewCacheMetrics(),
		circuitBreaker: NewCircuitBreaker(DefaultCircuitBreakerConfig()),
	}

	mlc.warmer = NewCacheWarmer(mlc, nil)

	return mlc
}

func (c *MultiLevelCache) Set(key string, value interface{}, ttl time.Duration) error {
	c.l1.Set(key, value, ttl)
	c.metrics.RecordSet()

	if c.l2 != nil {
		err := c.circuitBreaker.Execute(func() error {
			return c.l2.Set(key, value, ttl)
		})
		if err != nil {
			c.metrics.RecordError()
			return nil
		}
	}

	return nil
}

func (c *MultiLevelCache) Get(key string, dest interface{}) error {
	if value, found := c.l1.Get(key); found {
		c.metrics.RecordHit()
		return copyValue(value, dest)
	}

	if c.l2 != nil {
		var l2Hit bool
		err := c.circuitBreaker.Execute(func() error {
			err := c.l2.Get(key, dest)
			if err == nil {
				l2Hit = true
				c.l1.Set(key, dest, 5*time.Minute)
			}
			return err
		})

		if err == nil && l2Hit {
			c.metrics.RecordHit()
			return nil
		}

		if err != nil && err != ErrCacheMiss {
			c.metrics.RecordError()
		}
	}

	c.metrics.RecordMiss()
	return ErrCacheMiss
}

func (c *MultiLevelCache) Delete(key string) error {
	c.l1.Delete(key)
	c.metrics.RecordDelete()

	if c.l2 != nil {
		err := c.circuitBreaker.Execute(func() error {
			return c.l2.Delete(key)
		})
		if err != nil {
			c.metrics.RecordError()
		}
		return err
	}

	return nil
}

func (c *MultiLevelCache) DeletePattern(pattern string) error {
	c.l1.DeletePattern(pattern)

	if c.l2 != nil {
		return c.l2.DeletePattern(pattern)
	}

	return nil
}

func (c *MultiLevelCache) Exists(key string) (bool, error) {
	if _, found := c.l1.Get(key); found {
		return true, nil
	}

	if c.l2 != nil {
		return c.l2.Exists(key)
	}

	return false, nil
}

func (c *MultiLevelCache) Stats() map[string]interface{} {
	stats := map[string]interface{}{
		"l1":      c.l1.Stats(),
		"metrics": c.metrics.GetStats(),
	}

	stats["hit_rate_percent"] = c.metrics.HitRate()

	stats["circuit_breaker"] = c.circuitBreaker.GetStats()

	if c.warmer != nil {
		stats["warmer"] = c.warmer.GetStats()
	}

	if c.l2 != nil {
		stats["l2"] = c.l2.Stats()
	}

	return stats
}

func (c *MultiLevelCache) Health() error {
	if c.l2 != nil {
		return c.l2.Health()
	}

	return nil
}

func (c *MultiLevelCache) Close() error {
	if c.warmer != nil {
		c.warmer.Stop()
	}

	if c.l2 != nil {
		return c.l2.Close()
	}

	return nil
}

func (c *MultiLevelCache) GetWarmer() *CacheWarmer {
	return c.warmer
}

func (c *MultiLevelCache) GetMetrics() *CacheMetrics {
	return c.metrics
}

func (c *MultiLevelCache) GetCircuitBreaker() *CircuitBreaker {
	return c.circuitBreaker
}

func copyValue(src, dest interface{}) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return fmt.Errorf("destination must be a pointer, got %T", dest)
	}

	if destValue.IsNil() {
		return fmt.Errorf("destination pointer is nil")
	}

	destElem := destValue.Elem()
	if !destElem.CanSet() {
		return fmt.Errorf("destination is not settable")
	}

	if destElem.Type() == reflect.TypeOf((*interface{})(nil)).Elem() {
		return copyValueViaJSON(src, dest)
	}

	return copyValueViaJSON(src, dest)
}

func copyValueViaJSON(src, dest interface{}) error {
	jsonData, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("failed to marshal source value: %w", err)
	}

	err = json.Unmarshal(jsonData, dest)
	if err != nil {
		return fmt.Errorf("failed to unmarshal to destination: %w", err)
	}

	return nil
}
