// /ide/cmd/simulation/chartPro/case2/main.go

package main

// case2 — Sends sine on s0 and cosine on s1 to ChartPro.
//
// Two series running simultaneously with 90° phase difference.
// Set Series Count = 2 in the ChartPro Inspect panel before running.
//
// Usage:
//
//	go run ./cmd/simulation/chartPro/case2
//
// Português:
//
//	Envia seno em s0 e cosseno em s1. Duas séries com 90° de defasagem.
//	Configure Series Count = 2 no painel Inspect do ChartPro antes de rodar.

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
	serverURL  = "http://localhost:8080"
	projectID  = "50e44bcca3e807f611b16e190d63d069"
	apiKey     = "1e6d1fe8972052f0eca870268d3cb7775fc090b2177366af1ba7cbdb687cc849"
	deviceID   = "chartPro_1"
	interval   = 50 * time.Millisecond
	sinePeriod = 3 * time.Second
	amplitude  = 50.0
	offset     = 50.0
)

type webhookItem struct {
	DeviceID string  `json:"device_id"`
	Port     string  `json:"port"`
	Value    float64 `json:"value"`
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║  ChartPro Simulation — Case 2 (sin/cos s0+s1) ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	client := &http.Client{Timeout: 2 * time.Second}
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

			elapsed := now.Sub(startTime).Seconds()
			phase := elapsed / sinePeriod.Seconds() * 2 * math.Pi

			// s0: sine wave (0–100)
			s0 := math.Round((offset+amplitude*math.Sin(phase))*100) / 100

			// s1: cosine wave (0–100) — 90° ahead of s0
			s1 := math.Round((offset+amplitude*math.Cos(phase))*100) / 100

			if err := send(client, webhookURL, s0, s1); err != nil {
				fmt.Printf("  ✗ tick %d: %v\n", tick, err)
			} else if tick%20 == 0 {
				fmt.Printf("  ✓ tick %4d  s0=%.2f  s1=%.2f\n", tick, s0, s1)
			}
		}
	}
}

// send posts both series in a single batch request.
func send(client *http.Client, url string, s0, s1 float64) error {
	payload := []webhookItem{
		{DeviceID: deviceID, Port: "s0", Value: s0},
		{DeviceID: deviceID, Port: "s1", Value: s1},
	}

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
