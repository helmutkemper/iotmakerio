// /ide/ui/contextMenu/events.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Event wiring for an open context menu. All DOM event handlers
//	are registered here; every js.Func is stored on the panelState
//	so Close() can release them. No builder code, no state mutation
//	outside the event handlers themselves.
//
//	Two interaction modes coexist, selected by the pointer kind
//	detected at open time:
//	  - Fine pointer (mouse): hover moves the preview, click on a
//	    leaf item executes immediately.
//	  - Coarse pointer (touch): tap moves the preview AND marks the
//	    item as pending; a second tap on the same item executes.
//	    Tapping a different item moves the preview and pending mark.
//
//	Both modes agree on: tapping a submenu item descends (no "first
//	tap" semantics for non-leaf rows — they always navigate on a
//	single tap), tapping Back pops one level, tapping outside the
//	panel closes the menu.
//
// Português:
//
//	Wiring de eventos para um menu aberto. Todos os handlers DOM
//	são registrados aqui; cada js.Func é guardado no panelState
//	para que Close() libere tudo.
package contextMenu

import "syscall/js"

// detectPointerCoarse returns true when the primary pointer is
// coarse (touch). Wrapped in a helper so the Controller, the
// renderer, and events share one canonical definition.
//
// The matchMedia call is cheap, but we only call it at Open time
// and cache the result on panelState — CSS responds to changes
// live via @media, but our Go-side handlers would need a listener
// to re-register for a runtime mode change, and the complexity is
// not worth it for a single popover's lifetime.
func detectPointerCoarse() bool {
	win := js.Global()
	mm := win.Get("matchMedia")
	if !mm.Truthy() {
		return false
	}
	res := win.Call("matchMedia", "(pointer: coarse)")
	if !res.Truthy() {
		return false
	}
	return res.Get("matches").Bool()
}

// wireEvents registers every DOM listener the open panel needs.
// All handlers are attached via state.addListener so Close can
// both removeEventListener and Release them in one pass.
//
// Callback arguments carry the controller by closure so handlers
// can read and mutate state, call re-renders via the controller,
// and trigger Close.
func (c *Controller) wireEvents(overlay js.Value, panel js.Value) {
	c.wireOverlayClose(overlay, panel)
	c.wireListClicks(panel)
	if !c.state.pointerCoarse {
		c.wireListHover(panel)
	}
	c.wireEscape()
}

// wireOverlayClose makes a tap on the transparent overlay (outside
// the panel) close the menu. A tap on the panel itself is caught by
// stopPropagation so it does not bubble up and close.
func (c *Controller) wireOverlayClose(overlay, panel js.Value) {
	overlayHandler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		target := args[0].Get("target")
		// Close only if the click actually hit the overlay, not a
		// descendant. Walking up the DOM is slower than checking
		// the target id directly.
		if target.Get("id").String() == c.state.overlayID {
			go c.Close()
		}
		return nil
	})
	c.state.addListener(overlay, "click", overlayHandler)

	// The panel itself must swallow clicks so they never reach the
	// overlay's close handler. A single listener with
	// stopPropagation is cheaper than listening on every child row.
	panelStop := js.FuncOf(func(_ js.Value, args []js.Value) any {
		args[0].Call("stopPropagation")
		return nil
	})
	c.state.addListener(panel, "click", panelStop)
}

// wireListClicks handles taps on item rows and on the Back button.
// Reads data-act and data-idx attributes set by the renderer to
// decide what to do.
func (c *Controller) wireListClicks(panel js.Value) {
	handler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		btn := c.findClickedButton(args[0])
		if !btn.Truthy() {
			return nil
		}
		act := btn.Call("getAttribute", "data-act").String()
		switch act {
		case "back":
			c.handleBack()
		case "item":
			idxAttr := btn.Call("getAttribute", "data-idx").String()
			c.handleItemClick(parseIdx(idxAttr))
		}
		return nil
	})
	c.state.addListener(panel, "click", handler)
}

// wireListHover updates the preview column as the mouse moves over
// list rows. Mouseenter/mouseleave are better here than mouseover —
// mouseover fires for every child (icon, label) and would re-render
// the preview on every sub-element crossing.
//
// Registered only when pointerCoarse is false. On touch devices
// this never runs.
func (c *Controller) wireListHover(panel js.Value) {
	handler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		btn := c.findHoveredButton(args[0])
		if !btn.Truthy() {
			return nil
		}
		if btn.Call("getAttribute", "data-act").String() != "item" {
			return nil
		}
		idxAttr := btn.Call("getAttribute", "data-idx").String()
		c.handlePreviewChange(parseIdx(idxAttr))
		return nil
	})
	// mouseover bubbles and is cheaper than wiring mouseenter on
	// every row. We filter to buttons inside the handler.
	c.state.addListener(panel, "mouseover", handler)
}

// wireEscape lets the user dismiss the menu with the Esc key —
// keyboard accessibility on desktop, and a fallback if the
// overlay click handler somehow misses an edge case.
//
// Attaches to `document`, which persists across panel lifecycles —
// hence the extra care in releaseListeners to call
// removeEventListener before releasing the js.Func.
func (c *Controller) wireEscape() {
	doc := js.Global().Get("document")
	handler := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].Get("key").String() == "Escape" {
			go c.Close()
		}
		return nil
	})
	c.state.addListener(doc, "keydown", handler)
}

// findClickedButton walks up from the click target to find the
// closest <button> with a data-act attribute. A click on the icon
// <span> must be treated as a click on the parent row.
func (c *Controller) findClickedButton(evt js.Value) js.Value {
	target := evt.Get("target")
	return closestDataAct(target)
}

// findHoveredButton is the same walk, but for mouseover events.
// Kept as a separate function for readability at the call sites.
func (c *Controller) findHoveredButton(evt js.Value) js.Value {
	target := evt.Get("target")
	return closestDataAct(target)
}

// closestDataAct walks up the DOM from node until a button with
// data-act is found, or until nothing. Modern browsers expose
// Element.closest() which does exactly this — we use it and guard
// against old agents by returning a null JS value on failure.
func closestDataAct(node js.Value) js.Value {
	if !node.Truthy() {
		return js.Null()
	}
	closest := node.Get("closest")
	if !closest.Truthy() {
		return js.Null()
	}
	return node.Call("closest", "[data-act]")
}

// parseIdx converts a data-idx attribute string to an int. Returns
// -1 on failure so the caller can no-op cleanly.
func parseIdx(s string) int {
	n := 0
	neg := false
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}
