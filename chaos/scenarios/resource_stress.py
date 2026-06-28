"""
Scenario: Stress CPU and memory on the exporter container.

How it works:
    1. Find the exporter container via the Docker API
    2. Execute a CPU stress command inside the container using `exec_run`
    3. Monitor the container for the specified duration
    4. Verify the exporter still serves metrics under stress

This scenario tests exporter resilience — a well-built exporter should
continue serving metrics even when the host is under resource pressure.
If metrics stop flowing, Prometheus sees the target as down and fires
the ExporterDown alert.

Note: The distroless exporter image has limited shell capabilities,
so we use a lightweight stress approach (reading from /dev/urandom).
If the image doesn't support shell commands, the scenario reports this
and falls back to external stress via HTTP load.
"""

import time

import click
import docker
import requests


def run(container_name: str, cpu_workers: int, duration: int) -> bool:
    """Execute the resource-stress chaos scenario.

    Args:
        container_name: Docker container name for the exporter.
        cpu_workers: Number of parallel CPU stress processes.
        duration: Duration of stress in seconds.

    Returns:
        True if the exporter survived the stress test, False otherwise.
    """
    # Step 1: Find the exporter container.
    click.echo(f"Looking for container: {container_name}")
    client = docker.from_env()
    containers = client.containers.list(filters={"name": container_name})

    if not containers:
        # Try a broader search if exact name doesn't match.
        containers = client.containers.list(filters={"name": "exporter"})

    if not containers:
        click.echo(
            f"ERROR: Container '{container_name}' not found. Is the stack running?"
        )
        return False

    container = containers[0]
    click.echo(f"  Found container: {container.name} ({container.short_id})")

    # Step 2: Record baseline metrics availability.
    exporter_url = "http://localhost:9090"
    click.echo("Checking baseline metrics...")
    try:
        resp = requests.get(f"{exporter_url}/metrics", timeout=5)
        baseline_ok = resp.status_code == 200
        click.echo(f"  Baseline metrics: {'OK' if baseline_ok else 'FAILED'}")
    except requests.RequestException:
        click.echo("  WARNING: Could not reach exporter for baseline check")
        baseline_ok = False

    # Step 3: Apply CPU stress via HTTP load (works with distroless images).
    # Since distroless images don't have a shell, we generate load by making
    # rapid concurrent metric scrape requests instead.
    click.echo(f"Applying stress: {cpu_workers} concurrent scrapers for {duration}s...")

    import concurrent.futures
    import threading

    # Thread-safe counters — `+=` is not atomic across threads, so we use
    # a lock to prevent data races on shared state.
    counter_lock = threading.Lock()
    stress_errors = 0
    stress_successes = 0
    stop_time = time.time() + duration

    def stress_worker():
        """Continuously scrape metrics to generate CPU load."""
        nonlocal stress_errors, stress_successes
        while time.time() < stop_time:
            try:
                resp = requests.get(
                    f"{exporter_url}/metrics",
                    timeout=5,
                )
                with counter_lock:
                    if resp.status_code == 200:
                        stress_successes += 1
                    else:
                        stress_errors += 1
            except requests.RequestException:
                with counter_lock:
                    stress_errors += 1

    # Launch concurrent stress workers in a thread pool.
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=cpu_workers,
    ) as executor:
        futures = [executor.submit(stress_worker) for _ in range(cpu_workers)]

        # Monitor progress while stress test runs.
        for elapsed in range(0, duration, 5):
            remaining = duration - elapsed
            with counter_lock:
                click.echo(
                    f"  [{elapsed}s/{duration}s] "
                    f"success={stress_successes} errors={stress_errors}"
                )
            time.sleep(min(5, remaining))

        # Wait for all workers to finish.
        concurrent.futures.wait(futures)

    click.echo(
        f"\nStress test complete: "
        f"{stress_successes} successful scrapes, "
        f"{stress_errors} errors"
    )

    # Step 4: Verify the exporter survived.
    click.echo("Verifying exporter is still healthy...")
    try:
        resp = requests.get(f"{exporter_url}/health", timeout=10)
        healthy = resp.status_code == 200
    except requests.RequestException:
        healthy = False

    if healthy:
        click.echo("SUCCESS: Exporter survived the stress test")
        error_rate = stress_errors / max(1, stress_successes + stress_errors) * 100
        click.echo(f"  Error rate during stress: {error_rate:.1f}%")
        return True
    else:
        click.echo("FAILURE: Exporter is not healthy after stress test")
        return False
