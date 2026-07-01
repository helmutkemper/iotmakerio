# live — Real-time device communication (WASM client)

## What it does

Bridges the server's WebSocket endpoint to WASM device instances. When external hardware sends data via webhook, the data flows through the server Hub → WebSocket → this client → the target device's `LiveUpdate()` method.

## Architecture

```
Server WebSocket ──onmessage──▶ live.Client ──FindDevice──▶ SceneMgr
                                     │                          │
                                     │                    device.LiveUpdate()
                                     │                          │
                                     ◀──── SendFunc ◄───── device interaction
```

## How it works

1. `NewClient(sceneMgr)` creates a client bound to the backend SceneMgr.
2. `Connect()` reads `window._ideAuthToken` and `window._ideProjectID` from the SPA.
3. Opens a JS WebSocket to `ws(s)://host/ws/live/{projectID}?token={jwt}`.
4. On incoming message, parses `{device_id, port, value}`, finds the device via `SceneMgr.FindDevice()`, and calls `LiveUpdate(port, value)` if the device implements `scene.LiveUpdatable`.
5. Auto-reconnects on disconnect (3s delay).
6. `Send(deviceID, port, value)` sends outbound messages from devices to hardware.

## Prerequisites

The SPA must set two globals before the WASM loads:

```javascript
window._ideAuthToken = "Bearer " + jwtToken;  // already exists
window._ideProjectID = "your-project-id";      // new — set by ide.js
```

For testing without the SPA project flow, set `_ideProjectID` in the browser console before loading the WASM.

## Interface

Devices that want to receive live data must implement `scene.LiveUpdatable`:

```go
type LiveUpdatable interface {
    LiveUpdate(port string, value []byte) error
}
```

The `value` parameter is raw JSON (number, string, bool, object). The device is responsible for parsing it.

## Files

| File        | Purpose                                               |
|-------------|-------------------------------------------------------|
| `client.go` | WebSocket bridge — connect, dispatch, send, reconnect |
