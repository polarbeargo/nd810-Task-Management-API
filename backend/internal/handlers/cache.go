package handlers

import (
	"net/http"
	"task-manager/backend/internal/cache"
	"time"

	"github.com/gin-gonic/gin"
)

type CacheHandler struct {
	CacheWarmer *cache.IntegratedCacheWarmer
	Cache       cache.Cache
}

func NewCacheHandler(cacheWarmer *cache.IntegratedCacheWarmer, cacheInstance cache.Cache) *CacheHandler {
	return &CacheHandler{
		CacheWarmer: cacheWarmer,
		Cache:       cacheInstance,
	}
}

// WarmCache triggers immediate cache warming
// POST /cache/warm
func (h *CacheHandler) WarmCache(c *gin.Context) {
	if h.CacheWarmer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Cache warming not available",
			"message": "Cache warmer is not initialized",
		})
		return
	}

	err := h.CacheWarmer.WarmupCache()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to trigger cache warming",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Cache warming triggered successfully",
	})
}

// EnqueueWarmupJob enqueues a cache warmup job
// POST /cache/jobs/warmup
func (h *CacheHandler) EnqueueWarmupJob(c *gin.Context) {
	var req struct {
		Key      string        `json:"key" binding:"required"`
		Data     interface{}   `json:"data"`
		TTL      time.Duration `json:"ttl"`
		Priority int           `json:"priority"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"message": err.Error(),
		})
		return
	}

	if h.CacheWarmer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Cache warming not available",
			"message": "Cache warmer is not initialized",
		})
		return
	}

	if req.TTL == 0 {
		req.TTL = 15 * time.Minute
	}

	if req.Priority == 0 {
		req.Priority = 5
	}

	err := h.CacheWarmer.EnqueueWarmupJob(req.Key, req.Data, req.TTL, req.Priority)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to enqueue warmup job",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":   "success",
		"message":  "Warmup job enqueued successfully",
		"key":      req.Key,
		"ttl":      req.TTL.String(),
		"priority": req.Priority,
	})
}

// EnqueueBatchJob enqueues a batch cache warmup job
// POST /cache/jobs/batch
func (h *CacheHandler) EnqueueBatchJob(c *gin.Context) {
	var req struct {
		Keys     []string    `json:"keys" binding:"required"`
		Data     interface{} `json:"data"`
		Priority int         `json:"priority"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"message": err.Error(),
		})
		return
	}

	if h.CacheWarmer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Cache warming not available",
			"message": "Cache warmer is not initialized",
		})
		return
	}

	if req.Priority == 0 {
		req.Priority = 5
	}

	err := h.CacheWarmer.EnqueueBatchWarmupJob(req.Keys, req.Data, req.Priority)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to enqueue batch job",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":    "success",
		"message":   "Batch job enqueued successfully",
		"key_count": len(req.Keys),
		"priority":  req.Priority,
	})
}

// EnqueueScheduledJob enqueues a scheduled cache warmup job
// POST /cache/jobs/scheduled
func (h *CacheHandler) EnqueueScheduledJob(c *gin.Context) {
	var req struct {
		Key       string        `json:"key" binding:"required"`
		Data      interface{}   `json:"data"`
		TTL       time.Duration `json:"ttl"`
		ProcessAt time.Time     `json:"process_at" binding:"required"`
		Priority  int           `json:"priority"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"message": err.Error(),
		})
		return
	}

	if h.CacheWarmer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Cache warming not available",
			"message": "Cache warmer is not initialized",
		})
		return
	}

	if req.TTL == 0 {
		req.TTL = 15 * time.Minute
	}

	if req.Priority == 0 {
		req.Priority = 5
	}

	err := h.CacheWarmer.EnqueueScheduledWarmup(req.Key, req.Data, req.TTL, req.ProcessAt, req.Priority)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to enqueue scheduled job",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":     "success",
		"message":    "Scheduled job enqueued successfully",
		"key":        req.Key,
		"process_at": req.ProcessAt,
		"ttl":        req.TTL.String(),
		"priority":   req.Priority,
	})
}

// EvictCacheKey evicts a specific cache key or pattern
// DELETE /cache/jobs/evict/:key
func (h *CacheHandler) EvictCacheKey(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"message": "Key parameter is required",
		})
		return
	}

	if h.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Cache not available",
			"message": "Cache is not initialized",
		})
		return
	}

	if containsWildcard(key) {
		err := h.Cache.DeletePattern(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to evict cache pattern",
				"message": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "Cache pattern evicted successfully",
			"pattern": key,
		})
		return
	}

	err := h.Cache.Delete(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to evict cache key",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Cache key evicted successfully",
		"key":     key,
	})
}

// GetCacheHealth returns detailed cache warming system health
// GET /cache/health
func (h *CacheHandler) GetCacheHealth(c *gin.Context) {
	if h.CacheWarmer == nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "unavailable",
			"message": "Cache warmer is not initialized",
			"healthy": false,
		})
		return
	}

	isRunning := h.CacheWarmer.IsRunning()
	metrics := h.CacheWarmer.GetMetrics()

	health := gin.H{
		"status":  "healthy",
		"running": isRunning,
		"metrics": metrics,
	}

	if !isRunning {
		health["status"] = "degraded"
	}

	queueSizes, err := h.CacheWarmer.GetQueueSizes()
	if err == nil {
		health["queue_sizes"] = queueSizes
	}

	c.JSON(http.StatusOK, health)
}

// GetCacheStats returns comprehensive cache statistics
// GET /cache/stats
func (h *CacheHandler) GetCacheStats(c *gin.Context) {
	stats := gin.H{}

	if h.Cache != nil {
		stats["cache"] = h.Cache.Stats()
	}

	if h.CacheWarmer != nil {
		stats["cache_warming"] = h.CacheWarmer.GetMetrics()

		queueSizes, err := h.CacheWarmer.GetQueueSizes()
		if err == nil {
			stats["queue_sizes"] = queueSizes
		}
	}

	c.JSON(http.StatusOK, stats)
}

func containsWildcard(s string) bool {
	return len(s) > 0 && (s[len(s)-1] == '*' || s[0] == '*')
}
