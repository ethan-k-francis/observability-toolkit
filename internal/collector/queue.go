// queue.go collects message queue metrics.
//
// Why this matters for SRE:
// Queue depth and processing latency are leading indicators of system health.
// A rising queue depth signals backpressure — the system is receiving work
// faster than it can process it. This is often the earliest warning of
// downstream degradation, appearing minutes before CPU or memory alerts fire.
// By the time infrastructure metrics react, users are already experiencing
// timeouts and errors.
//
// Metrics exposed:
//   - obs_queue_depth (gauge): messages waiting to be processed
//   - obs_queue_messages_processed_total (counter): successful message count
//   - obs_queue_messages_failed_total (counter): failed message count
//   - obs_queue_processing_duration_seconds (histogram): per-message latency
package collector

import (
	"github.com/ethan-k-francis/observability-toolkit/internal/simulator"
	"github.com/prometheus/client_golang/prometheus"
)

// QueueCollector exposes message queue health metrics.
type QueueCollector struct {
	sim *simulator.Simulator

	depthDesc    *prometheus.Desc
	processedDesc *prometheus.Desc
	failedDesc   *prometheus.Desc
	durationDesc *prometheus.Desc
}

// NewQueueCollector creates metric descriptors for queue metrics.
func NewQueueCollector(sim *simulator.Simulator) *QueueCollector {
	return &QueueCollector{
		sim: sim,
		depthDesc: prometheus.NewDesc(
			"obs_queue_depth",
			"Current number of messages waiting in the queue",
			nil, nil,
		),
		processedDesc: prometheus.NewDesc(
			"obs_queue_messages_processed_total",
			"Total number of messages successfully processed",
			nil, nil,
		),
		failedDesc: prometheus.NewDesc(
			"obs_queue_messages_failed_total",
			"Total number of messages that failed processing",
			nil, nil,
		),
		durationDesc: prometheus.NewDesc(
			"obs_queue_processing_duration_seconds",
			"Time taken to process each message",
			nil, nil,
		),
	}
}

// Describe sends all queue metric descriptors to the channel.
func (c *QueueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.depthDesc
	ch <- c.processedDesc
	ch <- c.failedDesc
	ch <- c.durationDesc
}

// Collect gathers current queue metrics from the simulator.
func (c *QueueCollector) Collect(ch chan<- prometheus.Metric) {
	data := c.sim.QueueMetrics()

	// Queue depth is a gauge — it fluctuates as messages arrive and are consumed.
	// A sustained increase indicates backpressure (producers outpacing consumers).
	ch <- prometheus.MustNewConstMetric(c.depthDesc, prometheus.GaugeValue, data.Depth)

	// Counters only increase over time. Prometheus calculates rates from counters
	// using rate() or increase(), so we report the cumulative total and let
	// PromQL compute the per-second rate:
	//   rate(obs_queue_messages_processed_total[5m])
	ch <- prometheus.MustNewConstMetric(c.processedDesc, prometheus.CounterValue, data.ProcessedTotal)
	ch <- prometheus.MustNewConstMetric(c.failedDesc, prometheus.CounterValue, data.FailedTotal)

	// Processing duration histogram enables latency percentile queries:
	//   histogram_quantile(0.99, rate(obs_queue_processing_duration_seconds_bucket[5m]))
	// This is how we detect "the p99 latency crossed our 2s SLO threshold."
	ch <- prometheus.MustNewConstHistogram(
		c.durationDesc,
		data.ProcessingDuration.Count,
		data.ProcessingDuration.Sum,
		data.ProcessingDuration.Buckets,
	)
}
