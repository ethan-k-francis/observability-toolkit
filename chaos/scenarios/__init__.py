"""
Chaos engineering scenarios for observability-toolkit validation.

Each scenario tests a different failure mode and validates that the
monitoring pipeline (metrics → Prometheus → alerts → detection) works
correctly under adverse conditions.

Available scenarios:
    kill_exporter  — Stop the exporter container, verify ExporterDown alert
    spike_metrics  — Spike metrics via /chaos endpoint, verify SLO alerts
    resource_stress — CPU/memory stress on the exporter container
"""
