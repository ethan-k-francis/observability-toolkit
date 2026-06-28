// Package main is the entry point for the observability-toolkit exporter.
//
// This binary starts an HTTP server on :9090 with three endpoints:
//   - /metrics — Prometheus exposition format metrics
//   - /health  — health check for container orchestrators
//   - /chaos   — POST endpoint to artificially spike metrics (chaos engineering)
//
// The /chaos endpoint is the key addition in this phase. It allows the chaos
// engineering runner to inject metric anomalies without modifying the exporter's
// configuration or restarting it. This simulates real degradation scenarios
// (DB pool saturation, queue backpressure, cache thrashing) to validate that
// the alerting pipeline detects and fires the correct SLO-based alerts.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/ethan-k-francis/observability-toolkit/internal/collector"
	"github.com/ethan-k-francis/observability-toolkit/internal/simulator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// chaosRequest defines the JSON payload for the /chaos endpoint.
// The chaos runner sends this to activate metric spiking on a specific
// subsystem for a controlled duration.
type chaosRequest struct {
	// Target subsystem to spike: "dbpool", "queue", or "cache"
	Target string `json:"target"`
	// Multiplier applied to the target's metrics (e.g., 3.0 = 3x normal values)
	Multiplier float64 `json:"multiplier"`
	// Duration in seconds for the chaos effect to last
	Duration int `json:"duration"`
}

func main() {
	// Create the shared simulator instance. Both the collector (scrape path)
	// and the chaos handler (HTTP path) access this — the simulator uses a
	// mutex internally for thread safety.
	sim := simulator.New()

	// Register the custom collector that exposes DB pool, queue, and cache metrics.
	appCollector := collector.NewAppCollector(sim)
	prometheus.MustRegister(appCollector)

	// --- HTTP Endpoints ---

	// /metrics: Standard Prometheus scrape endpoint. Prometheus calls this
	// every 15s (configured in prometheus.yml) to collect all registered metrics.
	http.Handle("/metrics", promhttp.Handler())

	// /health: Returns 200 OK when the exporter is running. Used by Docker
	// healthcheck and Kubernetes probes to determine container readiness.
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// /chaos: Accepts POST requests with a JSON payload to artificially spike
	// metrics. This endpoint is used by the chaos engineering runner to validate
	// the alerting pipeline without needing to simulate real infrastructure failures.
	//
	// Example:
	//   curl -X POST http://localhost:9090/chaos \
	//     -H "Content-Type: application/json" \
	//     -d '{"target": "dbpool", "multiplier": 3.0, "duration": 300}'
	http.HandleFunc("/chaos", func(w http.ResponseWriter, r *http.Request) {
		// Only accept POST — GET would be confusing since this has side effects.
		if r.Method != http.MethodPost {
			http.Error(w, "POST method required", http.StatusMethodNotAllowed)
			return
		}

		var req chaosRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}

		// Validate the request. A multiplier <= 0 or duration <= 0 would have
		// no visible effect, which is confusing during chaos testing.
		if req.Target == "" || req.Multiplier <= 0 || req.Duration <= 0 {
			http.Error(
				w,
				"target, multiplier (>0), and duration (>0) are required",
				http.StatusBadRequest,
			)
			return
		}

		// Activate chaos mode on the simulator. The effect is time-limited —
		// it automatically expires after the specified duration.
		sim.ActivateChaos(req.Target, req.Multiplier, req.Duration)

		log.Printf(
			"chaos activated: target=%s multiplier=%.1f duration=%ds",
			req.Target, req.Multiplier, req.Duration,
		)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(
			w,
			"chaos activated: target=%s multiplier=%.1f duration=%ds\n",
			req.Target, req.Multiplier, req.Duration,
		)
	})

	log.Println("observability-toolkit exporter starting on :9090")
	log.Println("endpoints: /metrics, /health, /chaos")

	if err := http.ListenAndServe(":9090", nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
