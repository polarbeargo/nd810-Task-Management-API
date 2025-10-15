package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

func RateLimiter(r rate.Limit, b int) gin.HandlerFunc {
	var visitors = make(map[string]*rate.Limiter)
	var mu sync.Mutex

	getVisitor := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		limiter, exists := visitors[ip]
		if !exists {
			limiter = rate.NewLimiter(r, b)
			visitors[ip] = limiter
		}
		return limiter
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := getVisitor(ip)
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

type DistributedRateLimiter struct {
	redis  *redis.Client
	limits map[string]*RateLimit
}

type RateLimit struct {
	Rate    int           
	Window  time.Duration 
	KeyFunc func(*gin.Context) string
	OnLimit func(*gin.Context)
}

func NewDistributedRateLimiter(redisClient *redis.Client) *DistributedRateLimiter {
	return &DistributedRateLimiter{
		redis:  redisClient,
		limits: make(map[string]*RateLimit),
	}
}

func (rl *DistributedRateLimiter) CreateMiddleware(name string, limit *RateLimit) gin.HandlerFunc {
	rl.limits[name] = limit

	return func(c *gin.Context) {
		key := fmt.Sprintf("rate_limit:%s:%s", name, limit.KeyFunc(c))

		allowed, err := rl.checkLimit(key, limit)
		if err != nil {
			c.Header("X-RateLimit-Error", "true")
			c.Next()
			return
		}

		if !allowed {
			if limit.OnLimit != nil {
				limit.OnLimit(c)
				return
			}

			c.Header("X-RateLimit-Limit", strconv.Itoa(limit.Rate))
			c.Header("X-RateLimit-Window", limit.Window.String())
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Rate limit exceeded",
				"retry_after": limit.Window.Seconds(),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func (rl *DistributedRateLimiter) checkLimit(key string, limit *RateLimit) (bool, error) {
	ctx := context.Background()

	now := time.Now().UnixNano()
	windowStart := now - limit.Window.Nanoseconds()

	pipe := rl.redis.Pipeline()

	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart, 10))

	countCmd := pipe.ZCard(ctx, key)

	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})

	pipe.Expire(ctx, key, limit.Window)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to execute rate limit pipeline: %w", err)
	}

	count := countCmd.Val()
	return count < int64(limit.Rate), nil
}

func IPKeyFunc(c *gin.Context) string {
	return c.ClientIP()
}

func UserKeyFunc(c *gin.Context) string {
	userID, exists := c.Get("user_id")
	if !exists {
		return c.ClientIP() 
	}
	return fmt.Sprintf("user:%v", userID)
}

func APIKeyFunc(c *gin.Context) string {
	apiKey := c.GetHeader("X-API-Key")
	if apiKey == "" {
		return c.ClientIP()
	}
	return fmt.Sprintf("api_key:%s", apiKey)
}

type CircuitBreaker struct {
	maxFailures int
	resetTime   time.Duration
	failures    int
	lastFailure time.Time
	state       string 
	mu          sync.RWMutex
}

func NewCircuitBreaker(maxFailures int, resetTime time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		resetTime:   resetTime,
		state:       "closed",
	}
}

func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailure
	cb.mu.RUnlock()

	switch state {
	case "open":
		if time.Since(lastFailure) > cb.resetTime {
			cb.mu.Lock()
			cb.state = "half-open"
			cb.failures = 0
			cb.mu.Unlock()
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	}

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailure = time.Now()

		if cb.failures >= cb.maxFailures {
			cb.state = "open"
		}
		return err
	}

	cb.failures = 0
	cb.state = "closed"
	return nil
}
