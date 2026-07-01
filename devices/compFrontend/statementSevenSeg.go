// /ide/devices/compFrontend/statementSevenSeg.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
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

// =====================================================================
//  Seven-segment digit rendering
//
//  Segment layout for each digit:
//
//       ___a___
//      |       |
//      f       b
//      |___g___|
//      |       |
//      e       c
//      |___d___|
//
//  Each segment is a trapezoid rendered as an SVG polygon.
//  Active segments use onColor, inactive use a very dim version.
// =====================================================================

// segmentMap maps digit 0-9 to active segments (bitmask: a=1,b=2,c=4,d=8,e=16,f=32,g=64)
var segmentMap = [10]byte{
	0x3F, // 0: a,b,c,d,e,f
	0x06, // 1: b,c
	0x5B, // 2: a,b,d,e,g
	0x4F, // 3: a,b,c,d,g
	0x66, // 4: b,c,f,g
	0x6D, // 5: a,c,d,f,g
	0x7D, // 6: a,c,d,e,f,g
	0x07, // 7: a,b,c
	0x7F, // 8: a,b,c,d,e,f,g
	0x6F, // 9: a,b,c,d,f,g
}

// segMinus is the bitmask for a minus sign (only segment g).
const segMinus byte = 0x40

// renderDigit generates SVG polygon elements for one seven-segment digit.
// ox,oy is the top-left origin of the digit cell.
// w,h are the digit cell dimensions. sw is the segment stroke width.
// mask is the bitmask of active segments.
// onColor is the fill for active segments, offColor for inactive.
func renderDigit(ox, oy, w, h, sw float64, mask byte, onColor, offColor string) string {
	// Segment geometry: each segment is a thin rectangle with angled ends.
	// Using simple rects with rounded ends for clean rendering.
	hx := w * 0.12 // horizontal inset
	vy := h * 0.06 // vertical inset
	segW := w - 2*hx
	segH := h/2 - vy

	svg := ""

	type seg struct {
		bit     byte
		x, y    float64
		rw, rh  float64
		isHoriz bool
	}

	// Horizontal segments: a (top), g (middle), d (bottom)
	// Vertical segments: f (top-left), b (top-right), e (bottom-left), c (bottom-right)
	segs := []seg{
		{0x01, ox + hx, oy, segW, sw, true},                           // a: top
		{0x02, ox + w - hx - sw, oy + vy, sw, segH - vy, false},       // b: top-right
		{0x04, ox + w - hx - sw, oy + h/2 + vy, sw, segH - vy, false}, // c: bottom-right
		{0x08, ox + hx, oy + h - sw, segW, sw, true},                  // d: bottom
		{0x10, ox + hx, oy + h/2 + vy, sw, segH - vy, false},          // e: bottom-left
		{0x20, ox + hx, oy + vy, sw, segH - vy, false},                // f: top-left
		{0x40, ox + hx, oy + h/2 - sw/2, segW, sw, true},              // g: middle
	}

	for _, s := range segs {
		color := offColor
		if mask&s.bit != 0 {
			color = onColor
		}
		rx := 2.0
		if !s.isHoriz {
			rx = 1.5
		}
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" fill="%s"/>`,
			s.x, s.y, s.rw, s.rh, rx, color)
	}

	return svg
}

// =====================================================================
//  Default colors
// =====================================================================

const (
	kSevenSegDefaultOnColor  = "#FF3333" // classic red LED
	kSevenSegDefaultOffColor = "#222222" // dark grey — all segments visible as ghost traces
	kSevenSegDefaultBg       = "#0a0a0a" // near-black background
)

// StatementSevenSeg — dual device: backend data node + frontend 7-segment display.
//
// Backend: compact box with 1 input connector (current, int).
// Frontend: classic LCD-style seven-segment display showing the current
// integer value. The number of digits is configurable (default: 3).
// Negative values show a minus sign in the leftmost position.
//
// Português: Display LCD clássico de 7 segmentos. Número de dígitos
// configurável. Valores negativos mostram sinal de menos.
type StatementSevenSeg struct {
	backendStage  sprite.Stage
	frontendStage sprite.Stage
	backendElem   sprite.Element
	frontendElem  sprite.Element

	name         string
	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool

	backendWidth  rulesDensity.Density
	backendHeight rulesDensity.Density

	frontendWidth  rulesDensity.Density
	frontendHeight rulesDensity.Density

	pendingDragEnable *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller for the
	// backend stage. Devices without a frontend menu only
	// need this one.
	backendCtxMenu *contextMenu.Controller
	wireMgr        *wire.Manager

	label    string
	canvasEl js.Value

	currentValue int64

	// digits is the number of digit positions to display (default: 3).
	// Determines the frontend width. Values wider than digits are clamped
	// to all-9s (e.g. 3 digits → max 999, min -99).
	digits int

	// Visual customization
	onColor  string
	offColor string
	bgColor  string

	interactionLocked bool

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
	sceneNotify func()
	onRemove    func(id string)

	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementSevenSeg) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementSevenSeg) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementSevenSeg) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementSevenSeg) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementSevenSeg) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementSevenSeg) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementSevenSeg) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementSevenSeg) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementSevenSeg) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementSevenSeg) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	if e.backendElem != nil {
		e.backendElem.SetVisible(false)
		elem := e.backendElem
		e.backendElem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(false)
		elem := e.frontendElem
		e.frontendElem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
}

func (e *StatementSevenSeg) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementSevenSeg) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementSevenSeg) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementSevenSeg) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementSevenSeg) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementSevenSeg) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementSevenSeg) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementSevenSeg) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementSevenSeg) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG — compact box with 1 input connector
//
//	┌───────────────────┐  ← blue border (int type)
//	│ ◉ 7SEG       123  │
//	└───────────────────┘
//	seg1
// =====================================================================

const backendSegLabelHeight = 18

func (e *StatementSevenSeg) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendSegLabelHeight
}

func (e *StatementSevenSeg) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendSegLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	connY := boxH / 2.0
	borderColor := rulesDevice.KColorTypeInt

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, boxH-bw, rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, borderColor, bw)
	svg += fmt.Sprintf(`<circle cx="%.0f" cy="%.1f" r="%.0f" fill="%s" stroke="%s" stroke-width="1"/>`,
		rulesDevice.KConnectorOffsetLeft, connY, rulesDevice.KConnectorRadius, borderColor, rulesDevice.KColorConnectorStroke)
	svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">7SEG</text>`,
		connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central" font-weight="bold">%d</text>`,
		w-12, connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue, rulesDevice.KColorDeviceText, e.currentValue)

	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="12" font-family="%s" font-size="10" fill="#FF8833" text-anchor="end">🔒</text>`, w-4, rulesDevice.KDeviceFontFamily)
	}

	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, boxH+3, displayLabel)
	svg += `</svg>`
	return svg
}

// =====================================================================
//  Frontend SVG — seven-segment display
//
//  Rendered at 3x for crisp segment edges.
//  Background is near-black with a subtle bezel border.
//  Each digit cell is rendered via renderDigit().
// =====================================================================

func (e *StatementSevenSeg) renderFrontendSVG() string {
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	digits := e.digits
	if digits < 1 {
		digits = 3
	}

	// Layout
	padX := 12.0
	padY := 12.0
	digitGap := 8.0
	availW := w - 2*padX - float64(digits-1)*digitGap
	digitW := availW / float64(digits)
	digitH := h - 2*padY
	segStroke := digitW * 0.16

	// Clamp value to fit digits (e.g. 3 digits → -99..999)
	maxPos := int64(math.Pow10(digits)) - 1
	maxNeg := -(int64(math.Pow10(digits-1)) - 1)
	val := e.currentValue
	if val > maxPos {
		val = maxPos
	}
	if val < maxNeg {
		val = maxNeg
	}

	onColor := e.onColor
	if onColor == "" {
		onColor = kSevenSegDefaultOnColor
	}
	offColor := e.offColor
	if offColor == "" {
		offColor = kSevenSegDefaultOffColor
	}
	bgColor := e.bgColor
	if bgColor == "" {
		bgColor = kSevenSegDefaultBg
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background with bezel
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="6" ry="6" fill="%s"/>`, int(w), int(h), bgColor)
	svg += fmt.Sprintf(`<rect x="2" y="2" width="%d" height="%d" rx="4" ry="4" fill="none" stroke="#222" stroke-width="2"/>`, int(w)-4, int(h)-4)

	// Decompose value into digit masks
	negative := val < 0
	if negative {
		val = -val
	}

	masks := make([]byte, digits)
	// Fill from right to left with actual digits
	for i := digits - 1; i >= 0; i-- {
		if val > 0 || i == digits-1 {
			d := val % 10
			masks[i] = segmentMap[d]
			val /= 10
		} else if negative {
			// Place minus sign in the position just left of the first digit
			masks[i] = segMinus
			negative = false // only one minus sign
		}
		// Remaining positions stay 0x00 (all segments off = blank)
	}

	// Render each digit
	for i := 0; i < digits; i++ {
		ox := padX + float64(i)*(digitW+digitGap)
		oy := padY
		svg += renderDigit(ox, oy, digitW, digitH, segStroke, masks[i], onColor, offColor)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementSevenSeg) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementSevenSeg) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementSevenSeg) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("seg")
	e.backendWidth = 120
	e.backendHeight = 36
	e.digits = 3
	e.frontendWidth = rulesDensity.Density(30*e.digits + 20)
	e.frontendHeight = 60
	e.currentValue = 0
	e.onColor = kSevenSegDefaultOnColor
	e.offColor = kSevenSegDefaultOffColor
	e.bgColor = kSevenSegDefaultBg
	e.resizeLocked = true

	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id

	if e.backendStage != nil {
		totalH := e.backendTotalHeight()
		e.backendElem, err = e.backendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_back", X: 0, Y: 0,
			Width: e.backendWidth.GetFloat(), Height: totalH.GetFloat(),
			Index: rulesZIndex.Display, DragEnable: false, SvgXml: e.renderBackendSVG(),
		})
		if err != nil {
			return fmt.Errorf("backend element: %w", err)
		}
		e.backendElem.SetMinSizeD(80, 36+backendSegLabelHeight)
		e.wireBackendEvents()
	}

	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front", X: 100, Y: 100,
			Width: e.frontendWidth.GetFloat(), Height: e.frontendHeight.GetFloat(),
			Index: rulesZIndex.DisplayFrontend, DragEnable: false, SvgXml: e.renderFrontendSVG(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(60, 40)
		e.wireFrontendEvents()
	}

	if e.backendCtxMenu == nil {
		log.Printf("[SevenSeg] Warning: no shared hex menu set, menus disabled")
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

// =====================================================================
//  Backend events
// =====================================================================

func (e *StatementSevenSeg) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}

		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendSegLabelHeight)
		connY := boxH / 2.0
		elemX, elemY := e.backendElem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY

		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}

		if event.LocalY > boxH {
			return
		}

		dx := event.LocalX - rulesDevice.KConnectorOffsetLeft
		dy := event.LocalY - connY
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, "current"), menuX, menuY)
			return
		}

		go e.backendCtxMenu.OpenAtWorld(e.getBackendMenuItems(), menuX, menuY)
	})

	// [SCENE] real-time conflict feedback — notify scene
	// on every drag step so the stage-level overlay reacts
	// to position changes immediately, not only on release.
	e.backendElem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.backendElem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.backendElem.GetPositionD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.backendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendSegLabelHeight)
		connY := boxH / 2.0
		if ly > boxH {
			return ""
		}
		dx := lx - rulesDevice.KConnectorOffsetLeft
		dy := ly - connY
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			return sprite.CursorPointer
		}
		return ""
	})
}

// =====================================================================
//  Frontend events — display only (no click interaction)
// =====================================================================

func (e *StatementSevenSeg) wireFrontendEvents() {
	// [SCENE] real-time conflict feedback — notify scene
	// on every drag step so the stage-level overlay reacts
	// to position changes immediately, not only on release.
	e.frontendElem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.frontendElem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.frontendElem.GetPositionD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.frontendElem.SetPositionD(nx, ny)
	})
}

// =====================================================================
//  Hex menu — backend only
// =====================================================================

// getBackendMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementSevenSeg) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[SevenSeg] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[SevenSeg] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay
// =====================================================================

func (e *StatementSevenSeg) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementSevenSeg) GetInspectConfig() interface{} {
	lockValue := "false"
	if e.interactionLocked {
		lockValue = "true"
	}

	return overlay.Config{
		Title: fmt.Sprintf("%s", e.id),
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
					{Key: "current", Label: translate.T("propValue", "Value"), Type: overlay.FieldNumber, Value: strconv.FormatInt(e.currentValue, 10), Placeholder: "0"},
					{Key: "digits", Label: translate.T("propDigits", "Digits"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.digits), Placeholder: "3", Min: "1", Max: "10"},
					{Key: "onColor", Label: translate.T("propOnColor", "On Color"), Type: overlay.FieldColor, Value: e.onColor},
					{Key: "offColor", Label: translate.T("propOffColor", "Off Color"), Type: overlay.FieldColor, Value: e.offColor},
					{Key: "bgColor", Label: translate.T("propBgColor", "Background"), Type: overlay.FieldColor, Value: e.bgColor},
					{Key: "interactionLocked", Label: translate.T("propLockInteraction", "Lock Interaction"), Type: overlay.FieldCheckbox, Value: lockValue},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementSevenSeg.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementSevenSeg) ApplyProperties(values map[string]string) {
	changed := false

	if v, ok := values["id"]; ok && v != "" && v != e.id {
		oldID := e.id
		if e.wireMgr != nil {
			e.wireMgr.UnregisterElement(oldID)
		}
		e.id = v
		if e.label == oldID {
			e.label = v
		}
		e.RegisterConnectors()
		changed = true
	}
	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}
	if v, ok := values["current"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != e.currentValue {
			e.currentValue = n
			changed = true
		}
	}
	if v, ok := values["digits"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 10 && n != e.digits {
			e.digits = n
			// Resize frontend element to match new digit count
			e.frontendWidth = rulesDensity.Density(30*n + 20)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			changed = true
			log.Printf("[SevenSeg] %s: digits set to %d, width=%.0f", e.id, n, e.frontendWidth.GetFloat())
		}
	}
	if v, ok := values["onColor"]; ok && v != "" && v != e.onColor {
		e.onColor = v
		changed = true
	}
	if v, ok := values["offColor"]; ok && v != "" && v != e.offColor {
		e.offColor = v
		changed = true
	}
	if v, ok := values["bgColor"]; ok && v != "" && v != e.bgColor {
		e.bgColor = v
		changed = true
	}
	if v, ok := values["interactionLocked"]; ok {
		newLocked := v == "true"
		if newLocked != e.interactionLocked {
			e.interactionLocked = newLocked
			changed = true
		}
	}

	if changed {
		go func() {
			time.Sleep(200 * time.Millisecond)
			e.recacheBackend()
			e.recacheFrontend()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	}
}

// =====================================================================
//  Wire registration — 1 input port (int)
// =====================================================================

func (e *StatementSevenSeg) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "current"},
		IsOutput:           false,
		AllowedTypes:       []string{"int"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     1,
		Label:              "current",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.backendElem.GetPosition()
			_, h := e.backendElem.GetSize()
			boxH := h - float64(backendSegLabelHeight)
			return ex + rulesDevice.KConnectorOffsetLeft, ey + boxH/2
		},
	})
}

// =====================================================================
//  Live communication
// =====================================================================

func (e *StatementSevenSeg) LiveUpdate(port string, value []byte) error {
	if port != "current" {
		return fmt.Errorf("sevenseg %s: unknown port %q", e.id, port)
	}
	var num float64
	if err := json.Unmarshal(value, &num); err != nil {
		var s string
		if err2 := json.Unmarshal(value, &s); err2 != nil {
			return fmt.Errorf("sevenseg %s: cannot parse value: %w", e.id, err)
		}
		parsed, err3 := strconv.ParseFloat(s, 64)
		if err3 != nil {
			return fmt.Errorf("sevenseg %s: cannot parse string value %q: %w", e.id, s, err3)
		}
		num = parsed
	}

	e.currentValue = int64(num)
	log.Printf("[SevenSeg:%s] LiveUpdate value=%d", e.id, e.currentValue)
	e.recacheBackend()
	e.recacheFrontend()
	return nil
}

func (e *StatementSevenSeg) SendValue(port string, value int64) {
	if e.SendFunc == nil || e.interactionLocked {
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  Serialization
// =====================================================================

func (e *StatementSevenSeg) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"label":             e.label,
		"current":           e.currentValue,
		"digits":            e.digits,
		"onColor":           e.onColor,
		"offColor":          e.offColor,
		"bgColor":           e.bgColor,
		"interactionLocked": e.interactionLocked,
	}
}

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementSevenSeg) GetInitialized() bool   { return e.initialized }
func (e *StatementSevenSeg) GetID() string          { return e.id }
func (e *StatementSevenSeg) GetName() string        { return e.name }
func (e *StatementSevenSeg) GetSelected() bool      { return e.selected }
func (e *StatementSevenSeg) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementSevenSeg) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementSevenSeg) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementSevenSeg) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementSevenSeg) GetResize() bool        { return false }
func (e *StatementSevenSeg) GetResizeEnable() bool  { return false }
func (e *StatementSevenSeg) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementSevenSeg) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementSevenSeg) SetDragEnable(en bool) {
	e.dragEnabled = en
	if e.backendElem == nil {
		e.pendingDragEnable = &en
		return
	}
	e.backendElem.SetDragEnable(en)
	if e.frontendElem != nil {
		e.frontendElem.SetDragEnable(en)
	}
}

func (e *StatementSevenSeg) SetResizeEnable(_ bool) {}
func (e *StatementSevenSeg) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementSevenSeg) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementSevenSeg) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementSevenSeg) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementSevenSeg) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementSevenSeg) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementSevenSeg) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementSevenSeg) GetStatus() int                                         { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementSevenSeg) GetIconName() string     { return "7Seg" }
func (e *StatementSevenSeg) GetIconCategory() string { return "Display" }

func (e *StatementSevenSeg) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 5).
		Text("88").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 10).Y((rulesIcon.Height / 2).GetInt() + 5)

	wl, _ := utilsText.GetTextSize(data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight, rulesIcon.FontStyle, data.LabelFontSize.GetInt())
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).FontWeight(rulesIcon.FontWeight).FontStyle(rulesIcon.FontStyle).
		FontSize(data.LabelFontSize.GetInt()).Text(data.Label).Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - wl/2).Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, iconLabel, label)
	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}

// =====================================================================
//  Scene export
// =====================================================================

func (e *StatementSevenSeg) GetDeviceType() string { return "StatementSevenSeg" }
func (e *StatementSevenSeg) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementSevenSeg) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementSevenSeg) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementSevenSeg) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementSevenSeg) GetLabel() string         { return e.label }
func (e *StatementSevenSeg) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementSevenSeg) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}
