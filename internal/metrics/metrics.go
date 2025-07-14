package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Core DNS metrics per zone
	DNSQueries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_dns_queries_total",
			Help: "DNS queries by zone and type",
		},
		[]string{"zone", "query_type"},
	)

	DNSQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tsdnsreflector_dns_query_duration_seconds",
			Help:    "DNS query duration by zone",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"zone"},
	)

	// 4via6 translation metrics
	Via6Translations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_4via6_translations_total",
			Help: "4via6 translations by zone",
		},
		[]string{"zone"},
	)

	Via6Errors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_4via6_errors_total",
			Help: "4via6 translation errors by zone",
		},
		[]string{"zone", "error_type"},
	)

	// Backend DNS metrics
	BackendQueries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_backend_queries_total",
			Help: "Backend DNS queries by zone",
		},
		[]string{"zone", "backend"},
	)

	BackendErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_backend_errors_total",
			Help: "Backend DNS errors by zone",
		},
		[]string{"zone", "backend"},
	)

	// Cache metrics
	CacheOperations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_cache_operations_total",
			Help: "Cache operations by zone and result",
		},
		[]string{"zone", "result"}, // result: hit, miss
	)

	CacheSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tsdnsreflector_cache_size",
			Help: "Current cache size by zone",
		},
		[]string{"zone"},
	)

	CacheEvictions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_cache_evictions_total",
			Help: "Cache evictions by zone and type",
		},
		[]string{"zone", "eviction_type"},
	)

	// Client access metrics
	ClientQueries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_client_queries_total",
			Help: "DNS queries by zone, client type and status",
		},
		[]string{"zone", "client_type", "status"}, // client_type: tailscale, external; status: allowed, blocked
	)

	// System status
	TailscaleStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsdnsreflector_tailscale_status",
			Help: "Tailscale connection status (0=down, 1=up)",
		},
	)

	// Memory monitoring metrics
	ZoneMemoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tsdnsreflector_zone_memory_bytes",
			Help: "Memory usage by zone and type",
		},
		[]string{"zone", "type"},
	)

	MemoryViolations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsdnsreflector_memory_violations_total",
			Help: "Memory limit violations by zone and type",
		},
		[]string{"zone", "type"},
	)

	SystemMemoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tsdnsreflector_system_memory_bytes",
			Help: "System memory usage",
		},
		[]string{"type"},
	)
)

func RecordDNSQuery(zone, queryType string) func() {
	DNSQueries.WithLabelValues(zone, queryType).Inc()
	timer := prometheus.NewTimer(DNSQueryDuration.WithLabelValues(zone))
	return func() {
		timer.ObserveDuration()
	}
}

func RecordVia6Translation(zone string) {
	Via6Translations.WithLabelValues(zone).Inc()
}

func RecordVia6Error(zone, errorType string) {
	Via6Errors.WithLabelValues(zone, errorType).Inc()
}

func RecordBackendQuery(zone, backend string) {
	BackendQueries.WithLabelValues(zone, backend).Inc()
}

func RecordBackendError(zone, backend string) {
	BackendErrors.WithLabelValues(zone, backend).Inc()
}

func RecordCacheHit(zone string) {
	CacheOperations.WithLabelValues(zone, "hit").Inc()
}

func RecordCacheMiss(zone string) {
	CacheOperations.WithLabelValues(zone, "miss").Inc()
}

func UpdateCacheSize(zone string, size int) {
	CacheSize.WithLabelValues(zone).Set(float64(size))
}

func RecordCacheEviction(zone, evictionType string) {
	CacheEvictions.WithLabelValues(zone, evictionType).Inc()
}

func UpdateTailscaleStatus(up bool) {
	if up {
		TailscaleStatus.Set(1)
	} else {
		TailscaleStatus.Set(0)
	}
}

func UpdateZoneMemoryUsage(zone, memoryType string, bytes float64) {
	ZoneMemoryUsage.WithLabelValues(zone, memoryType).Set(bytes)
}

func RecordMemoryViolation(zone, violationType string) {
	MemoryViolations.WithLabelValues(zone, violationType).Inc()
}

func UpdateSystemMemoryUsage(alloc, sys, heapInuse uint64) {
	SystemMemoryUsage.WithLabelValues("alloc").Set(float64(alloc))
	SystemMemoryUsage.WithLabelValues("sys").Set(float64(sys))
	SystemMemoryUsage.WithLabelValues("heap_inuse").Set(float64(heapInuse))
}

func RecordExternalClientQuery(zone, status string) {
	ClientQueries.WithLabelValues(zone, "external", status).Inc()
}

func RecordTailscaleClientQuery(zone string) {
	ClientQueries.WithLabelValues(zone, "tailscale", "allowed").Inc()
}
