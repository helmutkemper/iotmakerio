# liveapi — Live device communication

## What it does

Real-time bidirectional communication between the IoTMaker IDE (browser) and external hardware/scripts. Enables frontend UI components (Gauge, etc.) to receive sensor data and send commands back to hardware.

## Architecture

```
Hardware ──POST──▶ Webhook ──▶ Redis "in" ──▶ Hub ──▶ WebSocket ──▶ Browser
Browser ──▶ WebSocket ──▶ Hub ──▶ Redis "out" ──▶ (Future: Forwarder/MQTT)
```

## Dependencies

No new dependencies. Uses `golang.org/x/net/websocket` (transitive dependency of Echo v4).

## Quick start

### 1. Create an API key

```bash
curl -X POST http://localhost:8080/api/v1/live/keys \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"project_id":"proj123","device_id":"gauge_0","label":"Garage sensor"}'
```

Save the `api_key` from the response — it will not be shown again.

### 2. Send data from hardware

```bash
curl -X POST http://localhost:8080/api/v1/webhook/proj123/gauge_0 \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <key>" \
  -d '{"port":"current","value":73}'
```

### 3. Connect from browser

```javascript
const ws = new WebSocket('ws://localhost:8080/ws/live/proj123?token=<jwt>');

ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  // { device_id: "gauge_0", port: "current", value: 73, ts: 1712345678 }
};

ws.send(JSON.stringify({ device_id: "gauge_0", port: "current", value: 85 }));
```

## Message format

```json
{ "device_id": "gauge_0", "port": "current", "value": 73, "ts": 1712345678 }
```

## Security

- **Browser**: JWT via `?token=` query param (browsers can't send headers during WS upgrade)
- **Hardware**: API key per device via `X-API-Key` header. SHA-256 hash stored, never the raw key
- **Isolation**: one active key per device; compromising one key doesn't affect others
- **No mandatory expiry**: hardware in the field can't easily rotate credentials
- **Soft revoke**: keys are never deleted, only marked with `revoked_at` for audit

## Files

| File          | Purpose                                            |
|---------------|----------------------------------------------------|
| `hub.go`      | WebSocket connection manager + Redis PubSub bridge |
| `handlers.go` | WebSocket upgrade, webhook receiver, API key CRUD  |
| `routes.go`   | Register() — route wiring                          |
