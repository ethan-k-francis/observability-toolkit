"""
Scenario: Kill the exporter container and verify Prometheus detects the outage.

How it works:
    1. Find the running exporter container via the Docker API
    2. Stop the container (simulates a crash or deployment failure)
    3. Wait for Prometheus to detect the missing scrape target
    4. Query the Prometheus alerts API for "ExporterDown"
    5. Restart the container to restore normal operation

The ExporterDown alert is configured with a 1-minute for-duration, so we
wait ~90 seconds (1 min evaluation + buffer for scrape interval) before
checking. This validates the fastest path from failure to detection.
"""

import time

import click
import docker
import requests


def run(exporter_url: str, prometheus_url: str) -> bool:
    """Execute the kill-exporter chaos scenario.

    Args:
        exporter_url: Base URL of the exporter (used for pre-check).
        prometheus_url: Base URL of Prometheus (used to verify alerts).

    Returns:
        True if the ExporterDown alert fired successfully, False otherwise.
    """
    # Step 1: Verify the exporter is running before we kill it.
    # This catches misconfiguration early (wrong URL, container not started).
    click.echo("Checking exporter is reachable...")
    try:
        resp = requests.get(f"{exporter_url}/health", timeout=5)
        resp.raise_for_status()
        click.echo(f"  Exporter is UP (status {resp.status_code})")
    except requests.RequestException as exc:
        click.echo(f"ERROR: Exporter not reachable at {exporter_url}: {exc}")
        return False

    # Step 2: Find and stop the exporter container via Docker SDK.
    # We use a name filter because Docker Compose generates predictable
    # container names: <project>-<service>-<replica>.
    click.echo("Connecting to Docker...")
    client = docker.from_env()
    containers = client.containers.list(filters={"name": "exporter"})

    if not containers:
        click.echo("ERROR: No exporter container found. Is the stack running?")
        return False

    container = containers[0]
    click.echo(f"  Found container: {container.name} ({container.short_id})")

    # Step 3: Stop the container. This triggers Prometheus to see the
    # target as "down" on the next scrape (within 15 seconds).
    click.echo(f"Stopping container: {container.name}")
    container.stop(timeout=10)
    click.echo("  Container stopped")

    # Step 4: Wait for Prometheus to evaluate the ExporterDown alert.
    # Alert has `for: 1m`, plus we need at least one scrape interval (15s)
    # for Prometheus to notice the target is gone. Total: ~90s with buffer.
    wait_seconds = 90
    click.echo(f"Waiting {wait_seconds}s for alert evaluation...")
    for remaining in range(wait_seconds, 0, -10):
        click.echo(f"  {remaining}s remaining...")
        time.sleep(min(10, remaining))

    # Step 5: Check Prometheus for the ExporterDown alert.
    click.echo("Querying Prometheus for ExporterDown alert...")
    try:
        resp = requests.get(f"{prometheus_url}/api/v1/alerts", timeout=10)
        resp.raise_for_status()
        alerts = resp.json().get("data", {}).get("alerts", [])

        exporter_down = [
            a for a in alerts
            if a.get("labels", {}).get("alertname") == "ExporterDown"
        ]

        if exporter_down:
            state = exporter_down[0].get("state", "unknown")
            click.echo(f"SUCCESS: ExporterDown alert is {state}")
            success = True
        else:
            click.echo("FAILURE: ExporterDown alert did not fire")
            click.echo(f"  Total alerts found: {len(alerts)}")
            for alert in alerts:
                name = alert.get("labels", {}).get("alertname", "?")
                click.echo(f"    - {name}: {alert.get('state', '?')}")
            success = False

    except requests.RequestException as exc:
        click.echo(f"ERROR: Failed to query Prometheus: {exc}")
        success = False

    # Step 6: Always restart the container to restore normal operation.
    click.echo(f"Restarting container: {container.name}")
    container.start()
    click.echo("  Container restarted")

    return success
