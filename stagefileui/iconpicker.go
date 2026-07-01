// ide/stagefileui/iconpicker.go — FontAwesome Free icon picker used by the
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// stage file Save and Edit dialogs.
//
// Replicates the visual + behavioural pattern of the wizard's icon picker
// (server/public/static/js/pages/projects_wizard.js — _iconPickerHTML,
// _iconGridHTML, _attachIconPicker) on the WASM side. The two are
// intentionally separate code paths because the wizard is server SPA
// JavaScript and this is Go-WASM; the contract is identical, so a maker
// who picked an icon during project creation recognises the same widget
// when saving or editing a stage file.
//
// Dependencies:
//
//   - window.FA_FREE_STYLES (loaded by ide/index.html before the WASM
//     runtime starts) — per-icon map of free font families.
//   - The FontAwesome Free CSS bundle (already loaded by ide/index.html
//     for the menu/overlay tooling), which is what actually paints the
//     glyphs once the picker writes the `fa-solid/regular/brands fa-NAME`
//     class onto an <i> element.
//
// When window.FA_FREE_STYLES is missing — e.g. the script tag was not
// added to a particular page yet — the picker degrades to a bare input.
// The maker can still type a name; only the grid is empty.
//
// Português:
//
//	Componente de seleção de ícones FontAwesome Free para os diálogos
//	de Save e Edit. Replica o picker do wizard de projetos (em JS) no
//	lado WASM. Lê window.FA_FREE_STYLES; sem ele degrada para input puro.

package stagefileui

import (
	"fmt"
	"sort"
	"strings"
	"syscall/js"
)

// gridCap caps the number of cells the picker grid will render at
// once. Matches the wizard's threshold (120). Higher values stutter
// the DOM update because each cell is a real <button> with an <i>
// child; lower values feel cramped on the typical "type 1-2 letters
// then scan" usage. 120 fits ~13 rows of 9 columns in the default
// max-height, which is what we ship in CSS below.
const gridCap = 120

// defaultIconName is the visual fallback for the preview chip when
// the input is empty or holds an unknown name. "cube" is the same
// stand-in the wizard uses. The picker never silently stores
// defaultIconName as the chosen value — Value() returns the input
// content verbatim, so an empty input still means "no choice".
const defaultIconName = "cube"

// IconPicker is the mounted state of a single picker. Build with
// NewIconPicker; query the current selection with Value().
//
// Root is the wrapper element the caller appends inside a dialog
// body. The caller does not poke at input/preview/grid directly —
// they are unexported because the picker manages its own DOM in
// response to typing and clicking.
type IconPicker struct {
	Root js.Value

	input   js.Value
	preview js.Value
	grid    js.Value

	// all is the sorted list of FA Free icon names captured at
	// construction. Pulled once because window.FA_FREE_STYLES is
	// static for the page lifetime; iterating Object.keys on
	// every keystroke would be wasteful for a 6000-entry map.
	all []string
}

// NewIconPicker constructs the picker DOM and appends Root inside
// host. The initial value pre-fills the input AND filters the grid
// to that string, which makes the chosen icon visible without
// scrolling when an Edit dialog opens on an existing file.
//
// Pass initial == "" for a fresh Save dialog — the grid then shows
// the first gridCap icons in alphabetical order.
func NewIconPicker(doc js.Value, host js.Value, initial string) *IconPicker {
	p := &IconPicker{all: faAllFreeIcons()}

	// Root wrapper. Vertical stack: input row above grid.
	root := doc.Call("createElement", "div")
	root.Get("style").Set("cssText",
		"display:flex;flex-direction:column;gap:8px;")
	p.Root = root

	// Input row: text field on the left, live preview chip on the right.
	row := doc.Call("createElement", "div")
	row.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:8px;")

	p.input = doc.Call("createElement", "input")
	p.input.Set("type", "text")
	p.input.Set("placeholder",
		"type to filter — e.g. cube, gauge, microchip")
	p.input.Set("value", initial)
	// Height + padding + font-size kept in lockstep with the
	// surrounding form fields (Name input, Folder select in the
	// Edit dialog). The save/edit dialogs use 44px min-height and
	// 14px font; matching them here keeps the three rows visually
	// aligned without the picker drawing the eye for being short.
	p.input.Get("style").Set("cssText", fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:10px;font-size:14px;"+
			"min-height:44px;box-sizing:border-box;"+
			"font-family:sans-serif;outline:none;",
		colMantle, colText, colSurface1))
	row.Call("appendChild", p.input)

	// Preview chip — shows the icon corresponding to the input's
	// current value (or `cube` when the value isn't a real name).
	// Width matches min-height so the chip stays square next to
	// the 44px input.
	p.preview = doc.Call("createElement", "i")
	p.preview.Get("style").Set("cssText", fmt.Sprintf(
		"width:44px;height:44px;display:flex;align-items:center;"+
			"justify-content:center;font-size:20px;color:%s;"+
			"background:%s;border:1px solid %s;border-radius:4px;"+
			"flex-shrink:0;",
		colBlue, colMantle, colSurface1))
	row.Call("appendChild", p.preview)

	root.Call("appendChild", row)

	// Grid container. 9 columns matches the wizard; max-height makes
	// the grid scrollable rather than letting it push dialog
	// content off-screen when many icons match.
	p.grid = doc.Call("createElement", "div")
	p.grid.Get("style").Set("cssText", fmt.Sprintf(
		"display:grid;grid-template-columns:repeat(9, 1fr);gap:4px;"+
			"padding:8px;background:%s;border:1px solid %s;"+
			"border-radius:4px;max-height:220px;overflow-y:auto;",
		colMantle, colSurface1))
	root.Call("appendChild", p.grid)

	// First render. Filter = initial pre-narrows to the chosen icon
	// when editing an existing file; harmless for fresh saves
	// because initial == "" matches every icon.
	p.refresh(doc)

	// Live filter: every keystroke re-renders preview + grid. The
	// grid re-render is cheap because we cap at gridCap cells and
	// each cell is a small `<button><i></i></button>`.
	p.input.Call("addEventListener", "input",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			p.refresh(doc)
			return nil
		}))

	// Click delegation — set the chosen name on the input and
	// re-render. Delegation means cells don't carry their own
	// listeners; re-renders are pure DOM swaps with no event
	// plumbing to clean up.
	p.grid.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			target := args[0].Get("target").Call("closest", "[data-pick]")
			if !target.Truthy() {
				return nil
			}
			name := target.Get("dataset").Get("pick").String()
			p.input.Set("value", name)
			p.refresh(doc)
			return nil
		}))

	host.Call("appendChild", root)
	return p
}

// Value returns the currently selected icon name — whatever is in
// the input, trimmed. Empty means "no choice"; the caller decides
// whether that maps to no field in a Create request or the
// "__clear__" sentinel in an Update request.
func (p *IconPicker) Value() string {
	return strings.TrimSpace(p.input.Get("value").String())
}

// refresh re-renders preview + grid based on the input's value. It
// is a pure function of input value → DOM state — calling it twice
// in a row produces the same result, which is what makes the
// keystroke and the click paths share the same code.
func (p *IconPicker) refresh(doc js.Value) {
	val := strings.TrimSpace(p.input.Get("value").String())

	// Preview: typed value when it matches a real free icon,
	// otherwise the default. The chip should never be empty —
	// invariant tested by inspecting that p.preview.className
	// always contains a fa-* class after refresh().
	previewName := val
	if previewName == "" || !isFreeIcon(previewName) {
		previewName = defaultIconName
	}
	p.preview.Set("className",
		fmt.Sprintf("%s fa-%s", faIconClass(previewName), previewName))

	// Grid: case-insensitive substring filter, capped at gridCap.
	p.grid.Set("innerHTML", "")
	filter := strings.ToLower(val)
	shown := 0
	total := 0
	for _, name := range p.all {
		if filter != "" && !strings.Contains(name, filter) {
			continue
		}
		total++
		if shown >= gridCap {
			// Count past the cap so the footer hint can report
			// the true match total ("Showing first X of Y").
			continue
		}
		cell := newIconCell(doc, name, name == val)
		p.grid.Call("appendChild", cell)
		shown++
	}

	if total == 0 {
		empty := doc.Call("createElement", "div")
		empty.Get("style").Set("cssText", fmt.Sprintf(
			"grid-column:1/-1;color:%s;font-size:12px;"+
				"text-align:center;padding:12px;",
			colSubtext))
		empty.Set("textContent",
			fmt.Sprintf("No icons match \"%s\"", val))
		p.grid.Call("appendChild", empty)
		return
	}
	if total > gridCap {
		hint := doc.Call("createElement", "div")
		hint.Get("style").Set("cssText", fmt.Sprintf(
			"grid-column:1/-1;color:%s;font-size:11px;"+
				"text-align:center;padding:6px 0 0;",
			colSubtext))
		hint.Set("textContent", fmt.Sprintf(
			"Showing first %d of %d matches — keep typing to narrow.",
			gridCap, total))
		p.grid.Call("appendChild", hint)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// faAllFreeIcons returns the sorted list of FA Free icon names. The
// source is window.FA_FREE_STYLES, populated by /static/js/fa-free-
// styles.js before the WASM starts (ide/index.html). When the
// global is absent (older shell, dev tools blocked the script,
// etc.), the function returns an empty slice — callers degrade to
// a no-grid input rather than crash.
//
// Sorting is alphabetical to keep grid order stable across renders;
// V8's Object.keys happens to preserve insertion order today but
// relying on engine quirks for layout would be brittle.
func faAllFreeIcons() []string {
	obj := js.Global().Get("FA_FREE_STYLES")
	if !obj.Truthy() {
		return nil
	}
	keys := js.Global().Get("Object").Call("keys", obj)
	n := keys.Length()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, keys.Index(i).String())
	}
	sort.Strings(out)
	return out
}

// faIconClass picks the CSS family class for a given icon name.
// Mirrors _faIconClass from the wizard:
//
//	brands → solid → regular
//
// brands wins because the brand glyphs share names with common
// solid icons in some FA releases (e.g. "amazon" exists as both).
// solid is the default visual style and most icons ship in it.
// regular is the fallback that fixes the well-known tofu on icons
// that are outline-only in Free (alarm-clock, aries, aquarius).
//
// Unknown name → fa-solid. That gives the maker a tofu glyph
// instead of nothing, which is what they need to notice the typo.
func faIconClass(name string) string {
	if name == "" {
		return "fa-solid"
	}
	obj := js.Global().Get("FA_FREE_STYLES")
	if !obj.Truthy() {
		return "fa-solid"
	}
	styles := obj.Get(name)
	if !styles.Truthy() {
		return "fa-solid"
	}
	hasBrands := false
	hasSolid := false
	for i := 0; i < styles.Length(); i++ {
		switch styles.Index(i).String() {
		case "brands":
			hasBrands = true
		case "solid":
			hasSolid = true
		}
	}
	if hasBrands {
		return "fa-brands"
	}
	if hasSolid {
		return "fa-solid"
	}
	return "fa-regular"
}

// isFreeIcon reports whether the given name resolves to a real FA
// Free icon (i.e. is a key in FA_FREE_STYLES).
func isFreeIcon(name string) bool {
	if name == "" {
		return false
	}
	obj := js.Global().Get("FA_FREE_STYLES")
	if !obj.Truthy() {
		return false
	}
	return obj.Get(name).Truthy()
}

// newIconCell builds one cell of the grid: a button carrying the
// icon name as data-pick so the grid's click delegate can pull the
// value without per-cell handlers. Selected cells get an accent
// border and the brand-blue foreground so the chosen icon stands
// out in a sea of subtext-grey cells.
func newIconCell(doc js.Value, name string, selected bool) js.Value {
	btn := doc.Call("createElement", "button")
	btn.Set("type", "button")
	btn.Set("title", name)
	btn.Get("dataset").Set("pick", name)
	border := "1px solid transparent"
	bg := colBase
	iconColor := colSubtext
	if selected {
		border = "1px solid " + colBlue
		bg = colSurface0
		iconColor = colBlue
	}
	btn.Get("style").Set("cssText", fmt.Sprintf(
		"aspect-ratio:1;background:%s;border:%s;border-radius:4px;"+
			"display:flex;align-items:center;justify-content:center;"+
			"color:%s;font-size:16px;cursor:pointer;padding:0;",
		bg, border, iconColor))
	i := doc.Call("createElement", "i")
	i.Set("className",
		fmt.Sprintf("%s fa-%s", faIconClass(name), name))
	btn.Call("appendChild", i)
	return btn
}
