// server/handler/codegen/stream_test.go — Tests for handleStream.
//
// What handleStream does today (post-async refactor with ownership gate):
//
//	GET /api/v1/codegen/jobs/{id}/stream is a Server-Sent Events
//	endpoint. The handler:
//	  1. Sets SSE headers and writes a ": connected job=X" comment.
//	     This is emitted BEFORE the ownership check — every caller, owner
//	     or not, sees the same well-formed SSE prelude.
//	  2. Checks ownership against codegen:job:{id}:userId. Mismatch (or
//	     missing key, or empty caller identity, or Redis trouble) emits
//	     a single event: error with the same "job not found or expired"
//	     literal that a non-existent job would later produce, and exits.
//	  3. Checks codegen:job:{id}:state immediately. If terminal, emits
//	     the matching event and returns. This fast path handles the
//	     EventSource reconnect case.
//	  4. Otherwise polls :state every streamPollInterval (500 ms).
//	  5. State transitions and their SSE events:
//	     done    → event: result + data: <:result JSON>
//	     failed  → event: error  + data: {"message":<:error>,...}
//	     unknown → event: error  + data: {"message":"job not found..."}
//	  6. There is no event:timeout in the new contract — the SSE channel
//	     stays open until a terminal state is reached or the client
//	     drops the connection (ctx.Done → return cleanly, no event).
//
// What we pin here:
//
//   - SSE headers (the IDE EventSource relies on Content-Type).
//   - state=done with :result → event: result with the JSON payload
//     (for the legitimate owner).
//   - state=failed with :error → event: error with the message
//     (for the legitimate owner).
//   - no :state key → event: error with a "job not found" message.
//   - request context cancelled → handler returns cleanly without
//     emitting an error (no event:timeout to assert; absence is the
//     contract).
//   - Cross-tenant: a different user opening the stream receives the
//     EXACT SAME event: error "job not found or expired" reply that
//     a non-existent job produces. No event: result is ever emitted
//     even though the underlying job is in state="done".
package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHandleStream_setsSSEHeaders pins the on-wire shape of the
// initial response. The IDE's EventSource client looks for these
// headers; if they ever drift (especially Content-Type), the IDE
// falls back to text mode and renders the SSE frames as garbage.
func TestHandleStream_setsSSEHeaders(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	// Seed ownership + terminal state so the handler returns promptly
	// after the SSE prelude (ownership gate passes; fast path fires).
	const jobID = "header-job"
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "done"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:result", jobID),
		`{"code":"","errors":[],"warnings":[]}`); err != nil {
		t.Fatalf("seed :result: %v", err)
	}

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/stream", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want %q", got, "text/event-stream")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control: got %q, want %q", got, "no-cache")
	}
	if got := rec.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection: got %q, want %q", got, "keep-alive")
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering: got %q, want %q (Nginx will buffer SSE without this)", got, "no")
	}
}

// TestHandleStream_donePresent_emitsResultEvent is the happy path.
// :userId, :state and :result are seeded; the ownership gate passes
// and the fast path inside the handler reads the terminal state on
// entry and emits the event without waiting for a tick.
func TestHandleStream_donePresent_emitsResultEvent(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	const jobID = "happy-stream-job"
	const payload = `{"code":"package main","errors":[],"warnings":[]}`
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "done"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:result", jobID), payload); err != nil {
		t.Fatalf("seed :result: %v", err)
	}

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/stream", "")
	rec := httptest.NewRecorder()

	start := time.Now()
	e.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	body := rec.Body.String()

	// The connected comment must come first — the IDE uses this as
	// a "channel established" signal.
	if !strings.Contains(body, ": connected job="+jobID) {
		t.Errorf("missing connected comment for job %q. body: %s", jobID, body)
	}
	if !strings.Contains(body, "event: result") {
		t.Errorf("missing 'event: result' line. body: %s", body)
	}
	if !strings.Contains(body, "data: "+payload) {
		t.Errorf("missing data line for payload. got body: %s", body)
	}
	// Fast path: must NOT have waited a full poll interval. The
	// ownership Redis Get adds a single round-trip against miniredis
	// which is well under the 250ms budget.
	if elapsed > 250*time.Millisecond {
		t.Errorf("handler took %v — fast path should return well under one poll tick", elapsed)
	}
}

// TestHandleStream_failedPresent_emitsErrorEvent verifies the failure
// path. :userId + :state=failed + :error → event: error carrying the
// message in the shape the WASM client reads (parsed.Get("message")).
func TestHandleStream_failedPresent_emitsErrorEvent(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	const jobID = "failed-stream-job"
	const errMsg = "codegen panic: nil pointer dereference"
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "failed"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:error", jobID), errMsg); err != nil {
		t.Fatalf("seed :error: %v", err)
	}

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/stream", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("missing 'event: error' line. body: %s", body)
	}

	// Pull out the data payload and verify the JSON shape the client
	// expects: {"message":"...","infraError":"..."}.
	dataLine := extractSSEData(t, body, "error")
	var got struct {
		Message    string `json:"message"`
		InfraError string `json:"infraError"`
	}
	decodeJSON(t, dataLine, &got)

	if got.Message != errMsg {
		t.Errorf("message: got %q, want %q", got.Message, errMsg)
	}
}

// TestHandleStream_unknownJob_emitsErrorEvent verifies what happens
// when the client opens a stream for a job that does not exist (TTL
// expired, never submitted, or wrong ID). The contract is to emit
// event: error explaining the job is gone — never to hang.
//
// Note: with the ownership gate in place, "unknown job" is detected
// at the gate (no :userId key → ownsJob returns false) BEFORE the
// state polling loop. The wire-level effect is the same — the same
// event: error with the same message — but the path is slightly
// shorter. We keep the original assertion because what the client
// observes has not changed.
func TestHandleStream_unknownJob_emitsErrorEvent(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	// No keys seeded. The handler must detect the missing :userId at
	// the ownership gate and emit the "job not found" error.
	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/no-such-job/stream", "")
	rec := httptest.NewRecorder()

	start := time.Now()
	e.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("missing 'event: error' line. body: %s", body)
	}

	dataLine := extractSSEData(t, body, "error")
	var got struct {
		Message string `json:"message"`
	}
	decodeJSON(t, dataLine, &got)
	if !strings.Contains(strings.ToLower(got.Message), "not found") &&
		!strings.Contains(strings.ToLower(got.Message), "expired") {
		t.Errorf("error message does not mention not-found/expired; got %q", got.Message)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("handler took %v — unknown-job detection should be on the fast path", elapsed)
	}
}

// TestHandleStream_crossTenant_emitsNotFoundEvent is the security pin:
// a different user opens the stream for a job owned by defaultTestUserID
// that is sitting in state="done" with a real result. The handler MUST:
//
//   - never emit event: result (would leak the generated code)
//   - emit event: error with the same "job not found or expired" literal
//     that an unknown job would have produced
//
// Comparing the body against the unknown-job test above: the two
// responses are wire-identical except for the jobID echoed in the
// ": connected" comment.
func TestHandleStream_crossTenant_emitsNotFoundEvent(t *testing.T) {
	e, mr, _ := newCodegenTestServerForUser(t, "attacker")

	const jobID = "victims-stream-job"
	const secretPayload = `{"code":"package secret","errors":[],"warnings":[]}`

	// Seed as if defaultTestUserID had submitted and completed this job.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "done"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:result", jobID), secretPayload); err != nil {
		t.Fatalf("seed :result: %v", err)
	}

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/stream", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	body := rec.Body.String()

	// Hard fail: the attacker MUST NOT see event: result or the result
	// payload anywhere in the body.
	if strings.Contains(body, "event: result") {
		t.Fatalf("cross-tenant request received event: result — owner's result leaked. body: %s", body)
	}
	if strings.Contains(body, secretPayload) {
		t.Fatalf("cross-tenant request body contains owner's result payload. body: %s", body)
	}

	// And the response shape must match the "job not found" reply.
	if !strings.Contains(body, "event: error") {
		t.Errorf("missing 'event: error' line. body: %s", body)
	}
	dataLine := extractSSEData(t, body, "error")
	var got struct {
		Message string `json:"message"`
	}
	decodeJSON(t, dataLine, &got)
	if !strings.Contains(strings.ToLower(got.Message), "not found") &&
		!strings.Contains(strings.ToLower(got.Message), "expired") {
		t.Errorf("cross-tenant error message must look like 'not found' or 'expired'; got %q",
			got.Message)
	}
}

// TestHandleStream_contextCancelled_cancelsTask exercises the
// client-disconnect branch. Rather than waiting any real time, the
// test passes a request context that is already cancelled. The handler
// should:
//
//   - Return without emitting any SSE event (event:timeout no longer
//     exists in the new contract; clean exit is the signal).
//   - Ask Asynq to abort the task associated with this job — the
//     test pre-seeds codegen:job:{id}:asynqTaskId so the cancel path
//     has something to look up.
//   - Mark codegen:job:{id}:state="cancelled" so any later status
//     poll reflects the user's intent.
//
// The ownership gate is satisfied via the seeded :userId — the
// cancellation behaviour we're testing here is independent of access
// control.
func TestHandleStream_contextCancelled_cancelsTask(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	const jobID = "ctx-cancel-job"
	// Seed ownership for the test user.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	// Seed :state=queued so the fast path does NOT take a terminal
	// branch on its own — we want the cancellation path to be what
	// flips the state.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "queued"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}
	// Seed an asynq task ID so cancelTaskFromStream finds something
	// to call CancelProcessing on. The ID does not need to refer to
	// a real Asynq task — Inspector.CancelProcessing returns an error
	// for unknown IDs which the handler logs and ignores.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID), "fake-task-id"); err != nil {
		t.Fatalf("seed :asynqTaskId: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the handler runs

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	rec := httptest.NewRecorder()

	start := time.Now()
	e.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	body := rec.Body.String()
	// Connected comment may or may not be flushed depending on timing;
	// the contract we DO pin is "no terminal SSE event was emitted".
	if strings.Contains(body, "event: result") {
		t.Errorf("got event: result on a cancelled context. body: %s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Errorf("got event: error on a cancelled context — clean exit is the contract. body: %s", body)
	}
	if strings.Contains(body, "event: timeout") {
		t.Errorf("got event: timeout — that event was removed in the async refactor. body: %s", body)
	}

	// State must reflect the cancellation. The handler writes
	// state=cancelled inside cancelTaskFromStream.
	state, err := mr.Get(fmt.Sprintf("codegen:job:%s:state", jobID))
	if err != nil {
		t.Fatalf("redis Get state after cancel: %v", err)
	}
	if state != "cancelled" {
		t.Errorf("state after cancel: got %q, want %q", state, "cancelled")
	}

	// Should return promptly when the context is already cancelled —
	// this catches a regression where the handler ignores ctx.Done.
	if elapsed > 1*time.Second {
		t.Errorf("handler took %v on a pre-cancelled context — it isn't honouring the request context", elapsed)
	}
}

// extractSSEData scans an SSE body for the first occurrence of
// "event: <name>\ndata: <payload>\n\n" and returns just the payload.
// Used by the failure-path tests to validate the JSON shape inside
// the data line without relying on regex.
func extractSSEData(t *testing.T, body, eventName string) string {
	t.Helper()
	marker := "event: " + eventName + "\n"
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("event %q not found in body: %s", eventName, body)
	}
	rest := body[i+len(marker):]
	const dataPrefix = "data: "
	if !strings.HasPrefix(rest, dataPrefix) {
		t.Fatalf("expected %q after event line; got: %s", dataPrefix, rest)
	}
	rest = rest[len(dataPrefix):]
	end := strings.Index(rest, "\n\n")
	if end < 0 {
		t.Fatalf("data line not terminated by blank line: %s", rest)
	}
	// Defensive: confirm the slice is at least valid JSON before
	// handing it to the test for unmarshalling.
	payload := rest[:end]
	var probe any
	if err := json.Unmarshal([]byte(payload), &probe); err != nil {
		t.Fatalf("extracted data is not valid JSON: %v. payload: %s", err, payload)
	}
	return payload
}
