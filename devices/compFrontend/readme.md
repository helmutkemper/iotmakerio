# compFrontend

Frontend visualization components for the IoTMaker IDE.

## Purpose

This package contains **dual-workspace devices** — components that exist simultaneously
on the **backend stage** (data/logic canvas) and the **frontend stage** (end-user
dashboard). The backend representation shows data connectors and properties; the
frontend representation shows a live, interactive visualization.

## How it fits in the project

```
factoryDevice/factory.go
    └── creates compFrontend.StatementGauge (and future components)
            ├── backend stage: data box with input connectors (max, current, min)
            └── frontend stage: interactive gauge visualization
```

- **Backend element** — wired into the codegen pipeline via connectors. Receives
  values from other devices through the wire manager.
- **Frontend element** — renders a visual widget (gauge, chart, LED, etc.) that
  updates in real time via `LiveUpdate()` from the WebSocket live system.

## Current components

| Component              | Description                                         |
|------------------------|-----------------------------------------------------|
| `StatementGauge`       | Semicircular speedometer gauge with slider overlay  |
| `StatementLED`         | On/off indicator light with toggle interaction      |
| `StatementBarGraph`    | Vertical bar indicator with proportional fill       |
| `StatementTextDisplay` | Resizable monospace text viewer with Monaco overlay |
| `StatementButton`      | Toggle push button with 3D press/release visual     |
| `StatementSevenSeg`    | Classic LCD seven-segment numeric display           |
| `StatementKnob`        | Rotary dial with click-to-set angle interaction     |
| `StatementChart`       | Real-time line/area/sparkline/sweep chart           |

## Architecture pattern

Every frontend component follows the dual-element pattern:

1. **Init()** creates elements on both stages (backend + frontend)
2. **RegisterConnectors()** registers input ports on the backend wire manager
3. **LiveUpdate(port, value)** receives real-time data from external hardware
4. **SendValue(port, value)** sends user interaction back to hardware
5. **GetDeviceType()** returns a stable string for scene serialization

## Interaction lock (standard for all components)

Every compFrontend component that allows the user to send data back to hardware
**must** expose an `interactionLocked` field and a corresponding `"Lock Interaction"`
checkbox in its Inspect panel (`GetInspectConfig()`).

When locked:
- The frontend element becomes **read-only** (clicks are ignored, no overlay opens)
- `LiveUpdate()` still works — data FROM hardware is always received
- `SendValue()` is blocked — data TO hardware is NOT sent
- A lock icon (🔒) appears on both backend and frontend SVGs

This is essential for dashboards where the gauge (or LED, knob, etc.) should only
display data, never send commands accidentally.

## Dependencies

- `sprite` — canvas element abstraction (Stage, Element)
- `wire` — connection manager for backend data flow
- `ui/overlay` — property inspection panel
- `ui/mainMenu` — hex menu integration
- `live` (via `SendFunc`) — WebSocket communication with external hardware
