package mainMenu

// spriteHexMenu.go — bridge between hexMenu package and sprite package.
// Creates sprite.Element instances for each hexagon, handles click/navigation/close.
//
// [CAMERA-FIX] All menu elements are screen-space: they ignore the camera transform,
// staying fixed on screen regardless of pan/zoom.
//
// [DENSITY-FIX] Config fields are rulesDensity.Density — scaling is automatic via
// .GetFloat(). The old scaledConfig() pattern is eliminated entirely.
//
// [BACKDROP-FIX] Backdrop uses canvas dimensions from stage.GetCanvasSize() instead
// of hardcoded 4000x4000, ensuring click-outside works at any camera position.

import (
	"fmt"
	"log"
	"time"

	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/rulesMainMenu"
	"github.com/helmutkemper/iotmakerio/sprite"
)

// SpriteHexMenu manages a hexagonal menu rendered as sprite.Element instances.
type SpriteHexMenu struct {
	stage  sprite.Stage
	config hexMenu.Config

	// active elements on current page
	elements []spriteHexMenuElem

	// backdrop catches click outside hexagons
	backdrop sprite.Element

	// navigation stack (each entry = items of that page)
	pageStack [][]hexMenu.MenuItem

	// rootItems: the top-level items passed to Open()/StartTutorial().
	// Used by advanceTutorial() to resolve PagePath navigation.
	rootItems []hexMenu.MenuItem

	// position where menu was opened (screen coordinates)
	posX, posY float64

	visible bool

	// [HEXMENU-FIX] Generation counter ensures unique IDs across Open/Close cycles.
	// Each showPage/createBackdrop increments this, so "hexMenu_resize_3" never
	// conflicts with a previous "hexMenu_resize_2" that's being async-destroyed.
	generation int

	// tutorial
	tutorialSteps   []hexMenu.TutorialStep
	tutorialCurrent int
	tutorialActive  bool
	flashTicker     *time.Ticker
	flashDone       chan struct{}

	// [GHOST-FIX] lastClosedAt records when Close() was last called.
	// Devices check WasJustClosed() to avoid re-opening the menu when the
	// backdrop fires before the device's own click handler (same pointer event
	// dispatched to multiple sprite elements at different z-indices).
	//
	// Problem scenario (without fix):
	//   1. Menu is open.  User clicks the device body.
	//   2. Backdrop (z=1000) receives click first → Close() → visible=false.
	//   3. Device (z=30) receives same click → IsVisible()=false → opens menu.
	//
	// Fix: device checks both IsVisible() and WasJustClosed().
	lastClosedAt time.Time
}

type spriteHexMenuElem struct {
	item   hexMenu.MenuItem
	elem   sprite.Element
	caches [5]string // pre-rendered SVG XML per pipeline state
}

// NewSpriteHexMenu creates a menu manager. Call once, reuse for multiple Open/Close.
func NewSpriteHexMenu(stage sprite.Stage, config hexMenu.Config) *SpriteHexMenu {
	config.ApplyDefaults()
	return &SpriteHexMenu{
		stage:  stage,
		config: config,
	}
}

// Open shows the menu at (posX, posY) with the given items.
// posX, posY are in screen (canvas pixel) coordinates.
// Blocks during SVG→bitmap conversion (call from goroutine if needed).
func (m *SpriteHexMenu) Open(items []hexMenu.MenuItem, posX, posY float64) {
	if m.visible {
		m.Close()
	}

	m.posX = posX
	m.posY = posY
	m.visible = true
	m.pageStack = nil
	m.rootItems = items

	m.createBackdrop()
	m.showPage(items)
}

// OpenCentered shows the menu centered at (cx, cy) in screen coordinates.
func (m *SpriteHexMenu) OpenCentered(items []hexMenu.MenuItem, cx, cy float64) {
	// [DENSITY-FIX] .GetFloat() returns density-scaled pixel values automatically.
	radius := m.config.HexRadius.GetFloat()
	spacing := m.config.Spacing.GetFloat()
	w, h := hexMenu.GridBounds(items, radius, spacing)
	m.Open(items, cx-w/2, cy-h/2)
}

// Close hides all menu elements and schedules their destruction.
// Safe to call from inside a click handler — elements are hidden immediately,
// then destroyed asynchronously after the current event handler returns.
func (m *SpriteHexMenu) Close() {
	if !m.visible {
		return
	}
	m.stopFlash()
	m.tutorialActive = false

	// Collect elements to destroy after the current handler unwinds.
	toDestroy := make([]sprite.Element, 0, len(m.elements)+1)
	for i := range m.elements {
		m.elements[i].elem.SetVisible(false)
		toDestroy = append(toDestroy, m.elements[i].elem)
	}
	m.elements = nil

	if m.backdrop != nil {
		m.backdrop.SetVisible(false)
		toDestroy = append(toDestroy, m.backdrop)
		m.backdrop = nil
	}

	m.visible = false
	m.pageStack = nil
	m.lastClosedAt = time.Now() // [GHOST-FIX] record close timestamp

	// [HEXMENU-FIX] Destroy after a small delay so we're no longer inside
	// any click handler of the elements being destroyed. In Go WASM,
	// goroutines run on the same thread but yield at time.Sleep, which
	// allows the current JS event handler to return first.
	go func() {
		time.Sleep(50 * time.Millisecond)
		for _, elem := range toDestroy {
			elem.Destroy()
		}
	}()
}

// IsVisible returns whether the menu is open.
func (m *SpriteHexMenu) IsVisible() bool {
	return m.visible
}

// WasJustClosed returns true if Close() was called within the last
// ghostMenuCooldown window.
//
// Devices must check this in addition to IsVisible() to prevent the
// ghost-menu bug: when the user clicks on a device while the menu is open,
// the backdrop (higher z-index) fires Close() before the device's own click
// handler runs.  By the time the device handler fires, IsVisible() is already
// false, causing the device to open a new menu immediately.
//
// Usage inside a device click handler:
//
//	if e.hexMenu.IsVisible() || e.hexMenu.WasJustClosed() {
//	    if e.hexMenu.IsVisible() {
//	        e.hexMenu.Close()
//	    }
//	    return
//	}
func (m *SpriteHexMenu) WasJustClosed() bool {
	return time.Since(m.lastClosedAt) < ghostMenuCooldown
}

// ghostMenuCooldown is the window after Close() during which WasJustClosed()
// returns true.  100ms is large enough to cover the backdrop→device event
// dispatch delay in any browser, but small enough to feel instant to the user.
const ghostMenuCooldown = 100 * time.Millisecond

// --- page rendering ---

func (m *SpriteHexMenu) showPage(items []hexMenu.MenuItem) {
	m.generation++
	gen := fmt.Sprintf("_%d", m.generation)

	// [DENSITY-FIX] .GetFloat() returns density-scaled pixel values automatically.
	// No scaledConfig() copy needed — Density handles scaling internally.
	//
	// Português: .GetFloat() retorna valores em pixels escalados por densidade
	// automaticamente. Não é necessário copiar scaledConfig() — Density lida
	// com a escala internamente.
	radius := m.config.HexRadius.GetFloat()
	spacing := m.config.Spacing.GetFloat()

	// [PLACEMENT-FIX] Recalculate position for each page (submenu, goBack).
	// In centered mode the menu must re-center because each page has a
	// different grid size. In cursor mode this is a no-op (posX/posY stay).
	//
	// Português: Recalcula posição para cada página (submenu, goBack).
	// No modo centralizado o menu deve re-centralizar porque cada página
	// tem um tamanho de grid diferente.
	if Placement == PlacementCentered {
		canvasW, canvasH := m.stage.GetCanvasSize()
		cx := float64(canvasW) / 2
		cy := float64(canvasH) / 2
		w, h := hexMenu.GridBounds(items, radius, spacing)
		m.posX = cx - w/2
		m.posY = cy - h/2
	}

	offX, offY := hexMenu.GridOffset(items, radius, spacing)

	m.elements = make([]spriteHexMenuElem, 0, len(items))

	for _, item := range items {
		if hexMenu.IsStylesEmpty(item.Styles) {
			item.Styles = rulesMainMenu.MenuStyles()
		}

		// Pre-render 5 states. Config carries Density values; RenderHexagonSVG
		// calls .GetFloat() internally.
		//
		// Português: Pré-renderiza 5 estados. Config carrega valores Density;
		// RenderHexagonSVG chama .GetFloat() internamente.
		var caches [5]string
		for s := hexMenu.PipelineState(0); s < 5; s++ {
			caches[s] = hexMenu.RenderHexagonSVG(item, s, &m.config)
		}

		// Initial state
		initialState := hexMenu.PipelineNormal
		if m.tutorialActive && !m.isTutorialTarget(item.ID) {
			initialState = hexMenu.PipelineDisabled
		}

		// Pixel position (screen coordinates, density-scaled via Density).
		// [VIEWPORT-FIX] Use HexSvgWidth/Height (ceiled) so the sprite element
		// matches the SVG viewport dimensions exactly — no scaling mismatch.
		cx, cy := hexMenu.HexCenter(item.Col, item.Row, radius, spacing)
		w := hexMenu.HexSvgWidth(radius)
		h := hexMenu.HexSvgHeight(radius)

		elemX := m.posX + offX + cx - w/2
		elemY := m.posY + offY + cy - h/2

		elem, err := m.stage.CreateElement(sprite.ElementConfig{
			ID:     "hexMenu_" + item.ID + gen,
			X:      elemX,
			Y:      elemY,
			Width:  w,
			Height: h,
			Index:  m.config.ZIndex + 1,
			SvgXml: caches[initialState],
		})
		if err != nil {
			log.Printf("[hexMenu] create element error: %v", err)
			continue
		}

		// [CAMERA-FIX] Screen-space: fixed on screen, ignores camera pan/zoom.
		// Português: Screen-space: fixo na tela, ignora pan/zoom da câmera.
		elem.SetScreenSpace(true)
		elem.SetDragEnable(false)
		elem.SetResizeEnable(false)

		me := spriteHexMenuElem{
			item:   item,
			elem:   elem,
			caches: caches,
		}

		m.wireElemClick(&me)
		m.wireElemCursor(&me)

		m.elements = append(m.elements, me)
	}

	// Start tutorial flash if active
	if m.tutorialActive {
		m.startFlash()
	}
}

func (m *SpriteHexMenu) wireElemClick(me *spriteHexMenuElem) {
	capturedItem := me.item
	me.elem.SetOnClick(func(event sprite.PointerEvent) {
		// [DENSITY-FIX] .GetFloat() returns density-scaled radius for hit-test.
		// [VIEWPORT-FIX] Hex center in SVG is at (svgW/2, svgH/2) after Ceil fix.
		r := m.config.HexRadius.GetFloat()
		hx := hexMenu.HexSvgWidth(r) / 2
		hy := hexMenu.HexSvgHeight(r) / 2
		if !hexMenu.HexContains(hx, hy, r, event.LocalX, event.LocalY) {
			return
		}

		// Tutorial mode: only target item is clickable
		if m.tutorialActive && !m.isTutorialTarget(capturedItem.ID) {
			return
		}

		// GoBack special item
		if capturedItem.ID == "SysGoBack" {
			go m.goBack() // goroutine: goBack→showPage needs main JS thread for SVG
			return
		}

		switch capturedItem.Type {
		case hexMenu.ItemAction:
			if capturedItem.OnClick != nil {
				capturedItem.OnClick()
			}
			if m.tutorialActive {
				go m.advanceTutorial() // goroutine: may call showPage
			} else {
				m.Close()
			}

		case hexMenu.ItemSubmenu:
			if len(capturedItem.Submenu) > 0 {
				if m.tutorialActive {
					go m.advanceTutorial() // goroutine: may call showPage
				} else {
					go m.navigateToSubmenu(capturedItem.Submenu) // goroutine: showPage needs main JS thread
				}
			}
		}
	})
}

func (m *SpriteHexMenu) wireElemCursor(me *spriteHexMenuElem) {
	capturedID := me.item.ID
	me.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		// [DENSITY-FIX] .GetFloat() returns density-scaled radius for cursor hit-test.
		// [VIEWPORT-FIX] Hex center in SVG is at (svgW/2, svgH/2) after Ceil fix.
		r := m.config.HexRadius.GetFloat()
		hx := hexMenu.HexSvgWidth(r) / 2
		hy := hexMenu.HexSvgHeight(r) / 2
		if hexMenu.HexContains(hx, hy, r, lx, ly) {
			if m.tutorialActive && !m.isTutorialTarget(capturedID) {
				return "" // default cursor for disabled items
			}
			return sprite.CursorPointer
		}
		return "" // default
	})
}

// --- navigation ---

func (m *SpriteHexMenu) navigateToSubmenu(submenuItems []hexMenu.MenuItem) {
	// Save current page items
	currentDefs := make([]hexMenu.MenuItem, len(m.elements))
	for i, me := range m.elements {
		currentDefs[i] = me.item
	}
	m.pageStack = append(m.pageStack, currentDefs)

	// Hide current elements and destroy after handler returns
	toDestroy := make([]sprite.Element, len(m.elements))
	for i := range m.elements {
		m.elements[i].elem.SetVisible(false)
		toDestroy[i] = m.elements[i].elem
	}
	m.elements = nil

	go func() {
		time.Sleep(50 * time.Millisecond)
		for _, elem := range toDestroy {
			elem.Destroy()
		}
	}()

	m.showPage(submenuItems)
}

func (m *SpriteHexMenu) goBack() {
	if len(m.pageStack) == 0 {
		m.Close()
		return
	}

	prevItems := m.pageStack[len(m.pageStack)-1]
	m.pageStack = m.pageStack[:len(m.pageStack)-1]

	toDestroy := make([]sprite.Element, len(m.elements))
	for i := range m.elements {
		m.elements[i].elem.SetVisible(false)
		toDestroy[i] = m.elements[i].elem
	}
	m.elements = nil

	go func() {
		time.Sleep(50 * time.Millisecond)
		for _, elem := range toDestroy {
			elem.Destroy()
		}
	}()

	m.showPage(prevItems)
}

// --- backdrop ---

func (m *SpriteHexMenu) createBackdrop() {
	m.generation++
	gen := fmt.Sprintf("_%d", m.generation)

	// [BACKDROP-DIM] Use configurable opacity for the backdrop overlay.
	// Minimum 0.01 so the element is always hit-testable (catches click-outside).
	//
	// Português: Usa opacidade configurável para o overlay do backdrop.
	// Mínimo 0.01 para que o elemento sempre receba hit-test (captura click fora).
	opacity := BackdropOpacity
	if opacity < 0.01 {
		opacity = 0.01
	}
	backdropSVG := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1">`+
			`<rect width="1" height="1" fill="rgba(0,0,0,%.2f)"/></svg>`,
		opacity,
	)

	// [BACKDROP-FIX] Use actual canvas dimensions instead of hardcoded 4000×4000.
	canvasW, canvasH := m.stage.GetCanvasSize()

	var err error
	m.backdrop, err = m.stage.CreateElement(sprite.ElementConfig{
		ID:     "hexMenu_backdrop" + gen,
		X:      0,
		Y:      0,
		Width:  float64(canvasW),
		Height: float64(canvasH),
		Index:  m.config.ZIndex,
		SvgXml: backdropSVG,
	})
	if err != nil {
		log.Printf("[hexMenu] backdrop error: %v", err)
		return
	}

	// [CAMERA-FIX] Screen-space: fixed on screen, ignores camera pan/zoom.
	m.backdrop.SetScreenSpace(true)
	m.backdrop.SetDragEnable(false)
	m.backdrop.SetResizeEnable(false)
	m.backdrop.SetOnClick(func(event sprite.PointerEvent) {
		m.Close()
	})
}

// --- tutorial ---

// StartTutorial opens the menu in tutorial mode.
// Only the target item is clickable; it flashes between Attention1/Attention2.
func (m *SpriteHexMenu) StartTutorial(items []hexMenu.MenuItem, steps []hexMenu.TutorialStep, posX, posY float64) {
	if len(steps) == 0 {
		return
	}
	m.tutorialSteps = steps
	m.tutorialCurrent = 0
	m.tutorialActive = true
	m.Open(items, posX, posY)
	m.startFlash()
}

func (m *SpriteHexMenu) isTutorialTarget(id string) bool {
	if !m.tutorialActive || m.tutorialCurrent >= len(m.tutorialSteps) {
		return false
	}
	return m.tutorialSteps[m.tutorialCurrent].ItemID == id
}

func (m *SpriteHexMenu) advanceTutorial() {
	m.stopFlash()
	m.tutorialCurrent++
	if m.tutorialCurrent >= len(m.tutorialSteps) {
		m.tutorialActive = false
		m.Close()
		return
	}

	// Navigate to correct page for next step
	step := m.tutorialSteps[m.tutorialCurrent]
	if len(step.PagePath) > 0 {
		// Resolve PagePath: walk rootItems → submenu → submenu...
		targetItems := m.resolvePagePath(step.PagePath)
		if targetItems == nil {
			log.Printf("[hexMenu] tutorial: could not resolve PagePath %v", step.PagePath)
			m.tutorialActive = false
			m.Close()
			return
		}

		// Destroy current page elements
		toDestroy := make([]sprite.Element, len(m.elements))
		for i := range m.elements {
			m.elements[i].elem.SetVisible(false)
			toDestroy[i] = m.elements[i].elem
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			for _, el := range toDestroy {
				el.Destroy()
			}
		}()
		m.elements = nil
		m.pageStack = nil

		// Show the resolved submenu page
		m.showPage(targetItems)
	}

	m.startFlash()
}

// resolvePagePath walks through rootItems following the PagePath to find
// the target submenu items. Each element in path is a MenuItem.ID whose
// Submenu field contains the next level of items.
//
// Example: PagePath=["monitorOutput", "monValue"]
//
//	→ find "monitorOutput" in rootItems → get its Submenu
//	→ find "monValue" in that Submenu → get its Submenu
func (m *SpriteHexMenu) resolvePagePath(path []string) []hexMenu.MenuItem {
	current := m.rootItems
	for _, id := range path {
		found := false
		for _, item := range current {
			if item.ID == id {
				if len(item.Submenu) == 0 {
					return nil // path element has no submenu
				}
				current = item.Submenu
				found = true
				break
			}
		}
		if !found {
			return nil // path element not found
		}
	}
	return current
}

func (m *SpriteHexMenu) startFlash() {
	m.stopFlash()
	if !m.tutorialActive || m.tutorialCurrent >= len(m.tutorialSteps) {
		return
	}

	targetID := m.tutorialSteps[m.tutorialCurrent].ItemID

	// Find the target element NOW and capture a direct reference.
	var targetElem sprite.Element
	var cache1, cache2 string
	for i := range m.elements {
		if m.elements[i].item.ID == targetID {
			targetElem = m.elements[i].elem
			cache1 = m.elements[i].caches[hexMenu.PipelineAttention1]
			cache2 = m.elements[i].caches[hexMenu.PipelineAttention2]
			break
		}
	}
	if targetElem == nil {
		log.Printf("[hexMenu] startFlash: target %q not found in elements", targetID)
		return
	}

	m.flashTicker = time.NewTicker(500 * time.Millisecond)
	m.flashDone = make(chan struct{})

	go func() {
		state := true
		for {
			select {
			case <-m.flashDone:
				return
			case <-m.flashTicker.C:
				if state {
					_ = targetElem.CacheFromSvg(cache1)
				} else {
					_ = targetElem.CacheFromSvg(cache2)
				}
				state = !state
			}
		}
	}()
}

func (m *SpriteHexMenu) stopFlash() {
	if m.flashTicker != nil {
		m.flashTicker.Stop()
		m.flashTicker = nil
	}
	if m.flashDone != nil {
		close(m.flashDone)
		m.flashDone = nil
	}
}
