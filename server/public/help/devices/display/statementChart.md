# Chart

A real-time chart that plots data points as they arrive from hardware.

## Modes

**Line** (default): standard line chart with Y axis labels and grid.

**Area**: same as Line but with a filled area below the curve. Good for
humidity, pressure, and other continuous measurements.

**Sparkline**: compact mini-chart with no axes, no grid, no labels.
Ideal for overview cards showing trends at a glance.

**Sweep (ECG)**: the cursor writes from left to right and wraps around
when it reaches the end — like a medical monitor. Combined with the
green ECG grid, this creates an authentic heart monitor display.

## How to use

1. Place the Chart from the **Display** menu
2. Connect an **int** output to the `current` input
3. Choose a mode and grid style in the Inspect panel
4. Switch to the **Frontend** tab to see live data

Each value received via LiveUpdate adds one point to the buffer. When
the buffer is full, the oldest point is removed (scroll mode) or
overwritten (sweep mode).

## Resize

Click the frontend chart to open a menu. Select **Resize** to adjust
the chart dimensions. Larger charts show more detail.

## Properties

| Property           | Type     | Description                                 |
|--------------------|----------|---------------------------------------------|
| ID                 | text     | Unique identifier                           |
| Label              | text     | Display name below the backend box          |
| Mode               | select   | Line, Area, Sparkline, or Sweep (ECG)       |
| Grid               | select   | Standard (grey) or ECG (green)              |
| Buffer             | number   | Data points to keep (10-600, default 60)    |
| Auto Scale         | checkbox | Auto-calculate Y range from data            |
| Min Y / Max Y      | number   | Manual Y range (when auto scale is off)     |
| Line Color         | color    | Chart line color                            |
| Glow Effect        | checkbox | Soft glow around the line (ECG style)       |
| Lock Interaction   | checkbox | Prevents context menu from opening          |

## Connectors

| Port      | Direction | Type      | Description             |
|-----------|-----------|-----------|-------------------------|
| `current` | Input     | int/float | Data point to plot      |

## Performance

Each data point triggers an SVG re-render. For typical IoT sensors
(1-10 readings per second), this works well. For high-frequency data
(>50Hz), consider reducing the buffer size or using a lower update rate.
