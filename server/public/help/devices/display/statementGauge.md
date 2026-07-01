# Gauge — Semicircular Speedometer

## Description

The **Gauge** is a dual device: a backend data node with three input connectors and
a frontend semicircular speedometer that displays the current value in real time.

It is the first **live-enabled** device in the IDE — it can receive data from
external hardware via webhooks and display it in the browser without reloading.

## Ports

| Port        | Direction | Type | Description                          |
|-------------|-----------|------|--------------------------------------|
| **max**     | Input     | int  | Maximum scale value (top of arc)     |
| **current** | Input     | int  | Current needle position              |
| **min**     | Input     | int  | Minimum scale value (bottom of arc)  |

## Behavior

The gauge has two visual representations:

- **Backend** (code stage): a rectangular box showing three rows (Max, Current, Min)
  with input connectors on the left side.
- **Frontend** (display stage): a semicircular speedometer with a colored arc,
  numerical value, and min/max labels.

Both representations update simultaneously when values change via the Properties
panel, wire connections, or live webhook data.

### Arc color

The arc color changes based on the ratio `(current - min) / (max - min)`:

| Ratio      | Color  | Meaning        |
|------------|--------|----------------|
| 0% – 50%   | Green  | Normal range   |
| 50% – 75%  | Yellow | Warning range  |
| 75% – 100% | Red    | Critical range |

## Live Communication

The Gauge supports real-time data from external hardware via the webhook system.

### Setup

1. Create a device-scoped API key:

```
POST /api/v1/live/keys
Authorization: Bearer <jwt>
{"project_id":"<project>","device_id":"<gauge_id>","label":"sensor"}
```

2. Save the `api_key` from the response — it is shown only once.

### Sending data

```
POST /api/v1/webhook/<project_id>/<device_id>
X-API-Key: <api_key>
Content-Type: application/json

{"port":"current","value":73}
```

Supported ports: `current`, `max`, `min`.

The value is delivered to the browser via WebSocket in real time.

### Example: temperature sensor

```bash
# Send temperature reading
curl -X POST http://localhost:8080/api/v1/webhook/myproject/gauge_1 \
  -H "X-API-Key: abc123..." \
  -H "Content-Type: application/json" \
  -d '{"port":"current","value":42}'
```

## Properties (Inspect Panel)

| Field   | Type   | Editable | Description                                   |
|---------|--------|----------|-----------------------------------------------|
| ID      | text   | Yes      | Device identifier (used in code and webhooks) |
| Label   | text   | Yes      | Display name below the backend box            |
| Min     | number | Yes      | Minimum scale value                           |
| Max     | number | Yes      | Maximum scale value                           |
| Current | number | Yes      | Current needle value                          |

**Changing the ID** updates the webhook endpoint and re-registers wire connectors.
Make sure to update the API key if you change the device ID after creating one.

## Connection Examples

### Simple: constant value display

```
ConstInt(75) → Gauge.current
ConstInt(0)  → Gauge.min
ConstInt(100)→ Gauge.max
```

### Live: hardware sensor

No wire connections needed — data arrives via webhook. Place the Gauge on the
stage, note its ID, create an API key, and start sending data from your hardware.

## Tips

- Use **Inspect** (hex menu) to change ID, label, and default values.
- The device ID is used in the webhook URL — choose something meaningful
  (e.g. `temperature_sensor`).
- The frontend speedometer renders at 3× resolution for crisp arcs.
- Both backend and frontend update together — change a value in Properties
  and see both views refresh immediately.
