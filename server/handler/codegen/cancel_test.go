// server/handler/codegen/cancel_test.go — Tests for handleCancel.
//
// What handleCancel does:
//
//	POST /api/v1/codegen/jobs/{id}/cancel
//	  Verifies the caller's JWT identity matches codegen:job:{id}:userId.
//	  Looks up codegen:job:{id}:asynqTaskId, calls
//	  inspector.CancelProcessing on it, writes state="cancelled".
//
// What we pin here:
//
//   - Unknown job (no :asynqTaskId mapping, no :userId) returns 404 —
//     the client must not get a false "I cancelled it".
//   - Known and owned by the caller → 200 with {jobId, status: "cancelled"}
//     AND state flipped to "cancelled" in Redis (visible to status endpoint).
//   - Cancel is idempotent on the user-facing side for the owner: a
//     second cancel of the same job returns 200 because the mapping is
//     still there (TTL hasn't expired) — the Asynq cancel call returns
//     an error for a task that no longer exists, which we swallow.
//   - Known but owned by a different user → 404 (the same 404 a
//     non-existent job would produce). The job's state in Redis MUST
//     remain unchanged — the attacker should not be able to interfere
//     with the legitimate owner's job in any way.
//
// What we do NOT pin:
//
//   - That the worker actually stopped processing. The Asynq Inspector
//     can be told to cancel a task, but verifying the worker received
//     the signal requires running a real worker, which is a separate
//     integration test (see cmd/worker/codegen_test.go).
package codegen

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCancel_unknownJob_returns404(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	req := jsonRequest(t, http.MethodPost,
		"/api/v1/codegen/jobs/never-existed/cancel", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestHandleCancel_knownJob_marksStateCancelled verifies the happy
// path. We seed a :userId belonging to the test user plus an
// asynqTaskId pointing at a fictional task; the inspector call will
// fail (because the task does not exist in miniredis as an actual
// Asynq task), and the handler is expected to swallow that error and
// still flip the state. The reason this is the correct behaviour: the
// user's intent ("stop waiting") has nothing to do with whether the
// underlying task happens to still be in the queue. If we propagated
// that error, racy cancellations would surface as user-visible failures.
func TestHandleCancel_knownJob_marksStateCancelled(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t) // identity is defaultTestUserID

	const jobID = "known-job"
	// Seed ownership so the gate passes.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID), "fake-task"); err != nil {
		t.Fatalf("seed asynqTaskId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "running"); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	req := jsonRequest(t, http.MethodPost,
		"/api/v1/codegen/jobs/"+jobID+"/cancel", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		JobID  string `json:"jobId"`
		Status string `json:"status"`
	}
	decodeJSON(t, rec.Body.String(), &got)

	if got.JobID != jobID {
		t.Errorf("jobId: got %q, want %q", got.JobID, jobID)
	}
	if got.Status != "cancelled" {
		t.Errorf("status: got %q, want %q", got.Status, "cancelled")
	}

	// And the Redis state must reflect it for any subsequent status poll.
	state, err := mr.Get(fmt.Sprintf("codegen:job:%s:state", jobID))
	if err != nil {
		t.Fatalf("redis Get state: %v", err)
	}
	if state != "cancelled" {
		t.Errorf("state after cancel: got %q, want %q", state, "cancelled")
	}
}

// TestHandleCancel_idempotent verifies that cancelling an already-
// cancelled job still returns 200 for the legitimate owner. The Asynq
// Inspector call will error (task no longer exists) but the handler
// swallows that and re-writes state="cancelled".
func TestHandleCancel_idempotent(t *testing.T) {
	e, mr, _ := newCodegenTestServer(t)

	const jobID = "double-cancel-job"
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID), "fake-task"); err != nil {
		t.Fatalf("seed asynqTaskId: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := jsonRequest(t, http.MethodPost,
			"/api/v1/codegen/jobs/"+jobID+"/cancel", "")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("cancel call %d: got %d, want %d. body: %s",
				i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	// Final state still says cancelled (the second cancel did not
	// somehow flip it back).
	state, _ := mr.Get(fmt.Sprintf("codegen:job:%s:state", jobID))
	if state != "cancelled" {
		t.Errorf("state after two cancels: got %q, want %q", state, "cancelled")
	}
}

// TestHandleCancel_crossTenant_returns404 is the core security pin:
// user "attacker" tries to cancel a job owned by defaultTestUserID.
// The response MUST be 404 — bit-identical to the "unknown job" reply —
// AND the underlying state must NOT change. If either assertion fails
// the per-job isolation is broken.
//
// We use newCodegenTestServerForUser to swap the identity the test
// middleware injects; everything else mirrors the happy-path test.
func TestHandleCancel_crossTenant_returns404(t *testing.T) {
	e, mr, _ := newCodegenTestServerForUser(t, "attacker")

	const jobID = "victim-job"
	const realOwner = defaultTestUserID
	// Seed as if the legitimate user had created this job.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), realOwner); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:asynqTaskId", jobID), "real-task"); err != nil {
		t.Fatalf("seed :asynqTaskId: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "running"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}

	req := jsonRequest(t, http.MethodPost,
		"/api/v1/codegen/jobs/"+jobID+"/cancel", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d (cross-tenant must look like not-found). body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}

	// State must be untouched — the attacker should not be able to
	// influence the owner's job in any way.
	state, err := mr.Get(fmt.Sprintf("codegen:job:%s:state", jobID))
	if err != nil {
		t.Fatalf("redis Get state after cross-tenant cancel: %v", err)
	}
	if state != "running" {
		t.Errorf("state after cross-tenant cancel attempt: got %q, want %q (unchanged)",
			state, "running")
	}
}
