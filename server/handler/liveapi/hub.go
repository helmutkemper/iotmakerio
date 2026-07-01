// server/handler/liveapi/hub.go — WebSocket connection hub with Redis PubSub bridge.
//
// The Hub manages all active WebSocket connections from browsers. Each connection
// is scoped to a (userID, projectID) pair. Messages flow in two directions:
//
//	Inbound  (hardware → browser):
//	  Webhook handler publishes to Redis channel "live:in:{userID}:{projectID}".
//	  The Hub subscribes to this channel and broadcasts the message to all
//	  WebSocket connections for that (user, project) pair.
//
//	Outbound (browser → hardware):
//	  Browser sends a message via WebSocket. The Hub publishes it to Redis
//	  channel "live:out:{userID}:{projectID}". An external subscriber (future
//	  webhook forwarder, MQTT bridge, etc.) can consume these messages.
//
// Uses golang.org/x/net/websocket — already a transitive dependency of Echo v4 —
// so no new dependency is added to the project.
//
// Thread safety:
//
//	All Hub mutations happen on a single goroutine (the Run loop).
//	Reads from WebSocket happen on per-connection goroutines.
//	Writes to WebSocket are serialized via a buffered send channel.
//
// Português:
//
//	Hub gerencia conexões WebSocket do navegador. Redis PubSub faz a ponte
//	entre webhook (hardware→browser) e browser→hardware. Usa x/net/websocket
//	(dependência transitiva do Echo) — nenhuma dependência nova.
package liveapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"golang.org/x/net/websocket"

	"github.com/redis/go-redis/v9"
)

// ─── Message types ────────────────────────────────────────────────────────────

// LiveMessage is the standard envelope for all bidirectional communication
// between the browser and external hardware/scripts.
type LiveMessage struct {
	DeviceID string          `json:"device_id"`
	Port     string          `json:"port"`
	Value    json.RawMessage `json:"value"`
	Ts       int64           `json:"ts"`
}

// ─── Connection wrapper ───────────────────────────────────────────────────────

// connKey identifies a unique (user, project) subscription scope.
type connKey struct {
	UserID    string
	ProjectID string
}

// sendBufSize is the per-connection outbound buffer size.
const sendBufSize = 64

// Conn wraps a single WebSocket connection with its identity and send buffer.
type Conn struct {
	Key  connKey
	WS   *websocket.Conn
	Send chan []byte
}

// ─── Hub ──────────────────────────────────────────────────────────────────────

// Hub is the central coordinator for all live WebSocket connections.
// Create with NewHub(), start with go hub.Run(ctx).
type Hub struct {
	rdb *redis.Client

	// connections grouped by (user, project) scope.
	// Only accessed from the Run goroutine.
	conns map[connKey]map[*Conn]bool

	// Active Redis subscriptions per scope.
	subs map[connKey]context.CancelFunc

	register   chan *Conn
	unregister chan *Conn
	inbound    chan redisMsg
}

// redisMsg carries a message received from a Redis "in" subscription.
type redisMsg struct {
	Key     connKey
	Payload []byte
}

// NewHub creates a Hub. Call go hub.Run(ctx) to start the event loop.
func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		rdb:        rdb,
		conns:      make(map[connKey]map[*Conn]bool),
		subs:       make(map[connKey]context.CancelFunc),
		register:   make(chan *Conn, 64),
		unregister: make(chan *Conn, 64),
		inbound:    make(chan redisMsg, 256),
	}
}

// Run is the main event loop. Blocks until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	log.Println("[liveapi] hub started")
	defer h.shutdown()

	for {
		select {
		case <-ctx.Done():
			log.Println("[liveapi] hub stopping (context cancelled)")
			return
		case c := <-h.register:
			h.addConn(ctx, c)
		case c := <-h.unregister:
			h.removeConn(c)
		case msg := <-h.inbound:
			h.broadcast(msg)
		}
	}
}

// Register queues a new connection to be added by the Run loop.
func (h *Hub) Register(c *Conn) { h.register <- c }

// Unregister queues a connection for removal by the Run loop.
func (h *Hub) Unregister(c *Conn) { h.unregister <- c }

// PublishInbound sends a message from a webhook handler into the Hub
// for broadcasting to browsers. Safe from any goroutine.
func (h *Hub) PublishInbound(ctx context.Context, userID, projectID string, payload []byte) error {
	return h.rdb.Publish(ctx, redisChanIn(userID, projectID), payload).Err()
}

// PublishOutbound sends a browser message to the Redis "out" channel
// for external consumers (webhook forwarder, MQTT bridge, etc.).
func (h *Hub) PublishOutbound(ctx context.Context, userID, projectID string, payload []byte) error {
	return h.rdb.Publish(ctx, redisChanOut(userID, projectID), payload).Err()
}

// ─── Internal operations (Run goroutine only) ─────────────────────────────────

func (h *Hub) addConn(ctx context.Context, c *Conn) {
	set, exists := h.conns[c.Key]
	if !exists {
		set = make(map[*Conn]bool)
		h.conns[c.Key] = set
	}
	set[c] = true

	if !exists {
		subCtx, cancel := context.WithCancel(ctx)
		h.subs[c.Key] = cancel
		go h.subscribeRedis(subCtx, c.Key)
	}

	log.Printf("[liveapi] conn registered: user=%s project=%s (total=%d)",
		c.Key.UserID, c.Key.ProjectID, len(set))
}

func (h *Hub) removeConn(c *Conn) {
	set, exists := h.conns[c.Key]
	if !exists {
		return
	}
	if _, ok := set[c]; ok {
		delete(set, c)
		close(c.Send)
	}
	if len(set) == 0 {
		delete(h.conns, c.Key)
		if cancel, ok := h.subs[c.Key]; ok {
			cancel()
			delete(h.subs, c.Key)
		}
		log.Printf("[liveapi] scope removed: user=%s project=%s",
			c.Key.UserID, c.Key.ProjectID)
	}
}

func (h *Hub) broadcast(msg redisMsg) {
	set := h.conns[msg.Key]
	for c := range set {
		select {
		case c.Send <- msg.Payload:
		default:
			log.Printf("[liveapi] dropping slow conn: user=%s project=%s",
				c.Key.UserID, c.Key.ProjectID)
			h.removeConn(c)
			c.WS.Close()
		}
	}
}

func (h *Hub) subscribeRedis(ctx context.Context, key connKey) {
	channel := redisChanIn(key.UserID, key.ProjectID)
	sub := h.rdb.Subscribe(ctx, channel)
	defer sub.Close()

	ch := sub.Channel()
	log.Printf("[liveapi] redis subscribed: %s", channel)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[liveapi] redis unsubscribed: %s", channel)
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			h.inbound <- redisMsg{Key: key, Payload: []byte(msg.Payload)}
		}
	}
}

func (h *Hub) shutdown() {
	for key, set := range h.conns {
		for c := range set {
			close(c.Send)
			c.WS.Close()
		}
		if cancel, ok := h.subs[key]; ok {
			cancel()
		}
	}
	h.conns = nil
	h.subs = nil
	log.Println("[liveapi] hub shut down")
}

// ─── Redis channel naming ─────────────────────────────────────────────────────

func redisChanIn(userID, projectID string) string {
	return fmt.Sprintf("live:in:%s:%s", userID, projectID)
}

func redisChanOut(userID, projectID string) string {
	return fmt.Sprintf("live:out:%s:%s", userID, projectID)
}

// ─── Per-connection pumps ─────────────────────────────────────────────────────

// WritePump sends messages from the Send channel to the WebSocket.
// Exits when Send is closed or a write fails.
func (c *Conn) WritePump() {
	defer c.WS.Close()
	for msg := range c.Send {
		if _, err := c.WS.Write(msg); err != nil {
			log.Printf("[liveapi] write error: %v", err)
			return
		}
	}
}

// ReadPump reads messages from the WebSocket and publishes them to Redis "out".
// Exits on read error or connection close.
func (c *Conn) ReadPump(hub *Hub) {
	defer func() {
		hub.Unregister(c)
		c.WS.Close()
	}()

	buf := make([]byte, 4096)
	for {
		n, err := c.WS.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("[liveapi] read error: %v", err)
			}
			return
		}
		message := make([]byte, n)
		copy(message, buf[:n])

		// Only publish valid JSON.
		var check json.RawMessage
		if json.Unmarshal(message, &check) != nil {
			log.Printf("[liveapi] invalid JSON from browser, dropping")
			continue
		}

		if err := hub.PublishOutbound(context.Background(), c.Key.UserID, c.Key.ProjectID, message); err != nil {
			log.Printf("[liveapi] redis publish out: %v", err)
		}
	}
}
