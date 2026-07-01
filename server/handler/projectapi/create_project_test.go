// server/handler/projectapi/create_project_test.go — Validation tests
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// for handleCreateProject.
//
// Why this file is codegen-relevant:
//
//	The codegen pipeline (server/codegen) branches on req.Language and
//	currently accepts only "go" (or empty). The HTTP boundary that
//	persists which language a project targets is handleCreateProject —
//	specifically the programmingLanguageId field. These tests pin the
//	validation rules that decide which IDs the create endpoint accepts
//	BEFORE the value is ever stored or handed to codegen.
//
// Scope:
//
//	Pure validation paths only — the cases where the handler returns a
//	400 BEFORE reaching the database. Cases that depend on the DB
//	(e.g. "invalid programmingLanguageId" via store.ValidateProgrammingLanguageID,
//	the success path, conflict on duplicate name) belong in an
//	integration test that seeds programming_languages and project_ui_languages.
//
//	Note on uiLanguageId: the empty-uiLanguageId branch is unreachable
//	without a valid programmingLanguageId, because the programming
//	language is validated against the DB BEFORE the UI language empty
//	check runs. This is documented as a TODO for the integration pass.
package projectapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// newJSONContext builds an echo.Context with a JSON request body and a
// fresh response recorder. It does NOT set BearerClaims; handlers that
// call spaauth.BearerClaims see the zero-value claims (UserID == ""),
// which is fine for paths that 400 before any user-scoped DB work.
func newJSONContext(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)
	return c, rec
}

// newRawJSONContext is like newJSONContext but takes a literal string,
// allowing tests to send malformed JSON.
func newRawJSONContext(t *testing.T, method, path, raw string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(raw))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	return c, rec
}

// decodeFailBody pulls the human-readable error message out of the
// standard fail() envelope: {"metadata":{"status":N,"error":"..."},"data":null}.
// Returns "" if the body has the wrong shape — tests treat that as
// "missing error", which is itself a useful failure signal.
func decodeFailBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Metadata struct {
			Status int    `json:"status"`
			Error  string `json:"error"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		return ""
	}
	return env.Metadata.Error
}

// ─── handleCreateProject — validation paths ───────────────────────────────────

func TestHandleCreateProject_validationFailsBeforeDB(t *testing.T) {
	// Each case represents a request body that should be rejected with
	// a 400 before any database call. The wantErrSubstr is checked as
	// a case-insensitive substring of metadata.error so that minor
	// wording tweaks don't break the test, but a regression that
	// removes the validation entirely will.
	cases := []struct {
		name          string
		body          map[string]any
		wantErrSubstr string
	}{
		{
			name:          "missing name",
			body:          map[string]any{},
			wantErrSubstr: "name is required",
		},
		{
			name:          "blank name (whitespace only)",
			body:          map[string]any{"name": "   "},
			wantErrSubstr: "name is required",
		},
		{
			name: "name longer than 100 characters",
			body: map[string]any{
				"name": strings.Repeat("a", 101),
			},
			wantErrSubstr: "100 characters",
		},
		{
			name:          "name with forward slash",
			body:          map[string]any{"name": "a/b"},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with backslash",
			body:          map[string]any{"name": `a\b`},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with colon",
			body:          map[string]any{"name": "a:b"},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with asterisk",
			body:          map[string]any{"name": "a*b"},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with question mark",
			body:          map[string]any{"name": "a?b"},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with double quote",
			body:          map[string]any{"name": `a"b`},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with angle brackets",
			body:          map[string]any{"name": "a<b>c"},
			wantErrSubstr: "must not contain",
		},
		{
			name:          "name with pipe",
			body:          map[string]any{"name": "a|b"},
			wantErrSubstr: "must not contain",
		},
		{
			name: "unsupported type",
			body: map[string]any{
				"name": "ok",
				"type": "weird_type_that_does_not_exist",
			},
			wantErrSubstr: "unsupported project type",
		},
		{
			name: "missing programmingLanguageId",
			body: map[string]any{
				"name": "ok",
				"type": "custom_device",
				// programmingLanguageId omitted — should 400 before any DB call
			},
			wantErrSubstr: "programminglanguageid is required",
		},
		{
			name: "blank programmingLanguageId",
			body: map[string]any{
				"name":                  "ok",
				"type":                  "custom_device",
				"programmingLanguageId": "",
			},
			wantErrSubstr: "programminglanguageid is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newJSONContext(t, http.MethodPost, "/api/v1/projects", tc.body)

			if err := handleCreateProject(c); err != nil {
				t.Fatalf("handler returned error (expected JSON 400 instead): %v", err)
			}

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want %d. body: %s",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}

			gotMsg := strings.ToLower(decodeFailBody(t, rec))
			if !strings.Contains(gotMsg, strings.ToLower(tc.wantErrSubstr)) {
				t.Errorf("error message missing %q; got %q",
					tc.wantErrSubstr, gotMsg)
			}
		})
	}
}

// TestHandleCreateProject_malformedJSON pins the "invalid request body"
// path: when the JSON parser can't even parse the input, we want a 400
// with the generic message rather than a 500.
func TestHandleCreateProject_malformedJSON(t *testing.T) {
	c, rec := newRawJSONContext(t, http.MethodPost, "/api/v1/projects", `{"name": `)

	if err := handleCreateProject(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if msg := strings.ToLower(decodeFailBody(t, rec)); !strings.Contains(msg, "invalid request body") {
		t.Errorf("error message missing 'invalid request body'; got %q", msg)
	}
}

// TestHandleCreateProject_nameAtBoundaryReachesNextCheck verifies that
// a name of EXACTLY 100 characters does NOT trip the "too long" check.
// We expect the handler to fall through to the next validation
// (programmingLanguageId required) — proving the boundary is inclusive.
func TestHandleCreateProject_nameAtBoundaryReachesNextCheck(t *testing.T) {
	body := map[string]any{
		"name": strings.Repeat("a", 100),
		"type": "custom_device",
		// programmingLanguageId omitted — we expect THAT error, not the name one
	}
	c, rec := newJSONContext(t, http.MethodPost, "/api/v1/projects", body)

	if err := handleCreateProject(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	gotMsg := strings.ToLower(decodeFailBody(t, rec))
	if strings.Contains(gotMsg, "100 characters") {
		t.Errorf("100-character name was rejected as too long; got %q", gotMsg)
	}
	if !strings.Contains(gotMsg, "programminglanguageid") {
		t.Errorf("expected to fall through to programmingLanguageId check; got %q", gotMsg)
	}
}
