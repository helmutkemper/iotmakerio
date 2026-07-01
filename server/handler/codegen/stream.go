// server/handler/codegen/stream.go — Codegen result delivery over SSE.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The submit handler enqueues an Asynq task; the worker publishes the
// result to Redis under codegen:job:{id}:* and this handler streams it
// back to the WASM client over Server-Sent Events.
//
// Protocol (unchanged from the previous synchronous implementation, so
// stageWorkspace/workspace.go keeps working as-is):
//
//	event: result
//	data:  <JSON of codegen.Response>
//
//	event: error
//	data:  {"message":"...","infra_error":"..."}
//
// Polling strategy:
//
//	Every 500 ms, read codegen:job:{id}:state and codegen:job:{id}:result.
//	"done" → emit event:result with the JSON from :result, then close.
//	"failed" → emit event:error with the JSON from :error, then close.
//	"queued" / "running" → keep polling.
//	"unknown" / TTL expired → emit event:error explaining the job is gone.
//
//	500 ms is the cadence the project owner chose: the trade-off is between
//	Redis read load (~2 per second per open SSE) and end-to-end latency for
//	a sub-100ms job (the client sees the result up to 500ms after the
//	worker publishes it). For a CPU-bound pipeline that typically resolves
//	in 1-2s, the latency cost is invisible to humans.
//
// Ownership gate (see ownership.go):
//
//	Before any Redis poll, we verify the caller's JWT userID matches the
//	owner recorded at submit time. Mismatch is reported with the SAME
//	event: error "job not found or expired" message as a non-existent
//	job — the client cannot distinguish "you don't own this" from "this
//	never existed". This is intentional: leaking jobID existence would
//	let an authenticated attacker confirm which IDs are live.
//
//	The headers and the ": connected" comment are flushed BEFORE the
//	ownership check so the client always sees a well-formed SSE response
//	even when the answer is "no". An attacker watching only the response
//	shape sees the same thing the owner of a finished or expired job sees.
//
//	Special case: if the ownership Redis call fails because the request
//	context was cancelled (client disconnected before we could verify),
//	the gate does NOT emit any SSE event. It funnels into the same
//	disconnect path the ticker-loop ctx.Done branch uses — cancel the
//	Asynq task and mark state="cancelled". Without this carve-out, a
//	tab refresh during the brief window between flushing ": connected"
//	and finishing the ownership read would leave a spurious "not found"
//	frame on the wire AND skip the cancel-on-disconnect contract.
//
// No hard server-side timeout:
//
//	The previous implementation aborted after 15 seconds and emitted an
//	event:timeout, which broke any scene that needed longer than that to
//	compile. The new ceiling is the Asynq task timeout (120 s); when that
//	fires the worker marks state="failed" and this loop sees it on the
//	next tick.
//
//	The handler still honours the request context — if the client closes
//	the EventSource, ctx.Done() fires and we return without writing
//	anything. The browser's EventSource auto-reconnects on transient
//	failures, and because all state lives in Redis (not in this handler's
//	memory), a reconnect simply lands back in the same polling loop.
//
// Português:
//
//	Entrega o resultado do codegen via SSE. Polling de 500ms no Redis,
//	sem timeout duro do lado do servidor — quem decide o teto é o Asynq
//	via Timeout(120s) na task. Reconexão do EventSource cai de volta no
//	mesmo loop porque o estado vive todo no Redis.
//
//	Verifica propriedade antes de qualquer polling. Não-dono recebe a
//	mesma resposta que job inexistente, sem vazar a existência do jobID.
//	Se o ctx já estiver cancelado quando o gate roda, NÃO emite evento
//	— cai no mesmo caminho de desconexão (cancela a task, marca state).
package codegen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"server/handler/spaauth"
	"server/tasks"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

const (
	// streamPollInterval is the Redis poll cadence chosen by the project owner.
	// See package comment for the rationale.
	streamPollInterval = 500 * time.Millisecond

	// notFoundMessage is the user-facing string sent both when the job
	// genuinely does not exist AND when the caller is not the owner.
	// Centralising the literal here is what makes the two paths
	// indistinguishable to the client.
	notFoundMessage = "job not found or expired"
)

func (h *handler) handleStream(c echo.Context) error {
	jobID := c.Param("id")
	ctx := c.Request().Context()

	w := c.Response().Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering: no disables Nginx response buffering for this
	// endpoint. Without it, the proxy holds events in its buffer until a
	// flush threshold is reached and the client sees results in bursts.
	w.Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Greet the client with an SSE comment so the EventSource opens with
	// a confirmed connection. The browser does not surface this to the
	// JS listener; it is purely a heartbeat for any intermediate proxy.
	// We emit this BEFORE the ownership check on purpose — every caller
	// gets the same well-formed SSE prelude regardless of access.
	fmt.Fprintf(w, ": connected job=%s\n\n", jobID)
	flush()

	// Ownership gate. Three outcomes need separate handling:
	//
	//   1. The request context is already done. ownsJob's Redis.Get
	//      surfaces context.Canceled; we MUST treat this as the
	//      disconnect path (silent return, cancel the task, mark
	//      state="cancelled"). Emitting an SSE event here would put
	//      a "not found" frame on a connection the client has
	//      already abandoned AND skip the cancel-on-disconnect rule.
	//
	//   2. Genuine "no access" (mismatch, missing :userId key, empty
	//      caller identity, real Redis trouble). Emit the same
	//      notFoundMessage that the "job actually does not exist"
	//      branch in checkAndDeliver would emit later, then exit.
	//
	//   3. Owner confirmed. Fall through to the fast path / ticker
	//      loop below.
	claims := spaauth.BearerClaims(c)
	owns, ownErr := h.ownsJob(ctx, jobID, claims.UserID)
	if isClientGone(ctx, ownErr) {
		// Same policy as the ctx.Done branch in the ticker loop:
		// honour the disconnect by cancelling the Asynq task and
		// marking state="cancelled". Use context.Background() because
		// the request context is already terminal.
		h.cancelTaskFromStream(context.Background(), jobID)
		return nil
	}
	if ownErr != nil {
		// Log so ops can investigate genuine Redis trouble or a
		// middleware misconfiguration. The client gets nothing
		// specific — same wire shape as a non-existent job.
		fmt.Printf("[codegen/stream] ownership check job=%s: %v\n", jobID, ownErr)
		writeSSEError(w, flush, notFoundMessage, "")
		return nil
	}
	if !owns {
		writeSSEError(w, flush, notFoundMessage, "")
		return nil
	}

	// Fast path: if the worker has already finished by the time the
	// EventSource reaches us (very common — the WASM submit→fetch→open
	// EventSource sequence has measurable JS overhead while the worker
	// has nothing to do but run Generate), check the terminal keys before
	// entering the ticker loop. This is also the path a reconnecting
	// EventSource lands on after a proxy timeout.
	if delivered := h.checkAndDeliver(ctx, w, flush, jobID); delivered {
		// The fast path short-circuits on ctx cancellation without
		// emitting any SSE event. If that's why we landed here (the
		// client cancelled between submit and stream open), mirror
		// the ticker-loop disconnect behaviour explicitly: ask Asynq
		// to abort and mark state=cancelled. Without this, the
		// for/select ctx.Done branch — which does this cleanup —
		// would be bypassed entirely.
		if ctx.Err() != nil {
			h.cancelTaskFromStream(context.Background(), jobID)
		}
		return nil
	}

	ticker := time.NewTicker(streamPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client went away — closed the tab, hit F5, or network
			// dropped. Honour the project decision (option i): treat
			// every EventSource drop as intent to cancel, including
			// page refresh. The alternative (keep computing for a
			// client that may never return) was deemed worse by the
			// project owner; the worst case of this policy is that a
			// transient network blip cancels a near-finished job and
			// the user has to resubmit. Resubmitting is fast.
			//
			// Use context.Background() — the request context is
			// already cancelled, so it cannot do any more Redis work.
			h.cancelTaskFromStream(context.Background(), jobID)
			return nil

		case <-ticker.C:
			if h.checkAndDeliver(ctx, w, flush, jobID) {
				return nil
			}
		}
	}
}

// cancelTaskFromStream is the disconnect-driven cancellation path. It
// looks up the Asynq task ID associated with this job and asks Asynq
// to abort the worker. Best-effort: any failure here is logged but
// does not fail the SSE handler (which has already lost its client
// by the time this runs).
//
// Ownership is NOT re-checked here. Two of the three call sites reach
// this function only after handleStream's own gate has already passed.
// The third call site is the "ctx cancelled before the gate could run"
// branch — there, we have not confirmed ownership, but the only thing
// we do is cancel a task whose ID is bound to the SAME jobID the URL
// specified. The worst case is a disconnecting client happens to know
// a jobID belonging to someone else and that someone else's task gets
// cancelled. That window is narrow: the gate's Redis read only sees
// context.Canceled when the client has ALREADY abandoned the
// connection, so an attacker cannot engineer this race deterministically.
// Trade-off accepted in exchange for honouring the cancel-on-disconnect
// contract even during the ownership check window.
//
// Português:
//
//	Caminho de cancelamento disparado pela desconexão do cliente.
//	Best-effort: erros são apenas logados. Propriedade não é
//	re-verificada — ver comentário acima sobre o trade-off na
//	chamada onde isso ocorre.
func (h *handler) cancelTaskFromStream(ctx context.Context, jobID string) {
	asynqTaskKey := fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID)
	taskID, err := h.redis.Get(ctx, asynqTaskKey).Result()
	if err != nil {
		// redis.Nil is the common case — job already finished and the
		// mapping was either expired or simply never set (older code
		// path). Either way, nothing to cancel.
		return
	}
	if cancelErr := h.inspector.CancelProcessing(taskID); cancelErr != nil {
		// Worth logging because if this fires consistently it means
		// the task ID format changed or the Inspector lost its
		// Redis connection.
		fmt.Printf("[codegen/stream] cancelTaskFromStream task=%s: %v\n", taskID, cancelErr)
	}
	stateKey := fmt.Sprintf("codegen:job:%s:state", jobID)
	_ = h.redis.Set(ctx, stateKey, "cancelled", tasks.CodegenJobTTL).Err()
}

// checkAndDeliver looks up the job state in Redis and, when it is terminal,
// writes the corresponding SSE event and returns true. Returns false when
// the job is still in flight and the caller should keep polling.
//
// "unknown" — the state key has expired or never existed — is treated as
// terminal: we emit event:error explaining the job is gone, because there
// is no scenario in which continuing to poll would recover it.
//
// Client disconnect (ctx cancelled) is handled as a clean return — true
// without writing any event. Without this distinction, a cancelled
// request context would surface inside Redis as context.Canceled and
// the generic error branch would emit a spurious event:error to a
// client that already closed the connection.
func (h *handler) checkAndDeliver(
	ctx context.Context,
	w http.ResponseWriter,
	flush func(),
	jobID string,
) bool {
	// Short-circuit: the client may have closed the EventSource before
	// the fast path reached this function. No event to write, just exit.
	if ctx.Err() != nil {
		return true
	}

	stateKey := fmt.Sprintf("codegen:job:%s:state", jobID)
	state, err := h.redis.Get(ctx, stateKey).Result()

	if isClientGone(ctx, err) {
		return true
	}
	if errors.Is(err, redis.Nil) {
		// The state key is gone. Either the job never existed or its TTL
		// expired before completion (worker stalled past CodegenJobTTL).
		// Either way the client cannot recover; tell it so — using the
		// same literal that the ownership-mismatch path uses, so the
		// two cases remain indistinguishable on the wire.
		writeSSEError(w, flush, notFoundMessage, "")
		return true
	}
	if err != nil {
		// Infra error talking to Redis itself. Surface it and stop —
		// retrying in the same loop would only spam the logs.
		writeSSEError(w, flush, "internal stream error", err.Error())
		return true
	}

	if stillRunning(state) {
		return false
	}

	switch state {
	case "done":
		resultKey := fmt.Sprintf("codegen:job:%s:result", jobID)
		val, getErr := h.redis.Get(ctx, resultKey).Result()
		if isClientGone(ctx, getErr) {
			return true
		}
		if getErr != nil {
			// state says done but the result is missing. This is a
			// worker bug (state written before result, or result key
			// expired earlier than state). Report it as infra error.
			writeSSEError(w, flush,
				"job marked done but result is missing",
				getErr.Error())
			return true
		}
		// Forward the codegen.Response JSON verbatim. The client parses
		// it as JSON.parse(data) and reads .code, .errors, .warnings,
		// .diagnostics — exactly the fields codegen.Response defines.
		fmt.Fprintf(w, "event: result\ndata: %s\n\n", val)
		flush()
		return true

	case "cancelled":
		// The user cancelled this job — either via the explicit /cancel
		// endpoint or by closing the EventSource (which our disconnect
		// path turns into a cancel). In practice the client that did
		// the cancelling has already disconnected, so this event is
		// rarely observed; it covers the case where a SECOND viewer
		// (or a re-opened stream after F5+resubmit-on-same-id) is
		// listening when the cancellation lands.
		writeSSEError(w, flush, "codegen cancelled", "")
		return true

	case "failed":
		errKey := fmt.Sprintf("codegen:job:%s:error", jobID)
		msg, getErr := h.redis.Get(ctx, errKey).Result()
		if isClientGone(ctx, getErr) {
			return true
		}
		if getErr != nil || msg == "" {
			msg = "codegen job failed without an error message"
		}
		writeSSEError(w, flush, msg, "")
		return true

	default:
		// Unknown state string — neither queued/running nor a recognised
		// terminal state. Either a bug in the worker or a new state we
		// have not taught this handler about. Treat as terminal to
		// avoid an infinite loop, and report a clear infra error.
		writeSSEError(w, flush,
			fmt.Sprintf("unexpected job state: %q", state),
			"")
		return true
	}
}

// isClientGone reports whether the request context has been cancelled,
// either by the deadline expiring or the client disconnecting. Used
// throughout checkAndDeliver AND by the ownership gate to distinguish
// "the client went away" — where the contract is to return silently
// without writing an event — from genuine Redis or marshalling errors
// that should surface as event: error.
//
// The function checks both the error returned by the most recent Redis
// call (go-redis surfaces context.Canceled / context.DeadlineExceeded
// when the operation observed cancellation) and ctx.Err() directly,
// which covers the window where cancellation happens between two
// successive Redis calls.
//
// Português:
//
//	Distingue desconexão do cliente (saída silenciosa, sem evento) de
//	erro real de Redis (deve virar event:error). go-redis devolve
//	context.Canceled como erro quando o ctx é cancelado antes da
//	resposta — sem este check, esse erro viraria um event:error
//	enviado a um cliente que já fechou a conexão.
func isClientGone(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx.Err() != nil
}

// writeSSEError emits a single event:error frame and flushes it. The shape
// of the data payload matches what the WASM client expects:
//
//	parsed.Get("message")    — user-facing string
//	parsed.Get("infraError") — secondary technical detail (optional)
//
// See stageWorkspace/workspace.go onError listener. The infraError field
// was renamed from snake_case (infra_error) to camelCase (infraError) to
// honour the project-wide invariant on Go→JS JSON tags; the WASM client
// was updated in lockstep.
//
// json.Marshal is used (instead of fmt %q) because %q uses Go-flavoured
// escaping that diverges from JSON for non-ASCII runes and control bytes;
// the client parses the data with JSON.parse and would reject those.
func writeSSEError(w http.ResponseWriter, flush func(), message, infra string) {
	payload, err := json.Marshal(struct {
		Message    string `json:"message"`
		InfraError string `json:"infraError"`
	}{Message: message, InfraError: infra})
	if err != nil {
		// json.Marshal of two strings cannot reasonably fail; if it
		// somehow does we still need to terminate the stream cleanly
		// so the client does not hang waiting for an event.
		payload = []byte(`{"message":"internal: marshal error payload","infraError":""}`)
	}
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	flush()
}
