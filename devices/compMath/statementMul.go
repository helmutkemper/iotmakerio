// devices/compMath/statementMul.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compMath

import (
	"fmt"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/connection"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/ornament"
	"github.com/helmutkemper/iotmakerio/ornament/device"
	"github.com/helmutkemper/iotmakerio/ornament/math"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesDevice"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
	"github.com/helmutkemper/iotmakerio/rulesZIndex"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
	"github.com/helmutkemper/iotmakerio/wire"

	"github.com/helmutkemper/iotmakerio/scene"
	"github.com/helmutkemper/iotmakerio/scenegraph"
)

type StatementMul struct {
	// [SPRITE] replaced: block block.Block
	stage sprite.Stage
	elem  sprite.Element

	// [SPRITE] state managed locally (was inside Block).
	name         string
	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool
	width        rulesDensity.Density
	height       rulesDensity.Density

	// [SPRITE] pending state: stores values set before Init() (when elem is nil).
	pendingResizeEnable *bool
	pendingDragEnable   *bool
	pendingSelected     *bool

	// [SPRITE] warning mark as separate sprite.Element.
	warningMark        ornament.WarningMark
	warningElem        sprite.Element
	warningMarkEnabled bool

	// [SPRITE] resize button template.
	resizerButton block.ResizeButton

	// [HEXMENU] shared hex menu instance. The device no longer OPENS it (all
	// device menus are linear context menus — the old hexagonal output menu
	// and its first-click tutorial were removed after user testing); the
	// reference is kept only so a click on the device can close a hex menu
	// left open elsewhere (the main palette), keeping the ghost-menu
	// prevention rule intact.
	//
	// Português: Instância compartilhada do menu hexagonal. O device não a
	// ABRE mais (todos os menus do device são menus de contexto lineares — o
	// antigo menu hexagonal da saída e seu tutorial de primeiro clique foram
	// removidos após os testes com usuários); a referência fica só para um
	// clique no device fechar um menu hexagonal deixado aberto por outro
	// lugar (a palette principal), mantendo a regra de prevenção de menus
	// fantasma.
	hexMenu *mainMenu.SpriteHexMenu

	// [CTXMENU] linear context menu controller — the single menu system for
	// body and connector clicks.
	//
	// Português: Controlador do menu de contexto linear — o único sistema de
	// menus para cliques no corpo e nos conectores.
	ctxMenu *contextMenu.Controller

	// [WIRE] wire manager reference — set via SetWireManager() before Init()
	wireMgr *wire.Manager

	defaultWidth          rulesDensity.Density
	defaultHeight         rulesDensity.Density
	horizontalMinimumSize rulesDensity.Density
	verticalMinimumSize   rulesDensity.Density

	ornamentDraw *math.OrnamentMultiplier

	id string

	gridAdjust grid.Adjust

	// [LABEL] editable label displayed below the device (left-aligned).
	// Defaults to the device id. Edited via the Inspect panel or double-click.
	//
	// Português: Label editável exibido abaixo do device (alinhado à esquerda).
	// Padrão é o id do device. Editado pelo painel Inspect ou duplo-clique.
	label    string
	canvasEl js.Value // <canvas> DOM element for positioning overlays

	// [COMMENT] user comment — appears as a code comment in generated source.
	//
	// Português: Comentário do usuário — aparece como comentário no código gerado.
	comment string

	// [DATATYPE] current data type for all connectors (int, float, string).
	// Changed via the Inspect panel. Updates connector AllowedTypes and codegen output.
	//
	// Português: Tipo de dado atual para todos os conectores (int, float, string).
	// Alterado pelo painel Inspect. Atualiza AllowedTypes dos conectores e saída do codegen.
	dataType string

	// [HIDDEN] when true, the label below the device is not displayed.
	//
	// Português: Quando true, o label abaixo do device não é exibido.
	hidden bool

	// double-click detection
	lastClickTime time.Time

	// [SCENE] overlap policy and scene change notifier
	sceneNotify func()
	// [SCENEGRAPH] injected by scene.Serializer.Register (self-injection by
	// interface assertion). DragEnd reports through it so the scenegraph
	// refreshes geometry, recomputes conflicts (own + peers) and reassigns
	// parenting — the same EndDrag hook the containers use.
	// Português: Injetado pelo scene.Serializer.Register (auto-injeção por
	// assertion). O DragEnd reporta por ele para o scenegraph refrescar
	// geometria, recomputar conflitos (próprios + peers) e reatribuir
	// parenting — o mesmo gancho EndDrag dos containers.
	sceneMgr *scene.Serializer

	// [LIFECYCLE] onRemove is called by Remove() so external registries
	// (scene serializer, device manager) can clean up. Set by the factory.
	//
	// Português: onRemove é chamado por Remove() para que registros externos
	// (scene serializer, device manager) possam limpar. Definido pela factory.
	onRemove func(id string)
}

// [SPRITE] new method: must be called before Init()
func (e *StatementMul) SetStage(stage sprite.Stage) {
	e.stage = stage
}

// [WIRE] SetWireManager injects the shared wire.Manager. Must be called before Init().
//
// Português: Injeta o wire.Manager compartilhado. Deve ser chamado antes de Init().
func (e *StatementMul) SetWireManager(mgr *wire.Manager) {
	e.wireMgr = mgr
}

// SetCanvasEl stores the canvas DOM element for positioning overlay inputs.
func (e *StatementMul) SetCanvasEl(el js.Value) {
	e.canvasEl = el
}

// GetLabel returns the current editable label. Implements scene.Labeled.
func (e *StatementMul) GetLabel() string { return e.label }

// SetLabel updates the label and re-renders the SVG.
func (e *StatementMul) SetLabel(label string) {
	e.label = label
	go e.recacheOrnament()
}

// GetComment returns the user comment for code generation.
func (e *StatementMul) GetComment() string { return e.comment }

// SetComment sets the comment that appears in generated code.
func (e *StatementMul) SetComment(c string) { e.comment = c }

// GetDataType returns the current data type (int, float, string).
func (e *StatementMul) GetDataType() string { return e.dataType }

// SetDataType changes the data type for all connectors.
// Updates the wire.Manager's ConnectorInfo in-place (no disconnect).
//
// Português: Altera o tipo de dado de todos os conectores.
// Atualiza o ConnectorInfo do wire.Manager in-place (sem desconectar).
func (e *StatementMul) SetDataType(dt string) {
	if dt != "int" && dt != "float" {
		log.Printf("[Mul] %s: invalid dataType %q, keeping %q", e.id, dt, e.dataType)
		return
	}
	if dt == e.dataType {
		return
	}
	old := e.dataType
	e.dataType = dt
	e.updateConnectorTypes()
	// [PIN] repaint the connector pins with the new type's canonical color
	// and re-cache the ornament so the change is visible immediately. The
	// pin, the device accent and the wire of a type share one palette
	// (rulesDevice) — a stale pin color would break that contract on screen.
	// Português: Repinta os pinos com a cor canônica do novo tipo e
	// re-cacheia o ornamento para a mudança aparecer na hora. Pino, cor do
	// device e fio de um tipo compartilham uma paleta (rulesDevice) — um
	// pino com cor velha quebraria esse contrato na tela.
	if e.ornamentDraw != nil {
		e.ornamentDraw.SetConnectionTypes(dt, dt, dt)
		go e.recacheOrnament()
	}
	// Propagate the new type to wires already connected to this device so a
	// connected consumer (e.g. StatementCase) re-infers from the new type
	// instead of keeping the type captured when the wire was first connected.
	// Português: Propaga o novo tipo para fios já conectados a este device
	// para um consumidor conectado (ex.: StatementCase) re-inferir, em vez de
	// manter o tipo capturado quando o fio foi conectado pela primeira vez.
	if e.wireMgr != nil {
		e.wireMgr.RefreshElementWires(e.id)
	}
	log.Printf("[Mul] %s: dataType changed from %q to %q", e.id, old, dt)
}

// updateConnectorTypes updates AllowedTypes on all registered connectors
// via the wire.Manager pointer. Does NOT disconnect existing wires.
//
// Português: Atualiza AllowedTypes em todos os conectores registrados
// via ponteiro do wire.Manager. NÃO desconecta fios existentes.
func (e *StatementMul) updateConnectorTypes() {
	if e.wireMgr == nil {
		return
	}
	ports := []string{"inputX", "inputY", "output"}
	for _, port := range ports {
		conn := e.wireMgr.GetConnector(wire.ConnectorID{
			ElementID: e.id,
			PortName:  port,
		})
		if conn != nil {
			conn.AllowedTypes = []string{e.dataType}
		}
	}
}

// IsHidden returns whether the device is hidden on the canvas.
func (e *StatementMul) IsHidden() bool { return e.hidden }

// SetHidden sets whether the label is hidden below the device.
func (e *StatementMul) SetHidden(h bool) {
	e.hidden = h
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.recacheOrnament()
	}()
}

func (e *StatementMul) Append() {
	// [SPRITE] replaced: e.block.Append()
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementMul) Remove() {
	// [LIFECYCLE] notify external registries (scene serializer, device manager)
	if e.onRemove != nil {
		e.onRemove(e.id)
	}

	// [WIRE] unregister all connectors and wires for this element
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}

	// [SPRITE] destroy elements (not just hide — free resources)
	if e.warningElem != nil {
		e.warningElem.SetVisible(false)
		wElem := e.warningElem
		go func() {
			time.Sleep(50 * time.Millisecond)
			wElem.Destroy()
		}()
		e.warningElem = nil
	}
	if e.elem != nil {
		e.elem.SetVisible(false)
		elem := e.elem
		go func() {
			time.Sleep(50 * time.Millisecond)
			elem.Destroy()
		}()
		e.elem = nil
	}
}

// SetOnRemove sets the callback invoked when the device is removed.
// The factory uses this to unregister from scene serializer and device manager.
//
// Português: Define o callback invocado quando o device é removido.
// A factory usa isso para desregistrar do scene serializer e device manager.
func (e *StatementMul) SetOnRemove(fn func(id string)) {
	e.onRemove = fn
}

func (e *StatementMul) SetResizerButton(resizeButton block.ResizeButton) {
	// [SPRITE] replaced: e.block.SetResizerButton(resizeButton)
	e.resizerButton = resizeButton
}

func (e *StatementMul) SetGridAdjust(gridAdjust grid.Adjust) {
	e.gridAdjust = gridAdjust
}

func (e *StatementMul) GetWidth() (width rulesDensity.Density) {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}

func (e *StatementMul) GetHeight() (height rulesDensity.Density) {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}

// SetWarning sets the visibility of the warning mark
func (e *StatementMul) SetWarning(warning bool) {
	if !e.initialized {
		return
	}

	e.warningMarkEnabled = warning
	if e.warningElem != nil {
		e.warningElem.SetVisible(warning)
		if warning {
			e.updateWarningPosition()
		}
	}
}

func (e *StatementMul) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}

func (e *StatementMul) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
		e.updateWarningPosition()
	}
}

func (e *StatementMul) SetSize(width, height rulesDensity.Density) {
	e.width = width
	e.height = height
	if e.elem != nil {
		e.elem.SetSizeD(width, height)
	}
}

// SetHexMenu injects the shared hex menu instance. Must be called before Init().
// The same instance is shared across all devices in the workspace.
//
// Português: Injeta a instância compartilhada do menu hexagonal.
// Deve ser chamado antes de Init(). A mesma instância é compartilhada
// entre todos os devices do workspace.
func (e *StatementMul) SetHexMenu(m *mainMenu.SpriteHexMenu) {
	e.hexMenu = m
}

// SetContextMenu injects the shared linear context menu
// controller. Must be called before Init(). Used for the body
// and every port menu — the linear menu is the device's single
// menu system.
//
// Português: Injeta o controller do menu de contexto linear.
// Usado para o corpo e todos os menus de porta — o menu linear é
// o único sistema de menus do device.
func (e *StatementMul) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}

// getBodyMenuItems returns context menu items for a click on the body.
// Delete first (canonical order), Inspect second, Settings last.
//
// Português: Itens do menu de contexto para clique no corpo.
// Ordem canonizada: Delete, Inspect, Settings.
func (e *StatementMul) getBodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[CTXMENU] delete action for: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			x, y := e.elem.GetPosition()
			w, h := e.elem.GetSize()
			log.Printf("[CTXMENU] inspect: id=%v pos=(%.0f,%.0f) size=(%.0f,%.0f)",
				e.id, x, y, w, h)
			go e.showInspectOverlay()
		}),
		{
			ID:              "settings",
			Label:           translate.T("menuDeviceSettings", "Settings"),
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			HelpKey:         "helpMenuMulSettings",
			HelpFallback:    "Changes the data type of every connector at once. Choose Int, Float, or String — the inputs and the output all switch together so this operator keeps its signature consistent.",
			Submenu:         e.settingsSubmenuItems(),
		},
	}
}

// settingsSubmenuItems returns the type-picker submenu reached from
// the Settings item. The linear renderer injects its own Back row —
// submenus do NOT include a manual back button.
//
// Português: Submenu de escolha de tipo. O renderer injeta Back.
func (e *StatementMul) settingsSubmenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		{
			ID: "typeInt", Label: translate.T("menuTypeInt", "Int"),
			FontAwesomePath: rulesIcon.KFAPlus, ViewBox: "0 0 448 512",
			HelpKey:      "helpMenuTypeInt",
			HelpFallback: "Sets every connector on this device to integer.",
			OnClick:      func() { e.SetDataType("int") },
		},
		{
			ID: "typeFloat", Label: translate.T("menuTypeFloat", "Float"),
			FontAwesomePath: rulesIcon.KFADivide, ViewBox: "0 0 448 512",
			HelpKey:      "helpMenuTypeFloat",
			HelpFallback: "Sets every connector to 32-bit floating point.",
			OnClick:      func() { e.SetDataType("float") },
		},
	}
}

// getInputXMenuItems returns the linear menu for the inputX connector.
func (e *StatementMul) getInputXMenuItems() []contextMenu.Item {
	return mainMenu.ConnectorMenu(e.wireMgr, e.id, "inputX")
}

// getInputYMenuItems returns the linear menu for the inputY connector.
func (e *StatementMul) getInputYMenuItems() []contextMenu.Item {
	return mainMenu.ConnectorMenu(e.wireMgr, e.id, "inputY")
}

// getOutputMenuItems returns the output-connector menu: Connect +
// Monitor submenu. The 4-level Monitor → Value → Format structure is
// preserved from the original hex version; the linear renderer
// handles the depth gracefully via push-replace navigation.
//
// Português: Menu do conector de saída: Connect + submenu Monitor. A
// estrutura de 4 níveis vem da versão hexagonal original; o renderer
// linear lida com a profundidade via navegação push-replace.
func (e *StatementMul) getOutputMenuItems() []contextMenu.Item {
	return mainMenu.ConnectorMenu(e.wireMgr, e.id, "output",
		contextMenu.Item{
			ID:              "monitorOutput",
			Label:           translate.T("menuDeviceMonitor", "Monitor"),
			FontAwesomePath: rulesIcon.KFADesktop,
			ViewBox:         "0 0 576 512",
			HelpKey:         "helpMenuMonitor",
			HelpFallback:    "Opens the output monitor. See the current value, its history, or configure numeric format.",
			Submenu:         e.getOutputMonitorSubmenu(),
		},
	)
}

// getOutputMonitorSubmenu returns level 2: Value / History.
func (e *StatementMul) getOutputMonitorSubmenu() []contextMenu.Item {
	return []contextMenu.Item{
		{
			ID: "monValue", Label: translate.T("menuMonitorValue", "Value"),
			FontAwesomePath: rulesIcon.KFAEye, ViewBox: "0 0 512 512",
			HelpFallback: "Inspect the live value of this output.",
			Submenu:      e.getOutputValueSubmenu(),
		},
		{
			ID: "monHistory", Label: translate.T("menuMonitorHistory", "History"),
			FontAwesomePath: rulesIcon.KFAClockRotateLeft, ViewBox: "0 0 512 512",
			HelpFallback: "Show the history of values over time.",
			OnClick:      func() { log.Printf("[CTXMENU] monitor output history: id=%v", e.id) },
		},
	}
}

// getOutputValueSubmenu returns level 3: Format / Range.
func (e *StatementMul) getOutputValueSubmenu() []contextMenu.Item {
	return []contextMenu.Item{
		{
			ID: "valFormat", Label: translate.T("menuValueFormat", "Format"),
			FontAwesomePath: rulesIcon.KFAGear, ViewBox: "0 0 512 512",
			HelpFallback: "Pick a numeric format: decimal, hex, binary.",
			Submenu:      e.getOutputFormatSubmenu(),
		},
		{
			ID: "valRange", Label: translate.T("menuValueRange", "Range"),
			FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight, ViewBox: "0 0 512 512",
			HelpFallback: "Set an expected value range on this output.",
			OnClick:      func() { log.Printf("[CTXMENU] output value range: id=%v", e.id) },
		},
	}
}

// getOutputFormatSubmenu returns level 4: Decimal / Hex / Binary.
func (e *StatementMul) getOutputFormatSubmenu() []contextMenu.Item {
	return []contextMenu.Item{
		{
			ID: "fmtDecimal", Label: translate.T("menuFormatDecimal", "Decimal"),
			FontAwesomePath: rulesIcon.KFAPlus, ViewBox: "0 0 448 512",
			HelpFallback: "Show as base-10 decimal.",
			OnClick:      func() { log.Printf("[CTXMENU] output format = Decimal: id=%v", e.id) },
		},
		{
			ID: "fmtHex", Label: translate.T("menuFormatHex", "Hex"),
			FontAwesomePath: rulesIcon.KFABars, ViewBox: "0 0 448 512",
			HelpFallback: "Show as base-16 hexadecimal.",
			OnClick:      func() { log.Printf("[CTXMENU] output format = Hex: id=%v", e.id) },
		},
		{
			ID: "fmtBinary", Label: translate.T("menuFormatBinary", "Binary"),
			FontAwesomePath: rulesIcon.KFADivide, ViewBox: "0 0 448 512",
			HelpFallback: "Show as base-2 binary.",
			OnClick:      func() { log.Printf("[CTXMENU] output format = Binary: id=%v", e.id) },
		},
	}
}

func (e *StatementMul) Init() (err error) {

	// [SPRITE] guard: stage must be set before Init
	if e.stage == nil {
		log.Println("[SPRITE] Error: SetStage() must be called before Init()")
		return
	}

	e.SetName("stmMul")

	warningMark := new(ornament.WarningMarkExclamation)
	warningMark.SetMargin(0)
	_ = warningMark.Init()
	e.warningMark = warningMark

	size := rulesDensity.Density(60)
	e.defaultWidth = size
	e.defaultHeight = size
	e.horizontalMinimumSize = size
	e.verticalMinimumSize = size

	if e.width == 0 {
		e.width = e.defaultWidth
	}
	if e.height == 0 {
		e.height = e.defaultHeight
	}

	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id     // default label is the device id
	e.dataType = "int" // default data type

	e.resizeLocked = true

	e.ornamentDraw = new(math.OrnamentMultiplier)

	// LEGACY: ClickFunc is required by connection.Setup but never invoked
	// in the sprite system — clicks are handled by wireEvents().
	noopClick := js.FuncOf(func(this js.Value, args []js.Value) interface{} { return nil })

	inputXSetup := connection.Setup{
		FatherId:           e.id,
		Name:               "inputX",
		DataType:           e.dataType,
		AcceptNotConnected: false,
		LookedUp:           false,
		IsADataInput:       true,
		ClickFunc:          noopClick,
	}
	if err = inputXSetup.Verify(); err != nil {
		log.Printf("inputXSetup.Verify: %v", err)
		return
	}
	e.ornamentDraw.InputXSetup(inputXSetup)

	inputYSetup := connection.Setup{
		FatherId:           e.id,
		Name:               "inputY",
		DataType:           e.dataType,
		AcceptNotConnected: false,
		LookedUp:           false,
		IsADataInput:       true,
		ClickFunc:          noopClick,
	}
	if err = inputYSetup.Verify(); err != nil {
		log.Printf("inputYSetup.Verify: %v", err)
		return
	}
	e.ornamentDraw.InputYSetup(inputYSetup)

	outputSetup := connection.Setup{
		FatherId:           e.id,
		Name:               "output",
		DataType:           e.dataType,
		AcceptNotConnected: false,
		LookedUp:           false,
		IsADataInput:       false,
		ClickFunc:          noopClick,
	}
	if err = outputSetup.Verify(); err != nil {
		log.Printf("outputSetup.Verify: %v", err)
		return
	}
	e.ornamentDraw.OutputSetup(outputSetup)

	_ = e.ornamentDraw.Init()

	// [PIN] paint the connector pins with the initial data type's canonical
	// color before the first Update/cache, so the very first render already
	// matches the wires this device will accept.
	// Português: Pinta os pinos com a cor canônica do tipo inicial antes do
	// primeiro Update/cache, para o primeiro render já casar com os fios que
	// este device vai aceitar.
	e.ornamentDraw.SetConnectionTypes(e.dataType, e.dataType, e.dataType)

	// [SPRITE] serialize ornament SVG and create sprite.Element
	_ = e.ornamentDraw.Update(0, 0, e.width, e.height)

	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	totalHeight := e.height + rulesDevice.KLabelHeight
	ornamentSvg.Call("setAttribute", "width", e.width.GetInt())
	ornamentSvg.Call("setAttribute", "height", totalHeight.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)
	ornamentXml = e.injectLabelIntoSvg(ornamentXml, e.height)

	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:         e.id,
		X:          0,
		Y:          0,
		Width:      e.width.GetFloat(),
		Height:     totalHeight.GetFloat(),
		Index:      rulesZIndex.Math,
		DragEnable: false,
		SvgXml:     ornamentXml,
	})
	if err != nil {
		log.Printf("[SPRITE] Failed to create element: %v", err)
		return
	}

	// [SPRITE] min size
	e.elem.SetMinSizeD(e.horizontalMinimumSize, e.verticalMinimumSize+rulesDevice.KLabelHeight)

	// [SPRITE] resize handle buttons via adapter
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		resizeErr := e.elem.SetResizeButtons(adapter)
		if resizeErr != nil {
			log.Printf("[SPRITE] SetResizeButtons failed: %v (edge resize still works)", resizeErr)
		}
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}

	// [SPRITE] wire events
	e.wireEvents()

	// [SPRITE] warning element
	e.initWarningElement()

	e.initialized = true

	// [SPRITE] apply pending states
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

// [SPRITE] wireEvents: connects sprite events to local state logic.
func (e *StatementMul) wireEvents() {

	// [HEXMENU] Single click handler.
	// Hit-test order: connections first (small targets), then body (fallback).
	// Each click closes any open menu, then opens the appropriate one.
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		// [PIN] connector geometry — the SAME edge points the ornament draws
		// (device.OpAmpPinEdges) and the wire anchors use, so click, drawing
		// and wire can never disagree. Sizes come from the logical (Density)
		// space and are scaled at the comparison, keeping the hit-test
		// correct on any screen density.
		// Português: Geometria dos conectores — os MESMOS edge points que o
		// ornamento desenha e que os fios ancoram; clique, desenho e fio não
		// conseguem divergir. Tamanhos vêm do espaço lógico (Density) e são
		// escalados na comparação, mantendo o hit-test correto em qualquer
		// densidade de tela.
		wD, hD := e.elem.GetSizeD()
		pinInputX, pinInputY, pinOutput := device.OpAmpPinEdges(wD, hD-rulesDevice.KLabelHeight)

		// Double-click detection: open inspect overlay
		now := time.Now()
		isDoubleClick := now.Sub(e.lastClickTime) < 300*time.Millisecond // todo: 300*time.Millisecond must be moved to rules
		e.lastClickTime = now

		// World coordinates of click (passed to menu for positioning)
		elemX, elemY := e.elem.GetPosition()
		clickWX := elemX + event.LocalX
		clickWY := elemY + event.LocalY

		// Close any open menu first. The device itself only opens linear
		// context menus; the hex-menu check exists to close a hex menu left
		// open elsewhere (the main palette), preventing menu ghosts.
		// Português: Fecha qualquer menu aberto primeiro. O device só abre
		// menus de contexto lineares; a checagem do menu hexagonal fecha um
		// menu deixado aberto por outro lugar (a palette principal),
		// prevenindo menus fantasma.
		if e.ctxMenu != nil && e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		if e.hexMenu != nil && e.hexMenu.IsVisible() {
			e.hexMenu.Close()
			return
		}

		if isDoubleClick {
			go e.showInspectOverlay()
			return
		}

		// Connector pins — hit the padded standard pin box (click comfort is
		// preserved: the box is as generous as the old circular targets).
		// Português: Pinos — testa a caixa padrão do pino com folga (o
		// conforto de clique é preservado: a caixa é tão generosa quanto os
		// alvos circulares antigos).
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			pinInputX.X.GetFloat(), pinInputX.Y.GetFloat(), event.LocalX, event.LocalY) {
			log.Printf("[CTXMENU] inputX clicked on: %v", e.id)
			go e.ctxMenu.OpenAtWorld(e.getInputXMenuItems(), clickWX, clickWY)
			return
		}

		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			pinInputY.X.GetFloat(), pinInputY.Y.GetFloat(), event.LocalX, event.LocalY) {
			log.Printf("[CTXMENU] inputY clicked on: %v", e.id)
			go e.ctxMenu.OpenAtWorld(e.getInputYMenuItems(), clickWX, clickWY)
			return
		}

		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			pinOutput.X.GetFloat(), pinOutput.Y.GetFloat(), event.LocalX, event.LocalY) {
			log.Printf("[CTXMENU] output clicked on: %v", e.id)
			go e.ctxMenu.OpenAtWorld(e.getOutputMenuItems(), clickWX, clickWY)
			return
		}

		// No connection hit → body menu
		log.Printf("[CTXMENU] body clicked on: %v", e.id)
		go e.ctxMenu.OpenForDevice(e, e.getBodyMenuItems(), clickWX, clickWY)
	})

	// Drag: grid snap on end.
	// [SCENE] real-time conflict feedback — notify scene
	// on every drag step so the stage-level overlay reacts
	// to position changes immediately, not only on release.
	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.elem.GetPositionD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
		e.updateWarningPosition()

		// [WIRE] recalculate wire routes connected to this element
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		// [SCENE] notify scene change
		// [SCENEGRAPH] dx/dy=0: they only move container descendants (this
		// device has none); geometry is re-read live by refreshGeometry.
		// Português: dx/dy=0: eles só movem descendentes de container (este
		// device não tem); a geometria é relida ao vivo pelo refreshGeometry.
		if e.sceneMgr != nil {
			e.sceneMgr.EndDrag(e.id, 0, 0)
		}
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// Resize: re-cache ornament on end + grid snap.
	e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		wD, hD := e.elem.GetSizeD()
		newW, newH := e.gridAdjust.AdjustCenterD(wD, hD)
		e.elem.SetSizeD(newW, newH)

		e.width = newW
		e.height = newH

		go e.recacheOrnament()
		e.updateWarningPosition()

		// [WIRE] recalculate wire routes connected to this element
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		// [SCENE] notify scene change
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// [SPRITE] periodic resize redraw (1s interval)
	e.elem.SetResizeRedrawInterval(1000) // todo: move to rules
	e.elem.SetOnResizeRedraw(func(event sprite.ResizeEvent) {
		go e.recacheOrnament()
	})

	// [SPRITE] cursor hit-test: pointer cursor near connection points.
	e.elem.SetCursorHitTest(func(localX float64, localY float64) sprite.CursorStyle {
		// [PIN] same edge points and hit boxes as the click handler — one
		// geometry source (device.OpAmpPinEdges + rulesConnection.PinHit),
		// so the pointer cursor appears exactly where a click would land.
		// Português: Mesmos edge points e caixas do handler de clique — uma
		// fonte de geometria só, então o cursor pointer aparece exatamente
		// onde o clique cairia.
		wD, hD := e.elem.GetSizeD()
		pinInputX, pinInputY, pinOutput := device.OpAmpPinEdges(wD, hD-rulesDevice.KLabelHeight)

		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			pinInputX.X.GetFloat(), pinInputX.Y.GetFloat(), localX, localY) {
			return sprite.CursorPointer
		}
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			pinInputY.X.GetFloat(), pinInputY.Y.GetFloat(), localX, localY) {
			return sprite.CursorPointer
		}
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			pinOutput.X.GetFloat(), pinOutput.Y.GetFloat(), localX, localY) {
			return sprite.CursorPointer
		}

		return "" // default cursor
	})
}

// =====================================================================
//  [WIRE] Connector Registration & Dynamic Menus
// =====================================================================

// RegisterConnectors registers all connection points of this StatementMul
// with the wire.Manager. Must be called AFTER Init() (when e.elem exists).
//
// Português:
//
//	RegisterConnectors registra todos os pontos de conexão deste StatementMul
//	no wire.Manager. Deve ser chamado APÓS Init() (quando e.elem existe).
func (e *StatementMul) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}

	// inputX — left side. The wire anchors at the OUTER TIP of the standard
	// pin (rulesConnection.PinAnchor over the shared OpAmpPinEdges geometry).
	// Português: inputX — lado esquerdo. O fio ancora na PONTA EXTERNA do
	// pino padrão (PinAnchor sobre a geometria compartilhada OpAmpPinEdges).
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "inputX"},
		IsOutput:           false,
		AllowedTypes:       []string{e.dataType},
		AcceptNotConnected: false,
		Locked:             false,
		MaxConnections:     1,
		Label:              "Input X",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			wD, hD := e.elem.GetSizeD()
			pinInputX, _, _ := device.OpAmpPinEdges(wD, hD-rulesDevice.KLabelHeight)
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				pinInputX.X.GetFloat(), pinInputX.Y.GetFloat())
			return ex + ax, ey + ay
		},
	})

	// inputY — left side, lower pin; anchored at the pin's outer tip.
	// Português: inputY — lado esquerdo, pino inferior; ancorado na ponta
	// externa do pino.
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "inputY"},
		IsOutput:           false,
		AllowedTypes:       []string{e.dataType},
		AcceptNotConnected: false,
		Locked:             false,
		MaxConnections:     1,
		Label:              "Input Y",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			wD, hD := e.elem.GetSizeD()
			_, pinInputY, _ := device.OpAmpPinEdges(wD, hD-rulesDevice.KLabelHeight)
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				pinInputY.X.GetFloat(), pinInputY.Y.GetFloat())
			return ex + ax, ey + ay
		},
	})

	// output — right side, at the triangle vertex; anchored at the pin's
	// outer tip so the wire leaves from OUTSIDE the device silhouette.
	// Português: output — lado direito, no vértice do triângulo; ancorado na
	// ponta externa para o fio sair de FORA da silhueta do device.
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput:           true,
		AllowedTypes:       []string{e.dataType},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     0, // unlimited
		Label:              "Output",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			wD, hD := e.elem.GetSizeD()
			_, _, pinOutput := device.OpAmpPinEdges(wD, hD-rulesDevice.KLabelHeight)
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideRight,
				pinOutput.X.GetFloat(), pinOutput.Y.GetFloat())
			return ex + ax, ey + ay
		},
	})
}

// Wire connect/disconnect submenus are now generic functions in mainMenu:
// mainMenu.WireConnectSubmenu() and mainMenu.WireDisconnectSubmenu().
// Called automatically by mainMenu.ConnectorMenu().

// [SPRITE] re-serializes ornament SVG and re-caches as bitmap.
func (e *StatementMul) recacheOrnament() {
	if e.elem == nil || e.ornamentDraw == nil {
		return
	}

	wD, hD := e.elem.GetSizeD()
	ornH := hD - rulesDevice.KLabelHeight // ornament height without label area
	if ornH < 20 {
		ornH = 20
	}
	_ = e.ornamentDraw.Update(0, 0, wD, ornH)

	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", wD.GetInt())
	ornamentSvg.Call("setAttribute", "height", hD.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)
	ornamentXml = e.injectLabelIntoSvg(ornamentXml, ornH)
	_ = e.elem.CacheFromSvg(ornamentXml)
}

// injectLabelIntoSvg adds the editable label text at the bottom-left of the SVG.
// ornH is the ornament height (total element height minus label area).
// The label is injected just before the closing </svg> tag.
//
// Português: Injeta o texto do label editável no canto inferior esquerdo do SVG.
// ornH é a altura do ornamento (altura total menos a área do label).
func (e *StatementMul) injectLabelIntoSvg(svgXml string, ornH rulesDensity.Density) string {
	// If label is hidden, don't inject anything
	if e.hidden {
		return svgXml
	}

	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}

	// Escape XML entities in label
	displayLabel = strings.ReplaceAll(displayLabel, "&", "&amp;")
	displayLabel = strings.ReplaceAll(displayLabel, "<", "&lt;")
	displayLabel = strings.ReplaceAll(displayLabel, ">", "&gt;")
	displayLabel = strings.ReplaceAll(displayLabel, "\"", "&quot;")

	labelY := ornH.GetFloat() + 3

	labelSvg := fmt.Sprintf(
		//`<text x="4" y="%.1f" font-family="Arial,sans-serif" font-size="11" fill="#AABBCC" dominant-baseline="hanging">%s</text>`,
		rulesDevice.KDeviceLabel,
		labelY, displayLabel,
	)

	return strings.Replace(svgXml, "</svg>", labelSvg+"</svg>", 1)
}

// [SPRITE] creates warning mark as separate sprite.Element.
func (e *StatementMul) initWarningElement() {
	if e.warningMark == nil || e.stage == nil {
		return
	}

	_ = e.warningMark.Update(0, 0, e.width, e.height)
	warnSvg := e.warningMark.GetSvg().Get()
	warnSvg.Call("setAttribute", "width", e.width.GetInt())
	warnSvg.Call("setAttribute", "height", e.height.GetInt())
	warnXml := devices.SerializeSvgToXml(warnSvg)

	var err error
	e.warningElem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:     e.id + "_warning",
		X:      0,
		Y:      0,
		Width:  e.width.GetFloat(),
		Height: e.height.GetFloat(),
		Index:  2,
		SvgXml: warnXml,
	})
	if err != nil {
		log.Printf("[SPRITE] Failed to create warning element: %v", err)
		return
	}

	e.warningElem.SetVisible(false)
}

// [SPRITE] syncs warning element position with main element.
func (e *StatementMul) updateWarningPosition() {
	if e.warningElem == nil || e.elem == nil {
		return
	}

	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	e.warningElem.SetPosition(x, y)
	e.warningElem.SetSize(w, h)
}

func (e *StatementMul) GetID() (id string) {
	return e.id
}

func (e *StatementMul) SetSelected(selected bool) {
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
		e.SetDragEnable(true)
		e.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetSelected(true)
			go e.recacheOrnament()
		}
	} else {
		e.SetDragEnable(false)
		e.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetSelected(false)
			go e.recacheOrnament()
		}
	}
}

func (e *StatementMul) SetDragEnable(enabled bool) {
	if e.dragLocked {
		e.dragEnabled = false
		return
	}

	e.dragEnabled = enabled

	if e.elem == nil {
		e.pendingDragEnable = &enabled
		return
	}

	e.elem.SetDragEnable(enabled)

	if enabled {
		e.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

func (e *StatementMul) GetResizeEnable() (enabled bool) {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

func (e *StatementMul) SetResizeEnable(enabled bool) {
	if e.resizeLocked {
		if e.elem != nil {
			e.elem.SetResizeEnable(false)
			e.elem.ShowResizeButtons(false)
		}
		return
	}

	if e.elem == nil {
		e.pendingResizeEnable = &enabled
		return
	}

	e.elem.SetResizeEnable(enabled)
	e.elem.ShowResizeButtons(enabled)

	if enabled {
		e.SetDragEnable(false)
	}
}

// =====================================================================
//  Inspect overlay — properties + help from server + code preview
//
//  Português: Overlay de inspeção — propriedades + ajuda do servidor + preview
// =====================================================================

// showInspectOverlay opens the property panel for this Add device.
// Must be called in a goroutine (loads external scripts asynchronously).
func (e *StatementMul) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

// GetInspectConfig returns the overlay configuration for this device.
// Implements scene.Inspectable.
func (e *StatementMul) GetInspectConfig() interface{} {
	hiddenVal := "false"
	if e.hidden {
		hiddenVal = "true"
	}

	return overlay.Config{
		Title: fmt.Sprintf("%s", e.id),
		Width: "540px",
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:   "label",
						Label: translate.T("propLabel", "Label"),
						Type:  overlay.FieldText,
						Value: e.label,
					},
					{
						Key:      "id",
						Label:    "ID",
						Type:     overlay.FieldText,
						Value:    e.id,
						ReadOnly: true,
					},
					{
						Key:   "dataType",
						Label: translate.T("propDataType", "Data Type"),
						Type:  overlay.FieldSelect,
						Value: e.dataType,
						Options: []overlay.Option{
							{Value: "int", Label: "Int (integer arithmetic)"},
							{Value: "float", Label: "Float (decimal arithmetic)"},
						},
					},
					{
						Key:         "comment",
						Label:       translate.T("propComment", "Comment"),
						Type:        overlay.FieldTextarea,
						Value:       e.comment,
						Placeholder: translate.T("propCommentPlaceholder", "Comment shown in generated code..."),
						Rows:        3,
					},
					{
						Key:   "hidden",
						Label: translate.T("propHideLabel", "Hide Label"),
						Type:  overlay.FieldCheckbox,
						Value: hiddenVal,
					},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/math/statementMul.md", // todo: to rules
			},
			{
				Label:    translate.T("tabCodePreview", "Code Preview"),
				Type:     overlay.TabMonaco,
				Content:  e.codePreview(),
				Language: "go",
				ReadOnly: true,
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

// codePreview returns a preview of the generated code for this device.
func (e *StatementMul) codePreview() string {
	code := ""
	if e.comment != "" {
		// Multi-line comments
		for _, line := range strings.Split(e.comment, "\n") {
			code += fmt.Sprintf("// %s\n", line)
		}
	}

	switch e.dataType {
	case "float":
		code += fmt.Sprintf("var %s float64\n", e.id)
		code += fmt.Sprintf("%s = inputX + inputY", e.id)
	default: // int
		code += fmt.Sprintf("%s := inputX + inputY", e.id)
	}
	return code
}

// ApplyProperties applies the values from the inspect form to this device.
// Implements scene.Inspectable.
func (e *StatementMul) ApplyProperties(values map[string]string) {
	changed := false

	if v, ok := values["label"]; ok && v != "" && v != e.label {
		e.label = v
		changed = true
		log.Printf("[Mul] %s: label set to %q", e.id, v)
	}

	if v, ok := values["comment"]; ok && v != e.comment {
		e.comment = v
		log.Printf("[Mul] %s: comment set to %q", e.id, v)
	}

	if v, ok := values["hidden"]; ok {
		h := v == "true"
		if h != e.hidden {
			e.SetHidden(h)
			log.Printf("[Mul] %s: hidden set to %v", e.id, h)
		}
	}

	if v, ok := values["dataType"]; ok && v != e.dataType {
		e.SetDataType(v)
	}

	if changed {
		// recacheOrnament blocks on Image.onload — must run in a goroutine.
		go func() {
			time.Sleep(200 * time.Millisecond)
			e.recacheOrnament()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	}
}

// GetProperties returns codegen-relevant properties for scene export.
// Implements scene.Propertied.
func (e *StatementMul) GetProperties() map[string]interface{} {
	props := map[string]interface{}{}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	if e.hidden {
		props["hidden"] = true
	}
	if e.dataType != "" && e.dataType != "int" {
		props["dataType"] = e.dataType
	}
	return props
}

func (e *StatementMul) GetDeviceType() string { return "StatementMul" }

func (e *StatementMul) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementMul) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 5.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}

func (e *StatementMul) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementMul) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementMul) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	e.updateWarningPosition()
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementMul) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementMul) OpenInspect() { go e.showInspectOverlay() }
