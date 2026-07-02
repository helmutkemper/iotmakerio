// devices/compMath/statementDiv.go
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
	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/ornament"
	"github.com/helmutkemper/iotmakerio/ornament/math"
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

type StatementDiv struct {
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

	// [HEXMENU] single menu instance — reused for body and each connector.
	// Only one menu is visible at a time; Open() closes any previous one.
	hexMenu *mainMenu.SpriteHexMenu

	// [CTXMENU] linear context menu controller. Used by body
	// and all port clicks. Tutorial path (first-time output click)
	// still uses hexMenu because StartTutorialFromDevice flashes
	// hex items — a limitation that Delivery C will lift.
	ctxMenu *contextMenu.Controller

	// [HEXMENU] tutorial demo: first click on output uses guided tutorial mode.
	outputTutorialShown bool

	// [WIRE] wire manager reference — set via SetWireManager() before Init()
	wireMgr *wire.Manager

	defaultWidth          rulesDensity.Density
	defaultHeight         rulesDensity.Density
	horizontalMinimumSize rulesDensity.Density
	verticalMinimumSize   rulesDensity.Density

	ornamentDraw *math.OrnamentDivider

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

	// [LIFECYCLE] onRemove is called by Remove() so external registries
	// (scene serializer, device manager) can clean up. Set by the factory.
	//
	// Português: onRemove é chamado por Remove() para que registros externos
	// (scene serializer, device manager) possam limpar. Definido pela factory.
	onRemove func(id string)
}

// [SPRITE] new method: must be called before Init()
func (e *StatementDiv) SetStage(stage sprite.Stage) {
	e.stage = stage
}

// [WIRE] SetWireManager injects the shared wire.Manager. Must be called before Init().
//
// Português: Injeta o wire.Manager compartilhado. Deve ser chamado antes de Init().
func (e *StatementDiv) SetWireManager(mgr *wire.Manager) {
	e.wireMgr = mgr
}

// SetCanvasEl stores the canvas DOM element for positioning overlay inputs.
func (e *StatementDiv) SetCanvasEl(el js.Value) {
	e.canvasEl = el
}

// GetLabel returns the current editable label. Implements scene.Labeled.
func (e *StatementDiv) GetLabel() string { return e.label }

// SetLabel updates the label and re-renders the SVG.
func (e *StatementDiv) SetLabel(label string) {
	e.label = label
	go e.recacheOrnament()
}

// GetComment returns the user comment for code generation.
func (e *StatementDiv) GetComment() string { return e.comment }

// SetComment sets the comment that appears in generated code.
func (e *StatementDiv) SetComment(c string) { e.comment = c }

// GetDataType returns the current data type (int, float, string).
func (e *StatementDiv) GetDataType() string { return e.dataType }

// SetDataType changes the data type for all connectors.
// Updates the wire.Manager's ConnectorInfo in-place (no disconnect).
//
// Português: Altera o tipo de dado de todos os conectores.
// Atualiza o ConnectorInfo do wire.Manager in-place (sem desconectar).
func (e *StatementDiv) SetDataType(dt string) {
	if dt != "int" && dt != "float" {
		log.Printf("[Div] %s: invalid dataType %q, keeping %q", e.id, dt, e.dataType)
		return
	}
	if dt == e.dataType {
		return
	}
	old := e.dataType
	e.dataType = dt
	e.updateConnectorTypes()
	// Propagate the new type to wires already connected to this device so a
	// connected consumer (e.g. StatementCase) re-infers from the new type
	// instead of keeping the type captured when the wire was first connected.
	// Português: Propaga o novo tipo para fios já conectados a este device
	// para um consumidor conectado (ex.: StatementCase) re-inferir, em vez de
	// manter o tipo capturado quando o fio foi conectado pela primeira vez.
	if e.wireMgr != nil {
		e.wireMgr.RefreshElementWires(e.id)
	}
	log.Printf("[Div] %s: dataType changed from %q to %q", e.id, old, dt)
}

// updateConnectorTypes updates AllowedTypes on all registered connectors
// via the wire.Manager pointer. Does NOT disconnect existing wires.
//
// Português: Atualiza AllowedTypes em todos os conectores registrados
// via ponteiro do wire.Manager. NÃO desconecta fios existentes.
func (e *StatementDiv) updateConnectorTypes() {
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
func (e *StatementDiv) IsHidden() bool { return e.hidden }

// SetHidden sets whether the label is hidden below the device.
func (e *StatementDiv) SetHidden(h bool) {
	e.hidden = h
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.recacheOrnament()
	}()
}

func (e *StatementDiv) Append() {
	// [SPRITE] replaced: e.block.Append()
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementDiv) Remove() {
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
func (e *StatementDiv) SetOnRemove(fn func(id string)) {
	e.onRemove = fn
}

func (e *StatementDiv) SetResizerButton(resizeButton block.ResizeButton) {
	// [SPRITE] replaced: e.block.SetResizerButton(resizeButton)
	e.resizerButton = resizeButton
}

func (e *StatementDiv) SetGridAdjust(gridAdjust grid.Adjust) {
	e.gridAdjust = gridAdjust
}

func (e *StatementDiv) GetWidth() (width rulesDensity.Density) {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}

func (e *StatementDiv) GetHeight() (height rulesDensity.Density) {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}

// SetWarning sets the visibility of the warning mark
func (e *StatementDiv) SetWarning(warning bool) {
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

func (e *StatementDiv) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}

func (e *StatementDiv) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
		e.updateWarningPosition()
	}
}

func (e *StatementDiv) SetSize(width, height rulesDensity.Density) {
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
func (e *StatementDiv) SetHexMenu(m *mainMenu.SpriteHexMenu) {
	e.hexMenu = m
}

// SetContextMenu injects the shared linear context menu
// controller. Must be called before Init(). Used for body
// and port menus; the first-time tutorial on the output
// connector falls back to hexMenu.
//
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementDiv) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}

// getBodyMenuItems returns context menu items for a click on the body.
// Delete first (canonical order), Inspect second, Settings last.
//
// Português: Itens do menu de contexto para clique no corpo.
// Ordem canonizada: Delete, Inspect, Settings.
func (e *StatementDiv) getBodyMenuItems() []contextMenu.Item {
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
			HelpKey:         "helpMenuDivSettings",
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
func (e *StatementDiv) settingsSubmenuItems() []contextMenu.Item {
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
func (e *StatementDiv) getInputXMenuItems() []contextMenu.Item {
	return mainMenu.ConnectorMenu(e.wireMgr, e.id, "inputX")
}

// getInputYMenuItems returns the linear menu for the inputY connector.
func (e *StatementDiv) getInputYMenuItems() []contextMenu.Item {
	return mainMenu.ConnectorMenu(e.wireMgr, e.id, "inputY")
}

// getOutputMenuItems returns the normal (non-tutorial) output menu:
// Connect + Monitor submenu. The 4-level Monitor → Value → Format
// structure is preserved from the original hex version; the linear
// renderer handles the depth gracefully via push-replace navigation.
func (e *StatementDiv) getOutputMenuItems() []contextMenu.Item {
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
func (e *StatementDiv) getOutputMonitorSubmenu() []contextMenu.Item {
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
func (e *StatementDiv) getOutputValueSubmenu() []contextMenu.Item {
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
func (e *StatementDiv) getOutputFormatSubmenu() []contextMenu.Item {
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

// getOutputTutorialHexItems returns the tutorial-only hex version of
// the output menu. Must keep the same ID path as the linear version
// (monitorOutput → monValue → valFormat → fmtDecimal) so tutorial
// steps resolve correctly. Replaced in Delivery C by a linear
// tutorial.
func (e *StatementDiv) getOutputTutorialHexItems() []hexMenu.MenuItem {
	return []hexMenu.MenuItem{
		{ID: "connect_output", Col: 1, Row: 1,
			Label:           translate.T("menuDeviceConnect", "Connect"),
			FontAwesomePath: rulesIcon.KFALink, ViewBox: "0 0 640 512",
			Type: hexMenu.ItemAction, OnClick: func() {},
		},
		{ID: "monitorOutput", Col: 1, Row: 3,
			Label:           translate.T("menuDeviceMonitor", "Monitor"),
			FontAwesomePath: rulesIcon.KFADesktop, ViewBox: "0 0 576 512",
			Type: hexMenu.ItemSubmenu,
			Submenu: []hexMenu.MenuItem{
				hexMenu.GoBackItem(3, 3),
				{ID: "monValue", Col: 2, Row: 2, Label: "Value",
					FontAwesomePath: rulesIcon.KFAEye, ViewBox: "0 0 512 512",
					Type: hexMenu.ItemSubmenu,
					Submenu: []hexMenu.MenuItem{
						hexMenu.GoBackItem(3, 3),
						{ID: "valFormat", Col: 2, Row: 2, Label: "Format",
							FontAwesomePath: rulesIcon.KFAGear, ViewBox: "0 0 512 512",
							Type: hexMenu.ItemSubmenu,
							Submenu: []hexMenu.MenuItem{
								hexMenu.GoBackItem(4, 4),
								{ID: "fmtDecimal", Col: 3, Row: 1, Label: "Decimal",
									FontAwesomePath: rulesIcon.KFAPlus, ViewBox: "0 0 448 512",
									Type: hexMenu.ItemAction, OnClick: func() {},
									Styles: hexMenu.DefaultStyles(),
								},
							},
							Styles: hexMenu.DefaultStyles(),
						},
					},
					Styles: hexMenu.DefaultStyles(),
				},
			},
		},
	}
}

// getOutputTutorialSteps returns tutorial steps for the output connection menu.
// 4 levels deep — each step flashes the target item, user clicks, advances:
//
//	Step 1: Root page     → flash "Monitor"  → click → opens Monitor submenu
//	Step 2: Monitor page  → flash "Value"    → click → opens Value submenu
//	Step 3: Value page    → flash "Format"   → click → opens Format submenu
//	Step 4: Format page   → flash "Decimal"  → click → action executes, tutorial ends
func (e *StatementDiv) getOutputTutorialSteps() []hexMenu.TutorialStep {
	return []hexMenu.TutorialStep{
		{
			PagePath: nil,             // root page
			ItemID:   "monitorOutput", // flash Monitor
		},
		{
			PagePath: []string{"monitorOutput"}, // Monitor submenu
			ItemID:   "monValue",                // flash Value
		},
		{
			PagePath: []string{"monitorOutput", "monValue"}, // Value submenu
			ItemID:   "valFormat",                           // flash Format
		},
		{
			PagePath: []string{"monitorOutput", "monValue", "valFormat"}, // Format submenu
			ItemID:   "fmtDecimal",                                       // flash Decimal
		},
	}
}

func (e *StatementDiv) Init() (err error) {

	// [SPRITE] guard: stage must be set before Init
	if e.stage == nil {
		log.Println("[SPRITE] Error: SetStage() must be called before Init()")
		return
	}

	e.SetName("stmDiv")

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

	e.ornamentDraw = new(math.OrnamentDivider)

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
func (e *StatementDiv) wireEvents() {

	// [HEXMENU] Single click handler.
	// Hit-test order: connections first (small targets), then body (fallback).
	// Each click closes any open menu, then opens the appropriate one.
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		// Get device add size
		w, h := e.elem.GetSize()
		ornH := h - float64(rulesDevice.KLabelHeight) // ornament height without label
		connRadius := 10.0                            // todo: move to rules

		// Double-click detection: open inspect overlay
		now := time.Now()
		isDoubleClick := now.Sub(e.lastClickTime) < 300*time.Millisecond // todo: 300*time.Millisecond must be moved to rules
		e.lastClickTime = now

		// World coordinates of click (passed to menu for positioning)
		elemX, elemY := e.elem.GetPosition()
		clickWX := elemX + event.LocalX
		clickWY := elemY + event.LocalY

		// Close any open menu first — ctxMenu is the primary; hex
		// is only used for the first-time tutorial path below.
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

		// inputX at (2, 15)
		dx := event.LocalX - 2
		dy := event.LocalY - 15
		if dx*dx+dy*dy <= connRadius*connRadius {
			log.Printf("[CTXMENU] inputX clicked on: %v", e.id)
			go e.ctxMenu.OpenAtWorld(e.getInputXMenuItems(), clickWX, clickWY)
			return
		}

		// inputY at (2, ornH-18)
		dx = event.LocalX - 2
		dy = event.LocalY - (ornH - 18)
		if dx*dx+dy*dy <= connRadius*connRadius {
			log.Printf("[CTXMENU] inputY clicked on: %v", e.id)
			go e.ctxMenu.OpenAtWorld(e.getInputYMenuItems(), clickWX, clickWY)
			return
		}

		// todo:
		//   The code snippet below contains the tutorial, and the click event should be a connection feature;
		//   It should not reside within the device.

		// output at (width-12, ornH/2-2)
		dx = event.LocalX - (w - 12)
		dy = event.LocalY - (ornH/2 - 2)
		if dx*dx+dy*dy <= connRadius*connRadius {
			if !e.outputTutorialShown && e.hexMenu != nil {
				e.outputTutorialShown = true
				log.Printf("[HEXMENU] output clicked — first-time tutorial: %v", e.id)
				go e.hexMenu.StartTutorialFromDevice(
					e.getOutputTutorialHexItems(),
					e.getOutputTutorialSteps(),
					clickWX, clickWY,
				)
				return
			}
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
		w, h := e.elem.GetSize()
		ornH := h - float64(rulesDevice.KLabelHeight)
		connRadius := 10.0

		// inputX at (2, 15)
		dx := localX - 2
		dy := localY - 15
		if dx*dx+dy*dy <= connRadius*connRadius {
			return sprite.CursorPointer
		}

		// inputY at (2, ornH-18)
		dx = localX - 2
		dy = localY - (ornH - 18)
		if dx*dx+dy*dy <= connRadius*connRadius {
			return sprite.CursorPointer
		}

		// output at (width-12, ornH/2-2)
		dx = localX - (w - 12)
		dy = localY - (ornH/2 - 2)
		if dx*dx+dy*dy <= connRadius*connRadius {
			return sprite.CursorPointer
		}

		return "" // default cursor
	})
}

// =====================================================================
//  [WIRE] Connector Registration & Dynamic Menus
// =====================================================================

// RegisterConnectors registers all connection points of this StatementDiv
// with the wire.Manager. Must be called AFTER Init() (when e.elem exists).
//
// Português:
//
//	RegisterConnectors registra todos os pontos de conexão deste StatementDiv
//	no wire.Manager. Deve ser chamado APÓS Init() (quando e.elem existe).
func (e *StatementDiv) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}

	// inputX — left side, local position (2, 15)
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
			return ex + 2, ey + 15
		},
	})

	// inputY — left side, local position (2, ornH-18)
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
			_, h := e.elem.GetSize()
			ornH := h - float64(rulesDevice.KLabelHeight)
			return ex + 2, ey + ornH - 18
		},
	})

	// output — right side, local position (width-12, ornH/2-2)
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
			w, h := e.elem.GetSize()
			ornH := h - float64(rulesDevice.KLabelHeight)
			return ex + w - 12, ey + ornH/2 - 2
		},
	})
}

// Wire connect/disconnect submenus are now generic functions in mainMenu:
// mainMenu.WireConnectSubmenu() and mainMenu.WireDisconnectSubmenu().
// Called automatically by mainMenu.ConnectorMenu().

// [SPRITE] re-serializes ornament SVG and re-caches as bitmap.
func (e *StatementDiv) recacheOrnament() {
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
func (e *StatementDiv) injectLabelIntoSvg(svgXml string, ornH rulesDensity.Density) string {
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
func (e *StatementDiv) initWarningElement() {
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
func (e *StatementDiv) updateWarningPosition() {
	if e.warningElem == nil || e.elem == nil {
		return
	}

	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	e.warningElem.SetPosition(x, y)
	e.warningElem.SetSize(w, h)
}

func (e *StatementDiv) GetID() (id string) {
	return e.id
}

func (e *StatementDiv) SetSelected(selected bool) {
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

func (e *StatementDiv) SetDragEnable(enabled bool) {
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

func (e *StatementDiv) GetResizeEnable() (enabled bool) {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

func (e *StatementDiv) SetResizeEnable(enabled bool) {
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
func (e *StatementDiv) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

// GetInspectConfig returns the overlay configuration for this device.
// Implements scene.Inspectable.
func (e *StatementDiv) GetInspectConfig() interface{} {
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
				ContentURL: "/help/devices/math/statementDiv.md", // todo: to rules
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
func (e *StatementDiv) codePreview() string {
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
func (e *StatementDiv) ApplyProperties(values map[string]string) {
	changed := false

	if v, ok := values["label"]; ok && v != "" && v != e.label {
		e.label = v
		changed = true
		log.Printf("[Div] %s: label set to %q", e.id, v)
	}

	if v, ok := values["comment"]; ok && v != e.comment {
		e.comment = v
		log.Printf("[Div] %s: comment set to %q", e.id, v)
	}

	if v, ok := values["hidden"]; ok {
		h := v == "true"
		if h != e.hidden {
			e.SetHidden(h)
			log.Printf("[Div] %s: hidden set to %v", e.id, h)
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
func (e *StatementDiv) GetProperties() map[string]interface{} {
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

func (e *StatementDiv) GetDeviceType() string { return "StatementDiv" }

func (e *StatementDiv) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementDiv) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 5.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}

func (e *StatementDiv) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementDiv) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementDiv) MoveBy(dx, dy float64) {
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
