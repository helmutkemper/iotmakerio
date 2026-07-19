// /ide/ui/contextMenu/contextMenu.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Controller and public API. One Controller per workspace; the
//	workspace creates it once in Init and injects it into every
//	device via SetContextMenu. The same controller serves every
//	context menu on its workspace — only one can be visible at a
//	time.
//
//	Public surface, in order of likely use:
//	  New(stage)                              — construct
//	  Open(items, screenX, screenY)           — screen coords
//	  OpenAtWorld(items, worldX, worldY)      — world coords
//	  OpenAtElement(items, anchorElem)        — DOM element
//	  Close()                                 — dismiss
//	  IsOpen()                                — visibility check
//
//	All mutation of panelState happens inside methods on Controller.
//	Sub-packages (renderer.go, events.go, anchor.go) are called via
//	package-level functions and never touch the state directly.
//
// Português:
//
//	Controller e API pública. Um Controller por workspace; o
//	workspace o cria uma vez em Init e injeta em cada device via
//	SetContextMenu. O mesmo controller atende a todo menu de
//	contexto do seu workspace — apenas um pode estar visível por vez.
package contextMenu

import (
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/sprite"
)

// actionDelay is the sleep inserted between Close() and the chosen
// item's OnClick. The legacy hex menu used 150ms for the same
// reason: new canvas elements created while a DOM menu is being
// torn down produce jank. Part of the package contract — callers
// do NOT wrap their OnClick callbacks themselves.
const actionDelay = 150 * time.Millisecond

// Controller owns the single open panel (if any) for a workspace.
// Safe to construct with New(nil, js.Null()) in tests; world→viewport
// conversion returns identity in that case. Not safe for concurrent
// use from multiple goroutines — WASM is single-threaded and callers
// should funnel Open/Close through the main JS thread.
type Controller struct {
	// stage is the sprite stage whose camera this controller reads
	// when converting world coordinates. Never mutated by this
	// package.
	stage sprite.Stage

	// canvasEl is the <canvas> DOM element that owns this controller's
	// stage. Used by worldToViewport to add the canvas's viewport
	// offset to world coordinates — without it, a device at worldX=100
	// on a canvas that itself sits 250px from the left edge of the
	// viewport (because of the sidebar) would have its menu painted
	// 250px off. The field is a js.Value so tests can pass js.Null()
	// and get identity-level behaviour.
	//
	// Português: Elemento <canvas> DOM que possui a stage deste
	// controller. Soma o offset do canvas na viewport às coordenadas
	// calculadas, de forma que o painel (position: fixed) apareça
	// exatamente em cima do device clicado mesmo quando há sidebar
	// ou barra superior antes do canvas.
	canvasEl js.Value

	// state is the currently open panel, or nil when no menu is
	// visible.
	state *panelState

	// counter increments on every Open to produce unique overlay
	// ids. Two workspaces with their own controllers can both show
	// a menu at once (though unusual) without id collision.
	counter int

	// copyHandler performs the context-menu "copy" action: it duplicates a
	// device of the given type, pre-configured with the given properties, at the
	// user's next click on the stage. It is installed once by the workspace
	// (SetCopyHandler, wired to DeviceFactory.CreateCopy). When nil — the
	// zero-value default — OpenForDevice omits the "copy" item, so the feature
	// degrades cleanly instead of showing a dead entry.
	//
	// Português: Executa a ação "copy" do menu: duplica um device do tipo dado,
	// já configurado com as props dadas, no próximo clique no stage. Fixado uma
	// vez pelo workspace (SetCopyHandler → DeviceFactory.CreateCopy). Quando nil
	// (o padrão), OpenForDevice omite o item "copy".
	copyHandler func(deviceType string, props map[string]interface{}, sourceID string)
}

// New constructs a Controller. The stage is stored for world→viewport
// conversion in OpenAtWorld; canvasEl is the <canvas> DOM element
// hosting that stage and is used to add the canvas's on-screen
// offset to computed positions. Callers that never use world
// coordinates may pass (nil, js.Null()).
//
// The two-argument form replaces the earlier one-argument New —
// passing canvasEl here avoids every call site having to know about
// the coordinate system mismatch between the stage (canvas-local)
// and the popover (position: fixed, viewport-local).
func New(stage sprite.Stage, canvasEl js.Value) *Controller {
	return &Controller{stage: stage, canvasEl: canvasEl}
}

// IsOpen reports whether a menu is currently visible.
func (c *Controller) IsOpen() bool { return c.state != nil }

// Open shows the menu with the given items, anchored at a screen
// coordinate (canvas-pixel). Use this when the caller already has
// screen coordinates (e.g. a button handler with MouseEvent.clientX).
//
// If a menu is already open, it is closed first. The OnClick of
// whatever was previously selected is NOT invoked — closing is
// silent.
func (c *Controller) Open(items []Item, screenX, screenY float64) {
	if len(items) == 0 {
		return
	}
	if c.IsOpen() {
		c.closeSilent()
	}
	injectCSS()

	c.counter++
	state := &panelState{
		overlayID:        uniqueID(c.counter),
		anchorScreenX:    screenX,
		anchorScreenY:    screenY,
		pointerCoarse:    detectPointerCoarse(),
		stack:            [][]Item{items},
		previewIdx:       -1,
		pendingActionIdx: -1,
	}
	c.state = state

	left, top := decidePlacement(screenX, screenY, state.pointerCoarse)
	c.mount(left, top)
}

// OpenAtWorld is a convenience wrapper: convert world coordinates
// using the controller's stage camera AND the canvas viewport
// offset, then call Open. This is the method backend devices
// typically use — their click handlers produce world coordinates
// (element position + event.LocalX/Y), and the full conversion
// chain is:
//
//	world → canvas-local → viewport-fixed
//
// The first step scales+offsets by the stage camera; the second
// adds the canvas's on-screen position so that a popover with
// `position: fixed` lands exactly above the click. Without the
// second step the popover drifts by the width of the sidebar plus
// the height of the page header.
//
// Português: Converte coordenadas de mundo para coordenadas da
// viewport (fixed positioning) e chama Open. Conversão em dois
// passos: câmera da stage, depois offset do canvas na viewport.
func (c *Controller) OpenAtWorld(items []Item, worldX, worldY float64) {
	sx, sy := worldToViewport(c.stage, c.canvasEl, worldX, worldY)
	c.Open(items, sx, sy)
}

// OpenAtElement shows the menu anchored to the right edge of a DOM
// element. Used by frontend-side context menus whose click handler
// already has the element in hand (e.g. StatementChartPro's
// frontend SVG sprite backing element).
func (c *Controller) OpenAtElement(items []Item, anchor js.Value) {
	if len(items) == 0 {
		return
	}
	if c.IsOpen() {
		c.closeSilent()
	}
	injectCSS()

	c.counter++
	state := &panelState{
		overlayID:        uniqueID(c.counter),
		pointerCoarse:    detectPointerCoarse(),
		stack:            [][]Item{items},
		previewIdx:       -1,
		pendingActionIdx: -1,
	}
	c.state = state

	left, top := decidePlacementForElement(anchor, state.pointerCoarse)
	state.anchorScreenX = left
	state.anchorScreenY = top
	c.mount(left, top)
}

// Close dismisses the menu. No-op if nothing is open. Releases
// every js.Func the panel registered, so repeated Open/Close
// cycles don't leak WASM heap.
func (c *Controller) Close() { c.closeSilent() }

// closeSilent is the internal version of Close that makes clear in
// the source it is being used as part of a replacement (Open after
// another Open), not as a user-facing action.
func (c *Controller) closeSilent() {
	if c.state == nil {
		return
	}
	// Listeners first, DOM second: removeEventListener needs the
	// target node to still be alive to unbind the handler, and
	// detaching the overlay from the body before that would leave
	// document-level listeners (Escape) in the browser's table
	// pointing at a released js.Func.
	c.state.releaseListeners()

	doc := js.Global().Get("document")
	if overlay := doc.Call("getElementById", c.state.overlayID); overlay.Truthy() {
		parent := overlay.Get("parentNode")
		if parent.Truthy() {
			parent.Call("removeChild", overlay)
		}
	}
	c.state = nil
}

// mount inserts the currently-built panel HTML into the DOM and
// wires up its events. Called from Open and OpenAtElement after
// state has been set up. Also called by re-render paths (after
// navigating into a submenu) — see rerender().
//
// The resolved (left, top) pair is saved on the state so subsequent
// rerenders can reuse the exact same position. Without this the
// panel would snap back to the raw click point on every hover,
// which manifests as the panel "jumping" as the mouse moves over
// list items.
func (c *Controller) mount(left, top float64) {
	// Persist the resolved position so rerender() can read it.
	// Done before DOM work so that even if appendChild throws
	// (e.g. body detached), the state is internally consistent.
	c.state.panelLeft = left
	c.state.panelTop = top

	doc := js.Global().Get("document")
	body := doc.Get("body")

	html := buildPanelHTML(c.state, left, top)

	holder := doc.Call("createElement", "div")
	holder.Set("innerHTML", html)
	overlay := holder.Get("firstElementChild")
	if !overlay.Truthy() {
		// Should never happen — buildPanelHTML always emits a root
		// element. Defensive clear to avoid a zombie state.
		c.state = nil
		return
	}
	body.Call("appendChild", overlay)

	panel := overlay.Get("firstElementChild")
	c.wireEvents(overlay, panel)
}

// rerender rebuilds the panel HTML in place, preserving the
// overlay node so id-based handlers keep working. Used after any
// navigation or preview change — entering/leaving a submenu,
// moving hover to a new row, tapping a different item on touch.
//
// The cost is low: buildList + buildPreview for a handful of items
// is cheap, and rebuilding in one place is simpler than hunting
// specific nodes to toggle classes.
//
// IMPORTANT: the panel's (left, top) is read from the state's
// panelLeft/panelTop fields — the values resolved by
// decidePlacement at the original Open call. Using the raw anchor
// coordinates here would move the panel onto the click point on
// every hover, producing the "jumping" behaviour reported against
// the Delivery-A build.
func (c *Controller) rerender() {
	if c.state == nil {
		return
	}
	doc := js.Global().Get("document")
	overlay := doc.Call("getElementById", c.state.overlayID)
	if !overlay.Truthy() {
		return
	}

	// Release old listeners first — the new HTML has fresh nodes.
	c.state.releaseListeners()

	// Reuse the resolved position saved by mount(). Never read
	// anchorScreenX/Y here — that is the click point, not the
	// panel's corner.
	left, top := c.state.panelLeft, c.state.panelTop
	panel := overlay.Get("firstElementChild")
	if panel.Truthy() {
		overlay.Call("removeChild", panel)
	}

	// Re-use the builder: we only need the inner panel HTML.
	inner := buildList(c.state) + buildPreview(c.state)
	newPanel := doc.Call("createElement", "div")
	newPanel.Set("className", "cm-panel")
	newPanel.Get("style").Set("left", percentPx(left))
	newPanel.Get("style").Set("top", percentPx(top))
	newPanel.Set("innerHTML", inner)
	overlay.Call("appendChild", newPanel)

	// Re-wire: overlay click-outside stays alive because the
	// overlay element itself was not replaced, but its handlers were
	// just released — so we register the full set again, including
	// the overlay-click and Escape, to the fresh js.Func instances.
	c.wireEvents(overlay, newPanel)
}

// ── Event-handler entry points (called from events.go) ────────────

// handleBack pops one submenu level and re-renders.
func (c *Controller) handleBack() {
	if c.state == nil {
		return
	}
	c.state.popSubmenu()
	c.rerender()
}

// handleItemClick is the central dispatcher for a row tap/click.
// Behaviour branches by pointer kind and by whether the row is a
// leaf:
//
//	fine + leaf       → execute immediately
//	fine + submenu    → navigate
//	coarse + leaf     → first tap previews & pends; second confirms
//	coarse + submenu  → navigate on first tap (no pending state)
func (c *Controller) handleItemClick(idx int) {
	if c.state == nil || idx < 0 {
		return
	}
	items := c.state.currentLevel()
	if idx >= len(items) {
		return
	}
	it := items[idx]

	if !it.isLeaf() {
		c.state.pushSubmenu(it.Submenu)
		c.rerender()
		return
	}

	if c.state.pointerCoarse {
		c.handleCoarseLeafTap(idx, it)
		return
	}

	// Fine pointer: execute immediately.
	c.executeLeaf(it)
}

// handleCoarseLeafTap implements the two-tap dance on touch. Moving
// to a different index repositions the preview and pending flag;
// tapping the same index again confirms and executes.
func (c *Controller) handleCoarseLeafTap(idx int, it Item) {
	if c.state.pendingActionIdx == idx {
		c.executeLeaf(it)
		return
	}
	c.state.previewIdx = idx
	c.state.pendingActionIdx = idx
	c.rerender()
}

// handlePreviewChange updates the preview when the mouse hovers a
// new row on fine-pointer devices. This path is performance-critical:
// it fires once per row traversal and the user perceives any DOM
// reflow as a "jump" of the panel.
//
// The implementation deliberately AVOIDS rerender() (which rebuilds
// the whole panel from scratch). Instead it does a surgical update:
//
//  1. toggle the `cm-active` class on the previous and new rows
//  2. replace only the preview column's innerHTML
//
// Nothing else in the DOM changes — not the panel element, not the
// list column, not the event handlers. Event listeners are preserved
// because we never detach/reattach any node. The result is that
// hover no longer causes any layout pass on the panel itself, so
// there can be no visible shift.
//
// If anyone adds logic here that modifies the list items (e.g.
// hovering changes visibility of a row), that change won't be
// covered by this path — revert to rerender() or expand this
// function to cover the new case.
//
// Português: Atualiza o preview no hover com mudança cirúrgica no
// DOM — nunca via rerender completo. Troca apenas a classe cm-active
// e o innerHTML da coluna de preview. Nenhum reflow no resto do
// painel, então nenhum shift visual.
func (c *Controller) handlePreviewChange(idx int) {
	if c.state == nil || idx < 0 {
		return
	}
	if c.state.previewIdx == idx {
		return
	}
	oldIdx := c.state.previewIdx
	c.state.previewIdx = idx

	doc := js.Global().Get("document")
	overlay := doc.Call("getElementById", c.state.overlayID)
	if !overlay.Truthy() {
		return
	}

	// Find the list column and toggle the active class on the two
	// rows involved. The rows are identified by their data-idx
	// attribute — the same attribute used by the click dispatcher,
	// so this stays in sync with whatever the event layer thinks
	// is the current row.
	panel := overlay.Get("firstElementChild")
	if !panel.Truthy() {
		return
	}

	// Toggle off the previous active row (if any).
	if oldIdx >= 0 {
		sel := "button.cm-item[data-idx=\"" + itoa(oldIdx) + "\"]"
		if prev := panel.Call("querySelector", sel); prev.Truthy() {
			prev.Get("classList").Call("remove", "cm-active")
		}
	}
	// Toggle on the new active row.
	sel := "button.cm-item[data-idx=\"" + itoa(idx) + "\"]"
	if next := panel.Call("querySelector", sel); next.Truthy() {
		next.Get("classList").Call("add", "cm-active")
	}

	// Swap the preview column's innerHTML. buildPreview returns the
	// whole <div class="cm-preview">…</div>, so we replace the
	// existing preview element with the freshly built one. We do
	// NOT rebuild the list column, so its scroll position (if any)
	// and hover state are preserved.
	if prev := panel.Call("querySelector", ".cm-preview"); prev.Truthy() {
		// Set outerHTML to swap node in place. innerHTML on the
		// parent would risk touching .cm-list.
		prev.Set("outerHTML", buildPreview(c.state))
	}
}

// executeLeaf closes the menu, waits 150ms, then invokes the
// callback. The delay matches the legacy SafeRun contract. The
// callback runs on its own goroutine so a long-running OnClick
// cannot block the event loop.
func (c *Controller) executeLeaf(it Item) {
	cb := it.OnClick
	c.closeSilent()
	if cb == nil {
		return
	}
	go func() {
		time.Sleep(actionDelay)
		cb()
	}()
}

// ── Internal helpers ──────────────────────────────────────────────

// uniqueID produces a stable DOM id for one open panel. The counter
// is per-controller so two controllers (two workspaces) never
// collide even on the first call.
func uniqueID(counter int) string {
	// Small, readable, no dependence on time/rand: enough for the
	// single-controller case and trivially unique for per-workspace
	// use.
	return "iotm-ctx-" + itoa(counter)
}

// itoa is a minimal int→string without importing strconv for one
// number. Keeps the package's import list small.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// percentPx formats a float as "Npx" for inline style attributes.
// Rounds to integer pixels — sub-pixel popover positions cause
// visible blurring on some displays.
func percentPx(f float64) string { return itoa(int(f+0.5)) + "px" }
