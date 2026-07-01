// /ide/cmd/simulation/chartPro/case1/main.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package main

// case1 — Sends a continuous sine wave to ChartPro series s0.
//
// This is the simplest possible simulation: one series, one waveform,
// no configuration. Run it and watch the chart draw a smooth sine curve.
//
// The program sends data to the IoTMaker webhook endpoint every 50ms
// (20 updates/sec). The sine wave completes one full cycle every 3 seconds,
// giving a visually pleasing result at the default buffer size of 100 points.
//
// Usage:
//
//	go run ./cmd/simulation/chartPro/case1
//
// The program runs indefinitely until interrupted with Ctrl+C.
//
// Português:
//
//	Envia uma onda senoidal contínua para a série s0 do ChartPro.
//	Simulação mais simples possível: uma série, uma forma de onda,
//	sem configuração. Execute e observe o gráfico desenhando a curva.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ── Fixed configuration — no flags, no env vars, just run it ──────────

const (
	// serverURL is the base URL of the IoTMaker server.
	serverURL = "http://localhost:8080"

	// projectID is the project identifier for the webhook endpoint.
	// Must match the project ID configured in the IDE's Live Settings.
	projectID = "50e44bcca3e807f611b16e190d63d069"

	// apiKey authenticates the webhook request.
	// Must match an API key created in the IoTMaker portal (Live → API Keys).
	apiKey = "1e6d1fe8972052f0eca870268d3cb7775fc090b2177366af1ba7cbdb687cc849"

	// deviceID is the ChartPro component ID on the canvas.
	// Default name assigned by the IDE when the component is first placed.
	deviceID = "chartPro_1"

	// port is the series input port. "s0" = first series.
	port = "s0"

	// interval is the time between data points.
	// 50ms = 20 updates/sec — smooth enough for real-time visualization.
	interval = 50 * time.Millisecond

	// sinePeriod is how long one full sine cycle takes.
	// 3 seconds gives a readable waveform at 20 updates/sec.
	sinePeriod = 3 * time.Second

	// amplitude is the peak value of the sine wave (range: -amplitude to +amplitude).
	amplitude = 50.0

	// offset shifts the sine wave vertically so all values are positive.
	// With amplitude=50 and offset=50, values range from 0 to 100.
	offset = 50.0
)

// webhookItem matches the server's expected JSON structure.
// See server/handler/liveapi/handlers.go → webhookItem.
type webhookItem struct {
	DeviceID string  `json:"device_id"`
	Port     string  `json:"port"`
	Value    float64 `json:"value"`
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════╗")
	fmt.Println("║  ChartPro Simulation — Case 1 (sine s0)  ║")
	fmt.Println("╠═══════════════════════════════════════════╣")
	fmt.Printf("║  Server:   %s\n", serverURL)
	fmt.Printf("║  Device:   %s\n", deviceID)
	fmt.Printf("║  Port:     %s\n", port)
	fmt.Printf("║  Interval: %s\n", interval)
	fmt.Println("╚═══════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	// Graceful shutdown on SIGINT / SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// HTTP client with a short timeout — if the server is down,
	// we log the error and keep trying on the next tick.
	client := &http.Client{Timeout: 2 * time.Second}

	// webhookURL is the full endpoint path.
	webhookURL := fmt.Sprintf("%s/api/v1/webhook/%s", serverURL, projectID)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	startTime := time.Now()
	tick := 0

	for {
		select {
		case <-sigCh:
			fmt.Println("\n✓ Stopped.")
			return

		case now := <-ticker.C:
			tick++

			// Calculate sine value based on elapsed time.
			// Using wall clock time (not tick count) makes the waveform
			// independent of any jitter in the ticker interval.
			elapsed := now.Sub(startTime).Seconds()
			phase := elapsed / sinePeriod.Seconds() * 2 * math.Pi
			value := offset + amplitude*math.Sin(phase)

			// Round to 2 decimal places for cleaner display.
			value = math.Round(value*100) / 100

			// Send to webhook.
			if err := send(client, webhookURL, value); err != nil {
				fmt.Printf("  ✗ tick %d: %v\n", tick, err)
			} else if tick%20 == 0 {
				// Log every 20th tick (~1 second) to avoid flooding the terminal.
				fmt.Printf("  ✓ tick %4d  value=%.2f\n", tick, value)
			}
		}
	}
}

// send posts a single data point to the webhook endpoint.
func send(client *http.Client, url string, value float64) error {
	// Build the batch payload (array with one item).
	payload := []webhookItem{{
		DeviceID: deviceID,
		Port:     port,
		Value:    value,
	}}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return nil
}
