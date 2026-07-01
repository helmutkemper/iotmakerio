// /ide/server/handler/templateapi/handlers_test.go — Unit tests for templateapi helpers.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Purpose:
//
//	Exercises the pure helper functions declared in handlers.go so the core
//	ownership logic is covered without standing up a database or an Echo
//	context. Integration-level tests of the full handler still require a
//	live store and are kept out of this file on purpose — this file runs
//	in milliseconds and is safe for CI with the -short flag.
//
// Convention:
//
//	Follows Go's table-driven test style. Each case is a single truth
//	about the helper; adding a new case should never require changing an
//	existing one.
package templateapi

import "testing"

// TestStampTemplateOwnership verifies the ownership helper across every
// combination a real handler can encounter.
//
// The four cases below are the full state space:
//
//  1. Caller owns the template             → ("own",    true)
//  2. Caller does NOT own the template     → ("public", false)
//  3. Caller is anonymous (empty token)    → ("public", false)  — caller "" must never equal pkg ""
//  4. Caller is anonymous AND pkg is empty → ("public", false)  — conservative default
//
// The third and fourth cases together prove the function never returns
// `("own", true)` by accident when both ids are empty strings. That matters
// because in Go `"" == ""` evaluates true, so a naive `pkgUserID == callerID`
// check would misclassify an anonymous request as "owns everything with no
// owner". The helper guards against this with an explicit `callerID != ""`
// check; the test pins that guard in place.
func TestStampTemplateOwnership(t *testing.T) {
	tests := []struct {
		name       string
		pkgUserID  string
		callerID   string
		wantOrigin string
		wantIsOwn  bool
	}{
		{
			name:       "caller owns template",
			pkgUserID:  "user-alice",
			callerID:   "user-alice",
			wantOrigin: originOwn,
			wantIsOwn:  true,
		},
		{
			name:       "caller does not own template",
			pkgUserID:  "user-alice",
			callerID:   "user-bob",
			wantOrigin: originPublic,
			wantIsOwn:  false,
		},
		{
			name:       "anonymous caller sees public template",
			pkgUserID:  "user-alice",
			callerID:   "",
			wantOrigin: originPublic,
			wantIsOwn:  false,
		},
		{
			name:       "anonymous caller and empty owner are NOT a match",
			pkgUserID:  "",
			callerID:   "",
			wantOrigin: originPublic,
			wantIsOwn:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOrigin, gotIsOwn := stampTemplateOwnership(tc.pkgUserID, tc.callerID)
			if gotOrigin != tc.wantOrigin {
				t.Errorf("origin: got %q, want %q", gotOrigin, tc.wantOrigin)
			}
			if gotIsOwn != tc.wantIsOwn {
				t.Errorf("isOwn: got %v, want %v", gotIsOwn, tc.wantIsOwn)
			}
		})
	}
}

// TestOwnershipConstantsAreStable guards the on-wire contract.
//
// The WASM client mirrors these exact strings in
// /ide/templateclient/clientTypes.go and /ide/blackbox/clientTypes.go. A
// casual "let's rename originOwn to originMine" refactor on the server
// would not break compilation but would silently break every deployed
// WASM binary until all browsers hard-refreshed. This test keeps the
// values locked.
//
// If the on-wire contract intentionally changes, update the expected
// values in this test AND every mirror constant on the WASM side AS A
// SINGLE COMMIT.
func TestOwnershipConstantsAreStable(t *testing.T) {
	if originOwn != "own" {
		t.Errorf("originOwn drifted from on-wire contract: got %q, want %q", originOwn, "own")
	}
	if originPublic != "public" {
		t.Errorf("originPublic drifted from on-wire contract: got %q, want %q", originPublic, "public")
	}
}
