// server/handler/projectapi/code_files_test.go — Validation tests for
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// the snapshot contract that gates what gets stored as a project's
// source code.
//
// Why this file is codegen-relevant:
//
//	The multi-file save contract (validateCodeFileSet) is the ONE gate
//	shared by save, upload and rename: paths become ZIP keys and
//	#include operands downstream, and the extension whitelist is what
//	keeps a Go project Go and a C project C. These tests pin the gate
//	as a PURE FUNCTION — no echo, no DB — because the language-aware
//	rules need the project row, so the handler can only validate AFTER
//	the ownership lookup; unit-testing through the handler would need a
//	seeded database for every row of the table.
//
//	The two malformed-JSON tests still go through the handlers: bind
//	failures are the one validation that genuinely happens before any
//	DB touch.
//
// Português: O contrato do snapshot (validateCodeFileSet) é o portão
// único de save/upload/rename, testado aqui como FUNÇÃO PURA — as
// regras dependem da linguagem do projeto, então o handler só valida
// após o ownership; testar via handler exigiria banco semeado por
// linha da tabela. Os testes de JSON malformado continuam via handler.
package projectapi

import (
	"net/http"
	"strings"
	"testing"

	"server/store"
)

// ─── validateCodeFileSet — the snapshot contract as a table ───────────────────

func TestValidateCodeFileSet(t *testing.T) {
	f := func(path, content string) store.CodeFileEntry {
		return store.CodeFileEntry{Path: path, Content: content}
	}

	cases := []struct {
		name    string
		files   []store.CodeFileEntry
		lang    string
		wantSub string // "" → valid
	}{
		// Happy paths — one per language shape.
		{"go single file", []store.CodeFileEntry{f("main.go", "package main")}, "golang", ""},
		{"c header + sources, nested", []store.CodeFileEntry{
			f("api.h", "typedef int t;"), f("core.c", "int a;"), f("util/helpers.c", "int b;"),
		}, "c", ""},

		// Count rules.
		{"empty set", nil, "golang", "files is required"},
		{"too many files", func() []store.CodeFileEntry {
			out := make([]store.CodeFileEntry, maxCodeFiles+1)
			for i := range out {
				out[i] = f("f"+strings.Repeat("x", i)+".c", "int a;")
			}
			return out
		}(), "c", "too many files"},

		// Path spelling — each is a downstream hazard, not taste.
		{"empty path", []store.CodeFileEntry{f("", "x")}, "c", "needs a path"},
		{"absolute path", []store.CodeFileEntry{f("/etc/evil.c", "x")}, "c", "must be relative"},
		{"backslash", []store.CodeFileEntry{f(`sub\evil.c`, "x")}, "c", "must be relative"},
		{"dotdot segment", []store.CodeFileEntry{f("../evil.c", "x")}, "c", "invalid path segment"},
		{"hidden dotfile", []store.CodeFileEntry{f(".secret.c", "x")}, "c", "invalid path segment"},
		{"illegal char", []store.CodeFileEntry{f("ma*in.c", "x")}, "c", "invalid path segment"},
		{"too deep", []store.CodeFileEntry{f("a/b/c/d/e.c", "x")}, "c", "path too deep"},
		{"case-insensitive duplicate", []store.CodeFileEntry{
			f("Util.c", "x"), f("util.c", "y"),
		}, "c", "duplicate path"},

		// Extension-per-language — the rule that killed the .go stamp.
		{"go rejects .py", []store.CodeFileEntry{f("main.py", "x")}, "golang", "only .go"},
		{"go rejects .c", []store.CodeFileEntry{f("main.c", "x")}, "golang", "only .go"},
		{"c rejects .go", []store.CodeFileEntry{f("main.go", "x")}, "c", "only .c and .h"},
		{"go multi-file is a future slice", []store.CodeFileEntry{
			f("a.go", "package a"), f("b.go", "package a"),
		}, "golang", "single-file for now"},
		{"c headers alone", []store.CodeFileEntry{f("api.h", "typedef int t;")}, "c", "at least one .c"},

		// Content rule — the single-file era's "source is required".
		{"all blank", []store.CodeFileEntry{f("main.go", "   \n\t")}, "golang", "must have content"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateCodeFileSet(tc.files, tc.lang)
			if tc.wantSub == "" {
				if got != "" {
					t.Fatalf("want valid, got %q", got)
				}
				return
			}
			if !strings.Contains(strings.ToLower(got), strings.ToLower(tc.wantSub)) {
				t.Fatalf("message %q missing %q", got, tc.wantSub)
			}
		})
	}
}

// ─── Malformed JSON — the one pre-DB validation left in the handlers ──────────

func TestHandleSaveCodeVersion_malformedJSON(t *testing.T) {
	c, rec := newRawJSONContext(t, http.MethodPost,
		"/api/v1/projects/some-id/files/code/versions", `{"files":`)
	c.SetParamNames("id")
	c.SetParamValues("some-id")

	if err := handleSaveCodeVersion(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if msg := strings.ToLower(decodeFailBody(t, rec)); !strings.Contains(msg, "invalid request body") {
		t.Errorf("error message missing 'invalid request body'; got %q", msg)
	}
}

func TestHandleRenameCodeFile_malformedJSON(t *testing.T) {
	c, rec := newRawJSONContext(t, http.MethodPut,
		"/api/v1/projects/some-id/files/code/rename", `{"newName":`)
	c.SetParamNames("id")
	c.SetParamValues("some-id")

	if err := handleRenameCodeFile(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if msg := strings.ToLower(decodeFailBody(t, rec)); !strings.Contains(msg, "invalid request body") {
		t.Errorf("error message missing 'invalid request body'; got %q", msg)
	}
}

// TestHandleRenameCodeFile_missingNewName pins the one field check that
// still fires before the ownership lookup.
func TestHandleRenameCodeFile_missingNewName(t *testing.T) {
	for _, body := range []map[string]any{{}, {"newName": "   "}} {
		c, rec := newJSONContext(t, http.MethodPut,
			"/api/v1/projects/some-id/files/code/rename", body)
		c.SetParamNames("id")
		c.SetParamValues("some-id")

		if err := handleRenameCodeFile(c); err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if msg := strings.ToLower(decodeFailBody(t, rec)); !strings.Contains(msg, "newname is required") {
			t.Errorf("error message missing 'newname is required'; got %q", msg)
		}
	}
}
