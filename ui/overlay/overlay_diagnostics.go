package overlay

// overlay_diagnostics.go — Diagnostics overlay for codegen issues.
//
// ShowDiagnostics renders a floating panel listing every Diagnostic
// produced by the server, grouped by device (format C):
//
//	apds9960Log_1  ⚠ 2 warnings   ✕ 4 errors
//	  ✕ apds9960Log_1.clear: not connected
//	  ✕ apds9960Log_1.red: not connected
//	  ...
//
//	stmLoop_1  ✕ 1 error
//	  ✕ no stop condition connected
//
// Clicking a device header invokes onDeviceClick(deviceID), which the
// caller wires to the camera's FitAll so the canvas pans and zooms to
// enframe the offending device. The overlay stays open through the
// transition so the maker can cross-reference the list with the canvas.
//
// The overlay only appears when there is at least one diagnostic. A
// scene that generates clean code never triggers this window.
//
// Português: Overlay flutuante agrupando diagnósticos por device.
// Clicar no header faz pan+zoom pro device via callback. Só aparece
// quando há pelo menos um diagnóstico.

import (
	"fmt"
	"sort"
	"strings"
	"syscall/js"
)

// ShowDiagnostics opens an overlay displaying every Diagnostic, grouped
// by the first device ID in each Diagnostic.Devices slice. The window
// is sized for legibility — tall enough to show several groups without
// scroll, narrow enough not to cover the canvas.
//
// onDeviceClick receives the device ID of the header the user tapped.
// It may be nil; in that case device headers are rendered non-clickable.
//
// Returns the overlay Handle so callers can close the window
// programmatically (e.g. when the scene changes and a new codegen run
// starts).
//
// Português: Abre o overlay de diagnósticos agrupados por device.
// onDeviceClick é chamado com o ID do device quando o header é clicado.
// ShowDiagnostics opens the structured diagnostics panel.
//
// onDeviceClick receives the list of devices associated with whatever
// header or row the user tapped. It may be nil, in which case the
// items are rendered non-clickable.
//
// proceedLabel and onProceed together control an optional action
// button rendered at the bottom of the panel. When proceedLabel is
// non-empty AND onProceed is non-nil, a button with that label is
// drawn; clicking it runs onProceed THEN closes the overlay so the
// caller can present a follow-up surface (e.g. the generated code).
// Intended for the "warning-only" case: the code IS generated, we
// want the maker to see the warnings first, but if they decide to
// proceed we want a single click to move forward.
//
// When proceedLabel is empty or onProceed is nil, no button appears —
// use this form for hard errors where the maker must fix the scene
// before anything else happens.
//
// Returns the overlay Handle so callers can close the window
// programmatically (e.g. when the scene changes and a new codegen run
// starts).
//
// Português: Abre o overlay de diagnósticos. onDeviceClick recebe a
// lista de devices do item clicado. proceedLabel + onProceed ligam
// um botão opcional "prosseguir assim mesmo" — usado no fluxo de
// warning para o maker decidir avançar.
func ShowDiagnostics(
	title string,
	diags []Diagnostic,
	onDeviceClick func(deviceIDs []string),
	proceedLabel string,
	onProceed func(),
) Handle {
	if len(diags) == 0 {
		return Handle{}
	}

	html := buildDiagnosticsHTML(diags)

	cfg := Config{
		Title:  title,
		Width:  "560px",
		Height: "70vh",
		Tabs: []Tab{
			{
				Label:   "Issues",
				Type:    TabHTML,
				Content: html,
			},
		},
	}

	h := Show(cfg)

	// Wire click handlers on every device header. We delegate through
	// the overlay root element so late-rendered DOM still receives the
	// listener without needing to hunt individual nodes.
	if onDeviceClick != nil {
		attachDiagnosticsClickDelegate(h, onDeviceClick)
	}

	// Render the "proceed anyway" button when the caller wired one.
	// Kept as a DOM-injected footer rather than a tab feature so the
	// overlay's generic Config doesn't grow new options just for this
	// use case — the button is a local concern of the diagnostics
	// surface.
	if proceedLabel != "" && onProceed != nil {
		attachDiagnosticsProceedButton(h, proceedLabel, onProceed)
	}

	return h
}

// buildDiagnosticsHTML assembles the inner HTML of the Issues tab.
// The markup is self-contained: colours come from inline styles, so
// the overlay does not depend on a stylesheet being loaded on the page.
//
// Grouping:
//
//	A diagnostic referencing N devices appears under ALL N device
//	headers, not just the first. This surfaces the diagnostic from
//	every perspective — e.g. a "stop port wired outside" diagnostic
//	with Devices=[loop, producer] shows up when the user looks at
//	the loop AND when they look at the producer. Each row stores the
//	full Devices list in data-scope so clicking any element routes
//	the complete set to the camera for a combined pan+zoom.
//
// Devices are sorted alphabetically for deterministic output.
//
// Português: Cada Diagnostic aparece sob todos os devices em Devices,
// não só o primeiro. Cada linha guarda a lista inteira em data-scope
// para click enviar o set completo pra câmera enquadrar tudo junto.
func buildDiagnosticsHTML(diags []Diagnostic) string {
	// Build groups where key is a single device and the value is the
	// list of diagnostics mentioning that device. Diagnostics with no
	// Devices go under the pseudo-group "(scene)".
	groups := make(map[string][]Diagnostic)
	for _, d := range diags {
		if len(d.Devices) == 0 {
			groups["(scene)"] = append(groups["(scene)"], d)
			continue
		}
		for _, dev := range d.Devices {
			groups[dev] = append(groups[dev], d)
		}
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;font-size:13px;color:#cdd6f4;padding:12px;">`)

	for _, deviceID := range keys {
		entries := groups[deviceID]
		errCount, warnCount := countSeverities(entries)

		clickable := deviceID != "(scene)"
		// The header itself clicks to focus on just this device's
		// bbox — convenient when the user wants to see this one in
		// isolation. Each row underneath will carry the full scope.
		writeGroupHeader(&sb, deviceID, errCount, warnCount, clickable)

		sb.WriteString(`<ul style="list-style:none;margin:0 0 12px 0;padding:0 0 0 8px;">`)
		for _, d := range entries {
			writeDiagnosticRow(&sb, d)
		}
		sb.WriteString(`</ul>`)
	}

	sb.WriteString(`</div>`)
	return sb.String()
}

// writeGroupHeader emits one device header row. When clickable, a
// data-scope attribute carries the device IDs the delegate listener
// will forward to onDeviceClick. For a header the scope is just that
// one device, so clicking it centers the camera on it alone.
//
// Português: Emite o header de um grupo. data-scope contém o(s)
// device(s) alvo do click — só o próprio ID no caso do header.
func writeGroupHeader(sb *strings.Builder, deviceID string, errCount, warnCount int, clickable bool) {
	badges := ""
	if errCount > 0 {
		badges += fmt.Sprintf(
			`<span style="background:#f38ba8;color:#1e1e2e;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;margin-left:8px;">✕ %d</span>`,
			errCount,
		)
	}
	if warnCount > 0 {
		badges += fmt.Sprintf(
			`<span style="background:#f9e2af;color:#1e1e2e;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;margin-left:8px;">⚠ %d</span>`,
			warnCount,
		)
	}

	label := escapeHTML(deviceID)
	style := `display:flex;align-items:center;padding:6px 8px;border-radius:6px;font-weight:600;color:#cba6f7;`
	if clickable {
		style += `cursor:pointer;`
		sb.WriteString(fmt.Sprintf(
			`<div class="diag-click" data-scope="%s" style="%s" title="Click to focus this device on the canvas">%s%s</div>`,
			escapeAttr(deviceID), style, label, badges,
		))
	} else {
		sb.WriteString(fmt.Sprintf(
			`<div style="%s">%s%s</div>`,
			style, label, badges,
		))
	}
}

// writeDiagnosticRow emits one bullet-style row with icon, kind, and
// message. Error icons are red, warnings yellow.
//
// The row carries every device named in the Diagnostic via data-scope
// so clicking it pans+zooms the camera to a bbox that covers them all
// at once — useful for diagnostics that span multiple devices (e.g.
// "stop port wired outside the loop" names both the loop and the
// outside producer).
//
// Português: Emite uma linha de diagnóstico. data-scope lista todos os
// devices do diagnóstico, pra um único click enquadrar tudo junto.
func writeDiagnosticRow(sb *strings.Builder, d Diagnostic) {
	icon := "✕"
	color := "#f38ba8"
	if d.Severity == "warning" {
		icon = "⚠"
		color = "#f9e2af"
	}

	kindLabel := humanizeKind(d.Kind)
	msg := escapeHTML(d.Message)

	clickable := len(d.Devices) > 0
	style := "padding:4px 8px;display:flex;align-items:flex-start;gap:8px;"
	extraAttrs := ""
	if clickable {
		style += "cursor:pointer;"
		extraAttrs = fmt.Sprintf(` class="diag-click" data-scope="%s" title="Click to focus on all devices in this diagnostic"`,
			escapeAttr(strings.Join(d.Devices, ",")))
	}

	sb.WriteString(fmt.Sprintf(
		`<li style="%s"%s>`+
			`<span style="color:%s;flex-shrink:0;font-weight:600;">%s</span>`+
			`<div style="flex:1;min-width:0;">`+
			`<div style="color:#a6adc8;font-size:11px;text-transform:uppercase;letter-spacing:0.5px;margin-bottom:2px;">%s</div>`+
			`<div style="color:#cdd6f4;word-wrap:break-word;">%s</div>`+
			`</div></li>`,
		style, extraAttrs, color, icon, kindLabel, msg,
	))
}

// humanizeKind maps the machine-readable Kind constant to a short
// label that reads naturally in the UI. Unknown kinds pass through
// verbatim so we never fail loudly on a new backend kind.
//
// Português: Traduz Kind interno para label legível.
func humanizeKind(kind string) string {
	switch kind {
	case "scene_parse":
		return "Scene parse"
	case "geometric":
		return "Geometric conflict"
	case "scope_crossing_multi_output":
		return "Scope crossing"
	case "missing_connection":
		return "Missing connection"
	case "missing_stop":
		return "Missing stop condition"
	case "missing_interval":
		return "Missing interval"
	case "missing_condition":
		return "Missing condition"
	case "blackbox_def_missing":
		return "Black-box definition"
	case "unsupported_language":
		return "Unsupported language"
	case "emitter_internal":
		return "Emitter"
	default:
		return kind
	}
}

// countSeverities tallies errors and warnings in a diagnostic slice.
func countSeverities(diags []Diagnostic) (errors int, warnings int) {
	for _, d := range diags {
		if d.Severity == "warning" {
			warnings++
		} else {
			errors++
		}
	}
	return
}

// escapeHTML replaces the five XML-reserved characters so arbitrary
// strings (like error messages that might contain angle brackets or
// ampersands) can be safely interpolated into innerHTML.
//
// Português: Escapa caracteres especiais para interpolar texto em
// innerHTML com segurança.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// escapeAttr escapes a string for use inside an HTML attribute. In
// practice the same replacements as escapeHTML are sufficient because
// we always quote attribute values with `"`.
func escapeAttr(s string) string {
	return escapeHTML(s)
}

// attachDiagnosticsClickDelegate installs a single click listener on
// the overlay's panel element that dispatches to onDeviceClick
// whenever the user taps any element marked with the diag-click
// class. Event delegation lets us bind once instead of attaching a
// listener per item, and survives if the HTML is later re-rendered.
//
// The clicked element carries its target devices in data-scope as a
// comma-separated list. The listener parses this into a slice and
// forwards it so the caller can frame the whole set together (a
// diagnostic may name several devices — e.g. a loop and an outside
// producer — and the user benefits from seeing them all at once).
//
// Português: Listener delegado no painel. Lê data-scope (CSV de IDs)
// e repassa a lista completa pra onDeviceClick.
func attachDiagnosticsClickDelegate(h Handle, onDeviceClick func([]string)) {
	if !h.Panel.Truthy() {
		return
	}

	var listener js.Func
	listener = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) == 0 {
			return nil
		}
		evt := args[0]
		target := evt.Get("target")
		if !target.Truthy() {
			return nil
		}
		// Walk up the DOM tree until we find an element with
		// data-scope, or we exit the panel. Clicking any child of a
		// clickable row (badge, label, message) triggers the action.
		node := target
		for node.Truthy() {
			if node.Get("nodeType").Int() == 1 { // ELEMENT_NODE
				scope := node.Call("getAttribute", "data-scope")
				if scope.Truthy() {
					parts := strings.Split(scope.String(), ",")
					cleaned := parts[:0]
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							cleaned = append(cleaned, p)
						}
					}
					if len(cleaned) > 0 {
						onDeviceClick(cleaned)
					}
					return nil
				}
			}
			parent := node.Get("parentNode")
			if !parent.Truthy() || parent.Equal(h.Panel) {
				break
			}
			node = parent
		}
		return nil
	})

	h.Panel.Call("addEventListener", "click", listener)

	// The listener is leaked here by design: it lives as long as the
	// overlay panel itself lives. When the panel is removed from the
	// DOM (on close), the GC will collect the listener and the browser
	// drops the reference. For stricter lifetime management, Handle
	// would need an OnClose hook — not added yet because the scene
	// re-runs codegen frequently and the overhead is negligible.
	//
	// Português: Listener permanece até o painel ser removido do DOM.
	_ = listener
}

// attachDiagnosticsProceedButton injects a footer button into the
// diagnostics tab's content container. Clicking it fires onProceed
// and then closes the overlay, giving the caller one hook to chain
// the follow-up surface (typically "now show the code").
//
// The button lives inside the tab content rather than in the title
// bar so it sits below the warnings list — the reading order is:
// warnings first, then the decision button. Styling matches the
// red/peach/yellow theme of the diagnostics so it looks like it
// belongs, with an amber background to telegraph "this proceeds
// past advisory content".
//
// Português: Injeta um botão de rodapé no tab de diagnósticos.
// Clicar dispara onProceed e depois fecha o overlay.
func attachDiagnosticsProceedButton(h Handle, label string, onProceed func()) {
	if !h.Panel.Truthy() || onProceed == nil {
		return
	}

	doc := js.Global().Get("document")

	// The diagnostics HTML lives inside the tab's content div. Find
	// the outermost wrapper we emitted in buildDiagnosticsHTML (it's
	// the first direct child with font-family set) — simpler path is
	// to query the panel for its first div descendant carrying the
	// known inline style, and append the button there. If the query
	// comes back empty for any reason, we fall back to appending on
	// the panel itself so the button is at least visible.
	content := h.Panel.Call("querySelector", `[style*="font-family:-apple-system"]`)
	target := content
	if !target.Truthy() {
		target = h.Panel
	}

	btn := doc.Call("createElement", "button")
	btn.Set("textContent", label)
	btn.Get("style").Set("cssText",
		`margin-top:16px;`+
			`width:100%;`+
			`padding:10px 16px;`+
			`background:#f9e2af;`+ // Catppuccin Mocha yellow — same family as warning badges
			`color:#1e1e2e;`+
			`border:none;`+
			`border-radius:6px;`+
			`font-weight:600;`+
			`font-size:13px;`+
			`cursor:pointer;`+
			`font-family:inherit;`)

	// Darker amber on hover for affordance.
	hoverIn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		this.Get("style").Set("background", "#f5c560")
		return nil
	})
	hoverOut := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		this.Get("style").Set("background", "#f9e2af")
		return nil
	})
	btn.Call("addEventListener", "mouseenter", hoverIn)
	btn.Call("addEventListener", "mouseleave", hoverOut)

	click := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		// Run callback FIRST, then close — this gives the callback a
		// window where h.Panel is still in the DOM, which callers may
		// need (unlikely but cheaply safe).
		onProceed()
		if h.Close != nil {
			h.Close()
		}
		return nil
	})
	btn.Call("addEventListener", "click", click)

	target.Call("appendChild", btn)

	// Listeners are retained for the panel's lifetime by the same
	// leak-on-close strategy documented on attachDiagnosticsClickDelegate.
	_, _, _ = hoverIn, hoverOut, click
}
