// dbpool.go collects database connection pool metrics.
//
// Why this matters for SRE:
// DB pool saturation is one of the most common causes of application-level
// outages. When active connections approach max_connections, new queries start
// queuing — causing cascading latency increases across the entire request path.
// Standard infrastructure exporters don't expose these app-level pool internals,
// so teams often discover saturation only after users report slowness.
//
// Metrics exposed:
//   - obs_dbpool_active_connections (gauge): connections currently in use
//   - obs_dbpool_idle_connections (gauge): connections available in the pool
//   - obs_dbpool_max_connections (gauge): pool capacity limit
//   - obs_dbpool_wait_duration_seconds (histogram): time waiting for a connection
package collector

import (
	"github.com/ethan-k-francis/observability-toolkit/internal/simulator"
	"github.com/prometheus/client_golang/prometheus"
)

// DBPoolCollector exposes database connection pool health metrics.
// Each metric is defined by a prometheus.Desc (created once) and populated
// with current values on every Collect() call.
type DBPoolCollector struct {
	sim *simulator.Simulator

	// Descriptors are immutable metadata about each metric: name, help text,
	// and label dimensions. They're created once in the constructor and reused
	// on every scrape.
	activeDesc   *prometheus.Desc
	idleDesc     *prometheus.Desc
	maxDesc      *prometheus.Desc
	waitDuration *prometheus.Desc
}

// NewDBPoolCollector creates metric descriptors for DB pool metrics.
// Naming convention follows Prometheus best practices:
//   - Namespace: "obs" (observability toolkit)
//   - Subsystem: "dbpool" (database connection pool)
//   - Unit suffix: "_seconds" for duration, "_connections" for counts
func NewDBPoolCollector(sim *simulator.Simulator) *DBPoolCollector {
	return &DBPoolCollector{
		sim: sim,
		activeDesc: prometheus.NewDesc(
			"obs_dbpool_active_connections",
			"Number of active database connections currently in use",
			nil, nil,
		),
		idleDesc: prometheus.NewDesc(
			"obs_dbpool_idle_connections",
			"Number of idle database connections available in the pool",
			nil, nil,
		),
		maxDesc: prometheus.NewDesc(
			"obs_dbpool_max_connections",
			"Maximum number of connections allowed in the pool",
			nil, nil,
		),
		waitDuration: prometheus.NewDesc(
			"obs_dbpool_wait_duration_seconds",
			"Time spent waiting for a database connection from the pool",
			nil, nil,
		),
	}
}

// Describe sends all DB pool metric descriptors to the channel.
func (c *DBPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.activeDesc
	ch <- c.idleDesc
	ch <- c.maxDesc
	ch <- c.waitDuration
}

// Collect gathers current DB pool metrics from the simulator and sends
// them as Prometheus metric objects.
func (c *DBPoolCollector) Collect(ch chan<- prometheus.Metric) {
	data := c.sim.DBPoolMetrics()

	// Gauge metrics represent point-in-time values that can go up or down.
	// active_connections fluctuates as requests acquire and release connections.
	ch <- prometheus.MustNewConstMetric(c.activeDesc, prometheus.GaugeValue, data.ActiveConnections)
	ch <- prometheus.MustNewConstMetric(c.idleDesc, prometheus.GaugeValue, data.IdleConnections)
	ch <- prometheus.MustNewConstMetric(c.maxDesc, prometheus.GaugeValue, data.MaxConnections)

	// Histogram metrics capture the distribution of observed values across
	// predefined buckets. This enables percentile queries in PromQL:
	//   histogram_quantile(0.95, rate(obs_dbpool_wait_duration_seconds_bucket[5m]))
	ch <- prometheus.MustNewConstHistogram(
		c.waitDuration,
		data.WaitDuration.Count,
		data.WaitDuration.Sum,
		data.WaitDuration.Buckets,
	)
}
