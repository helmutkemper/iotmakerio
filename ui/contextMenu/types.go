// /ide/ui/contextMenu/types.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Public and internal type definitions for the context menu package.
//	Kept deliberately small — every other file in this package imports
//	this one, and only this one, for type references.
//
//	Design note: Item carries a translated Label and markdown fallback
//	already resolved by the caller. The package never calls translate.T
//	itself. This keeps the package agnostic to the locale pipeline and
//	testable without the translate bundle being loaded.
//
// Português:
//
//	Definições de tipos públicos e internos do package contextMenu.
//	Mantido propositalmente pequeno — todos os outros arquivos do package
//	importam este e apenas este para referências de tipos.
//
//	Nota de design: Item carrega Label já traduzido e fallback markdown
//	resolvidos pelo chamador. O package nunca chama translate.T sozinho.
//	Isso mantém o package agnóstico ao pipeline de locale e testável sem
//	ter o bundle de traduções carregado.
package contextMenu

import "syscall/js"

// Item is a single row in the context menu. The caller composes a
// slice of Items and hands it to Controller.Open (or one of the
// Open* variants). Submenu navigation is push-replace: tapping a
// parent replaces the visible list with Submenu and shows a Back
// row at the top of the list column.
type Item struct {
	// ID is a stable identifier. Used for hit-test, for the future
	// tutorial highlight, and as a key in hover-preview caching.
	// Must be unique within a single level (parent's Submenu slice).
	ID string

	// Label is the visible text, already passed through translate.T
	// by the caller.
	Label string

	// FontAwesomePath is the raw SVG path data for the leading icon.
	// Use constants from rulesIcon (e.g. rulesIcon.KFATrashCan).
	FontAwesomePath string

	// ViewBox is the SVG viewBox attribute that matches FontAwesomePath.
	// FontAwesome icons from different families have different viewBoxes;
	// always copy the one that matches the source icon (e.g. "0 0 512 512").
	ViewBox string

	// HelpKey is the translate.T key used to look up preview markdown
	// for this item. The renderer DOES resolve this one via translate.T
	// — it is the single exception to the "caller pre-translates" rule,
	// because markdown blocks are large and it would be awkward to force
	// the caller to pre-fetch them.
	//
	// If HelpKey is empty, HelpFallback is shown verbatim.
	HelpKey string

	// HelpFallback is the English markdown shown if HelpKey resolves
	// empty or is missing from the locale bundle. Kept short — one or
	// two sentences — because the preview column is not a full docs
	// page; it is a disambiguator.
	HelpFallback string

	// Danger marks the item as destructive. The renderer paints it
	// with the danger accent (Catppuccin red). Only Delete uses this
	// today, by convention.
	Danger bool

	// OnClick is invoked 150 ms after the menu closes. The delay
	// exists because the hex menu era taught us that creating new
	// canvas elements while a DOM menu is being torn down causes
	// browser jank. The delay is not configurable — it is part of
	// the package's contract with callers, same as the legacy
	// SafeRun helper.
	OnClick func()

	// Submenu, when non-empty, makes this item navigate instead of
	// execute. OnClick is ignored when Submenu is non-empty.
	Submenu []Item
}

// isLeaf reports whether this item executes an action on click
// instead of navigating into a submenu. Internal helper used by the
// renderer and events.
func (it Item) isLeaf() bool { return len(it.Submenu) == 0 }

// panelState is the entire internal state of an open menu. The
// Controller holds at most one panelState at a time.
//
// Fields are split in two groups: layout (set once at Open time)
// and navigation (mutated as the user taps Back or enters a
// submenu). Keeping the two groups separate makes it obvious what
// the renderer has to redraw on each transition.
type panelState struct {
	// --- Layout (stable for the lifetime of the panel) ---

	// overlayID is the DOM id of the root overlay div. Randomised
	// per open so two menus (if two workspaces are visible at once)
	// never collide.
	overlayID string

	// anchorScreenX, anchorScreenY are the click point in
	// viewport-fixed coordinates. Kept on the state for diagnostic
	// and future uses (e.g. re-deciding placement after a viewport
	// resize); rerender() does NOT read these to position the
	// panel — it uses panelLeft/panelTop instead, so re-renders
	// after a hover or submenu navigation do not drift the panel
	// onto the click point.
	anchorScreenX, anchorScreenY float64

	// panelLeft, panelTop are the absolute viewport-fixed
	// coordinates of the panel's top-left corner, as resolved by
	// anchor.decidePlacement at Open time. Unlike the anchor
	// coordinates, these are already clamped to the viewport and
	// flipped to the opposite side if the preferred side would
	// clip. Any rerender reuses these values verbatim — this is
	// what keeps the panel stationary as the user hovers items
	// or navigates into a submenu.
	//
	// Português: Posição resolvida do painel (após
	// decidePlacement). Usada por rerender() para manter o painel
	// no mesmo lugar enquanto o usuário navega por itens.
	panelLeft, panelTop float64

	// pointerCoarse is true when the device reports coarse-pointer
	// input (touch). Cached at open time so the renderer and event
	// wiring don't each call matchMedia.
	pointerCoarse bool

	// --- Navigation (mutated as user traverses) ---

	// stack holds parent levels for the push-replace submenu model.
	// Top of the stack is the currently visible list. Push on enter
	// submenu; pop on Back.
	stack [][]Item

	// previewIdx is the index of the item whose preview is currently
	// in the right column. -1 means no selection (initial state).
	previewIdx int

	// pendingActionIdx tracks "first tap" on coarse-pointer devices.
	// Second tap on the same index executes; tap on a different
	// index moves preview and resets this to the new index.
	// Unused on fine-pointer devices.
	pendingActionIdx int

	// listeners tracks every (target, event, handler) triple the
	// panel registers. Close() iterates over this list to call
	// removeEventListener first, then Release() on the js.Func. The
	// order matters: releasing a js.Func that the browser still
	// holds a reference to will panic if the browser later dispatches
	// an event to it. This is particularly important for handlers
	// attached to `document` (the Escape key handler) — when the
	// panel overlay is removed from the DOM, its own listeners are
	// collected by the browser, but document-level ones are not.
	listeners []listenerBinding
}

// listenerBinding records one DOM event subscription so it can be
// reliably torn down on Close. The three fields are precisely what
// removeEventListener needs.
type listenerBinding struct {
	target js.Value
	event  string
	fn     js.Func
}

// addListener attaches an event handler AND remembers it for cleanup.
// Always use this instead of calling target.addEventListener directly;
// a handler that is not in the listeners slice will either leak or
// crash at teardown.
func (s *panelState) addListener(target js.Value, event string, fn js.Func) {
	target.Call("addEventListener", event, fn)
	s.listeners = append(s.listeners, listenerBinding{target: target, event: event, fn: fn})
}

// releaseListeners removes every DOM subscription and releases the
// backing js.Func values. Safe to call when the slice is empty.
func (s *panelState) releaseListeners() {
	for _, b := range s.listeners {
		b.target.Call("removeEventListener", b.event, b.fn)
		b.fn.Release()
	}
	s.listeners = nil
}

// currentLevel returns the items currently visible in the list
// column. Panics if the stack is empty — which can only happen if
// Open was never called, a bug.
func (s *panelState) currentLevel() []Item {
	if len(s.stack) == 0 {
		return nil
	}
	return s.stack[len(s.stack)-1]
}

// isAtRoot reports whether the top of the stack is the original
// items slice passed to Open. Used by the renderer to decide
// whether to show a Back row.
func (s *panelState) isAtRoot() bool { return len(s.stack) == 1 }

// pushSubmenu descends into a submenu. Resets preview/pending
// state because the indices refer to the new level's items.
func (s *panelState) pushSubmenu(items []Item) {
	s.stack = append(s.stack, items)
	s.previewIdx = -1
	s.pendingActionIdx = -1
}

// popSubmenu returns to the previous level. Safe to call at root
// (no-op in that case). Resets preview/pending state.
func (s *panelState) popSubmenu() {
	if len(s.stack) <= 1 {
		return
	}
	s.stack = s.stack[:len(s.stack)-1]
	s.previewIdx = -1
	s.pendingActionIdx = -1
}
