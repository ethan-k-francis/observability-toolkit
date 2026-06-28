#!/usr/bin/env python3
"""
Chaos engineering runner for the observability-toolkit.

This CLI tool executes chaos scenarios against the exporter and validates
that Prometheus alerts fire correctly. It proves the observability pipeline
works end-to-end: metric spike → Prometheus detection → alert firing.

The idea behind chaos engineering is simple: if you don't test your
monitoring in failure conditions, you don't know if it works. This runner
automates that testing.

Usage:
    python chaos_runner.py kill    --exporter-url http://localhost:9090
    python chaos_runner.py spike   --target dbpool --multiplier 5
    python chaos_runner.py stress  --duration 30
"""

import sys

import click
import requests


@click.group()
def cli():
    """Chaos engineering toolkit for observability validation.

    Each command runs a specific chaos scenario and validates that the
    monitoring pipeline detects the injected failure.
    """
    pass


def check_prometheus_alerts(prometheus_url: str, alert_name: str = None) -> list:
    """Query Prometheus for currently firing alerts.

    Args:
        prometheus_url: Base URL of the Prometheus instance.
        alert_name: Optional filter — only return alerts with this name.

    Returns:
        List of alert dicts from the Prometheus API.
    """
    try:
        resp = requests.get(
            f"{prometheus_url}/api/v1/alerts",
            timeout=10,
        )
        resp.raise_for_status()
        alerts = resp.json().get("data", {}).get("alerts", [])

        if alert_name:
            # Filter to only the alert we're looking for.
            alerts = [
                a for a in alerts
                if a.get("labels", {}).get("alertname") == alert_name
            ]
        return alerts

    except requests.RequestException as exc:
        click.echo(f"ERROR: Failed to query Prometheus: {exc}", err=True)
        return []


@cli.command()
@click.option(
    "--exporter-url",
    default="http://localhost:9090",
    help="Exporter base URL",
)
@click.option(
    "--prometheus-url",
    default="http://localhost:9091",
    help="Prometheus base URL",
)
def kill(exporter_url: str, prometheus_url: str):
    """Kill the exporter container and verify Prometheus detects it.

    This scenario validates the ExporterDown alert by stopping the exporter
    container and checking that Prometheus fires the alert within the
    expected timeframe (1 minute for-duration + scrape interval).
    """
    from scenarios.kill_exporter import run
    success = run(exporter_url, prometheus_url)
    sys.exit(0 if success else 1)


@cli.command()
@click.option(
    "--exporter-url",
    default="http://localhost:9090",
    help="Exporter base URL",
)
@click.option(
    "--prometheus-url",
    default="http://localhost:9091",
    help="Prometheus base URL",
)
@click.option(
    "--target",
    default="dbpool",
    type=click.Choice(["dbpool", "queue", "cache"]),
    help="Which metric subsystem to spike",
)
@click.option(
    "--multiplier",
    default=5.0,
    type=float,
    help="How much to multiply metric values (e.g., 5.0 = 5x normal)",
)
@click.option(
    "--duration",
    default=60,
    type=int,
    help="How long to spike metrics in seconds",
)
def spike(
    exporter_url: str,
    prometheus_url: str,
    target: str,
    multiplier: float,
    duration: int,
):
    """Spike metrics via the /chaos endpoint to trigger SLO alerts.

    This scenario activates the exporter's chaos mode, which multiplies
    the target subsystem's metrics to simulate degradation. It then waits
    for the alert evaluation window and checks Prometheus for fired alerts.
    """
    from scenarios.spike_metrics import run
    success = run(exporter_url, prometheus_url, target, multiplier, duration)
    sys.exit(0 if success else 1)


@cli.command()
@click.option(
    "--container",
    default="observability-toolkit-exporter-1",
    help="Docker container name for the exporter",
)
@click.option(
    "--cpu-workers",
    default=2,
    type=int,
    help="Number of CPU stress workers to spawn",
)
@click.option(
    "--duration",
    default=30,
    type=int,
    help="Duration of stress test in seconds",
)
def stress(container: str, cpu_workers: int, duration: int):
    """Stress the exporter container's CPU and memory resources.

    This scenario executes resource-intensive processes inside the exporter
    container to simulate resource contention and validate that metrics
    continue to be served under load.
    """
    from scenarios.resource_stress import run
    success = run(container, cpu_workers, duration)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    cli()
