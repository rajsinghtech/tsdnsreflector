package memory

import (
	"fmt"
	"testing"
	"time"

	"github.com/rajsingh/tsdnsreflector/internal/config"
	"github.com/rajsingh/tsdnsreflector/internal/logger"
)

func TestMemoryLimitEnforcement(t *testing.T) {
	logConfig := config.LoggingConfig{
		Level:  "debug",
		Format: "text",
	}
	log := logger.New(logConfig)
	
	limits := Limits{
		MaxZoneCount:     10,
		MaxTotalMemory:   1024 * 1024, // 1MB
		MaxCachePerZone:  512 * 1024,  // 512KB per zone
		MaxBufferPerZone: 256 * 1024,  // 256KB per zone
	}
	
	monitor := NewMonitor(log, limits)
	
	t.Run("zone_registration", func(t *testing.T) {
		err := monitor.RegisterZone("test-zone")
		if err != nil {
			t.Errorf("Failed to register zone: %v", err)
		}
		
		usage, exists := monitor.GetZoneUsage("test-zone")
		if !exists {
			t.Errorf("Zone not found after registration")
		}
		
		if usage.MaxCacheSize != limits.MaxCachePerZone {
			t.Errorf("Expected max cache size %d, got %d", limits.MaxCachePerZone, usage.MaxCacheSize)
		}
		
		t.Logf("Zone registered with max cache size: %d bytes", usage.MaxCacheSize)
	})
	
	t.Run("cache_limit_enforcement", func(t *testing.T) {
		zoneName := "test-cache-zone"
		err := monitor.RegisterZone(zoneName)
		if err != nil {
			t.Fatalf("Failed to register zone: %v", err)
		}
		
		// Test under limit - should succeed
		underLimit := int64(100 * 1024) // 100KB
		err = monitor.UpdateCacheUsage(zoneName, underLimit)
		if err != nil {
			t.Errorf("Cache usage under limit should succeed: %v", err)
		}
		
		usage, _ := monitor.GetZoneUsage(zoneName)
		if usage.CacheSize != underLimit {
			t.Errorf("Expected cache size %d, got %d", underLimit, usage.CacheSize)
		}
		
		// Test over limit - should fail
		overLimit := int64(600 * 1024) // 600KB (exceeds 512KB limit)
		err = monitor.UpdateCacheUsage(zoneName, overLimit)
		if err == nil {
			t.Errorf("Cache usage over limit should fail")
		}
		
		memErr, ok := err.(*MemoryLimitError)
		if !ok {
			t.Errorf("Expected MemoryLimitError, got %T", err)
		} else {
			if memErr.Type != "cache_memory" {
				t.Errorf("Expected error type 'cache_memory', got %s", memErr.Type)
			}
			if memErr.Current != overLimit {
				t.Errorf("Expected current %d, got %d", overLimit, memErr.Current)
			}
			if memErr.Limit != limits.MaxCachePerZone {
				t.Errorf("Expected limit %d, got %d", limits.MaxCachePerZone, memErr.Limit)
			}
		}
		
		t.Logf("Cache limit enforcement working: under=%d succeeded, over=%d failed", underLimit, overLimit)
	})
	
	t.Run("global_memory_limits", func(t *testing.T) {
		// Register multiple zones and check global limits
		zones := []string{"zone1", "zone2", "zone3"}
		for _, zone := range zones {
			err := monitor.RegisterZone(zone)
			if err != nil {
				t.Fatalf("Failed to register zone %s: %v", zone, err)
			}
		}
		
		// Update each zone to use memory near per-zone limit
		perZoneUsage := int64(400 * 1024) // 400KB per zone
		for _, zone := range zones {
			err := monitor.UpdateCacheUsage(zone, perZoneUsage)
			if err != nil {
				t.Errorf("Failed to set cache usage for zone %s: %v", zone, err)
			}
		}
		
		totalUsage := monitor.GetTotalMemoryUsage()
		expectedTotal := perZoneUsage * int64(len(zones))
		if totalUsage < expectedTotal {
			t.Errorf("Total usage %d should be at least %d", totalUsage, expectedTotal)
		}
		
		// Test global limit check
		err := monitor.CheckGlobalLimits()
		if totalUsage > limits.MaxTotalMemory && err == nil {
			t.Errorf("Global limit check should fail when total exceeds limit")
		} else if totalUsage <= limits.MaxTotalMemory && err != nil {
			t.Errorf("Global limit check should pass when total under limit: %v", err)
		}
		
		t.Logf("Global memory check: total=%d, limit=%d, zones=%d", totalUsage, limits.MaxTotalMemory, len(zones))
	})
	
	t.Run("memory_limit_error_interface", func(t *testing.T) {
		zoneName := "test-error-zone"
		err := monitor.RegisterZone(zoneName)
		if err != nil {
			t.Fatalf("Failed to register zone: %v", err)
		}
		
		// Trigger a limit error
		overLimit := int64(600 * 1024) // Over 512KB limit
		err = monitor.UpdateCacheUsage(zoneName, overLimit)
		
		if err == nil {
			t.Fatalf("Expected memory limit error")
		}
		
		memErr, ok := err.(*MemoryLimitError)
		if !ok {
			t.Fatalf("Expected MemoryLimitError, got %T", err)
		}
		
		if !memErr.IsLimitExceeded() {
			t.Errorf("IsLimitExceeded() should return true")
		}
		
		if memErr.Error() == "" {
			t.Errorf("Error() should return non-empty string")
		}
		
		t.Logf("MemoryLimitError interface working: %s", memErr.Error())
	})
}

func TestMemoryMonitoringAccuracy(t *testing.T) {
	logConfig := config.LoggingConfig{
		Level:  "debug",
		Format: "text",
	}
	log := logger.New(logConfig)
	
	limits := Limits{
		MaxZoneCount:     5,
		MaxTotalMemory:   2 * 1024 * 1024, // 2MB
		MaxCachePerZone:  1024 * 1024,     // 1MB per zone
		MaxBufferPerZone: 512 * 1024,      // 512KB per zone
	}
	
	monitor := NewMonitor(log, limits)
	
	t.Run("accurate_memory_tracking", func(t *testing.T) {
		zoneName := "accuracy-test"
		err := monitor.RegisterZone(zoneName)
		if err != nil {
			t.Fatalf("Failed to register zone: %v", err)
		}
		
		// Simulate realistic DNS cache memory usage
		testSizes := []int64{
			1024,      // 1KB - small cache
			50 * 1024, // 50KB - medium cache
			200 * 1024, // 200KB - large cache
			400 * 1024, // 400KB - very large cache
		}
		
		for i, size := range testSizes {
			err := monitor.UpdateCacheUsage(zoneName, size)
			if err != nil {
				t.Errorf("Test %d: Failed to update cache usage to %d: %v", i, size, err)
				continue
			}
			
			usage, exists := monitor.GetZoneUsage(zoneName)
			if !exists {
				t.Errorf("Test %d: Zone not found", i)
				continue
			}
			
			if usage.CacheSize != size {
				t.Errorf("Test %d: Expected cache size %d, got %d", i, size, usage.CacheSize)
			}
			
			if time.Since(usage.LastUpdated) > time.Second {
				t.Errorf("Test %d: LastUpdated should be recent", i)
			}
		}
		
		t.Logf("Accurate memory tracking validated for sizes: %v", testSizes)
	})
	
	t.Run("zone_count_limits", func(t *testing.T) {
		// Register up to the limit (note: we already registered zones in previous tests)
		initialZoneCount := len(monitor.zones)
		
		// Try to register zones up to and beyond the limit
		for i := initialZoneCount; i < limits.MaxZoneCount+2; i++ {
			zoneName := fmt.Sprintf("zone-%d", i)
			err := monitor.RegisterZone(zoneName)
			
			if i < limits.MaxZoneCount {
				if err != nil {
					t.Errorf("Zone %d should register successfully: %v", i, err)
				}
			} else {
				if err == nil {
					t.Errorf("Zone %d should fail due to count limit", i)
				}
				
				memErr, ok := err.(*MemoryLimitError)
				if !ok {
					t.Errorf("Expected MemoryLimitError for zone count limit")
				} else if memErr.Type != "zone_count" {
					t.Errorf("Expected error type 'zone_count', got %s", memErr.Type)
				}
			}
		}
		
		t.Logf("Zone count limit enforcement working: max=%d", limits.MaxZoneCount)
	})
}

func TestMemoryMonitoringDisabledState(t *testing.T) {
	logConfig := config.LoggingConfig{
		Level:  "debug",
		Format: "text",
	}
	log := logger.New(logConfig)
	
	limits := Limits{
		MaxZoneCount:     10,
		MaxTotalMemory:   1024 * 1024,
		MaxCachePerZone:  512 * 1024,
		MaxBufferPerZone: 256 * 1024,
	}
	
	monitor := NewMonitor(log, limits)
	
	t.Run("disabled_monitoring", func(t *testing.T) {
		zoneName := "disabled-test"
		err := monitor.RegisterZone(zoneName)
		if err != nil {
			t.Fatalf("Failed to register zone: %v", err)
		}
		
		// Disable monitoring
		monitor.Disable()
		
		// Operations should succeed even with large values when disabled
		largeSize := int64(2 * 1024 * 1024) // 2MB (exceeds limits)
		err = monitor.UpdateCacheUsage(zoneName, largeSize)
		if err != nil {
			t.Errorf("Operations should succeed when monitoring disabled: %v", err)
		}
		
		err = monitor.CheckGlobalLimits()
		if err != nil {
			t.Errorf("Global limit check should pass when disabled: %v", err)
		}
		
		// Re-enable and verify limits work again
		monitor.Enable()
		
		err = monitor.UpdateCacheUsage(zoneName, largeSize)
		if err == nil {
			t.Errorf("Large cache usage should fail when monitoring re-enabled")
		}
		
		t.Logf("Disable/enable functionality working correctly")
	})
}

