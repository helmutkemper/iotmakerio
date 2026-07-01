// server/handler/codegen/status.go — Codegen job status endpoint.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Lightweight informational endpoint for the client (and for ops debugging).
// The SSE stream endpoint is the primary delivery channel for results — this
// endpoint is meant for two specific use cases:
//
//  1. The client wants to know the state without holding an EventSource open
//     (e.g. a "Jobs" panel that lists recent submissions).
//
//  2. The EventSource dropped the connection (proxy timeout, tab backgrounded)
//     and the client wants to decide whether to reopen the stream or display
//     a stale "still running" indicator.
//
// Response shape:
//
//	{
//	  "jobId":    "<id>",
//	  "language": "go",         // omitted when unknown
//	  "status":   "queued|running|done|failed|unknown",
//	  "error":    "...message"  // present only when status == "failed"
//	}
//
// "unknown" is reported in three distinct situations, and the client cannot
// tell them apart by design (see ownership.go for the rationale):
//
//   - The state key has expired or never existed.
//   - The jobID belongs to a different user.
//   - The Bearer claim is missing or empty (middleware misconfiguration).
//
// From the legitimate owner's perspective the third case is impossible (their
// JWT will always parse), so collapsing all three into "unknown" costs them
// nothing while denying an attacker any signal that the jobID exists.
//
// Português:
//
//	Espelha o estado real do job no Redis, MAS somente para o dono — qualquer
//	outro usuário recebe "unknown" idêntico ao caso de job inexistente.
package codegen

import (
	"errors"
	"fmt"
	"net/http"

	"server/handler/spaauth"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// statusResponse is the JSON the endpoint returns. Tags are camelCase per
// the project-wide invariant on Go→JS serialisation.
type statusResponse struct {
	JobID    string `json:"jobId"`
	Language string `json:"language,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// statusUnknown builds the canonical "no information available" response.
// Used both for the redis.Nil branch (key truly absent) and the
// ownership-mismatch branch (caller is not the owner). Centralising the
// shape here means the two paths are bit-identical to the client.
//
// Português:
//
//	Resposta canônica de "sem informação". Usada tanto para chave ausente
//	quanto para usuário não-dono — garantindo que o cliente não distinga.
func statusUnknown(jobID string) statusResponse {
	return statusResponse{JobID: jobID, Status: "unknown"}
}

func (h *handler) handleStatus(c echo.Context) error {
	jobID := c.Param("id")
	ctx := c.Request().Context()

	// Ownership gate. Three branches; only one of them lets us proceed
	// past this block.
	claims := spaauth.BearerClaims(c)
	owns, ownErr := h.ownsJob(ctx, jobID, claims.UserID)
	if ownErr != nil {
		// Infra error or empty userID. We log it (so ops can see Redis
		// trouble or middleware misconfiguration) but the response is
		// the same "unknown" the no-owner-key branch returns. This is
		// the deliberate "404 for everything that touches identity"
		// policy — never leak that the jobID exists.
		c.Logger().Warnf("[codegen] status: ownership check for job=%s: %v", jobID, ownErr)
		return c.JSON(http.StatusOK, statusUnknown(jobID))
	}
	if !owns {
		// Either the job is gone or it belongs to someone else. Same
		// response either way — the client cannot tell which.
		return c.JSON(http.StatusOK, statusUnknown(jobID))
	}

	state, err := h.redis.Get(ctx, fmt.Sprintf("codegen:job:%s:state", jobID)).Result()
	if err != nil {
		// redis.Nil means the state key is gone (TTL expired or never
		// set) even though the userId key was still around — possible
		// during the small window where one key was evicted before the
		// other. Report "unknown" to the owner, same as if the whole
		// job were gone.
		if errors.Is(err, redis.Nil) {
			return c.JSON(http.StatusOK, statusUnknown(jobID))
		}
		c.Logger().Errorf("[codegen] status: redis get state job=%s: %v", jobID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "internal: state lookup failed",
		})
	}

	resp := statusResponse{
		JobID:  jobID,
		Status: state,
	}

	// Language is best-effort: it is set at submit time but a job that
	// outlived its language key (or that was created before this code
	// shipped) simply omits the field. Failing the request because of a
	// missing-but-optional secondary key would be poor judgement.
	if lang, err := h.redis.Get(ctx, fmt.Sprintf("codegen:job:%s:language", jobID)).Result(); err == nil {
		resp.Language = lang
	} else if !errors.Is(err, redis.Nil) {
		c.Logger().Warnf("[codegen] status: redis get lang job=%s: %v", jobID, err)
	}

	// When the job failed, surface the infra error message so the client
	// can show something more useful than the literal string "failed".
	// Semantic errors from the codegen pipeline (geometric conflicts, etc.)
	// live in codegen:job:{id}:result.diagnostics, not in :error — those
	// are part of a successful "done" job whose Response carries errors.
	if state == "failed" {
		errMsg, getErr := h.redis.Get(ctx, fmt.Sprintf("codegen:job:%s:error", jobID)).Result()
		if getErr == nil {
			resp.Error = errMsg
		} else if !errors.Is(getErr, redis.Nil) {
			c.Logger().Warnf("[codegen] status: redis get error job=%s: %v", jobID, getErr)
		}
	}

	return c.JSON(http.StatusOK, resp)
}

// stillRunning is a small helper used by the stream handler — kept in this
// file because it shares its understanding of which state strings count as
// "the worker has not yet published a result". Keeping this here, rather
// than duplicating the literal in stream.go, makes the lifecycle explicit
// in one place. queued and running are non-terminal; done, failed,
// cancelled, and any unrecognised string are terminal.
//
// Português:
//
//	Helper compartilhado com o stream handler. queued e running são
//	estados não-terminais; done/failed/cancelled e qualquer outro
//	são terminais.
func stillRunning(state string) bool {
	return state == "queued" || state == "running"
}
