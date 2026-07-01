// server/store/project_help_files_test.go — Tests for the help-files
// CRUD layer.
//
// Each test uses a fresh in-memory SQLite database so they are isolated
// and parallel-safe. The tests do NOT exercise path validation or MIME
// mapping — those live in the handler layer (see
// server/handler/projectapi/help_files_test.go).
package store

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// ─── Test scaffolding ─────────────────────────────────────────────────────────

// setupHelpFilesTestSchema spins up an in-memory SQLite database, installs
// just the tables this test file needs, seeds two users / three projects,
// and assigns it to the package-level DB variable.
//
// Reassigning DB is safe because tests run sequentially within the package
// (we don't call t.Parallel) and each test calls this helper from scratch.
func setupHelpFilesTestSchema(t *testing.T) {
	t.Helper()

	// In-memory SQLite database with foreign-key support enabled.
	// "?_pragma=foreign_keys(1)" turns on FK enforcement, which is
	// off by default in SQLite. We need it for the cascade-delete
	// test.
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	DB = db

	stmts := []string{
		// Minimal users + projects schema for the FK joins.
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL
		)`,
	}
	stmts = append(stmts, helpFilesMigrationStmts()...)
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			t.Fatalf("create schema: %v\n%s", err, s)
		}
	}
	// One user with two projects, plus a second user with one project,
	// so the per-user quota tests can verify scoping.
	for _, q := range []string{
		`INSERT INTO users (id) VALUES ('u1')`,
		`INSERT INTO users (id) VALUES ('u2')`,
		`INSERT INTO projects (id, user_id) VALUES ('p1', 'u1')`,
		`INSERT INTO projects (id, user_id) VALUES ('p2', 'u1')`,
		`INSERT INTO projects (id, user_id) VALUES ('p3', 'u2')`,
	} {
		if _, err := DB.Exec(q); err != nil {
			t.Fatalf("seed: %v: %s", err, q)
		}
	}
}

// ─── Save / Get round-trip ────────────────────────────────────────────────────

func TestHelpFile_SaveGetRoundTrip(t *testing.T) {
	setupHelpFilesTestSchema(t)
	body := []byte("# Hello\n\nworld")
	if err := SaveHelpFile("p1", "readme.en.md", "text/markdown; charset=utf-8", body); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hf, err := GetHelpFile("p1", "readme.en.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hf.ProjectID != "p1" {
		t.Errorf("project: got %q want p1", hf.ProjectID)
	}
	if string(hf.Content) != string(body) {
		t.Errorf("content roundtrip: got %q", hf.Content)
	}
	if hf.SizeBytes != int64(len(body)) {
		t.Errorf("size: got %d want %d", hf.SizeBytes, len(body))
	}
	if hf.UpdatedAt.IsZero() {
		t.Error("updated_at: zero value")
	}
}

func TestHelpFile_GetMissing(t *testing.T) {
	setupHelpFilesTestSchema(t)
	_, err := GetHelpFile("p1", "missing.md")
	if err != ErrNoHelpFile {
		t.Errorf("expected ErrNoHelpFile, got %v", err)
	}
}

// ─── Save upsert ──────────────────────────────────────────────────────────────

func TestHelpFile_SaveUpsertReplacesContent(t *testing.T) {
	setupHelpFilesTestSchema(t)
	if err := SaveHelpFile("p1", "readme.en.md", "text/markdown; charset=utf-8", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := SaveHelpFile("p1", "readme.en.md", "text/markdown; charset=utf-8", []byte("second longer")); err != nil {
		t.Fatal(err)
	}
	hf, _ := GetHelpFile("p1", "readme.en.md")
	if string(hf.Content) != "second longer" {
		t.Errorf("upsert content: got %q", hf.Content)
	}
	if hf.SizeBytes != 13 {
		t.Errorf("upsert size: got %d want 13", hf.SizeBytes)
	}
}

// ─── List ─────────────────────────────────────────────────────────────────────

func TestHelpFile_List(t *testing.T) {
	setupHelpFilesTestSchema(t)
	files := []struct{ path, body string }{
		{"readme.en.md", "x"},
		{"readme.pt-br.md", "yy"},
		{"examples/foo.png", "zzzz"},
	}
	for _, f := range files {
		if err := SaveHelpFile("p1", f.path, "text/markdown; charset=utf-8", []byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListHelpFiles("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("List: got %d entries, want 3", len(got))
	}
	// Lexicographic order: "examples/" before "readme.*" because '/'
	// (0x2F) < 'r' (0x72) only when the first chars differ; but
	// 'e' < 'r', so examples/ files come first alphabetically.
	if got[0].Path != "examples/foo.png" {
		t.Errorf("List order: got first %q, want examples/foo.png", got[0].Path)
	}
	// Content blob should NOT be present in the list response (it is
	// HelpFileMeta, not HelpFile).
	if got[0].SizeBytes != 4 {
		t.Errorf("List size: got %d want 4", got[0].SizeBytes)
	}
}

func TestHelpFile_ListEmptyReturnsNonNilSlice(t *testing.T) {
	setupHelpFilesTestSchema(t)
	got, err := ListHelpFiles("p1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("List: expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("List on empty project: got %d entries, want 0", len(got))
	}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestHelpFile_Delete(t *testing.T) {
	setupHelpFilesTestSchema(t)
	_ = SaveHelpFile("p1", "readme.en.md", "text/markdown; charset=utf-8", []byte("hi"))

	deleted, err := DeleteHelpFile("p1", "readme.en.md")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("Delete: returned false on existing row")
	}

	// Idempotent: a second delete on the same path returns false but
	// no error.
	deleted2, err := DeleteHelpFile("p1", "readme.en.md")
	if err != nil {
		t.Fatal(err)
	}
	if deleted2 {
		t.Error("Delete: second call returned true (expected false)")
	}
}

// ─── Rename ───────────────────────────────────────────────────────────────────

func TestHelpFile_Rename(t *testing.T) {
	setupHelpFilesTestSchema(t)
	_ = SaveHelpFile("p1", "readme.en.md", "text/markdown; charset=utf-8", []byte("hi"))

	if err := RenameHelpFile("p1", "readme.en.md", "overview.en.md", "text/markdown; charset=utf-8"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	// Old gone, new there.
	if _, err := GetHelpFile("p1", "readme.en.md"); err != ErrNoHelpFile {
		t.Errorf("after rename: old path still exists, err=%v", err)
	}
	hf, err := GetHelpFile("p1", "overview.en.md")
	if err != nil {
		t.Fatalf("after rename: new path missing: %v", err)
	}
	if string(hf.Content) != "hi" {
		t.Errorf("content lost in rename: got %q", hf.Content)
	}
}

func TestHelpFile_RenameMissing(t *testing.T) {
	setupHelpFilesTestSchema(t)
	err := RenameHelpFile("p1", "ghost.md", "real.md", "text/markdown; charset=utf-8")
	if err != ErrNoHelpFile {
		t.Errorf("expected ErrNoHelpFile, got %v", err)
	}
}

func TestHelpFile_RenameConflict(t *testing.T) {
	setupHelpFilesTestSchema(t)
	_ = SaveHelpFile("p1", "a.md", "text/markdown; charset=utf-8", []byte("a"))
	_ = SaveHelpFile("p1", "b.md", "text/markdown; charset=utf-8", []byte("b"))

	err := RenameHelpFile("p1", "a.md", "b.md", "text/markdown; charset=utf-8")
	if err != ErrHelpPathConflict {
		t.Errorf("expected ErrHelpPathConflict, got %v", err)
	}
	// Both files must still exist with original contents.
	a, _ := GetHelpFile("p1", "a.md")
	b, _ := GetHelpFile("p1", "b.md")
	if string(a.Content) != "a" || string(b.Content) != "b" {
		t.Error("conflicted rename mutated either file")
	}
}

func TestHelpFile_RenameSelfIsNoOp(t *testing.T) {
	setupHelpFilesTestSchema(t)
	_ = SaveHelpFile("p1", "a.md", "text/markdown; charset=utf-8", []byte("a"))
	if err := RenameHelpFile("p1", "a.md", "a.md", "text/markdown; charset=utf-8"); err != nil {
		t.Errorf("self-rename: %v", err)
	}
}

// ─── Sums / quotas ────────────────────────────────────────────────────────────

func TestHelpFile_SumProjectBytes(t *testing.T) {
	setupHelpFilesTestSchema(t)
	if got, _ := SumProjectBytes("p1"); got != 0 {
		t.Errorf("empty project sum: got %d want 0", got)
	}
	_ = SaveHelpFile("p1", "a.md", "text/markdown; charset=utf-8", []byte("12345"))
	_ = SaveHelpFile("p1", "b.md", "text/markdown; charset=utf-8", []byte("xyz"))
	if got, _ := SumProjectBytes("p1"); got != 8 {
		t.Errorf("sum: got %d want 8", got)
	}
}

func TestHelpFile_SumUserBytes(t *testing.T) {
	setupHelpFilesTestSchema(t)
	// p1 and p2 belong to u1; p3 belongs to u2. The sum for u1 must
	// include p1 and p2 only.
	_ = SaveHelpFile("p1", "a.md", "text/markdown; charset=utf-8", []byte("aaaaa"))
	_ = SaveHelpFile("p2", "b.md", "text/markdown; charset=utf-8", []byte("bbb"))
	_ = SaveHelpFile("p3", "c.md", "text/markdown; charset=utf-8", []byte("ccccccc"))

	got, err := SumUserBytes("u1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 8 {
		t.Errorf("user u1 sum: got %d want 8", got)
	}
	got2, _ := SumUserBytes("u2")
	if got2 != 7 {
		t.Errorf("user u2 sum: got %d want 7", got2)
	}
}

// ─── Foreign-key cascade ──────────────────────────────────────────────────────

func TestHelpFile_DeleteCascadeFromProject(t *testing.T) {
	setupHelpFilesTestSchema(t)
	_ = SaveHelpFile("p1", "a.md", "text/markdown; charset=utf-8", []byte("hi"))
	if _, err := DB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`DELETE FROM projects WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := GetHelpFile("p1", "a.md"); err != ErrNoHelpFile {
		t.Errorf("cascade: file still present after project delete: %v", err)
	}
}

// ─── Sanity: large content ────────────────────────────────────────────────────

func TestHelpFile_LargeBlobRoundTrip(t *testing.T) {
	setupHelpFilesTestSchema(t)
	body := []byte(strings.Repeat("X", 100_000))
	if err := SaveHelpFile("p1", "big.md", "text/markdown; charset=utf-8", body); err != nil {
		t.Fatal(err)
	}
	hf, err := GetHelpFile("p1", "big.md")
	if err != nil {
		t.Fatal(err)
	}
	if hf.SizeBytes != 100_000 {
		t.Errorf("large size: got %d", hf.SizeBytes)
	}
	if len(hf.Content) != 100_000 || hf.Content[0] != 'X' || hf.Content[99_999] != 'X' {
		t.Error("large content corrupted in round-trip")
	}
}
