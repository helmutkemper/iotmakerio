// /ide/server/handler/templateapi/routes.go — Template Package API route registration.
//
// Routes mounted on /api/v1/templates:
//
//	POST   /api/v1/templates                     — create a new template package (no ZIP)
//	GET    /api/v1/templates                     — list templates visible to the caller
//	GET    /api/v1/templates/:id                 — get a single template (with full def)
//	POST   /api/v1/templates/:id/github          — submit a GitHub release URL (owner only)
//	PUT    /api/v1/templates/:id/visibility      — change visibility (owner only)
//	PUT    /api/v1/templates/:id/publishing      — set community publishing flags (owner only)
//	DELETE /api/v1/templates/:id                 — delete template + all ZIPs (owner only)
//	POST   /api/v1/templates/:id/generate        — generate configured output ZIP (maker)
//
// Two-step creation flow:
//
//  1. POST /api/v1/templates           → creates the parent record (status=no_version).
//  2. POST /api/v1/templates/:id/versions → uploads a ZIP, worker parses it.
//     Poll GET /api/v1/templates/:id until status != "pending".
//
// This mirrors the projects flow: the project is created first, then code
// is uploaded to it. The template name and description come from the "New
// Project" modal, NOT from inside the ZIP.
//
// Visibility vs Publishing:
//
//	PUT /visibility  — controls access (public/private). Any time.
//	PUT /publishing  — controls community discovery. Requires public+ready.
//
// Registration in cmd/server/main.go:
//
//	templateapi.Register(v1, asynqClient)
package templateapi

import (
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"server/handler/spaauth"
)

// ── Rate limit constants ───────────────────────────────────────────────────────

const (
	// uploadRateLimit is the maximum ZIP version uploads per IP per minute.
	uploadRateLimit = 5

	// generateRateLimit is the maximum generate requests per IP per minute.
	generateRateLimit = 10

	// maxUploadBodyBytes is the hard cap on the multipart request body.
	// 52 MB = 50 MB file limit + 2 MB multipart envelope headroom.
	maxUploadBodyBytes = "52MB"
)

// Register mounts all template API routes on the /api/v1/templates sub-group.
func Register(v1 *echo.Group, asynqClient *asynq.Client, rdb *redis.Client) {
	h := &handler{asynq: asynqClient, redis: rdb}

	// All template routes require a valid Bearer JWT.
	g := v1.Group("/templates", spaauth.RequireBearerToken())

	// ── Package CRUD ──────────────────────────────────────────────────────────
	g.POST("", h.handleCreate)               // create + enqueue github parse, returns job_id
	g.GET("/jobs/:jobId", h.handleJobStatus) // poll parse result from Redis
	g.GET("", h.handleList)
	g.GET("/:id", h.handleGet)
	g.PUT("/:id/visibility", h.handleVisibility)
	g.PUT("/:id/publishing", h.handlePublishing)
	g.PUT("/:id/tags", h.handleTags)
	g.DELETE("/:id", h.handleDelete)

	// ── GitHub release submit — rate limited ─────────────────────────────────
	g.POST("/:id/github",
		h.handleSubmitGithub,
		newIPRateLimiter(uploadRateLimit),
	)

	// ── Legacy ZIP upload — returns 410 Gone ──────────────────────────────────
	g.POST("/:id/versions", h.handleUploadVersion)

	// ── Generate — rate limit only (CPU-bound) ────────────────────────────────
	g.POST("/:id/generate",
		h.handleGenerate,
		newIPRateLimiter(generateRateLimit),
	)
}

// handler holds the dependencies for all template API handlers.
type handler struct {
	asynq *asynq.Client
	redis *redis.Client
}

// newIPRateLimiter returns an Echo middleware that allows ratePerMinute requests
// per remote IP using an in-memory token-bucket store.
func newIPRateLimiter(ratePerMinute int) echo.MiddlewareFunc {
	rps := rate.Limit(float64(ratePerMinute) / 60.0)

	store := middleware.NewRateLimiterMemoryStoreWithConfig(
		middleware.RateLimiterMemoryStoreConfig{
			Rate:      rps,
			Burst:     ratePerMinute,
			ExpiresIn: 2,
		},
	)

	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Skipper: middleware.DefaultSkipper,
		Store:   store,
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
		ErrorHandler: func(c echo.Context, err error) error {
			return err
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			return c.JSON(http.StatusTooManyRequests, map[string]any{
				"metadata": map[string]any{
					"status": http.StatusTooManyRequests,
					"error":  "too many requests — please wait a moment before trying again",
				},
				"data": nil,
			})
		},
	})
}
