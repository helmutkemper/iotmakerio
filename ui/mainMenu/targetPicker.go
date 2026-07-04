// ui/mainMenu/targetPicker.go — The hardware-target board picker overlay.
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	ShowTargetPicker opens the board selector shown when generating C code. It is
//	a BESPOKE overlay — built directly from DOM nodes rather than the shared
//	ui/overlay chrome — because that chrome is a form window and reads wrong for a
//	hardware chooser. Each board is a card (icon, name, RAM, description); the
//	selected card carries an accent border and a check, and clicking a card moves
//	the selection.
//
//	Below the cards sits a collapsible "Advanced" section, bound to the SELECTED
//	board. It exposes a single knob — the string-concatenation buffer size — as a
//	number plus a unit (bytes / KB / MB), prefilled with the selected board's
//	default and re-filled whenever the selection changes. The maker never has to
//	know that 1 KB is 1024 bytes: they pick a unit. On generate the value is
//	converted to bytes and handed back as an override; the buffer is the only
//	safe knob to expose (snprintf truncates), so the type profile is NOT here.
//
//	Structure: a fixed backdrop holds a centred modal whose inner HTML is the
//	header, the board cards, the advanced section and the footer buttons. A
//	single delegated click listener on the modal handles everything — selecting a
//	card (data-target-id), toggling advanced (data-action="toggle-advanced"),
//	generating (data-action="generate") and cancelling (data-action="cancel") —
//	via Element.closest(), so there is one listener regardless of card count. A
//	second listener on the backdrop cancels on an outside click.
//
//	onChosen is ALWAYS called exactly once, including the failure paths (no target
//	list), where it is called with (current, 0) so the caller proceeds with the
//	existing choice and no override, and generation is never blocked.
//
//	This is browser-only UI (WASM); it cannot be unit-tested offline. The custom
//	DOM is kept flat and the styling mirrors the app's Catppuccin Mocha chrome.
//
// Português:
//
//	Abre o seletor de placas ao gerar código C. Overlay PRÓPRIO (montado do DOM,
//	não o chrome de form do ui/overlay). Cada placa é um card; a selecionada tem
//	borda de acento + check. Abaixo dos cards, uma seção "Advanced" colapsável,
//	amarrada à placa SELECIONADA, expõe um único knob — o tamanho do buffer de
//	string — como número + unidade (bytes/KB/MB), pré-preenchido com o default da
//	placa e re-preenchido ao trocar a seleção. O maker nunca precisa saber que
//	1 KB = 1024 bytes: ele escolhe a unidade. No generate o valor vira bytes e é
//	devolvido como override; o buffer é o único knob seguro (snprintf trunca),
//	então o profile NÃO está aqui. Um listener delegado no modal trata tudo via
//	Element.closest(). onChosen é sempre chamado uma vez, inclusive nas falhas
//	(com current, 0). É UI de browser (WASM), não testável offline.
package mainMenu

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"syscall/js"
)

// Catppuccin Mocha palette, matching the app's overlay and menu chrome. Defined
// locally because the project has no shared palette package; if the theme moves,
// keep these in sync with ui/overlay.
const (
	pickerBase     = "#1e1e2e"                // modal background
	pickerMantle   = "#181825"                // card / input background
	pickerSurface1 = "#45475a"                // borders
	pickerText     = "#cdd6f4"                // primary text
	pickerSubtext  = "#a6adc8"                // secondary text
	pickerOverlay  = "#6c7086"                // muted text (RAM, hints)
	pickerBlue     = "#89b4fa"                // accent — selected card, icons, Generate
	pickerBlueTint = "rgba(137,180,250,0.12)" // selected card fill
	pickerCrust    = "#11111b"                // text on the accent button
)

// ShowTargetPicker opens the board picker and calls onChosen(id, bufferBytes)
// when the maker clicks Generate: id is the picked board, bufferBytes is the
// string-buffer override in bytes (0 when the maker left the advanced field on
// the board's default, so the codegen keeps that default). current is the last
// choice, highlighted on open (pass "" for none). See the package-level doc for
// the once-only / never-block guarantee.
func ShowTargetPicker(current string, onChosen func(id string, bufferBytes int)) {
	targets := LoadTargets()
	if len(targets) == 0 {
		onChosen(current, 0)
		return
	}

	selected := current
	if !containsTargetID(targets, selected) {
		selected = targets[0].ID
	}

	doc := js.Global().Get("document")

	backdrop := doc.Call("createElement", "div")
	backdrop.Get("style").Set("cssText",
		"position:fixed;inset:0;background:rgba(0,0,0,0.5);z-index:100000;"+
			"display:flex;align-items:center;justify-content:center;font-family:sans-serif;")

	modal := doc.Call("createElement", "div")
	modal.Get("style").Set("cssText", fmt.Sprintf(
		"width:480px;max-width:92vw;max-height:88vh;overflow:auto;background:%s;"+
			"border:1px solid %s;border-radius:12px;color:%s;box-shadow:0 16px 48px rgba(0,0,0,0.6);",
		pickerBase, pickerSurface1, pickerText))
	modal.Set("innerHTML", buildPickerHTML(targets, selected))
	backdrop.Call("appendChild", modal)
	doc.Get("body").Call("appendChild", backdrop)

	var funcs []js.Func
	closePicker := func() {
		backdrop.Call("remove")
		for _, f := range funcs {
			f.Release()
		}
	}

	// restyle repaints every card to reflect id as the selected one.
	restyle := func(id string) {
		cards := modal.Call("querySelectorAll", "[data-target-id]")
		for i := 0; i < cards.Get("length").Int(); i++ {
			card := cards.Index(i)
			on := card.Call("getAttribute", "data-target-id").String() == id
			st := card.Get("style")
			check := card.Call("querySelector", "[data-check]")
			if on {
				st.Set("borderColor", pickerBlue)
				st.Set("background", pickerBlueTint)
				if check.Truthy() {
					check.Get("style").Set("visibility", "visible")
				}
			} else {
				st.Set("borderColor", pickerSurface1)
				st.Set("background", pickerMantle)
				if check.Truthy() {
					check.Get("style").Set("visibility", "hidden")
				}
			}
		}
	}

	// fillAdvanced prefills the advanced field with the given board's default
	// buffer size, in bytes. Called on open and whenever the selection changes so
	// the field always shows the selected board's current value. The defaults are
	// small (well under 1 KB), so the unit resets to bytes.
	fillAdvanced := func(id string) {
		def := 0
		for _, t := range targets {
			if t.ID == id {
				def = t.StringBufferSize
				break
			}
		}
		if num := modal.Call("querySelector", "[data-buffer-num]"); num.Truthy() {
			num.Set("value", strconv.Itoa(def))
		}
		if unit := modal.Call("querySelector", "[data-buffer-unit]"); unit.Truthy() {
			unit.Set("value", "b")
		}
	}

	// readBufferBytes reads the advanced field (number + unit) and returns the
	// override in bytes. An empty or non-positive number returns 0, meaning "no
	// override" — the codegen then keeps the board's default. There is no upper
	// clamp: an absurd value is the maker's own choice, and snprintf stays safe
	// regardless (a too-large stack buffer is a compile concern, not a crash).
	readBufferBytes := func() int {
		num := modal.Call("querySelector", "[data-buffer-num]")
		if !num.Truthy() {
			return 0
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(num.Get("value").String()), 64)
		if err != nil || n <= 0 {
			return 0
		}
		mult := 1.0
		if unit := modal.Call("querySelector", "[data-buffer-unit]"); unit.Truthy() {
			switch unit.Get("value").String() {
			case "kb":
				mult = 1024
			case "mb":
				mult = 1024 * 1024
			}
		}
		return int(n * mult)
	}

	// One delegated click listener: actions first (data-action), then card
	// selection (data-target-id), resolved with Element.closest().
	clickFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		t := args[0].Get("target")
		if btn := t.Call("closest", "[data-action]"); btn.Truthy() {
			switch btn.Call("getAttribute", "data-action").String() {
			case "generate":
				id := selected
				bytes := readBufferBytes()
				closePicker()
				onChosen(id, bytes)
			case "cancel":
				closePicker()
			case "toggle-advanced":
				body := modal.Call("querySelector", "[data-advanced-body]")
				chev := modal.Call("querySelector", "[data-adv-chevron]")
				if body.Truthy() {
					if body.Get("style").Get("display").String() == "none" {
						body.Get("style").Set("display", "block")
						if chev.Truthy() {
							chev.Get("style").Set("transform", "rotate(180deg)")
						}
					} else {
						body.Get("style").Set("display", "none")
						if chev.Truthy() {
							chev.Get("style").Set("transform", "rotate(0deg)")
						}
					}
				}
			}
			return nil
		}
		if card := t.Call("closest", "[data-target-id]"); card.Truthy() {
			selected = card.Call("getAttribute", "data-target-id").String()
			restyle(selected)
			fillAdvanced(selected)
		}
		return nil
	})
	funcs = append(funcs, clickFn)
	modal.Call("addEventListener", "click", clickFn)

	// Clicking the backdrop (outside the modal) cancels.
	backFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("target").Equal(backdrop) {
			closePicker()
		}
		return nil
	})
	funcs = append(funcs, backFn)
	backdrop.Call("addEventListener", "click", backFn)
}

// buildPickerHTML renders the modal's inner HTML: header, one card per board,
// the collapsible advanced section, and the footer buttons. Every text value is
// HTML-escaped. Colours are inlined from the palette above so the overlay needs
// no stylesheet.
func buildPickerHTML(targets []TargetView, selected string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf(
		`<div style="padding:18px 20px 14px;">`+
			`<div style="font-size:16px;font-weight:500;color:%s;">Choose your board</div>`+
			`<div style="font-size:13px;color:%s;margin-top:4px;line-height:1.5;">`+
			`The board sets the number sizes and the memory your generated code targets.</div></div>`,
		pickerText, pickerSubtext))

	b.WriteString(`<div style="padding:0 12px;display:flex;flex-direction:column;gap:8px;">`)
	for _, t := range targets {
		border, bg, checkVis := pickerSurface1, pickerMantle, "hidden"
		if t.ID == selected {
			border, bg, checkVis = pickerBlue, pickerBlueTint, "visible"
		}
		ram := ""
		if r := formatRAM(t.RAMBytes); r != "" {
			ram = fmt.Sprintf(`<span style="font-size:13px;color:%s;"> · %s</span>`, pickerOverlay, html.EscapeString(r))
		}
		desc := ""
		if t.Description != "" {
			desc = fmt.Sprintf(`<div style="font-size:13px;color:%s;margin-top:4px;line-height:1.5;">%s</div>`,
				pickerSubtext, html.EscapeString(t.Description))
		}
		b.WriteString(fmt.Sprintf(
			`<div data-target-id="%s" style="display:flex;gap:12px;padding:13px 14px;`+
				`border:1px solid %s;border-radius:10px;cursor:pointer;background:%s;align-items:flex-start;">`+
				`<i class="fa-solid %s" style="font-size:18px;color:%s;width:20px;text-align:center;margin-top:2px;"></i>`+
				`<div style="flex:1;min-width:0;">`+
				`<div><span style="font-weight:500;color:%s;">%s</span>%s</div>%s</div>`+
				`<i data-check class="fa-solid fa-check" style="font-size:16px;color:%s;visibility:%s;margin-top:2px;"></i>`+
				`</div>`,
			html.EscapeString(t.ID), border, bg, faIcon(t.Icon), pickerBlue,
			pickerText, html.EscapeString(t.DisplayName), ram, desc, pickerBlue, checkVis))
	}
	b.WriteString(`</div>`)

	// Prefill the advanced field with the initially-selected board's default.
	def := 0
	for _, t := range targets {
		if t.ID == selected {
			def = t.StringBufferSize
			break
		}
	}
	b.WriteString(fmt.Sprintf(
		`<div style="padding:8px 20px 2px;">`+
			`<div data-action="toggle-advanced" style="display:inline-flex;align-items:center;gap:6px;`+
			`cursor:pointer;color:%s;font-size:13px;user-select:none;">`+
			`<i data-adv-chevron class="fa-solid fa-chevron-down" style="font-size:11px;transition:transform 0.15s;"></i>`+
			`<span>Advanced</span></div>`+
			`<div data-advanced-body style="display:none;margin-top:12px;">`+
			`<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;">`+
			`<div><div style="font-size:13px;color:%s;">String buffer size</div>`+
			`<div style="font-size:12px;color:%s;margin-top:2px;">How much room each joined text gets on this board.</div></div>`+
			`<span style="display:inline-flex;align-items:center;gap:6px;flex-shrink:0;">`+
			`<input data-buffer-num type="text" value="%d" style="width:72px;text-align:right;background:%s;color:%s;`+
			`border:1px solid %s;border-radius:6px;padding:6px 8px;font-size:13px;">`+
			`<select data-buffer-unit style="background:%s;color:%s;border:1px solid %s;border-radius:6px;`+
			`padding:6px 8px;font-size:13px;"><option value="b">bytes</option><option value="kb">KB</option>`+
			`<option value="mb">MB</option></select></span></div></div></div>`,
		pickerSubtext, pickerText, pickerOverlay, def,
		pickerMantle, pickerText, pickerSurface1,
		pickerMantle, pickerText, pickerSurface1))

	b.WriteString(fmt.Sprintf(
		`<div style="padding:14px 20px 18px;display:flex;justify-content:flex-end;gap:10px;">`+
			`<button data-action="cancel" style="padding:8px 16px;background:transparent;color:%s;`+
			`border:1px solid %s;border-radius:8px;cursor:pointer;font-size:13px;">Cancel</button>`+
			`<button data-action="generate" style="padding:8px 16px;background:%s;color:%s;border:none;`+
			`border-radius:8px;cursor:pointer;font-size:13px;font-weight:500;">`+
			`<i class="fa-solid fa-play" style="font-size:11px;margin-right:6px;"></i>Generate code</button></div>`,
		pickerSubtext, pickerSurface1, pickerBlue, pickerCrust))

	return b.String()
}

// faIcon maps a registry icon name to a FontAwesome solid class (the app's icon
// set). Unknown names fall back to a generic chip.
func faIcon(name string) string {
	switch name {
	case "wifi":
		return "fa-wifi"
	case "device-desktop":
		return "fa-desktop"
	case "cpu":
		return "fa-microchip"
	default:
		return "fa-microchip"
	}
}

// containsTargetID reports whether id matches a target in the list.
func containsTargetID(targets []TargetView, id string) bool {
	for _, t := range targets {
		if t.ID == id {
			return true
		}
	}
	return false
}

// formatRAM renders a RAM byte count for display: "2 KB RAM", "512 KB RAM",
// "1 MB RAM". Zero (a target with no meaningful figure, e.g. a desktop) renders
// as "ample RAM" rather than "0 bytes".
func formatRAM(bytes int) string {
	switch {
	case bytes <= 0:
		return "ample RAM"
	case bytes >= 1<<20:
		return fmt.Sprintf("%d MB RAM", bytes/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%d KB RAM", bytes/(1<<10))
	default:
		return fmt.Sprintf("%d bytes RAM", bytes)
	}
}
