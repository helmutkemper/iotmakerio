// /ide/devices/compLoop/statementLoop.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compLoop

// statementLoop.go — Loop container device.
//
// English:
//
//	A Complex device (kind = KindComplex) that provides a visual "for"
//	loop on the stage. The ornament is drawn by doubleLoopArrow and
//	carries a stop button (a bool-typed input connector) in the bottom-
//	right corner. Children placed inside the loop's border 3 become
//	statements of the loop body; codegen emits them inside a for {}.
//
//	This file wires the Loop into the scenegraph:
//	  - Declares KindComplex via the Kinded interface.
//	  - Declares its Padded container padding (LoopPadding).
//	  - Translates sprite drag/resize events to graph lifecycle calls
//	    so the scenegraph can move children rigidly, clamp resize, and
//	    raise conflicts in real time.
//	  - Subscribes to its own conflict events so the warning mark
//	    lights up the instant a child straddles the loop's border 3 or
//	    another device overlaps its border 1.
//
//	Responsibilities OUT of this file:
//	  - Containment math → scenegraph + rulesContainer.
//	  - Conflict detection → scenegraph.
//	  - JSON export → scene.Serializer.
//
// Português:
//
//	Device Complex (KindComplex) que representa um "for" visual na
//	stage. O desenho é feito por doubleLoopArrow e tem um botão de
//	parada (conector bool) no canto inferior-direito. Filhos colocados
//	na border 3 viram statements do corpo do loop.

import (
	"fmt"
	"log"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/connection"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/hexagon"
	"github.com/helmutkemper/iotmakerio/ornament/doubleLoopArrow"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesContainer"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
	"github.com/helmutkemper/iotmakerio/scene"
	"github.com/helmutkemper/iotmakerio/scenegraph"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
	"github.com/helmutkemper/iotmakerio/utilsDraw"
	"github.com/helmutkemper/iotmakerio/utilsText"
	"github.com/helmutkemper/iotmakerio/wire"
)

type StatementLoop struct {
	// Sprite/DOM ----------------------------------------------------
	stage sprite.Stage
	elem  sprite.Element

	// Local state (formerly inside block.Block) ---------------------
	name         string
	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool
	// [COMMENT] user comment — appears as `// ` lines above this container's
	// statement in the generated code and in the container's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste container no código gerado e no tooltip de hover.
	comment string
	width   rulesDensity.Density
	height  rulesDensity.Density

	// Pending state applied at the end of Init() --------------------
	pendingResizeEnable *bool
	pendingDragEnable   *bool
	pendingSelected     *bool
	resizerButton       block.ResizeButton

	// Shared hex menu instance --------------------------------------
	// [CTXMENU] linear context menu controller.
	ctxMenu *contextMenu.Controller

	// Wire manager for stop port ------------------------------------
	wireMgr *wire.Manager

	debugSelected bool

	// Size defaults and constraints ---------------------------------
	defaultWidth          rulesDensity.Density
	defaultHeight         rulesDensity.Density
	horizontalMinimumSize rulesDensity.Density
	verticalMinimumSize   rulesDensity.Density

	// Ornament drawings (body + icon) -------------------------------
	ornamentDraw     *doubleLoopArrow.DoubleLoopArrow
	ornamentDrawIcon *doubleLoopArrow.DoubleLoopArrow

	id         string
	debugMode  bool
	gridAdjust grid.Adjust
	iconStatus int

	// Scene integration ---------------------------------------------
	sceneMgr    *scene.Serializer
	sceneNotify func()
	onRemove    func(id string)

	// Canvas DOM element (for HTML overlay coordinate transforms) ---
	canvasEl js.Value

	// Dashed overlay shown during resize — drawn at the minimum
	// containment area so the user can see why the handle can't go
	// further.
	childBoundsDiv js.Value

	// Cached rectangles during an active resize gesture. Populated in
	// SetOnResizeStart, consumed in SetOnResizeMove/End, cleared in
	// SetOnResizeEnd. Using a *rulesContainer.Rect keeps the type
	// identical to what rulesContainer.ClampResize takes.
	resizeChildBounds *rulesContainer.Rect
	resizeParentInner *rulesContainer.Rect

	// dragStartPos remembers where the Loop's top-left was when the
	// drag began, so at EndDrag we can compute the total (dx, dy) and
	// hand it to the scenegraph to translate descendants.
	dragStartX, dragStartY float64
}

// =====================================================================
//  Dependency injection — called by the factory before Init()
// =====================================================================

func (e *StatementLoop) SetStage(stage sprite.Stage)       { e.stage = stage }
func (e *StatementLoop) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementLoop) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementLoop) SetWireManager(mgr *wire.Manager)       { e.wireMgr = mgr }
func (e *StatementLoop) SetCanvasEl(el js.Value)                { e.canvasEl = el }
func (e *StatementLoop) SetResizerButton(rb block.ResizeButton) { e.resizerButton = rb }
func (e *StatementLoop) SetDraggerButton(_ block.ResizeButton)  {}
func (e *StatementLoop) SetGridAdjust(ga grid.Adjust)           { e.gridAdjust = ga }
func (e *StatementLoop) SetOnRemove(fn func(id string))         { e.onRemove = fn }

// SetSceneNotify installs the scene-change callback fired at the end of
// any interactive gesture. The workspace wires this to
// sceneMgr.NotifyChange.
func (e *StatementLoop) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// =====================================================================
//  Lifecycle
// =====================================================================

func (e *StatementLoop) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

// Remove destroys the Loop: its visual elements, its wire registrations,
// its z-index entry, and its scenegraph subscription.
func (e *StatementLoop) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	devices.BackendZRegistry.Unregister(e.id)
	if e.elem != nil {
		e.elem.SetVisible(false)
		elem := e.elem
		e.elem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
}

// =====================================================================
//  Scenegraph interface — Kinded + Padded
// =====================================================================

// GetKind tells the scenegraph this is a container. Satisfies
// scene.Kinded.
func (e *StatementLoop) GetKind() scenegraph.Kind { return scenegraph.KindComplex }

// GetContainerPadding returns the padding used to derive border 3 from
// border 1. Satisfies scene.Padded.
func (e *StatementLoop) GetContainerPadding() rulesContainer.Padding {
	return rulesContainer.LoopPadding()
}

// =====================================================================
//  Scene geometry interface
// =====================================================================

func (e *StatementLoop) GetDeviceType() string { return "StatementLoop" }

func (e *StatementLoop) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

// GetInnerBBox returns the internal usable area — border 3 — derived
// from border 1 by applying LoopPadding. Single source of truth for
// Loop's padding lives in rulesContainer.
func (e *StatementLoop) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	pad := rulesContainer.LoopPadding()
	return &scene.Rect{
		X:      x + pad.Left,
		Y:      y + pad.Top,
		Width:  w - pad.Left - pad.Right,
		Height: h - pad.Top - pad.Bottom,
	}
}

// MoveBy shifts the Loop's outer rectangle by (dx, dy). Called by the
// scenegraph when an ancestor container is being dragged — not when
// the Loop itself is being dragged (the sprite handles that directly).
func (e *StatementLoop) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// RefreshVisual regenerates the SVG ornament and recalculates wire
// positions. Called by the import system after SetSize so the visual
// matches the restored dimensions.
func (e *StatementLoop) RefreshVisual() {
	e.recacheOrnament()
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// =====================================================================
//  Basic getters/setters
// =====================================================================

func (e *StatementLoop) Get() (container *html.TagSvg) { return nil }

func (e *StatementLoop) SetFatherId(_ string) {}

func (e *StatementLoop) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}

func (e *StatementLoop) GetID() string   { return e.id }
func (e *StatementLoop) GetName() string { return e.name }

func (e *StatementLoop) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}

func (e *StatementLoop) SetSize(w, h rulesDensity.Density) {
	e.width = w
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(w, h)
	}
}

func (e *StatementLoop) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}

func (e *StatementLoop) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}

func (e *StatementLoop) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}

func (e *StatementLoop) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}

func (e *StatementLoop) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		y := e.elem.GetYD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}

func (e *StatementLoop) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		x := e.elem.GetXD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}

func (e *StatementLoop) SetWidth(width rulesDensity.Density) {
	e.width = width
	if e.elem != nil {
		h := e.elem.GetHeightD()
		newW, newH := e.gridAdjust.AdjustCenterD(width, h)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementLoop) SetHeight(height rulesDensity.Density) {
	e.height = height
	if e.elem != nil {
		w := e.elem.GetWidthD()
		newW, newH := e.gridAdjust.AdjustCenterD(w, height)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementLoop) SetSelected(selected bool) {
	if e.selectLocked {
		e.selected = false
		return
	}
	e.selected = selected
	if e.elem == nil {
		e.pendingSelected = &selected
		return
	}
	if selected {
		e.elem.SetDragEnable(false)
		e.dragEnabled = false
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		e.ornamentDraw.SetSelected(true)
		go e.recacheOrnament()
	} else {
		e.ornamentDraw.SetSelected(false)
		go e.recacheOrnament()
	}
}

func (e *StatementLoop) SetDragEnable(enabled bool) {
	if e.dragLocked {
		e.dragEnabled = false
		return
	}
	e.dragEnabled = enabled
	if e.elem == nil {
		e.pendingDragEnable = &enabled
		return
	}
	if enabled {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		e.selected = false
		e.elem.SetDragEnable(true)
	} else {
		e.elem.SetDragEnable(false)
	}
}

func (e *StatementLoop) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

// SetResizeEnable toggles resize mode. When entering resize on a Loop
// that is nested inside another Complex, the sprite's built-in max
// size is set to the parent's inner bbox so the Loop can't grow past
// its container. This uses the scenegraph's ParentInnerBBox query.
func (e *StatementLoop) SetResizeEnable(enabled bool) {
	if e.resizeLocked {
		return
	}
	if e.elem == nil {
		e.pendingResizeEnable = &enabled
		return
	}
	if enabled {
		e.elem.SetDragEnable(false)
		e.dragEnabled = false
		e.selected = false

		if e.sceneMgr != nil {
			parentInner := e.sceneMgr.ParentInnerBBox(e.id)
			if parentInner != nil {
				elemX, elemY := e.elem.GetPosition()
				maxW := (parentInner.X + parentInner.Width) - elemX
				maxH := (parentInner.Y + parentInner.Height) - elemY
				if maxW < 0 {
					maxW = 0
				}
				if maxH < 0 {
					maxH = 0
				}
				e.elem.SetMaxSize(maxW, maxH)
			}
		}

		e.elem.SetResizeEnable(true)
		e.elem.ShowResizeButtons(true)
	} else {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		e.elem.SetMaxSize(0, 0)
	}
}

func (e *StatementLoop) GetInitialized() bool { return e.initialized }
func (e *StatementLoop) GetDragBlocked() bool { return e.dragLocked }
func (e *StatementLoop) GetDragEnable() bool  { return e.dragEnabled }
func (e *StatementLoop) GetResize() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementLoop) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementLoop) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementLoop) GetSelected() bool      { return e.selected }
func (e *StatementLoop) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}

func (e *StatementLoop) onConnectionClick() {}

// =====================================================================
//  Init — build the sprite, wire events, register connectors
// =====================================================================

// Init — NOTE: blocks during image caching. MUST be called from a
// goroutine, not the main thread.
func (e *StatementLoop) Init() error {
	if e.stage == nil {
		log.Println("Error: SetStage() must be called before Init()")
		return nil
	}

	_ = hexagon.KStageId // keep import reference

	if e.name == "" {
		e.SetName("stmLoop")
	}

	e.defaultWidth = 400
	e.defaultHeight = 300
	e.defaultWidth, e.defaultHeight = e.gridAdjust.AdjustCenterD(e.defaultWidth, e.defaultHeight)

	e.horizontalMinimumSize = 150
	e.verticalMinimumSize = 150
	e.horizontalMinimumSize, e.verticalMinimumSize = e.gridAdjust.AdjustCenterD(e.horizontalMinimumSize, e.verticalMinimumSize)

	if e.width == 0 || e.height == 0 {
		e.width = e.defaultWidth
		e.height = e.defaultHeight
	}

	e.id = rulesSequentialId.GetIdFromBase(e.name)

	e.ornamentDraw = new(doubleLoopArrow.DoubleLoopArrow)
	e.ornamentDrawIcon = new(doubleLoopArrow.DoubleLoopArrow)

	stopButton := connection.Setup{
		FatherId:           e.id,
		Name:               "stopButton",
		DataType:           "bool",
		AcceptNotConnected: true,
		LookedUp:           false,
		IsADataInput:       true,
		ClickFunc: js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			data := this.Call("getConnData")
			log.Printf("FatherId: %v", data.Get("FatherId").String())
			log.Printf("Name: %v", data.Get("Name").String())
			log.Printf("DataType: %v", data.Get("DataType").String())
			return nil
		}),
	}
	if err := stopButton.Verify(); err != nil {
		log.Printf("stopButton.Verify: %v", err)
		return err
	}

	e.ornamentDraw.StopButtonSetup(stopButton)
	if err := e.ornamentDraw.GetConnectionError(); err != nil {
		return err
	}
	_ = e.ornamentDraw.Init()
	_ = e.ornamentDrawIcon.Init()

	// Build sprite element from the ornament SVG.
	_ = e.ornamentDraw.Update(0, 0, e.width, e.height)
	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", e.width.GetInt())
	ornamentSvg.Call("setAttribute", "height", e.height.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)

	var err error
	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:         e.id,
		X:          0,
		Y:          0,
		Width:      e.width.GetFloat(),
		Height:     e.height.GetFloat(),
		Index:      1,
		DragEnable: false,
		SvgXml:     ornamentXml,
	})
	if err != nil {
		log.Printf("Failed to create sprite element: %v", err)
		return err
	}

	e.elem.SetMinSizeD(e.horizontalMinimumSize, e.verticalMinimumSize)
	devices.BackendZRegistry.Register(e.id, e.elem)

	// Resize handle buttons (via template adapter).
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		if setErr := e.elem.SetResizeButtons(adapter); setErr != nil {
			log.Printf("[SPRITE] ERROR: SetResizeButtons failed: %v", setErr)
		}
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}

	// Sprite → local state event wiring.
	e.wireEvents()

	// Warning mark element (starts hidden).

	e.initialized = true

	// Apply any state queued before Init().
	if e.pendingSelected != nil {
		e.SetSelected(*e.pendingSelected)
		e.pendingSelected = nil
	}
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}
	if e.pendingResizeEnable != nil {
		e.SetResizeEnable(*e.pendingResizeEnable)
		e.pendingResizeEnable = nil
	}

	return nil
}

// =====================================================================
//  Hex menu items
// =====================================================================

// getBodyMenuItems returns body context menu items for this container.
// Order: Delete first (canonical per D4), Inspect, Resize toggle,
// Forward/Backward z-ordering.
//
// Português: Itens do menu de contexto do corpo.
// Ordem canonizada: Delete, Inspect, Resize, Forward, Backward.
func (e *StatementLoop) getBodyMenuItems() []contextMenu.Item {
	resizeLabel := translate.T("menuDeviceResize", "Resize")
	resizeHelp := "Toggles corner handles so you can drag to resize the container."
	resizeAction := func() {
		e.resizeLocked = false
		e.SetResizeEnable(true)
	}
	if e.GetResizeEnable() {
		resizeLabel = translate.T("menuDeviceResizeCancel", "Lock Size")
		resizeHelp = "Locks the current size and hides the corner handles."
		resizeAction = func() { e.SetResizeEnable(false) }
	}

	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Loop] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			go e.showInspectOverlay()
		}),
		{
			ID:              "resize",
			Label:           resizeLabel,
			FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight,
			ViewBox:         "0 0 512 512",
			HelpFallback:    resizeHelp,
			OnClick:         resizeAction,
		},
		{
			ID:              "forward",
			Label:           translate.T("menuDeviceBringForward", "Forward"),
			FontAwesomePath: rulesIcon.KFAPlus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Brings this container above overlapping devices.",
			OnClick:         func() { devices.BackendZRegistry.MoveForward(e.id) },
		},
		{
			ID:              "backward",
			Label:           translate.T("menuDeviceSendBackward", "Backward"),
			FontAwesomePath: rulesIcon.KFAMinus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Sends this container below overlapping devices.",
			OnClick:         func() { devices.BackendZRegistry.MoveBackward(e.id) },
		},
	}
}

// =====================================================================
//  Child bounds overlay — dashed rectangle shown during resize
// =====================================================================

func (e *StatementLoop) showChildBoundsOverlay(bounds rulesContainer.Rect) {
	if e.canvasEl.IsUndefined() || e.canvasEl.IsNull() {
		return
	}
	margin := rulesContainer.LoopChildMargin

	innerX := bounds.X - margin
	innerY := bounds.Y - margin
	innerW := bounds.W + 2*margin
	innerH := bounds.H + 2*margin

	cam := e.stage.GetCamera()
	zoom := 1.0
	offsetX, offsetY := 0.0, 0.0
	if cam != nil {
		zoom = cam.Zoom
		if zoom <= 0 {
			zoom = 1
		}
		offsetX = cam.OffsetX
		offsetY = cam.OffsetY
	}

	cpxX := (innerX - offsetX) * zoom
	cpxY := (innerY - offsetY) * zoom
	cpxW := innerW * zoom
	cpxH := innerH * zoom

	rect := e.canvasEl.Call("getBoundingClientRect")
	canvasW := e.canvasEl.Get("width").Float()
	canvasH := e.canvasEl.Get("height").Float()
	cssScaleX := rect.Get("width").Float() / canvasW
	cssScaleY := rect.Get("height").Float() / canvasH

	screenX := rect.Get("left").Float() + cpxX*cssScaleX
	screenY := rect.Get("top").Float() + cpxY*cssScaleY
	screenW := cpxW * cssScaleX
	screenH := cpxH * cssScaleY

	doc := js.Global().Get("document")
	div := doc.Call("createElement", "div")
	s := div.Get("style")
	s.Set("position", "fixed")
	s.Set("left", fmt.Sprintf("%.0fpx", screenX))
	s.Set("top", fmt.Sprintf("%.0fpx", screenY))
	s.Set("width", fmt.Sprintf("%.0fpx", screenW))
	s.Set("height", fmt.Sprintf("%.0fpx", screenH))
	s.Set("border", "2px dashed rgba(255, 100, 100, 0.8)")
	s.Set("borderRadius", "4px")
	s.Set("background", "rgba(255, 100, 100, 0.05)")
	s.Set("pointerEvents", "none")
	s.Set("zIndex", "9999")
	s.Set("boxSizing", "border-box")

	doc.Get("body").Call("appendChild", div)
	e.childBoundsDiv = div
}

func (e *StatementLoop) hideChildBoundsOverlay() {
	if e.childBoundsDiv.IsUndefined() || e.childBoundsDiv.IsNull() {
		return
	}
	doc := js.Global().Get("document")
	doc.Get("body").Call("removeChild", e.childBoundsDiv)
	e.childBoundsDiv = js.Undefined()
}

// =====================================================================
//  Sprite event wiring
// =====================================================================

// wireEvents connects sprite events (click, drag, resize) to local
// state handlers AND to the scenegraph lifecycle.
//
// Drag flow (container drag):
//   - DragStart → sceneMgr.BeginDrag(id) (graph snapshots descendants)
//   - DragMove  → sceneMgr.UpdateDrag(id) (graph refreshes geometry,
//     fires conflicts in real time)
//   - DragEnd   → sceneMgr.EndDrag(id, dx, dy) (graph moves descendants
//     rigidly, reassigns parentage if clean)
//
// Resize flow:
//   - ResizeStart → cache child bounds and parent inner; show overlay;
//     sceneMgr.BeginResize
//   - ResizeMove  → clamp against child bounds (minimum size)
//   - ResizeEnd   → grid-snap; clamp against both bounds; sceneMgr.EndResize
func (e *StatementLoop) wireEvents() {
	// -----------------------------------------------------------------
	//  Click — hit-test the stop button; otherwise open the hex menu.
	// -----------------------------------------------------------------
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		w, h := e.elem.GetSize()
		// [PIN] standard pin hit box on the INTERIOR stop-button pin — the
		// left-facing pin whose outer tip sits at the historical anchor
		// (w-57, h-42); the body-side point is tip + PinLength.
		// Português: Caixa de clique do pino INTERNO da botoeira — o pino
		// voltado para a esquerda com a ponta externa no anchor histórico
		// (w-57, h-42); o ponto do lado do corpo é ponta + PinLength.
		stopY := h - 42
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			w-57+rulesConnection.PinLength(), stopY,
			event.LocalX, event.LocalY) {
			if e.ctxMenu != nil && e.wireMgr != nil {
				elemX, elemY := e.elem.GetPosition()
				menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
				go e.ctxMenu.OpenAtWorld(
					mainMenu.ConnectorMenu(e.wireMgr, e.id, "stop"),
					menuX, menuY,
				)
			}
			return
		}
		if e.ctxMenu == nil {
			return
		}
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
		go e.ctxMenu.OpenForDevice(e, e.getBodyMenuItems(), menuX, menuY)
	})

	// -----------------------------------------------------------------
	//  Drag — hand the gesture over to the scenegraph.
	// -----------------------------------------------------------------
	e.elem.SetOnDragStart(func(event sprite.DragEvent) {
		e.dragStartX, e.dragStartY = e.elem.GetPosition()
		if e.sceneMgr != nil {
			e.sceneMgr.BeginDrag(e.id)
		}
	})

	// DragMove fires on every frame while the mouse is held. We use it
	// to give the graph a chance to re-check conflicts in real time,
	// so red borders pop on/off against the devices the Loop is
	// passing over as the user drags.
	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.UpdateDrag(e.id)
		}
	})

	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		// Grid snap the Loop's final position.
		x, y := e.elem.GetPositionD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)

		// Compute the total delta of the gesture and hand it to the
		// scenegraph, which translates every descendant by (dx, dy)
		// and reassigns parentage if conflicts are clean.
		finalX, finalY := e.elem.GetPosition()
		dx, dy := rulesContainer.DragChildDelta(e.dragStartX, e.dragStartY, finalX, finalY)
		if e.sceneMgr != nil {
			e.sceneMgr.EndDrag(e.id, dx, dy)
		}

		// The Loop's own stop-port wire needs re-routing after the move.
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// -----------------------------------------------------------------
	//  Resize — cache floor (children bounds) and ceiling (parent
	//  inner); clamp during move; apply both clamps at end.
	// -----------------------------------------------------------------
	e.elem.SetOnResizeStart(func(event sprite.ResizeEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.BeginResize(e.id)
		}

		e.resizeChildBounds = nil
		if e.sceneMgr != nil {
			if b := e.sceneMgr.ChildrenBounds(e.id); b != nil {
				cb := rulesContainer.Rect{X: b.X, Y: b.Y, W: b.Width, H: b.Height}
				e.resizeChildBounds = &cb
				e.showChildBoundsOverlay(cb)
			}
		}

		e.resizeParentInner = nil
		if e.sceneMgr != nil {
			if p := e.sceneMgr.ParentInnerBBox(e.id); p != nil {
				pi := rulesContainer.Rect{X: p.X, Y: p.Y, W: p.Width, H: p.Height}
				e.resizeParentInner = &pi
			}
		}
	})

	e.elem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		// Only the minimum-size clamp (children floor) runs in move.
		// The maximum-size clamp (parent ceiling) runs at end — doing
		// it mid-move fights the sprite's internal size tracking and
		// makes the element jump to the parent's maximum.
		if e.resizeChildBounds != nil {
			loopX, loopY := e.elem.GetPosition()
			loopW, loopH := e.elem.GetSize()
			proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: loopW, H: loopH}
			pad := rulesContainer.LoopPadding()
			clamped := rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)

			if clamped.X != loopX || clamped.Y != loopY {
				e.elem.SetPosition(clamped.X, clamped.Y)
			}
			if clamped.W != loopW || clamped.H != loopH {
				e.elem.SetSize(clamped.W, clamped.H)
			}
		}
	})

	e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		e.hideChildBoundsOverlay()

		// Grid snap the new size.
		wD, hD := e.elem.GetSizeD()
		newW, newH := e.gridAdjust.AdjustCenterD(wD, hD)
		loopX, loopY := e.elem.GetPosition()
		proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: newW.GetFloat(), H: newH.GetFloat()}

		// Minimum clamp: can't shrink past children.
		if e.resizeChildBounds != nil {
			pad := rulesContainer.LoopPadding()
			proposed = rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)
		}

		// Maximum clamp: can't grow past parent container.
		if e.resizeParentInner != nil {
			proposed = rulesContainer.ClampToParent(proposed, *e.resizeParentInner)
		}

		e.resizeChildBounds = nil
		e.resizeParentInner = nil

		e.elem.SetPosition(proposed.X, proposed.Y)
		newW = rulesDensity.Density(proposed.W)
		newH = rulesDensity.Density(proposed.H)
		e.elem.SetSizeD(newW, newH)
		e.width = newW
		e.height = newH

		go e.recacheOrnament()

		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		if e.sceneMgr != nil {
			e.sceneMgr.EndResize(e.id)
		}

		// Exit resize mode; re-enable drag automatically. Without
		// this, the element stays in resize mode after the handle is
		// released, making it undraggable.
		e.SetResizeEnable(false)
		e.SetDragEnable(true)

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// Periodic resize redraw while the handle is held.
	e.elem.SetResizeRedrawInterval(1000)
	e.elem.SetOnResizeRedraw(func(event sprite.ResizeEvent) {
		go e.recacheOrnament()
	})

	// Cursor hit-test: pointer only over the stop button; default
	// cursor everywhere else (drag is the default).
	e.elem.SetCursorHitTest(func(localX, localY float64) sprite.CursorStyle {
		w, h := e.elem.GetSize()
		stopY := h - 42
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			w-57+rulesConnection.PinLength(), stopY, localX, localY) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// =====================================================================
//  Rendering helpers
// =====================================================================

func (e *StatementLoop) recacheOrnament() {
	if e.elem == nil || e.ornamentDraw == nil {
		return
	}
	wD, hD := e.elem.GetSizeD()
	_ = e.ornamentDraw.Update(0, 0, wD, hD)

	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", wD.GetInt())
	ornamentSvg.Call("setAttribute", "height", hD.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)
	_ = e.elem.CacheFromSvg(ornamentXml)
}

// =====================================================================
//  Icon (for hardware menu / palette)
// =====================================================================

func (e *StatementLoop) GetIconName() string     { return "Loop" }
func (e *StatementLoop) GetIconCategory() string { return "Loop" }
func (e *StatementLoop) SetStatus(status int)    { e.iconStatus = status }
func (e *StatementLoop) GetStatus() int          { return e.iconStatus }

func (e *StatementLoop) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)

	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).
		Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).
		Height(rulesIcon.Height.GetInt())

	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).
		Stroke(data.ColorBorder).
		Fill(data.ColorBackground).
		D(hexPath)

	xc := rulesIcon.Width / 4
	yc := rulesIcon.Height * 0.15
	wOrn := rulesIcon.Width / 2
	icon := e.ornamentDrawIcon.GetSvg().
		X(xc.GetInt()).
		Y(yc.GetInt())
	_ = e.ornamentDrawIcon.Update(0, 0, wOrn, wOrn)

	widthLabel, _ := utilsText.GetTextSize(
		data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight,
		rulesIcon.FontStyle, data.LabelFontSize.GetInt(),
	)
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).
		FontWeight(rulesIcon.FontWeight).
		FontStyle(rulesIcon.FontStyle).
		FontSize(data.LabelFontSize.GetInt()).
		Text(data.Label).
		Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - widthLabel/2).
		Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, icon, label)

	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}

// =====================================================================
//  Wire connectors
// =====================================================================

// RegisterConnectors registers the stop port as a wire input. true on
// the stop port breaks the loop.
func (e *StatementLoop) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "stop"},
		IsOutput:           false,
		AllowedTypes:       []string{"bool"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     1,
		Label:              "Stop",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			w, h := e.elem.GetSize()
			// Stop button position matches DoubleLoopArrow.Update().
			// The wire anchors at the interior pin's outer tip — exactly
			// the historical (w-57, h-42) the project always used.
			// Português: O fio ancora na ponta externa do pino interno —
			// exatamente o (w-57, h-42) histórico do projeto.
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				w-57+rulesConnection.PinLength(), h-42)
			return ex + ax, ey + ay
		},
	})

	// Register the container's live rect so the wire manager can draw a
	// LabVIEW-style tunnel marker where a wire crosses this container's border,
	// matching StatementCase. The closure returns the current geometry so the
	// marker tracks drags and resizes; a nil element yields a zero rect, which
	// the renderer skips. The DoubleLoopArrow border is drawn inset by
	// margin = Density(10) — the same inset caseBorder uses — so the tunnel sits
	// on that visible border line, not on the outer (mathematical) bounding box.
	// This is frontend-only: the tunnel is an overlay on an existing wire and
	// changes nothing in codegen, the serializer, or persistence.
	//
	// Português: Registra o rect vivo do container para o wire manager desenhar
	// o marcador de túnel (estilo LabVIEW) onde um fio cruza a borda, igual ao
	// StatementCase. O closure devolve a geometria atual para o marcador
	// acompanhar arrasto e resize; elemento nil dá rect zero, que o renderer
	// ignora. A borda DoubleLoopArrow é desenhada com inset margin = Density(10)
	// (o mesmo do caseBorder), então o túnel senta nessa linha visível, não na
	// bbox externa. É só frontend: o túnel é overlay sobre um fio existente e não
	// muda nada no codegen, no serializer nem na persistência.
	e.wireMgr.RegisterContainer(e.id, func() (x, y, w, h float64) {
		if e.elem == nil {
			return 0, 0, 0, 0
		}
		ex, ey := e.elem.GetPosition()
		ew, eh := e.elem.GetSize()
		m := rulesDensity.Density(10).GetFloat()
		return ex + m, ey + m, ew - 2*m, eh - 2*m
	})
}

// showInspectOverlay opens the container's Inspect: a Properties tab whose
// only editable datum is the universal COMMENT (containers have no other
// form-editable state — membership and geometry live on the stage).
// Português: Abre o Inspect do container: aba Properties cujo único dado
// editável é o COMENTÁRIO universal (containers não têm outro estado
// editável por formulário — membros e geometria vivem no stage).
func (e *StatementLoop) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementLoop) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:         "comment",
						Label:       translate.T("propComment", "Comment"),
						Type:        overlay.FieldTextarea,
						Value:       e.comment,
						Placeholder: translate.T("propCommentPlaceholder", "Comment shown in generated code..."),
						Rows:        3,
					},
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id, ReadOnly: true},
				},
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

// ApplyProperties restores the comment — the container's only form-editable
// datum. Membership and size are restored by the scene import machinery.
// Português: Restaura o comentário — único dado editável por formulário do
// container. Membros e tamanho são restaurados pelo import da cena.
func (e *StatementLoop) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
}

// GetProperties exports the container's scene properties. Until the comment
// existed, loops exported none — which is why this function is new: the
// serializer collects it via interface assertion, so its presence alone
// makes the comment flow scene → graph → IR (the LOOP_BEGIN stamp).
// Português: Exporta as properties de cena do container. Até o comentário
// existir, loops não exportavam nenhuma — por isso esta função é nova: o
// serializer a coleta por assertion, então a presença dela já faz o
// comentário fluir cena → graph → IR (o carimbo do LOOP_BEGIN).
func (e *StatementLoop) GetProperties() map[string]interface{} {
	props := map[string]interface{}{}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// container's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do container.
func (e *StatementLoop) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementLoop) SetComment(c string) { e.comment = c }
