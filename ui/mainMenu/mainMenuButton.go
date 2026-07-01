// ui/mainMenu/mainMenuButton.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package mainMenu

// mainMenuButton.go — Fixed hexagonal button that opens the IDE main menu.
//
// [DENSITY-FIX] All sizes use rulesDensity.Density — scaling is automatic via
// .GetFloat(). No manual "value * density" needed.
//
// [RULES-FIX] All visual configuration (colors, sizes, fonts, margins) comes from
// the rulesMainMenu package. To customize the menu appearance, modify the exported
// variables in rulesMainMenu/rules.go before calling Button.Init().
//
// [CAMERA-FIX] Button is screen-space: fixed on screen, ignores camera pan/zoom.
// [MENU-FIX] Menu items open relative to the button position.
//
// Features:
//   - Configurable position: SetCorner() with named constants or SetPosition() for manual
//   - Attention animation: flashes red, then rests, repeating in a cycle
//   - SetAttentionDuration/SetNormalDuration/SetFlashInterval for timing control
//   - Opens hierarchical hex menu: Math → Add/Sub/Mul/Div, Loop → Loop

import (
	"fmt"
	"log"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/blackbox"

	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesMainMenu"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/translate"
)

// Corner represents a screen corner for automatic button positioning.
type Corner int

const (
	// CornerTopLeft positions the button in the top-left corner (default).
	// Follows the Gutenberg diagram — primary optical area, where users
	// naturally look first. Standard placement for IDE/editor menus.
	CornerTopLeft Corner = iota

	// CornerTopRight positions the button in the top-right corner.
	CornerTopRight

	// CornerBottomLeft positions the button in the bottom-left corner.
	CornerBottomLeft

	// CornerBottomRight positions the button in the bottom-right corner.
	// Material Design FAB position — natural thumb reach on mobile.
	CornerBottomRight
)

// Button is a fixed hexagonal button on the stage that opens the
// main IDE menu when clicked. It has an attention animation that cycles
// between flashing (to attract the user's eye) and resting.
//
// [CAMERA-FIX] The button is rendered in screen-space: it stays fixed on screen
// regardless of camera pan/zoom.
//
// [DENSITY-FIX] hexRadius is rulesDensity.Density — .GetFloat() returns the
// density-scaled pixel value. No separate scaledRadius field needed.
//
// [RULES-FIX] All visual defaults come from the rulesMainMenu package.
//
// Português: Botão hexagonal fixo no palco que abre o menu principal da IDE.
// Todas as configurações visuais vêm do pacote rulesMainMenu.
type Button struct {
	stage sprite.Stage
	elem  sprite.Element
	panel *Panel

	// Position (screen coordinates — already in physical pixels)
	posX      float64
	posY      float64
	corner    Corner
	useCorner bool // true = auto-position from corner, false = manual posX/posY

	// Hex appearance (density-independent logical value; .GetFloat() scales it)
	hexRadius rulesDensity.Density

	// Attention animation timing (configurable via setters)
	attentionDuration    time.Duration // how long to flash (default 3s)
	normalDuration       time.Duration // how long to stay normal (default 15s)
	flashInterval        time.Duration // flash toggle speed (default 500ms)
	attentionEnabled     *bool         // nil = default (true), explicit true/false via setter
	stopAttentionOnClick *bool         // nil = default (true), any stage click stops attention

	// Animation state
	animDone chan struct{}

	// catchClickFunc: document-level one-shot listener to stop attention on any click.
	catchClickFunc *js.Func

	// SVG caches for 3 states
	cacheNormal     string
	cacheAttention1 string
	cacheAttention2 string

	// Generation counter for unique IDs
	generation int

	// Menu items (can be overridden via SetMenuItems)
	menuItems []hexMenu.MenuItem

	// readmes maps MenuItem.ID → ordered readme tabs for black-box device
	// items. Same shape as methodHelps below. Single-tab entries (the
	// common case) render without a tab bar.
	// Populated by SetItemReadmes() and forwarded to Panel on Open().
	readmes map[string][]blackbox.HelpTabClient

	// methodHelps maps MenuItem.ID → ordered help tabs for method items.
	// Populated by SetItemMethodHelps() and forwarded to Panel on Open().
	methodHelps map[string][]blackbox.HelpTabClient

	initialized bool
}

// --- Setters (call before Init) ---

// SetStage sets the sprite.Stage. Must be called before Init().
func (e *Button) SetStage(stage sprite.Stage) {
	e.stage = stage
}

// SetCorner positions the button at a named screen corner.
// Uses automatic margins from rulesMainMenu.CornerMarginX/Y (scaled by density).
// This is the recommended way to position the button. Default: CornerTopLeft.
//
// If SetPosition() was called previously, SetCorner() overrides it.
func (e *Button) SetCorner(corner Corner) {
	e.corner = corner
	e.useCorner = true
}

// SetPosition sets the button position manually (screen coordinates in pixels).
// Overrides any previous SetCorner() call.
func (e *Button) SetPosition(x, y float64) { //todo: ajustar density
	e.posX = x
	e.posY = y
	e.useCorner = false
	if e.elem != nil {
		e.elem.SetPosition(x, y)
	}
}

// SetAttentionDuration sets how long the button flashes to attract attention.
// Default: 3000 (3 seconds).
func (e *Button) SetAttentionDuration(ms int64) {
	e.attentionDuration = time.Duration(ms) * time.Millisecond
}

// SetNormalDuration sets how long the button stays normal between flash cycles.
// Default: 15000 (15 seconds).
func (e *Button) SetNormalDuration(ms int64) {
	e.normalDuration = time.Duration(ms) * time.Millisecond
}

// SetFlashInterval sets the toggle speed during the attention phase.
// Default: 500 (500ms per flash toggle).
func (e *Button) SetFlashInterval(ms int64) {
	e.flashInterval = time.Duration(ms) * time.Millisecond
}

// SetAttentionEnabled enables or disables the initial attention animation.
// Default: true (animation runs on Init). Set to false to start with no animation.
func (e *Button) SetAttentionEnabled(enabled bool) {
	e.attentionEnabled = &enabled
}

// SetStopAttentionOnClick controls whether any click on the stage stops
// the attention animation. Default: true.
func (e *Button) SetStopAttentionOnClick(enabled bool) {
	e.stopAttentionOnClick = &enabled
}

// SetMenuItems overrides the default IDE main menu items.
func (e *Button) SetMenuItems(items []hexMenu.MenuItem) {
	e.menuItems = items
}

// SetItemReadmes registers ordered readme tabs for black-box device menu items.
// The map key is the MenuItem.ID (e.g. "bb_APDS9960"). Each value is the
// per-language tab slice resolved by HelpReadmeTabs at the call site.
// Called after SetMenuItems; forwarded to the Panel on the next Open() call.
func (e *Button) SetItemReadmes(readmes map[string][]blackbox.HelpTabClient) {
	e.readmes = readmes
}

// SetItemMethodHelps registers ordered help tabs for black-box method items.
// The map key is the MenuItem.ID (e.g. "bb_APDS9960_init").
// Called after SetMenuItems; forwarded to the Panel on the next Open() call.
func (e *Button) SetItemMethodHelps(helps map[string][]blackbox.HelpTabClient) {
	e.methodHelps = helps
}

// SetHexRadius sets the radius of the hexagonal button (density-independent
// logical value). The actual size on screen is determined by Density automatically.
// Default: rulesMainMenu.ButtonHexRadius (32).
func (e *Button) SetHexRadius(radius rulesDensity.Density) {
	e.hexRadius = radius
}

// --- Init ---

// Init creates the button element on the stage and starts the animation.
// Must be called from a goroutine (blocks during SVG caching).
//
// [DENSITY-FIX] All sizes are rulesDensity.Density — .GetFloat() returns
// density-scaled pixel values. No manual multiplication needed.
// [RULES-FIX] All defaults come from rulesMainMenu package.
// [CAMERA-FIX] The button element is set to screen-space.
func (e *Button) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("[MainMenuButton] SetStage() must be called before Init()")
	}

	// Apply defaults from rulesMainMenu (density-independent logical values)
	if e.hexRadius == 0 {
		e.hexRadius = rulesMainMenu.ButtonHexRadius
	}
	if e.attentionDuration == 0 {
		e.attentionDuration = 3 * time.Second
	}
	if e.normalDuration == 0 {
		e.normalDuration = 15 * time.Second
	}
	if e.flashInterval == 0 {
		e.flashInterval = 500 * time.Millisecond
	}
	if e.menuItems == nil {
		log.Printf("[MainMenuButton] WARNING: no menu items set. Call SetMenuItems() before Init().")
		e.menuItems = []hexMenu.MenuItem{}
	}

	// Calculate position from corner (or default to top-left)
	if e.useCorner || (e.posX == 0 && e.posY == 0) {
		e.posX, e.posY = e.cornerPosition()
	}

	// [RULES-FIX] Config comes from rulesMainMenu.ButtonConfig().
	// Density values; RenderHexagonSVG calls .GetFloat() internally.
	//
	// Português: Config vem de rulesMainMenu.ButtonConfig().
	// Valores Density; RenderHexagonSVG chama .GetFloat() internamente.
	config := rulesMainMenu.ButtonConfig(e.hexRadius)
	config.ApplyDefaults()

	buttonItem := hexMenu.MenuItem{
		ID:              "mainMenu",
		Col:             1,
		Row:             1,
		Label:           translate.T("menuMainMenuIcon", "Menu"),
		FontAwesomePath: rulesIcon.KFABars,
		ViewBox:         "0 0 448 512",
		Styles:          rulesMainMenu.ButtonStyles(),
	}

	e.cacheNormal = hexMenu.RenderHexagonSVG(buttonItem, hexMenu.PipelineNormal, &config)
	e.cacheAttention1 = hexMenu.RenderHexagonSVG(buttonItem, hexMenu.PipelineAttention1, &config)
	e.cacheAttention2 = hexMenu.RenderHexagonSVG(buttonItem, hexMenu.PipelineAttention2, &config)

	// [DENSITY-FIX] .GetFloat() returns density-scaled pixel size.
	// [VIEWPORT-FIX] Use HexSvgWidth/Height (ceiled) to match SVG viewport.
	scaledRadius := e.hexRadius.GetFloat()
	w := hexMenu.HexSvgWidth(scaledRadius)
	h := hexMenu.HexSvgHeight(scaledRadius)

	e.generation++
	elemID := fmt.Sprintf("mainMenuBtn_%d", e.generation)

	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:     elemID,
		X:      e.posX,
		Y:      e.posY,
		Width:  w,
		Height: h,
		Index:  rulesMainMenu.ButtonZIndex,
		SvgXml: e.cacheNormal,
	})
	if err != nil {
		return fmt.Errorf("[MainMenuButton] create element: %w", err)
	}

	// [CAMERA-FIX] Screen-space: fixed on screen, ignores camera pan/zoom.
	e.elem.SetScreenSpace(true)
	e.elem.SetDragEnable(false)
	e.elem.SetResizeEnable(false)

	// Create the panel — one per button, lives for the button's lifetime.
	e.panel = NewPanel()

	// Wire click
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		// [DENSITY-FIX] .GetFloat() returns density-scaled radius for hit-test.
		// [VIEWPORT-FIX] Hex center in SVG is at (svgW/2, svgH/2) after Ceil fix.
		r := e.hexRadius.GetFloat()
		hx := hexMenu.HexSvgWidth(r) / 2
		hy := hexMenu.HexSvgHeight(r) / 2
		if !hexMenu.HexContains(hx, hy, r, event.LocalX, event.LocalY) {
			return
		}

		// Stop attention animation on first interaction
		e.StopAnimation()

		if e.panel.IsVisible() {
			e.panel.Close()
			return
		}

		// goroutine: CacheFromSvg needs Image.onload which requires the
		// JS event loop to be free — can't run inside a click handler.
		go func() {
			// Reset button to normal state (might be mid-flash)
			if e.elem != nil {
				_ = e.elem.CacheFromSvg(e.cacheNormal)
			}
			for id, tabs := range e.readmes {
				e.panel.SetItemReadme(id, tabs)
			}
			for id, tabs := range e.methodHelps {
				e.panel.SetItemMethodHelp(id, tabs)
			}
			e.panel.Open(e.menuItems)
		}()
	})

	// Wire cursor
	e.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		// [DENSITY-FIX] .GetFloat() returns density-scaled radius.
		// [VIEWPORT-FIX] Hex center in SVG is at (svgW/2, svgH/2).
		r := e.hexRadius.GetFloat()
		hx := hexMenu.HexSvgWidth(r) / 2
		hy := hexMenu.HexSvgHeight(r) / 2
		if hexMenu.HexContains(hx, hy, r, lx, ly) {
			return sprite.CursorPointer
		}
		return ""
	})

	e.initialized = true

	// Start attention animation cycle (if enabled)
	enabled := e.attentionEnabled == nil || *e.attentionEnabled // default: true
	if enabled {
		e.startAnimation()
	}

	return nil
}

// --- Position calculation ---

// cornerPosition calculates absolute (x, y) for the button based on the
// selected corner, density-scaled hex radius, and stage dimensions.
//
// [DENSITY-FIX] Uses Density.GetFloat() for all scaled values.
// [RULES-FIX] Margins come from rulesMainMenu.CornerMarginX/Y.
func (e *Button) cornerPosition() (float64, float64) {
	scaledRadius := e.hexRadius.GetFloat()
	w := hexMenu.HexSvgWidth(scaledRadius)
	h := hexMenu.HexSvgHeight(scaledRadius)
	mx := rulesMainMenu.CornerMarginX.GetFloat()
	my := rulesMainMenu.CornerMarginY.GetFloat()

	// [CAMERA-FIX] Use actual stage dimensions instead of hardcoded values.
	canvasW, canvasH := e.stage.GetCanvasSize()
	cw := float64(canvasW)
	ch := float64(canvasH)

	switch e.corner {
	case CornerTopRight:
		return cw - w - mx, my
	case CornerBottomLeft:
		return mx, ch - h - my
	case CornerBottomRight:
		return cw - w - mx, ch - h - my
	default: // CornerTopLeft
		return mx, my
	}
}

// menuOpenPosition calculates where the hex menu should open relative to
// the button, adapting to the corner so the menu doesn't go off-screen.
//
// [DENSITY-FIX] Uses Density.GetFloat() for all scaled values.
// [RULES-FIX] Gap comes from rulesMainMenu.MenuGap.
//
// Português: Calcula onde o menu hex deve abrir relativo ao botão,
// adaptando ao canto para que o menu não saia da tela.
func (e *Button) menuOpenPosition() (float64, float64) {
	scaledRadius := e.hexRadius.GetFloat()
	w := hexMenu.HexSvgWidth(scaledRadius)
	h := hexMenu.HexSvgHeight(scaledRadius)
	gap := rulesMainMenu.MenuGap.GetFloat()

	switch e.corner {
	case CornerTopRight:
		// Open below the button
		return e.posX - gap, e.posY + h + gap
	case CornerBottomLeft:
		// Open to the right of the button
		return e.posX + w + gap, e.posY - gap
	case CornerBottomRight:
		// Open above and to the left
		return e.posX - gap, e.posY - gap
	default: // CornerTopLeft
		// Open to the right of the button
		return e.posX + w + gap, e.posY
	}
}

// --- Animation ---

// StopAnimation stops the attention animation and cleans up the global
// click listener (if active).
func (e *Button) StopAnimation() {
	if e.animDone != nil {
		close(e.animDone)
		e.animDone = nil
	}
	e.removeCatchListener()
}

// removeCatchListener removes the document-level pointerdown listener
// that was installed to stop attention on any stage click.
func (e *Button) removeCatchListener() {
	if e.catchClickFunc != nil {
		js.Global().Get("document").Call("removeEventListener", "pointerdown", *e.catchClickFunc)
		e.catchClickFunc.Release()
		e.catchClickFunc = nil
	}
}

// startAnimation runs the attention cycle in a goroutine:
// flash for attentionDuration → normal for normalDuration → repeat.
func (e *Button) startAnimation() {
	e.StopAnimation()
	e.animDone = make(chan struct{})

	// Install global one-shot listener if enabled (default: true)
	stopOnClick := e.stopAttentionOnClick == nil || *e.stopAttentionOnClick
	if stopOnClick {
		e.installCatchListener()
	}

	go func() {
		done := e.animDone

		for {
			// === Attention phase: flash between Attention1/Attention2 ===
			flashEnd := time.After(e.attentionDuration)
			ticker := time.NewTicker(e.flashInterval)
			state := true

		flashLoop:
			for {
				select {
				case <-done:
					ticker.Stop()
					return
				case <-flashEnd:
					ticker.Stop()
					break flashLoop
				case <-ticker.C:
					if e.elem == nil {
						ticker.Stop()
						return
					}
					if state {
						_ = e.elem.CacheFromSvg(e.cacheAttention1)
					} else {
						_ = e.elem.CacheFromSvg(e.cacheAttention2)
					}
					state = !state
				}
			}

			// Return to normal
			if e.elem != nil {
				_ = e.elem.CacheFromSvg(e.cacheNormal)
			}

			// === Normal phase: wait normalDuration ===
			select {
			case <-done:
				return
			case <-time.After(e.normalDuration):
				// Continue to next attention cycle
			}
		}
	}()
}

// installCatchListener adds a one-shot document pointerdown listener.
func (e *Button) installCatchListener() {
	e.removeCatchListener()

	var cb js.Func
	cb = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		js.Global().Get("document").Call("removeEventListener", "pointerdown", cb)
		e.catchClickFunc = nil

		if e.animDone != nil {
			close(e.animDone)
			e.animDone = nil
		}

		go func() {
			cb.Release()
			if e.elem != nil {
				_ = e.elem.CacheFromSvg(e.cacheNormal)
			}
		}()
		return nil
	})
	e.catchClickFunc = &cb
	js.Global().Get("document").Call("addEventListener", "pointerdown", cb)
}

// SetVisible shows or hides the menu button element on the stage.
// Used by the image export to hide the button before capturing a clean screenshot.
//
// Português: Mostra ou esconde o botão de menu no stage.
// Usado pela exportação de imagem para capturar uma screenshot limpa.
func (e *Button) SetVisible(visible bool) {
	if e.elem != nil {
		e.elem.SetVisible(visible)
	}
}

// ClosePanel closes the hardware menu panel if it is currently open.
// Used by the workspace to dismiss the panel before loading an example
// from a steganography image.
//
// Português: Fecha o painel do menu de hardware se estiver aberto.
func (e *Button) ClosePanel() {
	if e.panel != nil && e.panel.IsVisible() {
		e.panel.Close()
	}
}
