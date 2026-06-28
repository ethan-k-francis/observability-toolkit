// Package collector implements a custom Prometheus collector that exposes
// application-specific health signals.
//
// Why implement prometheus.Collector instead of using NewGaugeVec/NewCounterVec?
//
// The Collector interface lets us generate metrics on-demand during each
// Prometheus scrape. This is essential when metrics come from external systems
// (databases, queues, caches) that we query at collection time. The simpler
// registration pattern (NewGaugeVec.Set()) requires updating metrics proactively,
// which adds complexity and can produce stale values if the update loop stalls.
//
// Architecture:
//
//	AppCollector (implements prometheus.Collector)
//	├── DBPoolCollector  — database connection pool metrics
//	├── QueueCollector   — message queue metrics
//	└── CacheCollector   — cache performance metrics
package collector

import (
	"github.com/ethan-k-francis/observability-toolkit/internal/simulator"
	"github.com/prometheus/client_golang/prometheus"
)

// AppCollector aggregates all sub-collectors into a single prometheus.Collector.
// Prometheus calls Describe() once at registration time to learn about all
// possible metrics, then calls Collect() on every scrape to get current values.
type AppCollector struct {
	dbPool *DBPoolCollector
	queue  *QueueCollector
	cache  *CacheCollector
}

// NewAppCollector creates the top-level collector. All sub-collectors share
// the same Simulator instance so metrics are generated from a consistent
// internal state.
func NewAppCollector(sim *simulator.Simulator) *AppCollector {
	return &AppCollector{
		dbPool: NewDBPoolCollector(sim),
		queue:  NewQueueCollector(sim),
		cache:  NewCacheCollector(sim),
	}
}

// Describe sends all metric descriptors to the provided channel. Prometheus
// uses these to validate that Collect() doesn't return unexpected metrics
// and to generate the HELP/TYPE lines in the exposition format.
func (c *AppCollector) Describe(ch chan<- *prometheus.Desc) {
	c.dbPool.Describe(ch)
	c.queue.Describe(ch)
	c.cache.Describe(ch)
}

// Collect is called by Prometheus on every scrape (default: every 15s).
// It delegates to each sub-collector, which queries the simulator for
// current metric values and sends them as prometheus.Metric objects.
func (c *AppCollector) Collect(ch chan<- prometheus.Metric) {
	c.dbPool.Collect(ch)
	c.queue.Collect(ch)
	c.cache.Collect(ch)
}
