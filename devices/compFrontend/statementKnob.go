// /ide/devices/compFrontend/statementKnob.go

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
//  Knob geometry constants
//
//  The knob sweeps 270° clockwise:
//    Start angle: 135° (7:30 position = min value)
//    End angle:   405° (4:30 position = max value)
//    Dead zone:   90° at the bottom (from 4:30 back to 7:30)
//
//  This is the standard sweep used in audio equipment, synthesizers,
//  and industrial control panels.
//
//       min ╲         ╱ max
//            ╲       ╱
//             ○─────○
//            ╱  knob  ╲
//           ╱           ╲
//          dead zone (90°)
// =====================================================================

const (
	// knobStartDeg is the angle in degrees where min value sits (7:30 clock position).
	// Measured clockwise from the 3 o'clock position (standard math convention).
	knobStartDeg = 135.0

	// knobSweepDeg is the total sweep in degrees from min to max.
	knobSweepDeg = 270.0

	// knobEndDeg is the angle where max value sits (4:30 clock position).
	knobEndDeg = knobStartDeg + knobSweepDeg // 405°
)

// =====================================================================
//  Default colors
// =====================================================================

const (
	kKnobDefaultColor      = "#5599FF" // knob indicator and value arc — blue (int type)
	kKnobDefaultTrackColor = "#252a3e" // track background
)

// StatementKnob — dual device: backend data node + frontend rotary knob.
//
// Backend: compact box with 3 input connectors (max, current, min) — same
// layout as Gauge and BarGraph.
// Frontend: circular rotary knob. Clicking anywhere on the knob sets the
// value based on the click angle (no dragging needed). The indicator line
// and value arc update immediately.
//
// Português: Knob rotativo. Clicar em qualquer lugar do knob define o
// valor com base no ângulo do click. A linha indicadora e o arco de
// valor atualizam imediatamente.
type StatementKnob struct {
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

	minValue     int64
	maxValue     int64
	currentValue int64

	knobColor  string
	trackColor string

	interactionLocked bool

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
	sceneNotify func()
	onRemove    func(id string)

	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementKnob) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementKnob) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementKnob) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementKnob) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementKnob) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementKnob) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementKnob) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementKnob) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementKnob) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementKnob) Remove() {
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

func (e *StatementKnob) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementKnob) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementKnob) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementKnob) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementKnob) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementKnob) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementKnob) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementKnob) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementKnob) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG — same 3-row layout as Gauge (Max / Current / Min)
// =====================================================================

const backendKnobLabelHeight = 18

func (e *StatementKnob) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendKnobLabelHeight
}

func (e *StatementKnob) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendKnobLabelHeight)
	rowH := boxH / 3.0
	bw := rulesDevice.KDeviceBorderWidth

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, boxH-bw, rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, rulesDevice.KColorTypeInt, bw)

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
		svg += fmt.Sprintf(`<circle cx="%.0f" cy="%.1f" r="%.0f" fill="%s" stroke="%s" stroke-width="1"/>`,
			rulesDevice.KConnectorOffsetLeft, r.y, rulesDevice.KConnectorRadius, rulesDevice.KColorTypeInt, rulesDevice.KColorConnectorStroke)
		svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central">%s</text>`,
			r.y, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted, r.label)
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central" font-weight="bold">%d</text>`,
			w-12, r.y, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizePort, rulesDevice.KColorDeviceText, r.value)
	}

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
//  Frontend SVG — rotary knob with 270° sweep
//
//  Rendered at 3x for crisp edges.
//  Components: background circle, track arc (270°, grey), value arc
//  (colored, partial), indicator line, center cap, value text.
// =====================================================================

func (e *StatementKnob) renderFrontendSVG() string {
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	cx := w / 2
	cy := h * 0.45 // center slightly above middle to make room for value text
	r := w * 0.36
	trackW := 14.0
	indicatorLen := r * 0.85

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

	// Angles in radians (SVG uses clockwise from 3 o'clock)
	startRad := knobStartDeg * math.Pi / 180
	endRad := knobEndDeg * math.Pi / 180
	valueRad := startRad + ratio*(endRad-startRad)

	knobColor := e.knobColor
	if knobColor == "" {
		knobColor = kKnobDefaultColor
	}
	trackColor := e.trackColor
	if trackColor == "" {
		trackColor = kKnobDefaultTrackColor
	}

	// Arc endpoint helper
	arcPoint := func(angle float64) (float64, float64) {
		return cx + r*math.Cos(angle), cy + r*math.Sin(angle)
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="12" ry="12" fill="#1a1a2e"/>`, int(w), int(h))

	// Outer ring (subtle metallic look)
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="#333" stroke-width="2"/>`, cx, cy, r+trackW/2+4)

	// Track arc (270° grey background)
	sx, sy := arcPoint(startRad)
	ex, ey := arcPoint(endRad)
	// large-arc-flag=1 because 270° > 180°
	svg += fmt.Sprintf(`<path d="M %.2f %.2f A %.2f %.2f 0 1 1 %.2f %.2f" fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round"/>`,
		sx, sy, r, r, ex, ey, trackColor, trackW)

	// Value arc (colored, from start to current position)
	if ratio > 0.01 {
		vx, vy := arcPoint(valueRad)
		// Determine large-arc-flag: >180° of sweep needs flag=1
		sweepAngle := ratio * knobSweepDeg
		largeArc := 0
		if sweepAngle > 180 {
			largeArc = 1
		}
		svg += fmt.Sprintf(`<path d="M %.2f %.2f A %.2f %.2f 0 %d 1 %.2f %.2f" fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round"/>`,
			sx, sy, r, r, largeArc, vx, vy, knobColor, trackW)
	}

	// Knob body (filled circle)
	bodyR := r - trackW/2 - 6
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="#1e2235" stroke="#333" stroke-width="1.5"/>`, cx, cy, bodyR)

	// Indicator line (from center outward to current angle)
	indX := cx + indicatorLen*math.Cos(valueRad)
	indY := cy + indicatorLen*math.Sin(valueRad)
	svg += fmt.Sprintf(`<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="4" stroke-linecap="round"/>`,
		cx, cy, indX, indY, knobColor)

	// Center cap
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="6" fill="%s"/>`, cx, cy, knobColor)

	// Value text below the knob
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="36" fill="#FFFFFF" text-anchor="middle" font-weight="bold">%d</text>`,
		cx, h-20, rulesDevice.KDeviceFontFamily, e.currentValue)

	// Min / Max labels at the sweep endpoints
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="18" fill="#555">%d</text>`,
		sx-16, sy+22, rulesDevice.KDeviceFontFamily, e.minValue)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="18" fill="#555" text-anchor="end">%d</text>`,
		ex+16, ey+22, rulesDevice.KDeviceFontFamily, e.maxValue)

	// Lock indicator
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="28" font-family="%s" font-size="22" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-12, rulesDevice.KDeviceFontFamily)
	}

	svg += `</svg>`
	return svg
}

// angleFromClick converts a click position (local to the frontend element)
// to a value in the min..max range based on the knob's 270° sweep.
// Returns the clamped integer value.
//
// The click coordinates are in element-local space. The SVG is rendered at
// 3x scale, but the element dimensions are 1x — we do NOT scale the click
// coordinates because they are already in element space.
func (e *StatementKnob) angleFromClick(localX, localY float64) int64 {
	// Element center (1x scale, not 3x)
	cx := e.frontendWidth.GetFloat() / 2
	cy := e.frontendHeight.GetFloat() * 0.45

	dx := localX - cx
	dy := localY - cy

	// atan2 gives angle in radians from the positive X axis (3 o'clock),
	// increasing clockwise in screen coordinates (Y increases downward).
	angleDeg := math.Atan2(dy, dx) * 180 / math.Pi
	if angleDeg < 0 {
		angleDeg += 360
	}

	// Map from 0-360° space to the knob's sweep.
	// The knob starts at 135° and sweeps 270° clockwise to 405° (=45°).
	// We need to handle the wrap-around: angles 0-45° are actually 360-405°
	// in the sweep coordinate system.
	sweepAngle := angleDeg - knobStartDeg
	if sweepAngle < 0 {
		sweepAngle += 360
	}

	// Clamp to the sweep range. The dead zone is from 270° to 360° of sweep.
	if sweepAngle > knobSweepDeg {
		// In the dead zone — snap to nearest end
		if sweepAngle < knobSweepDeg+45 {
			sweepAngle = knobSweepDeg // snap to max
		} else {
			sweepAngle = 0 // snap to min
		}
	}

	ratio := sweepAngle / knobSweepDeg
	val := float64(e.minValue) + ratio*float64(e.maxValue-e.minValue)
	result := int64(math.Round(val))

	// Clamp to range
	if result < e.minValue {
		result = e.minValue
	}
	if result > e.maxValue {
		result = e.maxValue
	}

	return result
}

func (e *StatementKnob) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementKnob) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementKnob) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("knob")
	e.backendWidth = 130
	e.backendHeight = 90
	e.frontendWidth = 100
	e.frontendHeight = 110
	e.maxValue = 100
	e.minValue = 0
	e.currentValue = 50
	e.knobColor = kKnobDefaultColor
	e.trackColor = kKnobDefaultTrackColor
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
		e.backendElem.SetMinSizeD(100, 70+backendKnobLabelHeight)
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
		e.frontendElem.SetMinSizeD(80, 88)
		e.wireFrontendEvents()
	}

	if e.backendCtxMenu == nil {
		log.Printf("[Knob] Warning: no shared hex menu set, menus disabled")
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

// =====================================================================
//  Backend events — same 3-connector pattern as Gauge
// =====================================================================

func (e *StatementKnob) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}
		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendKnobLabelHeight)
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
		boxH := h - float64(backendKnobLabelHeight)
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
//  Frontend events — click sets value based on angle
// =====================================================================

func (e *StatementKnob) wireFrontendEvents() {
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

	// Click anywhere on the knob sets the value based on the angle from
	// the center to the click position. This gives an intuitive "point
	// to set" interaction without needing continuous drag tracking.
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.interactionLocked {
			log.Printf("[Knob:%s] interaction locked — ignoring click", e.id)
			return
		}

		newVal := e.angleFromClick(event.LocalX, event.LocalY)
		if newVal != e.currentValue {
			e.currentValue = newVal
			log.Printf("[Knob:%s] set to %d via click", e.id, newVal)
			e.SendValue("current", newVal)

			go func() {
				e.recacheFrontend()
				e.recacheBackend()
			}()
		}
	})
}

// =====================================================================
//  Hex menu — backend only
// =====================================================================

// getBackendMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementKnob) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Knob] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[Knob] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay
// =====================================================================

func (e *StatementKnob) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementKnob) GetInspectConfig() interface{} {
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
					{Key: "knobColor", Label: translate.T("propKnobColor", "Knob Color"), Type: overlay.FieldColor, Value: e.knobColor},
					{Key: "trackColor", Label: translate.T("propTrackColor", "Track Color"), Type: overlay.FieldColor, Value: e.trackColor},
					{Key: "interactionLocked", Label: translate.T("propLockInteraction", "Lock Interaction"), Type: overlay.FieldCheckbox, Value: lockValue},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementKnob.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementKnob) ApplyProperties(values map[string]string) {
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
	if v, ok := values["knobColor"]; ok && v != "" && v != e.knobColor {
		e.knobColor = v
		changed = true
	}
	if v, ok := values["trackColor"]; ok && v != "" && v != e.trackColor {
		e.trackColor = v
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
//  Wire registration — 3 input ports (same as Gauge)
// =====================================================================

func (e *StatementKnob) RegisterConnectors() {
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
				boxH := h - float64(backendKnobLabelHeight)
				rowH := boxH / 3.0
				return ex + rulesDevice.KConnectorOffsetLeft, ey + pp.rowIdx*rowH + rowH/2
			},
		})
	}
}

// =====================================================================
//  Live communication
// =====================================================================

func (e *StatementKnob) LiveUpdate(port string, value []byte) error {
	var num float64
	if err := json.Unmarshal(value, &num); err != nil {
		var s string
		if err2 := json.Unmarshal(value, &s); err2 != nil {
			return fmt.Errorf("knob %s: cannot parse value for port %q: %w", e.id, port, err)
		}
		parsed, err3 := strconv.ParseFloat(s, 64)
		if err3 != nil {
			return fmt.Errorf("knob %s: cannot parse string value %q: %w", e.id, s, err3)
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
		return fmt.Errorf("knob %s: unknown port %q", e.id, port)
	}

	log.Printf("[Knob:%s] LiveUpdate port=%s value=%d", e.id, port, intVal)
	e.recacheBackend()
	e.recacheFrontend()
	return nil
}

func (e *StatementKnob) SendValue(port string, value int64) {
	if e.SendFunc == nil || e.interactionLocked {
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  Serialization
// =====================================================================

func (e *StatementKnob) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"label":             e.label,
		"min":               e.minValue,
		"max":               e.maxValue,
		"current":           e.currentValue,
		"knobColor":         e.knobColor,
		"trackColor":        e.trackColor,
		"interactionLocked": e.interactionLocked,
	}
}

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementKnob) GetInitialized() bool   { return e.initialized }
func (e *StatementKnob) GetID() string          { return e.id }
func (e *StatementKnob) GetName() string        { return e.name }
func (e *StatementKnob) GetSelected() bool      { return e.selected }
func (e *StatementKnob) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementKnob) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementKnob) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementKnob) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementKnob) GetResize() bool        { return false }
func (e *StatementKnob) GetResizeEnable() bool  { return false }
func (e *StatementKnob) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementKnob) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}
func (e *StatementKnob) SetDragEnable(en bool) {
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
func (e *StatementKnob) SetResizeEnable(_ bool) {}
func (e *StatementKnob) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementKnob) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementKnob) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementKnob) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementKnob) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementKnob) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementKnob) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementKnob) GetStatus() int                                         { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementKnob) GetIconName() string     { return "Knob" }
func (e *StatementKnob) GetIconCategory() string { return "Display" }

func (e *StatementKnob) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("◎").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 8).Y((rulesIcon.Height / 2).GetInt() + 5)

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

func (e *StatementKnob) GetDeviceType() string { return "StatementKnob" }
func (e *StatementKnob) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementKnob) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementKnob) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementKnob) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementKnob) GetLabel() string         { return e.label }
func (e *StatementKnob) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementKnob) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}
