# Pie Chart

Real-time pie or donut chart with **1–8 slices**. Each slice represents the latest value received on its input port, proportional to the total.

## Quick Start

1. Place the Pie Chart on the canvas
2. In **Inspect → Properties**, set **Slice Count** (default: 3)
3. Connect each `s0`, `s1`, … input to a data source
4. Each value received updates the corresponding slice size

## Ports

| Port | Type          | Description   |
|------|---------------|---------------|
| `s0` | int / float64 | Slice 0 value |
| `s1` | int / float64 | Slice 1 value |
| …    | …             | Up to `s7`    |

## Modes

- **Pie** — classic filled pie chart
- **Donut** — ring chart with total value displayed in the center

Toggle between modes with the **Donut Mode** checkbox in Properties.

## Properties

| Property         | Default | Description                                                   |
|------------------|---------|---------------------------------------------------------------|
| Slice Count      | 3       | Number of input slices (1–8). **Changing resets all values.** |
| Donut Mode       | off     | Ring chart with center total                                  |
| Show Legend      | on      | Color-coded labels below the chart                            |
| Show Percentages | on      | % labels on slices (hidden when slice < 5%)                   |

## Per-Slice Configuration (Slices tab)

- **Label** — display name (legend + backend connector)
- **Color** — slice color (Catppuccin palette defaults)

## Tips

- Negative values are ignored (treated as 0)
- Slices with 0 value are not drawn
- The percentage label only appears on slices ≥ 5% to avoid clutter
- In donut mode, the center shows the sum of all slice values
