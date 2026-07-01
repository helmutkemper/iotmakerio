// /ide/devices/compFrontend/statementBarGraph.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

import (
	"encoding/json"
	"fmt"
	"log"
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
//  Default colors
// =====================================================================

const (
	// kBarDefaultFillColor is the default bar fill — blue that matches int type.
	kBarDefaultFillColor = "#5599FF"

	// kBarDefaultBgColor is the default bar background — dark track.
	kBarDefaultBgColor = "#252a3e"
)

// StatementBarGraph — dual device: backend data node + frontend bar indicator.
//
// Backend: compact box with 3 input connectors (max, value, min) — same
// layout as the Gauge but oriented as a vertical bar on the frontend.
// Frontend: vertical bar that fills from bottom to top proportionally.
//
// When the user clicks the frontend bar (and interactionLocked is false),
// a slider overlay opens allowing them to send values to hardware.
//
// Português: Dispositivo dual — nó de dados no backend + barra vertical
// visual no frontend. Clique no frontend abre slider (bloqueável via
// Lock Interaction).
type StatementBarGraph struct {
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

	// Values
	minValue     int64
	maxValue     int64
	currentValue int64

	// Visual customization
	fillColor string
	bgColor   string

	// interactionLocked — standard compFrontend pattern. See readme.md.
	interactionLocked bool

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
	sceneNotify func()
	onRemove    func(id string)

	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementBarGraph) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementBarGraph) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementBarGraph) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementBarGraph) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementBarGraph) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementBarGraph) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementBarGraph) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementBarGraph) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementBarGraph) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementBarGraph) Remove() {
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

func (e *StatementBarGraph) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementBarGraph) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementBarGraph) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}

func (e *StatementBarGraph) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementBarGraph) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}

func (e *StatementBarGraph) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementBarGraph) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementBarGraph) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementBarGraph) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG — same 3-row layout as Gauge (Max / Value / Min)
//
//	┌───────────────────┐
//	│ ◉ Max          100│
//	├───────────────────┤
//	│ ◉ Value         50│
//	├───────────────────┤
//	│ ◉ Min            0│
//	└───────────────────┘
//	bar1
// =====================================================================

const backendBarLabelHeight = 18

func (e *StatementBarGraph) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendBarLabelHeight
}

func (e *StatementBarGraph) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendBarLabelHeight)
	rowH := boxH / 3.0
	bw := rulesDevice.KDeviceBorderWidth

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))

	// Outer rectangle — blue border (int type)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, boxH-bw,
		rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, rulesDevice.KColorTypeInt, bw)

	// Horizontal dividers
	svg += fmt.Sprintf(`<line x1="2" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`, rowH, w-2, rowH, rulesDevice.KColorDeviceDivider)
	svg += fmt.Sprintf(`<line x1="2" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`, 2*rowH, w-2, 2*rowH, rulesDevice.KColorDeviceDivider)

	type rowData struct {
		label string
		value int64
		y     float64
	}
	rows := []rowData{
		{"Max", e.maxValue, rowH / 2},
		{"Current", e.currentValue, rowH + rowH/2},
		{"Min", e.minValue, 2*rowH + rowH/2},
	}

	for _, r := range rows {
		// Input connector circle
		svg += fmt.Sprintf(`<circle cx="%.0f" cy="%.1f" r="%.0f" fill="%s" stroke="%s" stroke-width="1"/>`,
			rulesDevice.KConnectorOffsetLeft, r.y,
			rulesDevice.KConnectorRadius, rulesDevice.KColorTypeInt, rulesDevice.KColorConnectorStroke)
		// Label
		svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central">%s</text>`,
			r.y, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted, r.label)
		// Value
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central" font-weight="bold">%d</text>`,
			w-12, r.y, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizePort, rulesDevice.KColorDeviceText, r.value)
	}

	// Lock icon
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="12" font-family="%s" font-size="10" fill="#FF8833" text-anchor="end">🔒</text>`, w-4, rulesDevice.KDeviceFontFamily)
	}

	// Label below
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, boxH+3, displayLabel)

	svg += `</svg>`
	return svg
}

// =====================================================================
//  Frontend SVG — vertical bar with fill from bottom to top
//
//  Rendered at 3x resolution for crisp edges.
//  The bar sits inside a rounded-rect track with min/max labels and a
//  numeric value display centered on the bar.
// =====================================================================

func (e *StatementBarGraph) renderFrontendSVG() string {
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	// Layout constants (scaled)
	pad := 16.0    // padding around the bar track
	labelH := 40.0 // height reserved for "BAR" label at top
	rangeH := 30.0 // height reserved for min/max labels at bottom
	trackX := pad
	trackY := labelH
	trackW := w - 2*pad
	trackH := h - labelH - rangeH - pad
	cornerR := 8.0

	// Value ratio
	rangeVal := float64(e.maxValue - e.minValue)
	ratio := 0.5
	if rangeVal > 0 {
		ratio = float64(e.currentValue-e.minValue) / rangeVal
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	fillH := trackH * ratio
	fillY := trackY + trackH - fillH

	// Color based on ratio (same thresholds as gauge for consistency)
	barColor := e.fillColor
	if barColor == "" {
		barColor = kBarDefaultFillColor
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="12" ry="12" fill="#1a1a2e"/>`, int(w), int(h))

	// Title label
	svg += fmt.Sprintf(`<text x="%.1f" y="30" font-family="%s" font-size="24" fill="#88AACC" text-anchor="middle">BAR</text>`,
		w/2, rulesDevice.KDeviceFontFamily)

	// Bar track background
	bgColor := e.bgColor
	if bgColor == "" {
		bgColor = kBarDefaultBgColor
	}
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="#333" stroke-width="1"/>`,
		trackX, trackY, trackW, trackH, cornerR, cornerR, bgColor)

	// Bar fill — clipped to the track using clipPath so rounded corners
	// are preserved even when the bar is nearly full or nearly empty.
	svg += fmt.Sprintf(`<defs><clipPath id="bar-clip"><rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f"/></clipPath></defs>`,
		trackX, trackY, trackW, trackH, cornerR, cornerR)
	if ratio > 0.005 {
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" clip-path="url(#bar-clip)"/>`,
			trackX, fillY, trackW, fillH, barColor)
	}

	// Current value centered on the bar
	valueY := trackY + trackH/2 + 8
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="40" fill="#FFFFFF" text-anchor="middle" font-weight="bold">%d</text>`,
		w/2, valueY, rulesDevice.KDeviceFontFamily, e.currentValue)

	// Min / Max labels below the track
	bottomY := trackY + trackH + 24
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="20" fill="#666666">%d</text>`,
		trackX, bottomY, rulesDevice.KDeviceFontFamily, e.minValue)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="20" fill="#666666" text-anchor="end">%d</text>`,
		trackX+trackW, bottomY, rulesDevice.KDeviceFontFamily, e.maxValue)

	// Lock indicator
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="30" font-family="%s" font-size="22" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-12, rulesDevice.KDeviceFontFamily)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementBarGraph) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementBarGraph) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementBarGraph) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("bar")
	e.backendWidth = 130
	e.backendHeight = 90
	e.frontendWidth = 60
	e.frontendHeight = 140
	e.maxValue = 100
	e.minValue = 0
	e.currentValue = 50
	e.fillColor = kBarDefaultFillColor
	e.bgColor = kBarDefaultBgColor

	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id
	e.resizeLocked = true

	// --- Backend element ---
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
		e.backendElem.SetMinSizeD(100, 70+backendBarLabelHeight)
		e.wireBackendEvents()
	}

	// --- Frontend element ---
	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front", X: 100, Y: 100,
			Width: e.frontendWidth.GetFloat(), Height: e.frontendHeight.GetFloat(),
			Index: rulesZIndex.DisplayFrontend, DragEnable: false, SvgXml: e.renderFrontendSVG(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(40, 80)
		e.wireFrontendEvents()
	}

	if e.backendCtxMenu == nil {
		log.Printf("[BarGraph] Warning: no shared hex menu set, menus disabled")
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

// =====================================================================
//  Backend events — same pattern as Gauge (3 connectors)
// =====================================================================

func (e *StatementBarGraph) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}

		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendBarLabelHeight)
		rowH := boxH / 3.0
		elemX, elemY := e.backendElem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY

		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}

		if event.LocalY > boxH {
			return
		}

		// Hit-test 3 connectors
		ports := []string{"max", "current", "min"}
		centers := []float64{rowH / 2, rowH + rowH/2, 2*rowH + rowH/2}
		for i, cy := range centers {
			dx := event.LocalX - rulesDevice.KConnectorOffsetLeft
			dy := event.LocalY - cy
			if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
				go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, ports[i]), menuX, menuY)
				return
			}
		}

		go e.backendCtxMenu.OpenAtWorld(e.getBodyMenuItems(), menuX, menuY)
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
		boxH := h - float64(backendBarLabelHeight)
		rowH := boxH / 3.0

		if ly > boxH {
			return ""
		}

		centers := []float64{rowH / 2, rowH + rowH/2, 2*rowH + rowH/2}
		for _, cy := range centers {
			dx := lx - rulesDevice.KConnectorOffsetLeft
			dy := ly - cy
			if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
				return sprite.CursorPointer
			}
		}
		return ""
	})
}

// =====================================================================
//  Frontend events — reuses the Gauge slider overlay
// =====================================================================

func (e *StatementBarGraph) wireFrontendEvents() {
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

	// Click opens the slider overlay (same as Gauge).
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.interactionLocked {
			log.Printf("[BarGraph:%s] interaction locked — ignoring click", e.id)
			return
		}
		go e.showSliderOverlay()
	})
}

// showSliderOverlay reuses the same CSS class names as the gauge slider
// (gs-*) since they share an identical visual design. The CSS is injected
// once per session by injectSliderCSS() which is idempotent.
func (e *StatementBarGraph) showSliderOverlay() {
	doc := js.Global().Get("document")

	// Reuse the gauge slider overlay ID — only one can be open at a time.
	existing := doc.Call("getElementById", frontendSliderID)
	if existing.Truthy() {
		existing.Get("parentNode").Call("removeChild", existing)
	}

	// Inject shared slider CSS (same as gauge — idempotent).
	injectSharedSliderCSS()

	html := fmt.Sprintf(`
		<div class="gs-panel">
			<div class="gs-header">
				<span class="gs-title">%s</span>
				<button class="gs-close" id="gs-close-btn">✕</button>
			</div>
			<div class="gs-value" id="gs-value-display">%d</div>
			<input type="range" class="gs-slider" id="gs-slider-input"
				min="%d" max="%d" value="%d" step="1"/>
			<div class="gs-range">
				<span>%d</span>
				<span>%d</span>
			</div>
		</div>`,
		e.id, e.currentValue,
		e.minValue, e.maxValue, e.currentValue,
		e.minValue, e.maxValue,
	)

	ov := doc.Call("createElement", "div")
	ov.Set("id", frontendSliderID)
	ov.Set("className", "gs-overlay")
	ov.Set("innerHTML", html)
	doc.Get("body").Call("appendChild", ov)

	slider := doc.Call("getElementById", "gs-slider-input")
	display := doc.Call("getElementById", "gs-value-display")

	inputFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		val := slider.Get("value").String()
		display.Set("textContent", val)

		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			e.currentValue = n
			e.SendValue("current", n)
			go func() {
				e.recacheFrontend()
				e.recacheBackend()
			}()
		}
		return nil
	})
	slider.Call("addEventListener", "input", inputFn)

	closeBtn := doc.Call("getElementById", "gs-close-btn")
	closeFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if ov.Get("parentNode").Truthy() {
			ov.Get("parentNode").Call("removeChild", ov)
		}
		inputFn.Release()
		return nil
	})
	closeBtn.Call("addEventListener", "click", closeFn)

	bgClickFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		target := args[0].Get("target")
		if target.Get("id").String() == frontendSliderID {
			if ov.Get("parentNode").Truthy() {
				ov.Get("parentNode").Call("removeChild", ov)
			}
			inputFn.Release()
			closeFn.Release()
		}
		return nil
	})
	ov.Call("addEventListener", "click", bgClickFn)
}

// injectSharedSliderCSS injects the gs-* stylesheet used by both Gauge and
// BarGraph slider overlays. Idempotent — skips if already injected.
//
// Extracted here so both components share the same CSS without duplication.
// The Gauge's injectSliderCSS() does the same check by the same ID.
func injectSharedSliderCSS() {
	doc := js.Global().Get("document")
	if existing := doc.Call("getElementById", frontendSliderCSS); existing.Truthy() {
		return
	}

	css := `
.gs-overlay {
	position: fixed; inset: 0; z-index: 9000;
	background: rgba(0,0,0,0.5);
	display: flex; align-items: center; justify-content: center;
}
.gs-panel {
	background: #1e1e2e; border: 1px solid #2a2a40; border-radius: 12px;
	padding: 24px; min-width: 320px; max-width: 400px;
	display: flex; flex-direction: column; gap: 16px;
	box-shadow: 0 8px 32px rgba(0,0,0,0.6);
}
.gs-header {
	display: flex; justify-content: space-between; align-items: center;
}
.gs-title {
	color: #88AACC; font-family: Arial, sans-serif; font-size: 14px;
	font-weight: 600; letter-spacing: 0.5px;
}
.gs-close {
	background: none; border: none; color: #555; font-size: 18px;
	cursor: pointer; padding: 2px 6px; border-radius: 4px;
	transition: color 0.15s, background 0.15s;
}
.gs-close:hover { color: #fff; background: rgba(255,80,80,0.2); }
.gs-value {
	font-family: Arial, sans-serif; font-size: 48px; font-weight: bold;
	color: #fff; text-align: center; line-height: 1;
}
.gs-slider {
	-webkit-appearance: none; appearance: none; width: 100%; height: 8px;
	background: #333; border-radius: 4px; outline: none;
	transition: background 0.15s;
}
.gs-slider::-webkit-slider-thumb {
	-webkit-appearance: none; appearance: none; width: 24px; height: 24px;
	background: #6c8eff; border-radius: 50%; cursor: grab;
	border: 2px solid #fff; transition: background 0.15s;
}
.gs-slider::-webkit-slider-thumb:active { cursor: grabbing; background: #5575ee; }
.gs-slider::-moz-range-thumb {
	width: 24px; height: 24px; background: #6c8eff; border-radius: 50%;
	cursor: grab; border: 2px solid #fff;
}
.gs-range {
	display: flex; justify-content: space-between;
	color: #555; font-family: Arial, sans-serif; font-size: 12px;
}
`
	style := doc.Call("createElement", "style")
	style.Set("id", frontendSliderCSS)
	style.Set("textContent", css)
	doc.Get("head").Call("appendChild", style)
}

// =====================================================================
//  Hex menu
// =====================================================================

// getBodyMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementBarGraph) getBodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[BarGraph] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[BarGraph] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay
// =====================================================================

func (e *StatementBarGraph) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementBarGraph) GetInspectConfig() interface{} {
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
					{Key: "min", Label: "Min", Type: overlay.FieldNumber, Value: strconv.FormatInt(e.minValue, 10), Placeholder: "0"},
					{Key: "max", Label: "Max", Type: overlay.FieldNumber, Value: strconv.FormatInt(e.maxValue, 10), Placeholder: "100"},
					{Key: "current", Label: translate.T("propValue", "Value"), Type: overlay.FieldNumber, Value: strconv.FormatInt(e.currentValue, 10), Placeholder: "50"},
					{Key: "fillColor", Label: translate.T("propFillColor", "Fill Color"), Type: overlay.FieldColor, Value: e.fillColor},
					{Key: "bgColor", Label: translate.T("propBgColor", "Track Color"), Type: overlay.FieldColor, Value: e.bgColor},
					{Key: "interactionLocked", Label: translate.T("propLockInteraction", "Lock Interaction"), Type: overlay.FieldCheckbox, Value: lockValue},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementBarGraph.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementBarGraph) ApplyProperties(values map[string]string) {
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
		log.Printf("[BarGraph] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}
	if v, ok := values["min"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != e.minValue {
			e.minValue = n
			changed = true
		}
	}
	if v, ok := values["max"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != e.maxValue {
			e.maxValue = n
			changed = true
		}
	}
	if v, ok := values["current"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != e.currentValue {
			e.currentValue = n
			changed = true
		}
	}
	if v, ok := values["fillColor"]; ok && v != "" && v != e.fillColor {
		e.fillColor = v
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
			log.Printf("[BarGraph] %s: interactionLocked set to %v", e.id, newLocked)
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
//  Wire registration — 3 input ports (same as Gauge)
// =====================================================================

func (e *StatementBarGraph) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}

	ports := []struct {
		name   string
		rowIdx float64
	}{
		{"max", 0}, {"current", 1}, {"min", 2},
	}
	for _, p := range ports {
		pp := p
		e.wireMgr.RegisterConnector(wire.ConnectorInfo{
			ID:       wire.ConnectorID{ElementID: e.id, PortName: pp.name},
			IsOutput: false, AllowedTypes: []string{"int"},
			AcceptNotConnected: true, Locked: false, MaxConnections: 1,
			Label: pp.name,
			PositionFunc: func() (float64, float64) {
				ex, ey := e.backendElem.GetPosition()
				_, h := e.backendElem.GetSize()
				boxH := h - float64(backendBarLabelHeight)
				rowH := boxH / 3.0
				return ex + rulesDevice.KConnectorOffsetLeft, ey + pp.rowIdx*rowH + rowH/2
			},
		})
	}
}

// =====================================================================
//  Live communication
// =====================================================================

func (e *StatementBarGraph) LiveUpdate(port string, value []byte) error {
	var num float64
	if err := json.Unmarshal(value, &num); err != nil {
		var s string
		if err2 := json.Unmarshal(value, &s); err2 != nil {
			return fmt.Errorf("bar %s: cannot parse value for port %q: %w", e.id, port, err)
		}
		parsed, err3 := strconv.ParseFloat(s, 64)
		if err3 != nil {
			return fmt.Errorf("bar %s: cannot parse string value %q: %w", e.id, port, err3)
		}
		num = parsed
	}

	intVal := int64(num)

	switch port {
	case "current":
		e.currentValue = intVal
	case "max":
		e.maxValue = intVal
	case "min":
		e.minValue = intVal
	default:
		return fmt.Errorf("bar %s: unknown port %q", e.id, port)
	}

	log.Printf("[BarGraph:%s] LiveUpdate port=%s value=%d", e.id, port, intVal)
	e.recacheBackend()
	e.recacheFrontend()
	return nil
}

func (e *StatementBarGraph) SendValue(port string, value int64) {
	if e.SendFunc == nil {
		return
	}
	if e.interactionLocked {
		log.Printf("[BarGraph:%s] SendValue blocked — interaction locked", e.id)
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  Serialization — scene.Propertied
// =====================================================================

func (e *StatementBarGraph) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"label":             e.label,
		"min":               e.minValue,
		"max":               e.maxValue,
		"current":           e.currentValue,
		"fillColor":         e.fillColor,
		"bgColor":           e.bgColor,
		"interactionLocked": e.interactionLocked,
	}
}

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementBarGraph) GetInitialized() bool   { return e.initialized }
func (e *StatementBarGraph) GetID() string          { return e.id }
func (e *StatementBarGraph) GetName() string        { return e.name }
func (e *StatementBarGraph) GetSelected() bool      { return e.selected }
func (e *StatementBarGraph) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementBarGraph) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementBarGraph) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementBarGraph) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementBarGraph) GetResize() bool        { return false }
func (e *StatementBarGraph) GetResizeEnable() bool  { return false }
func (e *StatementBarGraph) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementBarGraph) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementBarGraph) SetDragEnable(en bool) {
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

func (e *StatementBarGraph) SetResizeEnable(_ bool) {}
func (e *StatementBarGraph) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementBarGraph) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementBarGraph) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementBarGraph) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementBarGraph) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementBarGraph) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementBarGraph) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementBarGraph) GetStatus() int                                         { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementBarGraph) GetIconName() string     { return "BarGraph" }
func (e *StatementBarGraph) GetIconCategory() string { return "Display" }

func (e *StatementBarGraph) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	// Bar chart icon symbol
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("▐").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 6).Y((rulesIcon.Height / 2).GetInt() + 5)

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

func (e *StatementBarGraph) GetDeviceType() string { return "StatementBarGraph" }
func (e *StatementBarGraph) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementBarGraph) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementBarGraph) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementBarGraph) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementBarGraph) GetLabel() string         { return e.label }
func (e *StatementBarGraph) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementBarGraph) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}
