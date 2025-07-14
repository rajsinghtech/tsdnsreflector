package memory

import (
	"runtime"
	"sync"
	"time"

	"github.com/rajsingh/tsdnsreflector/internal/logger"
	"github.com/rajsingh/tsdnsreflector/internal/metrics"
)

type Monitor struct {
	zones        map[string]*Usage
	mutex        sync.RWMutex
	logger       *logger.Logger
	globalLimits Limits
	enabled      bool
}

type Usage struct {
	CacheSize      int64  // Current cache memory usage in bytes
	QueryHistory   int64  // Query history buffer memory
	LastUpdated    time.Time
	MaxCacheSize   int64  // Per-zone cache memory limit
	MaxQueryBuffer int64  // Per-zone query buffer limit
}

type Limits struct {
	MaxZoneCount      int   // Maximum number of zones
	MaxTotalMemory    int64 // Total memory limit for all zones
	MaxCachePerZone   int64 // Default cache memory limit per zone
	MaxBufferPerZone  int64 // Default query buffer limit per zone
}

func NewMonitor(log *logger.Logger, limits Limits) *Monitor {
	return &Monitor{
		zones:        make(map[string]*Usage),
		logger:       log,
		globalLimits: limits,
		enabled:      true,
	}
}

func (m *Monitor) RegisterZone(zoneName string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if len(m.zones) >= m.globalLimits.MaxZoneCount {
		return &MemoryLimitError{
			Type:    "zone_count",
			Message: "maximum zone count exceeded",
			Limit:   int64(m.globalLimits.MaxZoneCount),
			Current: int64(len(m.zones)),
		}
	}

	m.zones[zoneName] = &Usage{
		MaxCacheSize:   m.globalLimits.MaxCachePerZone,
		MaxQueryBuffer: m.globalLimits.MaxBufferPerZone,
		LastUpdated:    time.Now(),
	}

	m.logger.ZoneInfo(zoneName, "Zone memory monitoring registered", 
		"maxCache", m.globalLimits.MaxCachePerZone, 
		"maxBuffer", m.globalLimits.MaxBufferPerZone)

	return nil
}

func (m *Monitor) UpdateCacheUsage(zoneName string, cacheSize int64) error {
	if !m.enabled {
		return nil
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	usage, exists := m.zones[zoneName]
	if !exists {
		return &MemoryLimitError{
			Type:    "zone_not_registered",
			Message: "zone not registered for memory monitoring",
		}
	}

	if cacheSize > usage.MaxCacheSize {
		m.logger.ZoneWarn(zoneName, "Cache memory limit exceeded",
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

func (m *Monitor) UpdateQueryBufferUsage(zoneName string, bufferSize int64) error {
	if !m.enabled {
		return nil
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	usage, exists := m.zones[zoneName]
	if !exists {
		return nil
	}

	if bufferSize > usage.MaxQueryBuffer {
		m.logger.ZoneWarn(zoneName, "Query buffer memory limit exceeded",
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

func (m *Monitor) GetTotalMemoryUsage() int64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var total int64
	for _, usage := range m.zones {
		total += usage.CacheSize + usage.QueryHistory
	}
	return total
}

func (m *Monitor) CheckGlobalLimits() error {
	if !m.enabled {
		return nil
	}

	totalUsage := m.GetTotalMemoryUsage()
	if totalUsage > m.globalLimits.MaxTotalMemory {
		m.logger.Error("Global memory limit exceeded",
			"current", totalUsage,
			"limit", m.globalLimits.MaxTotalMemory)
		
		metrics.RecordMemoryViolation("global", "total_memory")
		
		return &MemoryLimitError{
			Type:    "global_memory",
			Message: "global memory limit exceeded",
			Limit:   m.globalLimits.MaxTotalMemory,
			Current: totalUsage,
		}
	}

	return nil
}

func (m *Monitor) GetZoneUsage(zoneName string) (*Usage, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	usage, exists := m.zones[zoneName]
	if !exists {
		return nil, false
	}

	return &Usage{
		CacheSize:      usage.CacheSize,
		QueryHistory:   usage.QueryHistory,
		LastUpdated:    usage.LastUpdated,
		MaxCacheSize:   usage.MaxCacheSize,
		MaxQueryBuffer: usage.MaxQueryBuffer,
	}, true
}

func (m *Monitor) StartPeriodicCheck(interval time.Duration) {
	if !m.enabled {
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if err := m.CheckGlobalLimits(); err != nil {
				m.logger.Error("Global memory check failed", "error", err)
			}
			
			// Update system memory metrics
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			metrics.UpdateSystemMemoryUsage(memStats.Alloc, memStats.Sys, memStats.HeapInuse)
		}
	}()
}

func (m *Monitor) Disable() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.enabled = false
	m.logger.Info("Zone memory monitoring disabled")
}

func (m *Monitor) Enable() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.enabled = true
	m.logger.Info("Zone memory monitoring enabled")
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