package cache

import (
	"testing"
)

func TestCacheMetrics(t *testing.T) {
	metrics := NewCacheMetrics()

	if metrics.GetStats().Hits != 0 {
		t.Errorf("Expected 0 hits, got %d", metrics.GetStats().Hits)
	}

	metrics.RecordHit()
	metrics.RecordHit()
	metrics.RecordMiss()
	metrics.RecordSet()
	metrics.RecordDelete()
	metrics.RecordError()

	stats := metrics.GetStats()
	if stats.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
	if stats.Sets != 1 {
		t.Errorf("Expected 1 set, got %d", stats.Sets)
	}
	if stats.Deletes != 1 {
		t.Errorf("Expected 1 delete, got %d", stats.Deletes)
	}
	if stats.Errors != 1 {
		t.Errorf("Expected 1 error, got %d", stats.Errors)
	}

	expectedHitRate := 66.67 
	hitRate := metrics.HitRate()
	if hitRate < expectedHitRate-0.1 || hitRate > expectedHitRate+0.1 {
		t.Errorf("Expected hit rate around %.2f%%, got %.2f%%", expectedHitRate, hitRate)
	}

	metrics.Reset()
	stats = metrics.GetStats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("Expected metrics to be reset to 0")
	}
}

func TestCacheMetricsHitRate(t *testing.T) {
	metrics := NewCacheMetrics()

	if metrics.HitRate() != 0.0 {
		t.Errorf("Expected 0%% hit rate with no operations, got %.2f%%", metrics.HitRate())
	}

	metrics.RecordHit()
	metrics.RecordHit()
	if metrics.HitRate() != 100.0 {
		t.Errorf("Expected 100%% hit rate, got %.2f%%", metrics.HitRate())
	}

	metrics.RecordMiss()
	expectedRate := 66.67 
	hitRate := metrics.HitRate()
	if hitRate < expectedRate-0.1 || hitRate > expectedRate+0.1 {
		t.Errorf("Expected hit rate around %.2f%%, got %.2f%%", expectedRate, hitRate)
	}
}

func TestCacheMetricsConcurrency(t *testing.T) {
	metrics := NewCacheMetrics()

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				metrics.RecordHit()
				metrics.RecordMiss()
				metrics.RecordSet()
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	stats := metrics.GetStats()
	expectedHits := int64(1000) 
	if stats.Hits != expectedHits {
		t.Errorf("Expected %d hits, got %d", expectedHits, stats.Hits)
	}
	if stats.Misses != expectedHits {
		t.Errorf("Expected %d misses, got %d", expectedHits, stats.Misses)
	}
	if stats.Sets != expectedHits {
		t.Errorf("Expected %d sets, got %d", expectedHits, stats.Sets)
	}
}
