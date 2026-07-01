// server/codegen/blackbox/devicehelp.go — Build a DeviceHelp value from a
// flat map of markdown filenames → file bytes.
//
// Why this lives in `blackbox` (the parser package) rather than the worker
// it was lifted from:
//
//   - The worker uses it when ingesting a published GitHub release ZIP.
//   - The /api/v1/blackbox handler now uses it for projects the user is
//     editing, reading the same `.md` files from `project_help_files`
//     (SQLite blobs) instead of a ZIP.
//
// Both callers parse the same filename grammar (`readme.<lang>.md`,
// `<method>.<lang>.md`, `<method>.<N>.<lang>.md`) and emit the same
// DeviceHelp shape. Sharing the function means a future tweak to the
// grammar — e.g. accepting `<method>.<N>.<lang>.<region>.md` — only has
// to land in one place; we cannot drift the worker's view of help
// markdown apart from the live editor's.
//
// The file extracts these symbols out of the worker's main.go:
//
//   - BuildDeviceHelp
//   - HelpFileRe
//   - IsLangCode
//   - RewriteImagePaths
//   - firstHeading    (private — extracts "# heading" or returns "")
//   - truncateTitle   (private)
//   - mdImageRe       (private)
//   - readmeFileRe    (private)
//   - parsePositiveInt (private)
//
// Public symbols are PascalCase exported because cross-package callers
// reach for them. The private helpers stay private — exporting them
// would invite drift if a future caller wants only one-half of the
// pipeline.
//
// All exported names mirror the Go side of the rendered shape: any
// future field added to DeviceHelp lands in the parser package and
// flows through automatically.
package blackbox

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// HelpFileRe is the canonical filename grammar for per-method help files.
// Two recognised forms (case-insensitive):
//
//	<method>.<lang>.md          → group 1=method, group 2="",  group 3=lang
//	<method>.<N>.<lang>.md      → group 1=method, group 2="N", group 3=lang
//
// Examples that match:
//
//	Init.en.md                  method=Init, lang=en
//	Init.1.en.md                method=Init, order=1, lang=en
//	Run.pt-br.md                method=Run, lang=pt-br
//
// Examples that do NOT match (and are skipped silently by BuildDeviceHelp):
//
//	readme.md                   handled separately by readmeFileRe
//	readme.en.md                handled separately by readmeFileRe
//	notes.md                    no language tag
//	Init.txt                    not a markdown file
//
// Exported as HelpFileRe so external callers (today: the handler reading
// `project_help_files`) can pre-validate filenames before calling
// BuildDeviceHelp; the function would skip the same names internally,
// but pre-validation lets the caller surface a useful error.
var HelpFileRe = regexp.MustCompile(
	`(?i)^([a-zA-Z][a-zA-Z0-9_]*)\.(\d+\.)?([a-z]{2}(?:-[a-z]{2,8})?)\.md$`,
)

// readmeFileRe is the readme counterpart of HelpFileRe. Three recognised
// forms (case-insensitive); the prefix is the literal word "readme":
//
//	readme.md                   group 1="",  group 2=""  → order 0, lang "en"
//	readme.<lang>.md            group 1="",  group 2=lang
//	readme.<N>.<lang>.md        group 1="N", group 2=lang
//
// Kept as a separate regex (rather than reusing HelpFileRe with the
// literal "readme") so the unit tests for each grammar stay isolated and
// future tweaks to one path do not silently affect the other.
var readmeFileRe = regexp.MustCompile(
	`(?i)^readme(?:\.(\d+))?(?:\.([a-z]{2}(?:-[a-z]{2,8})?))?\.md$`,
)

// mdImageRe matches Markdown image syntax: ![alt](path).
//
// Used by RewriteImagePaths to swap relative image references for
// the public URL of the same asset. Kept private — the regex is an
// implementation detail of how rewriting works; callers should reach
// for RewriteImagePaths instead.
var mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// HelpTabTitleMaxLen caps the length of a help-tab title.
//
// If the project's title-length policy moves, update both places.
// Both being constants makes the drift static-analyzable.
const HelpTabTitleMaxLen = 34

// IsLangCode returns true when s looks like a BCP-47 language tag:
// a two-letter lowercase primary subtag (e.g. "en") with an optional
// region subtag after "-" (e.g. "pt-br").
//
// Used by BuildDeviceHelp to distinguish "readme.pt-br.md" (a localised
// readme) from "readme.1.md" (the digit "1" is not two letters → not a
// lang code; the file would be silently ignored).
func IsLangCode(s string) bool {
	if len(s) < 2 {
		return false
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts[0]) != 2 {
		return false
	}
	for _, c := range parts[0] {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// RewriteImagePaths replaces relative markdown image references with the
// matching public URL from imageURLs.
//
// Path normalisation:
//
//   - Bare filenames (e.g. "rp2040.svg") match imageURLs["rp2040.svg"].
//   - Relative paths (e.g. "examples/foo.png") match imageURLs["examples/foo.png"].
//   - Leading "./" or "/" is stripped before lookup.
//   - Absolute URLs (http://, https://, /static/, /files/) are left alone.
//
// Unknown paths (not present in imageURLs) are also left alone — the
// caller is responsible for warning about them if that matters.
//
// The function is pure: it does not mutate imageURLs and produces a new
// string. Callers can call it on every help-tab body without sharing
// state.
func RewriteImagePaths(content string, imageURLs map[string]string) string {
	return mdImageRe.ReplaceAllStringFunc(content, func(match string) string {
		parts := mdImageRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		alt, path := parts[1], parts[2]

		// Skip absolute URLs and already-rewritten paths.
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") ||
			strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/files/") {
			return match
		}

		// Normalise: strip leading "./" or "/" to match the imageURLs keys
		// which use relative paths from the repo root (e.g. "examples/foo.png").
		cleanPath := strings.TrimPrefix(path, "./")
		cleanPath = strings.TrimPrefix(cleanPath, "/")

		if url, ok := imageURLs[cleanPath]; ok {
			return "![" + alt + "](" + url + ")"
		}
		return match
	})
}

// BuildDeviceHelp turns a flat name → bytes map of markdown files into a
// fully-resolved DeviceHelp value (matching the shape produced by the
// worker for GitHub-sourced devices).
//
// mdFiles keys are the file basenames (e.g. "readme.en.md", "Init.en.md",
// "Init.1.en.md"). Files outside the recognised grammar are skipped
// silently — the caller does not need to pre-filter.
//
// imageURLs is consulted by RewriteImagePaths to swap relative image
// references for public URLs. Pass an empty map (or nil) when image
// rewriting is not desired; bare filenames will then be left alone.
//
// Returned warnings are non-fatal — they describe per-file issues
// (e.g. an out-of-range tab number) so the caller can surface them
// to the user without aborting the whole help build.
//
// The Methods map keys are lowercased ("init", "run", ...) — same
// convention the existing handler uses to look up tabs by method
// in toClientDef.
func BuildDeviceHelp(mdFiles map[string][]byte, imageURLs map[string]string) (DeviceHelp, []string) {
	var warnings []string
	help := DeviceHelp{
		Readme:  make(map[string][]HelpTab),
		Methods: make(map[string]MethodHelp),
	}

	// rawTab holds one markdown file before assembly into HelpTab.
	type rawTab struct {
		order   int    // 0 = no number in filename; N = explicit N
		hasNum  bool   // true when a number appeared in the filename
		content string // markdown with rewritten image paths
	}

	// Two parallel accumulators — same shape, different sources:
	//   raw       holds method help: method → lang → []rawTab
	//   rawReadme holds device readme: lang → []rawTab (no method dimension
	//             because every readme tab belongs to the device itself)
	raw := map[string]map[string][]rawTab{}
	rawReadme := map[string][]rawTab{}

	for name, data := range mdFiles {
		content := RewriteImagePaths(string(data), imageURLs)

		// Readme files: readme.md, readme.<lang>.md, readme.<N>.<lang>.md.
		// Tried before the method regex because both could in principle
		// match a name like "readme.1.en.md" (HelpFileRe would also accept
		// "readme" as a method name); the readme branch wins by being
		// matched first.
		if rm := readmeFileRe.FindStringSubmatch(name); rm != nil {
			numStr := rm[1] // "" or "N"
			lang := strings.ToLower(rm[2])
			if lang == "" {
				lang = "en" // bare "readme.md" defaults to English
			}

			order := 0
			hasNum := numStr != ""
			if hasNum {
				n, ok := parsePositiveInt(numStr)
				if !ok {
					warnings = append(warnings, "readme file "+name+
						": invalid tab number — skipped")
					continue
				}
				order = n
			}

			rawReadme[lang] = append(rawReadme[lang], rawTab{
				order:   order,
				hasNum:  hasNum,
				content: content,
			})
			continue
		}

		// Method help files: <method>.<lang>.md or <method>.<N>.<lang>.md.
		m := HelpFileRe.FindStringSubmatch(name)
		if m == nil {
			continue // unrecognised filename — skip silently
		}
		method := strings.ToLower(m[1]) // normalise to lowercase
		numStr := strings.TrimSuffix(m[2], ".")
		lang := strings.ToLower(m[3])

		order := 0
		hasNum := numStr != ""
		if hasNum {
			n, ok := parsePositiveInt(numStr)
			if !ok {
				warnings = append(warnings, "help file "+name+
					": invalid tab number — skipped")
				continue
			}
			order = n
		}

		if raw[method] == nil {
			raw[method] = map[string][]rawTab{}
		}
		raw[method][lang] = append(raw[method][lang], rawTab{
			order:   order,
			hasNum:  hasNum,
			content: content,
		})
	}

	// assembleTabs sorts a slice of rawTab using the unified rule and
	// converts to []HelpTab. Shared by readme and method assembly because
	// the rule is identical: unnumbered file first, then numbered files
	// ascending by N.
	//
	// Title always comes from the first "# heading" line. When the
	// markdown has no heading, Title is left empty as a sentinel — the
	// WASM client substitutes a localised "title not found" message
	// (`help.title.notFound`). This applies to BOTH readme and method
	// tabs: filenames like "Init.1.en" or "readme.1.en" are technical
	// identifiers, not human-friendly tab labels, and showing them as
	// titles would mask the missing heading and discourage authors
	// from writing real ones.
	assembleTabs := func(tabs []rawTab) []HelpTab {
		sort.Slice(tabs, func(i, j int) bool {
			if !tabs[i].hasNum && tabs[j].hasNum {
				return true
			}
			if tabs[i].hasNum && !tabs[j].hasNum {
				return false
			}
			return tabs[i].order < tabs[j].order
		})
		out := make([]HelpTab, 0, len(tabs))
		for _, t := range tabs {
			out = append(out, HelpTab{
				Order:   t.order,
				Title:   truncateTitle(firstHeading(t.content)),
				Content: t.content,
			})
		}
		return out
	}

	// Assemble readme tabs per language.
	for lang, tabs := range rawReadme {
		help.Readme[lang] = assembleTabs(tabs)
	}

	// Assemble method help tabs.
	//
	// Sorting rule (same as readme):
	//   1. Unnumbered file (hasNum=false) is always first — becomes the
	//      leading tab.
	//   2. Numbered files sort ascending by their number.
	//
	// init.en.md + init.1.en.md + init.2.en.md →
	//   [init.en.md (tab 0), init.1.en.md (tab 1), init.2.en.md (tab 2)]
	for method, langMap := range raw {
		mh := MethodHelp{Langs: make(map[string][]HelpTab)}
		for lang, tabs := range langMap {
			mh.Langs[lang] = assembleTabs(tabs)
		}
		help.Methods[method] = mh
	}

	return help, warnings
}

// parsePositiveInt parses a non-negative decimal integer without pulling
// strconv. Returns (n, true) on success, (0, false) on any non-digit
// character. Used by both the readme and method branches of
// BuildDeviceHelp to convert the optional `N` segment of the filename.
func parsePositiveInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}

// firstHeading returns the text of the first "# Heading" line found in
// the markdown content, or "" when no heading is present. The empty
// return is used by both readme and method assembly paths as a sentinel
// for the WASM client's localised "title not found" fallback.
func firstHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

// truncateTitle cuts s to at most HelpTabTitleMaxLen runes at the last
// word boundary before the limit. Appends "..." only when truncation
// occurs. Never cuts mid-word to avoid embarrassing truncations.
func truncateTitle(s string) string {
	maxLen := HelpTabTitleMaxLen
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)

	// Walk backward from the limit to find the last space.
	cut := maxLen
	for cut > 0 && runes[cut-1] != ' ' {
		cut--
	}
	if cut == 0 {
		// No space found — hard-cut only happens for pathological
		// single-word titles longer than the limit.
		cut = maxLen
	}
	return strings.TrimRight(string(runes[:cut]), " ") + "..."
}
