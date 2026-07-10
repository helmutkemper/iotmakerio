// /ide/devices/compConsts/statementBool.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compConsts

// statementBool.go — Boolean constant device.
//
// Visual design:
//
//	┌─────────────────────┐  ← 2px border, 5px corner, orange (#FF8833)
//	│ BOOL            ◉   │  ← 18px header; ◉ = output connector (orange)
//	├─────────────────────┤
//	│                     │
//	│        true         │  ← value: green when true, red when false
//	│                     │
//	└─────────────────────┘
//	bool1                   ← label
//
// Body click:      Inspect · Delete
// Connector click: Connect  (output-only)
// Double-click:    toggles value (fast workflow for makers)

import (
	"fmt"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
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

// kColorBoolTrue is the fill color for "true" text — bright green signals ON.
const kColorBoolTrue = "#44DD88"

// kColorBoolFalse is the fill color for "false" text — muted red signals OFF.
const kColorBoolFalse = "#FF5555"

// StatementBool is a boolean constant device.
// No inputs — single output connector that emits bool.
type StatementBool struct {
	stage sprite.Stage
	elem  sprite.Element

	id      string
	name    string
	value   bool
	label   string
	comment string // optional; written as // lines above the declaration in generated code

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
	// [SCENEGRAPH] injected by scene.Serializer.Register (self-injection by
	// interface assertion). DragEnd reports through it so the scenegraph
	// refreshes geometry, recomputes conflicts (own + peers) and reassigns
	// parenting — the same EndDrag hook the containers use.
	// Português: Injetado pelo scene.Serializer.Register (auto-injeção por
	// assertion). O DragEnd reporta por ele para o scenegraph refrescar
	// geometria, recomputar conflitos (próprios + peers) e reatribuir
	// parenting — o mesmo gancho EndDrag dos containers.
	sceneMgr *scene.Serializer
	onRemove func(id string)
}

// ── Dependency injection ──────────────────────────────────────────────────────

func (e *StatementBool) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementBool) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementBool) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementBool) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementBool) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementBool) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementBool) SetValue(v bool) {
	e.value = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementBool) GetValue() bool                 { return e.value }
func (e *StatementBool) SetOnRemove(fn func(id string)) { e.onRemove = fn }
func (e *StatementBool) GetComment() string             { return e.comment }
func (e *StatementBool) SetComment(c string)            { e.comment = c }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementBool) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementBool) Remove() {
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

func (e *StatementBool) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	// [PIN] the body is inset on the right by the pin length: the standard
	// connector pin lives in the freed margin, protruding from the border
	// with the wire anchored at its outer tip. The element size itself is
	// unchanged, so grid layout and saved scenes are unaffected.
	// Português: O corpo recua à direita o comprimento do pino: o pino
	// padrão vive na margem liberada, saindo da borda com o fio ancorado na
	// ponta externa. O tamanho do element não muda — grid e cenas salvas não
	// são afetados.
	pin := rulesConnection.PinBodyInset()
	bodyR := w - pin
	ts := rulesDevice.TypeStyleFor("bool")

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, bodyR-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, bodyR-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, bodyR-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// Type tag
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">%s</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color, ts.Tag,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, bodyR-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Value text — color changes with value
	valueStr := "false"
	valueColor := kColorBoolFalse
	if e.value {
		valueStr = "true"
		valueColor = kColorBoolTrue
	}
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		bodyR/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		valueColor, valueStr,
	)

	// Output connector
	// [PIN] the standard connector pin at the body's right border, filled
	// with the type color so pin and wire read as one continuous piece; the
	// wire anchors at the pin's outer tip (see RegisterConnectors).
	// Português: O pino padrão na borda direita do corpo, preenchido com a
	// cor do tipo para pino e fio lerem como uma peça contínua; o fio ancora
	// na ponta externa do pino (ver RegisterConnectors).
	svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideRight, bodyR, h/2, ts.Color)

	// Label
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementBool) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementBool) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("bool")
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
		log.Printf("[Bool] warning: no context menu set — menus disabled")
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

func (e *StatementBool) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if e.ctxMenu == nil {
			return
		}
		// Close-then-return — first click dismisses, next opens.
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX := elemX + event.LocalX
		menuY := elemY + event.LocalY

		// Double-click → toggle value (quick workflow for makers).
		now := time.Now()
		if now.Sub(e.lastClick) < 300*time.Millisecond {
			e.lastClick = time.Time{}
			e.value = !e.value
			go e.recacheSVG()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
			return
		}
		e.lastClick = now

		// Connector hit test.
		// [PIN] standard pin hit box — same edge point the renderer draws
		// and the wire anchors to, so click, drawing and wire agree.
		// Português: Caixa de clique do pino padrão — mesmo edge point que o
		// renderer desenha e onde o fio ancora; clique, desenho e fio
		// concordam.
		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2,
			event.LocalX, event.LocalY) {
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

	e.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		// [PIN] same hit box as the click handler — one geometry source.
		// Português: Mesma caixa do handler de clique — uma fonte de
		// geometria só.
		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2, lx, ly) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── Menu ──────────────────────────────────────────────────────────────────────

// bodyMenuItems returns the body context menu. Three items:
// Delete, Inspect, and a Toggle that flips the boolean value.
// Order canonicalised per D4 (Delete first, Inspect second).
//
// Português: Itens do menu do corpo: Delete, Inspect, Toggle.
func (e *StatementBool) bodyMenuItems() []contextMenu.Item {
	// Toggle label changes to reflect the current value so the
	// user sees "what will happen" not "what is".
	toggleLabel := translate.T("menuBoolSetTrue", "Set True")
	if e.value {
		toggleLabel = translate.T("menuBoolSetFalse", "Set False")
	}
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[Bool] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
		{
			ID:              "toggle",
			Label:           toggleLabel,
			FontAwesomePath: rulesIcon.KFAToggleOn,
			ViewBox:         "0 0 576 512",
			HelpKey:         "helpMenuBoolToggle",
			HelpFallback:    "Flips this boolean constant between true and false. The label shows which value will be set next.",
			OnClick: func() {
				e.value = !e.value
				go e.recacheSVG()
				if e.sceneNotify != nil {
					e.sceneNotify()
				}
			},
		},
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementBool) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementBool) inspectConfig() overlay.Config {
	valueStr := "false"
	if e.value {
		valueStr = "true"
	}
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:   "value",
						Label: translate.T("propValue", "Value"),
						Type:  overlay.FieldSelect,
						Value: valueStr,
						Options: []overlay.Option{
							{Value: "false", Label: "false"},
							{Value: "true", Label: "true"},
						},
					},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
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
			{
				Label:    "Code Preview",
				Type:     overlay.TabMonaco,
				Content:  e.codePreview(),
				Language: "go",
				ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: constBoolHelp()},
		},
		OnSave: func(values map[string]string) {
			if v, ok := values["value"]; ok {
				e.value = v == "true"
			}
			if lbl, ok := values["label"]; ok {
				e.label = lbl
			}
			if c, ok := values["comment"]; ok {
				e.comment = c
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

// codePreview returns a Go snippet showing how this device will appear in
// the generated source, including any user comment lines.
func (e *StatementBool) codePreview() string {
	code := ""
	if e.comment != "" {
		for _, line := range strings.Split(e.comment, "\n") {
			code += "// " + line + "\n"
		}
	}
	code += fmt.Sprintf("%s := %v", e.id, e.value)
	return code
}

func (e *StatementBool) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementBool) ApplyProperties(values map[string]string) {
	if v, ok := values["value"]; ok {
		e.value = v == "true"
	}
	if lbl, ok := values["label"]; ok {
		e.label = lbl
	}
	if c, ok := values["comment"]; ok {
		e.comment = c
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

func (e *StatementBool) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput:           true,
		AllowedTypes:       []string{"bool"},
		AcceptNotConnected: true,
		MaxConnections:     0,
		Label:              "Output",
		PositionFunc: func() (float64, float64) {
			// [PIN] the wire anchors at the OUTER TIP of the standard pin —
			// exactly the element's right edge, vertically centered on the
			// ornament.
			// Português: O fio ancora na PONTA EXTERNA do pino padrão —
			// exatamente a borda direita do element, centrado verticalmente
			// no ornamento.
			ex, ey := e.elem.GetPosition()
			w := e.elem.GetWidthD().GetFloat()
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideRight,
				w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2)
			return ex + ax, ey + ay
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementBool) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementBool) Get() *html.TagSvg { return nil }
func (e *StatementBool) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementBool) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementBool) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementBool) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementBool) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementBool) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementBool) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementBool) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementBool) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementBool) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementBool) MoveBy(dx, dy float64) {
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

func (e *StatementBool) GetInitialized() bool   { return e.initialized }
func (e *StatementBool) GetID() string          { return e.id }
func (e *StatementBool) GetName() string        { return e.name }
func (e *StatementBool) GetSelected() bool      { return e.selected }
func (e *StatementBool) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementBool) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementBool) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementBool) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementBool) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementBool) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementBool) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementBool) GetStatus() int  { return e.iconStatus }
func (e *StatementBool) SetStatus(s int) { e.iconStatus = s }
func (e *StatementBool) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementBool) SetSelected(sel bool) {
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

func (e *StatementBool) SetDragEnable(en bool) {
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

func (e *StatementBool) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementBool) GetIconName() string     { return "Bool" }
func (e *StatementBool) GetIconCategory() string { return "Constants" }

func (e *StatementBool) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("T/F").Fill(data.ColorIcon).
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

func (e *StatementBool) GetDeviceType() string { return "StatementBool" }
func (e *StatementBool) GetProperties() map[string]interface{} {
	props := map[string]interface{}{"value": e.value, "label": e.label}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}
func (e *StatementBool) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementBool) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementBool) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementBool) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help ──────────────────────────────────────────────────────────────────────

func constBoolHelp() string {
	return `# ConstBool — Boolean Constant

Outputs a fixed **bool** value to any connected device.

## Properties

| Property | Type   | Description                  |
|----------|--------|------------------------------|
| Value    | bool   | true or false                |
| Label    | string | Name shown below the device  |

## Output

| Port   | Type |
|--------|------|
| output | bool |

## Tips

- **Double-click** the device body to **toggle** the value instantly.
- Use the **Set True / Set False** item in the body menu.
- Connect to **Loop.stop**, **If.condition**, or any bool input.
`
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementBool) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
