// server/handler/projectapi/help_files_test.go — Tests for path
// validation and MIME whitelist used by the help-files endpoints.
//
// These tests cover the pure helpers (validateHelpFilePath,
// mimeForHelpExt). End-to-end HTTP tests that exercise the full
// handler chain are intentionally out of scope here — they would
// require a live SQLite database and an Echo server instance, which
// belong in an integration test pass.
package projectapi

import "testing"

// ─── Path validation ──────────────────────────────────────────────────────────

func TestValidateHelpFilePath_valid(t *testing.T) {
	good := []string{
		"readme.md",
		"readme.en.md",
		"readme.pt-br.md",
		"Init.en.md",
		"Run.pt-br.md",
		"diagram.svg",
		"examples/howTo.png",
		"examples/foo.bar.baz.png",
		"a.b",
		"a-b_c.d",
	}
	for _, p := range good {
		if err := validateHelpFilePath(p); err != nil {
			t.Errorf("path %q rejected: %v", p, err)
		}
	}
}

func TestValidateHelpFilePath_invalid(t *testing.T) {
	cases := []struct {
		path string
		why  string
	}{
		{"", "empty"},
		{"/readme.md", "leading slash"},
		{"readme.md/", "trailing slash"},
		{"a/b/c.md", "more than one slash"},
		{"../etc/passwd", "parent traversal"},
		{"a/../b.md", "parent in segment"},
		{"./a.md", "current-dir segment"},
		{"a/.b.md", "valid but checks segment dot rules"}, // actually allowed; verify nothing rejects it
		{"name with space.md", "space"},
		{"weird*.md", "asterisk"},
		{"weird?.md", "question mark"},
		{"a.b.", "trailing dot"},
		{"examples/.", "trailing dot in segment"},
		{`back\slash.md`, "backslash"},
		{`quote".md`, "quote"},
	}
	for _, c := range cases {
		err := validateHelpFilePath(c.path)
		// "a/.b.md" is actually a valid path under our regex — the
		// segment ".b" starts with a dot but does not end with one
		// and contains only allowed chars. Skip it from the
		// "must reject" list by checking err == nil and continuing.
		if c.path == "a/.b.md" {
			if err != nil {
				t.Errorf("path %q (%s) was rejected but should be valid: %v", c.path, c.why, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("path %q (%s) accepted but should be rejected", c.path, c.why)
		}
	}
}

func TestValidateHelpFilePath_lengthCap(t *testing.T) {
	// 200 chars exactly should pass.
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	long[len(long)-3] = '.'
	long[len(long)-2] = 'm'
	long[len(long)-1] = 'd'
	if err := validateHelpFilePath(string(long)); err != nil {
		t.Errorf("200-char path rejected: %v", err)
	}
	// 201 should fail.
	tooLong := append(long, 'x')
	if err := validateHelpFilePath(string(tooLong)); err == nil {
		t.Error("201-char path accepted")
	}
}

// ─── MIME whitelist ───────────────────────────────────────────────────────────

func TestMimeForHelpExt_recognised(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"readme.md", "text/markdown; charset=utf-8"},
		{"readme.en.md", "text/markdown; charset=utf-8"},
		{"image.png", "image/png"},
		{"PHOTO.JPG", "image/jpeg"}, // case-insensitive
		{"photo.jpeg", "image/jpeg"},
		{"diagram.svg", "image/svg+xml"},
		{"animation.gif", "image/gif"},
		{"modern.webp", "image/webp"},
	}
	for _, c := range cases {
		got, ok := mimeForHelpExt(c.path)
		if !ok {
			t.Errorf("path %q rejected", c.path)
			continue
		}
		if got != c.want {
			t.Errorf("path %q: got mime %q want %q", c.path, got, c.want)
		}
	}
}

func TestMimeForHelpExt_rejected(t *testing.T) {
	bad := []string{
		"binary.exe",
		"document.pdf", // PDFs are deliberately excluded
		"archive.zip",
		"script.js",
		"style.css",
		"page.html",
		"plain.txt",
		"noextension",
		"trailing.dot.",
	}
	for _, p := range bad {
		if _, ok := mimeForHelpExt(p); ok {
			t.Errorf("path %q accepted but should be rejected", p)
		}
	}
}

// ─── Hardcoded fallbacks ──────────────────────────────────────────────────────

// Sanity check — protect against an accidental edit that changes a
// constant without updating the seeded value in db_help_files.go.
// If these fall out of sync, the test is the louder of the two
// places where the divergence will be caught.
func TestDefaults_matchSeededValues(t *testing.T) {
	if defaultHelpMaxBytesPerProject != 5_000_000 {
		t.Errorf("defaultHelpMaxBytesPerProject = %d, want 5_000_000",
			defaultHelpMaxBytesPerProject)
	}
	if defaultHelpMaxBytesPerUser != 50_000_000 {
		t.Errorf("defaultHelpMaxBytesPerUser = %d, want 50_000_000",
			defaultHelpMaxBytesPerUser)
	}
	if helpFileBodyHardLimit <= defaultHelpMaxBytesPerProject {
		t.Errorf("helpFileBodyHardLimit (%d) must exceed per-project quota (%d)",
			helpFileBodyHardLimit, defaultHelpMaxBytesPerProject)
	}
}
