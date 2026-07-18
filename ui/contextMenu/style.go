// /ide/ui/contextMenu/style.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Single source of CSS for the context menu. Injected into the
//	document head once per page lifetime — idempotent via a marker
//	id on the <style> tag.
//
//	Design note: every other CSS in the project uses a three-letter
//	prefix on class names to avoid bleed. We use `cm-` here. The
//	old `td-ctx-*` classes (defined inline in statementTextDisplay.go)
//	are replaced by this file; when all frontend context menus have
//	migrated, that block will be deleted.
//
//	Pointer-adaptive rules live in this file, gated by
//	@media (pointer: coarse). Tablets get larger touch targets and
//	slightly taller preview text; mice get tighter rows.
//
// Português:
//
//	Fonte única de CSS do menu de contexto. Injetado no head do
//	documento uma única vez na vida da página — idempotente via um id
//	marcador na tag <style>.
package contextMenu

import "syscall/js"

// styleID marks the injected <style> tag. The presence of an element
// with this id short-circuits re-injection.
const styleID = "iotm-ctx-menu-css"

// injectCSS adds the package stylesheet to document.head the first
// time it is called; subsequent calls are no-ops. Safe to call on
// every Open — the cost is one getElementById lookup.
//
// The Controller calls this inside Open() rather than in New() so
// that the CSS only lands in the DOM once a menu is actually about
// to be shown. This matters for pages that embed the IDE but never
// open a context menu (read-only dashboards, future frontend-only
// mode): no stylesheet is injected until needed.
func injectCSS() {
	doc := js.Global().Get("document")
	if existing := doc.Call("getElementById", styleID); existing.Truthy() {
		return
	}
	style := doc.Call("createElement", "style")
	style.Set("id", styleID)
	style.Set("textContent", cssBlock)
	doc.Get("head").Call("appendChild", style)
}

// cssBlock holds the entire stylesheet. Kept as one constant so a
// reader can grep by class name and land in one place — split files
// would force a multi-file search for small CSS tweaks.
//
// Catppuccin Mocha palette, with the same accent blue (#6c8eff) that
// the sidebar uses.
const cssBlock = `
/* Overlay: transparent click-catcher covering the viewport.
   Variant A of the redesign is non-modal — no dim, just a layer
   that absorbs outside clicks and closes the menu. */
.cm-overlay {
  position: fixed;
  inset: 0;
  z-index: 9000;
  background: transparent;
  pointer-events: auto;
}

/* Panel: the floating popover itself. Anchored by inline top/left
   computed in anchor.go. Two columns: list on the left, preview on
   the right. */
.cm-panel {
  position: absolute;
  display: flex;
  flex-direction: row;
  background: #1e1e2e;
  border: 1px solid #2a2a40;
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.55), 0 0 0 1px #1a1a28;
  font-family: Arial, sans-serif;
  color: #cdd6f4;
  overflow: hidden;
  /* Default (fine pointer) dimensions — overridden below for coarse. */
  width: 440px;
  min-height: 200px;
  max-height: 320px;
}

/* Reset — every element inside the overlay uses border-box so that
   borders and padding never push siblings around. Without this, a
   <button> with user-agent defaults (content-box) can appear to
   shift on re-render because Chrome/Firefox have subtly different
   default paddings. Scoped to .cm-overlay so we don't touch the
   host page's global styles.
   
   Português: Reset pontual — force border-box em todo o conteúdo do
   overlay para que border e padding não desloquem o layout ao trocar
   classes. Escopo limitado ao overlay para não poluir o host. */
.cm-overlay, .cm-overlay * {
  box-sizing: border-box;
}

/* ── List column ──────────────────────────────────────────────── */

.cm-list {
  flex: 0 0 170px;
  background: #1a1a28;
  border-right: 1px solid #2a2a40;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  /* Scroll without a scrollbar (field 2026-07-17: "o menu contextual
     não deveria ter barra de rolagem, ele simplesmente rola") — wheel,
     trackpad and touch still scroll; only the bar is hidden.
     Português: Rola sem barra — roda/trackpad/touch continuam rolando;
     só a barra some. */
  scrollbar-width: none;
}
.cm-list::-webkit-scrollbar {
  display: none;
}

.cm-back {
  display: flex;
  align-items: center;
  gap: 8px;
  background: none;
  border: none;
  border-bottom: 1px solid #2a2a40;
  color: #6c8eff;
  cursor: pointer;
  font-size: 13px;
  padding: 10px 14px;
  text-align: left;
  font-family: inherit;
}
.cm-back:hover { background: rgba(108, 142, 255, 0.10); }

.cm-item {
  display: flex;
  align-items: center;
  gap: 10px;
  background: none;
  border: none;
  border-left: 3px solid transparent;
  color: #bac2de;
  cursor: pointer;
  font-size: 14px;
  font-family: inherit;
  padding: 12px 14px;
  text-align: left;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.cm-item:hover { background: rgba(255, 255, 255, 0.04); color: #cdd6f4; }

/* Active row: blue accent, matches the sidebar's active item. */
.cm-item.cm-active {
  background: rgba(108, 142, 255, 0.12);
  color: #ffffff;
  border-left-color: #6c8eff;
}

/* Danger variant (Delete): red accent when active. */
.cm-item.cm-danger.cm-active {
  background: rgba(243, 139, 168, 0.12);
  border-left-color: #f38ba8;
  color: #f38ba8;
}

/* Pending confirmation (coarse-pointer only): first tap selected it
   but did not execute yet; a subtle outline hints that a second tap
   will confirm. */
.cm-item.cm-pending {
  outline: 1px dashed rgba(108, 142, 255, 0.35);
  outline-offset: -4px;
}

/* Icon slot. Width/height are explicit so icons never inherit the
   row's font-size. */
.cm-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: inherit;
}
.cm-icon svg {
  width: 16px;
  height: 16px;
  fill: currentColor;
}

.cm-label {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Submenu chevron hint. */
.cm-chev {
  width: 8px;
  height: 8px;
  border-right: 1.5px solid currentColor;
  border-top: 1.5px solid currentColor;
  transform: rotate(45deg);
  opacity: 0.5;
  flex-shrink: 0;
}

/* ── Preview column ───────────────────────────────────────────── */

.cm-preview {
  flex: 1;
  padding: 14px 16px;
  background: #1e1e2e;
  overflow-y: auto;
  line-height: 1.55;
  min-width: 0; /* allow flex child to shrink below content width */
  scrollbar-width: none; /* same scrollbar-less scroll as .cm-list */
}
.cm-preview::-webkit-scrollbar {
  display: none;
}

/* The previewed item's full label, atop its explanation — rescues
   labels the narrow list column truncates. Português: O rótulo
   completo do item, sobre a explicação — resgata rótulos truncados. */
.cm-preview-title {
  font-weight: 700;
  font-size: 14px;
  color: #cdd6f4;
  margin: 0 0 8px 0;
  padding-bottom: 6px;
  border-bottom: 1px solid #2a2a40;
}

.cm-preview h1,
.cm-preview h2,
.cm-preview h3 {
  font-size: 14px;
  font-weight: 500;
  margin: 0 0 6px;
  color: #cdd6f4;
}

.cm-preview p {
  font-size: 13px;
  color: #a6adc8;
  margin: 0 0 8px;
}

.cm-preview code {
  background: #2a2a40;
  padding: 1px 5px;
  border-radius: 3px;
  font-family: monospace;
  font-size: 12px;
  color: #f5c2e7;
}

.cm-preview-empty {
  color: #585b70;
  font-size: 12px;
  font-style: italic;
}

/* Danger-tinted preview header when the active item is destructive. */
.cm-preview-danger {
  color: #f38ba8 !important;
}

/* ── Coarse-pointer (tablet) adjustments ──────────────────────── */
/*
   Rationale: touch targets of 44-48px are the iOS/Android baseline.
   Preview text gets a bump to 14px because reading distance on a
   tablet is longer than on a mouse-driven desktop. */
@media (pointer: coarse) {
  .cm-panel {
    width: 520px;
    min-height: 260px;
    max-height: 400px;
  }
  .cm-list { flex: 0 0 200px; }
  .cm-item { padding: 16px 14px; font-size: 15px; }
  .cm-back { padding: 14px 14px; font-size: 14px; }
  .cm-icon { width: 22px; height: 22px; }
  .cm-icon svg { width: 18px; height: 18px; }
  .cm-preview { padding: 16px 18px; }
  .cm-preview p { font-size: 14px; }
  .cm-preview h1,
  .cm-preview h2,
  .cm-preview h3 { font-size: 15px; }
}
`
