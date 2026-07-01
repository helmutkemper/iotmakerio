// server/store/project_help_files_shift_test.go — Tests for the
// pure helpers used by InsertHelpFileWithShift: classifyHelpPath
// and numberedHelpPath.
//
// The full cascade is exercised end-to-end in the handler tests
// where a real DB is available; these focus on the path-grammar
// helpers that drive position inference and reverse construction.

package store

import "testing"

func TestClassifyHelpPath(t *testing.T) {
	cases := []struct {
		path, basename, lang string
		wantPos              int
		wantOK               bool
	}{
		{"Init.en.md", "Init", "en", 0, true},
		{"Init.1.en.md", "Init", "en", 1, true},
		{"Init.42.en.md", "Init", "en", 42, true},
		{"Init.pt-br.md", "Init", "pt-br", 0, true},
		{"Init.5.pt-br.md", "Init", "pt-br", 5, true},
		// Wrong basename
		{"Run.en.md", "Init", "en", 0, false},
		// Wrong language
		{"Init.fr.md", "Init", "en", 0, false},
		// Numbered with leading zero — invalid
		{"Init.01.en.md", "Init", "en", 0, false},
		// 0 as numbered (should be primary form, not "Init.0.en.md")
		{"Init.0.en.md", "Init", "en", 0, false},
		// Wrong extension
		{"Init.en.txt", "Init", "en", 0, false},
		// Empty mid
		{"Init..en.md", "Init", "en", 0, false},
		// Non-numeric mid
		{"Init.foo.en.md", "Init", "en", 0, false},
	}
	for _, c := range cases {
		gotPos, gotOK := classifyHelpPath(c.path, c.basename, c.lang)
		if gotPos != c.wantPos || gotOK != c.wantOK {
			t.Errorf("classifyHelpPath(%q,%q,%q) = (%d,%v); want (%d,%v)",
				c.path, c.basename, c.lang,
				gotPos, gotOK, c.wantPos, c.wantOK)
		}
	}
}

func TestNumberedHelpPath(t *testing.T) {
	cases := []struct {
		basename string
		pos      int
		lang     string
		want     string
	}{
		{"Init", 0, "en", "Init.en.md"},
		{"Init", 1, "en", "Init.1.en.md"},
		{"Init", 42, "pt-br", "Init.42.pt-br.md"},
		{"readme", 0, "en", "readme.en.md"},
		{"readme", 1, "en", "readme.1.en.md"},
	}
	for _, c := range cases {
		got := numberedHelpPath(c.basename, c.pos, c.lang)
		if got != c.want {
			t.Errorf("numberedHelpPath(%q,%d,%q) = %q; want %q",
				c.basename, c.pos, c.lang, got, c.want)
		}
	}
}

// Round-trip: building a path and classifying it returns the same position.
func TestHelpPathRoundTrip(t *testing.T) {
	for _, basename := range []string{"Init", "Run", "readme"} {
		for _, lang := range []string{"en", "pt-br", "zh-CN"} {
			for _, pos := range []int{0, 1, 5, 100} {
				path := numberedHelpPath(basename, pos, lang)
				gotPos, ok := classifyHelpPath(path, basename, lang)
				if !ok {
					t.Errorf("round-trip failed: %q didn't classify", path)
					continue
				}
				if gotPos != pos {
					t.Errorf("round-trip pos: %q -> %d, want %d", path, gotPos, pos)
				}
			}
		}
	}
}
