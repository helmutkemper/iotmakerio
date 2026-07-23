// /ide/devices/compFlow/statementIfElse.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// statementIfElse.go — If/Else container device.
//
// A Complex device (kind = KindComplex) that splits its children into
// two branches ("true" / "false") governed by a bool input on the
// left edge. Integrates with the scenegraph the same way the Loop does
// (BeginDrag/UpdateDrag/EndDrag).
//
// Branch management (trueBranchIDs, falseBranchIDs, toggleBranch,
// assignNewChildren, applyBranchVisibility) is orthogonal to the
// containment rule engine and lives entirely inside this file.

import (
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
	"github.com/helmutkemper/iotmakerio/ornament/ifElseBorder"
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

type StatementIfElse struct {
	stage sprite.Stage
	elem  sprite.Element

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

	pendingResizeEnable *bool
	pendingDragEnable   *bool
	pendingSelected     *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller.
	ctxMenu *contextMenu.Controller
	wireMgr *wire.Manager

	defaultWidth          rulesDensity.Density
	defaultHeight         rulesDensity.Density
	horizontalMinimumSize rulesDensity.Density
	verticalMinimumSize   rulesDensity.Density

	ornamentDraw     *ifElseBorder.IfElseBorder
	ornamentDrawIcon *ifElseBorder.IfElseBorder

	id         string
	gridAdjust grid.Adjust
	iconStatus int

	sceneNotify func()
	onRemove    func(id string)
	sceneMgr    *scene.Serializer
	canvasEl    js.Value

	resizeChildBounds *rulesContainer.Rect
	resizeParentInner *rulesContainer.Rect

	dragStartX, dragStartY float64

	// Branch management
	selectedBranch string
	trueBranchIDs  []string
	falseBranchIDs []string
}

// ── Dependency injection ─────────────────────────────────────────────

func (e *StatementIfElse) SetStage(stage sprite.Stage)       { e.stage = stage }
func (e *StatementIfElse) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementIfElse) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementIfElse) SetWireManager(mgr *wire.Manager)       { e.wireMgr = mgr }
func (e *StatementIfElse) SetCanvasEl(el js.Value)                { e.canvasEl = el }
func (e *StatementIfElse) SetResizerButton(rb block.ResizeButton) { e.resizerButton = rb }
func (e *StatementIfElse) SetDraggerButton(_ block.ResizeButton)  {}
func (e *StatementIfElse) SetGridAdjust(ga grid.Adjust)           { e.gridAdjust = ga }
func (e *StatementIfElse) SetOnRemove(fn func(id string))         { e.onRemove = fn }

// SetSceneNotify wraps the workspace callback so every scene change
// also re-evaluates branch assignment.
func (e *StatementIfElse) SetSceneNotify(fn func()) {
	e.sceneNotify = func() {
		e.assignNewChildren()
		e.applyBranchVisibility()
		if fn != nil {
			fn()
		}
	}
}

// ── Lifecycle ────────────────────────────────────────────────────────

func (e *StatementIfElse) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementIfElse) Remove() {
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

// ── Scenegraph interface — Kinded + Padded ───────────────────────────

func (e *StatementIfElse) GetKind() scenegraph.Kind { return scenegraph.KindComplex }

func (e *StatementIfElse) GetContainerPadding() rulesContainer.Padding {
	return rulesContainer.IfElsePadding()
}

// ── Init ─────────────────────────────────────────────────────────────

func (e *StatementIfElse) Init() (err error) {
	if e.stage == nil {
		log.Println("Error: SetStage() must be called before Init()")
		return
	}

	_ = hexagon.KStageId

	if e.name == "" {
		e.SetName("stmIfElse")
	}

	e.defaultWidth = 400
	e.defaultHeight = 300
	e.defaultWidth, e.defaultHeight = e.gridAdjust.AdjustCenterD(e.defaultWidth, e.defaultHeight)

	e.horizontalMinimumSize = 200
	e.verticalMinimumSize = 150
	e.horizontalMinimumSize, e.verticalMinimumSize = e.gridAdjust.AdjustCenterD(e.horizontalMinimumSize, e.verticalMinimumSize)

	if e.width == 0 || e.height == 0 {
		e.width = e.defaultWidth
		e.height = e.defaultHeight
	}

	e.id = rulesSequentialId.GetIdFromBase(e.name)

	if e.selectedBranch == "" {
		e.selectedBranch = "true"
	}

	e.ornamentDraw = new(ifElseBorder.IfElseBorder)
	e.ornamentDrawIcon = new(ifElseBorder.IfElseBorder)

	conditionButton := connection.Setup{
		FatherId:           e.id,
		Name:               "conditionButton",
		DataType:           "bool",
		AcceptNotConnected: false,
		LookedUp:           false,
		IsADataInput:       true,
		ClickFunc: js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			data := this.Call("getConnData")
			log.Printf("ConditionButton FatherId: %v", data.Get("FatherId").String())
			return nil
		}),
	}
	if err = conditionButton.Verify(); err != nil {
		log.Printf("conditionButton.Verify: %v", err)
		return
	}

	e.ornamentDraw.ConditionButtonSetup(conditionButton)
	e.ornamentDraw.SetBranch(e.selectedBranch)

	if err = e.ornamentDraw.GetConnectionError(); err != nil {
		return
	}

	_ = e.ornamentDraw.Init()
	_ = e.ornamentDrawIcon.Init()

	_ = e.ornamentDraw.Update(0, 0, e.width, e.height)
	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", e.width.GetInt())
	ornamentSvg.Call("setAttribute", "height", e.height.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)

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
		return
	}

	e.elem.SetMinSizeD(e.horizontalMinimumSize, e.verticalMinimumSize)
	devices.BackendZRegistry.Register(e.id, e.elem)

	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		if rerr := e.elem.SetResizeButtons(adapter); rerr != nil {
			log.Printf("[SPRITE] ERROR: SetResizeButtons failed: %v", rerr)
		}
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}

	e.wireEvents()

	e.initialized = true

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

// ── Branch management ────────────────────────────────────────────────

func (e *StatementIfElse) toggleBranch() {
	e.assignNewChildren()

	if e.selectedBranch == "true" {
		e.selectedBranch = "false"
	} else {
		e.selectedBranch = "true"
	}

	log.Printf("[IfElse] toggle branch to %q on %v", e.selectedBranch, e.id)

	e.applyBranchVisibility()
	e.ornamentDraw.SetBranch(e.selectedBranch)
	go e.recacheOrnament()
}

func (e *StatementIfElse) assignNewChildren() {
	if e.sceneMgr == nil {
		return
	}

	containedIDs := e.sceneMgr.ChildrenOf(e.id)

	known := make(map[string]bool, len(e.trueBranchIDs)+len(e.falseBranchIDs))
	for _, id := range e.trueBranchIDs {
		known[id] = true
	}
	for _, id := range e.falseBranchIDs {
		known[id] = true
	}

	for _, id := range containedIDs {
		if !known[id] {
			if e.selectedBranch == "true" {
				e.trueBranchIDs = append(e.trueBranchIDs, id)
			} else {
				e.falseBranchIDs = append(e.falseBranchIDs, id)
			}
		}
	}

	containedSet := make(map[string]bool, len(containedIDs))
	for _, id := range containedIDs {
		containedSet[id] = true
	}
	e.trueBranchIDs = filterExisting(e.trueBranchIDs, containedSet)
	e.falseBranchIDs = filterExisting(e.falseBranchIDs, containedSet)
}

func filterExisting(ids []string, existingSet map[string]bool) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if existingSet[id] {
			result = append(result, id)
		}
	}
	return result
}

func (e *StatementIfElse) applyBranchVisibility() {
	if e.stage == nil {
		return
	}

	var showIDs, hideIDs []string
	if e.selectedBranch == "true" {
		showIDs = e.trueBranchIDs
		hideIDs = e.falseBranchIDs
	} else {
		showIDs = e.falseBranchIDs
		hideIDs = e.trueBranchIDs
	}

	for _, id := range hideIDs {
		if elem, found := e.stage.GetElement(id); found {
			elem.SetVisible(false)
		}
		if warnElem, found := e.stage.GetElement(id + "_warning"); found {
			warnElem.SetVisible(false)
		}
		// Take the device out of the wire layer and out of collision so the
		// inactive branch shows no orphan wire, is not a cross-branch connect
		// target, and does not collide with the active branch. Registration is
		// preserved — these are flag flips, reversed when the branch is shown.
		if e.wireMgr != nil {
			e.wireMgr.SetElementHidden(id, true)
		}
		if e.sceneMgr != nil {
			e.sceneMgr.SetHidden(id, true)
		}
	}

	for _, id := range showIDs {
		if elem, found := e.stage.GetElement(id); found {
			elem.SetVisible(true)
		}
		// Reverse the hide: redraw its wires, allow it as a connect target, and
		// put it back into collision (which re-surfaces any real conflict mark
		// via the scenegraph observer).
		if e.wireMgr != nil {
			e.wireMgr.SetElementHidden(id, false)
		}
		if e.sceneMgr != nil {
			e.sceneMgr.SetHidden(id, false)
		}
	}
}

// ── Hex menu items ───────────────────────────────────────────────────

// getBodyMenuItems returns body context menu items for this container.
// Order: Delete first (canonical per D4), Inspect, Resize toggle,
// Forward/Backward z-ordering.
//
// Português: Itens do menu de contexto do corpo.
// Ordem canonizada: Delete, Inspect, Resize, Forward, Backward.
func (e *StatementIfElse) getBodyMenuItems() []contextMenu.Item {
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
			log.Printf("[IfElse] delete: %v", e.id)
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

// ── Sprite event wiring ──────────────────────────────────────────────

func (e *StatementIfElse) wireEvents() {
	// Click — three hit regions: toggle pill, condition connector, body.
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		_, h := e.elem.GetSize()

		if event.LocalX >= 20 && event.LocalX <= 100 &&
			event.LocalY >= 16 && event.LocalY <= 38 {
			log.Printf("[IfElse] toggle clicked on %v", e.id)
			e.toggleBranch()
			return
		}

		// [PIN] standard pin hit box at the container's LEFT edge — same
		// edge point the border ornament draws and the wire anchors to.
		// Português: Caixa de clique do pino padrão na borda ESQUERDA do
		// container — mesmo edge point que o ornamento desenha e o fio
		// ancora.
		connY := h / 2
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), connY,
			event.LocalX, event.LocalY) {
			if e.ctxMenu != nil && e.wireMgr != nil {
				elemX, elemY := e.elem.GetPosition()
				menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
				go e.ctxMenu.OpenAtWorld(
					mainMenu.ConnectorMenu(e.wireMgr, e.id, "condition"),
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

	// Drag — scenegraph lifecycle.
	e.elem.SetOnDragStart(func(event sprite.DragEvent) {
		e.dragStartX, e.dragStartY = e.elem.GetPosition()
		if e.sceneMgr != nil {
			e.sceneMgr.BeginDrag(e.id)
		}
	})

	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.UpdateDrag(e.id)
		}
	})

	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.elem.GetPositionD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)

		finalX, finalY := e.elem.GetPosition()
		dx, dy := rulesContainer.DragChildDelta(e.dragStartX, e.dragStartY, finalX, finalY)
		if e.sceneMgr != nil {
			e.sceneMgr.EndDrag(e.id, dx, dy)
		}

		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		e.assignNewChildren()
		e.applyBranchVisibility()

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// Resize — scenegraph lifecycle + two-axis clamp.
	e.elem.SetOnResizeStart(func(event sprite.ResizeEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.BeginResize(e.id)
		}

		e.resizeChildBounds = nil
		e.resizeParentInner = nil

		if e.sceneMgr != nil {
			if bounds := e.sceneMgr.ChildrenBounds(e.id); bounds != nil {
				cb := rulesContainer.Rect{X: bounds.X, Y: bounds.Y, W: bounds.Width, H: bounds.Height}
				e.resizeChildBounds = &cb
			}
			if parentInner := e.sceneMgr.ParentInnerBBox(e.id); parentInner != nil {
				pi := rulesContainer.Rect{X: parentInner.X, Y: parentInner.Y, W: parentInner.Width, H: parentInner.Height}
				e.resizeParentInner = &pi
			}
		}
	})

	e.elem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		if e.resizeChildBounds != nil {
			loopX, loopY := e.elem.GetPosition()
			loopW, loopH := e.elem.GetSize()
			proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: loopW, H: loopH}
			pad := rulesContainer.IfElsePadding()
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
		wD, hD := e.elem.GetSizeD()
		newW, newH := e.gridAdjust.AdjustCenterD(wD, hD)
		loopX, loopY := e.elem.GetPosition()
		proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: newW.GetFloat(), H: newH.GetFloat()}

		if e.resizeChildBounds != nil {
			pad := rulesContainer.IfElsePadding()
			proposed = rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)
		}
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

		e.SetResizeEnable(false)
		e.SetDragEnable(true)

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetResizeRedrawInterval(1000)
	e.elem.SetOnResizeRedraw(func(event sprite.ResizeEvent) {
		go e.recacheOrnament()
	})

	e.elem.SetCursorHitTest(func(localX, localY float64) sprite.CursorStyle {
		_, h := e.elem.GetSize()
		if localX >= 20 && localX <= 100 && localY >= 16 && localY <= 38 {
			return sprite.CursorPointer
		}
		connY := h / 2
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), connY, localX, localY) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── SVG helpers ──────────────────────────────────────────────────────

func (e *StatementIfElse) recacheOrnament() {
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

// ── Wire connectors ──────────────────────────────────────────────────

func (e *StatementIfElse) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "condition"},
		IsOutput:           false,
		AllowedTypes:       []string{"bool"},
		AcceptNotConnected: false,
		Locked:             false,
		MaxConnections:     1,
		Label:              "Condition",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			_, h := e.elem.GetSize()
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), h/2)
			return ex + ax, ey + ay
		},
	})
}

// ── Identity ─────────────────────────────────────────────────────────

func (e *StatementIfElse) GetDeviceType() string   { return "StatementIfElse" }
func (e *StatementIfElse) GetIconName() string     { return "IfElse" }
func (e *StatementIfElse) GetIconCategory() string { return "Logic" }

// ── Scene geometry interface ────────────────────────────────────────

func (e *StatementIfElse) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementIfElse) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	pad := rulesContainer.IfElsePadding()
	return &scene.Rect{
		X:      x + pad.Left,
		Y:      y + pad.Top,
		Width:  w - pad.Left - pad.Right,
		Height: h - pad.Top - pad.Bottom,
	}
}

func (e *StatementIfElse) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

func (e *StatementIfElse) RefreshVisual() {
	e.recacheOrnament()
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// ── Branch serialization ─────────────────────────────────────────────

func (e *StatementIfElse) GetProperties() map[string]interface{} {
	e.assignNewChildren()
	props := map[string]interface{}{
		"selectedBranch": e.selectedBranch,
		"trueBranchIDs":  e.trueBranchIDs,
		"falseBranchIDs": e.falseBranchIDs,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// container's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do container.
func (e *StatementIfElse) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementIfElse) SetComment(c string) { e.comment = c }

// showInspectOverlay opens the container's Inspect: a Properties tab whose
// only editable datum is the universal COMMENT (containers have no other
// form-editable state — membership and geometry live on the stage).
// Português: Abre o Inspect do container: aba Properties cujo único dado
// editável é o COMENTÁRIO universal (containers não têm outro estado
// editável por formulário — membros e geometria vivem no stage).
func (e *StatementIfElse) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementIfElse) inspectConfig() overlay.Config {
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

func (e *StatementIfElse) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if branch, ok := values["selectedBranch"]; ok {
		e.selectedBranch = branch
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetBranch(branch)
		}
	}
}

// ── Basic getters/setters ────────────────────────────────────────────

func (e *StatementIfElse) Get() *html.TagSvg    { return nil }
func (e *StatementIfElse) SetFatherId(_ string) {}

func (e *StatementIfElse) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}
func (e *StatementIfElse) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementIfElse) SetSize(w, h rulesDensity.Density) {
	e.width = w
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(w, h)
	}
}
func (e *StatementIfElse) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementIfElse) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}
func (e *StatementIfElse) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementIfElse) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementIfElse) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		y := e.elem.GetYD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}
func (e *StatementIfElse) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		x := e.elem.GetXD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}
func (e *StatementIfElse) SetWidth(width rulesDensity.Density) {
	e.width = width
	if e.elem != nil {
		h := e.elem.GetHeightD()
		newW, newH := e.gridAdjust.AdjustCenterD(width, h)
		e.elem.SetSizeD(newW, newH)
	}
}
func (e *StatementIfElse) SetHeight(height rulesDensity.Density) {
	e.height = height
	if e.elem != nil {
		w := e.elem.GetWidthD()
		newW, newH := e.gridAdjust.AdjustCenterD(w, height)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementIfElse) GetID() string          { return e.id }
func (e *StatementIfElse) GetName() string        { return e.name }
func (e *StatementIfElse) GetInitialized() bool   { return e.initialized }
func (e *StatementIfElse) GetSelected() bool      { return e.selected }
func (e *StatementIfElse) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementIfElse) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementIfElse) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementIfElse) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementIfElse) GetStatus() int         { return e.iconStatus }
func (e *StatementIfElse) SetStatus(s int)        { e.iconStatus = s }
func (e *StatementIfElse) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementIfElse) GetResize() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementIfElse) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

func (e *StatementIfElse) SetSelected(selected bool) {
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

func (e *StatementIfElse) SetDragEnable(enabled bool) {
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

func (e *StatementIfElse) SetResizeEnable(enabled bool) {
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

func (e *StatementIfElse) onConnectionClick() {}

// ── Icon ─────────────────────────────────────────────────────────────

func (e *StatementIfElse) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)

	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())

	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).
		Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	xc := rulesIcon.Width / 4
	yc := rulesIcon.Height * 0.15
	wOrn := rulesIcon.Width / 2
	icon := e.ornamentDrawIcon.GetSvg().X(xc.GetInt()).Y(yc.GetInt())
	_ = e.ornamentDrawIcon.Update(0, 0, wOrn, wOrn)

	widthLabel, _ := utilsText.GetTextSize(
		data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight,
		rulesIcon.FontStyle, data.LabelFontSize.GetInt(),
	)
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).FontWeight(rulesIcon.FontWeight).
		FontStyle(rulesIcon.FontStyle).FontSize(data.LabelFontSize.GetInt()).
		Text(data.Label).Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - widthLabel/2).Y(data.LabelY.GetInt())

	svgIcon.Append(hexDraw, icon, label)

	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementIfElse) OpenInspect() { go e.showInspectOverlay() }
