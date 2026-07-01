// ui/overlay/overlay.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package overlay

// overlay.go — Draggable floating panel with tabs for form, markdown, and Monaco editor.
//
// Usage:
//
//	handle := overlay.Show(overlay.Config{
//	    Title: "ConstInt — Properties",
//	    Tabs: []overlay.Tab{
//	        {Label: "Properties", Type: overlay.TabForm, Fields: [...]},
//	        {Label: "Help", Type: overlay.TabMarkdown, Content: "# ConstInt\n..."},
//	    },
//	    OnSave: func(values map[string]string) { ... },
//	})
//
// The panel creates a semi-transparent backdrop, a draggable window with a thin
// title bar, and a tab strip that switches between content panels.
//
// External scripts (Monaco, Marked) are loaded lazily on first use from CDN.
//
// Português: Painel flutuante arrastável com abas para formulário, markdown e Monaco.
// Scripts externos são carregados preguiçosamente do CDN no primeiro uso.

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/steganography"
)

// Library URLs — configurable. Default to CDN, can be overridden to
// point to the local server (e.g. "http://localhost:8080/static/monaco/vs/loader.js").
//
// Set these BEFORE any overlay is opened (e.g. in main.go during init).
//
// Português: URLs das bibliotecas — configuráveis. Padrão é CDN, pode ser
// sobrescrito para apontar ao servidor local.
var (
	MonacoLoaderURL = "/monaco/vs/loader.js"
	MonacoBaseURL   = "/monaco/vs"
	MarkedURL       = "/marked/marked.min.js"
	HighlightURL    = "/highlight/highlight.min.js"
)

// Script load state.
var (
	markedLoaded  bool
	monacoLoaded  bool
	hljsLoaded    bool
	markedLoading bool
	monacoLoading bool
	hljsLoading   bool
)

// ── IoTMaker image example loading ─────────────────────────────────────────
//
// When the Help tab renders markdown containing PNG images, each image is
// checked for the "IOTM" steganography marker. If found, a "Load Example"
// button overlay is added. Clicking it extracts the embedded scene JSON
// and calls globalOnLoadExample to reconstruct the stage.
//
// Português: Quando o Help renderiza markdown com imagens PNG, cada imagem
// é verificada para o marcador "IOTM". Se encontrado, um botão "Load Example"
// é adicionado. Clicar extrai o JSON e chama globalOnLoadExample.

var (
	// globalOnLoadExample is called when the user clicks "Load Example" on
	// an IoTMaker-enhanced image. Set by the workspace during Init via
	// SetOnLoadExample().
	globalOnLoadExample func(sceneJSON string)

	// globalOnBeforeLoadExample is called just before loading an example.
	// Used by the workspace to close any open menu or panel before the
	// stage is reconstructed. Set via SetOnBeforeLoadExample().
	globalOnBeforeLoadExample func()

	// loadExampleLabel is the translated button text. Set by the workspace
	// via SetLoadExampleLabel() so the overlay package stays i18n-agnostic.
	loadExampleLabel = "▶ Load Example"
)

// SetOnLoadExample sets the global callback for loading examples from images.
func SetOnLoadExample(fn func(sceneJSON string)) {
	globalOnLoadExample = fn
}

// SetOnBeforeLoadExample sets a callback invoked before loading an example.
// Use this to close menus, panels, or overlays before the stage is rebuilt.
func SetOnBeforeLoadExample(fn func()) {
	globalOnBeforeLoadExample = fn
}

// SetLoadExampleLabel sets the translated label for the "Load Example" button.
func SetLoadExampleLabel(label string) {
	loadExampleLabel = label
}

// EnhanceIoTMakerImages is the public entry point for scanning images in a
// DOM container for the IOTM steganography marker. Called by the overlay's
// own markdown renderer and also by the hardware menu panel.
//
// Português: Ponto de entrada público para varrer imagens em um container DOM.
func EnhanceIoTMakerImages(doc js.Value, container js.Value) {
	enhanceIoTMakerImages(doc, container)
}

// pendingScrollRestore holds the scrollTop value of the help content area
// saved just before an OnSaveReopen close. The next Show() call restores
// this position so the user doesn't lose their place in the documentation
// when the overlay rebuilds with updated prop values.
//
// This is especially important for the embedded properties mode: the user
// may be deep in the documentation when they click Apply, and losing scroll
// would be disorienting.
//
// Lifecycle: saved in OnSaveReopen wrapper → restored in Show() → cleared.
var pendingScrollRestore float64

// panelGeometry is one captured panel rectangle (viewport pixels). has
// distinguishes a real capture from the zero value (a fresh Show with no prior
// reopen).
type panelGeometry struct {
	has    bool
	left   float64
	top    float64
	width  float64
	height float64
}

// pendingGeometryRestore holds the panel's position and size saved just before
// an OnSaveReopen close, so the next Show() reopens the panel exactly where the
// user left it instead of snapping back to the centred default. The user may
// have dragged or resized the inspector before clicking Apply; losing that on
// every reopen is jarring.
//
// Captured via getBoundingClientRect (works whether the panel is still centred
// via transform or already absolutely positioned from a drag/resize) and
// reapplied as absolute left/top + explicit width/height. The drag and resize
// handlers re-measure the live rect on their first interaction, so they cope
// with the reopened panel being absolutely positioned without extra wiring.
//
// Lifecycle: saved in OnSaveReopen wrapper → restored in Show() → cleared.
var pendingGeometryRestore panelGeometry

// =====================================================================
//  Color palette — Catppuccin Mocha (matches IDE theme)
// =====================================================================

const (
	colBase     = "#1e1e2e" // window background
	colMantle   = "#181825" // code/content background
	colSurface0 = "#313244" // title bar, tab bar
	colSurface1 = "#45475a" // borders, inactive tab
	colSurface2 = "#585b70" // hover
	colText     = "#cdd6f4" // primary text
	colSubtext  = "#a6adc8" // secondary text
	colBlue     = "#89b4fa" // primary action
	colGreen    = "#a6e3a1" // code text, success
	colRed      = "#f38ba8" // close, error
	colPeach    = "#fab387" // active tab accent
	colOverlay0 = "#6c7086" // placeholder
)

// =====================================================================
//  Show — creates and displays the overlay
// =====================================================================

// Show creates a floating overlay panel and adds it to the document body.
// Returns a Handle that can be used to close the overlay programmatically.
func Show(cfg Config) Handle {
	if cfg.Width == "" {
		cfg.Width = "600px"
	}
	if cfg.Height == "" {
		cfg.Height = "85vh"
	}

	doc := js.Global().Get("document")

	// Lock page scroll while the overlay is open.
	//
	// Two-part fix for scroll propagation:
	//
	//   Part 1 — body.overflow:hidden (page-level guard):
	//     Prevents the iframe's own body from scrolling when wheel events
	//     escape the overlay's inner containers. Simple and effective for
	//     textarea and markdown areas.
	//
	//   Part 2 — wheel preventDefault on backdrop, bubble phase:
	//     When Monaco receives a wheel event it handles the scroll internally
	//     (moves the viewport). If the user reaches the top or bottom edge and
	//     keeps scrolling, Monaco does NOT consume the event — it bubbles up
	//     through Monaco's DOM, through the overlay, reaches the backdrop, and
	//     then the browser tries to scroll a parent element via "scroll chaining".
	//     Scroll chaining can cross iframe boundaries (same-origin), causing the
	//     parent SPA frame to scroll or re-render.
	//     Calling preventDefault() in the BUBBLE phase (after Monaco has had a
	//     chance to process the event first) stops this chaining without
	//     interfering with Monaco's own internal scrolling.
	//     { passive: false } is required — browsers otherwise ignore preventDefault
	//     on wheel events registered as passive.
	//
	// Português:
	//   Part 1: overflow:hidden no body impede scroll no iframe.
	//   Part 2: preventDefault na fase bubble impede scroll chaining
	//   iframe→frame pai que causava redirecionamento para a landing page.
	body := doc.Get("body")
	prevOverflow := body.Get("style").Get("overflow").String()
	body.Get("style").Set("overflow", "hidden")

	// ---- Panel (draggable) ----
	panel := doc.Call("createElement", "div")
	panel.Get("style").Set("cssText", fmt.Sprintf(
		"position:absolute;top:8vh;left:50%%;transform:translateX(-50%%);"+
			"width:%s;max-height:%s;"+
			"background:%s;border:1px solid %s;border-radius:6px;"+
			"display:flex;flex-direction:column;overflow:hidden;"+
			"box-shadow:0 12px 40px rgba(0,0,0,0.6);font-family:sans-serif;",
		cfg.Width, cfg.Height, colBase, colSurface1))

	// ---- Backdrop ----
	backdrop := doc.Call("createElement", "div")
	backdrop.Get("style").Set("cssText",
		"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
			"background:rgba(0,0,0,0.45);z-index:99999;")

	// Part 2 of scroll fix: preventDefault on wheel events that reach the
	// backdrop (bubble phase). The listener is registered with { passive: false }
	// so the browser allows preventDefault(). stopPropagation is NOT called —
	// Monaco must still receive the event in capture phase to scroll internally.
	wheelFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		args[0].Call("preventDefault")
		return nil
	})
	listenerOpts := js.Global().Get("Object").New()
	listenerOpts.Set("passive", false)
	listenerOpts.Set("capture", false) // bubble phase — after Monaco processes
	backdrop.Call("addEventListener", "wheel", wheelFn, listenerOpts)

	// Close helper — restores body scroll lock and releases the wheel handler.
	removeFn := func() {
		if backdrop.Get("parentNode").Truthy() {
			body.Get("style").Set("overflow", prevOverflow)
			wheelFn.Release()
			doc.Get("body").Call("removeChild", backdrop)
			if cfg.OnClose != nil {
				cfg.OnClose()
			}
		}
	}

	// When OnSaveReopen is set, wrap the original OnSave so the overlay
	// closes after saving and then calls the reopen function. This is the
	// mechanism for the interactive diagram to reflect prop changes: the
	// user edits props, clicks Save, the panel closes and reopens fresh
	// with updated values and the SVG showing the new connections.
	//
	// Before closing, the scroll position of the help content area is saved
	// so the reopened overlay can restore it — the user doesn't lose their
	// place in the documentation.
	if cfg.OnSaveReopen != nil {
		originalOnSave := cfg.OnSave
		reopenFn := cfg.OnSaveReopen
		cfg.OnSave = func(values map[string]string) {
			if originalOnSave != nil {
				originalOnSave(values)
			}
			// Save scroll position before closing so it can be restored
			// in the reopened overlay instance.
			scrollEl := backdrop.Call("querySelector", "[data-help-scroll]")
			if scrollEl.Truthy() {
				pendingScrollRestore = scrollEl.Get("scrollTop").Float()
			}
			// Save the panel's current position and size so the reopened
			// overlay lands exactly where the user left it — they may have
			// dragged or resized it before clicking Apply.
			grect := panel.Call("getBoundingClientRect")
			pendingGeometryRestore = panelGeometry{
				has:    true,
				left:   grect.Get("left").Float(),
				top:    grect.Get("top").Float(),
				width:  grect.Get("width").Float(),
				height: grect.Get("height").Float(),
			}
			removeFn()
			reopenFn()
		}
	}

	// ---- Title bar ----
	titleBar := buildTitleBar(doc, cfg.Title, cfg.Actions, removeFn, panel)
	panel.Call("appendChild", titleBar)

	// ---- Tab bar + content area ----
	tabBar, contentArea := buildTabs(doc, cfg, removeFn)
	panel.Call("appendChild", tabBar)
	panel.Call("appendChild", contentArea)

	// ---- Restore scroll position from previous OnSaveReopen cycle --------
	// When the overlay was closed and reopened via OnSaveReopen, the scroll
	// position of the help content area was saved in pendingScrollRestore.
	// Restore it after a short delay to ensure marked.js has finished
	// rendering the markdown content. Since marked.js is already loaded at
	// this point (it was used in the previous instance), rendering is
	// synchronous — a 50ms delay is more than sufficient for the browser
	// to complete layout.
	if pendingScrollRestore > 0 {
		scrollToRestore := pendingScrollRestore
		pendingScrollRestore = 0
		js.Global().Call("setTimeout",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				scrollEl := backdrop.Call("querySelector", "[data-help-scroll]")
				if scrollEl.Truthy() {
					scrollEl.Set("scrollTop", scrollToRestore)
				}
				return nil
			}), 50)
	}

	// ---- Restore panel geometry from previous OnSaveReopen cycle ----------
	// When the overlay was closed and reopened via OnSaveReopen, the panel's
	// position and size were saved in pendingGeometryRestore. Reapply them so
	// a drag/resize the user did before Apply survives the reopen, overriding
	// the centred default set at panel creation. Applied as absolute left/top
	// plus explicit width/height (height pins both height and max-height, since
	// the default uses max-height only).
	if pendingGeometryRestore.has {
		g := pendingGeometryRestore
		pendingGeometryRestore = panelGeometry{}
		ps := panel.Get("style")
		ps.Set("left", fmt.Sprintf("%.0fpx", g.left))
		ps.Set("top", fmt.Sprintf("%.0fpx", g.top))
		ps.Set("transform", "none")
		ps.Set("width", fmt.Sprintf("%.0fpx", g.width))
		ps.Set("maxHeight", fmt.Sprintf("%.0fpx", g.height))
		ps.Set("height", fmt.Sprintf("%.0fpx", g.height))
	}

	// ---- Drag logic (mouse + touch) ----
	attachDragLogic(doc, titleBar, panel)

	// ---- Resize handle (bottom-right corner) ----
	attachResizeHandle(doc, panel)

	backdrop.Call("appendChild", panel)

	// Backdrop click → close
	backdrop.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("target").Equal(backdrop) {
				removeFn()
			}
			return nil
		}))

	// Escape → close
	escFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("key").String() == "Escape" {
			removeFn()
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", escFn)

	doc.Get("body").Call("appendChild", backdrop)

	return Handle{Close: removeFn, Panel: panel}
}

// =====================================================================
//  Title bar
// =====================================================================

// buildTitleBar constructs the draggable header for an overlay
// panel. It renders, left to right:
//
//   - title text (ellipsised if too long)
//   - any custom actions (Config.Actions) as labelled buttons
//   - maximize toggle (green circle)
//   - close (red circle)
//
// Actions sit BEFORE the maximize button so the row reads from
// "what this overlay can do" → "window controls". Empty actions
// render exactly the same bar as before this parameter existed,
// keeping every existing call site visually identical.
//
// Português: Constrói a barra de título arrastável do overlay.
// Ordem: título → ações customizadas → maximizar → fechar.
// Lista vazia de ações = comportamento idêntico ao anterior.
func buildTitleBar(doc js.Value, title string, actions []Action, closeFn func(), panel js.Value) js.Value {
	bar := doc.Call("createElement", "div")
	bar.Get("style").Set("cssText", fmt.Sprintf(
		"height:30px;min-height:30px;"+
			"background:%s;border-bottom:1px solid %s;"+
			"display:flex;align-items:center;justify-content:space-between;"+
			"padding:0 8px;cursor:move;user-select:none;-webkit-user-select:none;",
		colSurface0, colSurface1))

	// Title
	titleEl := doc.Call("createElement", "span")
	titleEl.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:11px;font-weight:600;flex:1;"+
			"white-space:nowrap;overflow:hidden;text-overflow:ellipsis;", colText))
	titleEl.Set("textContent", title)
	bar.Call("appendChild", titleEl)

	// Button container (right side)
	btnGroup := doc.Call("createElement", "div")
	btnGroup.Get("style").Set("cssText",
		"display:flex;align-items:center;gap:6px;flex-shrink:0;")

	// ── Custom actions ───────────────────────────────────────────────────
	// Rendered as small text buttons with optional FA icons. Sit between
	// the title and the window controls so the bar reads as "what" →
	// "window". Each action carries its own OnClick closure; clicking
	// stops propagation so the title bar's mousedown-drag doesn't fire.
	for _, act := range actions {
		a := act // capture
		btn := doc.Call("createElement", "button")
		btn.Get("style").Set("cssText", fmt.Sprintf(
			"background:transparent;border:1px solid %s;border-radius:3px;"+
				"color:%s;font-size:11px;font-weight:500;cursor:pointer;"+
				"padding:3px 8px;display:flex;align-items:center;gap:5px;"+
				"transition:background 0.12s;",
			colSurface1, colText))
		if a.Icon != "" {
			icon := doc.Call("createElement", "i")
			// Default to fa-solid; callers that need brands/regular can
			// embed the full class via a small wrapper since Action.Icon
			// is intentionally just the name. Solid covers the typical
			// "download / copy / share" verbs that surface in titlebars.
			icon.Set("className", "fa-solid fa-"+a.Icon)
			icon.Get("style").Set("cssText", "font-size:11px;")
			icon.Call("setAttribute", "aria-hidden", "true")
			btn.Call("appendChild", icon)
		}
		if a.Label != "" {
			lbl := doc.Call("createElement", "span")
			lbl.Set("textContent", a.Label)
			btn.Call("appendChild", lbl)
		}
		btn.Call("addEventListener", "mouseenter",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				btn.Get("style").Set("background", colSurface1)
				return nil
			}))
		btn.Call("addEventListener", "mouseleave",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				btn.Get("style").Set("background", "transparent")
				return nil
			}))
		btn.Call("addEventListener", "mousedown",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				// Prevent the title-bar drag from kicking in when the
				// maker grabs the action button itself.
				args[0].Call("stopPropagation")
				return nil
			}))
		btn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				args[0].Call("stopPropagation")
				if a.OnClick != nil {
					a.OnClick()
				}
				return nil
			}))
		btnGroup.Call("appendChild", btn)
	}

	// ── Maximize button (□ square icon) ──────────────────────────────────
	// Toggles between normal windowed mode and fullscreen.
	maximizeBtn := doc.Call("createElement", "button")
	maximizeBtn.Get("style").Set("cssText", fmt.Sprintf(
		"width:14px;height:14px;border-radius:50%%;border:none;"+
			"background:%s;cursor:pointer;padding:0;"+
			"transition:opacity 0.15s;opacity:0.8;",
		colGreen))
	var isMaximized bool
	var savedCSS string

	maximizeBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			maximizeBtn.Get("style").Set("opacity", "1")
			return nil
		}))
	maximizeBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			maximizeBtn.Get("style").Set("opacity", "0.8")
			return nil
		}))
	maximizeBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("stopPropagation")
			if !isMaximized {
				// Save current panel CSS and maximize.
				savedCSS = panel.Get("style").Get("cssText").String()
				panel.Get("style").Set("cssText",
					fmt.Sprintf(
						"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
							"background:%s;border:none;border-radius:0;"+
							"display:flex;flex-direction:column;overflow:hidden;"+
							"box-shadow:none;font-family:sans-serif;z-index:100000;"+
							"transform:none;",
						colBase))
				bar.Get("style").Set("cursor", "default")
				isMaximized = true
			} else {
				// Restore saved CSS.
				panel.Get("style").Set("cssText", savedCSS)
				bar.Get("style").Set("cursor", "move")
				isMaximized = false
			}
			return nil
		}))
	btnGroup.Call("appendChild", maximizeBtn)

	// ── Close button (● red circle) ──────────────────────────────────────
	closeBtn := doc.Call("createElement", "button")
	closeBtn.Get("style").Set("cssText", fmt.Sprintf(
		"width:14px;height:14px;border-radius:50%%;border:none;"+
			"background:%s;cursor:pointer;flex-shrink:0;padding:0;"+
			"transition:opacity 0.15s;opacity:0.8;", colRed))
	closeBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeBtn.Get("style").Set("opacity", "1")
			return nil
		}))
	closeBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeBtn.Get("style").Set("opacity", "0.8")
			return nil
		}))
	closeBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeFn()
			return nil
		}))
	btnGroup.Call("appendChild", closeBtn)

	bar.Call("appendChild", btnGroup)
	return bar
}

// =====================================================================
//  Tab system
// =====================================================================

func buildTabs(doc js.Value, cfg Config, closeFn func()) (js.Value, js.Value) {
	tabBar := doc.Call("createElement", "div")
	tabBar.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;background:%s;border-bottom:1px solid %s;"+
			"min-height:28px;gap:0;padding:0 4px;align-items:flex-end;"+
			"flex-shrink:0;",
		colBase, colSurface1))

	contentArea := doc.Call("createElement", "div")
	contentArea.Get("style").Set("cssText",
		"flex:1;overflow:auto;position:relative;min-height:0;")

	// Create content panels (one per tab, only one visible at a time)
	panels := make([]js.Value, len(cfg.Tabs))
	buttons := make([]js.Value, len(cfg.Tabs))

	for i, tab := range cfg.Tabs {
		// Content panel — one per tab, only the active one is display:block.
		// Monaco needs explicit height to size the editor; other tab types
		// use auto height and let the contentArea scroll naturally.
		p := doc.Call("createElement", "div")
		panelCSS := "display:none;width:100%;"
		if tab.Type == TabMonaco || tab.Type == TabHelpDeck {
			panelCSS = "display:none;width:100%;height:100%;"
		}
		p.Get("style").Set("cssText", panelCSS)
		panels[i] = p

		// Render content based on tab type
		switch tab.Type {
		case TabForm:
			renderForm(doc, p, tab, cfg.OnSave, cfg.ValidateBeforeSave, closeFn)
		case TabMarkdown:
			renderMarkdown(doc, p, tab)
		case TabMonaco:
			renderMonaco(doc, p, tab)
		case TabHTML:
			renderHTML(doc, p, tab)
		case TabHelpDeck:
			// When EmbeddedFields are present, inject the (already wrapped)
			// OnSave callback so the embedded form's Apply button triggers
			// the same save + close + reopen cycle as the regular form.
			if len(tab.EmbeddedFields) > 0 {
				tab.EmbeddedOnSave = cfg.OnSave
			}
			renderHelpDeck(doc, p, tab)
		}

		contentArea.Call("appendChild", p)

		// Tab button
		idx := i // capture
		btn := doc.Call("createElement", "button")
		btn.Get("style").Set("cssText", tabButtonStyle(false))
		btn.Set("textContent", tab.Label)
		buttons[i] = btn

		btn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				// Switch: hide all, show selected
				for j := range panels {
					panels[j].Get("style").Set("display", "none")
					buttons[j].Get("style").Set("cssText", tabButtonStyle(false))
				}
				panels[idx].Get("style").Set("display", "block")
				buttons[idx].Get("style").Set("cssText", tabButtonStyle(true))
				return nil
			}))

		tabBar.Call("appendChild", btn)
	}

	// Show first tab by default
	if len(panels) > 0 {
		panels[0].Get("style").Set("display", "block")
		buttons[0].Get("style").Set("cssText", tabButtonStyle(true))
	}

	return tabBar, contentArea
}

func tabButtonStyle(active bool) string {
	if active {
		return fmt.Sprintf(
			"background:%s;color:%s;border:1px solid %s;border-bottom:none;"+
				"border-radius:4px 4px 0 0;padding:10px 18px;"+
				"font-size:13px;font-weight:600;cursor:pointer;"+
				"margin-bottom:-1px;position:relative;z-index:1;"+
				"font-family:sans-serif;min-height:42px;",
			colBase, colPeach, colSurface1)
	}
	return fmt.Sprintf(
		"background:transparent;color:%s;border:none;"+
			"border-radius:4px 4px 0 0;padding:10px 18px;"+
			"font-size:13px;font-weight:400;cursor:pointer;"+
			"font-family:sans-serif;transition:color 0.15s;min-height:42px;",
		colSubtext)
}

// renderHTML injects the tab's Content string as trusted innerHTML
// into the panel. The content is expected to come from Go code (e.g.
// ShowDiagnostics building a pre-rendered list); it is NOT sanitized
// here — callers that embed user-provided strings must escape them
// before passing in.
//
// A scrollable wrapper is applied so long diagnostic lists stay within
// the overlay's max-height without pushing the chrome.
//
// Português: Injeta o Content como innerHTML confiável no painel.
// Conteúdo vem do Go; não é sanitizado. Wrapper com scroll interno
// limita altura dentro do overlay.
func renderHTML(doc js.Value, container js.Value, tab Tab) {
	wrapper := doc.Call("createElement", "div")
	wrapper.Get("style").Set("cssText",
		"overflow-y:auto;max-height:70vh;width:100%;box-sizing:border-box;")
	wrapper.Set("innerHTML", tab.Content)
	container.Call("appendChild", wrapper)
}

// =====================================================================
//  Form renderer
// =====================================================================

func renderForm(doc js.Value, container js.Value, tab Tab, onSave func(map[string]string), validate func(map[string]string) bool, closeFn func()) {
	form := doc.Call("createElement", "div")
	form.Get("style").Set("cssText",
		"padding:16px;display:flex;flex-direction:column;gap:12px;")

	// Map of field key → input element (for reading values on Save)
	inputs := make(map[string]js.Value, len(tab.Fields))

	// Save button element (created early so doSave can reference it for feedback)
	var applyBtn js.Value

	// doSave collects all field values and calls onSave.
	doSave := func() {
		if onSave == nil {
			return
		}
		values := make(map[string]string, len(inputs))
		for key, el := range inputs {
			if el.Get("type").String() == "checkbox" {
				if el.Get("checked").Bool() {
					values[key] = "true"
				} else {
					values[key] = "false"
				}
			} else {
				values[key] = el.Get("value").String()
			}
		}
		// proceed performs the actual save plus the apply-button feedback.
		// Factored out so the optional ValidateBeforeSave gate can defer it to
		// a goroutine without duplicating the body.
		proceed := func() {
			onSave(values)

			// Visual feedback on apply button
			if applyBtn.Truthy() {
				applyBtn.Set("textContent", "Applied!")
				applyBtn.Get("style").Set("background", colGreen)
				js.Global().Call("setTimeout",
					js.FuncOf(func(this js.Value, args []js.Value) interface{} {
						applyBtn.Set("textContent", "Apply")
						applyBtn.Get("style").Set("background", colBlue)
						return nil
					}), 1000)
			}
		}

		// ValidateBeforeSave gate. It may block (an async round-trip such as a
		// codegen validation pass), so run it in a goroutine and proceed only
		// on a true return. A false return means the gate already surfaced why
		// it blocked; we leave the overlay open and do nothing further. The
		// goroutine keeps the JS event loop free so the gate's own fetch/SSE
		// callbacks can run (a synchronous block here would deadlock them).
		if validate != nil {
			go func() {
				if validate(values) {
					proceed()
				}
			}()
			return
		}
		proceed()
	}

	// Enter key handler for inputs (not textarea — Enter adds newline there)
	enterKeyFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		e := args[0]
		if e.Get("key").String() == "Enter" {
			e.Call("preventDefault")
			doSave()
		}
		return nil
	})

	for _, field := range tab.Fields {
		row := doc.Call("createElement", "div")
		// align-items:flex-start (instead of center) so multi-line
		// fields like FieldMap and FieldSlice keep the label pinned
		// to the top of the row instead of centring it vertically
		// against the column of inputs. The label's padding-top
		// below compensates for the input's own top padding so
		// single-line fields still look aligned (top-of-label vs
		// top-of-input). Checkbox fields end up ~5px lower than
		// the label baseline; acceptable trade-off — checkbox
		// rows are visually distinctive enough that the small
		// offset reads as intentional spacing rather than mis-
		// alignment.
		row.Get("style").Set("cssText",
			"display:flex;align-items:flex-start;gap:10px;")

		// Label
		label := doc.Call("createElement", "label")
		// padding-top:5px matches the input's padding-top so the
		// top edge of the label text aligns with the top edge of
		// the input's text. For multi-line fields the label sits
		// at the top of the column, matching the first row.
		label.Get("style").Set("cssText", fmt.Sprintf(
			"color:%s;font-size:12px;font-weight:500;min-width:90px;"+
				"text-align:right;flex-shrink:0;padding-top:5px;", colText))
		label.Set("textContent", field.Label)
		row.Call("appendChild", label)

		// Input
		var input js.Value
		switch field.Type {
		case FieldSelect:
			input = doc.Call("createElement", "select")
			input.Get("style").Set("cssText", selectStyle())
			for _, opt := range field.Options {
				option := doc.Call("createElement", "option")
				option.Set("value", opt.Value)
				option.Set("textContent", opt.Label)
				if opt.Value == field.Value {
					option.Set("selected", true)
				}
				input.Call("appendChild", option)
			}
			// Enter on select → save
			input.Call("addEventListener", "keydown", enterKeyFn)
		case FieldCheckbox:
			input = doc.Call("createElement", "input")
			input.Set("type", "checkbox")
			input.Get("style").Set("cssText",
				"width:16px;height:16px;accent-color:"+colBlue+";cursor:pointer;")
			if field.Value == "true" {
				input.Set("checked", true)
			}
			// Enter on checkbox → save
			input.Call("addEventListener", "keydown", enterKeyFn)
		case FieldTextarea:
			// Textarea: Enter adds newline, Ctrl+Enter saves
			input = doc.Call("createElement", "textarea")
			rows := field.Rows
			if rows == 0 {
				rows = 4
			}
			input.Set("rows", rows)
			input.Get("style").Set("cssText", inputStyle()+"resize:vertical;")
			input.Set("value", field.Value)
			if field.Placeholder != "" {
				input.Set("placeholder", field.Placeholder)
			}
			input.Call("addEventListener", "keydown",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					e := args[0]
					if e.Get("key").String() == "Enter" && e.Get("ctrlKey").Bool() {
						e.Call("preventDefault")
						doSave()
					}
					return nil
				}))
		case FieldColor:
			input = doc.Call("createElement", "input")
			input.Set("type", "color")
			input.Get("style").Set("cssText",
				"width:40px;height:28px;border:none;cursor:pointer;"+
					"background:transparent;padding:0;")
			input.Set("value", field.Value)
		case FieldFile:
			fileContainer, hiddenInput := buildFileField(doc, field)
			row.Call("appendChild", fileContainer)
			input = hiddenInput
		case FieldMap:
			// FieldMap renders a row builder for map[K]V props. The
			// hidden input is collected by the existing doSave loop
			// like any other text field — the JSON it carries is
			// the up-to-date serialised map, kept in sync on every
			// keystroke and row add/remove inside buildMapField.
			mapContainer, hiddenInput := buildMapField(doc, field)
			row.Call("appendChild", mapContainer)
			input = hiddenInput
		case FieldSlice:
			// FieldSlice renders a row builder for []T props. Same
			// hidden-input pattern as FieldMap — the JSON array is
			// kept in sync with the rows on every edit / add /
			// remove / move (↑↓).
			sliceContainer, hiddenInput := buildSliceField(doc, field)
			row.Call("appendChild", sliceContainer)
			input = hiddenInput
		case FieldCaseEditor:
			// FieldCaseEditor renders the StatementCase cases editor. Same
			// hidden-input pattern as FieldSlice/FieldMap — the JSON array of
			// cases is kept in sync on every edit and collected by doSave
			// under field.Key.
			caseContainer, hiddenInput := buildCaseEditorField(doc, field)
			row.Call("appendChild", caseContainer)
			input = hiddenInput
		default: // text, number
			input = doc.Call("createElement", "input")
			input.Set("type", string(field.Type))
			input.Get("style").Set("cssText", inputStyle())
			input.Set("value", field.Value)
			if field.Placeholder != "" {
				input.Set("placeholder", field.Placeholder)
			}
			if field.Min != "" {
				input.Set("min", field.Min)
			}
			if field.Max != "" {
				input.Set("max", field.Max)
			}
			// Block the minus key for non-negative number fields. The HTML min
			// attribute only validates on submit/spinner, not on free typing,
			// so a negative could otherwise be typed in. Gated on a
			// non-negative min (min set and not starting with "-") so number
			// fields that legitimately allow negatives are unaffected. Paste /
			// spinner edge cases are clamped by the field's consumer (OnSave),
			// which is the source of truth.
			if field.Type == FieldNumber && field.Min != "" && !strings.HasPrefix(field.Min, "-") {
				input.Call("addEventListener", "keydown", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					if len(args) > 0 && args[0].Get("key").String() == "-" {
						args[0].Call("preventDefault")
					}
					return nil
				}))
			}
			applyIdentifierFilter(input, field)
			// Enter on text/number → save
			input.Call("addEventListener", "keydown", enterKeyFn)
		}

		inputs[field.Key] = input

		// ReadOnly: disable editing and dim the input
		if field.ReadOnly {
			input.Set("readOnly", true)
			input.Set("disabled", true)
			input.Get("style").Set("opacity", "0.6")
			input.Get("style").Set("cursor", "not-allowed")
		}

		// ConnectionColor: accent the border to match the highlighted element
		// on the interactive diagram SVG. Only the border and a faint glow are
		// changed — text colour stays at the standard colText value.
		//
		// ConnectionRole: stored as a data attribute so fetchAndInjectSVG can
		// reactively update the border colour after the SVG palette is parsed.
		if field.ConnectionColor != "" {
			input.Get("style").Set("borderColor", field.ConnectionColor)
			input.Get("style").Set("boxShadow", "0 0 0 2px "+field.ConnectionColor+"44")
		}
		if field.ConnectionRole != "" {
			input.Call("setAttribute", "data-connection-role", field.ConnectionRole)
		}

		row.Call("appendChild", input)
		form.Call("appendChild", row)
	}

	// ── Action buttons: Apply + Close (Windows-style, right-aligned) ─────
	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText",
		"display:flex;justify-content:flex-end;gap:8px;padding-top:8px;"+
			"border-top:1px solid "+colSurface1+";margin-top:4px;")

	// Apply button — saves values without closing (or triggers OnSaveReopen)
	if onSave != nil {
		applyBtn = doc.Call("createElement", "button")
		applyBtn.Get("style").Set("cssText", fmt.Sprintf(
			"background:%s;color:%s;border:none;border-radius:4px;"+
				"padding:6px 20px;cursor:pointer;font-size:12px;font-weight:600;"+
				"transition:opacity 0.15s;", colBlue, colBase))
		applyBtn.Set("textContent", "Apply")

		applyBtn.Call("addEventListener", "mouseenter",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				applyBtn.Get("style").Set("opacity", "0.85")
				return nil
			}))
		applyBtn.Call("addEventListener", "mouseleave",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				applyBtn.Get("style").Set("opacity", "1")
				return nil
			}))
		applyBtn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				doSave()
				return nil
			}))
		btnRow.Call("appendChild", applyBtn)
	}

	// Close button — closes without saving
	closeFormBtn := doc.Call("createElement", "button")
	closeFormBtn.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;color:%s;border:1px solid %s;border-radius:4px;"+
			"padding:6px 20px;cursor:pointer;font-size:12px;font-weight:600;"+
			"transition:opacity 0.15s;", colSurface0, colText, colSurface1))
	closeFormBtn.Set("textContent", "Close")

	closeFormBtn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeFormBtn.Get("style").Set("opacity", "0.85")
			return nil
		}))
	closeFormBtn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeFormBtn.Get("style").Set("opacity", "1")
			return nil
		}))
	closeFormBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if closeFn != nil {
				closeFn()
			}
			return nil
		}))
	btnRow.Call("appendChild", closeFormBtn)

	form.Call("appendChild", btnRow)

	container.Call("appendChild", form)
}

func inputStyle() string {
	return fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:5px 8px;font-size:13px;"+
			"font-family:monospace;outline:none;"+
			"transition:border-color 0.15s;",
		colMantle, colText, colSurface1)
}

func selectStyle() string {
	return fmt.Sprintf(
		"flex:1;background:%s;color:%s;border:1px solid %s;"+
			"border-radius:4px;padding:5px 8px;font-size:13px;"+
			"font-family:sans-serif;outline:none;cursor:pointer;"+
			"appearance:auto;",
		colMantle, colText, colSurface1)
}

// =====================================================================
//  Markdown renderer
// =====================================================================

// =====================================================================
//  Interactive diagram renderer
// =====================================================================

// diagramInspectorCSS is injected as a <style> element directly inside the
// SVG so the inspector-mode classes work in the inline SVG context.
// Class names follow the IoTMaker Interactive Diagram Specification:
//   - .conn-group  — each connectable element group
//   - .conn-role-bg — role badge background (hidden by default)
//   - .conn-role    — role badge text (hidden by default)
//   - .conn-num     — element number text
//   - .readme-badges — full badge list visible in readme mode
//
// See docs/INTERACTIVE_DIAGRAM_SPEC.md for the full SVG contract.
const diagramInspectorCSS = `
.active .pad        { fill: var(--rc) !important; stroke: var(--rc) !important; }
.active .conn-role-bg   { fill: var(--rc) !important; visibility: visible !important; }
.active .conn-role      { visibility: visible !important; }
.active .readme-badges { display: none !important; }
.dimmed .pad           { opacity: 0.18 !important; }
.dimmed .readme-badges { opacity: 0.18 !important; }
.dimmed .conn-num       { opacity: 0.25 !important; }
`

// ConnectionRoleFallbackColor returns a neutral colour used as a fallback when
// the SVG palette is unavailable or the role is not found in it.
//
// The primary colour source is always the SVG's data-palette attribute.
// This fallback provides a visible but non-committal accent so the user
// can still see that a connection: tag is present on the prop.
//
// Português: Cor neutra de fallback quando a paleta do SVG não está disponível.
func ConnectionRoleFallbackColor() string {
	return "#6b7280" // neutral slate
}

// ConnectionRoleLabel converts a role identifier to a human-readable badge
// label by replacing underscores with spaces.
//
// Examples: "I2C_SDA" → "I2C SDA", "GPIO_INT" → "GPIO INT", "DATABASE" → "DATABASE"
func ConnectionRoleLabel(role string) string {
	return strings.ReplaceAll(role, "_", " ")
}

// parsePalette extracts the role→colour mapping from the data-palette attribute
// on an inline SVG element.
//
// Format: "ROLE1:#hex1, ROLE2:#hex2, ROLE3:#hex3"
//
// Rules:
//   - Comma-separated pairs of ROLE:#colour
//   - Whitespace around commas and colons is trimmed
//   - Role keys are uppercased for case-insensitive matching
//   - Returns an empty map when the attribute is missing or empty
//
// Português: Extrai o mapa role→cor do atributo data-palette de um elemento SVG.
func parsePalette(svgEl js.Value) map[string]string {
	raw := svgEl.Call("getAttribute", "data-palette")
	if raw.IsNull() || raw.IsUndefined() {
		return nil
	}
	rawStr := strings.TrimSpace(raw.String())
	if rawStr == "" {
		return nil
	}

	palette := make(map[string]string)
	pairs := strings.Split(rawStr, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		// Use last colon to split — colour values start with '#' so no ambiguity.
		colonIdx := strings.LastIndex(pair, ":")
		if colonIdx < 0 {
			continue
		}
		role := strings.TrimSpace(pair[:colonIdx])
		colour := strings.TrimSpace(pair[colonIdx+1:])
		if role == "" || colour == "" {
			continue
		}
		palette[strings.ToUpper(role)] = colour
	}
	return palette
}

// paletteColor looks up a role in the parsed palette (case-insensitive).
// Returns the fallback colour when the role is not found.
func paletteColor(palette map[string]string, role string) string {
	if palette != nil {
		if c, ok := palette[strings.ToUpper(role)]; ok {
			return c
		}
	}
	return ConnectionRoleFallbackColor()
}

// updateFormInputsFromPalette scans the document for form inputs that have a
// data-connection-role attribute and updates their border colour from the
// SVG palette.
//
// This is called after an interactive SVG is loaded and its data-palette is
// parsed. It creates a smooth visual transition: the inputs initially show a
// neutral slate border (from ConnectionRoleFallbackColor), then glow with the
// real palette colour once the SVG loads.
//
// The query targets the entire document because the Properties tab (where the
// inputs live) and the Help tab (where the SVG lives) are siblings in the
// same overlay panel.
//
// Português: Atualiza reativamente as bordas dos inputs do formulário com as
// cores da paleta do SVG após o diagrama ser carregado.
func updateFormInputsFromPalette(doc js.Value, palette map[string]string) {
	inputs := doc.Call("querySelectorAll", "[data-connection-role]")
	n := inputs.Get("length").Int()
	for i := 0; i < n; i++ {
		input := inputs.Index(i)
		role := input.Call("getAttribute", "data-connection-role")
		if role.IsNull() || role.IsUndefined() {
			continue
		}
		colour := paletteColor(palette, role.String())
		if colour == ConnectionRoleFallbackColor() {
			// Palette didn't have this role — keep the existing fallback.
			continue
		}
		input.Get("style").Set("borderColor", colour)
		input.Get("style").Set("boxShadow", "0 0 0 2px "+colour+"44")
	}
}

// activateInlineSVGs scans the given container for <img> elements whose src
// matches the interactive SVG URL (diagramURL). Each match is replaced with
// an inline <svg> that can be manipulated via CSS classes to highlight elements.
//
// When diagramProps is non-empty (inspector mode):
//   - Each element listed in diagramProps gets the "active" class and its role
//     badge + colour applied via CSS custom property --rc.
//   - All remaining elements get the "dimmed" class (reduced opacity).
//
// When diagramProps is empty (readme mode):
//   - The SVG renders as-is — all elements visible with their default function
//     badges. This gives the maker a full overview before any configuration.
//
// This function is called by renderMarkdown after marked.js sets innerHTML.
// It bridges the specialist's ![](rp2040.svg) markdown image reference and
// the IoTMaker Interactive Diagram convention (data-id, .conn-group, .active,
// .dimmed). See docs/INTERACTIVE_DIAGRAM_SPEC.md.
//
// Português: Pós-processa o HTML renderizado pelo marked.js. Substitui <img>
// de SVGs interativos por <svg> inline com destaque dos elementos selecionados.
func activateInlineSVGs(doc js.Value, container js.Value, diagramURL string, props []DiagramProp) {
	if diagramURL == "" {
		return
	}

	// Extract the bare filename from the full URL for flexible matching.
	// "/static/devices/owner/repo/rp2040.svg" → "rp2040.svg"
	svgFilename := diagramURL
	if idx := strings.LastIndex(diagramURL, "/"); idx >= 0 {
		svgFilename = diagramURL[idx+1:]
	}

	// Find all <img> elements in the rendered markdown.
	imgs := container.Call("querySelectorAll", "img")
	n := imgs.Get("length").Int()
	if n == 0 {
		return
	}

	for i := 0; i < n; i++ {
		img := imgs.Call("item", i)
		src := img.Call("getAttribute", "src")
		if src.IsNull() || src.IsUndefined() {
			continue
		}
		srcStr := src.String()

		// Match: exact URL, or src ends with the SVG filename.
		// The worker rewrites ![](rp2040.svg) → ![](/static/devices/.../rp2040.svg)
		// so the exact match covers the normal case. The suffix match handles
		// edge cases (manual pages with bare filenames, different base paths).
		if srcStr != diagramURL && !strings.HasSuffix(srcStr, "/"+svgFilename) && srcStr != svgFilename {
			continue
		}

		// Replace this <img> with an inline SVG fetched from the server.
		// Capture loop variables for the async callback.
		capturedImg := img
		capturedProps := props

		// Create a placeholder wrapper that will receive the inline SVG.
		wrapper := doc.Call("createElement", "div")
		wrapper.Get("style").Set("cssText",
			"display:flex;flex-direction:column;align-items:center;"+
				"padding:8px 0;")

		// Loading placeholder.
		placeholder := doc.Call("createElement", "div")
		placeholder.Get("style").Set("cssText", fmt.Sprintf(
			"color:%s;font-size:12px;padding:12px;font-family:sans-serif;", colSubtext))
		placeholder.Set("textContent", "Loading diagram…")
		wrapper.Call("appendChild", placeholder)

		// Insert wrapper before the <img>, then remove the <img>.
		parent := capturedImg.Get("parentNode")
		if parent.IsNull() || parent.IsUndefined() {
			continue
		}
		parent.Call("insertBefore", wrapper, capturedImg)
		parent.Call("removeChild", capturedImg)

		// Fetch SVG and inject inline.
		fetchAndInjectSVG(doc, wrapper, placeholder, diagramURL, capturedProps)
	}
}

// fetchAndInjectSVG fetches an SVG file, injects it inline into the wrapper,
// and applies element activation/dimming based on the provided props.
//
// Colour resolution: when the SVG is loaded, its data-palette attribute is
// parsed to build a role→colour map. Each DiagramProp's colour is resolved
// from this palette using the Role as the key. If the palette is missing or
// the role is not found, ConnectionRoleFallbackColor() is used.
//
// After activation, addImageLightbox is called to make the SVG clickable
// for fullscreen viewing.
func fetchAndInjectSVG(doc js.Value, wrapper js.Value, placeholder js.Value, url string, props []DiagramProp) {
	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			status := resp.Get("status").Int()
			placeholder.Set("textContent",
				fmt.Sprintf("Diagram not available (HTTP %d).", status))
			return js.Null()
		}
		return resp.Call("text")
	})

	thenText := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			return nil
		}
		svgText := args[0].String()

		// Remove loading placeholder.
		if !placeholder.IsNull() && !placeholder.IsUndefined() {
			wrapper.Call("removeChild", placeholder)
		}

		// Inject SVG inline so CSS classes can reach its internal elements.
		svgWrap := doc.Call("createElement", "div")
		svgWrap.Set("innerHTML", svgText)
		wrapper.Call("appendChild", svgWrap)

		svgEl := svgWrap.Call("querySelector", "svg")
		if svgEl.IsNull() || svgEl.IsUndefined() {
			log.Printf("[Overlay] activateInlineSVGs: no <svg> found in %s", url)
			return nil
		}

		// Size the SVG to fit within the markdown content area.
		svgEl.Get("style").Set("maxHeight", "55vh")
		svgEl.Get("style").Set("width", "100%")
		svgEl.Get("style").Set("display", "block")

		// Inject inspector-mode CSS directly inside the SVG so the
		// .active / .dimmed classes work even in an inline context.
		styleEl := doc.Call("createElement", "style")
		styleEl.Set("textContent", diagramInspectorCSS)
		firstChild := svgEl.Get("firstChild")
		if !firstChild.IsNull() && !firstChild.IsUndefined() {
			svgEl.Call("insertBefore", styleEl, firstChild)
		} else {
			svgEl.Call("appendChild", styleEl)
		}

		// Parse the colour palette from the SVG's data-palette attribute.
		// This is the primary source of truth for highlight colours.
		palette := parsePalette(svgEl)

		// Resolve the active set of (data-id → role) pairs to highlight.
		//
		// Two sources, tried in order. We commit to whichever first
		// returns a non-empty list; we don't merge them. The form
		// inputs are the ground truth in the running session — they
		// reflect every Apply the user has performed in this overlay.
		//
		// 1. The `props` argument. The classical path: built upstream
		//    in statementBlackBoxInit.go from e.def.Props +
		//    e.propValues, threaded through Tab.DiagramProps and
		//    forwarded by renderHelpDeck → renderMarkdown.
		//
		// 2. DOM fallback. Scan the document for inputs carrying a
		//    data-connection-role attribute (planted by renderForm
		//    when a prop has a connection: tag). Read the input's
		//    .value as the data-id, the data-connection-role attribute
		//    as the Role. This bypasses every link in the upstream
		//    chain — if the inputs are visible to the user, the
		//    diagram can highlight from them.
		//
		// The fallback exists because the upstream chain has many
		// links that can each silently drop the payload (parser tag
		// extraction, JSON serialisation through clientPropDef, the
		// propValues map keyed by FieldName, the Tab struct copy,
		// the renderHelpDeck → renderMarkdown forward). Anchoring on
		// the DOM, which the user can see directly, makes the
		// highlight robust to any of those breaking.
		//
		// Português: Resolve o conjunto de pinos a destacar. Tenta
		// primeiro o caminho clássico (props vindo dos campos do
		// device). Se vier vazio, varre o DOM procurando inputs com
		// data-connection-role — eles são plantados pelo renderForm
		// para todo prop com tag connection: e carregam o valor
		// atual do form, bypassando toda a cadeia upstream.
		type activeProp struct {
			id    string
			role  string
			label string
		}
		var active []activeProp

		if len(props) > 0 {
			active = make([]activeProp, 0, len(props))
			for _, dp := range props {
				active = append(active, activeProp{
					id:    dp.ID,
					role:  dp.Role,
					label: dp.Label,
				})
			}
		} else {
			// DOM fallback. The query is rooted at `doc` (the document)
			// rather than at `svgWrap` because the form lives in a
			// sibling tab, not inside the markdown panel that holds
			// the SVG. This is the same scope used by
			// updateFormInputsFromPalette below.
			inputs := doc.Call("querySelectorAll", "[data-connection-role]")
			n := inputs.Get("length").Int()
			active = make([]activeProp, 0, n)
			for i := 0; i < n; i++ {
				input := inputs.Index(i)
				role := input.Call("getAttribute", "data-connection-role").String()
				value := input.Get("value").String()
				if role == "" || value == "" {
					continue
				}
				active = append(active, activeProp{
					id:    value,
					role:  role,
					label: ConnectionRoleLabel(role),
				})
			}
		}

		// When there are active elements, switch to inspector mode:
		// highlight selected elements, dim all others.
		// When there are no active elements, leave the SVG in readme
		// mode: all elements visible with their default function badges.
		if len(active) > 0 {
			// Activate each configured element.
			//
			// Lookup strategy:
			//   1. Try the exact data-id first (case-sensitive — fastest path).
			//   2. If that fails, fall back to a case-insensitive walk over
			//      every .conn-group, comparing data-id with EqualFold.
			//
			// Why the fallback exists. CSS attribute selectors are
			// case-sensitive, but the prop value comes from the Go
			// default tag, the form input, or the user's last selection.
			// A struct that declared `default:"gp4"` (lower case) would
			// silently fail against an SVG that defines `data-id="GP4"`
			// — every element stays in readme mode because the for-loop
			// here never finds a match, so .active is never added, and
			// the :not(.active) dim sweep finds nothing to dim. We log
			// when the fallback engages so the specialist can fix the
			// casing at the source.
			for _, ap := range active {
				g := svgEl.Call("querySelector",
					fmt.Sprintf(`[data-id="%s"]`, ap.id))

				if g.IsNull() || g.IsUndefined() {
					// Fallback: case-insensitive scan of every conn-group.
					target := strings.ToUpper(ap.id)
					all := svgEl.Call("querySelectorAll", ".conn-group[data-id]")
					n := all.Get("length").Int()
					for j := 0; j < n; j++ {
						cand := all.Index(j)
						candID := cand.Call("getAttribute", "data-id").String()
						if strings.EqualFold(candID, target) {
							g = cand
							log.Printf(
								"[Overlay] data-id case mismatch: prop value %q "+
									"matched SVG element %q via case-insensitive "+
									"fallback. Align the casing in the Go struct "+
									"(prop default/options) or in the SVG to silence "+
									"this warning.",
								ap.id, candID)
							break
						}
					}
				}

				if g.IsNull() || g.IsUndefined() {
					all := svgEl.Call("querySelectorAll", ".conn-group[data-id]")
					n := all.Get("length").Int()
					ids := make([]string, 0, n)
					for j := 0; j < n; j++ {
						ids = append(ids,
							all.Index(j).Call("getAttribute", "data-id").String())
					}
					log.Printf(
						"[Overlay] no SVG element found for prop value %q "+
							"(role %q). Available data-id values in the diagram: %v",
						ap.id, ap.role, ids)
					continue
				}

				// Resolve colour from palette (primary) or fallback.
				colour := paletteColor(palette, ap.role)

				g.Get("style").Call("setProperty", "--rc", colour)
				tr := g.Call("querySelector", ".conn-role")
				if !tr.IsNull() && !tr.IsUndefined() {
					tr.Set("textContent", ap.label)
				}
				g.Get("classList").Call("add", "active")
			}

			// Dim all elements that were not activated.
			allGroups := svgEl.Call("querySelectorAll", ".conn-group:not(.active)")
			dimN := allGroups.Get("length").Int()
			for j := 0; j < dimN; j++ {
				allGroups.Index(j).Get("classList").Call("add", "dimmed")
			}
		}

		// Reactively update form input borders with the actual palette colours.
		//
		// When the overlay first renders the Properties form, inputs with a
		// connection: tag get a neutral fallback border (#6b7280). Now that the
		// SVG palette is available, we can apply the real colours. This creates
		// a smooth visual transition: neutral grey → vibrant role colour.
		//
		// We query the entire document for [data-connection-role] inputs
		// because the Properties tab and the Help tab (where the SVG lives)
		// are siblings in the same overlay panel.
		if palette != nil {
			updateFormInputsFromPalette(doc, palette)
		}

		// Make the SVG clickable for fullscreen lightbox viewing.
		addImageLightbox(doc, svgWrap)

		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		errMsg := "network error"
		if len(args) > 0 && args[0].Get("message").Truthy() {
			errMsg = args[0].Get("message").String()
		}
		placeholder.Set("textContent",
			fmt.Sprintf("Failed to load diagram: %s", errMsg))
		log.Printf("[Overlay] fetchAndInjectSVG error (%s): %s", url, errMsg)
		return nil
	})

	js.Global().Call("fetch", url).
		Call("then", thenResponse).
		Call("then", thenText).
		Call("catch", catchFn)
}

// addImageLightbox makes all images and inline SVGs inside the given container
// clickable. Clicking opens a fullscreen lightbox overlay that shows the image
// centred on a dark semi-transparent backdrop.
//
// For inline SVGs, the lightbox clones the SVG element (preserving inspector-
// mode state: active/dimmed classes) and displays it at full viewport size.
// For regular <img> tags, the src is used to create a full-size image.
//
// Close: click the backdrop, click the × button, or press Escape.
//
// Português: Adiciona lente de aumento para imagens e SVGs inline no markdown.
// Clicar abre um lightbox fullscreen com a imagem centralizada.
func addImageLightbox(doc js.Value, container js.Value) {
	// Find all images and inline SVGs in the container.
	imgs := container.Call("querySelectorAll", "img, svg")
	n := imgs.Get("length").Int()
	for i := 0; i < n; i++ {
		el := imgs.Index(i)
		tagName := strings.ToLower(el.Get("tagName").String())

		// Skip tiny decorative elements (icons, badges).
		if tagName == "svg" {
			vb := el.Call("getAttribute", "viewBox")
			if vb.IsNull() || vb.IsUndefined() || vb.String() == "" {
				continue
			}
		}

		el.Get("style").Set("cursor", "zoom-in")

		capturedEl := el
		capturedTag := tagName

		el.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("stopPropagation")
			showLightbox(doc, capturedEl, capturedTag)
			return nil
		}))
	}
}

// enhanceIoTMakerImages scans all <img> tags in the rendered markdown for
// PNG images containing the "IOTM" steganography marker. For each match,
// the image is wrapped in a container and a "Load Example" button overlay
// is added at the bottom.
//
// The check is asynchronous: each image is loaded onto a temporary canvas
// to read its pixels. Already-loaded images (cached) are processed immediately.
//
// Português: Varre todos os <img> no markdown renderizado procurando PNGs
// com o marcador "IOTM". Para cada match, adiciona um botão "Load Example".
func enhanceIoTMakerImages(doc js.Value, container js.Value) {
	if globalOnLoadExample == nil {
		return
	}

	imgs := container.Call("querySelectorAll", "img")
	n := imgs.Get("length").Int()
	for i := 0; i < n; i++ {
		img := imgs.Index(i)
		src := img.Get("src").String()

		// Only check PNG images (steganography is PNG-only).
		if !strings.HasSuffix(strings.ToLower(src), ".png") {
			// Also check data URLs and blob URLs.
			srcLower := strings.ToLower(src)
			if !strings.Contains(srcLower, "image/png") {
				continue
			}
		}

		capturedImg := img
		capturedSrc := src

		// Process each image in a goroutine — loading may be async.
		go func() {
			if !checkImageForMarker(doc, capturedImg) {
				return
			}

			// Image has the IOTM marker — add the "Load Example" overlay.
			addLoadExampleOverlay(doc, capturedImg, capturedSrc)
		}()
	}
}

// checkImageForMarker draws the given <img> element onto a temporary canvas
// and reads the first pixels to check for the "IOTM" steganography marker.
// Returns true if the marker is found.
//
// If the image is not yet loaded, waits for the "load" event.
func checkImageForMarker(doc js.Value, img js.Value) bool {
	// Wait for image to load if not already.
	if !img.Get("complete").Bool() || img.Get("naturalWidth").Int() == 0 {
		ch := make(chan bool, 1)
		var onLoad js.Func
		onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			onLoad.Release()
			ch <- true
			return nil
		})
		var onError js.Func
		onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			onError.Release()
			ch <- false
			return nil
		})
		img.Call("addEventListener", "load", onLoad)
		img.Call("addEventListener", "error", onError)
		if ok := <-ch; !ok {
			return false
		}
	}

	imgW := img.Get("naturalWidth").Int()
	imgH := img.Get("naturalHeight").Int()
	if imgW == 0 || imgH == 0 {
		return false
	}

	// Draw to a small temporary canvas — we only need the first ~11 pixels
	// to check the 4-byte marker, but we draw the full image because
	// canvas drawImage requires it. The canvas is discarded immediately.
	tempCanvas := doc.Call("createElement", "canvas")
	tempCanvas.Set("width", imgW)
	tempCanvas.Set("height", imgH)
	tempCtx := tempCanvas.Call("getContext", "2d")
	tempCtx.Call("drawImage", img, 0, 0)

	// Read a small strip of pixels — first row is enough for the marker check.
	// Reading fewer pixels is faster than reading the entire image.
	checkW := imgW
	if checkW > 100 {
		checkW = 100
	}
	imgData := tempCtx.Call("getImageData", 0, 0, checkW, 1)
	dataArray := imgData.Get("data")

	pixelLen := dataArray.Get("length").Int()
	pixels := make([]byte, pixelLen)
	js.CopyBytesToGo(pixels, dataArray)

	return steganography.HasMarker(pixels)
}

// addLoadExampleOverlay wraps an <img> in a positioned container and adds
// a semi-transparent "Load Example" button at the bottom.
func addLoadExampleOverlay(doc js.Value, img js.Value, src string) {
	// Wrap the image in a container for positioning.
	parent := img.Get("parentNode")
	wrapper := doc.Call("createElement", "div")
	wrapper.Get("style").Set("cssText",
		"position:relative;display:inline-block;margin:8px 0;")

	// Insert wrapper before img, then move img into wrapper.
	parent.Call("insertBefore", wrapper, img)
	wrapper.Call("appendChild", img)

	// Create the "Load Example" button overlay.
	btn := doc.Call("createElement", "button")
	btn.Get("style").Set("cssText", fmt.Sprintf(
		"position:absolute;bottom:8px;left:50%%;transform:translateX(-50%%);"+
			"background:rgba(30,30,46,0.80);color:#89b4fa;"+
			"backdrop-filter:blur(8px);-webkit-backdrop-filter:blur(8px);"+
			"border:1px solid rgba(137,180,250,0.3);border-radius:6px;"+
			"padding:8px 20px;font-size:13px;font-weight:600;"+
			"cursor:pointer;min-height:44px;"+
			"transition:background 0.2s,color 0.2s;"+
			"z-index:10;"))
	btn.Set("textContent", loadExampleLabel)

	// Hover effect.
	btn.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			btn.Get("style").Set("background", "rgba(137,180,250,0.9)")
			btn.Get("style").Set("color", "#1e1e2e")
			return nil
		}))
	btn.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			btn.Get("style").Set("background", "rgba(30,30,46,0.80)")
			btn.Get("style").Set("color", "#89b4fa")
			return nil
		}))

	// Click: extract full JSON and call the global callback.
	capturedSrc := src
	btn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("stopPropagation")
			if globalOnLoadExample == nil {
				return nil
			}

			go func() {
				// Close any open menu or panel before rebuilding the stage.
				if globalOnBeforeLoadExample != nil {
					globalOnBeforeLoadExample()
				}

				jsonBytes := extractJSONFromImageSrc(doc, capturedSrc)
				if jsonBytes == nil {
					log.Printf("[Overlay] Load Example: failed to extract JSON from %s", capturedSrc)
					return
				}
				log.Printf("[Overlay] Load Example: extracted %d bytes, loading...", len(jsonBytes))
				globalOnLoadExample(string(jsonBytes))
			}()
			return nil
		}))

	wrapper.Call("appendChild", btn)
}

// extractJSONFromImageSrc loads an image from a URL, draws it to a canvas,
// and extracts the embedded JSON via steganography. Returns nil on failure.
func extractJSONFromImageSrc(doc js.Value, src string) []byte {
	// Load image.
	img := doc.Call("createElement", "img")
	img.Set("crossOrigin", "anonymous")

	ch := make(chan bool, 1)
	var onLoad js.Func
	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onLoad.Release()
		ch <- true
		return nil
	})
	var onError js.Func
	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onError.Release()
		ch <- false
		return nil
	})
	img.Call("addEventListener", "load", onLoad)
	img.Call("addEventListener", "error", onError)
	img.Set("src", src)

	if ok := <-ch; !ok {
		return nil
	}

	imgW := img.Get("naturalWidth").Int()
	imgH := img.Get("naturalHeight").Int()

	tempCanvas := doc.Call("createElement", "canvas")
	tempCanvas.Set("width", imgW)
	tempCanvas.Set("height", imgH)
	tempCtx := tempCanvas.Call("getContext", "2d")
	tempCtx.Call("drawImage", img, 0, 0)

	imgData := tempCtx.Call("getImageData", 0, 0, imgW, imgH)
	dataArray := imgData.Get("data")

	pixelLen := dataArray.Get("length").Int()
	pixels := make([]byte, pixelLen)
	js.CopyBytesToGo(pixels, dataArray)

	jsonBytes, err := steganography.Extract(pixels)
	if err != nil {
		log.Printf("[Overlay] Extract from image: %v", err)
		return nil
	}
	return jsonBytes
}

// showLightbox displays the given element in a fullscreen overlay.
func showLightbox(doc js.Value, sourceEl js.Value, tagName string) {
	// Backdrop
	backdrop := doc.Call("createElement", "div")
	backdrop.Get("style").Set("cssText",
		"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
			"background:rgba(0,0,0,0.85);z-index:99999;"+
			"display:flex;align-items:center;justify-content:center;"+
			"cursor:zoom-out;")

	// Close button
	closeBtn := doc.Call("createElement", "button")
	closeBtn.Get("style").Set("cssText",
		"position:absolute;top:16px;right:16px;background:none;border:none;"+
			"color:#fff;font-size:28px;cursor:pointer;z-index:100000;"+
			"width:40px;height:40px;display:flex;align-items:center;"+
			"justify-content:center;border-radius:50%;"+
			"transition:background 0.15s;")
	closeBtn.Set("innerHTML", "&#215;") // ×
	closeBtn.Call("addEventListener", "mouseenter", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		closeBtn.Get("style").Set("background", "rgba(255,255,255,0.15)")
		return nil
	}))
	closeBtn.Call("addEventListener", "mouseleave", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		closeBtn.Get("style").Set("background", "none")
		return nil
	}))

	// Close function — shared by backdrop click, button click, and Escape key.
	var escapeFn js.Func
	closeLightbox := func() {
		if backdrop.Get("parentNode").Truthy() {
			doc.Get("body").Call("removeChild", backdrop)
		}
		doc.Call("removeEventListener", "keydown", escapeFn)
	}

	escapeFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("key").String() == "Escape" {
			closeLightbox()
		}
		return nil
	})

	backdrop.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		closeLightbox()
		return nil
	}))
	closeBtn.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		closeLightbox()
		return nil
	}))
	doc.Call("addEventListener", "keydown", escapeFn)

	backdrop.Call("appendChild", closeBtn)

	// Content: clone the source element for fullscreen display.
	if tagName == "svg" {
		// Clone the SVG with all classes intact (preserves active/dimmed state).
		clone := sourceEl.Call("cloneNode", true)
		clone.Get("style").Set("cssText",
			"max-width:95vw;max-height:90vh;width:auto;height:auto;"+
				"display:block;cursor:default;")
		clone.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("stopPropagation") // don't close when clicking the SVG itself
			return nil
		}))
		backdrop.Call("appendChild", clone)
	} else {
		// Regular <img> — create a new img element with the same src.
		img := doc.Call("createElement", "img")
		src := sourceEl.Call("getAttribute", "src")
		if !src.IsNull() && !src.IsUndefined() {
			img.Set("src", src.String())
		}
		img.Get("style").Set("cssText",
			"max-width:95vw;max-height:90vh;object-fit:contain;"+
				"display:block;cursor:default;border-radius:4px;")
		img.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("stopPropagation")
			return nil
		}))
		backdrop.Call("appendChild", img)
	}

	doc.Get("body").Call("appendChild", backdrop)
}

// =====================================================================
//  Help deck renderer (tablet-friendly two-level manual pages)
// =====================================================================

// renderHelpDeck renders a TabHelpDeck tab.
//
// Design rationale:
//
//	The IDE targets tablet + mouse use. Normal tab buttons (24px height,
//	11px font) are too small for finger navigation. The help deck solves
//	this by splitting documentation into two visual levels:
//
//	  Level 1 — Card grid:
//	    Large cards (min 60×60px touch target, well above the 44px minimum).
//	    Each card shows the page name and a language badge.
//	    Cards scroll horizontally when there are many pages.
//	    Both tap and click work identically.
//
//	  Level 2 — Markdown view:
//	    Full-width Markdown rendered with Marked.js (same as TabMarkdown).
//	    A "← Back" button (large, thumb-friendly) returns to the card grid.
//
//	The transition between levels uses display:none / display:block —
//	no animation library needed, zero flicker on WASM canvas.
//
// Português:
//
//	Dois níveis visuais para toque: grade de cards (Nível 1) e
//	markdown expandido (Nível 2). Botão "← Back" grande para tablet.
//
// renderHelpDeck renders a TabHelpDeck tab as a two-level layout:
//
//	[ Properties ][ Help ]________________________  ← outer tab bar (tabButtonStyle)
//	[ sub-tab ][ sub-tab ][ sub-tab ]_____________  ← inner sub-tab bar (sub-tab style)
//	Markdown content of the active sub-tab__________
//
// Each HelpCard becomes one sub-tab button. The first sub-tab is active by
// default — no extra tap needed to start reading. This is better for tablets
// than the previous card-grid → reader two-step interaction.
//
// Sub-tab buttons are deliberately shorter than the outer tabs (38px vs 42px)
// and use a visually distinct style so the user clearly understands the
// two levels of hierarchy.
//
// Português:
//
//	Cada HelpCard vira uma sub-aba. A primeira está aberta por padrão.
//	Design de dois níveis: aba externa (Properties/Help) e sub-aba interna
//	por página de manual.
func renderHelpDeck(doc js.Value, container js.Value, tab Tab) {
	if len(tab.HelpCards) == 0 {
		empty := doc.Call("createElement", "div")
		empty.Get("style").Set("cssText", fmt.Sprintf(
			"padding:32px;text-align:center;color:%s;font-size:14px;", colSubtext))
		empty.Set("textContent", "No documentation available.")
		container.Call("appendChild", empty)
		return
	}

	// Root: full height column
	root := doc.Call("createElement", "div")
	root.Get("style").Set("cssText",
		"display:flex;flex-direction:column;height:100%;overflow:hidden;")
	container.Call("appendChild", root)

	// ── Sub-tab bar ────────────────────────────────────────────────────────
	// Visually separate from the outer tab bar: slightly indented, smaller
	// font, different accent colour (colBlue instead of colPeach) so the
	// two levels of tabs are never confused.
	subBar := doc.Call("createElement", "div")
	subBar.Get("style").Set("cssText", fmt.Sprintf(
		"display:flex;flex-wrap:wrap;gap:2px;"+
			"background:%s;border-bottom:1px solid %s;"+
			"padding:6px 10px 0;flex-shrink:0;",
		colBase, colSurface1))
	root.Call("appendChild", subBar)

	// ── Content area: one panel per sub-tab ───────────────────────────────
	content := doc.Call("createElement", "div")
	content.Get("style").Set("cssText", "flex:1;overflow:auto;position:relative;min-height:0;")
	// Mark as the scrollable help area so Show() can save/restore scrollTop
	// across OnSaveReopen cycles. querySelector("[data-help-scroll]") is used
	// both by the OnSaveReopen wrapper (to save) and the post-build restore
	// logic (to apply the saved position).
	content.Call("setAttribute", "data-help-scroll", "true")
	root.Call("appendChild", content)

	panels := make([]js.Value, len(tab.HelpCards))
	subBtns := make([]js.Value, len(tab.HelpCards))

	subTabActive := func(idx int) string {
		return fmt.Sprintf(
			"background:%s;color:%s;border:1px solid %s;border-bottom:1px solid %s;"+
				"border-radius:4px 4px 0 0;padding:8px 14px;"+
				"font-size:12px;font-weight:600;cursor:pointer;"+
				"margin-bottom:-1px;position:relative;z-index:1;"+
				"font-family:sans-serif;min-height:38px;white-space:nowrap;"+
				"display:inline-flex;align-items:center;gap:6px;",
			colBase, colBlue, colSurface1, colBase)
	}
	subTabInactive := func() string {
		return fmt.Sprintf(
			"background:transparent;color:%s;border:none;"+
				"border-radius:4px 4px 0 0;padding:8px 14px;"+
				"font-size:12px;font-weight:400;cursor:pointer;"+
				"font-family:sans-serif;min-height:38px;white-space:nowrap;"+
				"display:inline-flex;align-items:center;gap:6px;"+
				"transition:color 0.15s;",
			colSubtext)
	}

	// switchTo activates sub-tab idx and hides the rest.
	switchTo := func(idx int) {
		for j := range panels {
			if j == idx {
				panels[j].Get("style").Set("display", "block")
				subBtns[j].Get("style").Set("cssText", subTabActive(j))
			} else {
				panels[j].Get("style").Set("display", "none")
				subBtns[j].Get("style").Set("cssText", subTabInactive())
			}
		}
	}

	for i, hc := range tab.HelpCards {
		// ── Panel ──────────────────────────────────────────────────────
		p := doc.Call("createElement", "div")
		p.Get("style").Set("cssText", "display:none;width:100%;")
		// Forward the parent tab's DiagramURL and DiagramProps so that any
		// interactive SVG image (e.g. ![](rp2040.svg)) embedded in the
		// markdown is post-processed: fetched inline and elements highlighted
		// according to the current Inspect panel prop values.
		//
		// When this card's content contains PlaceholderMarker and the parent
		// tab carries EmbeddedFields, forward them so renderMarkdown can
		// inject the inline control panel at the placeholder position.
		var embFields []Field
		var embOnSave func(map[string]string)
		var embHeader string
		if len(tab.EmbeddedFields) > 0 && strings.Contains(hc.Content, PlaceholderMarker) {
			embFields = tab.EmbeddedFields
			embOnSave = tab.EmbeddedOnSave
			embHeader = tab.EmbeddedHeader
		}
		renderMarkdown(doc, p, Tab{
			Type:           TabMarkdown,
			Content:        hc.Content,
			DiagramURL:     tab.DiagramURL,
			DiagramProps:   tab.DiagramProps,
			EmbeddedFields: embFields,
			EmbeddedOnSave: embOnSave,
			EmbeddedHeader: embHeader,
		})
		content.Call("appendChild", p)
		panels[i] = p

		// ── Sub-tab button ─────────────────────────────────────────────
		btn := doc.Call("createElement", "button")
		btn.Get("style").Set("cssText", subTabInactive())

		// Button label: page name + language badge
		btn.Set("innerHTML", fmt.Sprintf(
			`%s<span style="font-size:10px;font-weight:500;background:%s;color:%s;`+
				`border-radius:3px;padding:1px 5px;">%s</span>`,
			escapeXMLStr(hc.Name), colPeach, colBase, escapeXMLStr(hc.Language)))

		idx := i // capture for closure
		btn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				switchTo(idx)
				return nil
			}))

		subBar.Call("appendChild", btn)
		subBtns[i] = btn
	}

	// Activate the first sub-tab by default — no extra tap needed.
	switchTo(0)
}

// escapeXMLStr escapes the minimal set of XML special characters for safe
// inline HTML insertion. Used only inside renderHelpDeck button labels.
func escapeXMLStr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func renderMarkdown(doc js.Value, container js.Value, tab Tab) {
	wrapper := doc.Call("createElement", "div")
	wrapper.Get("style").Set("cssText", fmt.Sprintf(
		"padding:16px;color:%s;font-size:13px;line-height:1.7;"+
			"font-family:sans-serif;",
		colText))

	// Inject markdown CSS (headings, code blocks, links)
	style := doc.Call("createElement", "style")
	style.Set("textContent", markdownCSS())
	wrapper.Call("appendChild", style)

	// Content div
	content := doc.Call("createElement", "div")
	content.Set("className", "md-content")
	wrapper.Call("appendChild", content)
	container.Call("appendChild", wrapper)

	// renderMD parses markdown string and sets innerHTML.
	// After the HTML is injected, any interactive SVG images are post-processed:
	// <img> tags whose src matches DiagramURL are replaced with inline SVGs
	// that highlight the elements selected in the Inspect panel Properties tab.
	//
	// When EmbeddedFields are present and the markdown contains the placeholder
	// marker inside an HTML comment, the entire comment is replaced with a
	// marker <div> before parsing. After marked.js renders the HTML, the marker
	// is located in the DOM and the control panel form is injected at that
	// position.
	renderMD := func(md string) {
		// Detect the placeholder marker inside an HTML comment and replace
		// the entire comment with a DOM-queryable marker div.
		// This is robust against whitespace variations — e.g.:
		//   <!-- place_the_control_panel_here -->
		//   <!--  place_the_control_panel_here  -->
		//   <!-- place_the_control_panel_here-->
		hasEmbeddedPanel := len(tab.EmbeddedFields) > 0 && strings.Contains(md, PlaceholderMarker)
		if hasEmbeddedPanel {
			md = ReplaceHTMLCommentContaining(md, PlaceholderMarker,
				`<div data-embedded-props="true"></div>`)
		}

		loadMarked(doc, func() {
			marked := js.Global().Get("marked")
			if !marked.Truthy() {
				content.Set("innerHTML",
					"<pre style=\"white-space:pre-wrap;\">"+escapeHTML(md)+"</pre>")
				return
			}
			html := marked.Call("parse", md)
			content.Set("innerHTML", html.String())

			// Inject embedded control panel at the placeholder position.
			// The form is rendered as real DOM elements (not innerHTML) so
			// event handlers (Apply, Enter-to-save) work correctly.
			if hasEmbeddedPanel {
				placeholder := content.Call("querySelector", "[data-embedded-props]")
				if placeholder.Truthy() {
					renderEmbeddedForm(doc, placeholder, tab.EmbeddedFields, tab.EmbeddedOnSave, tab.EmbeddedHeader)
				}
			}

			// Post-process interactive SVGs embedded in the markdown.
			// When DiagramURL is set (e.g. "/static/devices/owner/repo/rp2040.svg"),
			// find <img> tags pointing to that SVG and replace each one with an
			// inline <svg> element where the selected elements are highlighted.
			if tab.DiagramURL != "" {
				activateInlineSVGs(doc, content, tab.DiagramURL, tab.DiagramProps)
			}

			// Make all remaining images (non-SVG) clickable for lightbox.
			addImageLightbox(doc, content)

			// Scan PNG images for IoTMaker steganography markers.
			// Images with embedded stage data get a "Load Example" button overlay.
			enhanceIoTMakerImages(doc, content)
		})
	}

	// If ContentURL is set, fetch from server; otherwise use inline Content.
	if tab.ContentURL != "" {
		// Show loading state
		content.Set("innerHTML", fmt.Sprintf(
			"<p style=\"color:%s;\">Loading...</p>", colSubtext))

		log.Printf("[Overlay] Fetching markdown from %s", tab.ContentURL)

		thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			resp := args[0]
			if !resp.Get("ok").Bool() {
				status := resp.Get("status").Int()
				content.Set("innerHTML", fmt.Sprintf(
					"<p style=\"color:%s;\">Failed to load: HTTP %d</p>", colRed, status))
				return js.Null()
			}
			return resp.Call("text")
		})

		thenText := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].IsNull() || args[0].IsUndefined() {
				return nil
			}
			md := args[0].String()
			log.Printf("[Overlay] Markdown loaded: %d bytes from %s", len(md), tab.ContentURL)
			renderMD(md)
			return nil
		})

		catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			errMsg := "unknown error"
			if args[0].Get("message").Truthy() {
				errMsg = args[0].Get("message").String()
			}
			content.Set("innerHTML", fmt.Sprintf(
				"<p style=\"color:%s;\">Failed to load: %s</p>", colRed, escapeHTML(errMsg)))
			log.Printf("[Overlay] Markdown fetch error: %s", errMsg)
			return nil
		})

		js.Global().Call("fetch", tab.ContentURL).
			Call("then", thenResponse).
			Call("then", thenText).
			Call("catch", catchFn)
	} else {
		renderMD(tab.Content)
	}
}

// renderEmbeddedForm injects a compact property form inside a markdown
// document at the position of the PlaceholderMarker marker div.
//
// The form is visually distinct from surrounding markdown content: a
// bordered container with a subtle background gives the maker a clear
// visual cue that "this is the configuration area". The styling follows
// the same Catppuccin Mocha palette as renderForm() so the fields look
// consistent with the standalone Properties tab.
//
// Unlike renderForm(), this function does NOT create a Close button —
// the overlay's own close mechanisms (×, Escape, backdrop click) handle
// that. Only the Apply button is shown, keeping the embedded form compact.
//
// The placeholder div's own content is replaced entirely — it acts as the
// mount point for the form DOM elements.
//
// Português: Renderiza um formulário de propriedades inline no markdown,
// no lugar do marcador PlaceholderMarker. Estilo compacto sem botão Close.
func renderEmbeddedForm(doc js.Value, placeholder js.Value, fields []Field, onSave func(map[string]string), header string) {
	// Clear the marker div and style it as a form container.
	placeholder.Set("innerHTML", "")
	placeholder.Get("style").Set("cssText", fmt.Sprintf(
		"background:%s;border:1px solid %s;border-radius:6px;"+
			"padding:16px;margin:12px 0;"+
			"display:flex;flex-direction:column;gap:10px;",
		colMantle, colSurface1))

	// Section header so the maker knows what this area is.
	// The text comes from the caller via translate.T() — the overlay
	// package does not import the i18n system directly.
	if header == "" {
		header = "Properties" // fallback, should not happen in practice
	}
	headerEl := doc.Call("createElement", "div")
	headerEl.Get("style").Set("cssText", fmt.Sprintf(
		"color:%s;font-size:12px;font-weight:600;margin-bottom:4px;"+
			"text-transform:uppercase;letter-spacing:0.5px;",
		colPeach))
	headerEl.Set("textContent", header)
	placeholder.Call("appendChild", headerEl)

	// Map of field key → input element (for reading values on Apply).
	inputs := make(map[string]js.Value, len(fields))

	// Apply button element (created early so doSave can reference it).
	var applyBtn js.Value

	// doSave collects all field values and calls onSave.
	doSave := func() {
		if onSave == nil {
			return
		}
		values := make(map[string]string, len(inputs))
		for key, el := range inputs {
			if el.Get("type").String() == "checkbox" {
				if el.Get("checked").Bool() {
					values[key] = "true"
				} else {
					values[key] = "false"
				}
			} else {
				values[key] = el.Get("value").String()
			}
		}
		log.Printf("[Overlay] Embedded Apply: %v", values)
		onSave(values)

		// Visual feedback on apply button.
		if applyBtn.Truthy() {
			applyBtn.Set("textContent", "Applied!")
			applyBtn.Get("style").Set("background", colGreen)
			js.Global().Call("setTimeout",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					applyBtn.Set("textContent", "Apply")
					applyBtn.Get("style").Set("background", colBlue)
					return nil
				}), 1000)
		}
	}

	// Enter key handler (not textarea — Enter adds newline there).
	enterKeyFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		e := args[0]
		if e.Get("key").String() == "Enter" {
			e.Call("preventDefault")
			doSave()
		}
		return nil
	})

	// Render each field — same logic as renderForm() but in a compact layout.
	for _, field := range fields {
		row := doc.Call("createElement", "div")
		// Same alignment rationale as renderForm above — flex-start
		// keeps the label at the top for multi-line fields, and the
		// label's padding-top compensates for single-line inputs.
		row.Get("style").Set("cssText",
			"display:flex;align-items:flex-start;gap:10px;")

		// Label
		label := doc.Call("createElement", "label")
		label.Get("style").Set("cssText", fmt.Sprintf(
			"color:%s;font-size:12px;font-weight:500;min-width:90px;"+
				"text-align:right;flex-shrink:0;padding-top:5px;", colText))
		label.Set("textContent", field.Label)
		row.Call("appendChild", label)

		// Input element
		var input js.Value
		switch field.Type {
		case FieldSelect:
			input = doc.Call("createElement", "select")
			input.Get("style").Set("cssText", selectStyle())
			for _, opt := range field.Options {
				option := doc.Call("createElement", "option")
				option.Set("value", opt.Value)
				option.Set("textContent", opt.Label)
				if opt.Value == field.Value {
					option.Set("selected", true)
				}
				input.Call("appendChild", option)
			}
			input.Call("addEventListener", "keydown", enterKeyFn)
		case FieldCheckbox:
			input = doc.Call("createElement", "input")
			input.Set("type", "checkbox")
			input.Get("style").Set("cssText",
				"width:16px;height:16px;accent-color:"+colBlue+";cursor:pointer;")
			if field.Value == "true" {
				input.Set("checked", true)
			}
			input.Call("addEventListener", "keydown", enterKeyFn)
		case FieldTextarea:
			input = doc.Call("createElement", "textarea")
			rows := field.Rows
			if rows == 0 {
				rows = 4
			}
			input.Set("rows", rows)
			input.Get("style").Set("cssText", inputStyle()+"resize:vertical;")
			input.Set("value", field.Value)
			if field.Placeholder != "" {
				input.Set("placeholder", field.Placeholder)
			}
			input.Call("addEventListener", "keydown",
				js.FuncOf(func(this js.Value, args []js.Value) interface{} {
					e := args[0]
					if e.Get("key").String() == "Enter" && e.Get("ctrlKey").Bool() {
						e.Call("preventDefault")
						doSave()
					}
					return nil
				}))
		case FieldColor:
			input = doc.Call("createElement", "input")
			input.Set("type", "color")
			input.Get("style").Set("cssText",
				"width:40px;height:28px;border:none;cursor:pointer;"+
					"background:transparent;padding:0;")
			input.Set("value", field.Value)
		case FieldFile:
			fileContainer, hiddenInput := buildFileField(doc, field)
			row.Call("appendChild", fileContainer)
			input = hiddenInput
		case FieldMap:
			// Same as renderForm — see the comment there.
			mapContainer, hiddenInput := buildMapField(doc, field)
			row.Call("appendChild", mapContainer)
			input = hiddenInput
		case FieldSlice:
			// Same as renderForm — see the comment there.
			sliceContainer, hiddenInput := buildSliceField(doc, field)
			row.Call("appendChild", sliceContainer)
			input = hiddenInput
		case FieldCaseEditor:
			// FieldCaseEditor renders the StatementCase cases editor. Same
			// hidden-input pattern as FieldSlice/FieldMap — the JSON array of
			// cases is kept in sync on every edit and collected by doSave
			// under field.Key.
			caseContainer, hiddenInput := buildCaseEditorField(doc, field)
			row.Call("appendChild", caseContainer)
			input = hiddenInput
		default: // text, number
			input = doc.Call("createElement", "input")
			input.Set("type", string(field.Type))
			input.Get("style").Set("cssText", inputStyle())
			input.Set("value", field.Value)
			if field.Placeholder != "" {
				input.Set("placeholder", field.Placeholder)
			}
			if field.Min != "" {
				input.Set("min", field.Min)
			}
			if field.Max != "" {
				input.Set("max", field.Max)
			}
			applyIdentifierFilter(input, field)
			input.Call("addEventListener", "keydown", enterKeyFn)
		}

		inputs[field.Key] = input

		// ReadOnly
		if field.ReadOnly {
			input.Set("readOnly", true)
			input.Set("disabled", true)
			input.Get("style").Set("opacity", "0.6")
			input.Get("style").Set("cursor", "not-allowed")
		}

		// ConnectionColor accent
		if field.ConnectionColor != "" {
			input.Get("style").Set("borderColor", field.ConnectionColor)
			input.Get("style").Set("boxShadow", "0 0 0 2px "+field.ConnectionColor+"44")
		}
		if field.ConnectionRole != "" {
			input.Call("setAttribute", "data-connection-role", field.ConnectionRole)
		}

		row.Call("appendChild", input)
		placeholder.Call("appendChild", row)
	}

	// ── Apply button (right-aligned, no Close button for embedded mode) ──
	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText",
		"display:flex;justify-content:flex-end;gap:8px;padding-top:8px;"+
			"border-top:1px solid "+colSurface1+";margin-top:4px;")

	if onSave != nil {
		applyBtn = doc.Call("createElement", "button")
		applyBtn.Get("style").Set("cssText", fmt.Sprintf(
			"background:%s;color:%s;border:none;border-radius:4px;"+
				"padding:6px 20px;cursor:pointer;font-size:12px;font-weight:600;"+
				"transition:opacity 0.15s;", colBlue, colBase))
		applyBtn.Set("textContent", "Apply")

		applyBtn.Call("addEventListener", "mouseenter",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				applyBtn.Get("style").Set("opacity", "0.85")
				return nil
			}))
		applyBtn.Call("addEventListener", "mouseleave",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				applyBtn.Get("style").Set("opacity", "1")
				return nil
			}))
		applyBtn.Call("addEventListener", "click",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				doSave()
				return nil
			}))
		btnRow.Call("appendChild", applyBtn)
	}

	placeholder.Call("appendChild", btnRow)
}

// ReplaceHTMLCommentContaining finds the first HTML comment in s that
// contains the given marker text and replaces the entire comment (from
// "<!--" to "-->") with the replacement string.
//
// This is robust against whitespace variations inside the comment:
//
//	<!-- place_the_control_panel_here -->      ← standard
//	<!--  place_the_control_panel_here  -->    ← extra spaces
//	<!-- place_the_control_panel_here-->       ← no trailing space
//	<!--place_the_control_panel_here -->       ← no leading space
//
// Returns the original string unchanged when the marker is not found
// inside any HTML comment.
//
// Português: Encontra o primeiro comentário HTML que contém o texto
// marcador e substitui o comentário inteiro pela string de substituição.
// Robusto contra variações de espaçamento dentro do comentário.
func ReplaceHTMLCommentContaining(s, marker, replacement string) string {
	markerIdx := strings.Index(s, marker)
	if markerIdx < 0 {
		return s
	}
	// Walk backwards from the marker to find "<!--"
	openIdx := strings.LastIndex(s[:markerIdx], "<!--")
	if openIdx < 0 {
		return s
	}
	// Walk forwards from the marker to find "-->"
	closeSearch := markerIdx + len(marker)
	closeIdx := strings.Index(s[closeSearch:], "-->")
	if closeIdx < 0 {
		return s
	}
	// closeIdx is relative to closeSearch; convert to absolute and include "-->"
	endIdx := closeSearch + closeIdx + 3
	return s[:openIdx] + replacement + s[endIdx:]
}

func markdownCSS() string {
	return fmt.Sprintf(`
.md-content h1 { font-size:20px; color:%s; margin:0 0 12px; border-bottom:1px solid %s; padding-bottom:6px; }
.md-content h2 { font-size:16px; color:%s; margin:16px 0 8px; }
.md-content h3 { font-size:14px; color:%s; margin:12px 0 6px; }
.md-content p { margin:0 0 10px; }
.md-content code { background:%s; color:%s; padding:2px 5px; border-radius:3px; font-size:12px; font-family:monospace; }
.md-content pre { background:%s; border:1px solid %s; border-radius:4px; padding:12px; overflow-x:auto; margin:8px 0; }
.md-content pre code { background:transparent; padding:0; }
.md-content ul, .md-content ol { margin:0 0 10px; padding-left:20px; }
.md-content li { margin:3px 0; }
.md-content a { color:%s; text-decoration:none; }
.md-content a:hover { text-decoration:underline; }
.md-content blockquote { border-left:3px solid %s; margin:8px 0; padding:4px 12px; color:%s; }
.md-content table { border-collapse:collapse; margin:8px 0; width:100%%; }
.md-content th, .md-content td { border:1px solid %s; padding:6px 10px; text-align:left; font-size:12px; color:%s; }
.md-content th { background:%s; font-weight:600; color:%s; }
.md-content img { max-width:100%%; height:auto; display:block; border-radius:4px; }
`,
		colPeach, colSurface1,
		colBlue,
		colText,
		colSurface0, colGreen,
		colMantle, colSurface1,
		colBlue,
		colPeach, colSubtext,
		colSurface1, colText,
		colSurface0, colPeach)
}

// =====================================================================
//  Monaco editor renderer
// =====================================================================

func renderMonaco(doc js.Value, container js.Value, tab Tab) {
	wrapper := doc.Call("createElement", "div")
	wrapper.Get("style").Set("cssText",
		"width:100%;height:100%;min-height:300px;position:relative;")
	container.Call("appendChild", wrapper)

	// Loading indicator
	loading := doc.Call("createElement", "div")
	loading.Get("style").Set("cssText", fmt.Sprintf(
		"position:absolute;top:50%%;left:50%%;transform:translate(-50%%,-50%%);"+
			"color:%s;font-size:12px;font-family:sans-serif;", colSubtext))
	loading.Set("textContent", "Loading editor...")
	wrapper.Call("appendChild", loading)

	lang := tab.Language
	if lang == "" {
		lang = "plaintext"
	}

	loadMonaco(doc, func() {
		// Remove loading indicator
		if loading.Get("parentNode").Truthy() {
			wrapper.Call("removeChild", loading)
		}

		monaco := js.Global().Get("monaco")
		if !monaco.Truthy() {
			loading.Set("textContent", "Failed to load Monaco editor")
			return
		}

		// Create editor container
		editorDiv := doc.Call("createElement", "div")
		editorDiv.Get("style").Set("cssText",
			"width:100%;height:100%;min-height:300px;")
		wrapper.Call("appendChild", editorDiv)

		// Create Monaco editor instance
		opts := js.Global().Get("Object").New()
		opts.Set("value", tab.Content)
		opts.Set("language", lang)
		opts.Set("theme", "vs-dark")
		opts.Set("readOnly", tab.ReadOnly)
		opts.Set("fontSize", 13)
		opts.Set("lineNumbers", "on")
		opts.Set("scrollBeyondLastLine", false)
		opts.Set("automaticLayout", true)
		opts.Set("wordWrap", "on")

		minimapOpts := js.Global().Get("Object").New()
		minimapOpts.Set("enabled", false)
		opts.Set("minimap", minimapOpts)

		paddingOpts := js.Global().Get("Object").New()
		paddingOpts.Set("top", 8)
		paddingOpts.Set("bottom", 8)
		opts.Set("padding", paddingOpts)

		monaco.Get("editor").Call("create", editorDiv, opts)
		log.Printf("[Overlay] Monaco editor created: language=%s readOnly=%v", lang, tab.ReadOnly)
	})
}

// =====================================================================
//  Drag logic (mouse + touch)
// =====================================================================

func attachDragLogic(doc js.Value, titleBar js.Value, panel js.Value) {
	var isDragging bool
	var dragStartX, dragStartY float64
	var panelStartX, panelStartY float64
	var centered bool = true // starts centered via transform

	// startDrag captures the initial positions for both mouse and touch.
	startDrag := func(clientX, clientY float64) {
		isDragging = true
		dragStartX = clientX
		dragStartY = clientY

		if centered {
			rect := panel.Call("getBoundingClientRect")
			panelStartX = rect.Get("left").Float()
			panelStartY = rect.Get("top").Float()
			panel.Get("style").Set("left", fmt.Sprintf("%.0fpx", panelStartX))
			panel.Get("style").Set("top", fmt.Sprintf("%.0fpx", panelStartY))
			panel.Get("style").Set("transform", "none")
			centered = false
		} else {
			panelStartX = panel.Get("offsetLeft").Float()
			panelStartY = panel.Get("offsetTop").Float()
		}
	}

	// moveDrag updates the panel position.
	moveDrag := func(clientX, clientY float64) {
		if !isDragging {
			return
		}
		dx := clientX - dragStartX
		dy := clientY - dragStartY
		newX := panelStartX + dx
		newY := panelStartY + dy
		if newY < 0 {
			newY = 0
		}
		panel.Get("style").Set("left", fmt.Sprintf("%.0fpx", newX))
		panel.Get("style").Set("top", fmt.Sprintf("%.0fpx", newY))
	}

	// ── Mouse events ─────────────────────────────────────────────────────
	titleBar.Call("addEventListener", "mousedown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			e := args[0]
			if e.Get("target").Get("tagName").String() == "BUTTON" {
				return nil
			}
			e.Call("preventDefault")
			startDrag(e.Get("clientX").Float(), e.Get("clientY").Float())
			return nil
		}))

	doc.Call("addEventListener", "mousemove",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isDragging {
				args[0].Call("preventDefault")
				moveDrag(args[0].Get("clientX").Float(), args[0].Get("clientY").Float())
			}
			return nil
		}))

	doc.Call("addEventListener", "mouseup",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			isDragging = false
			return nil
		}))

	// ── Touch events (tablet drag) ───────────────────────────────────────
	touchOpts := js.Global().Get("Object").New()
	touchOpts.Set("passive", false)

	titleBar.Call("addEventListener", "touchstart",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			e := args[0]
			if e.Get("target").Get("tagName").String() == "BUTTON" {
				return nil
			}
			e.Call("preventDefault")
			touch := e.Get("touches").Index(0)
			startDrag(touch.Get("clientX").Float(), touch.Get("clientY").Float())
			return nil
		}), touchOpts)

	doc.Call("addEventListener", "touchmove",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isDragging {
				args[0].Call("preventDefault")
				touch := args[0].Get("touches").Index(0)
				moveDrag(touch.Get("clientX").Float(), touch.Get("clientY").Float())
			}
			return nil
		}), touchOpts)

	doc.Call("addEventListener", "touchend",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			isDragging = false
			return nil
		}))
}

// =====================================================================
//  Resize handle (bottom-right corner, mouse + touch)
// =====================================================================

// attachResizeHandle adds a draggable resize grip to the bottom-right corner
// of the panel. Works with both mouse and touch events.
func attachResizeHandle(doc js.Value, panel js.Value) {
	handle := doc.Call("createElement", "div")
	handle.Get("style").Set("cssText", fmt.Sprintf(
		"position:absolute;bottom:0;right:0;width:16px;height:16px;"+
			"cursor:nwse-resize;z-index:10;"+
			"background:linear-gradient(135deg, transparent 50%%, %s 50%%);"+
			"border-radius:0 0 6px 0;opacity:0.5;transition:opacity 0.15s;",
		colSurface1))

	handle.Call("addEventListener", "mouseenter",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			handle.Get("style").Set("opacity", "1")
			return nil
		}))
	handle.Call("addEventListener", "mouseleave",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			handle.Get("style").Set("opacity", "0.5")
			return nil
		}))

	var isResizing bool
	var resizeStartX, resizeStartY float64
	var startWidth, startHeight float64

	startResize := func(clientX, clientY float64) {
		isResizing = true
		resizeStartX = clientX
		resizeStartY = clientY
		rect := panel.Call("getBoundingClientRect")
		startWidth = rect.Get("width").Float()
		startHeight = rect.Get("height").Float()

		// Convert to absolute positioning if still centered.
		transform := panel.Get("style").Get("transform").String()
		if transform != "" && transform != "none" {
			left := rect.Get("left").Float()
			top := rect.Get("top").Float()
			panel.Get("style").Set("left", fmt.Sprintf("%.0fpx", left))
			panel.Get("style").Set("top", fmt.Sprintf("%.0fpx", top))
			panel.Get("style").Set("transform", "none")
		}
	}

	moveResize := func(clientX, clientY float64) {
		if !isResizing {
			return
		}
		dx := clientX - resizeStartX
		dy := clientY - resizeStartY
		newW := startWidth + dx
		newH := startHeight + dy
		if newW < 320 {
			newW = 320
		}
		if newH < 200 {
			newH = 200
		}
		panel.Get("style").Set("width", fmt.Sprintf("%.0fpx", newW))
		panel.Get("style").Set("maxHeight", fmt.Sprintf("%.0fpx", newH))
		panel.Get("style").Set("height", fmt.Sprintf("%.0fpx", newH))
	}

	// Mouse
	handle.Call("addEventListener", "mousedown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			args[0].Call("stopPropagation")
			startResize(args[0].Get("clientX").Float(), args[0].Get("clientY").Float())
			return nil
		}))

	doc.Call("addEventListener", "mousemove",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isResizing {
				args[0].Call("preventDefault")
				moveResize(args[0].Get("clientX").Float(), args[0].Get("clientY").Float())
			}
			return nil
		}))

	doc.Call("addEventListener", "mouseup",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			isResizing = false
			return nil
		}))

	// Touch
	touchOpts := js.Global().Get("Object").New()
	touchOpts.Set("passive", false)

	handle.Call("addEventListener", "touchstart",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			args[0].Call("stopPropagation")
			touch := args[0].Get("touches").Index(0)
			startResize(touch.Get("clientX").Float(), touch.Get("clientY").Float())
			return nil
		}), touchOpts)

	doc.Call("addEventListener", "touchmove",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isResizing {
				args[0].Call("preventDefault")
				touch := args[0].Get("touches").Index(0)
				moveResize(touch.Get("clientX").Float(), touch.Get("clientY").Float())
			}
			return nil
		}), touchOpts)

	doc.Call("addEventListener", "touchend",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			isResizing = false
			return nil
		}))

	panel.Call("appendChild", handle)
}

// =====================================================================
//  Script loading — lazy, once
// =====================================================================

func loadMarked(doc js.Value, callback func()) {
	if markedLoaded {
		callback()
		return
	}

	if markedLoading {
		// Poll until loaded
		js.Global().Call("setTimeout",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				loadMarked(doc, callback)
				return nil
			}), 100)
		return
	}

	markedLoading = true

	// Monaco's AMD loader installs window.define. If marked.js sees it,
	// it registers as AMD module instead of creating window.marked.
	// Fix: fetch source, hide define, eval, restore define.
	//
	// Português: O loader AMD do Monaco instala window.define. Se o marked.js
	// detecta, ele se registra como módulo AMD ao invés de criar window.marked.
	// Solução: buscar fonte, esconder define, eval, restaurar define.
	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			log.Printf("[Overlay] ERROR: failed to fetch marked.js: HTTP %d", resp.Get("status").Int())
			markedLoading = false
			callback()
			return js.Null()
		}
		return resp.Call("text")
	})

	thenSource := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			markedLoading = false
			callback()
			return nil
		}
		source := args[0].String()

		// Save and hide AMD define so marked.js creates a global
		savedDefine := js.Global().Get("define")
		hasAMD := savedDefine.Truthy()
		if hasAMD {
			js.Global().Set("define", js.Undefined())
		}

		// Execute marked.js source via Function constructor
		js.Global().Get("Function").New(source).Call("call", js.Global())

		// Restore AMD define
		if hasAMD {
			js.Global().Set("define", savedDefine)
		}

		markedLoaded = true
		markedLoading = false
		log.Printf("[Overlay] marked.js loaded (via fetch + eval)")
		callback()
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		markedLoading = false
		log.Printf("[Overlay] ERROR: failed to load marked.js: %v", args[0])
		callback()
		return nil
	})

	js.Global().Call("fetch", MarkedURL).
		Call("then", thenResponse).
		Call("then", thenSource).
		Call("catch", catchFn)
}

func loadMonaco(doc js.Value, callback func()) {
	if monacoLoaded {
		callback()
		return
	}

	if monacoLoading {
		js.Global().Call("setTimeout",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				loadMonaco(doc, callback)
				return nil
			}), 200)
		return
	}

	monacoLoading = true
	script := doc.Call("createElement", "script")
	script.Set("src", MonacoLoaderURL)
	script.Set("onload", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Printf("[Overlay] Monaco loader loaded, configuring require...")

		// Configure AMD loader
		require := js.Global().Get("require")
		configObj := js.Global().Get("Object").New()
		pathsObj := js.Global().Get("Object").New()
		pathsObj.Set("vs", MonacoBaseURL)
		configObj.Set("paths", pathsObj)
		require.Call("config", configObj)

		// Load Monaco editor module
		deps := js.Global().Get("Array").New("vs/editor/editor.main")
		require.Invoke(deps, js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			monacoLoaded = true
			monacoLoading = false
			log.Printf("[Overlay] Monaco editor loaded")
			callback()
			return nil
		}))
		return nil
	}))
	script.Set("onerror", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		monacoLoading = false
		log.Printf("[Overlay] ERROR: failed to load Monaco from %s", MonacoLoaderURL)
		callback()
		return nil
	}))
	doc.Get("head").Call("appendChild", script)
}

// =====================================================================
//  Helpers
// =====================================================================

func _escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// =====================================================================
//  Convenience functions
// =====================================================================

// ShowCode opens an overlay with a Monaco editor displaying the given code.
// The editor is read-only. Useful for displaying generated code.
//
// Português: Abre um overlay com Monaco mostrando o código. Editor é read-only.
func ShowCode(title, code, language string) Handle {
	return Show(Config{
		Title: title,
		Width: "70vw",
		Tabs: []Tab{
			{Label: "Code", Type: TabMonaco, Content: code, Language: language, ReadOnly: true},
		},
	})
}

// ShowCodeMulti opens an overlay with one Monaco tab per file in the
// given map. Each tab is read-only. The language hint is per-file —
// detectFileLanguage maps the extension (.c, .h, .go, ...) to the
// Monaco identifier; defaultLanguage covers anything detectFile
// doesn't recognise (today: the only extensions we ship are .c, .h,
// and .go, so defaultLanguage rarely activates).
//
// Tab ordering: a small list of "main" file names comes first
// (main.c, main.go), then everything else in alphabetical order.
// This puts the file the maker is most likely to read at the front
// without forcing the caller to pre-sort the map (map iteration in
// Go is randomised, so the caller couldn't rely on its own
// insertion order anyway).
//
// actions is an optional slice of title-bar buttons — pass nil for
// a code viewer with no extra buttons. Typical callers attach a
// "Download .zip" or "Copy all" action so the maker can take the
// whole multi-file output home.
//
// Português: Abre overlay com uma tab Monaco por arquivo. Cada tab
// é read-only e o hint de linguagem é por extensão. main.c / main.go
// aparecem primeiro; os outros em ordem alfabética. actions
// permite anexar botões customizados (Download ZIP, etc.) na
// barra de título — passe nil pra omitir.
func ShowCodeMulti(title string, files map[string]string, defaultLanguage string, actions []Action) Handle {
	// Sort filenames: "main" candidates first, then alphabetical.
	// Maps in Go iterate in random order, so the sort is the only
	// way to give the maker a stable reading order.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri := mainFileRank(names[i])
		rj := mainFileRank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})

	tabs := make([]Tab, 0, len(names))
	for _, name := range names {
		lang := detectFileLanguage(name)
		if lang == "" {
			lang = defaultLanguage
		}
		tabs = append(tabs, Tab{
			Label:    name,
			Type:     TabMonaco,
			Content:  files[name],
			Language: lang,
			ReadOnly: true,
		})
	}

	return Show(Config{
		Title:   title,
		Width:   "70vw",
		Tabs:    tabs,
		Actions: actions,
	})
}

// mainFileRank returns 0 for filenames the user is most likely to
// open first (main.c, main.go), 1 for everything else. Used as the
// primary sort key in ShowCodeMulti so the principal file leads
// the tab strip regardless of map iteration order.
func mainFileRank(name string) int {
	switch name {
	case "main.c", "main.go", "main.cpp", "main.rs", "main.py":
		return 0
	}
	return 1
}

// detectFileLanguage maps a filename to the Monaco language ID for
// syntax highlighting. Returns "" when the extension is unknown,
// signalling the caller to fall back to its own default. The map
// is intentionally small — every backend the codegen emits today
// produces one of these extensions, and adding new ones is a
// one-line change here when a future backend lands.
func detectFileLanguage(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".c"), strings.HasSuffix(filename, ".h"):
		return "c"
	case strings.HasSuffix(filename, ".cpp"), strings.HasSuffix(filename, ".hpp"), strings.HasSuffix(filename, ".cc"):
		return "cpp"
	case strings.HasSuffix(filename, ".go"):
		return "go"
	case strings.HasSuffix(filename, ".rs"):
		return "rust"
	case strings.HasSuffix(filename, ".py"):
		return "python"
	case strings.HasSuffix(filename, ".js"):
		return "javascript"
	case strings.HasSuffix(filename, ".ts"):
		return "typescript"
	case strings.HasSuffix(filename, ".json"):
		return "json"
	case strings.HasSuffix(filename, ".md"):
		return "markdown"
	}
	return ""
}

// ShowError opens an overlay displaying error messages.
//
// Português: Abre um overlay mostrando mensagens de erro.
func ShowError(title, message string) Handle {
	md := "## Errors\n\n```\n" + message + "\n```"
	return Show(Config{
		Title: title,
		Width: "500px",
		Tabs: []Tab{
			{Label: "Errors", Type: TabMarkdown, Content: md},
		},
	})
}

// =====================================================================
//  Preload — load libraries eagerly (e.g. during splash screen)
// =====================================================================

// PreloadMonaco loads the Monaco editor library eagerly.
// Blocks until Monaco is fully loaded. Call from a goroutine.
// Returns nil on success, error message on failure.
//
// Example (in main.go):
//
//	go func() {
//	    _ = splash.AddText("Loading code editor...")
//	    if err := overlay.PreloadMonaco(); err != nil {
//	        log.Printf("Monaco preload failed: %s", err)
//	    }
//	    _ = splash.AddText("Code editor ready")
//	}()
//
// Português: Carrega a biblioteca Monaco eagerly. Bloqueia até carregar.
// Chame de dentro de uma goroutine.
func PreloadMonaco() error {
	done := make(chan error, 1)
	doc := js.Global().Get("document")

	loadMonaco(doc, func() {
		if monacoLoaded {
			done <- nil
		} else {
			done <- fmt.Errorf("failed to load Monaco from %s", MonacoLoaderURL)
		}
	})

	return <-done
}

// PreloadMarked loads the Marked.js library eagerly.
// Blocks until loaded. Call from a goroutine.
//
// Português: Carrega a biblioteca Marked.js eagerly. Bloqueia até carregar.
func PreloadMarked() error {
	done := make(chan error, 1)
	doc := js.Global().Get("document")

	loadMarked(doc, func() {
		if markedLoaded {
			done <- nil
		} else {
			done <- fmt.Errorf("failed to load Marked from %s", MarkedURL)
		}
	})

	return <-done
}

// ── highlight.js loading ───────────────────────────────────────────────────
//
// The /highlight/highlight.min.js shipped with the server is the
// "common" build of highlight.js v11.9.0. It already ships with 36
// languages embedded (bash, c, cpp, csharp, css, diff, dockerfile,
// go, graphql, ini, java, javascript, json, kotlin, less, lua,
// makefile, markdown, objectivec, perl, php, plaintext, python,
// python-repl, r, ruby, rust, scss, shell, sql, swift, typescript,
// vbnet, wasm, xml, yaml) — the curated "common" set highlight.js
// publishes as a one-stop bundle. No per-language modules need to
// be loaded separately for any of these.
//
// Note: my earlier reading missed this because the registerLanguage
// calls in the minified bundle live inside a loop
// (`for (const k of Object.keys(Ke)) He.registerLanguage(...)`),
// not as direct `hljs.registerLanguage('go', ...)` literals. A
// grep for the literal pattern found zero hits and I incorrectly
// concluded the file was core-only.
//
// Aliases provided for free: `c` covers `arduino`/`ino`/`h`/`hpp`,
// `bash` covers `sh`/`zsh`, `javascript` covers `js`/`jsx`,
// `typescript` covers `ts`/`tsx`, `xml` covers `html`, `yaml`
// covers `yml`. So a ```arduino fence renders with C colors with
// no extra setup.

func loadHighlight(doc js.Value, callback func()) {
	if hljsLoaded {
		callback()
		return
	}

	if hljsLoading {
		// Poll until loaded.
		js.Global().Call("setTimeout",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				loadHighlight(doc, callback)
				return nil
			}), 100)
		return
	}

	hljsLoading = true

	// Plain <script src=...> append — NOT the fetch+eval pattern that
	// loadMarked uses. Reason:
	//
	// Marked is published as UMD. Its module body is:
	//   !function(e,t){...t((e=globalThis).marked={})...}
	// The assignment expression `(e=globalThis).marked={}` runs in
	// any scope, so `new Function(source).call(window)` correctly
	// puts `marked` on `window`.
	//
	// Highlight.js v11.x ships without UMD. Its body is:
	//   var hljs=function(){...}();
	// Inside `new Function(source)`, the leading `var hljs` becomes
	// a LOCAL of the synthetic function, not a global. window.hljs
	// stays undefined, and every downstream call fails silently.
	//
	// A plain <script> executes in real global scope, so the top-
	// level `var hljs` becomes window.hljs as expected. Hiding
	// window.define (the trick loadMarked uses for AMD interference)
	// is unnecessary here: highlight.js v11 has no AMD branch (only
	// a CommonJS check for `typeof exports/module`), so the Monaco
	// loader's window.define cannot interfere.
	s := doc.Call("createElement", "script")
	s.Set("src", HighlightURL)

	s.Set("onload", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		hljs := js.Global().Get("hljs")
		if !hljs.Truthy() {
			log.Printf("[Overlay] ERROR: highlight.js script loaded but window.hljs is missing")
			hljsLoading = false
			callback()
			return nil
		}
		hljsLoaded = true
		hljsLoading = false
		// listLanguages is a known API in v11 and returns an array;
		// we log the count so a future regression — e.g. someone
		// replaces the file with the core-only build — surfaces
		// loudly at boot time instead of in silent dead highlighting.
		nlangs := hljs.Call("listLanguages").Get("length").Int()
		log.Printf("[Overlay] highlight.js loaded with %d language(s) embedded", nlangs)
		callback()
		return nil
	}))

	s.Set("onerror", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Printf("[Overlay] ERROR: failed to load highlight.js from %s", HighlightURL)
		hljsLoading = false
		callback()
		return nil
	}))

	doc.Get("head").Call("appendChild", s)
}

// PreloadHighlight loads highlight.js eagerly. Blocks until loaded.
// Call from a goroutine, typically from the splash screen during
// app boot. After this returns, window.hljs is ready and any
// markdown preview with code fences will get syntax highlighting
// when rendered.
//
// Português: Carrega highlight.js eagerly. Bloqueia até carregar.
// Chamar de uma goroutine durante o splash.
func PreloadHighlight() error {
	done := make(chan error, 1)
	doc := js.Global().Get("document")

	loadHighlight(doc, func() {
		if hljsLoaded {
			done <- nil
		} else {
			done <- fmt.Errorf("failed to load highlight.js from %s", HighlightURL)
		}
	})

	return <-done
}

// sanitizeIdentifier strips every character that cannot appear in a C/Go
// identifier and removes any leading digits, so the result is always a valid
// identifier prefix or the empty string. It backs the Field InputFilter
// "identifier" path: running on every keystroke and paste means a name like
// "input a" never reaches the device (the space is dropped as it is typed) and
// "9temp" collapses to "temp". An empty result is acceptable — the codegen
// skips variables with an empty name (an unbound device contributes nothing).
//
// Português: Remove todo caractere que não pode estar num identificador C/Go e
// tira dígitos iniciais — o resultado é sempre um prefixo de identificador
// válido ou vazio. Roda a cada tecla e colagem; vazio é aceitável (o codegen
// ignora variáveis sem nome).
func sanitizeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '_',
			r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return strings.TrimLeft(b.String(), "0123456789")
}

// applyIdentifierFilter wires the Field.InputFilter "identifier" constraint onto
// a freshly created text input: a native pattern/title hint plus a live "input"
// listener that re-sanitises the value on every keystroke and paste. No-op when
// the field has no filter (or an unrecognised one), so it is safe to call
// unconditionally from each input-rendering path.
//
// Português: Liga a restrição InputFilter "identifier" a um input de texto: dica
// nativa (pattern/title) + listener "input" que re-sanitiza a cada tecla e
// colagem. No-op quando o campo não tem filtro, então é seguro chamar sempre.
func applyIdentifierFilter(input js.Value, field Field) {
	if field.InputFilter != "identifier" {
		return
	}
	input.Set("pattern", "[A-Za-z_][A-Za-z0-9_]*")
	input.Set("title", "Letters, digits and underscore only; must start with a letter or underscore")
	input.Call("addEventListener", "input",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			el := args[0].Get("target")
			raw := el.Get("value").String()
			if clean := sanitizeIdentifier(raw); clean != raw {
				el.Set("value", clean)
			}
			return nil
		}))
}
