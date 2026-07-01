// server/handler/projectapi/create_project_integration_test.go —
// Integration tests for handleCreateProject that need a live SQLite
// database and a real Echo middleware chain.
//
// Why this file is codegen-relevant:
//
//	handleCreateProject is the gatekeeper that decides which
//	programming language a project targets. The codegen pipeline
//	(server/codegen) currently only emits Go, so today the contract is
//	"create accepts a programmingLanguageId that exists in the
//	programming_languages table". These tests pin both halves of that
//	contract — the DB-validated reject paths and the happy path that
//	persists the language ID.
//
// What's covered here that the unit tests can't reach:
//
//   - Happy path: 201, full Project response, DB row, on-disk dirs,
//     auto-generated readme.md.
//   - 409 conflict on duplicate (user, name).
//   - 400 "invalid programmingLanguageId" — the post-empty-check path
//     that runs store.ValidateProgrammingLanguageID against the DB.
//   - 400 "uiLanguageId is required" — unreachable in the unit tests
//     because programmingLanguageId must validate against the DB
//     first.
//   - 400 "invalid uiLanguageId" — same DB-dependent reason.
//   - Cross-user isolation: user A creating a project does not
//     pollute user B's project list.
package projectapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"server/config"
	"server/store"
)

// ─── Happy path ───────────────────────────────────────────────────────────────

func TestHandleCreateProject_happyPath(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-happy-path"
	token := newTestUserToken(t, userID)

	body := map[string]any{
		"name":                  "My First Project",
		"type":                  store.ProjectTypeCustomDevice,
		"programmingLanguageId": "golang",
		"uiLanguageId":          "en",
	}
	req := authedJSONRequest(t, http.MethodPost, "/api/v1/projects", token, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got store.Project
	decodeOKData(t, rec, &got)

	// Verify the response carries everything the frontend needs.
	if got.ID == "" {
		t.Errorf("response.id is empty")
	}
	if got.UserID != userID {
		t.Errorf("response.userId: got %q, want %q", got.UserID, userID)
	}
	if got.Name != "My First Project" {
		t.Errorf("response.name: got %q, want %q", got.Name, "My First Project")
	}
	if got.Visibility != store.ProjectVisibilityPrivate {
		t.Errorf("response.visibility: got %q, want %q (server forces private)",
			got.Visibility, store.ProjectVisibilityPrivate)
	}
	if got.ProgrammingLanguageID != "golang" {
		t.Errorf("response.programmingLanguageId: got %q, want %q",
			got.ProgrammingLanguageID, "golang")
	}
	if got.UILanguageID != "en" {
		t.Errorf("response.uiLanguageId: got %q, want %q", got.UILanguageID, "en")
	}
	if got.PublishToFeed || got.PublishToSearch || got.ReadyToUse {
		t.Errorf("publish flags should be false on a freshly created project; got %+v",
			got)
	}

	// Verify the row landed in the DB.
	stored, err := store.GetProjectByIDAndUser(got.ID, userID)
	if err != nil {
		t.Fatalf("GetProjectByIDAndUser: %v", err)
	}
	if stored.Name != got.Name {
		t.Errorf("DB row name: got %q, want %q", stored.Name, got.Name)
	}

	// Verify the on-disk layout.
	cfg := config.Get()
	base := projectBasePath(cfg, userID, got.Type, got.ID)
	for _, sub := range []string{
		store.ProjectFileSectionCode,
		store.ProjectFileSectionImg,
		store.ProjectFileSectionDocs,
	} {
		dir := filepath.Join(base, sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected dir %q to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %q to be a directory", dir)
		}
	}

	// Verify the auto-generated readme.md exists and has the expected
	// frontmatter scaffold (this is the bridge between the project and
	// the marketplace card; it also doubles as a smoke test that the
	// readme template hasn't gone empty).
	readmePath := filepath.Join(base, store.ProjectFileSectionDocs, projectReadmeFilename)
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("readme.md: %v", err)
	}
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("readme.md missing frontmatter delimiter; first 80 bytes: %q",
			string(content[:min(80, len(content))]))
	}
	if !strings.Contains(string(content), "title: My First Project") {
		t.Errorf("readme.md missing title from frontmatter; got: %q", string(content))
	}
}

// ─── Conflict on duplicate name ───────────────────────────────────────────────

func TestHandleCreateProject_conflictOnDuplicateName(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-conflict"
	token := newTestUserToken(t, userID)

	body := map[string]any{
		"name":                  "Duplicate Project",
		"type":                  store.ProjectTypeCustomDevice,
		"programmingLanguageId": "golang",
		"uiLanguageId":          "en",
	}

	// First create — should succeed.
	req := authedJSONRequest(t, http.MethodPost, "/api/v1/projects", token, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: got %d, want %d. body: %s",
			rec.Code, http.StatusCreated, rec.Body.String())
	}

	// Second create with the same name — should 409.
	req = authedJSONRequest(t, http.MethodPost, "/api/v1/projects", token, body)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want %d. body: %s",
			rec.Code, http.StatusConflict, rec.Body.String())
	}
	msg := strings.ToLower(decodeFailBody(t, rec))
	if !strings.Contains(msg, "already exists") {
		t.Errorf("conflict error message missing 'already exists'; got %q", msg)
	}
}

// ─── DB-validated rejection: programmingLanguageId not in the table ──────────

func TestHandleCreateProject_invalidProgrammingLanguageId(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-invalid-lang"
	token := newTestUserToken(t, userID)

	body := map[string]any{
		"name":                  "Project With Bad Lang",
		"type":                  store.ProjectTypeCustomDevice,
		"programmingLanguageId": "this-language-does-not-exist",
		"uiLanguageId":          "en",
	}
	req := authedJSONRequest(t, http.MethodPost, "/api/v1/projects", token, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	msg := strings.ToLower(decodeFailBody(t, rec))
	if !strings.Contains(msg, "invalid programminglanguageid") {
		t.Errorf("error message missing 'invalid programmingLanguageId'; got %q", msg)
	}
}

// ─── DB-validated rejection: uiLanguageId empty (only reachable past the
//     programmingLanguageId DB validation) ────────────────────────────────────

func TestHandleCreateProject_uiLanguageIdRequired(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-no-ui-lang"
	token := newTestUserToken(t, userID)

	body := map[string]any{
		"name":                  "Project Missing UI Lang",
		"type":                  store.ProjectTypeCustomDevice,
		"programmingLanguageId": "golang",
		// uiLanguageId omitted on purpose — handler must 400 here, not 500.
	}
	req := authedJSONRequest(t, http.MethodPost, "/api/v1/projects", token, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	msg := strings.ToLower(decodeFailBody(t, rec))
	if !strings.Contains(msg, "uilanguageid is required") {
		t.Errorf("error message missing 'uiLanguageId is required'; got %q", msg)
	}
}

// ─── DB-validated rejection: uiLanguageId not in the table ────────────────────

func TestHandleCreateProject_invalidUILanguageId(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-bad-ui-lang"
	token := newTestUserToken(t, userID)

	body := map[string]any{
		"name":                  "Project With Bad UI Lang",
		"type":                  store.ProjectTypeCustomDevice,
		"programmingLanguageId": "golang",
		"uiLanguageId":          "klingon",
	}
	req := authedJSONRequest(t, http.MethodPost, "/api/v1/projects", token, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	msg := strings.ToLower(decodeFailBody(t, rec))
	if !strings.Contains(msg, "invalid uilanguageid") {
		t.Errorf("error message missing 'invalid uiLanguageId'; got %q", msg)
	}
}

// ─── Cross-user isolation ─────────────────────────────────────────────────────
//
// The "user_id is the scoping key" claim from routes.go must be true at
// the GET layer too: a project created by user A must NOT show up in
// user B's list. This catches a hypothetical regression where a
// missing WHERE clause leaks projects across owners.

func TestHandleCreateProject_isolatesAcrossUsers(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userA = "u-alice"
	const userB = "u-bob"
	tokenA := newTestUserToken(t, userA)
	tokenB := newTestUserToken(t, userB)

	body := map[string]any{
		"name":                  "Alice's Project",
		"type":                  store.ProjectTypeCustomDevice,
		"programmingLanguageId": "golang",
		"uiLanguageId":          "en",
	}
	req := authedJSONRequest(t, http.MethodPost, "/api/v1/projects", tokenA, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("alice create: got %d, want 201. body: %s", rec.Code, rec.Body.String())
	}

	// Bob lists his projects and must see exactly zero.
	req = authedJSONRequest(t, http.MethodGet, "/api/v1/projects", tokenB, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob list: got %d, want 200. body: %s", rec.Code, rec.Body.String())
	}

	var bobProjects []store.Project
	decodeOKData(t, rec, &bobProjects)
	if len(bobProjects) != 0 {
		t.Errorf("bob saw %d projects, want 0. projects: %s",
			len(bobProjects), mustJSON(t, bobProjects))
	}
}

// mustJSON is a tiny helper for diagnostic error messages.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		return "<marshal-failed>"
	}
	return string(b)
}
