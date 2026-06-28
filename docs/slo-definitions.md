# Health Targets — Service Level Objectives (SLOs)

A **Service Level Objective (SLO)** is a plain agreement about what "healthy" looks like.

Each SLO is built from a **Service Level Indicator (SLI)** — the specific number you measure — plus a target and an alert rule.

This doc defines five health targets for the demo app: what we measure, what "good" means, and when to alert.

| Part | Question it answers |
|---|---|
| **Service Level Indicator (SLI)** | Which number tells us if things are OK? |
| **Service Level Objective (SLO)** | What value is acceptable? |
| **Alert** | When do we notify someone? |

---

## 1. Database connection pool

**Analogy:** A parking lot with a fixed number of spaces. When almost full, new cars wait.

| Field | Value |
|---|---|
| **Service Level Indicator (SLI)** | Active connections ÷ max connections |
| **Service Level Objective (SLO)** | Below 85% full |
| **Alert name** | `HighDBPoolUtilization` |
| **Fires when** | Above 85% for 5 minutes |
| **Severity** | Warning |

**Why 85%?** Leaves headroom for short traffic spikes. Sustained overload above this causes queueing and slow responses.

**Why wait 5 minutes?** Brief spikes (e.g. a batch job) are normal. We only alert on sustained problems.

```promql
obs_dbpool_active_connections / obs_dbpool_max_connections > 0.85
```

---

## 2. Queue backlog

**Analogy:** A line at a coffee shop. A long line means the baristas can't keep up.

| Field | Value |
|---|---|
| **Service Level Indicator (SLI)** | Messages waiting in the queue |
| **Service Level Objective (SLO)** | Fewer than 100 messages waiting |
| **Alert name** | `QueueBackpressure` |
| **Fires when** | Over 100 for 3 minutes |
| **Severity** | Warning |

**Why 100?** At ~50 messages/second processing, 100 messages ≈ 2 seconds of delay — when users start to feel it.

**Why 3 minutes?** Queues naturally swell and shrink. Short bursts are OK; sustained growth is not.

```promql
obs_queue_depth > 100
```

---

## 3. Cache effectiveness

**Analogy:** A desk drawer for frequently used files. If you keep missing the drawer, you walk to the filing cabinet every time (slow).

| Field | Value |
|---|---|
| **Service Level Indicator (SLI)** | Cache hits ÷ (hits + misses) over 5 minutes |
| **Service Level Objective (SLO)** | At least 80% hit rate |
| **Alert name** | `LowCacheHitRate` |
| **Fires when** | Below 80% for 10 minutes |
| **Severity** | Warning |

**Why 80%?** Below this, the cache stops helping much — too many requests hit the slow backing store.

**Why 10 minutes?** Caches are "cold" after restarts. Allow time to warm up before alerting.

```promql
rate(obs_cache_hits_total[5m])
/ (rate(obs_cache_hits_total[5m]) + rate(obs_cache_misses_total[5m]))
< 0.80
```

---

## 4. Slow processing (99th percentile / p99)

**Analogy:** Average wait time can look fine while some customers wait forever. We watch the slowest 1% — the **99th percentile (p99)**.

| Field | Value |
|---|---|
| **Service Level Indicator (SLI)** | 99th percentile (p99) processing time |
| **Service Level Objective (SLO)** | Under 2 seconds |
| **Alert name** | `HighProcessingLatency` |
| **Fires when** | p99 over 2s for 5 minutes |
| **Severity** | Warning |

**Why p99?** Average hides outliers. The 99th percentile (p99) catches real user pain without being as noisy as the 99.9th percentile (p99.9).

**Why 2 seconds?** Beyond this, downstream timeouts and cascading failures become likely.

```promql
histogram_quantile(0.99,
  rate(obs_queue_processing_duration_seconds_bucket[5m])
) > 2
```

---

## 5. Exporter is running

**Analogy:** If the smoke detector's battery is dead, you won't know about a fire.

| Field | Value |
|---|---|
| **Service Level Indicator (SLI)** | Is the exporter responding? (`up` metric) |
| **Service Level Objective (SLO)** | Available 99.9% of the time |
| **Alert name** | `ExporterDown` |
| **Fires when** | Down for 1 minute |
| **Severity** | Critical |

**Why 1 minute?** Without the exporter, all other alerts go blind. Fast detection matters.

```promql
up{job="observability-exporter"} == 0
```

---

## Alert summary

| Alert | Severity | How long before firing | What to do |
|---|---|---|---|
| ExporterDown | Critical | 1 min | Restart exporter, check network |
| HighDBPoolUtilization | Warning | 5 min | Increase pool size, check slow queries |
| QueueBackpressure | Warning | 3 min | Add consumers, check downstream services |
| LowCacheHitRate | Warning | 10 min | Check cache size, keys, eviction settings |
| HighProcessingLatency | Warning | 5 min | Profile slow paths, check dependencies |
