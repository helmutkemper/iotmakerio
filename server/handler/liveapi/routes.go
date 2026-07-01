// server/handler/liveapi/routes.go — Route registration for live device communication.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Three route groups:
//
//  1. WebSocket upgrade (JWT via query param ?token=):
//     GET /ws/live/:project_id?token=<jwt>
//
//  2. Webhook receiver (API key via X-API-Key header):
//     POST /api/v1/webhook/:project_id/:device_id
//
//  3. API key management (JWT via Authorization header):
//     POST   /api/v1/live/keys           — create new device-scoped key
//     GET    /api/v1/live/keys            — list keys for a project
//     DELETE /api/v1/live/keys/:key_id    — revoke a key
//
// Usage in main.go:
//
//	hub := liveapi.NewHub(rdb)
//	go hub.Run(ctx)
//	liveapi.Register(e, hub)
//
// Português:
//
//	Três grupos de rotas: WebSocket, webhook e gerenciamento de API keys.
//	Nenhuma dependência nova — usa x/net/websocket (transitiva do Echo).
package liveapi

import (
	"github.com/labstack/echo/v4"

	spaauth "server/handler/spaauth"
)

// Register wires all live communication routes onto the Echo instance.
func Register(e *echo.Echo, hub *Hub) {
	h := &Handlers{hub: hub}

	// ── WebSocket (browser ↔ server, bidirectional) ───────────────────────
	// JWT is passed as ?token= query param because browsers cannot send
	// custom headers during WebSocket upgrade. Validated inside the handler.
	e.GET("/ws/live/:project_id", h.handleWebSocket)

	// ── Webhook (hardware → server → browser) ────────────────────────────
	// Authenticated by project-scoped API key (X-API-Key header).
	// Accepts an array of {device_id, port, value} in one request.
	e.POST("/api/v1/webhook/:project_id", h.handleWebhookBatch)

	// ── API key management (portal users) ─────────────────────────────────
	keys := e.Group("/api/v1/live/keys", spaauth.RequireBearerToken())
	keys.POST("", h.handleCreateAPIKey)
	keys.GET("", h.handleListAPIKeys)
	keys.DELETE("/:key_id", h.handleRevokeAPIKey)

	// ── Live project management (portal users) ────────────────────────────
	projects := e.Group("/api/v1/live/projects", spaauth.RequireBearerToken())
	projects.POST("", h.handleCreateProject)
	projects.GET("", h.handleListProjects)
	projects.DELETE("/:project_id", h.handleDeleteProject)
}
