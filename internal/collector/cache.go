// cache.go collects cache performance metrics.
//
// Why this matters for SRE:
// Cache hit rate is a key efficiency indicator — a dropping hit rate means
// more requests hit the slower backing store (database, API, disk), increasing
// latency and load on downstream systems. Cache evictions indicate memory
// pressure or an undersized cache. Both are invisible to infrastructure-level
// monitoring but critical for maintaining response time SLOs.
//
// Metrics exposed:
//   - obs_cache_hits_total (counter): successful cache lookups
//   - obs_cache_misses_total (counter): cache lookups that missed
//   - obs_cache_evictions_total (counter): entries removed due to capacity
//   - obs_cache_size_bytes (gauge): current cache memory usage
package collector

import (
	"github.com/ethan-k-francis/observability-toolkit/internal/simulator"
	"github.com/prometheus/client_golang/prometheus"
)

// CacheCollector exposes cache performance metrics.
type CacheCollector struct {
	sim *simulator.Simulator

	hitsDesc      *prometheus.Desc
	missesDesc    *prometheus.Desc
	evictionsDesc *prometheus.Desc
	sizeBytesDesc *prometheus.Desc
}

// NewCacheCollector creates metric descriptors for cache metrics.
func NewCacheCollector(sim *simulator.Simulator) *CacheCollector {
	return &CacheCollector{
		sim: sim,
		hitsDesc: prometheus.NewDesc(
			"obs_cache_hits_total",
			"Total number of cache hits (successful lookups)",
			nil, nil,
		),
		missesDesc: prometheus.NewDesc(
			"obs_cache_misses_total",
			"Total number of cache misses (lookups that fell through to backing store)",
			nil, nil,
		),
		evictionsDesc: prometheus.NewDesc(
			"obs_cache_evictions_total",
			"Total number of cache entry evictions due to memory pressure",
			nil, nil,
		),
		sizeBytesDesc: prometheus.NewDesc(
			"obs_cache_size_bytes",
			"Current size of the cache in bytes",
			nil, nil,
		),
	}
}

// Describe sends all cache metric descriptors to the channel.
func (c *CacheCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.hitsDesc
	ch <- c.missesDesc
	ch <- c.evictionsDesc
	ch <- c.sizeBytesDesc
}

// Collect gathers current cache metrics from the simulator.
func (c *CacheCollector) Collect(ch chan<- prometheus.Metric) {
	data := c.sim.CacheMetrics()

	// Cache hits, misses, and evictions are counters (monotonically increasing).
	// The hit rate is derived in PromQL rather than exported as a separate metric:
	//   rate(obs_cache_hits_total[5m]) /
	//   (rate(obs_cache_hits_total[5m]) + rate(obs_cache_misses_total[5m]))
	// This approach is more flexible — dashboards can compute rates over any window.
	ch <- prometheus.MustNewConstMetric(c.hitsDesc, prometheus.CounterValue, data.HitsTotal)
	ch <- prometheus.MustNewConstMetric(c.missesDesc, prometheus.CounterValue, data.MissesTotal)
	ch <- prometheus.MustNewConstMetric(c.evictionsDesc, prometheus.CounterValue, data.EvictionsTotal)

	// Cache size is a gauge — it fluctuates as entries are added and evicted.
	ch <- prometheus.MustNewConstMetric(c.sizeBytesDesc, prometheus.GaugeValue, data.SizeBytes)
}
