// ui/overlay/overlay_progress.go — Progress overlay for long-running tasks.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// ShowProgress opens a centred, modal overlay with three elements:
//
//   - A CSS spinner (no FontAwesome dependency — pure border + keyframes).
//   - A title and a descriptive message.
//   - An elapsed-time counter that updates every second.
//   - An optional Cancel button.
//
// Designed for operations where:
//
//	(a) The user has just clicked "do X" and is now actively waiting.
//	(b) The operation may take seconds to tens of seconds.
//	(c) The user benefits from explicit "still alive" feedback rather
//	    than a mute spinner that could just as well be a frozen tab.
//
// The current caller is the WASM codegen flow in stageWorkspace —
// between clicking "Export → Go Code" and the Monaco overlay opening
// with the generated source. Other long operations (large file
// uploads, multi-step imports) can adopt the same widget.
//
// Lifecycle:
//
//	The returned Handle exposes Close(); calling it removes the
//	overlay from the DOM, clears the JS-side setInterval that drives
//	the counter, and releases every js.FuncOf bound by this widget.
//	Close is idempotent — calling it twice is a no-op the second
//	time. The caller MUST close the handle exactly once via Close;
//	the widget does not auto-close on Cancel because the caller
//	usually wants to perform additional cleanup (close EventSource,
//	notify server) before tearing down the UI.
//
// Português:
//
//	Overlay modal central com spinner CSS, mensagem, contador "Xs"
//	atualizando a cada segundo, e botão Cancel opcional. Lifecycle
//	via Handle.Close (idempotente). Caller fecha — não fecha sozinho
//	no Cancel para permitir cleanup adicional (fechar EventSource,
//	avisar servidor, etc.) antes da remoção da UI.
package overlay

import (
	"fmt"
	"syscall/js"
)

// progressKeyframesInjected guards against injecting the @keyframes
// rule more than once per page. The first ShowProgress call appends
// a <style> tag to <head> with the spinner rotation keyframes;
// subsequent calls reuse it. Using a package-level flag avoids
// scanning the DOM for an existing tag on every open.
//
// Português: flag de pacote para evitar injetar a regra de keyframes
// mais de uma vez por página.
var progressKeyframesInjected bool

// progressKeyframesCSS is the minimal CSS the spinner needs.
//
// The .iotm-progress-spinner rule is a 32px circle with a thick
// border that is mostly transparent and one accent-coloured arc;
// spinning the element rotates the visible arc and creates the
// classic "spinning ring" effect. The animation is 0.9s linear and
// infinite — slow enough to read as deliberate progress, not anxiety.
const progressKeyframesCSS = `
@keyframes iotmProgressSpin {
  to { transform: rotate(360deg); }
}
.iotm-progress-spinner {
  width: 36px;
  height: 36px;
  border: 4px solid ` + colSurface1 + `;
  border-top-color: ` + colBlue + `;
  border-radius: 50%;
  animation: iotmProgressSpin 0.9s linear infinite;
  flex-shrink: 0;
}
`

// ensureProgressKeyframes installs the @keyframes CSS once per page.
// Subsequent calls are no-ops thanks to progressKeyframesInjected.
func ensureProgressKeyframes(doc js.Value) {
	if progressKeyframesInjected {
		return
	}
	style := doc.Call("createElement", "style")
	style.Set("textContent", progressKeyframesCSS)
	doc.Get("head").Call("appendChild", style)
	progressKeyframesInjected = true
}

// ShowProgress opens the progress modal and returns a Handle for the
// caller to close it. onCancel may be nil — in that case the Cancel
// button is omitted entirely, yielding a spinner+message+counter
// modal with no user action. When onCancel is set, the callback fires
// on click and the caller is responsible for invoking Handle.Close
// (typically right inside onCancel for instant visual feedback).
//
// title, message and cancelLabel all go through translate.T at the
// call site — this function does no localisation itself, it just
// renders the strings the caller passes. cancelLabel is consumed only
// when onCancel != nil. Passing an empty cancelLabel together with a
// non-nil onCancel falls back to "Cancel" so the button never renders
// blank — but that fallback is a safety net, not the intended path;
// the project-wide invariant on i18n (INVARIANTS.md §3) requires the
// caller to pass translate.T("…", "Cancel") explicitly.
//
// Português:
//
//	Abre o modal de progresso e retorna o Handle. onCancel pode ser
//	nil (sem botão). title, message e cancelLabel vêm já localizados
//	via translate.T — esta função não faz i18n. Quando cancelLabel
//	é vazio e onCancel != nil, cai para "Cancel" como safety net
//	(mas o caller deveria sempre passar a string via T()).
func ShowProgress(title, message, cancelLabel string, onCancel func()) Handle {
	doc := js.Global().Get("document")
	ensureProgressKeyframes(doc)

	// Lock page scroll while the modal is open — same convention as
	// the Show() overlay.
	body := doc.Get("body")
	prevOverflow := body.Get("style").Get("overflow").String()
	body.Get("style").Set("overflow", "hidden")

	// Backdrop. Blocks the rest of the IDE; clicks on it do nothing
	// (the user must click Cancel or wait for the operation to
	// finish), matching the project's choice of a modal-blocking UX.
	backdrop := doc.Call("createElement", "div")
	backdrop.Get("style").Set("cssText",
		"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
			"background:rgba(0,0,0,0.55);z-index:99999;"+
			"display:flex;align-items:center;justify-content:center;"+
			"font-family:sans-serif;")

	// Panel. Compact — no editor, no tabs, just text and controls.
	panel := doc.Call("createElement", "div")
	panel.Get("style").Set("cssText", fmt.Sprintf(
		"min-width:340px;max-width:480px;background:%s;border:1px solid %s;"+
			"border-radius:8px;padding:24px 28px;color:%s;"+
			"box-shadow:0 12px 40px rgba(0,0,0,0.6);",
		colBase, colSurface1, colText))

	// Header row: spinner + title side-by-side.
	header := doc.Call("createElement", "div")
	header.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:14px;margin-bottom:12px;")

	spinner := doc.Call("createElement", "div")
	spinner.Set("className", "iotm-progress-spinner")
	header.Call("appendChild", spinner)

	titleEl := doc.Call("createElement", "div")
	titleEl.Get("style").Set("cssText", fmt.Sprintf(
		"font-size:16px;font-weight:600;color:%s;", colText))
	titleEl.Set("textContent", title)
	header.Call("appendChild", titleEl)

	panel.Call("appendChild", header)

	// Message — secondary line below the title.
	if message != "" {
		messageEl := doc.Call("createElement", "div")
		messageEl.Get("style").Set("cssText", fmt.Sprintf(
			"font-size:13px;color:%s;line-height:1.5;margin-bottom:14px;",
			colSubtext))
		messageEl.Set("textContent", message)
		panel.Call("appendChild", messageEl)
	}

	// Counter — updated by setInterval below. Starts at 0s so the
	// user sees a non-empty value the moment the modal opens.
	counterEl := doc.Call("createElement", "div")
	counterEl.Get("style").Set("cssText", fmt.Sprintf(
		"font-size:12px;color:%s;font-variant-numeric:tabular-nums;",
		colOverlay0))
	counterEl.Set("textContent", "0s")
	panel.Call("appendChild", counterEl)

	// Cancel button — only rendered when onCancel was supplied. The
	// click handler is wrapped in a js.FuncOf that we Release inside
	// removeFn so the binding does not leak across multiple opens.
	var (
		cancelFn    js.Func
		hoverInFn   js.Func
		hoverOutFn  js.Func
		buttonBound bool // true when onCancel was provided and the 3 funcs above were set
	)
	if onCancel != nil {
		buttonRow := doc.Call("createElement", "div")
		buttonRow.Get("style").Set("cssText",
			"display:flex;justify-content:flex-end;margin-top:16px;")

		cancelBtn := doc.Call("createElement", "button")
		// cancelLabel comes pre-localised from the caller via
		// translate.T(). Safety net: if the caller forgot, we use the
		// literal "Cancel" so the button is never blank. The invariant
		// (INVARIANTS.md §3) still requires the caller to pass T()'d
		// text — this fallback is purely defensive.
		btnText := cancelLabel
		if btnText == "" {
			btnText = "Cancel"
		}
		cancelBtn.Set("textContent", btnText)
		cancelBtn.Get("style").Set("cssText", fmt.Sprintf(
			"background:%s;color:%s;border:1px solid %s;"+
				"padding:6px 14px;border-radius:4px;font-size:13px;"+
				"cursor:pointer;font-family:inherit;",
			colSurface0, colText, colSurface1))

		// Subtle hover state — pure CSS would need a class; we set
		// it inline via mouseenter/mouseleave to avoid stylesheet
		// management for a one-shot widget. Both funcs are declared
		// in the function's outer scope so removeFn can Release them.
		hoverInFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			cancelBtn.Get("style").Set("background", colSurface1)
			return nil
		})
		hoverOutFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			cancelBtn.Get("style").Set("background", colSurface0)
			return nil
		})
		cancelBtn.Call("addEventListener", "mouseenter", hoverInFn)
		cancelBtn.Call("addEventListener", "mouseleave", hoverOutFn)

		cancelFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			// Defer the user callback so a panicking onCancel does
			// not leave our js.FuncOf handler stack mid-frame. (In
			// practice WASM goroutines absorb panics; this is belt
			// and braces.)
			go onCancel()
			return nil
		})
		cancelBtn.Call("addEventListener", "click", cancelFn)

		buttonRow.Call("appendChild", cancelBtn)
		panel.Call("appendChild", buttonRow)

		buttonBound = true
	}

	backdrop.Call("appendChild", panel)
	doc.Get("body").Call("appendChild", backdrop)

	// Set up the 1-second interval that drives the counter. Using
	// the JS-side setInterval (rather than a Go time.Ticker in a
	// goroutine) keeps the update path entirely on the main thread
	// and avoids the cross-boundary marshalling of every tick.
	var elapsed int
	tickFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		elapsed++
		counterEl.Set("textContent", fmt.Sprintf("%ds", elapsed))
		return nil
	})
	intervalID := js.Global().Call("setInterval", tickFn, 1000)

	// removeFn does the full cleanup: clear the interval, release
	// every js.FuncOf, remove the backdrop from the DOM, and restore
	// the body scroll lock. Idempotent — second call short-circuits
	// because the backdrop is no longer attached.
	removed := false
	removeFn := func() {
		if removed {
			return
		}
		removed = true
		js.Global().Call("clearInterval", intervalID)
		tickFn.Release()
		if buttonBound {
			cancelFn.Release()
			hoverInFn.Release()
			hoverOutFn.Release()
		}
		if backdrop.Get("parentNode").Truthy() {
			body.Get("style").Set("overflow", prevOverflow)
			doc.Get("body").Call("removeChild", backdrop)
		}
	}

	return Handle{Close: removeFn, Panel: panel}
}
