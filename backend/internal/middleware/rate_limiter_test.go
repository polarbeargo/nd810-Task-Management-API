package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

func setupTestGin() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func TestRateLimiter_Allow(t *testing.T) {
	router := setupTestGin()

	limiter := RateLimiter(rate.Limit(1), 1)
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "127.0.0.1:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Expected first request to succeed, got status %d", w1.Code)
	}

	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Expected second request to be rate limited, got status %d", w2.Code)
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	router := setupTestGin()

	limiter := RateLimiter(rate.Limit(1), 1)
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "127.0.0.1:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w1.Code != http.StatusOK {
		t.Errorf("Expected first request to succeed, got status %d", w1.Code)
	}

	if w2.Code != http.StatusOK {
		t.Errorf("Expected second request from different IP to succeed, got status %d", w2.Code)
	}
}

func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return client, mr
}

func TestNewDistributedRateLimiter(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	limiter := NewDistributedRateLimiter(client)

	if limiter == nil {
		t.Error("Expected rate limiter to be created")
	}

	if limiter.redis != client {
		t.Error("Expected Redis client to be set")
	}

	if limiter.limits == nil {
		t.Error("Expected limits map to be initialized")
	}
}

func TestDistributedRateLimiter_CreateMiddleware(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	limiter := NewDistributedRateLimiter(client)

	rateLimit := &RateLimit{
		Rate:    5,
		Window:  time.Minute,
		KeyFunc: IPKeyFunc,
	}

	middleware := limiter.CreateMiddleware("test", rateLimit)

	if middleware == nil {
		t.Error("Expected middleware to be created")
	}

	if len(limiter.limits) != 1 {
		t.Errorf("Expected 1 limit to be stored, got %d", len(limiter.limits))
	}

	if _, exists := limiter.limits["test"]; !exists {
		t.Error("Expected limit 'test' to be stored")
	}
}

func TestDistributedRateLimiter_AllowRequests(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	router := setupTestGin()
	limiter := NewDistributedRateLimiter(client)

	rateLimit := &RateLimit{
		Rate:    2,
		Window:  time.Minute,
		KeyFunc: IPKeyFunc,
	}

	middleware := limiter.CreateMiddleware("test", rateLimit)
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected request %d to succeed, got status %d", i+1, w.Code)
		}
	}

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected third request to be rate limited, got status %d", w.Code)
	}
}

func TestDistributedRateLimiter_RedisDown(t *testing.T) {
	client, mr := setupTestRedis(t)
	mr.Close() 

	router := setupTestGin()
	limiter := NewDistributedRateLimiter(client)

	rateLimit := &RateLimit{
		Rate:    1,
		Window:  time.Minute,
		KeyFunc: IPKeyFunc,
	}

	middleware := limiter.CreateMiddleware("test", rateLimit)
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected request to succeed when Redis is down (fail open), got status %d", w.Code)
	}

	if w.Header().Get("X-RateLimit-Error") != "true" {
		t.Error("Expected X-RateLimit-Error header when Redis is down")
	}
}

func TestDistributedRateLimiter_OnLimitCallback(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	router := setupTestGin()
	limiter := NewDistributedRateLimiter(client)

	onLimitCalled := false
	rateLimit := &RateLimit{
		Rate:    1,
		Window:  time.Minute,
		KeyFunc: IPKeyFunc,
		OnLimit: func(c *gin.Context) {
			onLimitCalled = true
			c.JSON(http.StatusForbidden, gin.H{"custom": "rate limit"})
		},
	}

	middleware := limiter.CreateMiddleware("test", rateLimit)
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "127.0.0.1:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if !onLimitCalled {
		t.Error("Expected OnLimit callback to be called")
	}

	if w2.Code != http.StatusForbidden {
		t.Errorf("Expected custom status from OnLimit callback, got %d", w2.Code)
	}
}

func TestIPKeyFunc(t *testing.T) {
	router := setupTestGin()
	router.GET("/test", func(c *gin.Context) {
		key := IPKeyFunc(c)
		c.JSON(http.StatusOK, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestUserKeyFunc(t *testing.T) {
	router := setupTestGin()
	router.GET("/test", func(c *gin.Context) {
		c.Set("user_id", "user123")
		key := UserKeyFunc(c)
		c.JSON(http.StatusOK, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	body := w.Body.String()
	if !contains(body, "user:user123") {
		t.Errorf("Expected response to contain 'user:user123', got %s", body)
	}
}

func TestUserKeyFunc_NoUser(t *testing.T) {
	router := setupTestGin()
	router.GET("/test", func(c *gin.Context) {
		key := UserKeyFunc(c)
		c.JSON(http.StatusOK, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	body := w.Body.String()
	if contains(body, "user:") {
		t.Errorf("Expected IP fallback, not user prefix. Got %s", body)
	}
}

func TestAPIKeyFunc(t *testing.T) {
	router := setupTestGin()
	router.GET("/test", func(c *gin.Context) {
		key := APIKeyFunc(c)
		c.JSON(http.StatusOK, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "api123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	body := w.Body.String()
	if !contains(body, "api_key:api123") {
		t.Errorf("Expected response to contain 'api_key:api123', got %s", body)
	}
}

func TestAPIKeyFunc_NoAPIKey(t *testing.T) {
	router := setupTestGin()
	router.GET("/test", func(c *gin.Context) {
		key := APIKeyFunc(c)
		c.JSON(http.StatusOK, gin.H{"key": key})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	body := w.Body.String()
	if contains(body, "api_key:") {
		t.Errorf("Expected IP fallback, not api_key prefix. Got %s", body)
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)

	if cb == nil {
		t.Error("Expected circuit breaker to be created")
	}

	if cb.maxFailures != 3 {
		t.Errorf("Expected maxFailures 3, got %d", cb.maxFailures)
	}

	if cb.resetTime != time.Minute {
		t.Errorf("Expected resetTime 1 minute, got %v", cb.resetTime)
	}

	if cb.state != "closed" {
		t.Errorf("Expected initial state 'closed', got %s", cb.state)
	}

	if cb.failures != 0 {
		t.Errorf("Expected initial failures 0, got %d", cb.failures)
	}
}

func TestCircuitBreaker_SuccessfulCall(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)

	err := cb.Call(func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected successful call, got error: %v", err)
	}

	if cb.state != "closed" {
		t.Errorf("Expected state to remain 'closed', got %s", cb.state)
	}

	if cb.failures != 0 {
		t.Errorf("Expected failures to remain 0, got %d", cb.failures)
	}
}

func TestCircuitBreaker_FailedCall(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)

	testErr := errors.New("test error")
	err := cb.Call(func() error {
		return testErr
	})

	if err != testErr {
		t.Errorf("Expected test error, got: %v", err)
	}

	if cb.failures != 1 {
		t.Errorf("Expected failures to be 1, got %d", cb.failures)
	}

	if cb.state != "closed" {
		t.Errorf("Expected state to remain 'closed' after one failure, got %s", cb.state)
	}
}

func TestCircuitBreaker_Open(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Minute)

	testErr := errors.New("test error")

	cb.Call(func() error { return testErr })

	cb.Call(func() error { return testErr })

	if cb.state != "open" {
		t.Errorf("Expected state to be 'open' after max failures, got %s", cb.state)
	}

	err := cb.Call(func() error {
		t.Error("Function should not be called when circuit is open")
		return nil
	})

	if err == nil {
		t.Error("Expected error when circuit is open")
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, time.Millisecond*100)

	cb.Call(func() error { return errors.New("test error") })

	if cb.state != "open" {
		t.Errorf("Expected state to be 'open', got %s", cb.state)
	}

	time.Sleep(time.Millisecond * 150)

	err := cb.Call(func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected successful call after reset time, got: %v", err)
	}

	if cb.state != "closed" {
		t.Errorf("Expected state to be 'closed' after successful half-open call, got %s", cb.state)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func BenchmarkRateLimiter(b *testing.B) {
	router := setupTestGin()
	limiter := RateLimiter(rate.Limit(1000), 100)
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkDistributedRateLimiter(b *testing.B) {
	client, mr := setupTestRedis(&testing.T{})
	defer mr.Close()

	router := setupTestGin()
	limiter := NewDistributedRateLimiter(client)

	rateLimit := &RateLimit{
		Rate:    1000,
		Window:  time.Minute,
		KeyFunc: IPKeyFunc,
	}

	middleware := limiter.CreateMiddleware("bench", rateLimit)
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkCircuitBreaker(b *testing.B) {
	cb := NewCircuitBreaker(100, time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Call(func() error {
			return nil
		})
	}
}
