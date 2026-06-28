// Package simulator generates realistic metric values for the observability
// toolkit exporter. It uses sine waves with random noise to produce values
// that fluctuate naturally over time — mimicking real application behavior
// where metrics drift with traffic patterns, time of day, and load.
//
// In a production exporter, this package would be replaced with actual
// instrumentation clients that query real databases, message queues, and
// caches. The simulator exists to demonstrate the full observability pipeline
// without requiring external infrastructure.
//
// Thread safety: All public methods are safe for concurrent use. The simulator
// is accessed from both the Prometheus scrape goroutine (via collectors) and
// the HTTP handler goroutine (via the /chaos endpoint).
package simulator

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// --- Data types returned by the simulator ---

// DBPoolData holds a snapshot of database connection pool metrics.
type DBPoolData struct {
	ActiveConnections float64
	IdleConnections   float64
	MaxConnections    float64
	WaitDuration      HistogramData
}

// QueueData holds a snapshot of message queue metrics.
type QueueData struct {
	Depth              float64
	ProcessedTotal     float64
	FailedTotal        float64
	ProcessingDuration HistogramData
}

// CacheData holds a snapshot of cache performance metrics.
type CacheData struct {
	HitsTotal      float64
	MissesTotal    float64
	EvictionsTotal float64
	SizeBytes      float64
}

// HistogramData represents a Prometheus histogram snapshot.
// Prometheus histograms consist of cumulative bucket counts, a total
// observation count, and the sum of all observed values.
type HistogramData struct {
	Count   uint64
	Sum     float64
	Buckets map[float64]uint64
}

// --- Simulator core ---

// Simulator generates realistic metric values using sine waves, noise, and
// monotonically increasing counters. It also supports a "chaos mode" that
// artificially spikes metrics to test alerting pipelines.
type Simulator struct {
	mu        sync.Mutex
	startTime time.Time
	rng       *rand.Rand

	// Monotonic counters — these only increase over time, matching how
	// Prometheus counters work. We accumulate based on elapsed time and
	// a simulated rate (with sine-wave variation for realism).
	processedTotal float64
	failedTotal    float64
	hitsTotal      float64
	missesTotal    float64
	evictionsTotal float64

	// Histogram observation tracking — we accumulate count and sum over time
	// to simulate a real histogram that grows with each observation.
	dbWaitCount    uint64
	dbWaitSum      float64
	queueProcCount uint64
	queueProcSum   float64

	// Timestamp of the last counter advancement, used to calculate dt
	// (time delta) for rate-based counter increments.
	lastUpdate time.Time

	// Chaos mode — when active, a target subsystem's metrics are multiplied
	// to simulate degradation (high load, slow responses, etc.).
	chaosTarget     string
	chaosMultiplier float64
	chaosExpiry     time.Time
}

// New creates a Simulator with initial state. The start time seeds the
// sine wave phase so metrics begin mid-oscillation (more realistic than
// always starting from zero).
func New() *Simulator {
	now := time.Now()
	return &Simulator{
		startTime:  now,
		rng:        rand.New(rand.NewSource(now.UnixNano())),
		lastUpdate: now,
	}
}

// sineWithNoise generates a value oscillating around `base` with the given
// amplitude and period (in seconds), plus uniform random noise.
//
// Example: sineWithNoise(40, 15, 300, 5) produces values between ~20-60
// with a 5-minute oscillation period and ±5 random jitter.
func (s *Simulator) sineWithNoise(base, amplitude, period, noise float64) float64 {
	elapsed := time.Since(s.startTime).Seconds()
	sine := base + amplitude*math.Sin(2*math.Pi*elapsed/period)
	jitter := (s.rng.Float64() - 0.5) * 2 * noise
	return math.Max(0, sine+jitter)
}

// chaosMultiplierFor returns the chaos multiplier for a given target subsystem.
// Returns 1.0 (no effect) if chaos mode is inactive or expired.
// Must be called with the mutex held.
func (s *Simulator) chaosMultiplierFor(target string) float64 {
	if s.chaosTarget == target && time.Now().Before(s.chaosExpiry) {
		return s.chaosMultiplier
	}
	return 1.0
}

// advanceCounters increments monotonic counters based on elapsed time since
// the last update. This is called at the start of each metric snapshot method.
//
// The approach: each counter has a base rate (e.g., 50 messages/sec) that
// oscillates via sine wave. We multiply rate × dt to get the increment.
// This ensures counters always increase regardless of scrape interval.
// Must be called with the mutex held.
func (s *Simulator) advanceCounters() {
	now := time.Now()
	dt := now.Sub(s.lastUpdate).Seconds()
	if dt < 0.001 {
		return
	}
	s.lastUpdate = now

	// Apply chaos multipliers to the appropriate subsystems during
	// counter advancement (not on the returned totals) so counters
	// remain monotonically increasing even after chaos mode expires.
	queueMult := s.chaosMultiplierFor("queue")
	cacheMult := s.chaosMultiplierFor("cache")
	dbMult := s.chaosMultiplierFor("dbpool")

	// Queue counters: ~50 processed/sec, ~2 failed/sec at baseline.
	// Chaos mode on "queue" increases the failure rate.
	s.processedTotal += s.sineWithNoise(50, 10, 120, 5) * dt
	s.failedTotal += s.sineWithNoise(2, 1, 60, 0.5) * dt * queueMult

	// Cache counters: ~200 hits/sec, ~40 misses/sec, ~5 evictions/sec.
	// Chaos mode on "cache" increases misses and evictions.
	s.hitsTotal += s.sineWithNoise(200, 30, 90, 10) * dt
	s.missesTotal += s.sineWithNoise(40, 10, 90, 5) * dt * cacheMult
	s.evictionsTotal += s.sineWithNoise(5, 2, 180, 1) * dt * cacheMult

	// Histogram observations: simulate individual request latencies.
	// Each observation is drawn from a normal distribution centered on
	// a realistic mean, with chaos mode shifting the mean higher.
	observations := int(math.Max(1, s.sineWithNoise(10, 3, 60, 2)*dt))
	for i := 0; i < observations; i++ {
		// DB wait: baseline ~50ms, chaos shifts mean upward
		wait := math.Abs(s.rng.NormFloat64()*0.02 + 0.05*dbMult)
		s.dbWaitCount++
		s.dbWaitSum += wait

		// Queue processing: baseline ~500ms, chaos shifts mean upward
		proc := math.Abs(s.rng.NormFloat64()*0.2 + 0.5*queueMult)
		s.queueProcCount++
		s.queueProcSum += proc
	}
}

// generateBuckets creates histogram bucket counts from a simulated normal
// distribution. Each bucket's cumulative count is derived from the normal
// CDF (cumulative distribution function), which naturally produces
// non-decreasing counts — a requirement for valid Prometheus histograms.
//
// Parameters:
//   - bounds: histogram bucket upper bounds (e.g., {0.01, 0.05, 0.1, ...})
//   - mean: center of the distribution (e.g., 0.05 for 50ms)
//   - stddev: spread of the distribution
//   - totalCount: total number of observations to distribute across buckets
func (s *Simulator) generateBuckets(bounds []float64, mean, stddev float64, totalCount uint64) map[float64]uint64 {
	buckets := make(map[float64]uint64, len(bounds))
	for _, bound := range bounds {
		// Normal CDF: P(X <= bound) = 0.5 * (1 + erf((bound - mean) / (stddev * sqrt(2))))
		// This gives the fraction of observations at or below this bucket boundary.
		z := (bound - mean) / (stddev * math.Sqrt2)
		fraction := 0.5 * (1 + math.Erf(z))
		buckets[bound] = uint64(float64(totalCount) * fraction)
	}
	return buckets
}

// --- Public metric snapshot methods ---

// DBPoolMetrics returns a snapshot of database connection pool state.
// Active connections oscillate around 40 (of 100 max), with chaos mode
// pushing utilization toward the pool limit.
func (s *Simulator) DBPoolMetrics() DBPoolData {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceCounters()

	mult := s.chaosMultiplierFor("dbpool")
	maxConn := 100.0
	active := s.sineWithNoise(40, 15, 300, 5) * mult
	active = math.Min(active, maxConn)
	idle := math.Max(0, maxConn-active)

	// Histogram buckets for wait duration (seconds).
	// Normal distribution centered on 50ms baseline, chaos shifts right.
	buckets := s.generateBuckets(
		[]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		0.05*mult, 0.02*mult,
		s.dbWaitCount,
	)

	return DBPoolData{
		ActiveConnections: active,
		IdleConnections:   idle,
		MaxConnections:    maxConn,
		WaitDuration: HistogramData{
			Count:   s.dbWaitCount,
			Sum:     s.dbWaitSum,
			Buckets: buckets,
		},
	}
}

// QueueMetrics returns a snapshot of message queue state.
// Queue depth oscillates around 30 at baseline, with chaos mode causing
// backpressure (depth spikes above the 100-message alert threshold).
func (s *Simulator) QueueMetrics() QueueData {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceCounters()

	mult := s.chaosMultiplierFor("queue")
	depth := s.sineWithNoise(30, 20, 180, 10) * mult

	// Histogram buckets for processing duration (seconds).
	// Normal distribution centered on 500ms baseline, chaos shifts right.
	buckets := s.generateBuckets(
		[]float64{0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0},
		0.5*mult, 0.2*mult,
		s.queueProcCount,
	)

	return QueueData{
		Depth:          depth,
		ProcessedTotal: s.processedTotal,
		FailedTotal:    s.failedTotal,
		ProcessingDuration: HistogramData{
			Count:   s.queueProcCount,
			Sum:     s.queueProcSum,
			Buckets: buckets,
		},
	}
}

// CacheMetrics returns a snapshot of cache performance.
// Cache size oscillates around 50MB. During chaos mode on "cache",
// miss and eviction rates increase (simulating cache thrashing).
func (s *Simulator) CacheMetrics() CacheData {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceCounters()

	sizeBytes := s.sineWithNoise(50*1024*1024, 10*1024*1024, 600, 1024*1024)

	return CacheData{
		HitsTotal:      s.hitsTotal,
		MissesTotal:    s.missesTotal,
		EvictionsTotal: s.evictionsTotal,
		SizeBytes:      sizeBytes,
	}
}

// ActivateChaos enables chaos mode for a specific target subsystem.
// The multiplier is applied to the target's metrics for the specified duration.
//
// Valid targets: "dbpool", "queue", "cache"
//
// Example: ActivateChaos("dbpool", 3.0, 60) triples DB pool utilization
// and wait times for 60 seconds, which should trigger HighDBPoolUtilization.
func (s *Simulator) ActivateChaos(target string, multiplier float64, durationSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chaosTarget = target
	s.chaosMultiplier = multiplier
	s.chaosExpiry = time.Now().Add(time.Duration(durationSec) * time.Second)
}
