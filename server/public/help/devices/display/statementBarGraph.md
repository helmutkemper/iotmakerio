# BarGraph

A vertical bar indicator that fills proportionally to a value.

## What it does

The BarGraph displays an integer value as a vertical bar that fills from
bottom to top. Ideal for battery level, signal strength, volume, tank
level, or any percentage-like metric.

## How to use

1. Place the BarGraph from the **Display** menu
2. Connect **int** outputs to the `max`, `value`, and `min` inputs
3. Switch to the **Frontend** tab to see the live bar

The bar fills proportionally: when `value` equals `min` the bar is empty,
when `value` equals `max` the bar is full.

## Interactive mode

By default, clicking the bar on the frontend opens a **slider overlay**
(same as the Gauge) allowing the user to adjust the value and send it
to external hardware via WebSocket.

To make the bar **read-only**, check **Lock Interaction** in the Inspect
panel. A 🔒 icon will appear.

## Properties

| Property           | Type     | Description                                  |
|--------------------|----------|----------------------------------------------|
| ID                 | text     | Unique identifier for wiring and code gen    |
| Label              | text     | Display name shown below the backend box     |
| Min                | number   | Minimum scale value (empty bar)              |
| Max                | number   | Maximum scale value (full bar)               |
| Value              | number   | Current value                                |
| Fill Color         | color    | Bar fill color (default: blue #5599FF)       |
| Track Color        | color    | Background track color                       |
| Lock Interaction   | checkbox | Prevents slider overlay from opening         |

## Connectors

| Port      | Direction | Type | Description              |
|-----------|-----------|------|--------------------------|
| `max`     | Input     | int  | Maximum scale value      |
| `current` | Input     | int  | Current value to display |
| `min`     | Input     | int  | Minimum scale value      |
