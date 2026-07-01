// server/handler/liveapi/handlers.go — HTTP handlers for live device communication.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Endpoints:
//
//	GET /ws/live/:project_id?token=<jwt>   (WebSocket upgrade)
//	  Authenticated by JWT passed as query parameter (browsers cannot send
//	  custom headers during WebSocket upgrade). Uses x/net/websocket.Handler
//	  to perform the upgrade within the Echo handler.
//
//	POST /api/v1/webhook/:project_id/:device_id  (Webhook receiver)
//	  Authenticated by device-scoped API key (X-API-Key header).
//
//	POST   /api/v1/live/keys         — create device-scoped API key
//	GET    /api/v1/live/keys         — list keys for a project
//	DELETE /api/v1/live/keys/:key_id — revoke a key
//
// Português:
//
//	Handlers HTTP para comunicação live. WebSocket usa JWT via query param.
//	Webhook usa API key por device. Gerenciamento de chaves via JWT do portal.
package liveapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"golang.org/x/net/websocket"

	"github.com/labstack/echo/v4"

	"server/auth"
	"server/config"
	spaauth "server/handler/spaauth"
	"server/store"
)

// Handlers holds dependencies injected at route registration time.
type Handlers struct {
	hub *Hub
}

// ─── Response helpers (project convention) ────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": 200},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}

// ─── WebSocket upgrade ────────────────────────────────────────────────────────

// handleWebSocket upgrades an HTTP connection to a WebSocket for live
// bidirectional communication between the browser and the IoTMaker server.
//
// Authentication: JWT passed as query parameter ?token=<jwt>.
// Browsers cannot send custom headers (Authorization) during a WebSocket
// upgrade, so the token is extracted from the URL instead.
//
// Path: GET /ws/live/:project_id?token=<jwt>
func (h *Handlers) handleWebSocket(c echo.Context) error {
	projectID := c.Param("project_id")
	if projectID == "" {
		return fail(c, http.StatusBadRequest, "missing project_id")
	}

	// Validate JWT from query parameter.
	tokenStr := c.QueryParam("token")
	if tokenStr == "" {
		return fail(c, http.StatusUnauthorized, "missing token query parameter")
	}

	cfg := config.Get()
	claims, err := auth.ParseJWT(tokenStr, cfg.JWTSecret)
	if err != nil {
		return fail(c, http.StatusUnauthorized, "invalid or expired token")
	}
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid token claims")
	}

	// Use x/net/websocket.Handler to perform the upgrade.
	// The handler function runs inside the upgraded connection.
	// We capture hub, claims, and projectID in the closure.
	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		conn := &Conn{
			Key: connKey{
				UserID:    claims.UserID,
				ProjectID: projectID,
			},
			WS:   ws,
			Send: make(chan []byte, sendBufSize),
		}

		h.hub.Register(conn)

		// WritePump runs on this goroutine (blocks until Send is closed).
		// ReadPump runs on a separate goroutine.
		go conn.ReadPump(h.hub)
		conn.WritePump()
	})

	// ServeHTTP performs the WebSocket upgrade and blocks until the
	// connection is closed. Echo does not need to write a response after this.
	wsHandler.ServeHTTP(c.Response(), c.Request())
	return nil
}

// ─── Webhook receiver ─────────────────────────────────────────────────────────

// ─── Webhook authentication helper ────────────────────────────────────────────

// validateWebhookKey extracts and validates the API key from the X-API-Key
// header. Returns the validated key or writes an error response and returns nil.
func (h *Handlers) validateWebhookKey(c echo.Context, projectID string) *store.APIKey {
	rawKey := c.Request().Header.Get("X-API-Key")
	if rawKey == "" {
		fail(c, http.StatusUnauthorized, "missing X-API-Key header")
		return nil
	}

	apiKey, err := store.ValidateAPIKey(rawKey, projectID)
	if err != nil {
		switch err {
		case store.ErrAPIKeyNotFound:
			fail(c, http.StatusUnauthorized, "invalid api key")
		case store.ErrAPIKeyRevoked:
			fail(c, http.StatusForbidden, "api key has been revoked")
		case store.ErrAPIKeyExpired:
			fail(c, http.StatusForbidden, "api key has expired")
		default:
			log.Printf("[liveapi] api key validation error: %v", err)
			fail(c, http.StatusInternalServerError, "internal error")
		}
		return nil
	}
	return apiKey
}

// ─── Webhook (batch) ──────────────────────────────────────────────────────────

// webhookItem is a single device update inside a webhook request.
type webhookItem struct {
	DeviceID string          `json:"device_id"`
	Port     string          `json:"port"`
	Value    json.RawMessage `json:"value"`
}

// handleWebhookBatch receives data for multiple devices in a single request.
// One HTTP call updates the entire dashboard.
//
// Path: POST /api/v1/webhook/:project_id
// Body: [
//
//	{"device_id":"gauge_1","port":"current","value":73},
//	{"device_id":"gauge_2","port":"current","value":42}
//
// ]
func (h *Handlers) handleWebhookBatch(c echo.Context) error {
	projectID := c.Param("project_id")
	if projectID == "" {
		return fail(c, http.StatusBadRequest, "missing project_id")
	}

	apiKey := h.validateWebhookKey(c, projectID)
	if apiKey == nil {
		return nil // error already written
	}

	// Parse the JSON array from the request body.
	var items []webhookItem
	if err := c.Bind(&items); err != nil {
		return fail(c, http.StatusBadRequest, "invalid JSON body — expected array of {device_id, port, value}")
	}

	if len(items) == 0 {
		return fail(c, http.StatusBadRequest, "empty batch — at least one item required")
	}

	ctx := context.Background()
	now := time.Now().Unix()
	published := 0

	for _, item := range items {
		if item.DeviceID == "" || item.Port == "" {
			continue // skip malformed items silently
		}

		msg := LiveMessage{
			DeviceID: item.DeviceID,
			Port:     item.Port,
			Value:    item.Value,
			Ts:       now,
		}
		payload, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		if err := h.hub.PublishInbound(ctx, apiKey.UserID, projectID, payload); err != nil {
			log.Printf("[liveapi] redis publish error (batch): %v", err)
			continue
		}
		published++
	}

	return ok(c, map[string]any{
		"published": published,
		"total":     len(items),
	})
}

// ─── API Key management ───────────────────────────────────────────────────────

// handleCreateAPIKey generates a new project-scoped API key.
// The raw key is returned exactly once — it is never stored in the database.
//
// Path: POST /api/v1/live/keys
// Body: { "project_id": "...", "label": "my sensor" }
func (h *Handlers) handleCreateAPIKey(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid or missing token")
	}

	var body struct {
		ProjectID string  `json:"project_id"`
		Label     string  `json:"label"`
		ExpiresAt *string `json:"expires_at,omitempty"` // optional RFC3339
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid JSON body")
	}
	if body.ProjectID == "" {
		return fail(c, http.StatusBadRequest, "project_id is required")
	}

	// Generate the raw key and its SHA-256 hash.
	rawKey, keyHash, err := store.GenerateAPIKey()
	if err != nil {
		log.Printf("[liveapi] key generation error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to generate key")
	}

	id := auth.MustNewID()
	now := time.Now().UTC().Format(time.RFC3339)

	apiKey := &store.APIKey{
		ID:        id,
		UserID:    claims.UserID,
		ProjectID: body.ProjectID,
		DeviceID:  "", // project-scoped — not tied to a specific device
		KeyHash:   keyHash,
		Label:     body.Label,
		ExpiresAt: body.ExpiresAt,
		CreatedAt: now,
	}

	if err := store.CreateAPIKey(apiKey); err != nil {
		log.Printf("[liveapi] create api key error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to create api key")
	}

	return ok(c, map[string]any{
		"id":         id,
		"api_key":    rawKey,
		"project_id": body.ProjectID,
		"label":      body.Label,
		"warning":    "Save this key now. It will not be shown again.",
	})
}

// handleListAPIKeys lists all API keys for a project (metadata only, no raw keys).
// Path: GET /api/v1/live/keys?project_id=...
func (h *Handlers) handleListAPIKeys(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid or missing token")
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return fail(c, http.StatusBadRequest, "missing project_id query param")
	}

	keys, err := store.ListAPIKeysByProject(claims.UserID, projectID)
	if err != nil {
		log.Printf("[liveapi] list api keys error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to list keys")
	}

	return ok(c, map[string]any{"keys": keys})
}

// handleRevokeAPIKey soft-revokes an API key.
// Path: DELETE /api/v1/live/keys/:key_id
func (h *Handlers) handleRevokeAPIKey(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid or missing token")
	}

	keyID := c.Param("key_id")
	if keyID == "" {
		return fail(c, http.StatusBadRequest, "missing key_id")
	}

	if err := store.RevokeAPIKey(keyID, claims.UserID); err != nil {
		if err == store.ErrAPIKeyNotFound {
			return fail(c, http.StatusNotFound, "key not found or already revoked")
		}
		log.Printf("[liveapi] revoke api key error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to revoke key")
	}

	return ok(c, map[string]any{"revoked": true, "key_id": keyID})
}

// ─── Live Project management ──────────────────────────────────────────────────

// handleCreateProject creates a new live project with a unique server-generated ID.
//
// Path: POST /api/v1/live/projects
// Body: { "name": "My Temperature Sensor" }
func (h *Handlers) handleCreateProject(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid or missing token")
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid JSON body")
	}
	if body.Name == "" {
		return fail(c, http.StatusBadRequest, "name is required")
	}

	// Check if a project with this name already exists for this user.
	proj, isNew, err := store.EnsureLiveProject(claims.UserID, body.Name)
	if err != nil {
		log.Printf("[liveapi] ensure project error: %v", err)
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	if !isNew {
		// Return existing project.
		return ok(c, proj)
	}

	// Generate unique ID and create.
	proj.ID = auth.MustNewID()
	if err := store.CreateLiveProject(proj); err != nil {
		log.Printf("[liveapi] create project error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to create project")
	}

	return ok(c, proj)
}

// handleListProjects returns all live projects for the authenticated user.
//
// Path: GET /api/v1/live/projects
func (h *Handlers) handleListProjects(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid or missing token")
	}

	projects, err := store.ListLiveProjects(claims.UserID)
	if err != nil {
		log.Printf("[liveapi] list projects error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to list projects")
	}

	return ok(c, map[string]any{"projects": projects})
}

// handleDeleteProject removes a live project.
//
// Path: DELETE /api/v1/live/projects/:project_id
func (h *Handlers) handleDeleteProject(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	if claims.UserID == "" {
		return fail(c, http.StatusUnauthorized, "invalid or missing token")
	}

	projectID := c.Param("project_id")
	if projectID == "" {
		return fail(c, http.StatusBadRequest, "missing project_id")
	}

	if err := store.DeleteLiveProject(claims.UserID, projectID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, http.StatusNotFound, "project not found")
		}
		log.Printf("[liveapi] delete project error: %v", err)
		return fail(c, http.StatusInternalServerError, "failed to delete project")
	}

	return ok(c, map[string]any{"deleted": true})
}
