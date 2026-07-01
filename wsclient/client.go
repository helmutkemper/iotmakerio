//go:build js && wasm

package wsclient

// client.go — WebSocket client for WASM (browser-side).
//
// English:
//
//	Connects to the channel WebSocket server and provides a simple pub/sub
//	API for frontend devices (gauges, buttons, sliders) to bind to backend
//	channels.
//
//	Features:
//	  - Auto-reconnect with exponential backoff (1s → 2s → 4s → ... → 30s)
//	  - Automatic re-subscribe on reconnect (subscriptions survive reconnects)
//	  - Non-blocking Send() for UI → backend communication
//	  - Snapshot support: gauges get the current value immediately on subscribe
//	  - Thread-safe (all operations protected by mutex)
//
//	Usage (WASM frontend):
//
//	  client := wsclient.New("ws://localhost:8080/ws")
//	  client.Connect()
//
//	  // Gauge subscribes to a backend channel:
//	  client.Subscribe("total", func(value interface{}, ts int64) {
//	      gauge.SetValue(value.(float64))
//	  })
//
//	  // Button sends an event to the backend:
//	  client.Send("btn_start", true)
//
//	  // Cleanup:
//	  client.Unsubscribe("total")
//	  client.Close()
//
// Português:
//
//	Conecta ao servidor WebSocket de canais e fornece uma API pub/sub simples
//	para devices frontend (gauges, botões, sliders) se ligarem a canais do backend.
//
//	Características:
//	  - Reconexão automática com backoff exponencial (1s → 2s → 4s → ... → 30s)
//	  - Re-inscrição automática ao reconectar
//	  - Send() não-bloqueante para comunicação UI → backend
//	  - Suporte a snapshot: gauges recebem valor atual imediatamente
//	  - Thread-safe

import (
	"encoding/json"
	"log"
	"sync"
	"syscall/js"
	"time"
)

// HandlerFunc is the callback signature for channel value updates.
// Called with the value and timestamp (Unix milliseconds).
//
// Português: Assinatura do callback para atualizações de valor de canal.
type HandlerFunc func(value interface{}, timestamp int64)

// State represents the client connection state.
//
// Português: Representa o estado da conexão do cliente.
type State int

const (
	// StateDisconnected means not connected and not trying to connect.
	StateDisconnected State = iota

	// StateConnecting means a connection attempt is in progress.
	StateConnecting

	// StateConnected means the WebSocket is open and operational.
	StateConnected

	// StateClosed means Close() was called — no more reconnection attempts.
	StateClosed
)

// Client is a WebSocket client for WASM that connects to the channel server.
//
// Português: Cliente WebSocket para WASM que conecta ao servidor de canais.
type Client struct {
	url   string
	state State
	ws    js.Value // JavaScript WebSocket object

	// Subscriptions: channel → handler. Survives reconnects.
	// Português: Inscrições: canal → handler. Sobrevive a reconexões.
	subs map[string]HandlerFunc

	// Reconnection state.
	// Português: Estado de reconexão.
	reconnectDelay time.Duration
	maxDelay       time.Duration

	// JS callback references (must be stored to prevent GC).
	// Português: Referências de callbacks JS (devem ser armazenadas para evitar GC).
	onOpen       js.Func
	onClose      js.Func
	onMessage    js.Func
	onError      js.Func
	callbacksSet bool

	// State change callback.
	// Português: Callback de mudança de estado.
	OnStateChange func(state State)

	mu sync.Mutex
}

// New creates a new Client targeting the given WebSocket URL.
// Call Connect() to establish the connection.
//
// Português: Cria um novo Client apontando para a URL WebSocket fornecida.
// Chame Connect() para estabelecer a conexão.
func New(url string) *Client {
	return &Client{
		url:            url,
		state:          StateDisconnected,
		subs:           make(map[string]HandlerFunc),
		reconnectDelay: time.Second,
		maxDelay:       30 * time.Second,
	}
}

// Connect establishes the WebSocket connection. If the connection drops,
// the client automatically reconnects with exponential backoff.
//
// Português: Estabelece a conexão WebSocket. Se a conexão cair, o cliente
// reconecta automaticamente com backoff exponencial.
func (c *Client) Connect() {
	c.mu.Lock()
	if c.state == StateClosed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	c.connect()
}

// connect creates the JavaScript WebSocket and wires up event handlers.
//
// Português: Cria o WebSocket JavaScript e conecta os handlers de eventos.
func (c *Client) connect() {
	c.mu.Lock()
	c.setState(StateConnecting)
	c.mu.Unlock()

	// Release previous JS callbacks if any.
	// Português: Libera callbacks JS anteriores se houver.
	c.releaseCallbacks()

	// Create JavaScript WebSocket.
	// Português: Cria WebSocket JavaScript.
	ws := js.Global().Get("WebSocket").New(c.url)

	c.onOpen = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		c.mu.Lock()
		c.ws = ws
		c.setState(StateConnected)
		c.reconnectDelay = time.Second // reset backoff
		c.mu.Unlock()

		log.Printf("[CHANNEL:WS] Connected to %s", c.url)

		// Re-subscribe to all channels.
		// Português: Re-inscreve em todos os canais.
		c.resubscribeAll()
		return nil
	})

	c.onClose = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		code := args[0].Get("code").Int()
		log.Printf("[CHANNEL:WS] Disconnected (code: %d)", code)

		c.mu.Lock()
		c.ws = js.Undefined()

		if c.state == StateClosed {
			c.mu.Unlock()
			return nil
		}

		c.setState(StateDisconnected)
		delay := c.reconnectDelay
		c.reconnectDelay *= 2
		if c.reconnectDelay > c.maxDelay {
			c.reconnectDelay = c.maxDelay
		}
		c.mu.Unlock()

		// Schedule reconnect.
		// Português: Agenda reconexão.
		log.Printf("[CHANNEL:WS] Reconnecting in %v...", delay)
		go func() {
			time.Sleep(delay)
			c.mu.Lock()
			closed := c.state == StateClosed
			c.mu.Unlock()
			if !closed {
				c.connect()
			}
		}()
		return nil
	})

	c.onMessage = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		data := args[0].Get("data").String()
		c.handleMessage([]byte(data))
		return nil
	})

	c.onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Printf("[CHANNEL:WS] Error")
		return nil
	})

	ws.Set("onopen", c.onOpen)
	ws.Set("onclose", c.onClose)
	ws.Set("onmessage", c.onMessage)
	ws.Set("onerror", c.onError)

	c.mu.Lock()
	c.callbacksSet = true
	c.mu.Unlock()
}

// Subscribe registers a handler for a channel. If already connected,
// sends a subscribe message immediately. The handler will be re-registered
// automatically on reconnect.
//
// If the server has a retained value for the channel, the handler will be
// called immediately with the snapshot.
//
// Português: Registra um handler para um canal. Se já conectado, envia
// mensagem de inscrição imediatamente. O handler será re-registrado
// automaticamente em reconexões.
func (c *Client) Subscribe(channel string, handler HandlerFunc) {
	c.mu.Lock()
	c.subs[channel] = handler
	connected := c.state == StateConnected
	c.mu.Unlock()

	if connected {
		c.sendJSON(subMsg{Type: "sub", Channel: channel})
	}
}

// Unsubscribe removes a channel subscription and notifies the server.
//
// Português: Remove uma inscrição de canal e notifica o servidor.
func (c *Client) Unsubscribe(channel string) {
	c.mu.Lock()
	delete(c.subs, channel)
	connected := c.state == StateConnected
	c.mu.Unlock()

	if connected {
		c.sendJSON(subMsg{Type: "unsub", Channel: channel})
	}
}

// Send publishes a value from the frontend to the backend via the server.
// Used for UI interactions (button clicks, slider changes, text input).
//
// Português: Publica um valor do frontend para o backend via servidor.
// Usado para interações de UI (cliques de botão, mudanças de slider).
func (c *Client) Send(channel string, value interface{}) {
	c.sendJSON(pubMsg{Type: "pub", Channel: channel, Value: value})
}

// Close permanently disconnects the client. No more reconnection attempts.
//
// Português: Desconecta permanentemente o cliente. Sem mais tentativas de reconexão.
func (c *Client) Close() {
	c.mu.Lock()
	c.setState(StateClosed)
	ws := c.ws
	c.mu.Unlock()

	if !ws.IsUndefined() && !ws.IsNull() {
		ws.Call("close", 1000, "client closed")
	}

	c.releaseCallbacks()
	log.Printf("[CHANNEL:WS] Client closed")
}

// IsConnected returns true if the WebSocket is currently open.
//
// Português: Retorna true se o WebSocket está atualmente aberto.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == StateConnected
}

// GetState returns the current connection state.
//
// Português: Retorna o estado atual da conexão.
func (c *Client) GetState() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// --- Internal methods ---

// handleMessage processes an incoming WebSocket message.
//
// Português: Processa uma mensagem WebSocket recebida.
func (c *Client) handleMessage(data []byte) {
	var msg struct {
		Type    string      `json:"type"`
		Channel string      `json:"ch"`
		Value   interface{} `json:"v"`
		Time    int64       `json:"t"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[CHANNEL:WS] Invalid message: %v", err)
		return
	}

	switch msg.Type {
	case "pub", "snapshot":
		c.mu.Lock()
		handler, exists := c.subs[msg.Channel]
		c.mu.Unlock()

		if exists && handler != nil {
			handler(msg.Value, msg.Time)
		}

	case "pong":
		// Keepalive response, nothing to do.

	case "error":
		log.Printf("[CHANNEL:WS] Server error: %v", msg.Value)
	}
}

// resubscribeAll sends subscribe messages for all active subscriptions.
// Called after a successful reconnect.
//
// Português: Envia mensagens de inscrição para todas as inscrições ativas.
// Chamada após uma reconexão bem-sucedida.
func (c *Client) resubscribeAll() {
	c.mu.Lock()
	channels := make([]string, 0, len(c.subs))
	for ch := range c.subs {
		channels = append(channels, ch)
	}
	c.mu.Unlock()

	for _, ch := range channels {
		c.sendJSON(subMsg{Type: "sub", Channel: ch})
	}

	if len(channels) > 0 {
		log.Printf("[CHANNEL:WS] Re-subscribed to %d channels", len(channels))
	}
}

// sendJSON marshals and sends a message over the WebSocket.
// Non-blocking: silently fails if not connected.
//
// Português: Serializa e envia uma mensagem pelo WebSocket.
// Não-bloqueante: falha silenciosamente se não conectado.
func (c *Client) sendJSON(v interface{}) {
	c.mu.Lock()
	ws := c.ws
	connected := c.state == StateConnected
	c.mu.Unlock()

	if !connected || ws.IsUndefined() || ws.IsNull() {
		return
	}

	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[CHANNEL:WS] Marshal error: %v", err)
		return
	}

	ws.Call("send", string(data))
}

// setState updates the connection state and fires the callback if set.
// Must be called with c.mu held.
//
// Português: Atualiza o estado da conexão e dispara o callback se definido.
// Deve ser chamado com c.mu mantido.
func (c *Client) setState(s State) {
	c.state = s
	if c.OnStateChange != nil {
		// Fire asynchronously to avoid holding the lock during callback.
		fn := c.OnStateChange
		go fn(s)
	}
}

// releaseCallbacks frees JS function references to prevent memory leaks.
//
// Português: Libera referências de funções JS para evitar vazamento de memória.
func (c *Client) releaseCallbacks() {
	c.mu.Lock()
	wasSet := c.callbacksSet
	c.callbacksSet = false
	c.mu.Unlock()

	if !wasSet {
		return
	}
	c.onOpen.Release()
	c.onClose.Release()
	c.onMessage.Release()
	c.onError.Release()
}

// --- Wire format helpers ---

type subMsg struct {
	Type    string `json:"type"`
	Channel string `json:"ch"`
}

type pubMsg struct {
	Type    string      `json:"type"`
	Channel string      `json:"ch"`
	Value   interface{} `json:"v"`
}
