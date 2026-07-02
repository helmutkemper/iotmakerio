// devices/compConsts/statementConstFloat.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compConsts

// statementConstFloat.go — Floating-point constant device.
//
// Visual design:
//
//	┌─────────────────────┐  ← border color is the float-family color
//	│ FLOAT           ◉   │
//	├─────────────────────┤
//	│                     │
//	│       3.14          │  ← value, 16px bold
//	│                     │
//	└─────────────────────┘
//	constFloat1
//
// Float is ABSTRACT here, like the "int" constant and the abstract-float
// variables: the maker does not pick a bit-width. The output port is "float",
// and the target PROFILE decides the width at codegen (float on AVR, double on
// a 64-bit target). See docs/CLAUDE_NUMERIC_TYPES_AND_TARGETS.md. With no
// per-device precision there is nothing to select, store, or restore on reload
// — which removes the old float32-reload connector desync at the root.
//
// Body click:      Inspect · Delete
// Connector click: Connect (output-only)
// Double-click:    Inspect overlay

import (
	"fmt"
	"log"
	"math"
	"strconv"
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

// StatementConstFloat is a floating-point constant device.
// No inputs — single output connector typed "float" (abstract). The element
// width is the target profile's job at codegen, not a per-device choice.
type StatementConstFloat struct {
	stage sprite.Stage
	elem  sprite.Element

	id    string
	name  string
	value float64 // stored as float64; the codegen/profile decides the emitted width
	label string

	width  rulesDensity.Density
	height rulesDensity.Density

	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool

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

func (e *StatementConstFloat) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementConstFloat) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementConstFloat) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementConstFloat) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementConstFloat) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementConstFloat) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementConstFloat) SetValue(v float64) {
	e.value = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementConstFloat) GetValue() float64              { return e.value }
func (e *StatementConstFloat) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// portType returns the wire type advertised on the output connector — the
// abstract "float". The target profile decides the element width at codegen.
func (e *StatementConstFloat) portType() string { return "float" }

// previewGoType mirrors the Go backend for the Code Preview tab: goTypeName
// widens the abstract "float" to Go's float64, exactly like emitConst/emitVar.
func (e *StatementConstFloat) previewGoType() string { return "float64" }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementConstFloat) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementConstFloat) Remove() {
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

// formatValue formats the float for display.
// Uses up to 6 significant digits and removes unnecessary trailing zeros.
func (e *StatementConstFloat) formatValue() string {
	v := e.value
	if v == math.Trunc(v) && math.Abs(v) < 1e9 {
		return strconv.FormatFloat(v, 'f', 1, 64) // e.g. "3.0" not "3"
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

func (e *StatementConstFloat) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	ts := rulesDevice.TypeStyleFor(e.portType())

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, w-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, w-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// Type tag (F32 or F64)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">%s</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color, ts.Tag,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, w-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Value
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		w/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, e.formatValue(),
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

func (e *StatementConstFloat) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementConstFloat) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("constFloat")
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
		log.Printf("[ConstFloat] warning: no context menu set — menus disabled")
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

func (e *StatementConstFloat) wireEvents() {
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

		now := time.Now()
		if now.Sub(e.lastClick) < 300*time.Millisecond {
			e.lastClick = time.Time{}
			go e.showInspectOverlay()
			return
		}
		e.lastClick = now

		w, _ := e.elem.GetSize()
		dx := event.LocalX - (w - rulesDevice.KConnectorOffsetRight)
		dy := event.LocalY - e.height.GetFloat()/2
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "output"), menuX, menuY)
			return
		}

		go e.ctxMenu.OpenForDevice(e, e.bodyMenuItems(), menuX, menuY)
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
func (e *StatementConstFloat) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[ConstFloat] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementConstFloat) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementConstFloat) inspectConfig() overlay.Config {
	codeType := e.previewGoType()
	codeVal := e.formatValue()

	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:         "value",
						Label:       translate.T("propValue", "Value"),
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatFloat(e.value, 'f', -1, 64),
						Placeholder: "0.0",
					},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id, ReadOnly: true},
				},
			},
			{
				Label:    "Code Preview",
				Type:     overlay.TabMonaco,
				Content:  fmt.Sprintf("// Generated code:\n%s := %s(%s)", e.id, codeType, codeVal),
				Language: "go",
				ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: constFloatHelp()},
		},
		OnSave: func(values map[string]string) {
			if v, ok := values["value"]; ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					e.value = f
				}
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

func (e *StatementConstFloat) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementConstFloat) ApplyProperties(values map[string]string) {
	if v, ok := values["value"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			e.value = f
		}
	}
	if lbl, ok := values["label"]; ok {
		e.label = lbl
	}

	// No precision to restore, and no connector to re-register: the output type
	// is the fixed abstract "float", so the reload can never leave the connector
	// diverging from portType(). This removes the old float32-scalar reload
	// connector desync at the root (same resolution as the ConstArrayFloat
	// sibling).
	//
	// Português: Sem precisão a restaurar nem connector a re-registrar: a saída
	// é o "float" abstrato fixo, então o reload nunca desalinha o connector.
	// Remove o desync de reload do float32 escalar na raiz.

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.recacheSVG()
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	}()
}

// ── Wire connectors ───────────────────────────────────────────────────────────

func (e *StatementConstFloat) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput:           true,
		AllowedTypes:       []string{e.portType()},
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

func (e *StatementConstFloat) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementConstFloat) Get() *html.TagSvg { return nil }
func (e *StatementConstFloat) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementConstFloat) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstFloat) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementConstFloat) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementConstFloat) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementConstFloat) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementConstFloat) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementConstFloat) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementConstFloat) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstFloat) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstFloat) MoveBy(dx, dy float64) {
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

func (e *StatementConstFloat) GetInitialized() bool   { return e.initialized }
func (e *StatementConstFloat) GetID() string          { return e.id }
func (e *StatementConstFloat) GetName() string        { return e.name }
func (e *StatementConstFloat) GetSelected() bool      { return e.selected }
func (e *StatementConstFloat) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementConstFloat) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementConstFloat) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementConstFloat) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementConstFloat) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementConstFloat) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementConstFloat) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementConstFloat) GetStatus() int  { return e.iconStatus }
func (e *StatementConstFloat) SetStatus(s int) { e.iconStatus = s }
func (e *StatementConstFloat) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementConstFloat) SetSelected(sel bool) {
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

func (e *StatementConstFloat) SetDragEnable(en bool) {
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

func (e *StatementConstFloat) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementConstFloat) GetIconName() string     { return "ConstFloat" }
func (e *StatementConstFloat) GetIconCategory() string { return "Constants" }

func (e *StatementConstFloat) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("3.14").Fill(data.ColorIcon).
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

func (e *StatementConstFloat) GetDeviceType() string { return "StatementConstFloat" }
func (e *StatementConstFloat) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"value": e.value,
		// precision is the abstract "float": emitConstFloat reads it and
		// goTypeName/cTypeName widen it per backend (Go float64; C float/double
		// per target profile). No concrete token is stored, so the reload path
		// cannot desync the connector (removes KNOWN_ISSUES §2A.3's scalar twin).
		"precision": "float",
		"label":     e.label,
	}
}
func (e *StatementConstFloat) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementConstFloat) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementConstFloat) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementConstFloat) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help ──────────────────────────────────────────────────────────────────────

func constFloatHelp() string {
	return `# ConstFloat — Floating-Point Constant

Outputs a fixed **floating-point** value.

## Precision is the target's job, not yours

Float is **abstract**, just like ` + "`int`" + `. You do not pick 32- or 64-bit
here; the **target profile** decides the width when the code is generated —
` + "`float`" + ` on an 8/32-bit MCU (AVR, RP2040), ` + "`double`" + ` on a 64-bit target.
The same constant is portable across targets, and there is no width setting to
get wrong or to lose on reload.

## Generated code

| Language | Output                        |
|----------|-------------------------------|
| Go       | ` + "`constFloat1 := float64(3.14)`" + `  |
| C (MCU)  | ` + "`float constFloat1 = 3.14f;`" + `     |
| C (64b)  | ` + "`double constFloat1 = 3.14;`" + `     |

## Properties

| Property | Type    | Description                 |
|----------|---------|-----------------------------|
| Value    | float64 | The number to emit          |
| Label    | string  | Name shown below the device |

## Output

| Port   | Type  |
|--------|-------|
| output | float |

## Tips

- **Double-click** the device to open Properties.
- Connect to **Mul**, **Add**, or any float input device.
`
}
