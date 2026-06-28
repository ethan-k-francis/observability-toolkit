"""
Scenario: Spike metrics via the /chaos endpoint to trigger SLO alerts.

How it works:
    1. POST to the exporter's /chaos endpoint with target, multiplier, and duration
    2. The exporter's simulator multiplies the target subsystem's metrics
    3. Wait for the alert evaluation window (for-duration + buffer)
    4. Query Prometheus for any firing alerts

This scenario validates that the SLO-based alerts respond correctly to
degraded application metrics. For example, spiking "dbpool" with 3x multiplier
should push active connections above 85% of max, triggering HighDBPoolUtilization.

Expected alert mappings:
    dbpool × 3  → HighDBPoolUtilization (active/max > 0.85 for 5m)
    queue  × 5  → QueueBackpressure (depth > 100 for 3m)
    cache  × 3  → LowCacheHitRate (hit_rate < 0.80 for 10m)
"""

import time

import click
import requests


def run(
    exporter_url: str,
    prometheus_url: str,
    target: str,
    multiplier: float,
    duration: int,
) -> bool:
    """Execute the spike-metrics chaos scenario.

    Args:
        exporter_url: Base URL of the exporter.
        prometheus_url: Base URL of Prometheus.
        target: Subsystem to spike ("dbpool", "queue", or "cache").
        multiplier: Factor to multiply metric values by.
        duration: How long to keep chaos active (seconds).

    Returns:
        True if at least one related alert fired, False otherwise.
    """
    click.echo(
        f"Activating chaos: target={target} multiplier={multiplier}x "
        f"duration={duration}s"
    )

    # Step 1: Activate chaos mode on the exporter.
    # The /chaos endpoint accepts a JSON payload and tells the simulator
    # to multiply the target subsystem's metrics for the specified duration.
    try:
        resp = requests.post(
            f"{exporter_url}/chaos",
            json={
                "target": target,
                "multiplier": multiplier,
                "duration": duration,
            },
            timeout=10,
        )
        if resp.status_code != 200:
            click.echo(
                f"ERROR: Chaos endpoint returned {resp.status_code}: "
                f"{resp.text.strip()}"
            )
            return False
        click.echo(f"  Response: {resp.text.strip()}")
    except requests.RequestException as exc:
        click.echo(f"ERROR: Failed to reach exporter: {exc}")
        return False

    # Step 2: Wait for Prometheus to evaluate alerts.
    # The wait time depends on the alert's for-duration:
    #   dbpool: 5m for-duration → wait ~6 min
    #   queue:  3m for-duration → wait ~4 min
    #   cache:  10m for-duration → wait ~11 min
    # We use a shorter wait for demo purposes and check periodically.
    wait_seconds = min(duration + 60, 360)
    click.echo(
        f"Monitoring for alerts over {wait_seconds}s "
        f"(chaos active for {duration}s)..."
    )

    # Poll Prometheus periodically rather than waiting the full duration.
    # This gives us earlier detection and better feedback during demos.
    check_interval = 30
    alerts_found = []

    for elapsed in range(0, wait_seconds, check_interval):
        remaining = wait_seconds - elapsed
        click.echo(f"  [{elapsed}s elapsed, {remaining}s remaining] Checking alerts...")
        time.sleep(min(check_interval, remaining))

        try:
            resp = requests.get(
                f"{prometheus_url}/api/v1/alerts",
                timeout=10,
            )
            resp.raise_for_status()
            alerts = resp.json().get("data", {}).get("alerts", [])

            # Look for any firing alerts (not just pending).
            firing = [a for a in alerts if a.get("state") == "firing"]
            if firing:
                alerts_found = firing
                click.echo(f"  Found {len(firing)} firing alert(s)!")
                break

            # Show pending alerts as progress feedback.
            pending = [a for a in alerts if a.get("state") == "pending"]
            if pending:
                names = [
                    a.get("labels", {}).get("alertname", "?") for a in pending
                ]
                click.echo(f"  Pending alerts: {', '.join(names)}")

        except requests.RequestException:
            click.echo("  (Prometheus query failed, retrying...)")

    # Step 3: Report results.
    if alerts_found:
        click.echo(f"\nSUCCESS: {len(alerts_found)} alert(s) firing:")
        for alert in alerts_found:
            name = alert.get("labels", {}).get("alertname", "unknown")
            summary = alert.get("annotations", {}).get("summary", "N/A")
            click.echo(f"  - {name}: {summary}")
        return True
    else:
        click.echo(
            "\nNo alerts fired within the monitoring window. "
            "This may be expected if the for-duration is longer than "
            "the chaos duration. Try increasing --duration."
        )
        return False
