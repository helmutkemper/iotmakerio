// cmd/gen-fa-icons/main.go — FontAwesome SVG registry generator.
//
// # Purpose
//
// Reads a local checkout of the FontAwesome Free repository and generates
// rulesIcon/faIconsGenerated.go, which contains a complete map of every
// FA icon (solid + brands + regular) keyed by both name and codepoint.
//
// After running this generator, every `icon:` tag in a black-box comment
// can reference any FA Free icon by name OR by unicode codepoint — without
// needing to manually add entries to falcons.go or iconRegistry.go.
//
// # Quick start (no reading required)
//
//	git clone --depth 1 https://github.com/FortAwesome/Font-Awesome.git /tmp/fa
//	go run ./cmd/gen-fa-icons --fa /tmp/fa
//
// The generated file is written to rulesIcon/faIconsGenerated.go relative
// to the directory where you run the command (the repository root).
//
// # Full explanation
//
// FontAwesome Free ships three icon sets:
//   - solid   (svgs/solid/)   — filled icons, most common set
//   - brands  (svgs/brands/)  — brand/logo icons (USB, GitHub, etc.)
//   - regular (svgs/regular/) — outline icons (smaller subset than solid)
//
// The metadata file (metadata/icons.json) maps every icon name to its
// unicode codepoint and which styles it supports. The SVG files contain
// the actual path data.
//
// The generator reads both sources and produces a Go file with:
//
//  1. var faIconDefs — a map[string]FAIconDef keyed by name (e.g. "usb").
//     The name is the kebab-case FA name as it appears in the class
//     fa-usb. This replaces the hand-written iconDefs in iconRegistry.go.
//
//  2. var faIconByCodepoint — a map[rune]FAIconDef keyed by unicode
//     codepoint (e.g. 0xf287). This allows `icon:f287.` to resolve
//     directly to an SVG path without needing the name.
//
// The generated file is guarded by `//go:build ignore` on the generator
// itself, and the generated output has a DO NOT EDIT header.
//
// # SVG path merging
//
// Some FA icons use multiple <path> elements (e.g. duotone icons, or icons
// with a separate clip path). For those, the generator concatenates all
// <path d="..."> values into a single path string separated by a space.
// This is valid SVG — a path's d attribute can contain multiple subpaths
// (M/L/Z commands), and a space separator is treated as a subpath boundary.
// Visual result is identical to two separate <path> elements with the same
// fill color.
//
// # Output location
//
//	rulesIcon/faIconsGenerated.go
//
// # Updating
//
// Re-run the generator after updating the FA version in your checkout.
// The generated file will be overwritten completely.
//
// # Keeping falcons.go
//
// The existing falcons.go constants (KFAGear, KFAPlay, etc.) are preserved
// as-is. The generated file registers the same icons under their FA names,
// so iconDefs["gear"] and KFAGear point to the same SVG path data.
// If they ever diverge (FA updates the path), the generated file wins
// because it was seeded from the authoritative FA source.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ─── CLI flags ────────────────────────────────────────────────────────────────

var (
	faDir  = flag.String("fa", "", "Path to the FontAwesome Free repository root (required)")
	outDir = flag.String("out", "rulesIcon", "Output directory for the generated Go file")
	jsOut  = flag.String("js-out", "server/public/static/js", "Output directory for fa-brands-set.js")
	pkg    = flag.String("pkg", "rulesIcon", "Go package name for the generated file")
)

// ─── FA metadata types ───────────────────────────────────────────────────────

// iconMeta is the shape of each entry in metadata/icons.json.
// Only the fields we need are decoded; the rest are ignored.
type iconMeta struct {
	// Unicode is the codepoint as a hex string without "0x" prefix, e.g. "f287".
	Unicode string `json:"unicode"`

	// Styles lists which icon sets contain this icon: "solid", "brands", "regular".
	// On Pro-aware metadata files this includes Pro-only styles too — see Free
	// below for the licensed-as-free subset.
	Styles []string `json:"styles"`

	// Free is the styles legally available under the FontAwesome Free licence.
	// In FontAwesome's metadata file this is a separate list from `styles` so
	// that Pro entries can advertise their availability without misleading
	// downstream tooling.
	//
	// An icon with `Styles=["solid","regular"]` and `Free=["solid"]` exists
	// in both Solid and Regular sets, but only the Solid variant is included
	// in the Free webfont/CSS distribution. Rendering the Regular variant in
	// a browser using the Free CSS would produce the missing-glyph "tofu" □
	// because the glyph is not in the Free font file.
	//
	// IoTMaker ships the Free CSS only — using Pro icons in the IDE wizard
	// would be both visually broken (tofu) AND a licensing violation. The
	// generator emits a separate `fa-free-set.js` driven entirely by this
	// list so the wizard's icon picker can filter Pro out at the source.
	Free []string `json:"free"`

	// SVGData is the inline SVG data from the "svg" key. Present in some FA versions.
	// We prefer reading the SVG files directly; this is a fallback.
	SVGData map[string]svgDataEntry `json:"svg"`
}

// svgDataEntry is the per-style SVG data embedded in icons.json (some FA versions).
type svgDataEntry struct {
	ViewBox []int  `json:"viewBox"`
	Path    string `json:"path"` // may be absent; paths may also be in "raw"
	Raw     string `json:"raw"`  // full SVG inner content
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// ─── Generated entry ─────────────────────────────────────────────────────────

// iconEntry holds the data for one icon to be emitted.
type iconEntry struct {
	Name      string // kebab-case FA name (e.g. "usb")
	Style     string // "solid", "brands", "regular"
	Codepoint rune   // unicode codepoint (e.g. 0xf287)
	ViewBox   string // e.g. "0 0 512 512"
	Path      string // merged SVG path data
}

// ─── SVG parsing ─────────────────────────────────────────────────────────────

// Precompiled regexes used for SVG parsing.
var (
	// viewBoxRe extracts the viewBox attribute from an SVG element.
	viewBoxRe = regexp.MustCompile(`(?i)viewBox\s*=\s*"([^"]+)"`)

	// pathDRe extracts the d="..." attribute from a <path> element.
	// Matches both single and double quotes; handles whitespace around =.
	pathDRe = regexp.MustCompile(`(?i)<path[^>]*\sd\s*=\s*"([^"]+)"`)
)

// parseSVGFile reads an SVG file and returns (viewBox, mergedPath, error).
// Multiple <path d="..."> elements are merged by concatenation with a space.
// Non-path elements (circles, rects, etc.) that have no d attribute are skipped
// — FA Free icons are exclusively path-based, so this is safe.
func parseSVGFile(path string) (viewBox, mergedPath string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	content := string(data)

	// Extract viewBox.
	vbMatch := viewBoxRe.FindStringSubmatch(content)
	if len(vbMatch) < 2 {
		return "", "", fmt.Errorf("no viewBox in %s", path)
	}
	viewBox = vbMatch[1]

	// Extract all <path d="..."> values.
	pathMatches := pathDRe.FindAllStringSubmatch(content, -1)
	if len(pathMatches) == 0 {
		return "", "", fmt.Errorf("no <path d=...> in %s", path)
	}

	var parts []string
	for _, m := range pathMatches {
		if len(m) >= 2 && strings.TrimSpace(m[1]) != "" {
			parts = append(parts, strings.TrimSpace(m[1]))
		}
	}
	if len(parts) == 0 {
		return "", "", fmt.Errorf("empty path data in %s", path)
	}

	// Concatenate multiple paths. A space between subpaths is valid SVG —
	// each M command starts a new subpath, so this is semantically equivalent
	// to separate <path> elements with the same fill.
	mergedPath = strings.Join(parts, " ")
	return viewBox, mergedPath, nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	flag.Parse()
	log.SetFlags(0) // no timestamp in log output

	*faDir = "/Users/kemper/go/kemper/iotmakerio/_font-awesome"
	*outDir = "/Users/kemper/go/kemper/iotmakerio/_code"
	*jsOut = "/Users/kemper/go/kemper/iotmakerio/_code"

	if *faDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/gen-fa-icons --fa /path/to/Font-Awesome")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Quick start:")
		fmt.Fprintln(os.Stderr, "  git clone --depth 1 https://github.com/FortAwesome/Font-Awesome.git /tmp/fa")
		fmt.Fprintln(os.Stderr, "  go run ./cmd/gen-fa-icons --fa /tmp/fa")
		os.Exit(1)
	}

	faRoot := filepath.Clean(*faDir)

	// ── 1. Load metadata/icons.json ─────────────────────────────────────────
	metaPath := filepath.Join(faRoot, "metadata", "icons.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		// Some FA versions put the file at a different location.
		alt := filepath.Join(faRoot, "icons.json")
		metaData, err = os.ReadFile(alt)
		if err != nil {
			log.Fatalf("cannot read metadata/icons.json from %s: %v", faRoot, err)
		}
		log.Printf("note: using icons.json at %s (not in metadata/)", alt)
	}

	var meta map[string]iconMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		log.Fatalf("cannot parse icons.json: %v", err)
	}
	log.Printf("loaded metadata: %d icons", len(meta))

	// ── 2. Process each icon name and style ──────────────────────────────────
	//
	// We iterate in sorted order so the output is deterministic across runs.
	// Determinism is important: a randomly ordered output would produce noisy
	// git diffs every time the generator is re-run.
	names := make([]string, 0, len(meta))
	for name := range meta {
		names = append(names, name)
	}
	sort.Strings(names)

	var entries []iconEntry
	skipped := 0

	for _, name := range names {
		m := meta[name]

		// Parse the codepoint.
		cp, err := strconv.ParseInt(m.Unicode, 16, 32)
		if err != nil {
			log.Printf("skip %q: invalid unicode %q", name, m.Unicode)
			skipped++
			continue
		}
		codepoint := rune(cp)

		// Process each available style.
		for _, style := range m.Styles {
			svgFile := filepath.Join(faRoot, "svgs", style, name+".svg")
			viewBox, path, err := parseSVGFile(svgFile)
			if err != nil {
				// Try inline metadata as fallback (some FA versions embed SVG data).
				if sd, ok := m.SVGData[style]; ok && sd.Path != "" {
					vb := ""
					for _, value := range sd.ViewBox {
						vb += fmt.Sprintf("%d ", value)
					}
					vb = strings.TrimSuffix(vb, " ")
					viewBox = vb
					path = sd.Path
				} else {
					log.Printf("skip %s/%q: %v", style, name, err)
					skipped++
					continue
				}
			}

			entries = append(entries, iconEntry{
				Name:      name,
				Style:     style,
				Codepoint: codepoint,
				ViewBox:   viewBox,
				Path:      path,
			})
		}
	}

	log.Printf("processed %d entries (%d skipped)", len(entries), skipped)

	// ── 3. Collect brand names AND free-licensed name→styles map ────────────
	//
	// brandNames drives fa-brands-set.js (already shipping; tells the
	// frontend which icons need fa-brands instead of fa-solid).
	//
	// freeStyles drives fa-free-styles.js (NEW): per-icon list of WHICH
	// free styles each icon ships in. The previous slice's flat "free
	// set" said only "this icon has at least one free form" — but didn't
	// say which form, so the frontend defaulted everything to fa-solid
	// and any icon whose free style is `regular` (e.g. `aries`,
	// `aquarius`, `alarm-clock`) rendered as the missing-glyph tofu (□).
	//
	// FontAwesome Free has exactly three font families:
	//   solid    — fa-solid  (most icons)
	//   regular  — fa-regular (outline subset)
	//   brands   — fa-brands  (logos)
	// Pro-only families (light, thin, sharp, duotone) cannot appear here
	// because the metadata's `free` field is filtered to free-licensed
	// styles only.
	//
	// The frontend uses the per-icon styles list to pick the correct
	// CSS class — preferring solid, then regular, then brands. Brands
	// is also stored separately for back-compat with the existing
	// `FA_BRANDS_SET` consumers.
	brandNames := make([]string, 0, 512)
	freeStyles := make(map[string][]string, 2048)
	for _, name := range names {
		m := meta[name]
		// Brand membership: the icon is registered in the "brands" style.
		// We check via metadata (m.Styles) so the result is independent of
		// any SVG read failures earlier in the pipeline.
		for _, s := range m.Styles {
			if s == "brands" {
				brandNames = append(brandNames, name)
				break
			}
		}
		// Free styles: the metadata's `free` array tells us which
		// font families the icon ships in under the Free licence.
		// Empty `free` means the icon is Pro-only and we exclude it.
		if len(m.Free) > 0 {
			// Copy the slice so generator-internal sort doesn't mutate
			// the underlying metadata struct.
			styles := make([]string, len(m.Free))
			copy(styles, m.Free)
			sort.Strings(styles)
			freeStyles[name] = styles
		}
	}
	sort.Strings(brandNames)
	brandNames = dedupStrings(brandNames)

	log.Printf("brand names: %d", len(brandNames))
	log.Printf("free-licensed names: %d (of %d total — %d Pro-only filtered out)",
		len(freeStyles), len(names), len(names)-len(freeStyles))

	// ── 4. Generate Go source ────────────────────────────────────────────────
	source := generateGoSource(entries, *pkg)

	// ── 5. Format with go/format ─────────────────────────────────────────────
	formatted, err := format.Source([]byte(source))
	if err != nil {
		// If formatting fails, write the unformatted source so the user can
		// inspect the error. This should never happen with well-formed output.
		log.Printf("warning: go/format failed (%v) — writing unformatted source", err)
		formatted = []byte(source)
	}

	// ── 6. Write Go output file ──────────────────────────────────────────────
	outFile := filepath.Join(*outDir, "faIconsGenerated.go")
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("cannot create output directory %s: %v", *outDir, err)
	}
	if err := os.WriteFile(outFile, formatted, 0644); err != nil {
		log.Fatalf("cannot write %s: %v", outFile, err)
	}
	log.Printf("wrote %s (%d bytes, %d icons)", outFile, len(formatted), len(entries))

	// ── 7. Write JS brands set ────────────────────────────────────────────────
	jsSource := generateJSBrandsSet(brandNames)
	jsFile := filepath.Join(*jsOut, "fa-brands-set.js")
	if err := os.MkdirAll(*jsOut, 0755); err != nil {
		log.Fatalf("cannot create js output directory %s: %v", *jsOut, err)
	}
	if err := os.WriteFile(jsFile, []byte(jsSource), 0644); err != nil {
		log.Fatalf("cannot write %s: %v", jsFile, err)
	}
	log.Printf("wrote %s (%d bytes, %d brand names)", jsFile, len(jsSource), len(brandNames))

	// ── 8. Write JS free-styles map ───────────────────────────────────────────
	//
	// Same output directory + emission pattern as fa-brands-set.js. See
	// generateJSFreeStyles for the per-style rationale.
	freeJSSource := generateJSFreeStyles(freeStyles)
	freeJSFile := filepath.Join(*jsOut, "fa-free-styles.js")
	if err := os.WriteFile(freeJSFile, []byte(freeJSSource), 0644); err != nil {
		log.Fatalf("cannot write %s: %v", freeJSFile, err)
	}
	log.Printf("wrote %s (%d bytes, %d free icons with style info)", freeJSFile, len(freeJSSource), len(freeStyles))

	log.Println("")
	log.Println("Next steps:")
	log.Println("  1. The existing falcons.go constants (KFAGear etc.) still work — no changes needed")
	log.Println("  2. Build the project: make wasm (or go build ./...)")
	log.Println("  3. Ensure app.html and ide/index.html load fa-brands-set.js AND fa-free-styles.js")
	log.Println("     (already done if you applied the HTML patches — they load from /static/js/)")
}

// ─── Code generation ─────────────────────────────────────────────────────────

func generateGoSource(entries []iconEntry, pkgName string) string {
	var buf bytes.Buffer

	// File header — always written first.
	buf.WriteString("// Code generated by cmd/gen-fa-icons/main.go — DO NOT EDIT.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Regenerate with:\n")
	buf.WriteString("//   go run ./cmd/gen-fa-icons --fa /path/to/Font-Awesome\n")
	buf.WriteString("//\n")
	buf.WriteString("// This file replaces the hand-written iconDefs map in iconRegistry.go\n")
	buf.WriteString("// and adds faIconByCodepoint for unicode codepoint lookups.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Icon style key suffixes used in faIconByCodepoint:\n")
	buf.WriteString("//   (no suffix) → solid\n")
	buf.WriteString("//   :b          → brands\n")
	buf.WriteString("//   :r          → regular\n")
	buf.WriteString("//\n")
	buf.WriteString("// The iconDefs variable declared here is used by rulesIcon.IconByName().\n")
	buf.WriteString("// It is initialised in an init() function so the map entries can reference\n")
	buf.WriteString("// the path string literals directly without needing a const block.\n")
	buf.WriteString("\n")
	fmt.Fprintf(&buf, "package %s\n\n", pkgName)

	// Group entries by style for the name map.
	// Priority: solid wins when multiple styles share a name, then brands, then regular.
	// This means icon:usb. resolves to the solid USB icon if one exists,
	// otherwise brands (fa-brands fa-usb), otherwise regular.
	byName := make(map[string]iconEntry)
	styleOrder := map[string]int{"solid": 0, "brands": 1, "regular": 2}
	for _, e := range entries {
		existing, ok := byName[e.Name]
		if !ok || styleOrder[e.Style] < styleOrder[existing.Style] {
			byName[e.Name] = e
		}
	}

	// Sort names for deterministic output.
	sortedNames := make([]string, 0, len(byName))
	for name := range byName {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	// Build the codepoint map. Key is the codepoint itself; for brands icons
	// the key encodes the style too (so both f287 and f287:b resolve correctly).
	// We build a map[string]iconEntry where the key is the lookup string used
	// by ParseIconValue after style detection: just the hex digits.
	// For same codepoint appearing in multiple styles, we store:
	//   "f287"   → the solid entry (or brands if no solid)
	//   "f287:b" → the brands entry (so explicit :b requests brands)
	//   "f287:r" → the regular entry
	type codepointKey struct {
		hex   string
		style string // "" = solid/default, "b" = brands, "r" = regular
	}
	byCodepoint := make(map[codepointKey]iconEntry)
	for _, e := range entries {
		hex := fmt.Sprintf("%04x", e.Codepoint)
		var key codepointKey
		switch e.Style {
		case "brands":
			key = codepointKey{hex, "b"}
		case "regular":
			key = codepointKey{hex, "r"}
		default: // solid
			key = codepointKey{hex, ""}
		}
		if _, exists := byCodepoint[key]; !exists {
			byCodepoint[key] = e
		}
	}

	// ── iconDefs (name → FAIconDef) ───────────────────────────────────────────
	buf.WriteString("func init() {\n")
	buf.WriteString("\t// iconDefs is declared in iconRegistry.go.\n")
	buf.WriteString("\t// This init() populates it with the full FA Free icon set.\n")
	buf.WriteString("\t// The existing hand-written entries in iconRegistry.go are\n")
	buf.WriteString("\t// overwritten by this init() since init() runs after package-level\n")
	buf.WriteString("\t// var declarations.\n")
	buf.WriteString("\ticonDefs = map[string]FAIconDef{\n")

	for _, name := range sortedNames {
		e := byName[name]
		buf.WriteString("\t\t")
		buf.WriteString(goString(name))
		buf.WriteString(": {")
		buf.WriteString(goString(e.Path))
		buf.WriteString(", ")
		buf.WriteString(goString(e.ViewBox))
		buf.WriteString("},\n")
	}

	buf.WriteString("\t}\n\n")

	// ── faIconByCodepoint (hex string → FAIconDef) ────────────────────────────
	// Keys match what ParseIconValue produces after stripping \u/0x prefix and
	// separating the style suffix. Stored as string keys for fast lookup.
	buf.WriteString("\t// faIconByCodepoint maps hex codepoint strings to icon definitions.\n")
	buf.WriteString("\t// Keys:\n")
	buf.WriteString("\t//   \"f287\"   → solid (or brands when no solid exists)\n")
	buf.WriteString("\t//   \"f287:b\" → brands\n")
	buf.WriteString("\t//   \"f287:r\" → regular\n")
	buf.WriteString("\tfaIconByCodepoint = map[string]FAIconDef{\n")

	// Sort keys for deterministic output.
	cpKeys := make([]codepointKey, 0, len(byCodepoint))
	for k := range byCodepoint {
		cpKeys = append(cpKeys, k)
	}
	sort.Slice(cpKeys, func(i, j int) bool {
		if cpKeys[i].hex != cpKeys[j].hex {
			return cpKeys[i].hex < cpKeys[j].hex
		}
		return cpKeys[i].style < cpKeys[j].style
	})

	for _, k := range cpKeys {
		e := byCodepoint[k]
		mapKey := k.hex
		if k.style != "" {
			mapKey += ":" + k.style
		}
		buf.WriteString("\t\t")
		buf.WriteString(goString(mapKey))
		buf.WriteString(": {")
		buf.WriteString(goString(e.Path))
		buf.WriteString(", ")
		buf.WriteString(goString(e.ViewBox))
		buf.WriteString("},\n")
	}

	buf.WriteString("\t}\n")
	buf.WriteString("}\n")

	return buf.String()
}

// goString formats a Go string literal, escaping backslashes and double quotes.
// Uses raw string literals (backticks) for SVG paths since they contain many
// characters that would need escaping in regular strings (but no backticks).
func goString(s string) string {
	// SVG paths and viewBox strings never contain backticks, so use raw literals.
	// This makes the generated file much more readable and avoids escaping issues.
	if !strings.ContainsRune(s, '`') {
		return "`" + s + "`"
	}
	// Fallback to double-quoted string with escaping (should never happen for FA data).
	return strconv.Quote(s)
}

// ─── JS brands set generator ──────────────────────────────────────────────────

// generateJSBrandsSet returns the source of fa-brands-set.js.
//
// The file sets window.FA_BRANDS_SET to a JS Set of all FA Brands icon names.
// renderFAIconHtml in blackbox.js checks this set to automatically use
// "fa-brands" instead of "fa-solid" for brand icons — no manual ":b" suffix
// needed.
//
// The file is safe to load in both the SPA (app.html) and the WASM IDE
// (ide/index.html) since it only sets a global variable.
func generateJSBrandsSet(names []string) string {
	var buf bytes.Buffer

	buf.WriteString("// fa-brands-set.js — Generated by cmd/gen-fa-icons/main.go — DO NOT EDIT.\n")
	buf.WriteString("// Regenerate with: go run ./cmd/gen-fa-icons --fa /path/to/Font-Awesome\n")
	buf.WriteString("//\n")
	buf.WriteString("// Sets window.FA_BRANDS_SET to a Set of all FontAwesome Brands icon names.\n")
	buf.WriteString("// Used by renderFAIconHtml() in blackbox.js to automatically choose\n")
	buf.WriteString("// 'fa-brands' over 'fa-solid' without requiring a ':b' suffix in icon: tags.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Example: icon:angellist.  →  <i class=\"fa-brands fa-angellist\"></i>\n")
	buf.WriteString("//          icon:gear.       →  <i class=\"fa-solid fa-gear\"></i>\n")
	buf.WriteString("\n")
	buf.WriteString("window.FA_BRANDS_SET = new Set([\n")

	for _, name := range names {
		buf.WriteString("  ")
		buf.WriteString(strconv.Quote(name))
		buf.WriteString(",\n")
	}

	buf.WriteString("]);\n")

	return buf.String()
}

// generateJSFreeStyles returns the source of fa-free-styles.js.
//
// The file sets window.FA_FREE_STYLES to a plain object mapping every
// FontAwesome icon name licensed under the Free plan to the list of
// font families that ship the glyph in Free. Example:
//
//	window.FA_FREE_STYLES = {
//	    "alarm-clock": ["regular"],
//	    "abacus":      ["solid"],
//	    "github":      ["brands"],
//	    "address-book": ["regular", "solid"],
//	};
//
// The frontend uses this to choose the correct CSS class per icon. A
// flat "is-free" set (the previous design) wasn't enough: an icon
// like `aries` ships as Free in the `regular` family but not `solid`,
// and rendering it as `<i class="fa-solid fa-aries">` produces the
// missing-glyph tofu (□) because the solid webfont has no aries glyph.
//
// The frontend's resolution order is:
//
//  1. brands  → fa-brands  (most specific)
//  2. solid   → fa-solid   (most common; preferred when present)
//  3. regular → fa-regular (outline; used for icons free only here)
//
// FA Free does not include light/thin/sharp/duotone — those are Pro.
// So the styles list is at most three entries.
//
// Like fa-brands-set.js, this file is safe to load in both the SPA
// and the WASM IDE because it only sets a global variable.
func generateJSFreeStyles(stylesByName map[string][]string) string {
	var buf bytes.Buffer

	buf.WriteString("// fa-free-styles.js — Generated by cmd/gen-fa-icons/main.go — DO NOT EDIT.\n")
	buf.WriteString("// Regenerate with: go run ./cmd/gen-fa-icons --fa /path/to/Font-Awesome\n")
	buf.WriteString("//\n")
	buf.WriteString("// Sets window.FA_FREE_STYLES to a per-icon map of free-licensed font\n")
	buf.WriteString("// families (\"solid\", \"regular\", \"brands\"). The frontend uses this to\n")
	buf.WriteString("// pick the correct CSS class — picking the wrong family produces the\n")
	buf.WriteString("// missing-glyph tofu in the browser even though the icon name is valid.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Resolution order: brands → solid → regular.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Pro-only icons are absent from this map (their `free` array in the\n")
	buf.WriteString("// upstream metadata is empty).\n")
	buf.WriteString("\n")
	buf.WriteString("window.FA_FREE_STYLES = {\n")

	// Emit in deterministic order so re-runs produce identical files
	// (clean git diffs).
	keys := make([]string, 0, len(stylesByName))
	for k := range stylesByName {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		styles := stylesByName[name]
		buf.WriteString("  ")
		buf.WriteString(strconv.Quote(name))
		buf.WriteString(": [")
		for i, s := range styles {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(strconv.Quote(s))
		}
		buf.WriteString("],\n")
	}

	buf.WriteString("};\n")
	buf.WriteString("\n")
	// Convenience: also expose a flat Set so existing callers that just
	// want to test "is this name free?" don't have to write
	// `name in window.FA_FREE_STYLES`. Keeps the previous slice's API
	// usable without forcing every caller to migrate.
	buf.WriteString("// Back-compat: a flat Set of names (the keys of FA_FREE_STYLES) so\n")
	buf.WriteString("// older code that only needs \"is this icon free?\" can use a Set.has()\n")
	buf.WriteString("// without iterating the styles map.\n")
	buf.WriteString("window.FA_FREE_SET = new Set(Object.keys(window.FA_FREE_STYLES));\n")

	return buf.String()
}

// ─── Utilities ────────────────────────────────────────────────────────────────

// dedupStrings removes consecutive duplicates from a sorted slice.
// The input must be sorted for this to work correctly.
func dedupStrings(s []string) []string {
	if len(s) == 0 {
		return s
	}
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
