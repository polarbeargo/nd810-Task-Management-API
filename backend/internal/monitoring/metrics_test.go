package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupTestGin() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func TestMetricsMiddleware(t *testing.T) {
	resetGlobalMetrics()

	router := setupTestGin()
	router.Use(MetricsMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	metrics := GetMetrics()

	if metrics.RequestCount != 1 {
		t.Errorf("Expected RequestCount to be 1, got %d", metrics.RequestCount)
	}

	if metrics.ActiveRequests != 0 {
		t.Errorf("Expected ActiveRequests to be 0 after request completion, got %d", metrics.ActiveRequests)
	}

	if metrics.ErrorCount != 0 {
		t.Errorf("Expected ErrorCount to be 0 for successful request, got %d", metrics.ErrorCount)
	}

	if len(metrics.StatusCodes) == 0 {
		t.Error("Expected status codes to be tracked")
	}

	if len(metrics.Endpoints) == 0 {
		t.Error("Expected endpoints to be tracked")
	}
}

func TestMetricsMiddleware_ErrorTracking(t *testing.T) {
	resetGlobalMetrics()

	router := setupTestGin()
	router.Use(MetricsMiddleware())
	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test error"})
	})

	req, _ := http.NewRequest("GET", "/error", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	metrics := GetMetrics()

	if metrics.ErrorCount != 1 {
		t.Errorf("Expected ErrorCount to be 1, got %d", metrics.ErrorCount)
	}

	if metrics.StatusCodes["Internal Server Error"] != 1 {
		t.Errorf("Expected 1 Internal Server Error, got %d", metrics.StatusCodes["Internal Server Error"])
	}
}

func TestMetricsMiddleware_MultipleRequests(t *testing.T) {
	resetGlobalMetrics()

	router := setupTestGin()
	router.Use(MetricsMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	metrics := GetMetrics()

	if metrics.RequestCount != 5 {
		t.Errorf("Expected RequestCount to be 5, got %d", metrics.RequestCount)
	}

	if metrics.StatusCodes["OK"] != 5 {
		t.Errorf("Expected 5 OK responses, got %d", metrics.StatusCodes["OK"])
	}

	if metrics.Endpoints["GET /test"] != 5 {
		t.Errorf("Expected 5 calls to GET /test, got %d", metrics.Endpoints["GET /test"])
	}
}

func TestGetMetrics_ThreadSafety(t *testing.T) {
	resetGlobalMetrics()

	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			_ = GetMetrics()
		}
		done <- true
	}()

	router := setupTestGin()
	router.Use(MetricsMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := 0; i < 50; i++ {
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	<-done 

	metrics := GetMetrics()
	if metrics.RequestCount < 0 || metrics.RequestCount > 50 {
		t.Errorf("Unexpected RequestCount: %d", metrics.RequestCount)
	}
}

func TestGetSystemMetrics(t *testing.T) {
	metrics := GetSystemMetrics()

	if metrics.Uptime <= 0 {
		t.Error("Expected positive uptime")
	}

	if metrics.GoroutineCount <= 0 {
		t.Error("Expected positive goroutine count")
	}

	if metrics.CPUCount <= 0 {
		t.Error("Expected positive CPU count")
	}

	if metrics.GoVersion == "" {
		t.Error("Expected non-empty Go version")
	}

	if metrics.MemoryUsage.Alloc < 0 {
		t.Error("Expected non-negative memory allocation")
	}

	if metrics.MemoryUsage.TotalAlloc < 0 {
		t.Error("Expected non-negative total memory allocation")
	}

	if metrics.GoVersion != runtime.Version() {
		t.Errorf("Expected Go version %s, got %s", runtime.Version(), metrics.GoVersion)
	}
}

func TestBToMb(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected uint64
	}{
		{0, 0},
		{1024 * 1024, 1},           
		{1024 * 1024 * 5, 5},       
		{1024 * 1024 * 1024, 1024}, 
	}

	for _, test := range tests {
		result := bToMb(test.bytes)
		if result != test.expected {
			t.Errorf("bToMb(%d) = %d, expected %d", test.bytes, result, test.expected)
		}
	}
}

func TestRegisterHealthCheck(t *testing.T) {
	resetGlobalHealthChecker()

	checkFunc := func(ctx context.Context) error {
		return nil
	}

	RegisterHealthCheck("test_check", checkFunc)

	checks := RunHealthChecks()
	if len(checks) != 1 {
		t.Errorf("Expected 1 health check, got %d", len(checks))
	}

	check, exists := checks["test_check"]
	if !exists {
		t.Error("Expected test_check to be registered")
	}

	if check.Name != "test_check" {
		t.Errorf("Expected check name 'test_check', got %s", check.Name)
	}

	if check.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %s", check.Status)
	}
}

func TestRegisterHealthCheck_Failing(t *testing.T) {
	resetGlobalHealthChecker()

	checkFunc := func(ctx context.Context) error {
		return errors.New("check failed")
	}

	RegisterHealthCheck("failing_check", checkFunc)

	checks := RunHealthChecks()
	check := checks["failing_check"]

	if check.Status != "unhealthy" {
		t.Errorf("Expected status 'unhealthy', got %s", check.Status)
	}

	if check.Message != "check failed" {
		t.Errorf("Expected message 'check failed', got %s", check.Message)
	}
}

func TestRunHealthChecks(t *testing.T) {
	resetGlobalHealthChecker()

	RegisterHealthCheck("check1", func(ctx context.Context) error { return nil })
	RegisterHealthCheck("check2", func(ctx context.Context) error { return errors.New("failed") })

	checks := RunHealthChecks()

	if len(checks) != 2 {
		t.Errorf("Expected 2 health checks, got %d", len(checks))
	}

	if checks["check1"].Status != "healthy" {
		t.Errorf("Expected check1 to be healthy, got %s", checks["check1"].Status)
	}

	if checks["check2"].Status != "unhealthy" {
		t.Errorf("Expected check2 to be unhealthy, got %s", checks["check2"].Status)
	}
}

func TestMetricsHandler(t *testing.T) {
	resetGlobalMetrics()

	router := setupTestGin()
	router.GET("/metrics", MetricsHandler())

	req, _ := http.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse metrics response: %v", err)
	}

	if _, exists := response["application"]; !exists {
		t.Error("Expected application metrics in response")
	}

	if _, exists := response["system"]; !exists {
		t.Error("Expected system metrics in response")
	}

	if _, exists := response["timestamp"]; !exists {
		t.Error("Expected timestamp in response")
	}
}

func TestHealthHandler_Healthy(t *testing.T) {
	resetGlobalHealthChecker()
	RegisterHealthCheck("test", func(ctx context.Context) error { return nil })

	router := setupTestGin()
	router.GET("/health", HealthHandler())

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", response["status"])
	}
}

func TestHealthHandler_Unhealthy(t *testing.T) {
	resetGlobalHealthChecker()
	RegisterHealthCheck("failing", func(ctx context.Context) error {
		return errors.New("service down")
	})

	router := setupTestGin()
	router.GET("/health", HealthHandler())

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status ServiceUnavailable, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if response["status"] != "unhealthy" {
		t.Errorf("Expected status 'unhealthy', got %v", response["status"])
	}
}

func TestReadinessHandler_Ready(t *testing.T) {
	resetGlobalHealthChecker()
	RegisterHealthCheck("test", func(ctx context.Context) error { return nil })

	router := setupTestGin()
	router.GET("/ready", ReadinessHandler())

	req, _ := http.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse readiness response: %v", err)
	}

	if response["status"] != "ready" {
		t.Errorf("Expected status 'ready', got %v", response["status"])
	}
}

func TestReadinessHandler_NotReady(t *testing.T) {
	resetGlobalHealthChecker()
	RegisterHealthCheck("failing", func(ctx context.Context) error {
		return errors.New("not ready")
	})

	router := setupTestGin()
	router.GET("/ready", ReadinessHandler())

	req, _ := http.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status ServiceUnavailable, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse readiness response: %v", err)
	}

	if response["status"] != "not ready" {
		t.Errorf("Expected status 'not ready', got %v", response["status"])
	}
}

func TestLivenessHandler(t *testing.T) {
	router := setupTestGin()
	router.GET("/live", LivenessHandler())

	req, _ := http.NewRequest("GET", "/live", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse liveness response: %v", err)
	}

	if response["status"] != "alive" {
		t.Errorf("Expected status 'alive', got %v", response["status"])
	}

	if _, exists := response["uptime"]; !exists {
		t.Error("Expected uptime in liveness response")
	}
}

func TestMetrics_Concurrency(t *testing.T) {
	resetGlobalMetrics()

	router := setupTestGin()
	router.Use(MetricsMiddleware())
	router.GET("/test", func(c *gin.Context) {
		time.Sleep(time.Millisecond * 10) 
		c.Status(http.StatusOK)
	})

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			req, _ := http.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	metrics := GetMetrics()
	if metrics.RequestCount != 10 {
		t.Errorf("Expected RequestCount to be 10, got %d", metrics.RequestCount)
	}

	if metrics.ActiveRequests != 0 {
		t.Errorf("Expected ActiveRequests to be 0 after all requests complete, got %d", metrics.ActiveRequests)
	}
}

func resetGlobalMetrics() {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	globalMetrics.RequestCount = 0
	globalMetrics.RequestDuration = 0
	globalMetrics.ActiveRequests = 0
	globalMetrics.ErrorCount = 0
	globalMetrics.StatusCodes = make(map[string]int64)
	globalMetrics.Endpoints = make(map[string]int64)
	globalMetrics.StartTime = time.Now()
	globalMetrics.LastRequest = time.Time{}
	globalMetrics.totalDuration = 0
}

func resetGlobalHealthChecker() {
	globalHealthChecker.mu.Lock()
	defer globalHealthChecker.mu.Unlock()
	globalHealthChecker.checks = make(map[string]HealthCheck)
}

func BenchmarkMetricsMiddleware(b *testing.B) {
	resetGlobalMetrics()

	router := setupTestGin()
	router.Use(MetricsMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkGetMetrics(b *testing.B) {
	resetGlobalMetrics()

	globalMetrics.RequestCount = 1000
	globalMetrics.StatusCodes["OK"] = 800
	globalMetrics.StatusCodes["Not Found"] = 200
	globalMetrics.Endpoints["GET /api/v1/users"] = 500
	globalMetrics.Endpoints["POST /api/v1/users"] = 300

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetMetrics()
	}
}

func BenchmarkGetSystemMetrics(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetSystemMetrics()
	}
}

func BenchmarkRunHealthChecks(b *testing.B) {
	resetGlobalHealthChecker()

	for i := 0; i < 5; i++ {
		name := "check" + string(rune('0'+i))
		RegisterHealthCheck(name, func(ctx context.Context) error { return nil })
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RunHealthChecks()
	}
}
