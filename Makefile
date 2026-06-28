# Observability Toolkit — Build, orchestration, and chaos engineering targets
#
# Usage: make <target>
# Run `make help` to see all available targets.

BINARY := observability-toolkit
COMPOSE_PROJECT := observability-toolkit

.PHONY: build test lint up down clean help
.PHONY: chaos-run chaos-kill chaos-spike chaos-stress

# --- Build & Test ---

## build: Compile the Go exporter binary
build:
	go build -o bin/$(BINARY) ./cmd/exporter/

## test: Run all Go tests with race detection enabled
test:
	go test -race -v ./...

## lint: Run golangci-lint for static analysis
lint:
	golangci-lint run ./...

# --- Docker Compose ---

## up: Start all services (exporter, Prometheus, Grafana) in detached mode
up:
	docker compose -p $(COMPOSE_PROJECT) up -d --build

## down: Stop and remove all containers
down:
	docker compose -p $(COMPOSE_PROJECT) down

## clean: Remove build artifacts and tear down containers with volumes
clean:
	rm -rf bin/
	docker compose -p $(COMPOSE_PROJECT) down -v --remove-orphans

# --- Chaos Engineering ---
# These targets run chaos scenarios that inject failures and validate
# that the monitoring pipeline (metrics → Prometheus → alerts) works.
# Prerequisites: stack must be running (make up) and Python deps installed
# (pip install -r chaos/requirements.txt).

## chaos-run: Show available chaos engineering scenarios
chaos-run:
	@echo "Chaos Engineering Scenarios"
	@echo "==========================="
	@echo ""
	@echo "Prerequisites:"
	@echo "  1. Stack is running: make up"
	@echo "  2. Python deps installed: pip install -r chaos/requirements.txt"
	@echo ""
	@echo "Available scenarios:"
	@echo "  make chaos-kill    Kill exporter, verify ExporterDown alert"
	@echo "  make chaos-spike   Spike DB pool metrics to trigger alerts"
	@echo "  make chaos-stress  CPU/memory stress on exporter container"
	@echo ""
	@echo "Or run directly:"
	@echo "  cd chaos && python3 chaos_runner.py --help"

## chaos-kill: Kill the exporter container and verify ExporterDown alert fires
chaos-kill:
	cd chaos && python3 chaos_runner.py kill

## chaos-spike: Spike DB pool metrics to trigger HighDBPoolUtilization alert
chaos-spike:
	cd chaos && python3 chaos_runner.py spike --target dbpool --multiplier 3 --duration 360

## chaos-stress: Stress test the exporter container with concurrent load
chaos-stress:
	cd chaos && python3 chaos_runner.py stress --duration 30

# --- Local CI ---

.PHONY: lint-ci ci-security pr-commit-check ci
lint-ci:
	pre-commit run --all-files

ci-security:
	trivy fs --severity HIGH,CRITICAL --exit-code 1 .

pr-commit-check:
	@chmod +x .github/scripts/commit-message-lint.sh
	@.github/scripts/commit-message-lint.sh --base-ref origin/main

ci: lint-ci ci-security pr-commit-check
	@echo "Local CI checks passed."

# --- Help ---

## help: Show available targets
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
