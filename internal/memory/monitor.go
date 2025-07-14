package memory

import (
	"runtime"
	"sync"
	"time"

	"github.com/rajsingh/tsdnsreflector/internal/logger"
	"github.com/rajsingh/tsdnsreflector/internal/metrics"
)

type ZoneMemoryMonitor struct {
	zones        map[string]*ZoneMemoryUsage
	mutex        sync.RWMutex
	logger       *logger.Logger
	globalLimits GlobalMemoryLimits
	enabled      bool
}

type ZoneMemoryUsage struct {
	CacheSize      int64  // Current cache memory usage in bytes
	QueryHistory   int64  // Query history buffer memory
	LastUpdated    time.Time
	MaxCacheSize   int64  // Per-zone cache memory limit
	MaxQueryBuffer int64  // Per-zone query buffer limit
}

type GlobalMemoryLimits struct {
	MaxZoneCount      int   // Maximum number of zones
	MaxTotalMemory    int64 // Total memory limit for all zones
	MaxCachePerZone   int64 // Default cache memory limit per zone
	MaxBufferPerZone  int64 // Default query buffer limit per zone
}

func NewZoneMemoryMonitor(log *logger.Logger, limits GlobalMemoryLimits) *ZoneMemoryMonitor {
	return &ZoneMemoryMonitor{
		zones:        make(map[string]*ZoneMemoryUsage),
		logger:       log,
		globalLimits: limits,
		enabled:      true,
	}
}

func (zmm *ZoneMemoryMonitor) RegisterZone(zoneName string) error {
	zmm.mutex.Lock()
	defer zmm.mutex.Unlock()

	if len(zmm.zones) >= zmm.globalLimits.MaxZoneCount {
		return &MemoryLimitError{
			Type:    "zone_count",
			Message: "maximum zone count exceeded",
			Limit:   int64(zmm.globalLimits.MaxZoneCount),
			Current: int64(len(zmm.zones)),
		}
	}

	zmm.zones[zoneName] = &ZoneMemoryUsage{
		MaxCacheSize:   zmm.globalLimits.MaxCachePerZone,
		MaxQueryBuffer: zmm.globalLimits.MaxBufferPerZone,
		LastUpdated:    time.Now(),
	}

	zmm.logger.ZoneInfo(zoneName, "Zone memory monitoring registered", 
		"maxCache", zmm.globalLimits.MaxCachePerZone, 
		"maxBuffer", zmm.globalLimits.MaxBufferPerZone)

	return nil
}

func (zmm *ZoneMemoryMonitor) UpdateCacheUsage(zoneName string, cacheSize int64) error {
	if !zmm.enabled {
		return nil
	}

	zmm.mutex.Lock()
	defer zmm.mutex.Unlock()

	usage, exists := zmm.zones[zoneName]
	if !exists {
		return &MemoryLimitError{
			Type:    "zone_not_registered",
			Message: "zone not registered for memory monitoring",
		}
	}

	if cacheSize > usage.MaxCacheSize {
		zmm.logger.ZoneWarn(zoneName, "Cache memory limit exceeded",
			"current", cacheSize,
			"limit", usage.MaxCacheSize)
		
		metrics.RecordMemoryViolation(zoneName, "cache")
		
		return &MemoryLimitError{
			Type:    "cache_memory",
			Message: "zone cache memory limit exceeded",
			Limit:   usage.MaxCacheSize,
			Current: cacheSize,
		}
	}

	usage.CacheSize = cacheSize
	usage.LastUpdated = time.Now()
	
	metrics.UpdateZoneMemoryUsage(zoneName, "cache", float64(cacheSize))
	return nil
}

func (zmm *ZoneMemoryMonitor) UpdateQueryBufferUsage(zoneName string, bufferSize int64) error {
	if !zmm.enabled {
		return nil
	}

	zmm.mutex.Lock()
	defer zmm.mutex.Unlock()

	usage, exists := zmm.zones[zoneName]
	if !exists {
		return nil
	}

	if bufferSize > usage.MaxQueryBuffer {
		zmm.logger.ZoneWarn(zoneName, "Query buffer memory limit exceeded",
			"current", bufferSize,
			"limit", usage.MaxQueryBuffer)
		
		metrics.RecordMemoryViolation(zoneName, "query_buffer")
		
		return &MemoryLimitError{
			Type:    "query_buffer_memory",
			Message: "zone query buffer memory limit exceeded",
			Limit:   usage.MaxQueryBuffer,
			Current: bufferSize,
		}
	}

	usage.QueryHistory = bufferSize
	usage.LastUpdated = time.Now()
	
	metrics.UpdateZoneMemoryUsage(zoneName, "query_buffer", float64(bufferSize))
	return nil
}

func (zmm *ZoneMemoryMonitor) GetTotalMemoryUsage() int64 {
	zmm.mutex.RLock()
	defer zmm.mutex.RUnlock()

	var total int64
	for _, usage := range zmm.zones {
		total += usage.CacheSize + usage.QueryHistory
	}
	return total
}

func (zmm *ZoneMemoryMonitor) CheckGlobalLimits() error {
	if !zmm.enabled {
		return nil
	}

	totalUsage := zmm.GetTotalMemoryUsage()
	if totalUsage > zmm.globalLimits.MaxTotalMemory {
		zmm.logger.Error("Global memory limit exceeded",
			"current", totalUsage,
			"limit", zmm.globalLimits.MaxTotalMemory)
		
		metrics.RecordMemoryViolation("global", "total_memory")
		
		return &MemoryLimitError{
			Type:    "global_memory",
			Message: "global memory limit exceeded",
			Limit:   zmm.globalLimits.MaxTotalMemory,
			Current: totalUsage,
		}
	}

	return nil
}

func (zmm *ZoneMemoryMonitor) GetZoneUsage(zoneName string) (*ZoneMemoryUsage, bool) {
	zmm.mutex.RLock()
	defer zmm.mutex.RUnlock()

	usage, exists := zmm.zones[zoneName]
	if !exists {
		return nil, false
	}

	return &ZoneMemoryUsage{
		CacheSize:      usage.CacheSize,
		QueryHistory:   usage.QueryHistory,
		LastUpdated:    usage.LastUpdated,
		MaxCacheSize:   usage.MaxCacheSize,
		MaxQueryBuffer: usage.MaxQueryBuffer,
	}, true
}

func (zmm *ZoneMemoryMonitor) StartPeriodicCheck(interval time.Duration) {
	if !zmm.enabled {
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if err := zmm.CheckGlobalLimits(); err != nil {
				zmm.logger.Error("Global memory check failed", "error", err)
			}
			
			// Update system memory metrics
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			metrics.UpdateSystemMemoryUsage(memStats.Alloc, memStats.Sys, memStats.HeapInuse)
		}
	}()
}

func (zmm *ZoneMemoryMonitor) Disable() {
	zmm.mutex.Lock()
	defer zmm.mutex.Unlock()
	zmm.enabled = false
	zmm.logger.Info("Zone memory monitoring disabled")
}

func (zmm *ZoneMemoryMonitor) Enable() {
	zmm.mutex.Lock()
	defer zmm.mutex.Unlock()
	zmm.enabled = true
	zmm.logger.Info("Zone memory monitoring enabled")
}

type MemoryLimitError struct {
	Type    string
	Message string
	Limit   int64
	Current int64
}

func (e *MemoryLimitError) Error() string {
	return e.Message
}

func (e *MemoryLimitError) IsLimitExceeded() bool {
	return e.Current > e.Limit
}