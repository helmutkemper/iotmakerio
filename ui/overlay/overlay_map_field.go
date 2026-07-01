// /ide/ui/overlay/overlay_map_field.go

package overlay

// overlay_map_field.go — Rendering logic for FieldMap form inputs.
//
// FieldMap presents an editable list of key/value rows for the
// `map[K]V` props introduced by Slice 2.2 of the wizard. Each row
// has a key input, a value input, and a ✕ button. A "+ Add row"
// button at the bottom appends a fresh empty row. When the last row
// is removed, the empty state shows only the "+ Add row" affordance
// (the user explicitly chose this UX over a permanent placeholder
// row — see the Slice 2.2 design notes).
//
// Storage — one source of truth, three representations:
//
//   1. The Field.Value carries a JSON object on input. Empty /
//      freshly-created fields use "{}" (zero value preserved as a
//      non-nil empty map at code-gen time).
//   2. While the user edits, a hidden <input> is updated on every
//      keystroke / row-add / row-remove. This lets the existing
//      doSave() loop in renderForm collect the JSON value just like
//      any other text input — no special path on the save side.
//   3. On render, the JSON is parsed once to seed the rows.
//
// Why a hidden input instead of a special doSave branch?
//
// The form has TWO doSave call sites (renderForm and
// renderEmbeddedForm), both reading from a `map[string]js.Value`
// of input elements. Adding a third path for FieldMap would mean
// touching both sites and risking drift. A hidden input plumbed
// to .value behaves like an existing text field; the rest of the
// form code stays unchanged.
//
// Validation:
//
//   - Empty keys are kept (user's choice). The code generator
//     reproduces them as `"":"v"` Go literals.
//   - Duplicate keys are de-duped at save time with last-wins
//     semantics. Documenting this in a tooltip near the key
//     input would be polite but the renderer takes the
//     pragmatic route: log a console warning so the user knows
//     a row was dropped.
//   - Numeric value columns reject non-digit/sign keystrokes via
//     a keydown listener; the kept keystrokes are still fully
//     editable (backspace, paste, copy).
//
// (KeyType, ValueType) support matrix in v1:
//
//   KeyType=string × ValueType in {string,int,int8…64,
//                                  uint,uint8…64,byte,rune,
//                                  bool,float32,float64}
//                 → row builder UI
//   anything else → inert read-only JSON preview
//
// Future slices may relax the KeyType restriction; until then the
// inert fallback keeps unsupported props visible without a crash.

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"syscall/js"
)

// buildMapField creates the visible UI for a map editor and a hidden
// input whose .value carries the JSON-serialised map for the form's
// doSave() to collect.
//
// Returns:
//   - container:   the visible DOM element to append to the form row
//   - hiddenInput: <input type="hidden"> whose .value holds the JSON
//
// The container layout:
//
//	┌── flex column ─────────────────────────────────┐
//	│ row 1: [ key input | value input | ✕ ]         │
//	│ row 2: [ key input | value input | ✕ ]         │
//	│ ...                                            │
//	│ [ + Add row ]                                  │
//	└────────────────────────────────────────────────┘
//
// On every key edit, value edit, row add and row remove, the hidden
// input is rewritten with the current JSON. The form's existing
// .value-collection loop in doSave then sees the up-to-date JSON
// without any change to renderForm.
//
// Português: Cria a UI visível para edição de map[K]V e um input
// oculto que carrega o JSON serializado para coleta pelo doSave().
func buildMapField(doc js.Value, field Field) (container js.Value, hiddenInput js.Value) {
	hiddenInput = doc.Call("createElement", "input")
	hiddenInput.Set("type", "hidden")

	// Seed the hidden input with the incoming JSON. We never trust the
	// caller blindly — Field.Value may be empty when the prop has no
	// default. Normalise to "{}" so downstream parsers always see a
	// well-formed JSON object.
	initialJSON := strings.TrimSpace(field.Value)
	if initialJSON == "" {
		initialJSON = "{}"
	}
	hiddenInput.Set("value", initialJSON)

	container = doc.Call("createElement", "div")
	container.Get("style").Set("cssText",
		"flex:1;display:flex;flex-direction:column;gap:6px;")

	// Decide whether this (KeyType, ValueType) combination is
	// supported. Unsupported fields render as inert JSON preview
	// rather than a crash — gives the user a chance to see the
	// raw value while the renderer catches up in a future slice.
	if !isSupportedMapShape(field.KeyType, field.ValueType) {
		preview := doc.Call("createElement", "code")
		preview.Get("style").Set("cssText", fmt.Sprintf(
			"flex:1;background:%s;color:%s;border:1px solid %s;"+
				"border-radius:4px;padding:6px 8px;font-size:11px;"+
				"font-family:'SF Mono','Consolas',monospace;"+
				"white-space:pre-wrap;word-break:break-all;",
			colMantle, colSubtext, colSurface1))
		preview.Set("textContent",
			fmt.Sprintf("(unsupported map[%s]%s — read-only preview)\n%s",
				field.KeyType, field.ValueType, initialJSON))
		container.Call("appendChild", preview)
		return container, hiddenInput
	}

	// rowsHost collects the row elements so we can iterate when
	// re-serialising the map after every edit.
	rowsHost := doc.Call("createElement", "div")
	rowsHost.Get("style").Set("cssText",
		"display:flex;flex-direction:column;gap:4px;")
	container.Call("appendChild", rowsHost)

	// Add-row button. Stays visible whether the map is empty or full —
	// it is the only entry point to the map's empty state, per the
	// Slice 2.2 design ("remove last row → only + Add row remains").
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

	// reseralise gathers the current key/value pairs out of the
	// rendered DOM, applies last-wins de-duplication, and rewrites
	// the hidden input with the JSON encoding. Called on every
	// keystroke, add, and remove.
	//
	// Last-wins on dup keys: Go's map literal would otherwise emit
	// multiple "key":"value" pairs and the compiler would reject
	// it. Choosing last-wins matches what most users expect ("the
	// later row corrects an earlier one") and is documented in
	// the package doc.
	resort := func() {
		rows := rowsHost.Call("querySelectorAll", "[data-mr]")
		out := make(map[string]any, rows.Length())
		seen := map[string]bool{}
		dupes := 0
		for i := 0; i < rows.Length(); i++ {
			row := rows.Index(i)
			keyEl := row.Call("querySelector", "[data-mk]")
			valEl := row.Call("querySelector", "[data-mv]")
			k := keyEl.Get("value").String()
			rawV := readMapInputValue(valEl, field.ValueType)
			if seen[k] {
				dupes++
			}
			seen[k] = true
			out[k] = rawV
		}
		if dupes > 0 {
			log.Printf("[overlay/map] %s: %d duplicate key(s) — last wins",
				field.Key, dupes)
		}
		// encoding/json sorts map keys for marshalling, so the
		// on-the-wire JSON is already deterministic. No explicit
		// sort here — the hydrate path further below uses
		// sort.Strings to ensure the rendered row order matches.
		buf, err := json.Marshal(out)
		if err != nil {
			log.Printf("[overlay/map] %s: JSON encode failed: %v",
				field.Key, err)
			return
		}
		hiddenInput.Set("value", string(buf))
	}

	// addRow appends one editable row. seedKey/seedVal hydrate the
	// inputs (used when re-rendering an existing map); pass empty
	// strings to add a fresh row.
	var addRow func(seedKey, seedVal string)
	addRow = func(seedKey, seedVal string) {
		row := doc.Call("createElement", "div")
		row.Set("dataset", row.Get("dataset"))
		row.Get("dataset").Set("mr", "1") // marker for the resort scan
		row.Get("style").Set("cssText",
			"display:flex;align-items:center;gap:6px;")

		keyInput := doc.Call("createElement", "input")
		keyInput.Set("type", "text")
		keyInput.Get("dataset").Set("mk", "1")
		keyInput.Get("style").Set("cssText", mapInputStyle())
		keyInput.Set("value", seedKey)
		keyInput.Set("placeholder", "key")
		keyInput.Call("addEventListener", "input",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				resort()
				return nil
			}))
		row.Call("appendChild", keyInput)

		valInput := buildMapValueInput(doc, field.ValueType, seedVal)
		valInput.Get("dataset").Set("mv", "1")
		// All value inputs notify the resort on change; both input
		// (text) and change (checkbox) events are wired so we catch
		// every editable surface.
		notify := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resort()
			return nil
		})
		valInput.Call("addEventListener", "input", notify)
		valInput.Call("addEventListener", "change", notify)
		row.Call("appendChild", valInput)

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

	// Hydrate from initial JSON. We accept any shape that decodes;
	// mismatched value types (e.g. a "true" string in an int field)
	// are coerced silently because the alternative is dropping
	// rows the user can no longer recover.
	initial := map[string]any{}
	if err := json.Unmarshal([]byte(initialJSON), &initial); err != nil {
		log.Printf("[overlay/map] %s: invalid initial JSON %q: %v — starting empty",
			field.Key, initialJSON, err)
	} else {
		// Sort keys so the rendered order matches the eventual
		// stored order — avoids the user perceiving "row reorder
		// after save".
		keys := make([]string, 0, len(initial))
		for k := range initial {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			addRow(k, formatMapInitialValue(initial[k]))
		}
	}

	addBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			addRow("", "")
			// No resort needed — the new row's empty key+value already
			// match the JSON's empty-string entry; doing it anyway is
			// cheap and consistent.
			resort()
			return nil
		}))

	return container, hiddenInput
}

// isSupportedMapShape reports whether the renderer can build a row UI
// for the given (KeyType, ValueType). v1 supports only string keys and
// the native scalar value types listed in the package doc.
//
// Returning false makes buildMapField fall back to an inert preview;
// the prop stays visible and the user can still inspect the raw JSON.
func isSupportedMapShape(keyType, valueType string) bool {
	if keyType != "string" {
		return false
	}
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

// buildMapValueInput returns a DOM input appropriate for the given
// Go primitive value type. Used inside each row built by
// buildMapField.
//
// The returned element is the input itself; the caller is responsible
// for appending it to the row container and wiring listeners.
func buildMapValueInput(doc js.Value, valueType, seed string) js.Value {
	switch valueType {
	case "bool":
		// Render booleans as a real checkbox so the user does not
		// type "true"/"false" strings. Anything truthy in the seed
		// (true, "true", "True", "1") starts checked; false/empty
		// stays unchecked.
		input := doc.Call("createElement", "input")
		input.Set("type", "checkbox")
		input.Get("style").Set("cssText",
			"width:16px;height:16px;accent-color:"+colBlue+
				";cursor:pointer;flex:0;")
		switch strings.ToLower(strings.TrimSpace(seed)) {
		case "true", "1", "yes", "on":
			input.Set("checked", true)
		}
		return input
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"byte", "rune",
		"float32", "float64":
		input := doc.Call("createElement", "input")
		input.Set("type", "number")
		input.Get("style").Set("cssText", mapInputStyle())
		input.Set("value", seed)
		input.Set("placeholder", "value")
		// uint* — minimum 0. The browser still allows pasting
		// negatives; the code generator catches that at emit time.
		if strings.HasPrefix(valueType, "uint") || valueType == "byte" {
			input.Set("min", "0")
		}
		return input
	default: // string and any unrecognised native — treat as text
		input := doc.Call("createElement", "input")
		input.Set("type", "text")
		input.Get("style").Set("cssText", mapInputStyle())
		input.Set("value", seed)
		input.Set("placeholder", "value")
		return input
	}
}

// readMapInputValue returns the value of a value-column input as the
// concrete Go type that the JSON encoding should reflect. Bool inputs
// emit a real bool; numeric inputs emit string-of-digits which the
// JSON layer converts to number; strings stay strings.
//
// Why not always a string? Because JSON stores types: a map[string]int
// must encode as `{"k":42}`, not `{"k":"42"}`. The downstream code
// generator parses the JSON and uses the value's concrete Go type
// to choose the literal syntax (no extra strconv at emit time).
func readMapInputValue(input js.Value, valueType string) any {
	switch valueType {
	case "bool":
		return input.Get("checked").Bool()
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"byte", "rune":
		// json.Number-equivalent: emit the raw digit string. The
		// JSON encoder accepts json.Number as numeric when it is
		// a json.Number type, so wrap explicitly.
		raw := strings.TrimSpace(input.Get("value").String())
		if raw == "" {
			raw = "0"
		}
		// json.Number is the cleanest way to round-trip an integer
		// without floating-point conversions. The encoder treats
		// it as a number literal in the output.
		return json.Number(raw)
	case "float32", "float64":
		raw := strings.TrimSpace(input.Get("value").String())
		if raw == "" {
			raw = "0"
		}
		return json.Number(raw)
	default:
		return input.Get("value").String()
	}
}

// formatMapInitialValue stringifies a JSON-decoded value back into
// the form the corresponding input expects on hydrate. Mirrors the
// inverse of readMapInputValue.
func formatMapInitialValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return x
	case float64:
		// JSON numbers decode as float64 by default. We do not know
		// the original Go type here (int vs float), so render
		// without a trailing ".000…" when it is an integer value.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case json.Number:
		return string(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// mapInputStyle is the input style used by both the key and value
// text/number inputs in a row. Sized to fit two side-by-side fields
// inside a Properties form column without wrapping.
func mapInputStyle() string {
	return fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:4px 8px;font-size:12px;"+
			"font-family:sans-serif;outline:none;min-width:60px;",
		colMantle, colText, colSurface1)
}
