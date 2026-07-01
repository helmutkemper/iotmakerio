// handler/codegen/register.go — Code generation route registration.
//
// Mounts four routes on /api/v1/codegen, all protected by
// spaauth.RequireBearerToken():
//
//	POST   /:language                — handleSubmit  (enqueue codegen job)
//	GET    /jobs/:id/status          — handleStatus  (poll current state)
//	GET    /jobs/:id/stream          — handleStream  (SSE result delivery)
//	POST   /jobs/:id/cancel          — handleCancel  (interrupt the job)
//
// Two-layer access control:
//
//  1. spaauth.RequireBearerToken (this file)
//     Rejects any request without a valid JWT in the Authorization header
//     with HTTP 401. Closes the "anonymous attacker burns worker CPU"
//     attack surface.
//
//  2. Per-route ownership check (ownership.go)
//     Inside every status/stream/cancel handler, the caller's JWT userID
//     is compared against codegen:job:{jobID}:userId, the owner recorded
//     by handleSubmit. Mismatch yields the exact same response shape as
//     "job does not exist" — never 403 — so an authenticated attacker
//     cannot probe for live jobIDs by watching response codes.
//
// Both layers must be present. Removing layer 1 reopens the anonymous-DOS
// path; removing layer 2 reverts to the previous behaviour where any
// authenticated user could read or cancel any jobID they could observe
// (logs, shared screens, etc.).
//
// The handler struct carries three live dependencies:
//
//	asynq     — Client used by submit to enqueue codegen:run tasks.
//	redis     — Client used by every handler to read and write the
//	             codegen:job:{id}:* keys that drive the state machine.
//	inspector — Used by cancel and by stream's disconnect path to
//	             ask Asynq to abort an in-flight task. Constructed
//	             once per process from the same RedisClientOpt the
//	             asynq.Client was built from.
//
// Português:
//
//	Registra as quatro rotas do codegen. A proteção é em duas camadas:
//	(1) middleware Bearer barra anônimo; (2) check de propriedade em
//	cada handler usa codegen:job:{id}:userId para garantir que só o
//	dono opera sobre o jobID. Mismatch responde como job inexistente,
//	sem vazar a existência do jobID.
package codegen

import (
	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"server/handler/spaauth"
)

// Register mounts codegen API routes on the given echo.Group.
// The group should already be scoped to /api/v1/codegen.
//
// redisOpt is consumed (no longer ignored) to build the asynq.Inspector
// that handleCancel and the stream disconnect path rely on.
func Register(
	g *echo.Group,
	client *asynq.Client,
	rdb *redis.Client,
	redisOpt asynq.RedisClientOpt,
) {
	h := &handler{
		asynq:     client,
		redis:     rdb,
		inspector: asynq.NewInspector(redisOpt),
	}

	// Every codegen route requires a logged-in user. The per-job
	// ownership gate inside each handler then narrows access from
	// "any authenticated user" to "the user who submitted this jobID".
	auth := g.Group("", spaauth.RequireBearerToken())

	auth.POST("/:language", h.handleSubmit)
	auth.GET("/jobs/:id/status", h.handleStatus)
	auth.GET("/jobs/:id/stream", h.handleStream)
	auth.POST("/jobs/:id/cancel", h.handleCancel)
}

// handler bundles the three dependencies the four endpoints share.
// The Inspector is owned by Register and lives for the process
// lifetime; closing it is not necessary because the asynq library
// shares the underlying pool with the client.
type handler struct {
	asynq     *asynq.Client
	redis     *redis.Client
	inspector *asynq.Inspector
}
