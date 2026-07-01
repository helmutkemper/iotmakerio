// /ide/ui/contextMenu/renderer.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	DOM construction for an open context menu. Pure builders — no
//	event wiring lives here (see events.go), no layout math (see
//	anchor.go), no state mutation (see contextMenu.go).
//
//	Design note: the renderer emits innerHTML strings instead of
//	createElement-by-element. innerHTML is faster in WASM because
//	each createElement / appendChild round-trips through
//	syscall/js with its own marshalling cost. The cost we pay in
//	return is the discipline of escaping every user-controlled
//	string — escHTML in this file. All caller strings (labels,
//	help) pass through escHTML or through window.marked.parse,
//	never directly into innerHTML.
//
// Português:
//
//	Construção de DOM para um menu aberto. Construtores puros —
//	sem wiring de eventos (ver events.go), sem matemática de layout
//	(ver anchor.go), sem mutação de estado (ver contextMenu.go).
package contextMenu

import (
	"fmt"
	"strings"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/translate"
)

// buildPanelHTML returns the full HTML for the overlay + panel.
// The caller inserts the result into document.body as a single
// innerHTML assignment, then wires events (events.go) onto the
// resulting DOM.
//
// left, top are the panel's screen-space position decided by
// anchor.decidePlacement.
func buildPanelHTML(state *panelState, left, top float64) string {
	inlineStyle := fmt.Sprintf("left:%.0fpx;top:%.0fpx", left, top)

	listHTML := buildList(state)
	previewHTML := buildPreview(state)

	var sb strings.Builder
	sb.Grow(2048)
	sb.WriteString(`<div class="cm-overlay" id="`)
	sb.WriteString(state.overlayID)
	sb.WriteString(`"><div class="cm-panel" style="`)
	sb.WriteString(inlineStyle)
	sb.WriteString(`">`)
	sb.WriteString(listHTML)
	sb.WriteString(previewHTML)
	sb.WriteString(`</div></div>`)
	return sb.String()
}

// buildList renders the left column: optional Back row plus one
// button per item at the current level of the state stack.
func buildList(state *panelState) string {
	items := state.currentLevel()

	var sb strings.Builder
	sb.Grow(128 + 80*len(items))
	sb.WriteString(`<div class="cm-list">`)

	if !state.isAtRoot() {
		sb.WriteString(renderBackRow())
	}

	for i, it := range items {
		sb.WriteString(renderItem(i, it, i == state.previewIdx, i == state.pendingActionIdx))
	}

	sb.WriteString(`</div>`)
	return sb.String()
}

// renderBackRow returns the DOM for the "Back" row shown at the top
// of the list when the user is inside a submenu. The translate.T
// call is the only one the renderer performs on its own — the
// label is a package-owned string, not a caller-supplied one.
func renderBackRow() string {
	label := translate.T("ctxMenuBack", "Back")
	return fmt.Sprintf(
		`<button class="cm-back" data-act="back" type="button">`+
			`<span class="cm-icon">%s</span>`+
			`<span>%s</span>`+
			`</button>`,
		backArrowSVG,
		escHTML(label),
	)
}

// renderItem returns the DOM for a single list row.
//
// active: this item's preview is currently shown in the right column
// pending: coarse-pointer first-tap state — next tap executes
func renderItem(idx int, it Item, active, pending bool) string {
	classes := "cm-item"
	if it.Danger {
		classes += " cm-danger"
	}
	if active {
		classes += " cm-active"
	}
	if pending {
		classes += " cm-pending"
	}

	chevron := ""
	if !it.isLeaf() {
		chevron = `<span class="cm-chev"></span>`
	}

	return fmt.Sprintf(
		`<button class="%s" data-act="item" data-idx="%d" type="button">`+
			`<span class="cm-icon">%s</span>`+
			`<span class="cm-label">%s</span>`+
			`%s`+
			`</button>`,
		classes,
		idx,
		renderIconSVG(it.FontAwesomePath, it.ViewBox),
		escHTML(it.Label),
		chevron,
	)
}

// renderIconSVG returns an inline SVG with the given path and
// viewBox. Uses fill="currentColor" so the icon colour inherits
// from the row, which is what makes the danger-red work.
func renderIconSVG(pathD, viewBox string) string {
	if pathD == "" {
		return ""
	}
	vb := viewBox
	if vb == "" {
		vb = "0 0 512 512"
	}
	return fmt.Sprintf(
		`<svg viewBox="%s" xmlns="http://www.w3.org/2000/svg"><path d="%s"/></svg>`,
		escAttr(vb),
		escAttr(pathD),
	)
}

// buildPreview returns the right column. Content depends on the
// currently previewed item; if no item is selected, shows a muted
// hint instead of nothing (so the column's width doesn't collapse
// visually during the gap between open and first hover).
func buildPreview(state *panelState) string {
	body := emptyPreviewBody()

	items := state.currentLevel()
	if state.previewIdx >= 0 && state.previewIdx < len(items) {
		it := items[state.previewIdx]
		body = helpMarkdownFor(it)
		if it.Danger {
			body = `<div class="cm-preview-danger">` + body + `</div>`
		}
	}

	return `<div class="cm-preview" id="cm-preview">` + body + `</div>`
}

// emptyPreviewBody is the placeholder shown before the user picks
// any item. Translate key kept private to this package.
func emptyPreviewBody() string {
	msg := translate.T("ctxMenuPreviewHint", "Hover an item to see what it does.")
	return `<p class="cm-preview-empty">` + escHTML(msg) + `</p>`
}

// helpMarkdownFor returns the rendered HTML preview body for an
// item. Resolution order: translate.T(HelpKey) if HelpKey is set,
// else HelpFallback verbatim. Empty fallback yields the empty
// preview body.
func helpMarkdownFor(it Item) string {
	md := ""
	if it.HelpKey != "" {
		md = translate.T(it.HelpKey, it.HelpFallback)
	} else {
		md = it.HelpFallback
	}
	if md == "" {
		return emptyPreviewBody()
	}
	return renderMarkdown(md)
}

// renderMarkdown converts markdown to HTML using window.marked, the
// same library the sidebar and overlay use. Falls back to escaped
// plain text if marked is not loaded — safer than shipping raw
// markdown into innerHTML.
func renderMarkdown(md string) string {
	marked := js.Global().Get("marked")
	if !marked.Truthy() {
		// marked not yet loaded — show as escaped plain text to avoid
		// leaking markdown syntax as visible characters while still
		// being safe against injection.
		return "<p>" + escHTML(md) + "</p>"
	}
	html := marked.Call("parse", md)
	if !html.Truthy() {
		return "<p>" + escHTML(md) + "</p>"
	}
	return html.String()
}

// escHTML escapes text for safe embedding in innerHTML body context.
// Minimal set — the strings here are labels and markdown, not user
// input with arbitrary HTML intent.
func escHTML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return r.Replace(s)
}

// escAttr escapes for a double-quoted attribute value. Quote
// character must be escaped; angle brackets are escaped defensively
// even though they are not strictly required inside attributes.
func escAttr(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// backArrowSVG is the inline SVG for the Back row's chevron. Kept
// here as a package-private constant because it never changes and
// importing FontAwesome just for this one glyph would cost more
// bytes than embedding the path.
const backArrowSVG = `<svg viewBox="0 0 512 512" xmlns="http://www.w3.org/2000/svg">` +
	`<path d="M41.4 233.4c-12.5 12.5-12.5 32.8 0 45.3l160 160c12.5 12.5 32.8 12.5 45.3 0s12.5-32.8 0-45.3L141.2 288 448 288c17.7 0 32-14.3 32-32s-14.3-32-32-32l-306.7 0L246.6 118.6c12.5-12.5 12.5-32.8 0-45.3s-32.8-12.5-45.3 0l-160 160z"/>` +
	`</svg>`
