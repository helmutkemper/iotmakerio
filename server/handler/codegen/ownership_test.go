// server/handler/codegen/ownership_test.go — Tests for the ownsJob helper.
//
// We pin the helper's contract independently of the handlers that use it
// because the deny-on-error policy (Redis hiccup → owns=false) is easy
// to break by mistake — a future refactor might propagate the Redis
// error and forget to keep the deny side of the contract.
//
// What we cover here:
//
//   - Empty userID returns (false, errEmptyUserID). The Bearer middleware
//     is supposed to prevent this, but if it gets bypassed by routing
//     misconfiguration, the helper must still deny.
//   - Missing :userId key in Redis returns (false, nil) — no owner
//     recorded means no access.
//   - Matching userID returns (true, nil).
//   - Mismatched userID returns (false, nil) — does NOT return an error,
//     does NOT log; the caller (which has feature-tagged logs) is in
//     charge of how to react.
package codegen

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestOwnsJob_emptyUserID(t *testing.T) {
	_, _, h := newCodegenTestServer(t)

	owns, err := h.ownsJob(context.Background(), "any-job", "")
	if owns {
		t.Errorf("owns should be false for empty userID; got true")
	}
	if !errors.Is(err, errEmptyUserID) {
		t.Errorf("err: got %v, want %v", err, errEmptyUserID)
	}
}

func TestOwnsJob_keyAbsent(t *testing.T) {
	_, _, h := newCodegenTestServer(t)

	// Nothing seeded — the :userId key for this job does not exist.
	owns, err := h.ownsJob(context.Background(), "ghost-job", "anyone")
	if owns {
		t.Errorf("owns should be false when key is absent; got true")
	}
	if err != nil {
		t.Errorf("err should be nil for missing key; got %v", err)
	}
}

func TestOwnsJob_match(t *testing.T) {
	_, mr, h := newCodegenTestServer(t)

	const jobID = "owned-job"
	const owner = "owner-user"
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), owner); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}

	owns, err := h.ownsJob(context.Background(), jobID, owner)
	if !owns {
		t.Errorf("owns should be true when userID matches recorded owner; got false")
	}
	if err != nil {
		t.Errorf("err should be nil on match; got %v", err)
	}
}

func TestOwnsJob_mismatch(t *testing.T) {
	_, mr, h := newCodegenTestServer(t)

	const jobID = "someones-job"
	const owner = "real-owner"
	if err := mr.Set(fmt.Sprintf("codegen:job:%s:userId", jobID), owner); err != nil {
		t.Fatalf("seed :userId: %v", err)
	}

	// A different user asks. The helper must say "no" but must NOT
	// surface an error — mismatch is not an exception, it's the
	// normal answer to "is this person the owner?".
	owns, err := h.ownsJob(context.Background(), jobID, "attacker")
	if owns {
		t.Errorf("owns should be false on mismatch; got true")
	}
	if err != nil {
		t.Errorf("err should be nil on mismatch (not an error case); got %v", err)
	}
}
