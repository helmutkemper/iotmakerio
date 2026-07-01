// server/handler/codegen/submit_test.go — Tests for handleSubmit.
//
// What handleSubmit does today (post-async refactor with ownership gate):
//
//  1. Reads spaauth.BearerClaims; empty UserID → 500 (middleware misconfig).
//  2. Binds {"scene": ...} from the request body. Empty/missing scene → 400.
//  3. Loads BlackBox defs that the scene references (skipped here —
//     the test scenes have no BlackBox devices, so the store is not
//     consulted).
//  4. Generates a job ID and primes Redis with:
//     codegen:job:{id}:state    = "queued"
//     codegen:job:{id}:language = <language>
//     codegen:job:{id}:userId   = <caller's JWT UserID>
//     all with TTL = tasks.CodegenJobTTL.
//  5. Enqueues an Asynq task of type tasks.TypeCodegenRun on the
//     tasks.QueueCodegen queue, carrying the scene and serialised
//     BlackBox defs.
//  6. Returns 202 Accepted with {"stream_url": "/api/v1/codegen/jobs/{id}/stream"}.
//
// What we pin here:
//
//   - 400 on a malformed body or empty scene.
//   - 202 + stream_url shape on a valid empty scene.
//   - :state primed to "queued" with the right TTL.
//   - :userId primed to the caller's identity (the ownership gate).
//   - A task of type codegen:run lands on the codegen queue.
//   - An unsupported language still returns 202 — the error surfaces
//     via the SSE stream once the worker runs, not here.
//   - Empty UserID (middleware bypassed) → 500, nothing written to Redis.
//
// Things we deliberately do NOT pin in this file:
//
//   - The contents of codegen:job:{id}:result. That key is written
//     by the worker, not by submit. The worker is exercised by tests
//     in server/cmd/worker (or by integration tests that boot both
//     processes), not here.
package codegen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/tasks"
)

func TestHandleSubmit_invalidBody(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", `{"scene": `)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "invalid body") {
		t.Errorf("body missing 'invalid body'; got %q", body)
	}
}

// TestHandleSubmit_missingScene mirrors TestHandleSubmit_invalidBody but
// covers the case where the JSON is parseable yet the scene field is
// absent or empty — which the handler now rejects with 400 because
// codegen.Generate against an empty RawMessage has nothing to do.
func TestHandleSubmit_missingScene(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", `{}`)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleSubmit_emptyScene_returnsAcceptedWithStreamURL(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var resp struct {
		StreamURL string `json:"stream_url"`
	}
	decodeJSON(t, rec.Body.String(), &resp)

	if resp.StreamURL == "" {
		t.Fatal("stream_url is empty")
	}
	const wantPrefix = "/api/v1/codegen/jobs/"
	const wantSuffix = "/stream"
	if !strings.HasPrefix(resp.StreamURL, wantPrefix) {
		t.Errorf("stream_url prefix: got %q, want prefix %q", resp.StreamURL, wantPrefix)
	}
	if !strings.HasSuffix(resp.StreamURL, wantSuffix) {
		t.Errorf("stream_url suffix: got %q, want suffix %q", resp.StreamURL, wantSuffix)
	}
}

// TestHandleSubmit_primesStateInRedis verifies the side effect the SSE
// stream and status endpoints depend on: after submit, the :state key
// MUST exist and equal "queued", and the :language key MUST carry the
// path param. These are the keys that let a reconnecting EventSource
// find its way back to a job mid-flight.
func TestHandleSubmit_primesStateInRedis(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, body: %s", rec.Code, rec.Body.String())
	}

	jobID := extractJobIDFromStreamURL(t, rec.Body.String())

	stateKey := fmt.Sprintf("codegen:job:%s:state", jobID)
	state, err := mr.Get(stateKey)
	if err != nil {
		t.Fatalf("redis Get %q: %v", stateKey, err)
	}
	if state != "queued" {
		t.Errorf("state: got %q, want %q", state, "queued")
	}

	langKey := fmt.Sprintf("codegen:job:%s:language", jobID)
	lang, err := mr.Get(langKey)
	if err != nil {
		t.Fatalf("redis Get %q: %v", langKey, err)
	}
	if lang != "go" {
		t.Errorf("language: got %q, want %q", lang, "go")
	}
}

// TestHandleSubmit_primesUserIDInRedis pins the ownership gate setup.
// Without this :userId key, every subsequent status/stream/cancel call
// would deny access to even the legitimate owner. The TTL window is
// asserted by the broader TTL test below — here we just confirm the
// presence and value.
func TestHandleSubmit_primesUserIDInRedis(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t) // identity is defaultTestUserID

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, body: %s", rec.Code, rec.Body.String())
	}
	jobID := extractJobIDFromStreamURL(t, rec.Body.String())

	userIDKey := fmt.Sprintf("codegen:job:%s:userId", jobID)
	got, err := mr.Get(userIDKey)
	if err != nil {
		t.Fatalf("redis Get %q: %v (the ownership gate has nothing to compare against)",
			userIDKey, err)
	}
	if got != defaultTestUserID {
		t.Errorf("userId: got %q, want %q", got, defaultTestUserID)
	}
}

// TestHandleSubmit_primesStateWithTTL verifies the persistence rule:
// the state key carries a TTL roughly equal to tasks.CodegenJobTTL.
// If a refactor accidentally drops the TTL, this test fires.
func TestHandleSubmit_primesStateWithTTL(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	jobID := extractJobIDFromStreamURL(t, rec.Body.String())

	// :state and :userId share the same TTL because they're written
	// from the same handler in the same window. Drift between them
	// would create a window where the job appears to exist for status
	// (state still alive) but is locked out (userId gone) or vice versa.
	for _, suffix := range []string{"state", "userId"} {
		key := fmt.Sprintf("codegen:job:%s:%s", jobID, suffix)
		ttl := mr.TTL(key)
		if ttl == 0 {
			t.Errorf("expected non-zero TTL on %q, got 0 (key unset or persistent)", key)
			continue
		}
		const slack = 10 * time.Second
		want := tasks.CodegenJobTTL
		if ttl > want+slack || ttl < want-slack {
			t.Errorf("TTL out of expected window for %q: got %v, want ~%v (±%v)",
				key, ttl, want, slack)
		}
	}
}

// TestHandleSubmit_emptyUserID_returns500 covers the misconfiguration
// path: a route that handleSubmit was mounted on without the Bearer
// middleware in front. Without identity, we MUST refuse to enqueue —
// the job would have no owner and be unreachable forever.
func TestHandleSubmit_emptyUserID_returns500(t *testing.T) {
	// Build the server with an empty userID — simulates the Bearer
	// middleware being absent.
	e, mr, _ := newCodegenTestServerForUser(t, "")

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	// And nothing should have been written to Redis — a half-submitted
	// job is worse than no submission.
	for _, key := range mr.Keys() {
		if strings.HasPrefix(key, "codegen:job:") {
			t.Errorf("unexpected codegen key in Redis after empty-uid submit: %q", key)
		}
	}
}

// TestHandleSubmit_enqueuesTaskOnCodegenQueue verifies the contract
// the worker depends on: after submit, exactly one task of type
// codegen:run sits on the codegen queue, and its payload carries the
// job ID, language and scene that came in over HTTP.
func TestHandleSubmit_enqueuesTaskOnCodegenQueue(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)
	insp := newInspector(t, mr)

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	jobID := extractJobIDFromStreamURL(t, rec.Body.String())

	pending, err := insp.ListPendingTasks(tasks.QueueCodegen)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("queue %q: got %d pending tasks, want 1", tasks.QueueCodegen, len(pending))
	}
	got := pending[0]
	if got.Type != tasks.TypeCodegenRun {
		t.Errorf("task type: got %q, want %q", got.Type, tasks.TypeCodegenRun)
	}

	// Decode the payload and verify the round-trip of jobID/language/scene.
	var p tasks.CodegenPayload
	if err := json.Unmarshal(got.Payload, &p); err != nil {
		t.Fatalf("unmarshal task payload: %v", err)
	}
	if p.JobID != jobID {
		t.Errorf("payload jobId: got %q, want %q", p.JobID, jobID)
	}
	if p.Language != "go" {
		t.Errorf("payload language: got %q, want %q", p.Language, "go")
	}
	if len(p.Scene) == 0 {
		t.Errorf("payload scene is empty")
	}
}

// TestHandleSubmit_unsupportedLanguage_stillAcceptsTask documents the
// contract that an unsupported language is NOT a 4xx at submit time.
// The handler returns 202 and the error reaches the client through the
// SSE stream once the worker runs codegen.Generate (which produces a
// KindUnsupportedLanguage diagnostic that flows into the Response).
//
// We do NOT verify the diagnostic contents here because that lives in
// the worker — and this package's tests intentionally do not run one.
// The worker-level test for the same path lives in cmd/worker.
func TestHandleSubmit_unsupportedLanguage_stillAcceptsTask(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)
	insp := newInspector(t, mr)

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/rust", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d (codegen errors flow through SSE, not HTTP). body: %s",
			rec.Code, http.StatusAccepted, rec.Body.String())
	}

	// And the task was enqueued, language carried verbatim — the worker
	// will decide what "rust" means when it runs Generate.
	pending, err := insp.ListPendingTasks(tasks.QueueCodegen)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("queue %q: got %d pending tasks, want 1", tasks.QueueCodegen, len(pending))
	}
	var p tasks.CodegenPayload
	if err := json.Unmarshal(pending[0].Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Language != "rust" {
		t.Errorf("payload language: got %q, want %q", p.Language, "rust")
	}
}

// TestHandleSubmit_persistsAsynqTaskID verifies the bridge that the
// cancel endpoint (and the stream's disconnect path) depend on: after
// submit, codegen:job:{jobID}:asynqTaskId is set to the ID returned by
// the Asynq Enqueue call. Without this mapping there is no way to ask
// Asynq to abort this specific task by job ID.
func TestHandleSubmit_persistsAsynqTaskID(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)
	insp := newInspector(t, mr)

	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	jobID := extractJobIDFromStreamURL(t, rec.Body.String())

	// Mapping must be present in Redis with the expected key shape.
	persistedTaskID, err := mr.Get(fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID))
	if err != nil {
		t.Fatalf("redis Get asynqTaskId for job=%s: %v", jobID, err)
	}
	if persistedTaskID == "" {
		t.Fatal("asynqTaskId is empty")
	}

	// And the persisted ID must match the ID Asynq sees on the queue.
	pending, err := insp.ListPendingTasks(tasks.QueueCodegen)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("queue %q: got %d pending tasks, want 1", tasks.QueueCodegen, len(pending))
	}
	if pending[0].ID != persistedTaskID {
		t.Errorf("asynqTaskId mismatch: persisted=%q, queue=%q",
			persistedTaskID, pending[0].ID)
	}
}

// extractJobIDFromStreamURL parses the JSON response from
// handleSubmit and returns the {id} segment of the stream URL.
// Failing this means the response shape is wrong, not the test —
// fail loudly.
func extractJobIDFromStreamURL(t *testing.T, respJSON string) string {
	t.Helper()
	var parsed struct {
		StreamURL string `json:"stream_url"`
	}
	if err := json.Unmarshal([]byte(respJSON), &parsed); err != nil {
		t.Fatalf("parse submit response: %v. raw: %s", err, respJSON)
	}
	const prefix = "/api/v1/codegen/jobs/"
	const suffix = "/stream"
	if !strings.HasPrefix(parsed.StreamURL, prefix) || !strings.HasSuffix(parsed.StreamURL, suffix) {
		t.Fatalf("unexpected stream_url shape: %q", parsed.StreamURL)
	}
	return strings.TrimSuffix(strings.TrimPrefix(parsed.StreamURL, prefix), suffix)
}
