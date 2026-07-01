// server/handler/codegen/status_test.go — Tests for handleStatus.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// What handleStatus does today (post-async refactor with ownership gate):
//
//	GET /api/v1/codegen/jobs/{id}/status verifies the caller's JWT
//	identity matches codegen:job:{id}:userId, then reads
//	codegen:job:{id}:state and reports it verbatim. The state machine is:
//
//	  queued  → submit handler primed this
//	  running → worker picked the task up, before Generate finishes
//	  done    → worker published the result
//	  failed  → worker hit an infra error (panic, timeout, etc.)
//	  unknown → state key absent, OR the caller is not the owner
//
//	When state="failed", the response also includes "error" with the
//	human-readable message stored at codegen:job:{id}:error. The
//	"language" key is included whenever codegen:job:{id}:language
//	exists, regardless of state.
//
// What we pin here:
//
//   - unknown job (no key) returns status="unknown" and no language.
//   - submit-then-status returns status="queued" (the new contract —
//     this used to be "unknown" before the refactor that primes :state).
//   - each terminal state (done/failed) is reflected verbatim for the
//     owner.
//   - failed status carries the :error message in the response.
//   - the JSON shape stays stable: jobId, status, optional language,
//     optional error.
//   - cross-tenant: a different user querying status receives the EXACT
//     SAME shape as an unknown job — no leak of existence or state.
package codegen

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleStatus_unknownJob_returnsUnknown pins the absent-key path:
// a job ID with no :state key returns status="unknown" with no language.
func TestHandleStatus_unknownJob_returnsUnknown(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/some-arbitrary-id/status", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		JobID    string `json:"jobId"`
		Status   string `json:"status"`
		Language string `json:"language"`
		Error    string `json:"error"`
	}
	decodeJSON(t, rec.Body.String(), &got)

	if got.JobID != "some-arbitrary-id" {
		t.Errorf("jobId: got %q, want %q", got.JobID, "some-arbitrary-id")
	}
	if got.Status != "unknown" {
		t.Errorf("status: got %q, want %q", got.Status, "unknown")
	}
	if got.Language != "" {
		t.Errorf("language should be empty when status is 'unknown'; got %q", got.Language)
	}
	if got.Error != "" {
		t.Errorf("error should be empty when status is 'unknown'; got %q", got.Error)
	}
}

// TestHandleStatus_submitThenStatus_returnsQueued documents the new
// contract: submit now primes :state="queued", so an immediate status
// query returns "queued" (not "unknown" as the previous synchronous
// implementation did). This is the key signal that the SSE stream
// has somewhere to land when an EventSource reconnects mid-job.
func TestHandleStatus_submitThenStatus_returnsQueued(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	// Submit a job.
	body := fmt.Sprintf(`{"scene": %s}`, emptyScene)
	req := jsonRequest(t, http.MethodPost, "/api/v1/codegen/go", body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	jobID := extractJobIDFromStreamURL(t, rec.Body.String())

	// Status of THAT job reports "queued" — submit primed :state, no
	// worker has picked it up in this test, so it stays queued.
	req = jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/status", "")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var got struct {
		Status   string `json:"status"`
		Language string `json:"language"`
	}
	decodeJSON(t, rec.Body.String(), &got)

	if got.Status != "queued" {
		t.Errorf("status: got %q, want %q", got.Status, "queued")
	}
	if got.Language != "go" {
		t.Errorf("language: got %q, want %q (submit also primes :language)", got.Language, "go")
	}
}

// TestHandleStatus_reflectsRealState exercises every terminal state
// for the legitimate owner. Each subtest seeds :userId (so the gate
// passes), :state (and where relevant, :language and :error) directly
// into miniredis and asserts the response shape.
func TestHandleStatus_reflectsRealState(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		lang     string
		errMsg   string
		wantErr  string // expected :error echo in the response
		wantLang string // expected :language echo in the response
	}{
		{name: "running", state: "running", lang: "go", wantLang: "go"},
		{name: "done", state: "done", lang: "go", wantLang: "go"},
		{name: "failed", state: "failed", lang: "go", errMsg: "boom", wantErr: "boom", wantLang: "go"},
		// failed without an :error key — common when Redis dropped the
		// error between :state being written and the client polling.
		{name: "failed-without-error-key", state: "failed", lang: "go", wantLang: "go"},
		// cancelled — explicit user intent (via the cancel endpoint or
		// the stream's disconnect path). The status endpoint reflects
		// it verbatim so a polling client knows not to wait.
		{name: "cancelled", state: "cancelled", lang: "go", wantLang: "go"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			e, mr, _ := newCodegenTestServer(t)
			const jobID = "seeded-job"

			// Seed ownership for the test user so the gate passes.
			if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID),
				defaultTestUserID); err != nil {
				t.Fatalf("seed :userId: %v", err)
			}
			if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), tc.state); err != nil {
				t.Fatalf("seed :state: %v", err)
			}
			if tc.lang != "" {
				if err := mr.Set(fmt.Sprintf("codegen:job:%s:language", jobID), tc.lang); err != nil {
					t.Fatalf("seed :language: %v", err)
				}
			}
			if tc.errMsg != "" {
				if err := mr.Set(fmt.Sprintf("codegen:job:%s:error", jobID), tc.errMsg); err != nil {
					t.Fatalf("seed :error: %v", err)
				}
			}

			req := jsonRequest(t, http.MethodGet,
				"/api/v1/codegen/jobs/"+jobID+"/status", "")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status code: got %d, want %d", rec.Code, http.StatusOK)
			}

			var got struct {
				JobID    string `json:"jobId"`
				Status   string `json:"status"`
				Language string `json:"language"`
				Error    string `json:"error"`
			}
			decodeJSON(t, rec.Body.String(), &got)

			if got.JobID != jobID {
				t.Errorf("jobId: got %q, want %q", got.JobID, jobID)
			}
			if got.Status != tc.state {
				t.Errorf("status: got %q, want %q", got.Status, tc.state)
			}
			if got.Language != tc.wantLang {
				t.Errorf("language: got %q, want %q", got.Language, tc.wantLang)
			}
			if got.Error != tc.wantErr {
				t.Errorf("error: got %q, want %q", got.Error, tc.wantErr)
			}
		})
	}
}

// TestHandleStatus_crossTenant_returnsUnknown is the security pin: a
// different user polling a job they don't own MUST receive the exact
// "unknown" shape that a non-existent job would produce. If status
// or language are leaked, an attacker can confirm jobID existence and
// even read the user's progress.
func TestHandleStatus_crossTenant_returnsUnknown(t *testing.T) {
	e, mr, _ := newCodegenTestServerForUser(t, "attacker")

	const jobID = "victims-status-job"
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), defaultTestUserID); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}
	// Seed an interesting state to make the leak detectable if the
	// gate fails.
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:state", jobID), "running"); err != nil {
		t.Fatalf("seed :state: %v", err)
	}
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:language", jobID), "go"); err != nil {
		t.Fatalf("seed :language: %v", err)
	}

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/"+jobID+"/status", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: got %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		JobID    string `json:"jobId"`
		Status   string `json:"status"`
		Language string `json:"language"`
		Error    string `json:"error"`
	}
	decodeJSON(t, rec.Body.String(), &got)

	// The shape must be indistinguishable from the "no key at all" reply.
	if got.Status != "unknown" {
		t.Errorf("status: got %q, want %q (cross-tenant must look like absent)",
			got.Status, "unknown")
	}
	if got.Language != "" {
		t.Errorf("language: got %q, want \"\" (cross-tenant must not leak language)", got.Language)
	}
	if got.Error != "" {
		t.Errorf("error: got %q, want \"\" (cross-tenant must not leak error)", got.Error)
	}
	if got.JobID != jobID {
		t.Errorf("jobId echo: got %q, want %q", got.JobID, jobID)
	}
}

// TestHandleStatus_responseShapeStable is a tiny structural pin against
// accidental field renames. The IDE's polling code reads "status" and
// "language" — if those drop or rename, the IDE silently reports
// "unknown" forever. Keeping a pin with the exact JSON keys catches
// that.
func TestHandleStatus_responseShapeStable(t *testing.T) {
	e, _, _ := newCodegenTestServer(t)

	req := jsonRequest(t, http.MethodGet,
		"/api/v1/codegen/jobs/x/status", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`"jobId"`, `"status"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response body missing key %s. got: %s", want, body)
		}
	}
}
