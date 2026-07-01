// /ide/server/handler/blackboxapi/handler_test.go — Unit tests for blackboxapi helpers.
//
// Purpose:
//
//	Exercises the pure helper logic declared in handler.go so the on-wire
//	ownership contract is covered without standing up a database or an
//	Echo context. This endpoint has a much simpler ownership story than
//	the template endpoint — every item is always "own" because the query
//	is caller-filtered — so the test surface is correspondingly smaller.
//
// Convention:
//
//	Follows the same style as templateapi/handlers_test.go. See that file
//	for the rationale behind the "TestOwnershipConstantsAreStable" pattern.
package blackboxapi

import "testing"

// TestOwnershipConstantsAreStable guards the on-wire contract.
//
// The WASM client mirrors this exact string in /ide/blackbox/clientTypes.go.
// A casual rename here would not break compilation but would silently break
// "My Items" on every deployed WASM binary until all browsers hard-refreshed.
// This test keeps the value locked.
//
// If the on-wire contract intentionally changes, update the expected value
// in this test AND every mirror constant on the WASM side AS A SINGLE COMMIT.
//
// Note: unlike templateapi, this package only exposes "own" — "public" and
// "curated" never appear here because:
//
//   - /api/v1/blackbox is caller-filtered at the store layer, so everything
//     it returns is, by construction, "own".
//   - Curated devices reach the client through the menu tree endpoint, and
//     the "curated" marker is stamped on the client side in
//     stageWorkspace/workspace.go (extractEmbeddedDefs). See the Phase 1
//     design doc at /ide/docs/tasks/REFACTOR_MY_ITEMS_PHASE_1.md for the
//     rationale behind that client-side stamping.
func TestOwnershipConstantsAreStable(t *testing.T) {
	if originOwn != "own" {
		t.Errorf("originOwn drifted from on-wire contract: got %q, want %q", originOwn, "own")
	}
}
