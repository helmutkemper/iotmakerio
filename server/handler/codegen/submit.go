// server/handler/codegen/submit.go — Codegen job submission endpoint.
//
// Flow:
//
//  1. Read the authenticated user from the Bearer claims. Empty userID
//     here is a configuration bug — the middleware should have rejected
//     the request before it reached us. We surface it as a 500.
//  2. Decode the request body: {"scene": <SceneJSON>}.
//  3. Resolve the black-box definitions the scene references (SQLite lookup).
//     This is intentionally done in the request thread, not in the worker:
//     the lookup is cheap, the result is mutable state in SQLite (the
//     specialist may have edited a black-box between the maker submitting
//     and the worker picking the job up), and keeping it here means the
//     worker stays self-contained — it only needs the payload and codegen.
//  4. Generate a job ID, prime Redis with state="queued", the language,
//     and the OWNER USER ID, then enqueue the codegen:run Asynq task.
//     The status, stream and cancel endpoints all read the userId key
//     to gate per-user access.
//  5. Respond 202 Accepted with {"stream_url": "..."} — the same shape the
//     synchronous implementation returned, so stageWorkspace/workspace.go
//     does not change.
//
// Why state is written before enqueue:
//
//	The Asynq client returns as soon as the task is in Redis, but the worker
//	may pick it up and finish before this handler writes anything else. If
//	the EventSource were to reconnect at that exact moment, status.go would
//	report "unknown" and the client would think the job was lost. Priming
//	state="queued" (with the same TTL the worker uses for the result) keeps
//	status.go honest.
//
// Why userId is written:
//
//	Per-user job isolation. See ownership.go for the rationale and the
//	response policy on mismatch. Note that userId is written BEFORE the
//	task is enqueued — once the task is on the queue, the worker may
//	publish a "done" state before this handler finishes, and any client
//	that opened the stream/status endpoint in that window must already
//	find a userId to compare against.
//
// Português:
//
//	Endpoint que enfileira um job de codegen. Faz o lookup dos BlackBoxDefs
//	no thread HTTP (custo baixo, estado mais fresco). Grava state="queued"
//	E userId no Redis ANTES de enfileirar para evitar janela onde
//	status.go/stream.go responderiam errado.
package codegen

import (
	"encoding/json"
	"fmt"
	"net/http"

	cryptoauth "server/auth"
	"server/handler/spaauth"
	"server/store"
	"server/tasks"

	"github.com/labstack/echo/v4"
)

func (h *handler) handleSubmit(c echo.Context) error {
	language := c.Param("language")

	// Identify the caller. The Bearer middleware should already have
	// populated claims; an empty UserID here means routing was set up
	// without the middleware — refuse to enqueue an unattributed job.
	claims := spaauth.BearerClaims(c)
	userID := claims.UserID
	if userID == "" {
		c.Logger().Errorf("[codegen] submit: empty userID in claims (auth middleware misconfigured?)")
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: missing identity",
		})
	}

	// Body: {"scene": {...}} for a full generation, or {"previewCase": {...}}
	// for the StatementCase inspect-panel preview. Exactly one is expected;
	// previewCase takes precedence when both are present (the preview path
	// needs no scene and no black-box resolution).
	var body struct {
		Scene       json.RawMessage         `json:"scene"`
		PreviewCase *tasks.CasePreviewInput `json:"previewCase"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	// Build the task payload. The two flows diverge only in what they put in
	// the payload; the Redis priming, enqueue and stream_url below are shared.
	payload := tasks.CodegenPayload{Language: language}

	if body.PreviewCase != nil {
		// Preview path: the worker renders the draft cases via
		// codegen.PreviewCase. No scene, so no black-box lookup is needed.
		payload.PreviewCase = body.PreviewCase
	} else {
		// Full-generation path: a scene is required, and the black-box
		// definitions it references are resolved here — a cheap SQLite lookup
		// kept in the request thread (mutable state, freshest here) so the
		// worker stays self-contained.
		if len(body.Scene) == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "scene is required"})
		}

		// Non-fatal: a scene that uses only built-in primitives (Add, loop,
		// if, …) returns an empty map and that is fine — codegen.Generate
		// handles a nil BlackBoxDefs map.
		bbDefs, err := store.LoadBlackBoxDefsForScene(body.Scene)
		if err != nil {
			c.Logger().Warnf("[codegen] loadBlackBoxDefs: %v", err)
		}

		// Marshalling cannot reasonably fail for a map of structs that were
		// themselves loaded from JSON in the store layer, but we surface the
		// error explicitly rather than silently dropping the defs.
		var bbDefsRaw json.RawMessage
		if len(bbDefs) > 0 {
			bbDefsRaw, err = json.Marshal(bbDefs)
			if err != nil {
				c.Logger().Errorf("[codegen] marshal bbDefs: %v", err)
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "internal: cannot serialise black-box definitions",
				})
			}
		}

		payload.Scene = body.Scene
		payload.BlackBoxDefs = bbDefsRaw
	}

	jobID, err := cryptoauth.NewID()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "job id"})
	}

	ctx := c.Request().Context()

	// Prime Redis BEFORE enqueueing. See the package comment for why.
	// Order of writes:
	//   1. state    — without this, status.go reports "unknown" in the
	//                 sub-millisecond window between Asynq accepting the
	//                 task and the worker writing its first "running".
	//   2. language — best-effort echo for the status endpoint; failure
	//                 here is tolerated.
	//   3. userId   — the ownership gate. Must be readable by every
	//                 status/stream/cancel call from this point on.
	//
	// Any of these failing is an infra-level problem that makes the job
	// non-recoverable; we abort and roll back what we already wrote so
	// a retry from the client sees a clean slate.
	stateKey := fmt.Sprintf("codegen:job:%s:state", jobID)
	if err := h.redis.Set(ctx, stateKey, "queued", tasks.CodegenJobTTL).Err(); err != nil {
		c.Logger().Errorf("[codegen] redis set state: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: queue unavailable",
		})
	}
	langKey := fmt.Sprintf("codegen:job:%s:language", jobID)
	_ = h.redis.Set(ctx, langKey, language, tasks.CodegenJobTTL).Err()

	userIDKey := fmt.Sprintf("codegen:job:%s:userId", jobID)
	if err := h.redis.Set(ctx, userIDKey, userID, tasks.CodegenJobTTL).Err(); err != nil {
		// Without userId we cannot enforce ownership on subsequent
		// requests — refuse to proceed and clean up what we already
		// wrote. The client can retry; the next submit attempt may
		// succeed if Redis recovered.
		c.Logger().Errorf("[codegen] redis set userId: %v", err)
		_ = h.redis.Del(ctx, stateKey, langKey).Err()
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: queue unavailable",
		})
	}

	// Build and enqueue the task. From this point on the request thread is
	// done — the worker will publish the result, and the EventSource that
	// the client opens on stream_url will deliver it.
	payload.JobID = jobID
	task, err := tasks.NewCodegenRunTask(payload)
	if err != nil {
		c.Logger().Errorf("[codegen] new task: %v", err)
		_ = h.redis.Del(ctx, stateKey, langKey, userIDKey).Err()
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: build task",
		})
	}
	taskInfo, err := h.asynq.EnqueueContext(ctx, task)
	if err != nil {
		c.Logger().Errorf("[codegen] enqueue: %v", err)
		// Roll back every key so a retry from the client does not see
		// a stale "queued" record for a job that was never accepted.
		_ = h.redis.Del(ctx, stateKey, langKey, userIDKey).Err()
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: enqueue failed",
		})
	}

	// Persist the Asynq task ID under codegen:job:{jobID}:asynqTaskId
	// so handleCancel and the stream-disconnect path can ask Asynq to
	// abort this specific task. Without this mapping, cancel would
	// have to scan every queue — Inspector.ListPending does that
	// internally, but it's expensive and racy compared to a direct
	// CancelProcessing(taskID).
	asynqTaskKey := fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID)
	_ = h.redis.Set(ctx, asynqTaskKey, taskInfo.ID, tasks.CodegenJobTTL).Err()

	c.Logger().Infof("[codegen] enqueued job=%s lang=%s user=%s preview=%t sceneBytes=%d",
		jobID, language, userID, payload.PreviewCase != nil, len(body.Scene))

	streamURL := fmt.Sprintf("/api/v1/codegen/jobs/%s/stream", jobID)

	// Contract preserved: workspace.go reads args[0].Get("stream_url").
	return c.JSON(http.StatusAccepted, map[string]string{
		"stream_url": streamURL,
	})
}
