// server/handler/blackboxapi/routes.go — Registers device endpoints.
//
// Routes:
//
//	GET  /api/v1/blackbox                  — list ready devices for the WASM IDE (public)
//	POST /api/v1/blackbox/submit           — submit a GitHub release URL as a device (auth)
//	GET  /api/v1/blackbox/jobs/:jobId      — poll submit job result from Redis (auth)
//	GET  /api/v1/blackbox/mine             — list own devices (auth)
//	GET  /api/v1/blackbox/:id              — get one device (auth)
//	PUT  /api/v1/blackbox/:id/meta         — update tags, visibility, category (auth)
//	DELETE /api/v1/blackbox/:id            — delete device (auth)
//
// Wizard routes (Slices 0–3 of the device wizard — see
// docs/CLAUDE_WIZARD_DESIGN.md and docs/tasks/WIZARD_TASKS.md):
//
//	POST   /api/v1/blackbox/wizard/parse                  — sync AST parse via codegen parser (auth)
//	POST   /api/v1/blackbox/wizard/analyze                — go/parser + go/types diagnostics (auth)
//	POST   /api/v1/blackbox/wizard/rewrite                — apply typed edits to source (auth)
//	GET    /api/v1/blackbox/wizard/draft/:projectId       — load this user's draft (auth)
//	POST   /api/v1/blackbox/wizard/draft/:projectId       — upsert this user's draft (auth)
//	DELETE /api/v1/blackbox/wizard/draft/:projectId       — discard this user's draft (auth)
//
// The GET /api/v1/blackbox endpoint is intentionally public — the WASM IDE
// loads it without a token, same as before.
//
// The submit flow:
//  1. POST /api/v1/blackbox/submit  → validates URL, enqueues device:github task
//  2. Client polls GET /api/v1/blackbox/jobs/:jobId every 2s
//  3. Worker downloads ZIP, parses IDS structs, saves to blackboxes table
//  4. Poll returns { status: "done", devices: [...] } or { status: "error" }
//
// Registration in cmd/server/main.go:
//
//	blackboxapi.Register(v1, asynqClient, rdb)
package blackboxapi

import (
	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"server/handler/spaauth"
	"server/middleware"
)

// Register mounts all device endpoints on the /api/v1 group.
func Register(v1 *echo.Group, asynqClient *asynq.Client, rdb *redis.Client) {
	h := &handler{asynq: asynqClient, redis: rdb}

	// OptionalAuth — when the user is logged in, the WASM sends the Bearer
	// token and the handler includes the user's own private devices in the
	// response. Anonymous callers see only public devices.
	v1.GET("/blackbox", h.handleList, middleware.OptionalAuth())

	// Protected — requires a valid Bearer JWT.
	auth := v1.Group("/blackbox", spaauth.RequireBearerToken())

	// Wizard endpoints. Registered before the dynamic /:id routes so Echo's
	// route resolver matches the static "wizard" segment first. (Echo
	// always prefers static segments over dynamic ones, but registering
	// in this order also makes the intent obvious to a reader.)
	auth.POST("/wizard/parse", h.handleWizardParse)
	auth.POST("/wizard/analyze", h.handleWizardAnalyze)
	auth.POST("/wizard/rewrite", h.handleWizardRewrite)
	auth.GET("/wizard/draft/:projectId", h.handleWizardDraftGet)
	auth.POST("/wizard/draft/:projectId", h.handleWizardDraftSave)
	auth.DELETE("/wizard/draft/:projectId", h.handleWizardDraftDelete)

	// Submit + management endpoints.
	auth.POST("/submit", h.handleSubmit)
	auth.GET("/jobs/:jobId", h.handleJobStatus)
	auth.GET("/mine", h.handleListMine)
	// GET /:id must be registered AFTER the more-specific /jobs/:jobId and /mine
	// routes. Echo resolves static path segments before dynamic ones, so /mine
	// and /jobs/:jobId always win over /:id for those prefixes.
	auth.GET("/:id", h.handleGetOne)
	auth.PUT("/:id/meta", h.handleUpdateMeta)
	auth.DELETE("/:id", h.handleDelete)
}

// handler holds handler dependencies.
type handler struct {
	asynq *asynq.Client
	redis *redis.Client
}
