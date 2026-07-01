// /ide/cmd/simulation/pieChart/case1/main.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package main

// case1 — Sends slowly drifting values to 3 slices of a PieChart.
//
// Each slice oscillates at a different speed, creating a pie that
// continuously rebalances. Visually demonstrates the proportional
// nature of the pie chart.
//
// Usage:
//
//	go run ./cmd/simulation/pieChart/case1

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

const (
	serverURL = "http://localhost:8080"
	projectID = "8ee7dc528ac83eba6c2e428b22ef7c25"
	apiKey    = "d0eebd00512967109d6953403ddc6887aa7bb358f5761efb0e74c94686e1a899"
	deviceID  = "pie_1"
	interval  = 200 * time.Millisecond
)

type webhookItem struct {
	DeviceID string  `json:"device_id"`
	Port     string  `json:"port"`
	Value    float64 `json:"value"`
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════╗")
	fmt.Println("║  PieChart Simulation — 3 slices drifting  ║")
	fmt.Println("╚═══════════════════════════════════════════╝")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("%s/api/v1/webhook/%s", serverURL, projectID)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	tick := 0

	for {
		select {
		case <-sigCh:
			fmt.Println("\n✓ Stopped.")
			return
		case now := <-ticker.C:
			tick++
			t := now.Sub(start).Seconds()

			// Three slices with different oscillation speeds.
			// All values stay positive (pie ignores negatives).
			s0 := 30 + 25*math.Sin(t*0.5) // slow
			s1 := 40 + 20*math.Sin(t*0.8) // medium
			s2 := 20 + 15*math.Sin(t*1.3) // fast

			s0 = math.Round(s0*10) / 10
			s1 = math.Round(s1*10) / 10
			s2 = math.Round(s2*10) / 10

			payload := []webhookItem{
				{DeviceID: deviceID, Port: "s0", Value: s0},
				{DeviceID: deviceID, Port: "s1", Value: s1},
				{DeviceID: deviceID, Port: "s2", Value: s2},
			}

			body, _ := json.Marshal(payload)
			req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", apiKey)

			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("  ✗ tick %d: %v\n", tick, err)
				continue
			}
			resp.Body.Close()

			if tick%5 == 0 {
				fmt.Printf("  ✓ tick %4d  s0=%.1f  s1=%.1f  s2=%.1f\n", tick, s0, s1, s2)
			}
		}
	}
}
