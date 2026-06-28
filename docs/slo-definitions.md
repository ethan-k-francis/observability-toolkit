# SLO Definitions

Service Level Objectives for the observability toolkit, defining what "healthy" means for each subsystem and how we detect breaches.

## SLO Framework

Each SLO consists of three parts:

| Component | What it answers |
|---|---|
| **SLI** (Service Level Indicator) | What are we measuring? |
| **SLO** (Service Level Objective) | What's the target? |
| **Alert** | How do we know when we've breached? |

---

## 1. DB Pool Utilization

| Field | Value |
|---|---|
| **SLI** | `obs_dbpool_active_connections / obs_dbpool_max_connections` |
| **SLO Target** | Utilization < 85% |
| **Alert** | `HighDBPoolUtilization` — fires when > 85% for 5 minutes |
| **Severity** | Warning |

### Rationale

Connection pool saturation causes cascading latency. Above 85%, the pool is nearly exhausted — new requests queue waiting for a connection, and tail latency increases non-linearly. The 85% threshold provides enough headroom for traffic spikes while catching sustained overload.

**Why 5 minutes?** Brief spikes during request bursts are normal (e.g., a batch job starts). The 5-minute for-duration filters out transient peaks and only fires when the pool is genuinely saturated.

### PromQL

```promql
obs_dbpool_active_connections / obs_dbpool_max_connections > 0.85
```

---

## 2. Queue Backpressure

| Field | Value |
|---|---|
| **SLI** | `obs_queue_depth` (current messages waiting) |
| **SLO Target** | Queue depth < 100 messages |
| **Alert** | `QueueBackpressure` — fires when > 100 for 3 minutes |
| **Severity** | Warning |

### Rationale

Queue depth is a leading indicator of system health. When depth exceeds 100, consumers can't keep up with producers. If sustained, messages will eventually be dropped or timeout, directly impacting users.

**Why 100 messages?** At a baseline processing rate of ~50 msg/sec, a depth of 100 represents ~2 seconds of backlog. This is the point where processing latency starts to noticeably impact end-user experience.

**Why 3 minutes?** Queues naturally fluctuate. The 3-minute window allows for normal burst absorption (e.g., a batch of events from an upstream service) while catching genuine capacity problems.

### PromQL

```promql
obs_queue_depth > 100
```

---

## 3. Cache Hit Rate

| Field | Value |
|---|---|
| **SLI** | `rate(hits[5m]) / (rate(hits[5m]) + rate(misses[5m]))` |
| **SLO Target** | Hit rate >= 80% |
| **Alert** | `LowCacheHitRate` — fires when < 80% for 10 minutes |
| **Severity** | Warning |

### Rationale

Cache hit rate directly correlates with response time. Below 80%, more than 1 in 5 requests hit the slower backing store (database, API, disk), significantly increasing average latency and load on downstream systems.

**Why 80%?** This is the inflection point where the cache stops providing meaningful acceleration. Production caches typically achieve 90-95%; falling below 80% indicates a structural problem (wrong eviction policy, insufficient size, key distribution skew).

**Why 10 minutes?** Cache hit rate drops temporarily after restarts (cold cache) or after configuration changes. The 10-minute for-duration allows for warm-up while catching persistent degradation like cache thrashing or size misconfiguration.

### PromQL

```promql
rate(obs_cache_hits_total[5m])
/ (rate(obs_cache_hits_total[5m]) + rate(obs_cache_misses_total[5m]))
< 0.80
```

---

## 4. Processing Latency (p99)

| Field | Value |
|---|---|
| **SLI** | `histogram_quantile(0.99, rate(duration_bucket[5m]))` |
| **SLO Target** | p99 latency < 2 seconds |
| **Alert** | `HighProcessingLatency` — fires when p99 > 2s for 5 minutes |
| **Severity** | Warning |

### Rationale

The p99 latency captures the experience of the slowest 1% of requests. A p99 above 2 seconds means a non-trivial fraction of users are experiencing unacceptable delays, even if the median looks healthy.

**Why p99 instead of p95 or average?** Average masks outliers. p95 is too forgiving for latency-sensitive systems. p99 catches tail latency problems that affect real users without being as noisy as p99.9.

**Why 2 seconds?** For message processing, 2 seconds is the threshold where downstream timeouts start to trigger, creating error cascading. Adjust this based on your SLA requirements.

**Why 5 minutes?** Transient latency spikes (GC pauses, network blips, cold starts) are normal. The 5-minute window ensures we're alerting on sustained degradation, not momentary hiccups.

### PromQL

```promql
histogram_quantile(0.99,
  rate(obs_queue_processing_duration_seconds_bucket[5m])
) > 2
```

---

## 5. Exporter Availability

| Field | Value |
|---|---|
| **SLI** | `up{job="observability-exporter"}` (Prometheus built-in) |
| **SLO Target** | 99.9% availability |
| **Alert** | `ExporterDown` — fires when down for 1 minute |
| **Severity** | Critical |

### Rationale

The exporter is the foundation of the entire observability pipeline. If it's down, we lose visibility into all application health signals. This is a meta-SLO — it protects the monitoring system itself.

**Why 1 minute?** Losing observability is critical because you can't diagnose problems you can't see. The aggressive 1-minute threshold ensures rapid detection. False positives from brief network blips are acceptable for a critical alert.

**Why critical severity?** All other SLOs depend on the exporter being available. A warning-level alert for exporter downtime could be deprioritized, leading to extended blindness.

### PromQL

```promql
up{job="observability-exporter"} == 0
```

---

## Alert Escalation Matrix

| Alert | Severity | For Duration | Action |
|---|---|---|---|
| ExporterDown | Critical | 1m | Page on-call, restart exporter |
| HighDBPoolUtilization | Warning | 5m | Scale pool, investigate queries |
| QueueBackpressure | Warning | 3m | Scale consumers, check downstream |
| LowCacheHitRate | Warning | 10m | Check cache config, size, keys |
| HighProcessingLatency | Warning | 5m | Profile processing, check deps |
