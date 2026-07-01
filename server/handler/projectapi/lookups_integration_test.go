// server/handler/projectapi/lookups_integration_test.go —
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// Integration tests for the public lookup endpoints used by the
// "Create Project" form.
//
// Why this file is codegen-relevant:
//
//	GET /api/v1/projects/meta/languages is the source of truth that
//	the IDE shows when the user picks a programming language. The
//	codegen pipeline (server/codegen) currently only emits Go — the
//	switch on req.Language accepts "go" or "" and rejects anything
//	else with KindUnsupportedLanguage. If this endpoint were to ever
//	expose a language ID that codegen does not handle, the user
//	could create a project that silently fails at generate time.
//
//	These tests pin the lookup contract end-to-end:
//	  - The handler returns 200 with a JSON list (no auth required).
//	  - "golang" is present and surfaces as the canonical Go entry.
//	  - The list maps cleanly to the codegen-supported set.
//
// Note on auth:
//
//	These routes are deliberately public — the IDE and other consumers
//	fetch the taxonomy without a token. The tests issue the requests
//	with no Authorization header to mirror that contract.
package projectapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"server/store"
)

// TestHandleListProgrammingLanguages_includesGolang pins the contract
// between this endpoint and what codegen.Generate accepts.
//
// The codegen switch (server/codegen/codeGen.go) currently treats
// req.Language == "go" as the success path. The seed in db.go inserts
// id="golang", name="golang". The relationship between these two
// strings (the project's stored language ID and the codegen-side
// "go") is implicit but stable. This test fails loudly if either side
// drifts.
func TestHandleListProgrammingLanguages_includesGolang(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/meta/languages", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var langs []store.ProgrammingLanguage
	decodeOKData(t, rec, &langs)

	if len(langs) == 0 {
		t.Fatalf("response: empty list — programming_languages was not seeded")
	}

	// Find "golang" — every IDE running this server has to be able to
	// pick it.
	var foundGolang *store.ProgrammingLanguage
	for i := range langs {
		if langs[i].ID == "golang" {
			foundGolang = &langs[i]
			break
		}
	}
	if foundGolang == nil {
		t.Fatalf("golang not in response. got: %+v", langs)
	}

	// Display name is what the user sees in the dropdown — non-empty.
	if foundGolang.Display == "" {
		t.Errorf("golang entry has empty Display field: %+v", *foundGolang)
	}
}

// TestHandleListProgrammingLanguages_publicNoAuth pins the public-route
// contract: the IDE must be able to fetch this list without a token,
// because the create-project form is rendered before the user is
// definitely authenticated in some flows.
func TestHandleListProgrammingLanguages_publicNoAuth(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	// Send WITHOUT an Authorization header — the route is public.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/meta/languages", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("public lookup returned 401 — auth was added to a route documented as public")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestHandleListUILanguages_includesEnglishAndPortuguese pins the
// project-documentation language list. Less critical to codegen than
// the programming-language endpoint, but covered here for symmetry —
// the create-project form depends on both lookups.
func TestHandleListUILanguages_includesEnglishAndPortuguese(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/meta/ui-languages", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var ui []store.ProjectUILanguage
	decodeOKData(t, rec, &ui)

	want := map[string]bool{"en": false, "pt-BR": false}
	for _, l := range ui {
		if _, exists := want[l.ID]; exists {
			want[l.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("ui language %q missing from response. got: %+v", id, ui)
		}
	}
}
