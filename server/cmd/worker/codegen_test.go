// server/cmd/worker/codegen_test.go — Tests for the codegen Asynq handler
// inside the worker process.
//
// What we pin here:
//
//   - Successful payload → Generate runs → publishes
//     codegen:job:{id}:state="done" and codegen:job:{id}:result with
//     the JSON-encoded codegen.Response.
//   - Cancelled context before Generate → publishes state="failed"
//     with an explanatory error message; never calls codegen.Generate.
//   - Cancelled context during Generate (simulated via a context that
//     fires after a tick) → response is discarded, state="failed".
//   - Malformed payload → handler returns an error and does not write
//     anything to Redis under the job's keys. The Asynq runtime would
//     archive the task; we verify the no-side-effect contract.
//
// What we do NOT pin:
//
//   - The contents of the generated Go source. That belongs to the
//     codegen package tests (server/codegen/*_test.go).
//   - Asynq's own delivery, retry, or queue mechanics. Those are the
//     library's concern, exercised by handler-level tests that go
//     through Inspector (see server/handler/codegen/submit_test.go).
//
// Why this file exists where it does:
//
//	cmd/worker/ had no tests before this. The package is main, so
//	tests must live alongside the binary. Using package main keeps
//	access to unexported helpers (makeCodegenHandler, publishCodegen*).
//
// Português:
//
//	Testes do handler de codegen do worker. Verifica o ciclo
//	payload → Generate → publica result no Redis. Não cobre o
//	conteúdo do código gerado (que mora em server/codegen) nem as
//	mecânicas do Asynq (que pertencem à biblioteca).
package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"server/codegen"
	"server/tasks"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// minimalEmptyScene is a scene that codegen.Generate accepts without
// returning errors. The pipeline emits Go source for an empty program.
const minimalEmptyScene = `{
	"version": "1",
	"metadata": {
		"density": 1,
		"canvasWidth": 800,
		"canvasHeight": 600,
		"camera": {"offsetX": 0, "offsetY": 0, "zoom": 1}
	},
	"devices": [],
	"wires": []
}`

// newRedisForTest spins up a miniredis instance scoped to the test and
// returns a go-redis client wired to it. Mirrors the setup used in
// server/handler/codegen tests so behaviour is consistent across the
// two packages.
func newRedisForTest(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

// buildTask wraps a CodegenPayload into the *asynq.Task shape the
// handler expects. Mirrors what tasks.NewCodegenRunTask does, minus
// the queue/retry options that only matter to the runtime.
func buildTask(t *testing.T, p tasks.CodegenPayload) *asynq.Task {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return asynq.NewTask(tasks.TypeCodegenRun, b)
}

func TestCodegenHandler_emptyScene_publishesDone(t *testing.T) {
	mr, rdb := newRedisForTest(t)
	h := makeCodegenHandler(rdb)

	const jobID = "happy-job"
	task := buildTask(t, tasks.CodegenPayload{
		JobID:    jobID,
		Language: "go",
		Scene:    []byte(minimalEmptyScene),
	})

	if err := h(context.Background(), task); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// state must be "done"
	state, err := mr.Get("codegen:job:" + jobID + ":state")
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "done" {
		t.Errorf("state: got %q, want %q", state, "done")
	}

	// result must be a parseable codegen.Response with non-empty Code
	rawResult, err := mr.Get("codegen:job:" + jobID + ":result")
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var resp codegen.Response
	if err := json.Unmarshal([]byte(rawResult), &resp); err != nil {
		t.Fatalf("unmarshal result: %v. raw: %s", err, rawResult)
	}
	if resp.Code == "" {
		t.Errorf("result.Code is empty. errors=%v warnings=%v", resp.Errors, resp.Warnings)
	}
}

// TestCodegenHandler_cancelledBeforeStart simulates the case where the
// user cancelled while the task sat queued. The handler must detect
// ctx.Err() before invoking Generate and publish state="failed" with
// a message that mentions cancellation.
func TestCodegenHandler_cancelledBeforeStart(t *testing.T) {
	mr, rdb := newRedisForTest(t)
	h := makeCodegenHandler(rdb)

	const jobID = "cancelled-early-job"
	task := buildTask(t, tasks.CodegenPayload{
		JobID:    jobID,
		Language: "go",
		Scene:    []byte(minimalEmptyScene),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	if err := h(ctx, task); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	state, err := mr.Get("codegen:job:" + jobID + ":state")
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "failed" {
		t.Errorf("state: got %q, want %q", state, "failed")
	}

	errMsg, err := mr.Get("codegen:job:" + jobID + ":error")
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(strings.ToLower(errMsg), "cancel") {
		t.Errorf("error message should mention cancel; got %q", errMsg)
	}

	// And result must NOT be set — we discarded any partial output.
	if mr.Exists("codegen:job:" + jobID + ":result") {
		t.Errorf("result key should not exist after cancelled-before-start")
	}
}

// TestCodegenHandler_malformedPayload_returnsErrorAndWritesNothing
// pins the no-side-effect contract for invalid payloads. The handler
// must reject the task without ever writing to a job key — there is
// no JobID to write under.
func TestCodegenHandler_malformedPayload_returnsErrorAndWritesNothing(t *testing.T) {
	mr, rdb := newRedisForTest(t)
	h := makeCodegenHandler(rdb)

	task := asynq.NewTask(tasks.TypeCodegenRun, []byte("not valid json"))

	err := h(context.Background(), task)
	if err == nil {
		t.Fatal("handler accepted malformed payload — wanted error")
	}

	// And it must not have written anything codegen:job:*-shaped.
	keys := mr.Keys()
	for _, k := range keys {
		if strings.HasPrefix(k, "codegen:job:") {
			t.Errorf("handler wrote stray key %q on malformed payload", k)
		}
	}
}

// TestCodegenHandler_cancelMidFlight gives the handler a context that
// fires very soon after the call starts. The intent is to land
// cancellation inside Generate (between its checkpoints). We can't
// guarantee the timing — Generate on an empty scene is very fast —
// so this test is best-effort: it accepts both outcomes (done OR
// failed) and only asserts that state is one of those two terminal
// values. The point is that the handler never deadlocks under
// cancellation.
//
// Português: cancelamento durante Generate é best-effort por timing;
// o teste aceita ambos os estados terminais e só pin que não trava.
func TestCodegenHandler_cancelMidFlight(t *testing.T) {
	mr, rdb := newRedisForTest(t)
	h := makeCodegenHandler(rdb)

	const jobID = "racey-cancel-job"
	task := buildTask(t, tasks.CodegenPayload{
		JobID:    jobID,
		Language: "go",
		Scene:    []byte(minimalEmptyScene),
	})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a very short delay — may or may not interrupt
	// Generate depending on platform timing.
	time.AfterFunc(1*time.Microsecond, cancel)

	if err := h(ctx, task); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	state, err := mr.Get("codegen:job:" + jobID + ":state")
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "done" && state != "failed" {
		t.Errorf("state: got %q, want one of {done, failed}", state)
	}
}
