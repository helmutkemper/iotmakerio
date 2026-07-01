// /live/client.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// live/client.go — Bidirectional live communication between browser and hardware.
//
// English:
//
//	The Client opens a JavaScript WebSocket connection from within WASM and
//	bridges incoming messages to device instances registered in the SceneMgr.
//
// Inbound flow (hardware → device):
//
//  1. WebSocket receives a JSON message: { device_id, port, value, ts }
//  2. Client looks up the device via SceneMgr.FindDevice(device_id)
//  3. If the device implements scene.LiveUpdatable, calls LiveUpdate(port, value)
//  4. The device updates its visual state (e.g. Gauge re-renders its SVG)
//
// Outbound flow (device → hardware):
//
//  1. Device calls Client.Send(deviceID, port, value)
//  2. Client serializes to JSON and sends via WebSocket
//
// Connection lifecycle:
//
//   - Connect() is called once after workspaces are initialized.
//   - It reads window._ideAuthToken (JWT) and localStorage["liveProjectID"]
//     from the SPA. If either is missing the client degrades gracefully.
//   - The WebSocket auto-reconnects on close after a brief delay.
//   - Listeners registered via OnReconnect are invoked whenever the
//     WebSocket transitions from "was connected" → "connected again".
//     Frontend display devices (ChartPro, Chart, PieChart) use this
//     hook to annotate their timelines with a FAIL marker, indicating
//     to the operator that there was an infrastructure interruption.
//
// Português:
//
//	Cliente de comunicação live. Abre WebSocket JS dentro do WASM e faz
//	ponte entre mensagens recebidas e devices registrados no SceneMgr.
//	Reconecta automaticamente. Se não houver token ou projeto, degrada sem erro.
//	Listeners de OnReconnect são chamados quando o WebSocket recupera
//	a conexão após uma queda, permitindo que displays marquem o evento.
package live

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/scene"
)

// ─── Message type ─────────────────────────────────────────────────────────────

// Message is the standard envelope for all live communication.
// Matches the server's LiveMessage struct.
//
// Português: Envelope padrão de comunicação live. Corresponde a
// LiveMessage no servidor.
type Message struct {
	DeviceID string          `json:"device_id"`
	Port     string          `json:"port"`
	Value    json.RawMessage `json:"value"`
	Ts       int64           `json:"ts"`
}

// ─── Client ───────────────────────────────────────────────────────────────────

// Client manages a WebSocket connection and dispatches messages to devices.
//
// The zero value is not usable — create via NewClient. The Client is safe
// to call from a single goroutine. Concurrent calls from multiple
// goroutines should serialize through scene.Serializer's own locking
// (FindDevice is the only call out, and it is goroutine-safe by contract).
type Client struct {
	sceneMgr *scene.Serializer

	// ws is the JavaScript WebSocket object. Null/undefined when disconnected.
	ws js.Value

	// connected tracks the current connection state. True between
	// onopen and onclose; false otherwise. Reads/writes happen from
	// the WS event goroutines and from external callers — guard with
	// connMu when in doubt.
	connected bool

	// jsFuncs holds JS callback references for cleanup. Without
	// keeping these alive the JS-side functions get garbage-collected
	// by the Go runtime and the WebSocket stops receiving events.
	jsFuncs []js.Func

	// projectID is read from localStorage on Connect().
	projectID string

	// token is the raw JWT (without "Bearer " prefix).
	token string

	// hadConnection records whether we ever observed a successful
	// onopen since the most recent dial. When a subsequent dial
	// succeeds, we use this flag to decide between "first connect"
	// (no fail notification) and "reconnect after drop" (fire
	// reconnect listeners).
	hadConnection bool

	// reconnectMu protects reconnectListeners.
	reconnectMu sync.RWMutex

	// reconnectListeners are notified each time the WebSocket
	// transitions from "was connected previously" → "open again".
	// They are NOT invoked on the very first successful connection,
	// only on subsequent ones — the first connect is not a "recovery".
	//
	// Listeners run on a dedicated goroutine; they must not block.
	reconnectListeners []func()
}

// NewClient creates a Client. The SceneMgr can be nil at creation time
// and set later via SetSceneMgr (needed when the client is created before
// the workspace is initialized).
//
// Call Connect() to start the WebSocket connection.
func NewClient(sceneMgr *scene.Serializer) *Client {
	return &Client{
		sceneMgr: sceneMgr,
	}
}

// SetSceneMgr sets the scene serializer for device lookup.
// Must be called before any messages arrive if the client was created
// with a nil SceneMgr.
func (c *Client) SetSceneMgr(mgr *scene.Serializer) {
	c.sceneMgr = mgr
}

// OnReconnect registers a callback that fires whenever the WebSocket
// transitions from a previously-open state back to open after a drop.
//
// This is the hook used by frontend display devices (ChartPro etc.) to
// annotate their timelines with a FAIL marker, telling the operator
// that there was an infrastructure interruption — distinct from a
// hardware reset, which is signalled in-band by the data stream.
//
// Listeners are NOT called on the first successful connection — that
// is normal startup, not a recovery. They ARE called on every
// subsequent reconnection.
//
// The callback runs on a fresh goroutine and must not block. There is
// no unregister API today; the lifetime of the listener matches the
// lifetime of the device that registered it. When the WASM page is
// reloaded the slice is reset along with everything else.
//
// Português: Registra callback disparado quando o WebSocket recupera
// a conexão depois de uma queda. Não dispara na primeira conexão.
// Usado pelos displays (ChartPro) para marcar FAIL na timeline.
func (c *Client) OnReconnect(fn func()) {
	if fn == nil {
		return
	}
	c.reconnectMu.Lock()
	c.reconnectListeners = append(c.reconnectListeners, fn)
	c.reconnectMu.Unlock()
}

// fireReconnect invokes all registered listeners in parallel.
// Internal. Called from dial.onopen when hadConnection was true.
func (c *Client) fireReconnect() {
	c.reconnectMu.RLock()
	listeners := make([]func(), len(c.reconnectListeners))
	copy(listeners, c.reconnectListeners)
	c.reconnectMu.RUnlock()

	for _, fn := range listeners {
		fn := fn
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[live] reconnect listener panic: %v", r)
				}
			}()
			fn()
		}()
	}
}

// Connect opens the WebSocket connection using credentials from the SPA.
// Reads project ID from localStorage first, then from window._ideProjectID.
// Reads JWT from window._ideAuthToken.
// If either is missing, logs a warning and returns without connecting.
//
// Must be called from a goroutine (blocks briefly for credential resolution).
func (c *Client) Connect() {
	// Read JWT from SPA. Format is "Bearer <token>" — strip prefix.
	authVal := js.Global().Get("_ideAuthToken")
	if !authVal.Truthy() {
		log.Println("[live] no _ideAuthToken set — live connection disabled")
		return
	}
	c.token = strings.TrimPrefix(authVal.String(), "Bearer ")
	if c.token == "" {
		log.Println("[live] empty token — live connection disabled")
		return
	}

	// Read project ID — priority: localStorage → window._ideProjectID.
	c.projectID = c.readProjectID()
	if c.projectID == "" {
		log.Println("[live] no project ID — live connection disabled (set via Live Config)")
		return
	}

	log.Printf("[live] connecting to project %s", c.projectID)
	c.dial()
}

// readProjectID reads the unique project ID from localStorage.
// Returns "" if not set. The project ID is a 32-char hex string generated
// once when the user first saves in the Settings dialog.
func (c *Client) readProjectID() string {
	storage := js.Global().Get("localStorage")
	if storage.Truthy() {
		saved := storage.Call("getItem", "liveProjectID")
		if saved.Truthy() {
			s := saved.String()
			if s != "" {
				return s
			}
		}
	}
	return ""
}

// readProjectName reads the human-readable project name from localStorage.
func (c *Client) readProjectName() string {
	storage := js.Global().Get("localStorage")
	if storage.Truthy() {
		saved := storage.Call("getItem", "liveProjectName")
		if saved.Truthy() {
			return saved.String()
		}
	}
	return ""
}

// generateProjectID creates a cryptographically random 128-bit hex string
// to use as a unique project identifier. Two users can never collide.
func generateProjectID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SaveProject stores the project name and generates a unique project ID
// if one doesn't exist yet. Does NOT touch the WebSocket connection.
// Returns the unique project ID.
func (c *Client) SaveProject(name string) string {
	storage := js.Global().Get("localStorage")

	// Save name.
	if storage.Truthy() && name != "" {
		storage.Call("setItem", "liveProjectName", name)
	}

	// Generate unique ID if not yet created.
	id := c.readProjectID()
	if id == "" {
		id = generateProjectID()
		if storage.Truthy() {
			storage.Call("setItem", "liveProjectID", id)
		}
		log.Printf("[live] generated project ID: %s", id)
	}

	return id
}

// SetProjectID saves the project ID to localStorage and reconnects.
// Called by the Connect button in the Settings dialog.
//
// Resets the hadConnection flag so the next successful open does not
// fire reconnect listeners — switching projects is not a "recovery",
// it is a fresh start.
//
// Português: Salva o project ID e reconecta. Reseta hadConnection
// para não disparar listeners de reconnect quando o usuário troca
// de projeto.
func (c *Client) SetProjectID(id string) {
	// Save to localStorage.
	storage := js.Global().Get("localStorage")
	if storage.Truthy() {
		if id != "" {
			storage.Call("setItem", "liveProjectID", id)
		} else {
			storage.Call("removeItem", "liveProjectID")
		}
	}

	// Close existing connection if any.
	if c.connected && c.ws.Truthy() {
		c.ws.Call("close")
		c.connected = false
	}

	// Switching projects is a fresh start, not a recovery.
	c.hadConnection = false
	c.projectID = id

	if id == "" {
		log.Println("[live] project ID cleared — disconnected")
		return
	}

	// Read JWT if not cached yet. This happens when Connect() was never
	// called (user connects manually via Settings instead of auto-connect).
	if c.token == "" {
		authVal := js.Global().Get("_ideAuthToken")
		if authVal.Truthy() {
			c.token = strings.TrimPrefix(authVal.String(), "Bearer ")
		}
	}

	if c.token == "" {
		log.Println("[live] no auth token — cannot connect")
		return
	}

	log.Printf("[live] connecting to project %s", id)
	c.dial()
}

// GetProjectID returns the current project ID.
func (c *Client) GetProjectID() string {
	return c.projectID
}

// Send sends a message from a device to external hardware via WebSocket.
// The message is published to the Redis "out" channel by the server Hub.
//
// Português: Envia uma mensagem de um device para hardware externo via WebSocket.
func (c *Client) Send(deviceID, port string, value interface{}) {
	if !c.connected {
		log.Printf("[live] Send: not connected, dropping message for %s.%s", deviceID, port)
		return
	}

	raw, err := json.Marshal(value)
	if err != nil {
		log.Printf("[live] Send: marshal error: %v", err)
		return
	}

	msg := Message{
		DeviceID: deviceID,
		Port:     port,
		Value:    raw,
		Ts:       time.Now().Unix(),
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[live] Send: marshal message error: %v", err)
		return
	}

	c.ws.Call("send", string(payload))
}

// IsConnected reports whether the WebSocket is currently open.
func (c *Client) IsConnected() bool {
	return c.connected
}

// Close shuts down the WebSocket connection and releases all JS callbacks.
func (c *Client) Close() {
	c.connected = false
	c.hadConnection = false
	if c.ws.Truthy() {
		c.ws.Call("close")
	}
	for _, f := range c.jsFuncs {
		f.Release()
	}
	c.jsFuncs = nil
}

// ─── Internal ─────────────────────────────────────────────────────────────────

// dial creates a new JavaScript WebSocket and wires the event handlers.
//
// When onopen fires we check hadConnection. If true, this is a reconnect
// (we were previously connected, lost it, and got it back) — fire all
// OnReconnect listeners. If false, this is the first successful open of
// this session, which is not a "recovery" event.
func (c *Client) dial() {
	// Build WebSocket URL from current page location.
	// Replace http(s):// with ws(s)://.
	origin := js.Global().Get("location").Get("origin").String()
	wsURL := strings.Replace(origin, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = fmt.Sprintf("%s/ws/live/%s?token=%s", wsURL, c.projectID, c.token)

	c.ws = js.Global().Get("WebSocket").New(wsURL)

	// onopen — mark connected; if we'd seen a previous connection,
	// fire reconnect listeners so displays can mark the gap.
	onOpen := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		isReconnect := c.hadConnection
		c.connected = true
		c.hadConnection = true
		if isReconnect {
			log.Printf("[live] reconnected to project %s", c.projectID)
			c.fireReconnect()
		} else {
			log.Printf("[live] connected to project %s", c.projectID)
		}
		return nil
	})
	c.ws.Set("onopen", onOpen)
	c.jsFuncs = append(c.jsFuncs, onOpen)

	// onclose — auto-reconnect after 3 seconds. The hadConnection
	// flag is intentionally NOT reset here; we need to remember that
	// we used to be open so the next onopen can be classified as a
	// reconnect rather than a fresh connect.
	onClose := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		wasConnected := c.connected
		c.connected = false
		if wasConnected {
			log.Printf("[live] disconnected from project %s — reconnecting in 3s", c.projectID)
		}
		go func() {
			time.Sleep(3 * time.Second)
			// Only reconnect if token and project are still set.
			if c.token != "" && c.projectID != "" {
				c.dial()
			}
		}()
		return nil
	})
	c.ws.Set("onclose", onClose)
	c.jsFuncs = append(c.jsFuncs, onClose)

	// onerror
	onError := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Println("[live] websocket error")
		return nil
	})
	c.ws.Set("onerror", onError)
	c.jsFuncs = append(c.jsFuncs, onError)

	// onmessage — dispatch to devices.
	onMessage := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		data := args[0].Get("data").String()
		go c.handleMessage(data)
		return nil
	})
	c.ws.Set("onmessage", onMessage)
	c.jsFuncs = append(c.jsFuncs, onMessage)
}

// handleMessage parses a JSON message and dispatches it to the target device.
// Runs in a goroutine to avoid blocking the JS event loop.
func (c *Client) handleMessage(raw string) {
	var msg Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		log.Printf("[live] invalid message: %v", err)
		return
	}

	if msg.DeviceID == "" {
		return
	}

	if c.sceneMgr == nil {
		log.Printf("[live] sceneMgr is nil — dropping message for %s.%s", msg.DeviceID, msg.Port)
		return
	}

	// Look up the device in the SceneMgr.
	dev := c.sceneMgr.FindDevice(msg.DeviceID)
	if dev == nil {
		log.Printf("[live] device %q not found in scene", msg.DeviceID)
		return
	}

	// Check if the device supports live updates.
	liveDev, ok := dev.(scene.LiveUpdatable)
	if !ok {
		log.Printf("[live] device %q does not implement LiveUpdatable", msg.DeviceID)
		return
	}

	// Dispatch the update.
	if err := liveDev.LiveUpdate(msg.Port, msg.Value); err != nil {
		log.Printf("[live] device %q update error: %v", msg.DeviceID, err)
	}
}
