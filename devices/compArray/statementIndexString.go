// devices/compArray/statementIndexString.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compArray

// statementIndexString.go — Array index reader device, INT element type.
//
// Visual design (fixed-size box, four connectors — two per side):
//
//	 array ●┐
//	        ├──┌──────────────┐
//	 index ●┘  │ INT           │●  value
//	           │    index       │
//	           │                │●  ok
//	           └──────────────┘
//	              indexInt1
//
// What it is:
//
//	A safe READER: given an array and an integer index, it outputs the element
//	at that position. It has NO data of its own — it only reads what the wires
//	feed it — so, unlike the constant collection devices, Inspect shows only the
//	manual (no editable properties).
//
//	One of THREE sibling devices (Int / Float / String), mirroring how the
//	constant collections are separate devices — one per element type, no type
//	select. The element type here is FIXED to int.
//
// The four ports (the names are a CONTRACT with the IR generator —
// ir.emitIndex reads exactly "array", "index", "value" and "ok"; renaming any
// of them silently breaks wiring resolution):
//
//	array  IN   []int   required   the collection to read from
//	index  IN   int     required   the position (always an int, even here)
//	value  OUT  int     required   the element; the type's zero when out of range
//	ok     OUT  bool    OPTIONAL   true when the index was in range
//
// Safety: the offline-tested codegen (ir.OpIndex) NEVER emits a raw subscript —
// the element is only read inside a bounds check, so an out-of-range or negative
// index yields the zero value (no panic in Go, no undefined read in C). The `ok`
// output is the graphical form of Go's comma-ok: wire it to react to a bad
// index, or leave it unwired and stay safe anyway (the check is then inlined and
// no `ok` variable is generated — see ir.emitIndex).
//
// UNWIRED = IDE ERROR for array / index / value (AcceptNotConnected: false):
// those three dangling are authoring mistakes the stage flags BEFORE codegen.
// Only `ok` is AcceptNotConnected: true — an unwired `ok` is legitimate.
//
// Português:
//
//	Um LEITOR seguro: dado um array e um índice inteiro, devolve o elemento
//	naquela posição. Não tem dados próprios — só lê o que os fios dão —, então,
//	ao contrário das coleções constantes, o Inspect mostra só o manual. É um de
//	TRÊS devices irmãos (Int/Float/String), um por tipo de elemento, sem
//	type-select. Os nomes das 4 portas são um CONTRATO com o IR (ir.emitIndex lê
//	exatamente "array"/"index"/"value"/"ok"). Segurança: o codegen nunca faz
//	subscrito cru — fora do range devolve o zero do tipo. O `ok` é o comma-ok
//	gráfico (opcional). array/index/value soltos são erro de IDE; `ok` solto é
//	legítimo.

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
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
	"github.com/helmutkemper/iotmakerio/utilsDraw"
	"github.com/helmutkemper/iotmakerio/utilsText"
	"github.com/helmutkemper/iotmakerio/wire"
)

// kIndexStringElement is the FIXED element type of this device. GetProperties
// exports it as "elementType"; the array port advertises "[]"+kIndexStringElement,
// and the value port advertises kIndexStringElement.
const kIndexStringElement = "string"

// kIndexBody (the shared "index" body label) is declared once in
// statementIndexInt.go, so all three index readers share it.

// StatementIndexString is the int-element array index reader device.
type StatementIndexString struct {
	stage sprite.Stage
	elem  sprite.Element

	id   string
	name string

	// label is an optional instance label. The reader has NO editable properties
	// (Inspect is manual-only), so label stays empty and the id is shown; it is
	// kept for scene round-trip parity with the other devices.
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
	ctxMenu       *contextMenu.Controller
	wireMgr       *wire.Manager
	gridAdjust    grid.Adjust

	iconStatus  int
	lastClick   time.Time
	sceneNotify func()
	onRemove    func(id string)
}

// ── Dependency injection ──────────────────────────────────────────────────────

func (e *StatementIndexString) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementIndexString) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementIndexString) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementIndexString) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementIndexString) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

func (e *StatementIndexString) SetContextMenu(c *contextMenu.Controller) { e.ctxMenu = c }
func (e *StatementIndexString) SetOnRemove(fn func(id string))           { e.onRemove = fn }

// Port type tokens. The array port carries the slice type ("[]int"); value is
// the element ("int"); index is always "int"; ok is "bool".
func (e *StatementIndexString) arrayType() string { return "[]" + kIndexStringElement }
func (e *StatementIndexString) valueType() string { return kIndexStringElement }
func (e *StatementIndexString) indexType() string { return "int" }
func (e *StatementIndexString) okType() string    { return "bool" }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementIndexString) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementIndexString) Remove() {
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

// ── Connector geometry ────────────────────────────────────────────────────────

// connectorLayout returns the LOCAL positions of the four connectors, so the SVG
// circles (renderSVG), the hit test (connectorAt) and the wire-manager positions
// (RegisterConnectors) all agree. Two on the left (array upper, index lower) and
// two on the right (value upper, ok lower), spread within the body below the
// header.
//
// NOTE: these fractions are the most likely thing to tweak once seen on the
// stage — adjust upperFrac/lowerFrac if the connectors look cramped.
//
// Português: Posições LOCAIS dos quatro conectores, para o SVG, o hit test e o
// wire-manager concordarem. As frações são o que mais provavelmente se ajusta ao
// ver na tela.
func (e *StatementIndexString) connectorLayout() (leftX, rightX, upperY, lowerY float64) {
	w := e.width.GetFloat()
	ornH := e.height.GetFloat() // body height (the label sits below it)
	headerH := float64(rulesDevice.KDeviceHeaderHeight)
	off := float64(rulesDevice.KConnectorOffsetRight)

	leftX = off
	rightX = w - off
	bodyTop := headerH
	bodyH := ornH - headerH
	upperY = bodyTop + bodyH*0.34
	lowerY = bodyTop + bodyH*0.70
	return leftX, rightX, upperY, lowerY
}

// ── SVG rendering ─────────────────────────────────────────────────────────────

func (e *StatementIndexString) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	// The device border/header tag take the VALUE type's colour family (int =
	// blue): the box "is" an int reader. Individual connectors are coloured by
	// their OWN type below.
	ts := rulesDevice.TypeStyleFor(e.valueType())

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

	// Type tag (INT — the element type this reader yields)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">%s</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color, ts.Tag,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, w-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Body label ("index")
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		w/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, escapeXml(kIndexBody),
	)

	// Four connectors, each coloured by its own wire type.
	leftX, rightX, upperY, lowerY := e.connectorLayout()
	r := rulesDevice.KConnectorRadius
	stroke := rulesDevice.KColorConnectorStroke
	circle := func(cx, cy float64, typ string) string {
		c := rulesDevice.TypeStyleFor(typ)
		return fmt.Sprintf(
			`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="%s" stroke-width="1"/>`,
			cx, cy, r, c.Color, stroke)
	}
	svg += circle(leftX, upperY, e.arrayType())  // array (in)
	svg += circle(leftX, lowerY, e.indexType())  // index (in)
	svg += circle(rightX, upperY, e.valueType()) // value (out)
	svg += circle(rightX, lowerY, e.okType())    // ok (out)

	// Label (instance id) below the box
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementIndexString) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementIndexString) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("indexString")
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
		log.Printf("[IndexString] warning: no context menu set — menus disabled")
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

// connectorAt returns the port name whose connector contains the local point
// (lx, ly), or "" if the point is on the body. Checks all four connectors using
// the same layout the renderer draws.
func (e *StatementIndexString) connectorAt(lx, ly float64) string {
	leftX, rightX, upperY, lowerY := e.connectorLayout()
	hit := rulesDevice.KConnectorHitRadius
	within := func(cx, cy float64) bool {
		dx, dy := lx-cx, ly-cy
		return dx*dx+dy*dy <= hit*hit
	}
	switch {
	case within(leftX, upperY):
		return "array"
	case within(leftX, lowerY):
		return "index"
	case within(rightX, upperY):
		return "value"
	case within(rightX, lowerY):
		return "ok"
	}
	return ""
}

func (e *StatementIndexString) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if e.ctxMenu == nil {
			return
		}
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

		if port := e.connectorAt(event.LocalX, event.LocalY); port != "" {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, port), menuX, menuY)
			return
		}

		go e.ctxMenu.OpenForDevice(e, e.bodyMenuItems(), menuX, menuY)
	})

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
		if e.connectorAt(lx, ly) != "" {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── Menu ──────────────────────────────────────────────────────────────────────

// bodyMenuItems returns the body context menu. Delete first, Inspect second —
// canonical order.
func (e *StatementIndexString) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[IndexString] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementIndexString) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

// inspectConfig — the reader has NO editable data, so Inspect is a single Help
// tab (the manual). This is decision "Inspect = só o manual".
func (e *StatementIndexString) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{Label: "Help", Type: overlay.TabMarkdown, Content: indexStringHelp()},
		},
	}
}

func (e *StatementIndexString) GetInspectConfig() interface{}       { return e.inspectConfig() }
func (e *StatementIndexString) ApplyProperties(_ map[string]string) {} // no editable properties

// ── Wire connectors ───────────────────────────────────────────────────────────

func (e *StatementIndexString) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}

	// Each connector's world position is the element origin plus its LOCAL
	// layout offset, so registration matches exactly what renderSVG draws.
	pos := func(right, lower bool) func() (float64, float64) {
		return func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			leftX, rightX, upperY, lowerY := e.connectorLayout()
			x, y := leftX, upperY
			if right {
				x = rightX
			}
			if lower {
				y = lowerY
			}
			return ex + x, ey + y
		}
	}

	// array — left upper, required
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "array"},
		IsOutput:           false,
		AllowedTypes:       []string{e.arrayType()},
		AcceptNotConnected: false,
		MaxConnections:     1,
		Label:              "Array",
		PositionFunc:       pos(false, false),
	})

	// index — left lower, required
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "index"},
		IsOutput:           false,
		AllowedTypes:       []string{e.indexType()},
		AcceptNotConnected: false,
		MaxConnections:     1,
		Label:              "Index",
		PositionFunc:       pos(false, true),
	})

	// value — right upper, required (a reader whose value is unused is a mistake)
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "value"},
		IsOutput:           true,
		AllowedTypes:       []string{e.valueType()},
		AcceptNotConnected: false,
		MaxConnections:     0, // unlimited fan-out
		Label:              "Value",
		PositionFunc:       pos(true, false),
	})

	// ok — right lower, OPTIONAL (the comma-ok; unwired is legitimate)
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "ok"},
		IsOutput:           true,
		AllowedTypes:       []string{e.okType()},
		AcceptNotConnected: true,
		MaxConnections:     0,
		Label:              "Ok",
		PositionFunc:       pos(true, true),
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementIndexString) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementIndexString) Get() *html.TagSvg { return nil }
func (e *StatementIndexString) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementIndexString) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementIndexString) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementIndexString) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementIndexString) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementIndexString) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementIndexString) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementIndexString) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementIndexString) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementIndexString) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementIndexString) MoveBy(dx, dy float64) {
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

func (e *StatementIndexString) GetInitialized() bool   { return e.initialized }
func (e *StatementIndexString) GetID() string          { return e.id }
func (e *StatementIndexString) GetName() string        { return e.name }
func (e *StatementIndexString) GetSelected() bool      { return e.selected }
func (e *StatementIndexString) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementIndexString) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementIndexString) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementIndexString) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementIndexString) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementIndexString) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementIndexString) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementIndexString) GetStatus() int  { return e.iconStatus }
func (e *StatementIndexString) SetStatus(s int) { e.iconStatus = s }
func (e *StatementIndexString) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementIndexString) SetSelected(sel bool) {
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

func (e *StatementIndexString) SetDragEnable(en bool) {
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

func (e *StatementIndexString) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementIndexString) GetIconName() string     { return "IndexString" }
func (e *StatementIndexString) GetIconCategory() string { return "Array" }

func (e *StatementIndexString) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 5).
		Text("[i]").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 14).Y((rulesIcon.Height / 2).GetInt() + 4)
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

func (e *StatementIndexString) GetDeviceType() string { return "StatementIndexString" }
func (e *StatementIndexString) GetProperties() map[string]interface{} {
	// elementType mirrors the sibling collection devices. The IR generator does
	// not need it (it derives the element type from the node type), but keeping
	// it makes the scene self-describing and consistent.
	return map[string]interface{}{
		"elementType": kIndexStringElement,
		"label":       e.label,
	}
}
func (e *StatementIndexString) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementIndexString) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementIndexString) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementIndexString) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help ──────────────────────────────────────────────────────────────────────

func indexStringHelp() string {
	return `# Index (string) — Read an item from a collection

Reads the item at a given **position** from a text (string) collection and outputs
it. It is safe by design: if the position is outside the collection, you get
an empty string instead of a crash.

## Inputs

| Port  | Type    | Required | Meaning                          |
|-------|---------|----------|----------------------------------|
| array | ` + "`[]string`" + ` | yes      | the collection to read from      |
| index | ` + "`int`" + `   | yes      | the position (0 is the first)    |

## Outputs

| Port  | Type   | Meaning                                             |
|-------|--------|-----------------------------------------------------|
| value | ` + "`string`" + `  | the item at that position — an **empty string** if out of range   |
| ok    | ` + "`bool`" + ` | **true** if the position was valid, else **false**  |

## The ` + "`ok`" + ` output is optional

Wire ` + "`ok`" + ` when you want to react to a bad position (light an alert, stop a
loop). Leave it unwired and you are still safe — you simply get an empty string for an
out-of-range position.

## Rules

- A **negative** index counts as out of range (you get an empty string, and ` + "`ok`" + ` is false).
- **array**, **index** and **value** must be connected. Only **ok** may be left
  unconnected.

## Tips

- Wire **array** from a collection device, and **index** from a number.
- Sibling devices exist for **int** and **float** collections.
`
}
