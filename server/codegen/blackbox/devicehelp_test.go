// server/codegen/blackbox/devicehelp_test.go — Round-trip tests for
// the extracted help-build pipeline. No I/O — pure function tests.
package blackbox

import (
	"strings"
	"testing"
)

// ─── BuildDeviceHelp ──────────────────────────────────────────────────────────

func TestBuildDeviceHelp_readme(t *testing.T) {
	mdFiles := map[string][]byte{
		"readme.md":       []byte("# Hello\n\nbody"),
		"readme.pt-br.md": []byte("# Olá\n\ncorpo"),
	}
	help, warns := BuildDeviceHelp(mdFiles, nil)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	enTabs := help.Readme["en"]
	if len(enTabs) != 1 {
		t.Fatalf("expected 1 en tab, got %d", len(enTabs))
	}
	if !strings.Contains(enTabs[0].Content, "Hello") {
		t.Errorf("en readme: got %q", enTabs[0].Content)
	}
	if enTabs[0].Title != "Hello" {
		t.Errorf("en title: got %q, want %q", enTabs[0].Title, "Hello")
	}
	ptTabs := help.Readme["pt-br"]
	if len(ptTabs) != 1 {
		t.Fatalf("expected 1 pt-br tab, got %d", len(ptTabs))
	}
	if !strings.Contains(ptTabs[0].Content, "Olá") {
		t.Errorf("pt-br readme: got %q", ptTabs[0].Content)
	}
	if ptTabs[0].Title != "Olá" {
		t.Errorf("pt-br title: got %q, want %q", ptTabs[0].Title, "Olá")
	}
}

// TestBuildDeviceHelp_readmeOrderedTabs covers the previously-broken case:
// readme.<N>.<lang>.md must produce additional ordered tabs alongside the
// unnumbered readme. The unnumbered file ranks first, then numbered files
// ascending by N — same rule as method tabs.
func TestBuildDeviceHelp_readmeOrderedTabs(t *testing.T) {
	mdFiles := map[string][]byte{
		"readme.en.md":   []byte("# Overview\nintro"),
		"readme.1.en.md": []byte("# Wiring\npins"),
		"readme.2.en.md": []byte("# Troubleshooting\nfaqs"),
	}
	help, warns := BuildDeviceHelp(mdFiles, nil)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	tabs := help.Readme["en"]
	if len(tabs) != 3 {
		t.Fatalf("expected 3 readme tabs, got %d", len(tabs))
	}
	wantTitles := []string{"Overview", "Wiring", "Troubleshooting"}
	for i, want := range wantTitles {
		if tabs[i].Title != want {
			t.Errorf("tab %d title: got %q, want %q", i, tabs[i].Title, want)
		}
	}
	wantOrders := []int{0, 1, 2}
	for i, want := range wantOrders {
		if tabs[i].Order != want {
			t.Errorf("tab %d order: got %d, want %d", i, tabs[i].Order, want)
		}
	}
}

// TestBuildDeviceHelp_readmeNoHeading verifies that a readme without a "#
// heading" line yields an empty Title — the WASM client treats empty as a
// sentinel for "use the localised fallback string". The same rule applies
// to method help (see TestBuildDeviceHelp_methodNoHeading) — neither path
// uses the filename as a fallback because filenames like "Init.1.en" are
// technical identifiers, not human-friendly tab titles.
func TestBuildDeviceHelp_readmeNoHeading(t *testing.T) {
	mdFiles := map[string][]byte{
		"readme.en.md": []byte("body without heading"),
	}
	help, _ := BuildDeviceHelp(mdFiles, nil)
	tabs := help.Readme["en"]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].Title != "" {
		t.Errorf("expected empty title for fallback, got %q", tabs[0].Title)
	}
}

// TestBuildDeviceHelp_methodNoHeading mirrors readmeNoHeading for the
// method-help path. Same contract: empty Title when the markdown lacks a
// "# heading" line; the WASM client substitutes the localised fallback so
// the missing heading is visible to the author.
func TestBuildDeviceHelp_methodNoHeading(t *testing.T) {
	mdFiles := map[string][]byte{
		"Init.1.en.md": []byte("body without heading"),
	}
	help, _ := BuildDeviceHelp(mdFiles, nil)
	tabs := help.Methods["init"].Langs["en"]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].Title != "" {
		t.Errorf("expected empty title for fallback, got %q", tabs[0].Title)
	}
}

// TestBuildDeviceHelp_readmeBareDefaultsToEn covers the "readme.md" case:
// no language segment means English by convention. This is the historical
// behaviour preserved through the refactor.
func TestBuildDeviceHelp_readmeBareDefaultsToEn(t *testing.T) {
	mdFiles := map[string][]byte{
		"readme.md": []byte("# Default\ntext"),
	}
	help, _ := BuildDeviceHelp(mdFiles, nil)
	tabs, ok := help.Readme["en"]
	if !ok || len(tabs) != 1 {
		t.Fatalf("expected en[0] to exist, got Readme=%+v", help.Readme)
	}
	if tabs[0].Title != "Default" {
		t.Errorf("title: got %q, want %q", tabs[0].Title, "Default")
	}
}

// TestBuildDeviceHelp_readmeOrderedNoUnnumbered covers the case where ALL
// readme files for a given language carry an explicit number — there is no
// "tab 0" baseline, so the first tab in the slice is whichever has the
// lowest N.
func TestBuildDeviceHelp_readmeOrderedNoUnnumbered(t *testing.T) {
	mdFiles := map[string][]byte{
		"readme.5.en.md": []byte("# Fifth"),
		"readme.2.en.md": []byte("# Second"),
		"readme.9.en.md": []byte("# Ninth"),
	}
	help, _ := BuildDeviceHelp(mdFiles, nil)
	tabs := help.Readme["en"]
	if len(tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(tabs))
	}
	wantTitles := []string{"Second", "Fifth", "Ninth"}
	for i, want := range wantTitles {
		if tabs[i].Title != want {
			t.Errorf("tab %d title: got %q, want %q", i, tabs[i].Title, want)
		}
	}
}

func TestBuildDeviceHelp_methodTabs(t *testing.T) {
	mdFiles := map[string][]byte{
		"Init.en.md":   []byte("# Setup\nstuff"),
		"Init.1.en.md": []byte("# Wiring\nmore stuff"),
	}
	help, _ := BuildDeviceHelp(mdFiles, nil)

	mh, ok := help.Methods["init"]
	if !ok {
		t.Fatalf("expected lowercase 'init' key, got keys: %v", help.Methods)
	}
	tabs := mh.Langs["en"]
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
	// Unnumbered file ranks first.
	if !strings.Contains(tabs[0].Content, "Setup") {
		t.Errorf("tab 0: got %q", tabs[0].Content)
	}
	if !strings.Contains(tabs[1].Content, "Wiring") {
		t.Errorf("tab 1: got %q", tabs[1].Content)
	}
}

func TestBuildDeviceHelp_unknownFilesIgnored(t *testing.T) {
	mdFiles := map[string][]byte{
		"notes.md":   []byte("orphan"),
		"random.txt": []byte("not even md"),
		"Init.en.md": []byte("# Init"),
	}
	help, _ := BuildDeviceHelp(mdFiles, nil)
	if _, ok := help.Methods["init"]; !ok {
		t.Errorf("Init.en.md should have been recognised")
	}
	if len(help.Readme) != 0 {
		t.Errorf("notes.md must NOT become a readme: %v", help.Readme)
	}
}

func TestBuildDeviceHelp_imageRewriting(t *testing.T) {
	mdFiles := map[string][]byte{
		"readme.md": []byte("![diagram](examples/foo.png)"),
	}
	imageURLs := map[string]string{
		"examples/foo.png": "/api/v1/projects/abc/files/help/examples/foo.png",
	}
	help, _ := BuildDeviceHelp(mdFiles, imageURLs)
	tabs := help.Readme["en"]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if !strings.Contains(tabs[0].Content, "/api/v1/projects/abc/files/help/examples/foo.png") {
		t.Errorf("expected rewritten URL, got: %q", tabs[0].Content)
	}
}

func TestBuildDeviceHelp_empty(t *testing.T) {
	help, warns := BuildDeviceHelp(nil, nil)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if len(help.Readme) != 0 || len(help.Methods) != 0 {
		t.Errorf("expected empty help, got: %+v", help)
	}
}

// ─── IsLangCode ───────────────────────────────────────────────────────────────

func TestIsLangCode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"en", true},
		{"pt-br", true},
		{"zh-hans", true},
		{"e", false},       // too short
		{"123", false},     // digits
		{"En", false},      // uppercase
		{"english", false}, // not 2 letters
		{"en-", true},      // primary subtag still 2 lowercase letters
		{"", false},
	}
	for _, c := range cases {
		if got := IsLangCode(c.in); got != c.want {
			t.Errorf("IsLangCode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// ─── RewriteImagePaths ────────────────────────────────────────────────────────

func TestRewriteImagePaths(t *testing.T) {
	imageURLs := map[string]string{
		"diagram.svg":      "/api/v1/projects/abc/files/help/diagram.svg",
		"examples/foo.png": "/api/v1/projects/abc/files/help/examples/foo.png",
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"bare filename",
			"![alt](diagram.svg)",
			"![alt](/api/v1/projects/abc/files/help/diagram.svg)",
		},
		{
			"relative path",
			"![alt](examples/foo.png)",
			"![alt](/api/v1/projects/abc/files/help/examples/foo.png)",
		},
		{
			"strips leading ./",
			"![alt](./diagram.svg)",
			"![alt](/api/v1/projects/abc/files/help/diagram.svg)",
		},
		{
			"strips leading /",
			"![alt](/diagram.svg)",
			"![alt](/api/v1/projects/abc/files/help/diagram.svg)",
		},
		{
			"absolute https left alone",
			"![alt](https://cdn.example.com/img.png)",
			"![alt](https://cdn.example.com/img.png)",
		},
		{
			"static prefix left alone",
			"![alt](/static/foo.png)",
			"![alt](/static/foo.png)",
		},
		{
			"unknown path left alone",
			"![alt](unknown.png)",
			"![alt](unknown.png)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RewriteImagePaths(c.in, imageURLs)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
