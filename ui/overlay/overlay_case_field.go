// /ide/ui/overlay/overlay_case_field.go

package overlay

// overlay_case_field.go — Rendering logic for FieldCaseEditor form inputs.
//
// FieldCaseEditor is the bespoke editor for a StatementCase device's ordered
// list of cases. A switch/case form input cannot be expressed with the generic
// FieldSlice (one primitive per row) or FieldMap (key→value) renderers, because
// every case row carries several heterogeneous controls:
//
//	┌── flex column ───────────────────────────────────────────────────────┐
//	│ [ label ] [ when input ▾ ] [ values ] [ ✓default ] [ ↑ ] [ ↓ ] [ ✕ ] │
//	│ [ label ] [ when input ▾ ] [ values ] [ ✓default ] [ ↑ ] [ ↓ ] [ ✕ ] │
//	│ ...                                                                   │
//	│ [ + Add case ]                                                        │
//	└───────────────────────────────────────────────────────────────────────┘
//
// "when input" is the match kind. Its value drives how "values" is read:
//
//	is            → values[0]                 (exactly one)
//	is any of     → all of values             (one or more)
//	between       → values[0]=lo, values[1]=hi (exactly two: "lo, hi")
//	greater than  → values[0]                 (exactly one)
//	less than     → values[0]
//	at least      → values[0]                 (>=)
//	at most       → values[0]                 (<=)
//
// To keep the v1 row a FIXED shape (no adaptive DOM rebuild, which is the most
// error-prone part of a hand-built editor), "values" is always a single text
// input holding comma-separated integers. The match-kind <select> only changes
// the input's placeholder and the per-row validation rule — not the number of
// inputs. A future slice may split "between" into separate min/max boxes; the
// JSON storage contract below does not change when it does.
//
// Storage (identical pattern to FieldSlice / FieldMap):
//
//   1. Field.Value carries a JSON array on input — the device's GetInspectConfig
//      serialises its cases as [{ "id", "label", "matchKind", "values":[...],
//      "isDefault" }]. "ids" (child membership) is deliberately NOT part of the
//      editor payload: the overlay edits case DEFINITIONS only; which child
//      belongs to which case is owned by the device's spatial assignment and
//      the import restore, never by this form. Empty / fresh fields use "[]".
//   2. A hidden <input> is rewritten on every edit (keystroke, kind change,
//      default toggle, add, remove, move). renderForm's doSave loop collects it
//      under Field.Key like any text field — no change to renderForm.
//   3. On render, the JSON is parsed once to seed the rows.
//   4. New rows are written with an EMPTY "id". The device's ApplyProperties
//      mints a fresh id for empty-id cases (it alone knows the device id) and
//      reconciles by id so surviving cases keep their child membership.
//
// Order matters: the DOM row order IS the array order, and a Case with any
// range/comparison kind lowers to a first-match-wins if/else-if chain, so the
// ↑/↓ buttons let the user reorder without recreating a row.
//
// Field.ValueType carries the selector type ("int" or "bool"), which the device
// derives from the wire. A bool selector has a fixed true/false shape and
// lowers to if/else, so the editor renders a read-only explanation instead of
// the editable list — there is nothing for the maker to add or reorder.
//
// Validation in this slice is per-row and visual only: a row whose values do
// not parse as integers, or whose count does not match the kind, gets a red
// border. Cross-case validation (a value claimed by two cases, an unreachable
// case) and blocking the overlay's Save button are deferred to the next slice,
// together with the live "generates" code preview.
//
// Português: Editor sob medida para a lista ordenada de cases de um
// StatementCase. Não cabe no FieldSlice (um primitivo por linha) nem no
// FieldMap (chave→valor): cada linha tem label + tipo de match + valores +
// default + reordenar. Para a v1, a linha tem formato FIXO (sem reconstruir
// DOM) — "values" é sempre um texto com inteiros separados por vírgula, e o
// <select> de tipo só muda o placeholder e a validação. Armazenamento igual ao
// do FieldSlice (JSON num input oculto coletado pelo doSave). O editor mexe só
// na DEFINIÇÃO dos cases — a associação de filhos é do device/restore. Linhas
// novas vão com "id" vazio; o ApplyProperties do device cria o id. bool vira um
// texto explicativo read-only (true/false, vira if/else). Validação cruzada,
// bloqueio do Save e o preview "generates" ficam para a próxima fatia.

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"syscall/js"
)

// caseEditorRow is the JSON shape of one case as the editor reads and writes
// it. It mirrors the device's caseEntry minus the child ids (membership), which
// the overlay never edits. Field names match the keys GetProperties writes so a
// device's serialised cases hydrate the editor directly.
//
// Português: Forma JSON de um case no editor — espelha o caseEntry do device
// sem os ids dos filhos (associação), que o overlay não edita.
type caseEditorRow struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	MatchKind string   `json:"matchKind"`
	Values    []string `json:"values"`
	IsDefault bool     `json:"isDefault"`
}

// caseMatchOption is one entry of the "when input" <select>.
type caseMatchOption struct {
	value string // stored matchKind ("is", "between", "gt", …)
	label string // human label ("is", "between", "greater than", …)
}

// caseMatchOptions returns the match kinds in the order they appear in the
// dropdown. The values are the matchKind strings the codegen understands; the
// labels are the maker-facing wording agreed for the overlay.
//
// Português: Tipos de match na ordem do dropdown. value = matchKind do codegen;
// label = texto para o maker.
func caseMatchOptions() []caseMatchOption {
	return []caseMatchOption{
		{"is", "is"},
		{"isAnyOf", "is any of"},
		{"between", "between"},
		{"gt", "greater than"},
		{"lt", "less than"},
		{"gte", "at least"},
		{"lte", "at most"},
	}
}

// caseMatchOptionsForType returns the match kinds offered for a given selector
// type. A string selector matches by equality only (is / is any of) — the
// relational/range kinds are meaningless for text. A float selector is the
// opposite: it must NEVER use the exact-equality kinds (there is no float
// switch in C and float equality is fragile), so only the range/relational
// kinds are offered. int (and enum) gets the full set. bool never reaches here
// (it has its own read-only branch with no kind dropdown).
//
// Português: Tipos de match oferecidos por tipo de seletor. string casa só por
// igualdade (is / is any of) — relacional/faixa não faz sentido para texto.
// float é o oposto: nunca usa igualdade exata (não há switch de float em C e
// igualdade de float é frágil), então só relacional/faixa. int (e enum) recebe
// o conjunto completo. bool não chega aqui (tem ramo próprio sem dropdown).
func caseMatchOptionsForType(selectorType string) []caseMatchOption {
	switch selectorType {
	case "string":
		return []caseMatchOption{
			{"is", "is"},
			{"isAnyOf", "is any of"},
		}
	case "float":
		return []caseMatchOption{
			{"between", "between"},
			{"gt", "greater than"},
			{"lt", "less than"},
			{"gte", "at least"},
			{"lte", "at most"},
		}
	default: // int, enum
		return caseMatchOptions()
	}
}

// caseValuesPlaceholder returns the hint shown in the values input for a given
// selector type and match kind, telling the maker what to type and how many.
//
// Português: Dica do campo de valores conforme o tipo de seletor e de match.
func caseValuesPlaceholder(selectorType, matchKind string) string {
	if selectorType == "string" {
		if matchKind == "isAnyOf" {
			return "e.g. red, green"
		}
		return "text"
	}
	switch matchKind {
	case "between":
		return "min, max"
	case "isAnyOf":
		return "e.g. 1, 2, 3"
	default:
		return "value"
	}
}

// caseValuesValid reports whether values is well-formed for the selector type
// and match kind. Each element must parse for the type — any non-empty text for
// string, a float for float, an integer for int — and the count must match the
// kind (one for is/gt/lt/gte/lte, exactly two for between, one or more for
// isAnyOf). It powers the per-row red-border marker; it is intentionally lenient
// about a default case (the caller skips validation for default rows).
//
// Português: Diz se values é válido para o tipo de seletor e o matchKind. Cada
// elemento deve casar com o tipo — texto não-vazio para string, float para
// float, inteiro para int — e a contagem deve bater com o kind. Alimenta a
// borda vermelha por linha.
func caseValuesValid(selectorType, matchKind string, values []string) bool {
	for _, v := range values {
		v = strings.TrimSpace(v)
		switch selectorType {
		case "string":
			if v == "" {
				return false
			}
		case "float":
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return false
			}
		default: // int, enum
			if _, err := strconv.Atoi(v); err != nil {
				return false
			}
		}
	}
	switch matchKind {
	case "between":
		return len(values) == 2
	case "isAnyOf":
		return len(values) >= 1
	default: // is, gt, lt, gte, lte
		return len(values) == 1
	}
}

// splitCaseValues turns the comma-separated values input into a trimmed
// []string, dropping empty fragments so "1, 2, " yields ["1","2"].
//
// Português: Quebra o texto de valores (vírgula) em []string aparada, sem
// fragmentos vazios.
func splitCaseValues(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// buildCaseEditorField creates the visible UI for the case editor and a hidden
// input whose .value carries the JSON-serialised []caseEditorRow for doSave().
//
// Returns:
//   - container:   the visible DOM element to append to the form row
//   - hiddenInput: <input type="hidden"> whose .value holds the JSON
//
// Português: Cria a UI do editor de cases e um input oculto com o JSON para o
// doSave() coletar.
func buildCaseEditorField(doc js.Value, field Field) (container js.Value, hiddenInput js.Value) {
	hiddenInput = doc.Call("createElement", "input")
	hiddenInput.Set("type", "hidden")

	initialJSON := strings.TrimSpace(field.Value)
	if initialJSON == "" {
		initialJSON = "[]"
	}
	hiddenInput.Set("value", initialJSON)

	container = doc.Call("createElement", "div")
	container.Get("style").Set("cssText",
		"flex:1;display:flex;flex-direction:column;gap:8px;min-width:0;")

	// Selector-type badge — read-only, mirrors the device's auto-detected wire
	// type. Blue for int, peach for bool, matching the connector colour family.
	selectorType := field.ValueType
	if selectorType == "" {
		selectorType = "int"
	}
	badgeColor := colBlue
	if selectorType == "bool" {
		badgeColor = colPeach
	}
	badge := doc.Call("createElement", "div")
	badge.Get("style").Set("cssText", fmt.Sprintf(
		"display:inline-flex;align-self:flex-start;align-items:center;gap:6px;"+
			"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:3px 9px;font-size:11px;font-family:sans-serif;",
		colSurface0, colSubtext, colSurface1))
	dot := doc.Call("createElement", "span")
	dot.Get("style").Set("cssText", fmt.Sprintf(
		"width:8px;height:8px;border-radius:50%%;background:%s;", badgeColor))
	badge.Call("appendChild", dot)
	badgeText := doc.Call("createElement", "span")
	badgeText.Set("textContent", "selector: "+selectorType+" — from the connected wire")
	badge.Call("appendChild", badgeText)
	container.Call("appendChild", badge)

	// A bool selector is exhaustive (true/false) and lowers to if/else — there
	// is no list to edit, add to, or reorder. Show a read-only explanation and
	// leave the hidden input untouched (the device keeps its true/false cases).
	if selectorType == "bool" {
		note := doc.Call("createElement", "div")
		note.Get("style").Set("cssText", fmt.Sprintf(
			"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
				"padding:10px 12px;font-size:12px;line-height:1.6;"+
				"font-family:sans-serif;",
			colMantle, colSubtext, colSurface1))
		note.Set("innerHTML",
			"A <strong>boolean</strong> selector has two fixed cases — "+
				"<strong>true</strong> and <strong>false</strong> — and generates "+
				"an <code>if / else</code>. There is nothing to add or reorder here. "+
				"Rewire the selector to an integer to edit multiple cases.")
		container.Call("appendChild", note)
		return container, hiddenInput
	}

	// rowsHost collects case rows. DOM order IS array order.
	rowsHost := doc.Call("createElement", "div")
	rowsHost.Get("style").Set("cssText",
		"display:flex;flex-direction:column;gap:6px;")
	container.Call("appendChild", rowsHost)

	addBtn := doc.Call("createElement", "button")
	addBtn.Get("style").Set("cssText", fmt.Sprintf(
		"align-self:flex-start;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:5px 11px;cursor:pointer;font-size:11px;"+
			"font-weight:500;font-family:sans-serif;transition:opacity 0.15s;",
		colSurface0, colText, colSurface1))
	addBtn.Set("textContent", "+ Add case")
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

	// resort scans rows in DOM order and rewrites the hidden input as a JSON
	// array of caseEditorRow. Called on every edit. It also refreshes each
	// row's per-row validity marker (red border on malformed values).
	resort := func() {
		rows := rowsHost.Call("querySelectorAll", "[data-cr]")
		out := make([]caseEditorRow, 0, rows.Length())
		for i := 0; i < rows.Length(); i++ {
			row := rows.Index(i)
			labelEl := row.Call("querySelector", "[data-c-label]")
			kindEl := row.Call("querySelector", "[data-c-kind]")
			valuesEl := row.Call("querySelector", "[data-c-values]")
			defaultEl := row.Call("querySelector", "[data-c-default]")

			kind := kindEl.Get("value").String()
			values := splitCaseValues(valuesEl.Get("value").String())
			isDefault := defaultEl.Get("checked").Bool()

			// A default case is the catch-all `else` / `default:` — its values
			// are irrelevant, so skip the marker and store no values for it.
			if isDefault {
				values = nil
				valuesEl.Get("style").Set("borderColor", colSurface1)
			} else if caseValuesValid(selectorType, kind, values) {
				valuesEl.Get("style").Set("borderColor", colSurface1)
			} else {
				valuesEl.Get("style").Set("borderColor", colRed)
			}

			out = append(out, caseEditorRow{
				ID:        row.Get("dataset").Get("crId").String(),
				Label:     labelEl.Get("value").String(),
				MatchKind: kind,
				Values:    values,
				IsDefault: isDefault,
			})
		}
		buf, err := json.Marshal(out)
		if err != nil {
			log.Printf("[overlay/case] %s: JSON encode failed: %v", field.Key, err)
			return
		}
		hiddenInput.Set("value", string(buf))
	}

	// addRow appends one editable case row, seeded from r.
	var addRow func(r caseEditorRow)
	addRow = func(r caseEditorRow) {
		row := doc.Call("createElement", "div")
		row.Get("dataset").Set("cr", "1")    // marker for the resort scan
		row.Get("dataset").Set("crId", r.ID) // "" for a fresh row → device mints id
		row.Get("style").Set("cssText",
			"display:flex;align-items:center;gap:4px;min-width:0;")

		// Label input.
		labelInput := doc.Call("createElement", "input")
		labelInput.Set("type", "text")
		labelInput.Get("dataset").Set("cLabel", "1")
		labelInput.Set("value", r.Label)
		labelInput.Set("placeholder", "label")
		labelInput.Get("style").Set("cssText", caseCellStyle("flex:0 1 130px;min-width:48px;"))
		row.Call("appendChild", labelInput)

		// Match-kind <select>.
		kindSelect := doc.Call("createElement", "select")
		kindSelect.Get("dataset").Set("cKind", "1")
		kindSelect.Get("style").Set("cssText", caseCellStyle("width:108px;cursor:pointer;"))
		for _, opt := range caseMatchOptionsForType(selectorType) {
			option := doc.Call("createElement", "option")
			option.Set("value", opt.value)
			option.Set("textContent", opt.label)
			if opt.value == r.MatchKind {
				option.Set("selected", true)
			}
			kindSelect.Call("appendChild", option)
		}
		row.Call("appendChild", kindSelect)

		// Values input (comma-separated integers).
		valuesInput := doc.Call("createElement", "input")
		valuesInput.Set("type", "text")
		valuesInput.Get("dataset").Set("cValues", "1")
		valuesInput.Set("value", strings.Join(r.Values, ", "))
		valuesInput.Set("placeholder", caseValuesPlaceholder(selectorType, r.MatchKind))
		valuesInput.Get("style").Set("cssText", caseCellStyle("flex:0 1 150px;min-width:56px;"))
		row.Call("appendChild", valuesInput)

		// Default checkbox + label. Selecting it makes this the catch-all case
		// and clears every other row's default (radio-like exclusivity).
		defaultWrap := doc.Call("createElement", "label")
		// margin-left:auto pushes the default checkbox and the ↑ ↓ ✕ buttons
		// after it to the right edge: with the label/value inputs no longer
		// growing (flex-grow:0) the slack collects here instead of stretching
		// the inputs, so the controls stay pinned right and the row never grows
		// wide enough to need a horizontal scrollbar.
		//
		// Português: margin-left:auto empurra o "default" e os botões ↑ ↓ ✕
		// para a direita. Como os inputs não crescem mais, a sobra fica aqui em
		// vez de esticar os inputs — controles ancorados à direita e sem barra
		// de rolagem horizontal.
		defaultWrap.Get("style").Set("cssText", fmt.Sprintf(
			"display:flex;align-items:center;gap:4px;margin-left:auto;color:%s;font-size:11px;"+
				"font-family:sans-serif;cursor:pointer;white-space:nowrap;", colSubtext))
		defaultBox := doc.Call("createElement", "input")
		defaultBox.Set("type", "checkbox")
		defaultBox.Get("dataset").Set("cDefault", "1")
		defaultBox.Set("checked", r.IsDefault)
		defaultBox.Get("style").Set("cssText",
			"width:14px;height:14px;accent-color:"+colPeach+";cursor:pointer;")
		defaultWrap.Call("appendChild", defaultBox)
		defaultText := doc.Call("createElement", "span")
		defaultText.Set("textContent", "default")
		defaultWrap.Call("appendChild", defaultText)
		row.Call("appendChild", defaultWrap)

		// applyDefaultDisabled greys the kind+values of a default row, since a
		// catch-all matches "everything else" and ignores its values.
		applyDefaultDisabled := func() {
			on := defaultBox.Get("checked").Bool()
			kindSelect.Set("disabled", on)
			valuesInput.Set("disabled", on)
			opacity := "1"
			if on {
				opacity = "0.45"
			}
			kindSelect.Get("style").Set("opacity", opacity)
			valuesInput.Get("style").Set("opacity", opacity)
		}
		applyDefaultDisabled()

		// Wire edits → resort.
		notify := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resort()
			return nil
		})
		labelInput.Call("addEventListener", "input", notify)
		valuesInput.Call("addEventListener", "input", notify)
		valuesInput.Call("addEventListener", "change", notify)

		// Kind change refreshes the placeholder before re-serialising.
		kindSelect.Call("addEventListener", "change",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				valuesInput.Set("placeholder",
					caseValuesPlaceholder(selectorType, kindSelect.Get("value").String()))
				resort()
				return nil
			}))

		// Default toggle enforces single-default and refreshes the disabled
		// state of all rows, then re-serialises.
		defaultBox.Call("addEventListener", "change",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				if defaultBox.Get("checked").Bool() {
					boxes := rowsHost.Call("querySelectorAll", "[data-c-default]")
					for i := 0; i < boxes.Length(); i++ {
						b := boxes.Index(i)
						if !b.Equal(defaultBox) {
							b.Set("checked", false)
						}
					}
				}
				// Refresh every row's disabled state (this row and any that was
				// just unset). dispatchEvent would be cleaner but a direct scan
				// keeps the handler self-contained.
				rows := rowsHost.Call("querySelectorAll", "[data-cr]")
				for i := 0; i < rows.Length(); i++ {
					rrow := rows.Index(i)
					rbox := rrow.Call("querySelector", "[data-c-default]")
					rkind := rrow.Call("querySelector", "[data-c-kind]")
					rvals := rrow.Call("querySelector", "[data-c-values]")
					ron := rbox.Get("checked").Bool()
					rkind.Set("disabled", ron)
					rvals.Set("disabled", ron)
					rop := "1"
					if ron {
						rop = "0.45"
					}
					rkind.Get("style").Set("opacity", rop)
					rvals.Get("style").Set("opacity", rop)
				}
				resort()
				return nil
			}))

		// ↑ / ↓ reorder (DOM order = array order), mirroring the slice editor.
		upBtn := doc.Call("createElement", "button")
		upBtn.Get("style").Set("cssText", caseMoveBtnStyle())
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

		downBtn := doc.Call("createElement", "button")
		downBtn.Get("style").Set("cssText", caseMoveBtnStyle())
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

		// ✕ remove.
		removeBtn := doc.Call("createElement", "button")
		removeBtn.Get("style").Set("cssText", fmt.Sprintf(
			"background:transparent;color:%s;border:none;cursor:pointer;"+
				"font-size:14px;width:22px;height:22px;display:flex;"+
				"align-items:center;justify-content:center;border-radius:4px;"+
				"transition:background 0.15s;", colRed))
		removeBtn.Set("innerHTML", "&times;")
		removeBtn.Set("title", "Remove case")
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
	var initial []caseEditorRow
	if err := json.Unmarshal([]byte(initialJSON), &initial); err != nil {
		log.Printf("[overlay/case] %s: invalid initial JSON %q: %v — starting empty",
			field.Key, initialJSON, err)
	} else {
		for _, r := range initial {
			if r.MatchKind == "" {
				// Backfill a legacy case the same way the device/codegen do.
				if len(r.Values) > 1 {
					r.MatchKind = "isAnyOf"
				} else {
					r.MatchKind = "is"
				}
			}
			addRow(r)
		}
	}

	addBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			addRow(caseEditorRow{MatchKind: "is"})
			resort()
			return nil
		}))

	return container, hiddenInput
}

// caseCellStyle returns the base style for a row input/select, with extra rules
// appended (e.g. width or flex). Matches the dark form palette used elsewhere
// in the overlay so the editor reads as native.
//
// Português: Estilo base de um input/select da linha, com regras extras
// concatenadas. Combina com a paleta escura do overlay.
func caseCellStyle(extra string) string {
	return fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:5px 6px;font-size:12px;font-family:sans-serif;"+
			"box-sizing:border-box;outline:none;%s",
		colMantle, colText, colSurface1, extra)
}

// caseMoveBtnStyle is the style for the ↑ and ↓ reorder buttons — compact and
// unobtrusive, identical in spirit to the slice editor's move buttons.
//
// Português: Estilo dos botões ↑/↓ de reordenar — compactos, no mesmo espírito
// do editor de slice.
func caseMoveBtnStyle() string {
	return fmt.Sprintf(
		"background:transparent;color:%s;border:none;cursor:pointer;"+
			"font-size:12px;width:20px;height:20px;display:flex;"+
			"align-items:center;justify-content:center;border-radius:4px;"+
			"transition:background 0.15s;font-family:sans-serif;",
		colSubtext)
}
