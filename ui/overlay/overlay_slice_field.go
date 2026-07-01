// /ide/ui/overlay/overlay_slice_field.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package overlay

// overlay_slice_field.go — Rendering logic for FieldSlice form inputs.
//
// FieldSlice presents an editable list of value rows for the `[]T`
// props introduced by Slice 2.4 of the wizard. Each row has a value
// input, ↑/↓ reorder buttons, and a ✕ remove button. A "+ Add row"
// button at the bottom appends a fresh empty row. The empty state
// shows only the "+ Add row" affordance — same design choice as the
// map renderer.
//
// Why the ↑/↓ buttons instead of drag-and-drop?
//
// The user explicitly chose (c): per-row ↑/↓ buttons. Native HTML5
// drag-and-drop would be more polished, but it costs another layer
// of code (drag image, drop targets, accessibility shims) for a v1
// surface area. Plain buttons are accessible by default, work with
// keyboard, and are trivially testable. A future iteration can
// upgrade to drag without changing the storage contract.
//
// Storage:
//
//   1. Field.Value carries a JSON array on input. Empty / freshly-
//      created fields use "[]". The renderer never produces a
//      malformed JSON: rows with empty values are kept (user's
//      choice); the JSON encoder handles escaping.
//   2. While the user edits, a hidden <input> is updated on every
//      keystroke / row add / row remove / row move. The existing
//      doSave loop in renderForm collects the JSON like any other
//      text field — same pattern as FieldFile and FieldMap.
//   3. On render, the JSON is parsed once to seed the rows.
//
// Order matters in slices (unlike maps), so the row order in the DOM
// IS the order of the JSON array. The ↑/↓ buttons swap a row with
// its neighbour without re-emitting the whole array; only the
// reorder + the resort fire on each click.
//
// Validation:
//
//   - Empty rows are kept. The code generator reproduces them as
//     `""` / `0` / `false` Go literals (the zero value of T).
//   - Numeric inputs use `<input type="number">` so the browser
//     rejects most non-digit input; the encoder still validates.
//   - Bool inputs use real `<input type="checkbox">`.
//
// (ValueType) support matrix in v1:
//
//   ValueType in {string, int, int8…64, uint, uint8…64, byte, rune,
//                 bool, float32, float64}
//                 → row builder UI
//   anything else → inert read-only JSON preview
//
// Future slices may widen support; until then the inert fallback
// keeps unsupported props visible without a crash.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syscall/js"
)

// buildSliceField creates the visible UI for a slice editor and a
// hidden input whose .value carries the JSON-serialised array for
// the form's doSave() to collect.
//
// Returns:
//   - container:   the visible DOM element to append to the form row
//   - hiddenInput: <input type="hidden"> whose .value holds the JSON
//
// The container layout:
//
//	┌── flex column ─────────────────────────────────┐
//	│ row 1: [ value | ↑ | ↓ | ✕ ]                   │
//	│ row 2: [ value | ↑ | ↓ | ✕ ]                   │
//	│ ...                                            │
//	│ [ + Add row ]                                  │
//	└────────────────────────────────────────────────┘
//
// The hidden input is rewritten on every edit. The form's existing
// .value-collection loop in doSave then sees the up-to-date JSON
// without any change to renderForm.
//
// Português: Cria a UI visível para edição de slice []T e um input
// oculto que carrega o JSON serializado para coleta pelo doSave().
func buildSliceField(doc js.Value, field Field) (container js.Value, hiddenInput js.Value) {
	hiddenInput = doc.Call("createElement", "input")
	hiddenInput.Set("type", "hidden")

	// Seed the hidden input with the incoming JSON. Normalise empty
	// to "[]" so downstream parsers always see a well-formed array.
	initialJSON := strings.TrimSpace(field.Value)
	if initialJSON == "" {
		initialJSON = "[]"
	}
	hiddenInput.Set("value", initialJSON)

	container = doc.Call("createElement", "div")
	container.Get("style").Set("cssText",
		"flex:1;display:flex;flex-direction:column;gap:6px;")

	// Decide whether this ValueType is supported. Unsupported fields
	// render as inert JSON preview rather than a crash — gives the
	// user a chance to see the raw value while the renderer catches
	// up in a future slice.
	if !isSupportedSliceShape(field.ValueType) {
		preview := doc.Call("createElement", "code")
		preview.Get("style").Set("cssText", fmt.Sprintf(
			"flex:1;background:%s;color:%s;border:1px solid %s;"+
				"border-radius:4px;padding:6px 8px;font-size:11px;"+
				"font-family:'SF Mono','Consolas',monospace;"+
				"white-space:pre-wrap;word-break:break-all;",
			colMantle, colSubtext, colSurface1))
		preview.Set("textContent",
			fmt.Sprintf("(unsupported []%s — read-only preview)\n%s",
				field.ValueType, initialJSON))
		container.Call("appendChild", preview)
		return container, hiddenInput
	}

	// rowsHost collects the row elements. The DOM order IS the
	// array order — no separate index tracking needed.
	rowsHost := doc.Call("createElement", "div")
	rowsHost.Get("style").Set("cssText",
		"display:flex;flex-direction:column;gap:4px;")
	container.Call("appendChild", rowsHost)

	// Add-row button stays visible whether the slice is empty or
	// full — same UX as the map renderer.
	addBtn := doc.Call("createElement", "button")
	addBtn.Get("style").Set("cssText", fmt.Sprintf(
		"align-self:flex-start;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:4px 10px;cursor:pointer;font-size:11px;"+
			"font-weight:500;font-family:sans-serif;transition:opacity 0.15s;",
		colSurface0, colText, colSurface1))
	addBtn.Set("textContent", "+ Add row")
	addBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			addBtn.Get("style").Set("opacity", "0.85")
			return nil
		}))
	addBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			addBtn.Get("style").Set("opacity", "1")
			return nil
		}))
	container.Call("appendChild", addBtn)

	// resort gathers row values in DOM order and rewrites the
	// hidden input as a JSON array. Called on every keystroke,
	// add, remove, and move (↑/↓).
	resort := func() {
		rows := rowsHost.Call("querySelectorAll", "[data-sr]")
		out := make([]any, 0, rows.Length())
		for i := 0; i < rows.Length(); i++ {
			row := rows.Index(i)
			valEl := row.Call("querySelector", "[data-sv]")
			out = append(out, readMapInputValue(valEl, field.ValueType))
		}
		buf, err := json.Marshal(out)
		if err != nil {
			log.Printf("[overlay/slice] %s: JSON encode failed: %v",
				field.Key, err)
			return
		}
		hiddenInput.Set("value", string(buf))
	}

	// addRow appends one editable row. seed hydrates the input
	// (used when re-rendering an existing slice); pass empty for
	// a fresh row.
	var addRow func(seed string)
	addRow = func(seed string) {
		row := doc.Call("createElement", "div")
		row.Get("dataset").Set("sr", "1") // marker for the resort scan
		row.Get("style").Set("cssText",
			"display:flex;align-items:center;gap:6px;")

		// Reuse buildMapValueInput from overlay_map_field.go — the
		// per-cell input logic is identical (text/number/checkbox
		// per Go primitive type). Sharing the helper keeps the
		// two renderers consistent without duplicate code.
		valInput := buildMapValueInput(doc, field.ValueType, seed)
		valInput.Get("dataset").Set("sv", "1")
		notify := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resort()
			return nil
		})
		valInput.Call("addEventListener", "input", notify)
		valInput.Call("addEventListener", "change", notify)
		row.Call("appendChild", valInput)

		// ↑ button: swaps this row with the previous sibling (if any).
		// First-row ↑ is a no-op (visually still clickable to keep
		// alignment; could be disabled but the no-op cost is zero).
		upBtn := doc.Call("createElement", "button")
		upBtn.Get("style").Set("cssText", sliceMoveBtnStyle())
		upBtn.Set("innerHTML", "&uarr;")
		upBtn.Set("title", "Move up")
		upBtn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				args[0].Call("preventDefault")
				prev := row.Get("previousElementSibling")
				if prev.Truthy() {
					rowsHost.Call("insertBefore", row, prev)
					resort()
				}
				return nil
			}))
		row.Call("appendChild", upBtn)

		// ↓ button: swaps this row with the next sibling (if any).
		// We swap by inserting `next` BEFORE `row` — DOM-order
		// equivalent to moving `row` one step forward.
		downBtn := doc.Call("createElement", "button")
		downBtn.Get("style").Set("cssText", sliceMoveBtnStyle())
		downBtn.Set("innerHTML", "&darr;")
		downBtn.Set("title", "Move down")
		downBtn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				args[0].Call("preventDefault")
				next := row.Get("nextElementSibling")
				if next.Truthy() {
					rowsHost.Call("insertBefore", next, row)
					resort()
				}
				return nil
			}))
		row.Call("appendChild", downBtn)

		// ✕ remove
		removeBtn := doc.Call("createElement", "button")
		removeBtn.Get("style").Set("cssText", fmt.Sprintf(
			"background:transparent;color:%s;border:none;cursor:pointer;"+
				"font-size:14px;width:24px;height:24px;display:flex;"+
				"align-items:center;justify-content:center;border-radius:4px;"+
				"transition:background 0.15s;",
			colRed))
		removeBtn.Set("innerHTML", "&times;")
		removeBtn.Set("title", "Remove row")
		removeBtn.Call("addEventListener", "mouseenter",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				removeBtn.Get("style").Set("background", colSurface1)
				return nil
			}))
		removeBtn.Call("addEventListener", "mouseleave",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				removeBtn.Get("style").Set("background", "transparent")
				return nil
			}))
		removeBtn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				args[0].Call("preventDefault")
				rowsHost.Call("removeChild", row)
				resort()
				return nil
			}))
		row.Call("appendChild", removeBtn)

		rowsHost.Call("appendChild", row)
	}

	// Hydrate from initial JSON.
	var initial []any
	if err := json.Unmarshal([]byte(initialJSON), &initial); err != nil {
		log.Printf("[overlay/slice] %s: invalid initial JSON %q: %v — starting empty",
			field.Key, initialJSON, err)
	} else {
		for _, v := range initial {
			addRow(formatMapInitialValue(v))
		}
	}

	addBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			addRow("")
			resort()
			return nil
		}))

	return container, hiddenInput
}

// isSupportedSliceShape reports whether the renderer can build a row
// UI for the given ValueType. v1 supports the same native scalar set
// as the map renderer (without the KeyType axis since slices have no
// key column).
func isSupportedSliceShape(valueType string) bool {
	switch valueType {
	case "string",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"byte", "rune",
		"bool",
		"float32", "float64":
		return true
	}
	return false
}

// sliceMoveBtnStyle is the style used by ↑ and ↓ buttons. Compact
// and unobtrusive — these are reorder controls, not primary actions.
func sliceMoveBtnStyle() string {
	return fmt.Sprintf(
		"background:transparent;color:%s;border:none;cursor:pointer;"+
			"font-size:12px;width:22px;height:22px;display:flex;"+
			"align-items:center;justify-content:center;border-radius:4px;"+
			"transition:background 0.15s;font-family:sans-serif;",
		colSubtext)
}
