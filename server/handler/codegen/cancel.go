// server/handler/codegen/cancel.go — Cancel an in-flight codegen job.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Endpoint: POST /api/v1/codegen/jobs/{id}/cancel
//
// Flow:
//
//  1. Verify that the caller (JWT claim) is the owner of this jobID.
//     Any negative outcome — missing claim, infra error, or mismatch —
//     returns the EXACT same 404 response that a non-existent jobID
//     would return. See ownership.go for the rationale.
//  2. Look up the Asynq task ID stored under codegen:job:{id}:asynqTaskId.
//     Missing key → 404 (the job either never existed or has expired).
//  3. Call inspector.CancelProcessing(taskID). Asynq signals the
//     worker by cancelling the handler context. The worker has four
//     cancellation checkpoints between codegen pipeline stages (see
//     server/codegen/codeGen.go), so Generate exits at the next stage
//     boundary — bounded extra CPU is one pipeline step, not 120 s.
//  4. Mark codegen:job:{id}:state="cancelled" so subsequent status
//     queries reflect the user's intent immediately, even before the
//     worker observes the cancellation and writes its own state="failed".
//
// State precedence: the cancel handler writes state="cancelled" first.
// The worker may later overwrite it with "failed" when ctx.Err() fires
// inside the codegen handler — both states are terminal, both signal
// "do not wait", so the order doesn't matter for the client. The
// distinction matters for ops: status reports "cancelled" while the
// task is still draining, and "failed" once the worker observed it.
//
// Response shape:
//
//	{ "jobId": "<id>", "status": "cancelled" }
//
// Idempotent on the user-facing side for the OWNER: cancelling an
// already-finished or already-cancelled job is a 200 with the same
// shape. The Asynq CancelProcessing call may return an error in those
// cases (task not being processed), which we log and ignore — the user
// asked to give up and we delivered that promise.
//
// Non-owner callers always receive 404, regardless of whether the job
// they tried to cancel exists or not.
//
// Português:
//
//	Endpoint que cancela um job em curso. Lê o asynqTaskId do Redis,
//	pede ao Inspector que cancele a task, e marca state=cancelled.
//	Idempotente do ponto de vista do dono. Qualquer outro usuário recebe
//	404, idêntico a job inexistente — sem vazar a existência do jobID.
package codegen

import (
	"errors"
	"fmt"
	"net/http"

	"server/handler/spaauth"
	"server/tasks"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// notFoundResponse is the single canonical reply for "this job does not
// exist OR you have no business asking about it". Centralised so the
// two code paths cannot drift apart by accident.
//
// Português:
//
//	Resposta canônica de 404 — usada tanto para job inexistente quanto
//	para acesso negado, sem distinção observável pelo cliente.
func notFoundResponse(c echo.Context) error {
	return c.JSON(http.StatusNotFound, map[string]string{
		"error": "job not found or already expired",
	})
}

func (h *handler) handleCancel(c echo.Context) error {
	jobID := c.Param("id")
	ctx := c.Request().Context()

	// Ownership gate. On any negative outcome we respond with the same
	// 404 the "no asynqTaskId mapping" branch produces — see ownership.go
	// for the policy rationale.
	claims := spaauth.BearerClaims(c)
	owns, ownErr := h.ownsJob(ctx, jobID, claims.UserID)
	if ownErr != nil {
		c.Logger().Warnf("[codegen] cancel: ownership check for job=%s: %v", jobID, ownErr)
		return notFoundResponse(c)
	}
	if !owns {
		return notFoundResponse(c)
	}

	asynqTaskKey := fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID)
	taskID, err := h.redis.Get(ctx, asynqTaskKey).Result()

	if errors.Is(err, redis.Nil) {
		// The mapping is gone. The owner check passed (so the userId
		// key is still around), but the asynqTaskId is gone — possible
		// during the small window where one key was evicted before the
		// other, or for legacy jobs created before this revision. Same
		// 404 a third-party would have received.
		return notFoundResponse(c)
	}
	if err != nil {
		c.Logger().Errorf("[codegen] cancel: redis get asynqTaskId job=%s: %v", jobID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: task lookup failed",
		})
	}

	// Ask Asynq to cancel processing. Errors here are non-fatal: the
	// task may have completed between the user's click and this code,
	// or it may have failed already. The user's intent ("stop waiting")
	// is honoured either way by the state="cancelled" write below.
	if cancelErr := h.inspector.CancelProcessing(taskID); cancelErr != nil {
		c.Logger().Warnf("[codegen] cancel: inspector.CancelProcessing(task=%s job=%s): %v",
			taskID, jobID, cancelErr)
	}

	// Mark state="cancelled" so the status endpoint reflects intent
	// immediately. The worker may later overwrite this with "failed"
	// when its own ctx.Err() check fires; that's fine — both states
	// are terminal and the client treats them the same.
	stateKey := fmt.Sprintf("codegen:job:%s:state", jobID)
	if setErr := h.redis.Set(ctx, stateKey, "cancelled", tasks.CodegenJobTTL).Err(); setErr != nil {
		c.Logger().Errorf("[codegen] cancel: redis set state job=%s: %v", jobID, setErr)
		// Don't fail the response on this — the Asynq cancel call
		// already went through and the user's intent has been
		// effected at the worker layer. status will report stale
		// "queued"/"running" for a tick but is self-correcting once
		// the worker writes its own "failed".
	}

	c.Logger().Infof("[codegen] cancelled job=%s task=%s user=%s",
		jobID, taskID, claims.UserID)

	return c.JSON(http.StatusOK, map[string]string{
		"jobId":  jobID,
		"status": "cancelled",
	})
}
