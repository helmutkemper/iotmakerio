# Chart Pro — Multi-Series Chart

Real-time chart with **1–8 independent data series**, dual Y axis, time window, alert zones, and per-series chart type (line or scatter).

## Quick Start

1. Place Chart Pro on the canvas
2. In **Inspect → Properties**, set **Series Count** (1–8)
3. Connect each `s0`, `s1`, … input to a data source
4. Data flows in and the chart updates live

## Ports

| Port | Type          | Description    |
|------|---------------|----------------|
| `s0` | int / float64 | Series 0 input |
| `s1` | int / float64 | Series 1 input |
| …    | …             | Up to `s7`     |

## Features

### Chart Type (Phase 3)
Each series can render as **Line** (connected polyline) or **Scatter** (individual dots). Mix both on the same chart — e.g. s0 as line, s1 as scatter. Set per series in the **Series** tab.

### Time Window (Phase 2)
Set **Time Window (sec)** > 0 to switch the X axis from sample-count to wall clock time. Points older than the window are automatically pruned.  Set to 0 to use buffer-count mode.

### Alert Zones (Phase 2)
Up to 4 horizontal bands behind the data lines. Each zone has min/max Y values (left axis), color, opacity, and an optional label. Use for marking normal/warning/critical ranges.

### Dual Y Axis (Phase 2)
Each series can be assigned to the **Left** or **Right** Y axis. Each axis auto-scales independently. Useful for overlaying signals with very different scales (e.g. temperature 20–30°C on the left vs pressure 980–1040 hPa on the right).

### Per-Series Configuration
- **Label** — display name (legend + backend connector)
- **Type** — Line or Scatter
- **Color** — line/dot color (Catppuccin palette defaults)
- **Glow Effect** — soft glow behind the line or larger halo on dots
- **Y Axis** — Left or Right

## Properties

| Property      | Default | Description                                              |
|---------------|---------|----------------------------------------------------------|
| Series Count  | 1       | Number of inputs (1–8). **Changing resets all buffers.** |
| Buffer        | 100     | Max data points per series (10–2000)                     |
| Time Window   | 0       | Seconds of visible data (0 = off, use buffer count)      |
| Auto Scale    | ✓       | Fit Y axes to visible data                               |
| Min Y / Max Y | 0 / 100 | Fixed Y range (when Auto Scale off)                      |
| Show Legend   | ✓       | Color-coded series labels below the chart                |

## Webhook Example

```bash
API_KEY="your-key" PROJECT_ID="your-project" \
CHART_ID="chartPro_1" MODE="ecg" INTERVAL=0.05 \
./simulation.sh
```

The script sends data to ports `s0`, `s1`, `s2` simultaneously.
