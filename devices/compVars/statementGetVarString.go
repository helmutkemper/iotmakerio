// /ide/devices/compVars/statementGetVarString.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compVars

// statementGetVarString.go — Get-variable device (string).
//
// Reads a user-declared project variable of type string and emits its current
// value on a single output. At code generation a GetVar produces NO instruction
// of its own — its output is a register alias for the variable (see
// resolveInput2 in server/codegen/ir/emit.go). One output, no inputs.
//
// Visual design (Option A tint):
//
//	┌─────────────────────┐  ← 2px border, 5px corner radius, type color (string = green)
//	│ VAR           string │  ← 18px header: "VAR" tag (left), type label (right)
//	├─────────────────────┤  ← divider
//	│                     │
//	│       counter   ◉   │  ← variable name, 16px bold, centered; ◉ = output connector
//	│                     │
//	└─────────────────────┘
//	getVarString1              ← editable label, 12px muted
//
// Body click:      Inspect · Delete
// Connector click: Connect (output-only)
// Double-click:    Inspect overlay
//
// Português: Device "ler variável" (string). Lê uma variável de projeto do
// tipo string e emite o valor atual numa saída. No codegen, um GetVar NÃO
// emite instrução — a saída é alias do registrador da variável. Uma saída,
// sem entradas.

import (
	"fmt"
	"log"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesDevice"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
	"github.com/helmutkemper/iotmakerio/rulesZIndex"
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

// StatementGetVarString is a get-variable device for type string.
// No inputs — single output connector that emits the variable's string value.
type StatementGetVarString struct {
	stage sprite.Stage
	elem  sprite.Element

	id      string
	name    string
	varName string // the project variable this device reads (its codegen identifier)
	label   string // editable name shown below ornament (defaults to id)

	// e.height is ornament height only. Total element = e.height + KLabelHeight.
	width  rulesDensity.Density
	height rulesDensity.Density

	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool // always true: constants do not resize

	pendingSelected     *bool
	pendingDragEnable   *bool
	pendingResizeEnable *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller.
	ctxMenu    *contextMenu.Controller
	wireMgr    *wire.Manager
	gridAdjust grid.Adjust

	iconStatus  int
	lastClick   time.Time
	sceneNotify func()
	onRemove    func(id string)
}

// ── Dependency injection ──────────────────────────────────────────────────────

func (e *StatementGetVarString) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementGetVarString) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementGetVarString) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementGetVarString) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementGetVarString) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementGetVarString) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementGetVarString) SetVarName(v string) {
	e.varName = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementGetVarString) GetVarName() string             { return e.varName }
func (e *StatementGetVarString) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementGetVarString) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementGetVarString) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	if e.elem != nil {
		e.elem.SetVisible(false)
		elem := e.elem
		e.elem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
}

// ── SVG rendering ─────────────────────────────────────────────────────────────

// renderSVG builds the complete SVG for this device including the label area.
// Total height = ornament height + KLabelHeight.
func (e *StatementGetVarString) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat() // ornament height only
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	ts := rulesDevice.TypeStyleFor("string")

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect — border color = type color
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header — rounded top, flat bottom
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, w-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, w-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// "VAR" tag (left) + type label (right)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" font-weight="bold" dominant-baseline="middle">VAR</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="middle">string</text>`,
		w-bw-6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, w-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Variable name (body)
	displayName := e.varName
	if displayName == "" {
		displayName = "?"
	}
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		w/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, escapeXml(displayName),
	)

	// Output connector
	svg += fmt.Sprintf(
		`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="%s" stroke-width="1"/>`,
		w-rulesDevice.KConnectorOffsetRight, h/2,
		rulesDevice.KConnectorRadius, ts.Color, rulesDevice.KColorConnectorStroke,
	)

	// Label
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementGetVarString) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementGetVarString) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("getVarString")
	e.resizeLocked = true
	if e.width == 0 {
		e.width = rulesDevice.KConstDefaultWidth
	}
	if e.height == 0 {
		e.height = rulesDevice.KConstDefaultHeight
	}
	totalH := e.height + rulesDevice.KLabelHeight
	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:     e.id,
		Width:  e.width.GetFloat(),
		Height: totalH.GetFloat(),
		Index:  rulesZIndex.Constant,
		SvgXml: e.renderSVG(),
	})
	if err != nil {
		return fmt.Errorf("create element: %w", err)
	}
	minH := rulesDensity.Density(rulesDevice.KConstMinHeight) + rulesDevice.KLabelHeight
	e.elem.SetMinSizeD(rulesDevice.KConstMinWidth, minH)
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		_ = e.elem.SetResizeButtons(adapter)
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}
	if e.ctxMenu == nil {
		log.Printf("[GetVarString] warning: no context menu set — menus disabled")
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

// ── Events ────────────────────────────────────────────────────────────────────

func (e *StatementGetVarString) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if e.ctxMenu == nil {
			return
		}
		// Close-then-return: first click dismisses, next click
		// decides which new menu to open. The linear renderer has
		// no ghost-click window — the overlay absorbs backdrop
		// clicks itself and only reports them after the close
		// finishes.
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX := elemX + event.LocalX
		menuY := elemY + event.LocalY

		// Double-click → Inspect.
		now := time.Now()
		if now.Sub(e.lastClick) < 300*time.Millisecond {
			e.lastClick = time.Time{}
			go e.showInspectOverlay()
			return
		}
		e.lastClick = now

		// Connector hit test.
		w, _ := e.elem.GetSize()
		dx := event.LocalX - (w - rulesDevice.KConnectorOffsetRight)
		dy := event.LocalY - e.height.GetFloat()/2
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "output"), menuX, menuY)
			return
		}

		go e.ctxMenu.OpenAtWorld(e.bodyMenuItems(), menuX, menuY)
	})

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
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(nx, ny)
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		w, _ := e.elem.GetSize()
		dx := lx - (w - rulesDevice.KConnectorOffsetRight)
		dy := ly - e.height.GetFloat()/2
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── Menu ──────────────────────────────────────────────────────────────────────

// bodyMenuItems returns the body context menu for this constant.
// Delete first, Inspect second — canonical order per decision D4,
// fixing the original inversion present on every compConsts device.
//
// Português: Itens do menu de contexto do corpo. Delete primeiro,
// Inspect depois — ordem canônica conforme decisão D4.
func (e *StatementGetVarString) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[GetVarString] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementGetVarString) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementGetVarString) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{Key: "varName", Label: translate.T("propVariable", "Variable"), Type: overlay.FieldText, Value: e.varName, Placeholder: "counter", InputFilter: "identifier"},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id, ReadOnly: true},
				},
			},
			{
				Label: "Code Preview", Type: overlay.TabMonaco,
				Content:  fmt.Sprintf("// Get variable: %s\n// Reads the variable's current value; emits no code of its own\n// (alias to the variable's register).", e.varName),
				Language: "go", ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: getVarStringHelp()},
		},
		OnSave: func(values map[string]string) {
			if v, ok := values["varName"]; ok {
				e.varName = v
			}
			if lbl, ok := values["label"]; ok {
				e.label = lbl
			}
			go func() {
				time.Sleep(200 * time.Millisecond)
				e.recacheSVG()
				if e.sceneNotify != nil {
					e.sceneNotify()
				}
			}()
		},
	}
}

func (e *StatementGetVarString) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementGetVarString) ApplyProperties(values map[string]string) {
	if v, ok := values["varName"]; ok {
		e.varName = v
	}
	if lbl, ok := values["label"]; ok {
		e.label = lbl
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.recacheSVG()
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	}()
}

// ── Wire connectors ───────────────────────────────────────────────────────────

func (e *StatementGetVarString) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput:           true,
		AllowedTypes:       []string{"string"},
		AcceptNotConnected: true,
		MaxConnections:     0,
		Label:              "Output",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			w := e.elem.GetWidthD().GetFloat()
			return ex + w - rulesDevice.KConnectorOffsetRight, ey + e.height.GetFloat()/2
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementGetVarString) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementGetVarString) Get() *html.TagSvg { return nil }
func (e *StatementGetVarString) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementGetVarString) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementGetVarString) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementGetVarString) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementGetVarString) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementGetVarString) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementGetVarString) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementGetVarString) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementGetVarString) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementGetVarString) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementGetVarString) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// ── State ─────────────────────────────────────────────────────────────────────

func (e *StatementGetVarString) GetInitialized() bool   { return e.initialized }
func (e *StatementGetVarString) GetID() string          { return e.id }
func (e *StatementGetVarString) GetName() string        { return e.name }
func (e *StatementGetVarString) GetSelected() bool      { return e.selected }
func (e *StatementGetVarString) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementGetVarString) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementGetVarString) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementGetVarString) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementGetVarString) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementGetVarString) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementGetVarString) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementGetVarString) GetStatus() int  { return e.iconStatus }
func (e *StatementGetVarString) SetStatus(s int) { e.iconStatus = s }
func (e *StatementGetVarString) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementGetVarString) SetSelected(sel bool) {
	if e.selectLocked {
		e.selected = false
		return
	}
	e.selected = sel
	if e.elem == nil {
		e.pendingSelected = &sel
		return
	}
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
	e.elem.ShowResizeButtons(false)
}

func (e *StatementGetVarString) SetDragEnable(en bool) {
	if e.dragLocked {
		e.dragEnabled = false
		return
	}
	e.dragEnabled = en
	if e.elem == nil {
		e.pendingDragEnable = &en
		return
	}
	e.elem.SetDragEnable(en)
	e.elem.ShowResizeButtons(false)
}

func (e *StatementGetVarString) SetResizeEnable(_ bool) {
	// Constant devices never resize — resizeLocked is always true.
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementGetVarString) GetIconName() string     { return "GetVarString" }
func (e *StatementGetVarString) GetIconCategory() string { return "Variables" }

func (e *StatementGetVarString) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 3).
		Text("VAR").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 16).Y((rulesIcon.Height / 2).GetInt() + 4)
	wl, _ := utilsText.GetTextSize(data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight, rulesIcon.FontStyle, data.LabelFontSize.GetInt())
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).FontWeight(rulesIcon.FontWeight).FontStyle(rulesIcon.FontStyle).
		FontSize(data.LabelFontSize.GetInt()).Text(data.Label).Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - wl/2).Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, labelIcon, label)
	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}

// ── Scene export ──────────────────────────────────────────────────────────────

func (e *StatementGetVarString) GetDeviceType() string { return "StatementGetVarString" }
func (e *StatementGetVarString) GetProperties() map[string]interface{} {
	return map[string]interface{}{"varName": e.varName, "label": e.label}
}

// GetVariableDecl implements scene.VariableDeclarer: this device declares the
// string variable it names. The serializer collects it into the scene's
// top-level "variables" array (Path A) so the codegen emits one zero-init
// declaration (int32_t <name> = 0; / var <name> int32). An empty name (device
// placed but not yet bound) contributes nothing.
//
// Português: Implementa scene.VariableDeclarer — declara a variável stringeira que
// nomeia. O serializer a coleta no array "variables" (Path A) para o codegen
// emitir uma declaração zero-init. Nome vazio (ainda não vinculado) não
// contribui.
func (e *StatementGetVarString) GetVariableDecl() (name string, typ string) {
	return e.varName, "string"
}
func (e *StatementGetVarString) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementGetVarString) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementGetVarString) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementGetVarString) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help text ─────────────────────────────────────────────────────────────────

func getVarStringHelp() string {
	return `# GetVarString — Get String Variable

Reads a user-declared project **variable** of type **string** and emits its current
value on the output.

At code generation a GetVar emits **no instruction of its own** — its output is
a register alias for the variable. Declare variables in the **Variables** panel,
then drag a chip onto the stage to place a Get or Set.

## Properties

| Property | Type   | Description                      |
|----------|--------|----------------------------------|
| Variable | string | Name of the string variable to read |
| Label    | string | Name shown below the device      |

## Output

| Port   | Type |
|--------|------|
| output | string |

## Tips

- **Double-click** the device body to open Properties quickly.
- Connect to **Add**, **Compare**, **Mul**, or **Div** inputs.
- Pair with **SetVarInt** to update the same variable elsewhere on the stage.
`
}
