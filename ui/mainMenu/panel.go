// ide/ui/mainMenu/panel.go — DOM-based three-column menu panel.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Three-column panel: rail (close + categories) | list | preview.
//
//	Animation: fade-in + slide-up only on Open(). Navigation never
//	rebuilds the overlay — only the content div is updated, so there
//	is no flicker between category changes.
//
//	Close button: first item in the rail column. Calls window._ideExit()
//	which is exposed by ide.js and navigates the SPA back to the home page.
//
//	Help in preview: when a leaf item (ItemAction) is selected, the preview
//	column fetches its markdown help file from:
//	  GET /help/devices/<category>/<itemID>/<locale>.md
//	with fallback to "en" if the browser locale file is not found.
//	Markdown is rendered via window.marked (already loaded by the overlay
//	package).
//
// Português:
//
//	Painel de três colunas. Animação só na abertura. Navegação atualiza
//	apenas a div de conteúdo. Botão fechar no topo da rail chama
//	window._ideExit(). Preview busca markdown de help com fallback de locale.
package mainMenu

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/rulesServer"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
)

const (
	// panelOverlayID is the id of the root overlay div — created once per Open().
	panelOverlayID = "ide-main-panel-overlay"
	// panelContentID is the id of the inner content wrapper updated on navigation.
	panelContentID = "ide-main-panel-content"
	// panelCSSID is the id of the injected <style> tag — injected only once ever.
	panelCSSID = "ide-main-panel-css"
	// panelSearchID is the id of the search input inside the list column.
	// The input event is captured via delegation on the overlay.
	panelSearchID = "ide-panel-search"
	// panelListItemsID is the scrollable items container inside the list column.
	// Only this div is updated on filter/navigation — the search input above it
	// is never touched, so the browser never drops keyboard focus.
	panelListItemsID = "ide-panel-list-items"
	// panelPreviewID is the id of the preview column div.
	// Updated independently so the list and search input are never touched.
	panelPreviewID = "ide-panel-preview"

	// helpLocaleFallback is the locale used when the browser locale file is absent.
	helpLocaleFallback = "en"
)

// helpCategoryByItemID maps a menu item ID to its help directory category.
// The path under EndpointHelp will be: /devices/<category>/<itemID>/<locale>.md
var helpCategoryByItemID = map[string]string{
	"Add":                  "math",
	"Sub":                  "math",
	"Mul":                  "math",
	"Div":                  "math",
	"EqualTo":              "logic",
	"notEqualTo":           "logic",
	"LessThan":             "logic",
	"LessThanOrEqualTo":    "logic",
	"GreaterThan":          "logic",
	"GreaterThanOrEqualTo": "logic",
	"ConstInt":             "const",
	"ConstBool":            "const",
	"ConstFloat":           "const",
	"ConstString":          "const",
	"ConstArrayInt":        "const",
	"ConstArrayFloat":      "const",
	"ConstArrayString":     "const",
	"GetVarInt":            "var",
	"GetVarFloat":          "var",
	"SetVarInt":            "var",
	"SetVarFloat":          "var",
	"GetVarString":         "var",
	"SetVarString":         "var",
	"Loop":                 "loop",
	"Gauge":                "display",
	"LED":                  "display",
	"BarGraph":             "display",
	"TextDisplay":          "display",
	"Button":               "display",
	"SevenSeg":             "display",
	"Knob":                 "display",
	"Chart":                "display",
}

// Panel is the three-column DOM menu panel for the main IDE button.
type Panel struct {
	visible bool

	// top-level items passed to Open() — used to build the rail.
	items []hexMenu.MenuItem

	// navigation state
	selectedCatIdx  int
	listItems       []hexMenu.MenuItem
	listStack       [][]hexMenu.MenuItem
	selectedListIdx int

	// filterText is the current search query typed in the list column.
	// Reset to "" when navigating to a new category, into a submenu, or back.
	filterText string

	// browserLocale is detected once on Open() and reused for all help fetches.
	browserLocale string

	// readmeByItemID holds the inline readme tabs keyed by MenuItem.ID.
	// Populated by SetItemReadme(s) when a black-box def has readme tabs
	// in its Help, or when the admin-edited menu tree provides a single
	// markdown blob (wrapped as a one-element slice). Used by
	// loadAndRenderHelp to avoid a server round-trip for device overviews.
	readmeByItemID map[string][]blackbox.HelpTabClient

	// methodHelpByItemID holds the ordered help tabs for method items.
	// Key is MenuItem.ID (e.g. "bb_APDS9960_init").
	// Populated by SetItemMethodHelp() from BlackBoxDefClient.Help.Methods.
	methodHelpByItemID map[string][]blackbox.HelpTabClient

	// activeTabByItemID tracks the currently selected tab index per item.
	// Reset to 0 when the item changes.
	activeTabByItemID map[string]int

	// JS function handles — released on close() to prevent WASM heap leaks.
	jsFuncs []js.Func
}

// NewPanel creates a panel. No DOM elements are created until Open() is called.
func NewPanel() *Panel {
	return &Panel{
		selectedListIdx:    -1,
		readmeByItemID:     make(map[string][]blackbox.HelpTabClient),
		methodHelpByItemID: make(map[string][]blackbox.HelpTabClient),
		activeTabByItemID:  make(map[string]int),
	}
}

// SetItemMethodHelp registers the ordered help tabs for a method MenuItem.ID.
func (p *Panel) SetItemMethodHelp(itemID string, tabs []blackbox.HelpTabClient) {
	if len(tabs) > 0 {
		p.methodHelpByItemID[itemID] = tabs
	}
}

// SetItemReadme registers inline readme tabs for a MenuItem.ID.
// Called by the menu builder for every black-box device that has
// non-empty Help.Readme tabs, and for menu-tree nodes that carry an
// admin-edited single-blob HelpMarkdown (wrapped as a one-element
// slice by mergeTreeHelp). When the user selects that item,
// loadAndRenderHelp uses these tabs directly instead of fetching from
// the server.
func (p *Panel) SetItemReadme(itemID string, tabs []blackbox.HelpTabClient) {
	if len(tabs) > 0 {
		p.readmeByItemID[itemID] = tabs
	}
}

// IsVisible reports whether the panel is currently open.
func (p *Panel) IsVisible() bool {
	return p.visible
}

// Open shows the panel. The overlay is created once with opening animation.
// Navigation never recreates the overlay — only the content div is updated.
func (p *Panel) Open(items []hexMenu.MenuItem) {
	if p.visible {
		p.close()
	}
	p.items = items
	p.visible = true
	p.selectedCatIdx = 0
	p.listStack = nil
	p.listItems = categorySubItems(items, 0)
	p.selectedListIdx = firstSelectableIdx(p.listItems)
	p.filterText = ""
	p.browserLocale = detectLocale()

	p.injectCSS()
	p.createOverlay()
}

// Close hides the panel and releases all resources.
func (p *Panel) Close() {
	p.close()
}

func (p *Panel) close() {
	if !p.visible {
		return
	}
	p.visible = false

	doc := js.Global().Get("document")
	el := doc.Call("getElementById", panelOverlayID)
	if !el.IsNull() && !el.IsUndefined() {
		el.Get("parentNode").Call("removeChild", el)
	}

	for _, f := range p.jsFuncs {
		f.Release()
	}
	p.jsFuncs = nil
}

// buildHelpTabs renders a tab bar + content area for help tabs.
// Used by both readme paths (device-level overview) and method paths
// (per-method documentation). Tabs follow the imp-* CSS naming
// convention used throughout the panel.
//
// Two UX rules apply uniformly:
//
//   - When len(tabs) == 1, the tab bar is suppressed and the single
//     document is rendered as one continuous body. A single tab is
//     always its own active tab; no point showing a button to "switch"
//     to it. This matches the historical UX of "the readme is the
//     overview" and removes the visually awkward "tab solitária" that
//     a one-tab method would otherwise show.
//
//   - When a tab's Title is empty, the localised string
//     `help.title.notFound` is substituted. The empty title is a
//     sentinel from the server: the markdown had no top-level "#
//     heading" line. Showing the localised fallback makes the missing
//     heading visible to the author so they go and add one, instead of
//     hiding behind a filename like "Init.1.en".
//
// Active tab index is stored in activeTabByItemID[itemID].
//
// Português: render unificado para abas de readme e de método.
// Suprime a barra quando há uma única aba e cai no fallback i18n
// quando o título está vazio (sentinel de "sem # heading").
func (p *Panel) buildHelpTabs(itemID string, tabs []blackbox.HelpTabClient) string {
	if len(tabs) == 0 {
		return ""
	}
	if len(tabs) == 1 {
		return `<div class="imp-help-body">` + markdownToHTML(tabs[0].Content) + `</div>`
	}

	active := p.activeTabByItemID[itemID]
	fallback := translate.T("help.title.notFound", "# title not found")

	bar := `<div class="imp-tab-bar">`
	for i, t := range tabs {
		title := t.Title
		if title == "" {
			title = fallback
		}
		cls := "imp-tab-btn"
		if i == active {
			cls += " imp-tab-active"
		}
		bar += fmt.Sprintf(
			`<button class="%s" data-action="help-tab" data-item="%s" data-tab="%d">%s</button>`,
			cls, escHTML(itemID), i, escHTML(title),
		)
	}
	bar += `</div>`

	var content string
	if active >= 0 && active < len(tabs) {
		content = markdownToHTML(tabs[active].Content)
	}
	return bar + `<div class="imp-help-body">` + content + `</div>`
}

// buildReadmeContent was an earlier readme-only renderer. Its rules
// (suppress bar when len==1, fall back to i18n for empty titles) were
// folded into buildHelpTabs above so a single function serves both
// readme and method help paths uniformly.

func (p *Panel) injectCSS() {
	doc := js.Global().Get("document")
	if existing := doc.Call("getElementById", panelCSSID); !existing.IsNull() {
		return
	}
	style := doc.Call("createElement", "style")
	style.Set("id", panelCSSID)
	style.Set("textContent", panelCSS)
	doc.Get("head").Call("appendChild", style)
}

// ─── Overlay creation (once per Open) ────────────────────────────────────────

func (p *Panel) createOverlay() {
	doc := js.Global().Get("document")

	html := fmt.Sprintf(
		`<div class="imp-panel">`+
			`<div class="imp-rail-col">%s%s</div>`+
			`<div class="imp-drag-handle" id="imp-drag-rail"></div>`+
			`<div id="%s" class="imp-columns">%s%s</div>`+
			`</div>`,
		p.buildCloseBtn(),
		p.buildRailButtons(),
		panelContentID,
		p.buildList(),
		p.buildPreview(""),
	)

	overlay := doc.Call("createElement", "div")
	overlay.Set("id", panelOverlayID)
	overlay.Set("innerHTML", html)
	doc.Get("body").Call("appendChild", overlay)

	p.wireClicks(overlay)
	p.wireInput(overlay)
	p.wireDragHandles(doc)
	p.loadPanelPrefs(doc)

	// Focus the search input so the maker can type immediately.
	p.focusSearch()

	// Load help for the initially selected item.
	p.loadAndRenderHelp(p.selectedItem())
}

// updatePreview replaces only the preview column — no list, no search input.
// Used by wireInput so the preview stays in sync with the first search result
// without touching the input or the list, keeping keyboard focus intact.
// Also used by the help-tab click handler when switching between readme
// tabs of a single device entry.
//
// Critical: calls enhancePreview() after the DOM swap so syntax
// highlighting, copy buttons, and steganography badges re-apply on the
// new contents. A previous version of this function only set the
// outerHTML and skipped enhancement — that left code blocks rendered as
// raw text after a tab switch (no <span class="hljs-*"> nodes), with
// only the help-tab paths affected because updateContent did the work
// inline. Centralising the enhancement keeps the two entry points in
// sync.
//
// Português: Substitui só a coluna de preview. Chama enhancePreview()
// para reaplicar destaque de sintaxe, botões de copiar e badges.
func (p *Panel) updatePreview(helpHTML string) {
	doc := js.Global().Get("document")
	el := doc.Call("getElementById", panelPreviewID)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Set("outerHTML", p.buildPreview(helpHTML))
	p.enhancePreview()
}

// updateListItems replaces only the scrollable items div inside the list
// column. The search input wrapper above it is never touched, so the
// browser never destroys the input element and never drops focus.
func (p *Panel) updateListItems() {
	doc := js.Global().Get("document")
	el := doc.Call("getElementById", panelListItemsID)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Set("innerHTML", p.buildListItems())
}

// updateContent replaces only list + preview — no animation, no flicker.
// After the swap, enhancePreview() runs the syntax highlighter, attaches
// copy buttons to each <pre>, and decorates IoTMaker-marked images.
func (p *Panel) updateContent(helpMD string) {
	doc := js.Global().Get("document")
	content := doc.Call("getElementById", panelContentID)
	if content.IsNull() || content.IsUndefined() {
		return
	}
	// buildList emits a fresh .imp-list with no inline width, so a bare
	// innerHTML swap drops the user's adjusted list-column width and snaps it
	// back to the CSS default on every category switch (Math → Logic, etc.).
	// Capture the current inline width before the swap and re-apply it after so
	// the resize sticks. The rail column lives outside panelContentID and is not
	// rebuilt here (updateRail only toggles button classes), so its width is
	// already preserved and needs no handling.
	//
	// Português: buildList gera um .imp-list sem largura inline, então trocar o
	// innerHTML zera a largura ajustada da coluna da lista a cada troca de
	// categoria. Captura a largura inline antes do swap e reaplica depois para o
	// ajuste persistir. A coluna do rail fica fora do panelContentID e não é
	// reconstruída aqui, então sua largura já é preservada.
	listW := ""
	if listCol := doc.Call("querySelector", ".imp-list"); listCol.Truthy() {
		listW = listCol.Get("style").Get("width").String()
	}
	content.Set("innerHTML", p.buildList()+p.buildPreview(helpMD))
	if listW != "" {
		if listCol := doc.Call("querySelector", ".imp-list"); listCol.Truthy() {
			listCol.Get("style").Set("width", listW)
		}
	}
	p.enhancePreview()
}

// enhancePreview applies post-render decorations to whatever is currently
// inside #ide-panel-preview:
//
//  1. Syntax-highlight every <pre><code> block with highlight.js.
//  2. Attach a copy-to-clipboard button to the top-right of each <pre>.
//  3. Scan PNG images for IoTMaker steganography markers and overlay a
//     "Load Example" button on matches.
//
// Called by both updateContent (full list+preview swap) and
// updatePreview (preview-only swap), so any path that re-renders the
// preview gets identical decoration. Keeping these two entry points on
// the same code path is the fix for the "code blocks render uncolored
// after switching help tabs" bug.
//
// The hljs portion is wrapped in a setTimeout(0) so the browser has a
// chance to paint the freshly set innerHTML/outerHTML before
// highlightElement walks it. Calling highlightElement synchronously on
// the same tick works in practice, but the deferred path matches the
// timing we documented elsewhere for layout-sensitive decorations
// (copy buttons measure pre.offsetWidth) and keeps the diagnostic logs
// after the new DOM is fully reachable.
//
// Português: Aplica destaque de sintaxe, botões de cópia e badges de
// esteganografia ao preview atual. Centraliza o trabalho que antes
// estava duplicado/inconsistente entre updateContent e updatePreview.
func (p *Panel) enhancePreview() {
	doc := js.Global().Get("document")

	hljs := js.Global().Get("hljs")
	if !hljs.Truthy() {
		// hljs was supposed to be loaded by overlay.PreloadHighlight()
		// during splash. If it's missing here something blocked that
		// preload path — surface a clear log so the cause is visible.
		log.Printf("[panel] highlight skipped: window.hljs is undefined")
	} else {
		// Diagnostic: how many languages did the preload actually
		// register? hljs.listLanguages() returns an array of ids.
		// Zero means the core loaded but every language script
		// failed (CDN unreachable, CSP block, etc) — in that case
		// highlightElement will run but classify everything as
		// "language-undefined" and emit no .hljs-* spans.
		langs := hljs.Call("listLanguages")
		log.Printf("[panel] hljs ready with %d language(s) registered",
			langs.Get("length").Int())

		// Suppress the "unescaped HTML" security warning since our
		// HTML comes from marked.parse() on trusted server-side help
		// content (no user-controlled input).
		cfg := js.Global().Get("Object").New()
		cfg.Set("ignoreUnescapedHTML", true)
		hljs.Call("configure", cfg)

		var cb js.Func
		cb = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			preview := doc.Call("getElementById", panelPreviewID)
			if preview.Truthy() {
				blocks := preview.Call("querySelectorAll", "pre code")
				n := blocks.Get("length").Int()
				log.Printf("[panel] highlighting %d <pre><code> block(s)", n)
				for i := 0; i < n; i++ {
					block := blocks.Index(i)
					// Clear previous highlighting flag so re-renders
					// of the same DOM node (e.g. after a tab switch
					// that leaves the cached HTML untouched) get
					// re-tokenized rather than being skipped.
					block.Call("removeAttribute", "data-highlighted")
					hljs.Call("highlightElement", block)
				}
				// Diagnostic: did highlightElement actually inject
				// any spans? If we ran on N blocks but the preview
				// has zero hljs-* nodes, the call was a no-op (most
				// commonly because the requested language wasn't
				// registered when the run executed).
				spans := preview.Call("querySelectorAll", "[class*=hljs-]")
				log.Printf("[panel] post-highlight: %d .hljs-* span(s) in preview",
					spans.Get("length").Int())

				p.attachCopyButtons(doc, preview)
			}
			cb.Release()
			return nil
		})
		js.Global().Call("setTimeout", cb, 0)
	}

	// Scan PNG images in the preview for IoTMaker steganography markers.
	// Images with embedded stage data get a "Load Example" button overlay.
	// Runs synchronously — image decoration does not depend on hljs
	// having finished, and we want the badges visible on first paint.
	preview := doc.Call("getElementById", panelPreviewID)
	if preview.Truthy() {
		overlay.EnhanceIoTMakerImages(doc, preview)
	}
}

// attachCopyButtons adds a copy-to-clipboard button to the top-right corner
// of each <pre> block inside the given preview container. The function is
// idempotent: a <pre> that already carries a `.imp-copy-btn` child is
// skipped, so calling enhancePreview repeatedly (which happens on every
// re-render) does not stack buttons.
//
// The button click swaps its icon to a green checkmark for 1.5s as
// visual feedback before reverting to the clipboard icon.
//
// Português: Insere um botão "copiar" no canto superior direito de cada
// bloco <pre>. Idempotente (não duplica em re-renders).
func (p *Panel) attachCopyButtons(doc, preview js.Value) {
	pres := preview.Call("querySelectorAll", "pre")
	np := pres.Get("length").Int()
	for i := 0; i < np; i++ {
		pre := pres.Index(i)
		// Skip if already has a copy button (re-render guard).
		if pre.Call("querySelector", ".imp-copy-btn").Truthy() {
			continue
		}
		pre.Get("style").Set("position", "relative")

		btn := doc.Call("createElement", "button")
		btn.Set("className", "imp-copy-btn")
		btn.Set("title", "Copy")
		btn.Set("innerHTML", `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`)

		// Capture the code element for the closure. textContent strips the
		// hljs span wrappers so the clipboard receives plain source code.
		codeEl := pre.Call("querySelector", "code")
		var clickFn js.Func
		clickFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			text := codeEl.Get("textContent").String()
			nav := js.Global().Get("navigator")
			if nav.Truthy() {
				clipboard := nav.Get("clipboard")
				if clipboard.Truthy() {
					clipboard.Call("writeText", text)
				}
			}
			// Visual feedback: swap icon to checkmark briefly.
			this.Set("innerHTML", `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#50fa7b" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`)
			var resetFn js.Func
			resetFn = js.FuncOf(func(t js.Value, a []js.Value) interface{} {
				this.Set("innerHTML", `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`)
				resetFn.Release()
				return nil
			})
			js.Global().Call("setTimeout", resetFn, 1500)
			return nil
		})
		btn.Call("addEventListener", "click", clickFn)
		pre.Call("appendChild", btn)
	}
}

// updateRail refreshes active class on rail buttons then updates content.
func (p *Panel) updateRail(helpMD string) {
	doc := js.Global().Get("document")
	btns := doc.Call("querySelectorAll", ".imp-rail-btn")
	length := btns.Get("length").Int()
	for i := 0; i < length; i++ {
		btn := btns.Call("item", i)
		catStr := ""
		v := btn.Call("getAttribute", "data-cat")
		if !v.IsNull() && !v.IsUndefined() {
			catStr = v.String()
		}
		var idx int
		fmt.Sscanf(catStr, "%d", &idx)
		if idx == p.selectedCatIdx {
			btn.Get("classList").Call("add", "imp-active")
		} else {
			btn.Get("classList").Call("remove", "imp-active")
		}
	}
	p.updateContent(helpMD)
}

// ─── Help loading ─────────────────────────────────────────────────────────────

// loadAndRenderHelp renders the preview column for the selected item.
//
// Resolution order:
//  1. Inline readme registered via SetItemReadme (from BlackBoxDefClient.Help.Readme).
//  2. Server-side help file at /help/devices/<category>/<id>/<locale>.md.
//  3. Empty preview (shows only item name).
//
// Accepts both ItemAction and ItemSubmenu so device-level readmes are shown
// when the maker selects a device entry before opening its submenu.
func (p *Panel) loadAndRenderHelp(item *hexMenu.MenuItem) {
	if item == nil {
		p.updateContent("")
		return
	}

	id := item.ID

	// 1. Inline readme tabs injected by the menu builder from
	//    BlackBoxDefClient.Help.Readme (or by mergeTreeHelp wrapping a
	//    single admin-edited markdown blob).
	//
	//    We pass "" to updateContent here — the actual rendering happens
	//    inside buildPreview, which detects the entry in readmeByItemID
	//    and calls buildHelpTabs. This keeps the initial render (here)
	//    and the tab-switch render (the help-tab click handler, which
	//    calls updatePreview("")) on a single code path. Without this
	//    unification, clicking a readme tab would land in a buildPreview
	//    branch that did not know about readmeByItemID and would render
	//    nothing — the "blue screen" symptom.
	if _, ok := p.readmeByItemID[id]; ok {
		p.updateContent("")
		return
	}

	// ItemSubmenu entries without an inline readme have nothing to show yet.
	if item.Type == hexMenu.ItemSubmenu {
		p.updateContent("")
		return
	}

	// 2. Server-side help file (legacy path for built-in devices).
	if item.Type != hexMenu.ItemAction {
		p.updateContent("")
		return
	}
	category, ok := helpCategoryByItemID[id]
	if !ok {
		p.updateContent("")
		return
	}

	locale := p.browserLocale
	go func() {
		md := fetchHelpFile(category, id, locale)
		html := markdownToHTML(md)
		p.updateContent(html)
	}()
}

// fetchHelpFile fetches /help/devices/<category>/<id>/<locale>.md.
// Falls back to "en" if the locale file returns a non-200 status.
// Returns the raw markdown string, or "" on complete failure.
func fetchHelpFile(category, id, locale string) string {
	base := rulesServer.ServerURL + rulesServer.EndpointHelp + "/devices"

	// Normalise locale: "pt-BR" → "pt-br", "en-US" → "en"
	loc := strings.ToLower(locale)
	// Strip region for "en-*" variants — our fallback file is just "en.md"
	if strings.HasPrefix(loc, "en") {
		loc = helpLocaleFallback
	}

	url := fmt.Sprintf("%s/%s/%s/%s.md", base, category, id, loc)
	md := fetchText(url)
	if md != "" {
		return md
	}

	// Fallback to English if locale file not found or returned empty.
	if loc != helpLocaleFallback {
		fallbackURL := fmt.Sprintf("%s/%s/%s/%s.md", base, category, id, helpLocaleFallback)
		md = fetchText(fallbackURL)
	}
	return md
}

// fetchText performs a synchronous GET and returns the response body as a string.
// Returns "" on any error (network, non-2xx, empty body).
func fetchText(url string) string {
	type result struct {
		body string
		ok   bool
	}
	ch := make(chan result, 1)

	thenResp := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			ch <- result{}
			return js.Null()
		}
		return resp.Call("text")
	})
	thenText := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			ch <- result{}
			return nil
		}
		ch <- result{body: args[0].String(), ok: true}
		return nil
	})
	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ch <- result{}
		return nil
	})

	js.Global().Call("fetch", url).
		Call("then", thenResp).
		Call("then", thenText).
		Call("catch", catchFn)

	res := <-ch
	thenResp.Release()
	thenText.Release()
	catchFn.Release()

	if !res.ok {
		log.Printf("[panel] help fetch failed: %s", url)
	}
	return res.body
}

// markdownToHTML converts a markdown string to HTML using window.marked.
// Returns the markdown as a <pre> block if marked is not available.
func markdownToHTML(md string) string {
	if md == "" {
		return ""
	}
	marked := js.Global().Get("marked")
	if !marked.Truthy() {
		// marked.js not yet loaded — wrap in preformatted block as fallback.
		return "<pre>" + escHTML(md) + "</pre>"
	}
	result := marked.Call("parse", md)
	if result.IsNull() || result.IsUndefined() {
		return "<pre>" + escHTML(md) + "</pre>"
	}
	return result.String()
}

// ─── HTML builders ────────────────────────────────────────────────────────────

// buildCloseBtn returns the back/close button at the top of the rail.
// Uses the same arrow-rotate-left icon as the hexagonal GoBackItem so the
// visual language is consistent between the panel and the device menus.
// Calls window._ideExit() which is exposed by ide.js and navigates the
// SPA back to the home page.
func (p *Panel) buildCloseBtn() string {
	// FontAwesome arrow-rotate-left path — same as hexMenu.GoBackItem.
	const backPath = "M48.5 224L40 224c-13.3 0-24-10.7-24-24L16 72c0-9.7 5.8-18.5 14.8-22.2s19.3-1.7 26.2 5.2L98.6 96.6c87.6-86.5 228.7-86.2 315.8 1c87.5 87.5 87.5 229.3 0 316.8s-229.3 87.5-316.8 0c-12.5-12.5-12.5-32.8 0-45.3s32.8-12.5 45.3 0c62.5 62.5 163.8 62.5 226.3 0s62.5-163.8 0-226.3c-62.2-62.2-162.7-62.5-225.3-1L185 183c6.9 6.9 8.9 17.2 5.2 26.2s-12.5 14.8-22.2 14.8L48.5 224z"
	icon := inlineSVG(backPath, "0 0 512 512", 22)
	return fmt.Sprintf(`<button class="imp-close-rail" data-action="close" title="Back">%s</button>`, icon)
}

func (p *Panel) buildRailButtons() string {
	html := ""
	for i, item := range p.items {
		active := ""
		if i == p.selectedCatIdx {
			active = " imp-active"
		}
		icon := inlineSVG(item.FontAwesomePath, item.ViewBox, 28)

		if item.BrandColor != "" {
			// Branded section — apply brand color via CSS custom property.
			// The .imp-brand class overrides default colors using --brand.
			html += fmt.Sprintf(
				`<button class="imp-rail-btn imp-brand%s" data-cat="%d" style="--brand:%s;--brand-bg:%s">%s<span class="imp-rail-label">%s</span></button>`,
				active, i, item.BrandColor, hexToRGBA(item.BrandColor, 0.12), icon, escHTML(item.Label),
			)
		} else {
			html += fmt.Sprintf(
				`<button class="imp-rail-btn%s" data-cat="%d">%s<span class="imp-rail-label">%s</span></button>`,
				active, i, icon, escHTML(item.Label),
			)
		}
	}
	return html
}

// buildList builds the full list column: search input (stable) + items div.
// Only the items div (panelListItemsID) is replaced during filtering —
// the search input is never touched.
func (p *Panel) buildList() string {
	return fmt.Sprintf(
		`<div class="imp-list">`+
			`<div class="imp-search-wrap">`+
			`<input id="%s" class="imp-search" type="text" `+
			`placeholder="Search all…" value="%s" `+
			`autocomplete="off" spellcheck="false"/>`+
			`</div>`+
			`<div id="%s" class="imp-list-items">%s</div>`+
			`</div>`+
			`<div class="imp-drag-handle" id="imp-drag-list"></div>`,
		panelSearchID, escHTML(p.filterText),
		panelListItemsID, p.buildListItems(),
	)
}

// buildListItems builds only the scrollable items inside the list column.
// Called by buildList on full rebuild and by updateListItems on filter/navigation.
func (p *Panel) buildListItems() string {
	html := ""
	isGlobalSearch := p.filterText != ""
	if !isGlobalSearch && len(p.listStack) > 0 {
		html += `<button class="imp-back" data-action="back">← Back</button>`
	}
	filter := strings.ToLower(p.filterText)
	hasVisible := false
	for i, item := range p.listItems {
		if item.ID == "SysGoBack" {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(item.Label), filter) {
			continue
		}
		hasVisible = true
		active := ""
		if i == p.selectedListIdx {
			active = " imp-active"
		}
		suffix := ""
		if isGlobalSearch {
			if cat, ok := helpCategoryByItemID[item.ID]; ok {
				suffix = fmt.Sprintf(`<span class="imp-cat-badge">%s</span>`, escHTML(cat))
			}
		} else if item.Type == hexMenu.ItemSubmenu {
			suffix = `<span class="imp-arrow">›</span>`
		}
		html += fmt.Sprintf(
			`<button class="imp-list-btn%s" data-item="%d"><span class="imp-list-label">%s</span>%s</button>`,
			active, i, escHTML(item.Label), suffix,
		)
	}
	if !hasVisible && filter != "" {
		html += `<p class="imp-no-results">No results</p>`
	}
	return html
}

// buildPreview renders the preview column.
// helpHTML is the already-converted HTML from the help markdown file.
// When empty, shows only the item name (or nothing if no item selected).
func (p *Panel) buildPreview(helpHTML string) string {
	html := fmt.Sprintf(`<div id="%s" class="imp-preview">`, panelPreviewID)

	sel := p.selectedItem()
	if sel != nil && sel.ID != "SysGoBack" {
		// Icon — shown for every item type when a FontAwesome path is available.
		if sel.FontAwesomePath != "" {
			iconStyle := ""
			if sel.BrandColor != "" {
				// Branded section — show icon on a colored circle background.
				iconStyle = fmt.Sprintf(
					` style="background:%s;border-radius:14px;padding:12px;color:#fff"`,
					sel.BrandColor,
				)
			}
			html += fmt.Sprintf(
				`<div class="imp-preview-icon"%s>%s</div>`,
				iconStyle,
				inlineSVG(sel.FontAwesomePath, sel.ViewBox, 56),
			)
		}

		html += fmt.Sprintf(`<p class="imp-preview-name">%s</p>`, escHTML(sel.Label))

		switch sel.Type {
		case hexMenu.ItemAction:
			html += `<button class="imp-place" data-action="place">+ Place on stage</button>`
			// Render priority for the help body:
			//   1. Readme tabs (device-level overview, from
			//      BlackBoxDef.Help.Readme or admin-edited menu tree).
			//   2. Method help tabs (per-method documentation from
			//      <method>[.<N>].<lang>.md files).
			//   3. Legacy server-side help file passed in via helpHTML.
			//
			// The two maps never have entries for the same sel.ID —
			// device-level items are bb_<Device>; method items are
			// bb_<Device>_<Method>. The explicit ordering documents
			// intent and would still be safe if the conventions changed.
			//
			// Critical: the help-tab click handler calls updatePreview("")
			// and lands here with helpHTML == "". Without the
			// readmeByItemID branch the tab switch would render nothing
			// and the panel would go blank ("blue screen" bug).
			if rTabs, ok := p.readmeByItemID[sel.ID]; ok && len(rTabs) > 0 {
				html += p.buildHelpTabs(sel.ID, rTabs)
			} else if tabs, ok := p.methodHelpByItemID[sel.ID]; ok && len(tabs) > 0 {
				html += p.buildHelpTabs(sel.ID, tabs)
			} else if helpHTML != "" {
				html += `<div class="imp-help-body">` + helpHTML + `</div>`
			}
		case hexMenu.ItemSubmenu:
			// Same readme-first priority as ItemAction. Submenus can
			// also carry a device-level readme (a device card the user
			// lands on before drilling into per-method blocks) or an
			// admin-edited section overview.
			if rTabs, ok := p.readmeByItemID[sel.ID]; ok && len(rTabs) > 0 {
				html += p.buildHelpTabs(sel.ID, rTabs)
			} else if helpHTML != "" {
				// Readme markdown from the tree or GitHub repo — show it as the overview.
				html += `<div class="imp-help-body">` + helpHTML + `</div>`
			} else {
				html += `<div class="imp-help-body">&nbsp;</div>`
			}
			// Always show the hint so the user knows they can click to navigate.
			html += `<p class="imp-preview-sub">Click to open subcategory</p>`
		}
	}

	return html + `</div>`
}

// selectedItem returns a pointer to the currently selected list item, or nil.
func (p *Panel) selectedItem() *hexMenu.MenuItem {
	if p.selectedListIdx < 0 || p.selectedListIdx >= len(p.listItems) {
		return nil
	}
	return &p.listItems[p.selectedListIdx]
}

// ─── Event wiring ─────────────────────────────────────────────────────────────

func (p *Panel) wireClicks(overlay js.Value) {
	fn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ev := args[0]

		// ── Image/SVG lightbox ─────────────────────────────────────────
		// If the click target is an <img> or <svg> inside .imp-help-body,
		// open a fullscreen lightbox instead of processing button actions.
		target := ev.Get("target")
		if isHelpMedia(target) {
			p.showImageLightbox(target)
			return nil
		}

		btn := nearestButton(ev.Get("target"))
		if btn.IsNull() || btn.IsUndefined() {
			return nil
		}

		switch attr(btn, "data-action") {
		case "close":
			// Close panel and exit IDE back to SPA.
			p.Close()
			exitFn := js.Global().Get("_ideExit")
			if exitFn.Truthy() {
				exitFn.Invoke()
			}
			return nil

		case "back":
			if p.filterText != "" {
				// Clear global search and restore current category.
				p.filterText = ""
				p.listItems = categorySubItems(p.items, p.selectedCatIdx)
				p.selectedListIdx = firstSelectableIdx(p.listItems)
			} else {
				p.goBack()
			}
			p.loadAndRenderHelp(p.selectedItem())
			return nil

		case "place":
			p.placeSelected()
			return nil

		case "help-tab":
			// Switch the active tab for a method help item.
			itemID := attr(btn, "data-item")
			tabIdx := 0
			if s := attr(btn, "data-tab"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					tabIdx = n
				}
			}
			p.activeTabByItemID[itemID] = tabIdx
			p.updatePreview("")
			return nil
		}

		// Rail category click.
		if catStr := attr(btn, "data-cat"); catStr != "" {
			var idx int
			fmt.Sscanf(catStr, "%d", &idx)
			if idx >= 0 && idx < len(p.items) {
				// Instant-action rail items (e.g. Exit) fire immediately
				// without entering list navigation.
				item := p.items[idx]
				if item.ID == "SysExit" {
					if item.OnClick != nil {
						p.Close()
						go item.OnClick()
					}
					return nil
				}
				p.selectedCatIdx = idx
				p.listStack = nil
				p.listItems = categorySubItems(p.items, idx)
				p.selectedListIdx = firstSelectableIdx(p.listItems)
				p.filterText = "" // reset search when changing category
				// Show content immediately without help, then load help async.
				p.updateRail("")
				p.loadAndRenderHelp(p.selectedItem())
			}
			return nil
		}

		// List item click.
		//
		// Two-step navigation for ItemSubmenu entries:
		//   1st click (item not yet selected) → select and show readme/preview.
		//   2nd click (item already selected)  → enter submenu.
		//
		// This ensures the maker always sees the readme.md content before
		// diving into the function list, regardless of whether the item was
		// auto-selected or requires a manual click.
		if itemStr := attr(btn, "data-item"); itemStr != "" {
			var idx int
			fmt.Sscanf(itemStr, "%d", &idx)
			if idx >= 0 && idx < len(p.listItems) {
				item := p.listItems[idx]
				if item.ID == "SysGoBack" {
					return nil
				}
				// Instant-action items fire OnClick immediately on list click,
				// without entering the preview stage. The Exit button uses this
				// so the maker does not need to click "Place on stage".
				if item.ID == "SysExit" {
					if item.OnClick != nil {
						p.Close()
						go item.OnClick()
					}
					return nil
				}

				alreadySelected := idx == p.selectedListIdx
				p.selectedListIdx = idx

				if item.Type == hexMenu.ItemSubmenu && len(item.Submenu) > 0 {
					if alreadySelected {
						// Second click on the same submenu item — enter it.
						p.listStack = append(p.listStack, p.listItems)
						p.listItems = item.Submenu
						p.selectedListIdx = firstSelectableIdx(p.listItems)
						p.filterText = "" // reset search when entering subcategory
					}
					// First click — just select the item. The preview column
					// below will show its readme (if registered) or the
					// "Click to open subcategory" hint.
				}

				// Show content immediately, then load help async.
				p.updateContent("")
				p.loadAndRenderHelp(p.selectedItem())
			}
			return nil
		}

		return nil
	})
	p.jsFuncs = append(p.jsFuncs, fn)
	overlay.Call("addEventListener", "click", fn)
}

// wireInput attaches a delegated "input" event listener on the overlay.
// Called once from createOverlay — survives all updateContent calls because
// it listens on the overlay element which is never replaced during navigation.
func (p *Panel) wireInput(overlay js.Value) {
	fn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ev := args[0]
		target := ev.Get("target")
		if target.IsNull() || target.IsUndefined() {
			return nil
		}
		id := target.Call("getAttribute", "id")
		if id.IsNull() || id.IsUndefined() || id.String() != panelSearchID {
			return nil
		}
		p.filterText = target.Get("value").String()
		if p.filterText != "" {
			// Global search: flat list of all ItemAction leaves across the whole menu.
			p.listItems = collectAllActions(p.items)
		} else {
			// Search cleared: restore the current category.
			p.listItems = categorySubItems(p.items, p.selectedCatIdx)
		}
		p.selectedListIdx = firstFilteredIdx(p.listItems, p.filterText)
		// Update items div (search input untouched → no focus loss).
		p.updateListItems()
		// Sync preview to show the first matching result immediately.
		// No goroutine, no help fetch — just the name and place button.
		p.updatePreview("")
		return nil
	})
	p.jsFuncs = append(p.jsFuncs, fn)
	overlay.Call("addEventListener", "input", fn)
}

// focusSearch moves keyboard focus to the search input after the overlay
// is added to the DOM. Uses setTimeout 0 so the browser has painted first.
func (p *Panel) focusSearch() {
	cb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		el := js.Global().Get("document").Call("getElementById", panelSearchID)
		if !el.IsNull() && !el.IsUndefined() {
			el.Call("focus")
		}
		return nil
	})
	// Release after the timeout fires — store temporarily so we can release.
	js.Global().Call("setTimeout", cb, 80)
	// Note: cb will be GC-eligible after the timeout fires. For a one-shot
	// setTimeout this is safe; the JS engine holds a reference until fired.
	p.jsFuncs = append(p.jsFuncs, cb)
}

func (p *Panel) placeSelected() {
	item := p.selectedItem()
	if item == nil || item.OnClick == nil {
		return
	}
	p.Close()
	go item.OnClick()
}

func (p *Panel) goBack() {
	if len(p.listStack) == 0 {
		return
	}
	p.listItems = p.listStack[len(p.listStack)-1]
	p.listStack = p.listStack[:len(p.listStack)-1]
	p.selectedListIdx = firstSelectableIdx(p.listItems)
	p.filterText = "" // reset search when going back
}

// ─── Image lightbox ───────────────────────────────────────────────────────────

// isHelpMedia returns true if el is an <img> or <svg> element inside .imp-help-body.
// Used by wireClicks to detect clicks that should open the lightbox overlay
// instead of being processed as button actions.
func isHelpMedia(el js.Value) bool {
	if el.IsNull() || el.IsUndefined() {
		return false
	}
	tag := strings.ToUpper(el.Get("tagName").String())
	if tag != "IMG" && tag != "SVG" {
		// Also check parent for clicks on <path>/<rect>/etc inside an SVG.
		parent := el.Get("parentElement")
		if parent.IsNull() || parent.IsUndefined() {
			return false
		}
		pTag := strings.ToUpper(parent.Get("tagName").String())
		if pTag == "SVG" {
			el = parent
		} else {
			// Walk up to find nearest <svg> ancestor (e.g. <path> inside nested <g>).
			el = nearestSVG(el)
			if el.IsNull() {
				return false
			}
		}
	}
	// Check that this element lives inside a .imp-help-body container.
	return isInsideHelpBody(el)
}

// nearestSVG walks up from el looking for an <svg> ancestor, stopping at body.
func nearestSVG(el js.Value) js.Value {
	for !el.IsNull() && !el.IsUndefined() {
		tag := strings.ToUpper(el.Get("tagName").String())
		if tag == "SVG" {
			return el
		}
		if tag == "BODY" || tag == "HTML" {
			break
		}
		el = el.Get("parentElement")
	}
	return js.Null()
}

// isInsideHelpBody checks whether el has an ancestor with class "imp-help-body".
func isInsideHelpBody(el js.Value) bool {
	cur := el.Get("parentElement")
	for !cur.IsNull() && !cur.IsUndefined() {
		cls := cur.Get("className")
		if !cls.IsNull() && !cls.IsUndefined() {
			if strings.Contains(cls.String(), "imp-help-body") {
				return true
			}
		}
		tag := strings.ToUpper(cur.Get("tagName").String())
		if tag == "BODY" || tag == "HTML" {
			break
		}
		cur = cur.Get("parentElement")
	}
	return false
}

// showImageLightbox creates a fullscreen overlay showing the clicked image or SVG.
// Clicking anywhere on the overlay closes it. The lightbox is self-contained
// and does not interfere with the panel's existing DOM or event wiring.
func (p *Panel) showImageLightbox(el js.Value) {
	doc := js.Global().Get("document")

	// Build overlay container.
	lb := doc.Call("createElement", "div")
	lb.Get("classList").Call("add", "imp-lightbox")

	tag := strings.ToUpper(el.Get("tagName").String())
	if tag == "IMG" {
		// Clone the image at full resolution.
		img := doc.Call("createElement", "img")
		img.Set("src", el.Get("src").String())
		lb.Call("appendChild", img)
	} else {
		// SVG — clone the node so the original stays in the help body.
		clone := el.Call("cloneNode", true)
		// Remove any max-width/max-height constraints from the inline style
		// so the lightbox CSS takes over.
		clone.Get("style").Set("cssText", "max-width:95vw;max-height:95vh;")
		// Remove cursor:zoom-in inherited from help-body rule.
		clone.Get("style").Set("cursor", "zoom-out")
		lb.Call("appendChild", clone)
	}

	// Hint text at the bottom.
	hint := doc.Call("createElement", "div")
	hint.Get("classList").Call("add", "imp-lightbox-hint")
	hint.Set("textContent", "Click anywhere to close")
	lb.Call("appendChild", hint)

	// Close on click anywhere.
	closeFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if lb.Get("parentNode").Truthy() {
			lb.Get("parentNode").Call("removeChild", lb)
		}
		return nil
	})
	lb.Call("addEventListener", "click", closeFn)
	// Store the callback so it's released when the panel closes.
	p.jsFuncs = append(p.jsFuncs, closeFn)

	// Close on Escape key.
	escFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		key := args[0].Get("key").String()
		if key == "Escape" {
			if lb.Get("parentNode").Truthy() {
				lb.Get("parentNode").Call("removeChild", lb)
			}
			// Remove this listener after closing.
			doc.Call("removeEventListener", "keydown", this)
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", escFn)
	p.jsFuncs = append(p.jsFuncs, escFn)

	doc.Get("body").Call("appendChild", lb)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// detectLocale resolves the user's preferred locale for help file paths.
//
// Priority:
//  1. localStorage "locale" — user's explicit SPA preference (set in sidebar
//     or profile page). Same-origin iframe shares localStorage with the SPA.
//  2. navigator.language — browser default (OS-level setting).
//  3. helpLocaleFallback ("en") — hardcoded last resort.
//
// The result is normalised to lowercase for file-path matching
// (e.g. "pt-BR" → "pt-br").
func detectLocale() string {
	// 1. User's explicit SPA preference from localStorage.
	storage := js.Global().Get("localStorage")
	if !storage.IsUndefined() && !storage.IsNull() {
		saved := storage.Call("getItem", "locale")
		if !saved.IsUndefined() && !saved.IsNull() {
			if s := saved.String(); s != "" {
				return strings.ToLower(s)
			}
		}
	}

	// 2. Browser locale from navigator.language.
	nav := js.Global().Get("navigator")
	if !nav.IsUndefined() && !nav.IsNull() {
		lang := nav.Get("language")
		if !lang.IsUndefined() && !lang.IsNull() {
			if s := lang.String(); s != "" {
				return strings.ToLower(s)
			}
		}
	}

	// 3. Hard fallback.
	return helpLocaleFallback
}

func categorySubItems(items []hexMenu.MenuItem, idx int) []hexMenu.MenuItem {
	if idx < 0 || idx >= len(items) {
		return nil
	}
	item := items[idx]
	if item.Type == hexMenu.ItemSubmenu && len(item.Submenu) > 0 {
		return item.Submenu
	}
	return []hexMenu.MenuItem{item}
}

func firstSelectableIdx(items []hexMenu.MenuItem) int {
	for i, it := range items {
		if it.ID != "SysGoBack" {
			return i
		}
	}
	return 0
}

// firstFilteredIdx returns the index (in items) of the first item whose label
// contains filter (case-insensitive). Falls back to firstSelectableIdx when
// no item matches or filter is empty.
// collectAllActions recursively walks the full menu tree and returns a flat
// slice of every ItemAction leaf. Used by global search so the maker can
// find any device regardless of which category it lives in.
func collectAllActions(items []hexMenu.MenuItem) []hexMenu.MenuItem {
	var result []hexMenu.MenuItem
	for _, item := range items {
		if item.ID == "SysGoBack" {
			continue
		}
		switch item.Type {
		case hexMenu.ItemAction:
			result = append(result, item)
		case hexMenu.ItemSubmenu:
			result = append(result, collectAllActions(item.Submenu)...)
		}
	}
	return result
}

func firstFilteredIdx(items []hexMenu.MenuItem, filter string) int {
	if filter == "" {
		return firstSelectableIdx(items)
	}
	f := strings.ToLower(filter)
	for i, it := range items {
		if it.ID == "SysGoBack" {
			continue
		}
		if strings.Contains(strings.ToLower(it.Label), f) {
			return i
		}
	}
	// No match — keep selection on first selectable item.
	return firstSelectableIdx(items)
}

func nearestButton(el js.Value) js.Value {
	for !el.IsNull() && !el.IsUndefined() {
		tag := el.Get("tagName").String()
		if tag == "BUTTON" {
			return el
		}
		if tag == "BODY" || tag == "HTML" {
			break
		}
		el = el.Get("parentElement")
	}
	return js.Null()
}

func attr(el js.Value, name string) string {
	v := el.Call("getAttribute", name)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	return v.String()
}

func inlineSVG(path, viewBox string, size int) string {
	if path == "" {
		return ""
	}
	if viewBox == "" {
		viewBox = "0 0 512 512"
	}
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="%s" fill="currentColor" aria-hidden="true"><path d="%s"/></svg>`,
		size, size, viewBox, path,
	)
}

func escHTML(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, '&', 'a', 'm', 'p', ';')
		case '<':
			out = append(out, '&', 'l', 't', ';')
		case '>':
			out = append(out, '&', 'g', 't', ';')
		case '"':
			out = append(out, '&', 'q', 'u', 'o', 't', ';')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// hexToRGBA converts a hex color string (e.g. "#E62E2E") to an rgba() CSS
// value with the given alpha. Returns a fallback transparent value if the
// input is malformed.
//
// Used by buildRailButtons to create semi-transparent brand color backgrounds
// for branded section items in the panel rail.
func hexToRGBA(hex string, alpha float64) string {
	if len(hex) == 7 && hex[0] == '#' {
		r, errR := strconv.ParseInt(hex[1:3], 16, 64)
		g, errG := strconv.ParseInt(hex[3:5], 16, 64)
		b, errB := strconv.ParseInt(hex[5:7], 16, 64)
		if errR == nil && errG == nil && errB == nil {
			return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", r, g, b, alpha)
		}
	}
	return "rgba(128,128,128,0.10)"
}

// ═══════════════════════════════════════════════════════════════════════════════
//  Column drag handles — resizable rail and list columns
// ═══════════════════════════════════════════════════════════════════════════════

// wireDragHandles sets up mouse-based column resizing using event delegation
// on the document. This approach survives innerHTML replacements that destroy
// and recreate the drag handle elements (e.g., updateContent rebuilds).
//
// Drag targets:
//   - imp-drag-rail: between the icon rail (col 1) and the list+preview area
//   - imp-drag-list: between the list (col 2) and the preview (col 3)
func (p *Panel) wireDragHandles(doc js.Value) {
	var (
		dragging bool
		target   string // "rail" or "list"
		moveFn   js.Func
		upFn     js.Func
	)

	moveFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if !dragging {
			return nil
		}
		evt := args[0]
		evt.Call("preventDefault")
		clientX := evt.Get("clientX").Int()

		railCol := doc.Call("querySelector", ".imp-rail-col")
		listCol := doc.Call("querySelector", ".imp-list")

		switch target {
		case "rail":
			if railCol.Truthy() {
				w := clientX
				if w < 60 {
					w = 60
				}
				if w > 200 {
					w = 200
				}
				railCol.Get("style").Set("width", strconv.Itoa(w)+"px")
			}
		case "list":
			if listCol.Truthy() {
				listRect := listCol.Call("getBoundingClientRect")
				listLeft := listRect.Get("left").Int()
				w := clientX - listLeft
				if w < 150 {
					w = 150
				}
				if w > 600 {
					w = 600
				}
				listCol.Get("style").Set("width", strconv.Itoa(w)+"px")
			}
		}
		return nil
	})
	p.jsFuncs = append(p.jsFuncs, moveFn)

	upFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if !dragging {
			return nil
		}
		dragging = false
		doc.Call("removeEventListener", "mousemove", moveFn)
		doc.Call("removeEventListener", "mouseup", upFn)
		doc.Get("body").Get("style").Set("cursor", "")
		doc.Get("body").Get("style").Set("userSelect", "")

		// Remove dragging class from both handles (only one is active, but safe).
		for _, hid := range []string{"imp-drag-rail", "imp-drag-list"} {
			h := doc.Call("getElementById", hid)
			if h.Truthy() {
				h.Get("classList").Call("remove", "imp-dragging")
			}
		}

		// Read final widths and save to server.
		railW := 96
		listW := 250
		railCol := doc.Call("querySelector", ".imp-rail-col")
		listCol := doc.Call("querySelector", ".imp-list")
		if railCol.Truthy() {
			railW = railCol.Call("getBoundingClientRect").Get("width").Int()
		}
		if listCol.Truthy() {
			listW = listCol.Call("getBoundingClientRect").Get("width").Int()
		}
		go p.savePanelPrefs(railW, listW)

		return nil
	})
	p.jsFuncs = append(p.jsFuncs, upFn)

	// Use mousedown delegation on the overlay to catch drag handles even
	// after innerHTML replacements recreate them.
	overlay := doc.Call("getElementById", panelOverlayID)
	if !overlay.Truthy() {
		return
	}

	downFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		evt := args[0]
		el := evt.Get("target")
		if !el.Truthy() {
			return nil
		}

		id := el.Get("id").String()
		var tgt string
		switch id {
		case "imp-drag-rail":
			tgt = "rail"
		case "imp-drag-list":
			tgt = "list"
		default:
			return nil
		}

		evt.Call("preventDefault")
		dragging = true
		target = tgt
		el.Get("classList").Call("add", "imp-dragging")

		doc.Get("body").Get("style").Set("cursor", "col-resize")
		doc.Get("body").Get("style").Set("userSelect", "none")
		doc.Call("addEventListener", "mousemove", moveFn)
		doc.Call("addEventListener", "mouseup", upFn)
		return nil
	})
	p.jsFuncs = append(p.jsFuncs, downFn)

	overlay.Call("addEventListener", "mousedown", downFn)
}

// loadPanelPrefs fetches saved column widths from the server and applies them.
// Runs asynchronously so it doesn't block panel rendering.
func (p *Panel) loadPanelPrefs(doc js.Value) {
	go func() {
		osName, browser := detectOSBrowser()
		token := rulesServer.GetAuthToken()
		if token == "" {
			return
		}

		url := rulesServer.ServerURL + rulesServer.EndpointPanelPrefs +
			"?os=" + osName + "&browser=" + browser

		headers := js.Global().Get("Object").New()
		headers.Set("Authorization", token)
		opts := js.Global().Get("Object").New()
		opts.Set("headers", headers)

		// Use a channel to wait for the fetch result.
		ch := make(chan [2]int, 1)

		var then1, then2 js.Func
		then1 = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resp := args[0]
			if !resp.Get("ok").Bool() {
				then1.Release()
				return js.Null()
			}
			return resp.Call("json")
		})

		then2 = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			defer then1.Release()
			defer then2.Release()

			if args[0].IsNull() || args[0].IsUndefined() {
				ch <- [2]int{0, 0}
				return nil
			}
			data := args[0].Get("data")
			if !data.Truthy() {
				ch <- [2]int{0, 0}
				return nil
			}

			railW := data.Get("rail_width").Int()
			listW := data.Get("list_width").Int()
			ch <- [2]int{railW, listW}
			return nil
		})

		js.Global().Call("fetch", url, opts).Call("then", then1).Call("then", then2)

		// Wait for the result and apply widths.
		result := <-ch
		if result[0] == 0 && result[1] == 0 {
			return // no prefs or error
		}

		railCol := doc.Call("querySelector", ".imp-rail-col")
		listCol := doc.Call("querySelector", ".imp-list")

		if railCol.Truthy() && result[0] > 0 {
			railCol.Get("style").Set("width", strconv.Itoa(result[0])+"px")
		}
		if listCol.Truthy() && result[1] > 0 {
			listCol.Get("style").Set("width", strconv.Itoa(result[1])+"px")
		}
	}()
}

// savePanelPrefs sends the current column widths to the server.
func (p *Panel) savePanelPrefs(railWidth, listWidth int) {
	osName, browser := detectOSBrowser()
	token := rulesServer.GetAuthToken()
	if token == "" {
		return
	}

	url := rulesServer.ServerURL + rulesServer.EndpointPanelPrefs
	body := fmt.Sprintf(`{"os":"%s","browser":"%s","rail_width":%d,"list_width":%d}`,
		osName, browser, railWidth, listWidth)

	headers := js.Global().Get("Object").New()
	headers.Set("Authorization", token)
	headers.Set("Content-Type", "application/json")
	opts := js.Global().Get("Object").New()
	opts.Set("method", "PUT")
	opts.Set("headers", headers)
	opts.Set("body", body)

	js.Global().Call("fetch", url, opts)
}

// detectOSBrowser parses navigator.userAgent to extract a short OS name and
// browser name. Returns ("unknown","unknown") if parsing fails.
//
// Examples:
//
//	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ... Chrome/120"  → "macos", "chrome"
//	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) ... Firefox/121"      → "windows", "firefox"
//	"Mozilla/5.0 (X11; Linux x86_64) ... Chrome/120"                 → "linux", "chrome"
func detectOSBrowser() (string, string) {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return "unknown", "unknown"
	}
	ua := nav.Get("userAgent")
	if !ua.Truthy() {
		return "unknown", "unknown"
	}
	raw := strings.ToLower(ua.String())

	// Detect OS.
	osName := "unknown"
	switch {
	case strings.Contains(raw, "macintosh") || strings.Contains(raw, "mac os"):
		osName = "macos"
	case strings.Contains(raw, "windows"):
		osName = "windows"
	case strings.Contains(raw, "linux"):
		osName = "linux"
	case strings.Contains(raw, "android"):
		osName = "android"
	case strings.Contains(raw, "iphone") || strings.Contains(raw, "ipad"):
		osName = "ios"
	case strings.Contains(raw, "chromeos") || strings.Contains(raw, "cros"):
		osName = "chromeos"
	}

	// Detect browser (order matters — check specific before generic).
	browser := "unknown"
	switch {
	case strings.Contains(raw, "edg/"):
		browser = "edge"
	case strings.Contains(raw, "opr/") || strings.Contains(raw, "opera"):
		browser = "opera"
	case strings.Contains(raw, "firefox"):
		browser = "firefox"
	case strings.Contains(raw, "safari") && !strings.Contains(raw, "chrome"):
		browser = "safari"
	case strings.Contains(raw, "chrome"):
		browser = "chrome"
	}

	return osName, browser
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const panelCSS = `
@keyframes imp-fade-in {
  from { opacity: 0; }
  to   { opacity: 1; }
}
@keyframes imp-slide-up {
  from { opacity: 0; transform: translateY(28px); }
  to   { opacity: 1; transform: translateY(0); }
}
#ide-main-panel-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.65);
  z-index: 2000;
  display: flex;
  align-items: stretch;
  animation: imp-fade-in 0.2s ease;
}
.imp-panel {
  display: flex;
  flex-direction: row;
  width: 100vw;
  height: 100vh;
  background: #1e1e2e;
  font-family: Arial, sans-serif;
  animation: imp-slide-up 0.3s cubic-bezier(0.22, 1, 0.36, 1);
}
.imp-rail-col {
  width: 96px;
  flex-shrink: 0;
  background: #151520;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  border-right: 1px solid #2a2a40;
  scrollbar-width: none;          /* Firefox */
  -ms-overflow-style: none;       /* IE / legacy Edge */
}
.imp-rail-col::-webkit-scrollbar { display: none; } /* Chrome, Safari, Edge */
.imp-close-rail {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 16px 0 14px;
  background: none;
  border: none;
  border-left: 3px solid transparent;
  border-bottom: 1px solid #2a2a40;
  color: #555;
  font-size: 22px;
  cursor: pointer;
  transition: color .15s, background .15s;
}
.imp-close-rail:hover { color: #fff; background: rgba(255,80,80,0.14); }
.imp-rail-btn {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  padding: 14px 6px 12px;
  background: none;
  border: none;
  border-left: 3px solid transparent;
  color: #777;
  cursor: pointer;
  transition: background .12s, color .12s, border-color .12s;
}
.imp-rail-btn:hover { background: rgba(255,255,255,0.06); color: #bbb; }
.imp-rail-btn.imp-active {
  background: rgba(108,142,255,0.12);
  color: #fff;
  border-left-color: #6c8eff;
}
/* Branded section — colored tint + left border using CSS custom properties
   set inline by buildRailButtons: --brand (hex color), --brand-bg (rgba). */
.imp-rail-btn.imp-brand {
  background: var(--brand-bg);
  border-left-color: var(--brand);
  color: var(--brand);
}
.imp-rail-btn.imp-brand:hover {
  background: var(--brand-bg);
  color: var(--brand);
  filter: brightness(1.3);
}
.imp-rail-btn.imp-brand.imp-active {
  background: var(--brand-bg);
  color: #fff;
  border-left-color: var(--brand);
  filter: brightness(1.4);
}
.imp-rail-label {
  font-size: 13px;
  line-height: 1.2;
  text-align: center;
  word-break: break-word;
  max-width: 84px;
}
.imp-columns {
  display: flex;
  flex-direction: row;
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.imp-list {
  width: 250px;
  flex-shrink: 0;
  background: #1a1a28;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-right: 1px solid #2a2a40;
}
/* Search input */
.imp-search-wrap {
  padding: 10px 12px 6px;
  border-bottom: 1px solid #2a2a40;
  flex-shrink: 0;
}
.imp-search {
  width: 100%;
  box-sizing: border-box;
  background: #12121e;
  border: 1px solid #2a2a40;
  border-radius: 6px;
  color: #ddd;
  font-size: 14px;
  padding: 7px 10px;
  outline: none;
  transition: border-color .15s;
}
.imp-search:focus { border-color: #6c8eff; }
.imp-search::placeholder { color: #444; }
.imp-cat-badge {
  font-size: 10px;
  color: #555;
  background: #1e1e2e;
  border: 1px solid #2a2a40;
  border-radius: 4px;
  padding: 2px 5px;
  flex-shrink: 0;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.imp-no-results {
  color: #555;
  font-size: 13px;
  padding: 16px;
  margin: 0;
  text-align: center;
}
/* Items container inside the list column — the only part replaced on filter */
.imp-list-items {
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  flex: 1;
}
.imp-back {
  display: flex;
  align-items: center;
  background: none;
  border: none;
  color: #6c8eff;
  cursor: pointer;
  font-size: 13px;
  padding: 10px 16px;
  text-align: left;
}
.imp-back:hover { background: rgba(108,142,255,0.10); }
.imp-list-btn {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: none;
  border: none;
  color: #999;
  cursor: pointer;
  font-size: 15px;
  padding: 12px 16px;
  text-align: left;
  gap: 6px;
}
.imp-list-btn:hover { background: rgba(255,255,255,0.06); color: #ddd; }
.imp-list-btn.imp-active { background: rgba(108,142,255,0.18); color: #fff; }
.imp-list-label { flex: 1; }
.imp-arrow { color: #555; font-size: 18px; flex-shrink: 0; }
.imp-preview {
  flex: 1;
  min-width: 0;
  padding: 28px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  overflow: hidden;
}
/* Preview icon — large, above the item name */
.imp-preview-icon {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  color: #6c8eff;
  opacity: 0.85;
  flex-shrink: 0;
}
.imp-preview-name {
  font-size: 26px;
  font-weight: 600;
  color: #fff;
  margin: 0;
  line-height: 1.3;
  flex-shrink: 0;
}
.imp-preview-sub { font-size: 15px; color: #555; margin: 0; }
.imp-place {
  padding: 12px 26px;
  background: #6c8eff;
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 16px;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
  align-self: flex-start;
  transition: background .15s;
}
.imp-place:hover { background: #5575ee; }
/* Help body — markdown rendered by marked.js */
.imp-help-body {
  color: #aaa;
  font-size: 14px;
  line-height: 1.7;
  border-top: 1px solid #2a2a40;
  padding-top: 16px;
  min-width: 0;
  flex: 1;
  overflow-x: hidden;
  overflow-y: auto;
  scrollbar-width: none;
  -ms-overflow-style: none;
}
.imp-help-body::-webkit-scrollbar { display: none; }
.imp-help-body h1 { color: #eee; font-size: 20px; font-weight: 700; margin: 14px 0 8px; border-bottom: 1px solid #2a2a40; padding-bottom: 6px; }
.imp-help-body h2 { color: #ddd; font-size: 17px; font-weight: 600; margin: 12px 0 6px; }
.imp-help-body h3 { color: #ccc; font-size: 14px; font-weight: 600; margin: 10px 0 4px; }
.imp-help-body h4 { color: #bbb; font-size: 13px; font-weight: 600; margin: 8px 0 4px; }
.imp-help-body p  { margin: 0 0 8px; }
.imp-help-body code { font-family:'Fira Code','Consolas',monospace; font-size:12px; }
.imp-help-body :not(pre)>code { background:#2a2a40; padding:1px 5px; border-radius:3px; color:#e0e0ff; }
.imp-help-body pre  { background:#282a36; padding:12px; border-radius:6px; overflow-x:auto; margin:8px 0; position:relative; }
.imp-copy-btn {
  position:absolute; top:6px; right:6px;
  background:rgba(255,255,255,0.08); border:1px solid rgba(255,255,255,0.12);
  border-radius:4px; padding:4px 6px; cursor:pointer;
  color:#888; display:flex; align-items:center; justify-content:center;
  opacity:0; transition:opacity .2s, background .15s;
}
.imp-help-body pre:hover .imp-copy-btn { opacity:1; }
.imp-copy-btn:hover { background:rgba(255,255,255,0.15); color:#ddd; }
.imp-help-body pre code { background:none; padding:0; color:#f8f8f2; }
.imp-help-body pre code.hljs { background:#282a36; }
/*
 * highlight.js token classes — full Dracula palette.
 *
 * Token categories (top of each rule block) come from highlight.js's
 * canonical scope list. The local /highlight/dracula.min.css that
 * used to ship next to the core is unusable in this deployment —
 * it's an HTML 404 page saved as CSS — so we ship the theme inline
 * here, scoped to .imp-help-body so the rules can't leak into other
 * parts of the panel.
 *
 * Why these specific classes: highlight.js emits ~30 distinct scope
 * names across languages. A C "#include <foo.h>" alone produces
 * hljs-meta (#include), hljs-meta-string (<foo.h>), hljs-keyword
 * (const), hljs-type (char), and so on. Without rules for each, the
 * tokens get correctly tagged in the DOM but render as plain text
 * because the browser has no color binding for the class. The list
 * below mirrors the official Dracula theme published with hljs.
 */
.imp-help-body .hljs-keyword          { color:#ff79c6; }
.imp-help-body .hljs-type             { color:#8be9fd; font-style:italic; }
.imp-help-body .hljs-built_in         { color:#8be9fd; }
.imp-help-body .hljs-function         { color:#50fa7b; }
.imp-help-body .hljs-function .hljs-title { color:#50fa7b; }
.imp-help-body .hljs-title            { color:#50fa7b; }
.imp-help-body .hljs-title.class_     { color:#8be9fd; font-style:italic; }
.imp-help-body .hljs-class .hljs-title { color:#8be9fd; font-style:italic; }
.imp-help-body .hljs-string           { color:#f1fa8c; }
.imp-help-body .hljs-number           { color:#bd93f9; }
.imp-help-body .hljs-literal          { color:#bd93f9; }
.imp-help-body .hljs-symbol           { color:#bd93f9; }
.imp-help-body .hljs-comment          { color:#6272a4; }
.imp-help-body .hljs-quote            { color:#6272a4; font-style:italic; }
.imp-help-body .hljs-doctag           { color:#ff79c6; }
.imp-help-body .hljs-params           { color:#ffb86c; }
.imp-help-body .hljs-attr             { color:#50fa7b; }
.imp-help-body .hljs-attribute        { color:#50fa7b; }
.imp-help-body .hljs-variable         { color:#f8f8f2; }
.imp-help-body .hljs-template-variable { color:#ff79c6; }
.imp-help-body .hljs-subst            { color:#f8f8f2; }
.imp-help-body .hljs-operator         { color:#ff79c6; }
.imp-help-body .hljs-punctuation      { color:#f8f8f2; }
.imp-help-body .hljs-meta             { color:#50fa7b; }
.imp-help-body .hljs-meta .hljs-keyword { color:#ff79c6; }
.imp-help-body .hljs-meta .hljs-string { color:#f1fa8c; }
.imp-help-body .hljs-meta-string      { color:#f1fa8c; }
.imp-help-body .hljs-meta-keyword     { color:#ff79c6; font-weight:bold; }
.imp-help-body .hljs-name             { color:#ff79c6; }
.imp-help-body .hljs-tag              { color:#ff79c6; }
.imp-help-body .hljs-selector-tag     { color:#ff79c6; }
.imp-help-body .hljs-selector-class   { color:#50fa7b; }
.imp-help-body .hljs-selector-id      { color:#bd93f9; }
.imp-help-body .hljs-selector-attr    { color:#50fa7b; }
.imp-help-body .hljs-selector-pseudo  { color:#bd93f9; }
.imp-help-body .hljs-property         { color:#8be9fd; }
.imp-help-body .hljs-section          { color:#50fa7b; font-weight:bold; }
.imp-help-body .hljs-bullet           { color:#ffb86c; }
.imp-help-body .hljs-link             { color:#8be9fd; text-decoration:underline; }
.imp-help-body .hljs-emphasis         { font-style:italic; }
.imp-help-body .hljs-strong           { font-weight:bold; }
.imp-help-body .hljs-regexp           { color:#ff5555; }
.imp-help-body .hljs-formula          { color:#bd93f9; }
.imp-help-body .hljs-template-tag     { color:#ff79c6; }
.imp-help-body .hljs-deletion         { color:#ff5555; }
.imp-help-body .hljs-addition         { color:#50fa7b; }
.imp-help-body table { border-collapse:collapse; width:100%; margin:8px 0; }
.imp-help-body th { background:#2a2a40; color:#ddd; padding:6px 10px; text-align:left; font-size:13px; }
.imp-help-body td { padding:5px 10px; border-bottom:1px solid #2a2a40; font-size:13px; }
.imp-help-body strong { color:#ddd; }
.imp-help-body img { max-width:100%; max-height:60vh; height:auto; object-fit:contain; display:block; border-radius:4px; margin:8px 0; cursor:zoom-in; }
/* Constrain inline SVGs rendered from markdown so they fit the preview column. */
.imp-help-body svg { max-width:100%; max-height:60vh; height:auto; object-fit:contain; display:block; margin:8px 0; cursor:zoom-in; }
.imp-copy-btn svg { max-width:none; max-height:none; display:inline; margin:0; cursor:pointer; }
/* ── Image lightbox (click-to-expand) ── */
.imp-lightbox {
  position:fixed; inset:0; z-index:10000;
  background:rgba(0,0,0,0.88);
  display:flex; align-items:center; justify-content:center;
  cursor:zoom-out;
  animation: imp-fade-in 0.15s ease;
}
.imp-lightbox img, .imp-lightbox svg {
  max-width:95vw; max-height:95vh; object-fit:contain;
  border-radius:6px;
  cursor:zoom-out;
}
.imp-lightbox-hint {
  position:absolute; bottom:18px; left:50%;
  transform:translateX(-50%);
  color:#666; font-size:13px; font-family:Arial,sans-serif;
  pointer-events:none;
}
.imp-help-body blockquote { border-left:3px solid #4455bb; margin:8px 0; padding:4px 12px; color:#888; font-size:13px; }
/* ── Method help tabs ── */
.imp-tab-bar {
  display: flex;
  gap: 0;
  flex-wrap: wrap;
  margin-top: 20px;
  border-bottom: 1px solid #4455bb;
}
.imp-tab-btn {
  background: #12121e;
  border: 1px solid #2a2a40;
  border-bottom: none;
  border-radius: 4px 4px 0 0;
  color: #666;
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  padding: 5px 12px;
  margin-right: 2px;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.imp-tab-btn:hover { background: #1e1e30; color: #bbb; border-color: #3a3a5a; }
.imp-tab-active {
  background: #1e1e2e !important;
  color: #e0e0ff !important;
  border-color: #4455bb !important;
  border-bottom-color: #1e1e2e !important;
  margin-bottom: -1px;
  z-index: 1;
  position: relative;
}
/* Adjacent sibling: remove top border of help-body when it follows a tab bar
   so only one horizontal line appears (the tab bar's own border-bottom). */
.imp-tab-bar + .imp-help-body {
  border-top: none;
  padding-top: 12px;
}

/* ── Column drag handles ──────────────────────────────────────────────── */
.imp-drag-handle {
  width: 5px;
  flex-shrink: 0;
  cursor: col-resize;
  background: transparent;
  transition: background 0.15s;
  position: relative;
  z-index: 10;
}
.imp-drag-handle:hover, .imp-drag-handle.imp-dragging {
  background: rgba(108, 142, 255, 0.4);
}
/* Wider invisible hit area for easier grabbing */
.imp-drag-handle::before {
  content: '';
  position: absolute;
  top: 0; bottom: 0;
  left: -3px; right: -3px;
}
`
