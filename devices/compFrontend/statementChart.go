// /ide/devices/compFrontend/statementChart.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
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

// ── Chart modes ───────────────────────────────────────────────────────

const (
	ChartModeLine      = "line"
	ChartModeArea      = "area"
	ChartModeSparkline = "sparkline"
	ChartModeSweep     = "sweep"
)

// ── Grid styles ───────────────────────────────────────────────────────

const (
	GridStandard = "standard"
	GridECG      = "ecg"
)

// ── Default values ────────────────────────────────────────────────────

const (
	kChartDefaultLineColor = "#f38ba8"
	kChartDefaultBg        = "#0d1117"
	kChartDefaultBuffer    = 60
)

// StatementChart — dual device: backend data node + frontend real-time chart.
//
// Backend: compact box with 1 input connector (current, int/float64).
// Frontend: resizable SVG chart with circular buffer.
//
// Modes: line, area, sparkline, sweep (ECG).
// Grid styles: standard (grey), ecg (green).
// Info overlay: title, unit, current value, min/avg/max, timestamp.
//
// Português: Gráfico em tempo real com buffer circular. Modos: line, area,
// sparkline, sweep. Info overlay configurável com título, unidade, stats.
type StatementChart struct {
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
	// [CTXMENU] linear context menu controllers for backend
	// and frontend stages. Dual devices open menus on their
	// respective stage; the factory wires both via
	// SetBackendContextMenu and SetFrontendContextMenu.
	backendCtxMenu  *contextMenu.Controller
	frontendCtxMenu *contextMenu.Controller
	wireMgr         *wire.Manager

	label string
	// [COMMENT] user comment — shown in the device's hover tooltip and kept
	// in the scene. Dashboard widgets emit no code statement, so unlike the
	// backend devices this never reaches the generated source — it is stage
	// documentation.
	// Português: Comentário do usuário — exibido no tooltip de hover e
	// gravado na cena. Widgets de dashboard não emitem statement, então
	// diferente dos devices de backend isto nunca chega ao código gerado —
	// é documentação do stage.
	comment  string
	canvasEl js.Value

	// Circular data buffer
	buffer     []float64
	bufferSize int
	sweepIndex int // write position for sweep mode

	// Chart configuration
	mode      string
	gridStyle string
	autoScale bool
	minY      float64
	maxY      float64
	lineColor string
	glowLine  bool

	// Grid appearance (all configurable via Inspect)
	gridColor   string  // hex #RRGGBB from color picker
	gridOpacity int     // 0-100 percent (0 = default per style)
	gridWidth   float64 // stroke width in SVG px (0 = default)

	// Info overlay displayed ON the chart SVG
	chartTitle    string // top-left label (e.g. "Temperature")
	chartUnit     string // unit suffix (e.g. "°C")
	showStats     bool   // show Min / Avg / Max at bottom
	showTimestamp bool   // show "Last: HH:MM:SS" at bottom-right
	lastUpdateTs  int64  // unix time of last LiveUpdate

	// hoverIndex tracks which data point the mouse is over. -1 = no hover.
	// Updated by CursorHitTest on the frontend element. Only triggers a
	// re-render when the index actually changes (not on every pixel move).
	hoverIndex int

	interactionLocked bool

	// tooltipEl is a small HTML div that shows the data value on mouse hover.
	// Created once in wireFrontendEvents, shown/hidden/repositioned via
	// CursorHitTest. Uses the canvas element's bounding rect for positioning.
	tooltipEl js.Value

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
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

	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementChart) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementChart) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementChart) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementChart) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementChart) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage — body clicks and port clicks route through this.
func (e *StatementChart) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}

// SetFrontendContextMenu injects the controller for the frontend
// stage — frontend element taps (Resize, Z-order) route through
// this. May be nil in backend-only compile targets.
func (e *StatementChart) SetFrontendContextMenu(c *contextMenu.Controller) {
	e.frontendCtxMenu = c
}
func (e *StatementChart) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementChart) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementChart) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementChart) Remove() {
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
	// Remove hover tooltip from DOM
	if e.tooltipEl.Truthy() && e.tooltipEl.Get("parentNode").Truthy() {
		e.tooltipEl.Get("parentNode").Call("removeChild", e.tooltipEl)
	}
}

func (e *StatementChart) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementChart) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementChart) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementChart) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementChart) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementChart) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementChart) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementChart) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementChart) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG
// =====================================================================

const backendChartLabelHeight = 18

func (e *StatementChart) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendChartLabelHeight
}

func (e *StatementChart) lastValue() string {
	if len(e.buffer) == 0 {
		return "—"
	}
	v := e.buffer[len(e.buffer)-1]
	if v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func (e *StatementChart) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendChartLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	connY := boxH / 2.0
	borderColor := rulesDevice.KColorTypeInt

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))
	// [PIN] the body is inset on the LEFT by the pin length: the standard
	// connector pins live in the freed margin, protruding from the border
	// with the wires anchored at their outer tips — the element's left edge.
	// Português: O corpo recua à ESQUERDA o comprimento do pino: os pinos
	// padrão vivem na margem liberada, saindo da borda com os fios ancorados
	// nas pontas externas — a borda esquerda do element.
	pin := rulesConnection.PinBodyInset()
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		pin+bw/2, bw/2, w-pin-bw, boxH-bw, rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, borderColor, bw)
	svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideLeft, pin, connY, borderColor)

	tag := strings.ToUpper(e.mode)
	if tag == "" {
		tag = "LINE"
	}
	svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">%s</text>`,
		connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted, tag)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central" font-weight="bold">%s</text>`,
		w-12, connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue, rulesDevice.KColorDeviceText, e.lastValue())

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
//  Frontend SVG
// =====================================================================

func (e *StatementChart) computeYRange() (float64, float64) {
	if !e.autoScale {
		return e.minY, e.maxY
	}
	if len(e.buffer) == 0 {
		return 0, 100
	}
	mn, mx := e.buffer[0], e.buffer[0]
	for _, v := range e.buffer {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	pad := (mx - mn) * 0.1
	if pad < 1 {
		pad = 1
	}
	return mn - pad, mx + pad
}

func (e *StatementChart) renderFrontendSVG() string {
	const scale = 2.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	isSparkline := e.mode == ChartModeSparkline
	isSweep := e.mode == ChartModeSweep
	isECG := e.gridStyle == GridECG
	showFill := e.mode == ChartModeArea || isSparkline

	// Layout — generous padding for readable labels
	var padL, padR, padT, padB float64
	if isSparkline {
		padL, padR, padT, padB = 4, 4, 4, 4
	} else {
		padL, padR, padT, padB = 56, 16, 16, 32
	}
	chartW := w - padL - padR
	chartH := h - padT - padB

	lineColor := e.lineColor
	if lineColor == "" {
		if isECG {
			lineColor = "#44dd88"
		} else {
			lineColor = kChartDefaultLineColor
		}
	}
	fillColor := lineColor + "33"

	minY, maxY := e.computeYRange()
	rangeY := maxY - minY
	if rangeY <= 0 {
		rangeY = 1
	}

	mapY := func(v float64) float64 {
		ratio := (v - minY) / rangeY
		return padT + chartH*(1-ratio)
	}

	n := len(e.buffer)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	bgColor := kChartDefaultBg
	if isECG {
		bgColor = "#0a0a0a"
	}
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="6" ry="6" fill="%s"/>`, int(w), int(h), bgColor)

	// ── Grid ──────────────────────────────────────────────────────────
	if !isSparkline {
		gColor := e.gridColor
		gAlpha := e.gridOpacity
		gStroke := e.gridWidth

		if isECG {
			if gColor == "" {
				gColor = "#33cc66"
			}
			if gAlpha == 0 {
				gAlpha = 40
			}
			if gStroke == 0 {
				gStroke = 1.0
			}
			fineA := fmt.Sprintf("%.2f", float64(gAlpha)/100.0*0.5)
			coarseA := fmt.Sprintf("%.2f", float64(gAlpha)/100.0)
			svg += fmt.Sprintf(`<g opacity="%s">`, fineA)
			for x := padL; x <= w-padR; x += 10 {
				svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`, x, padT, x, h-padB, gColor, gStroke*0.6)
			}
			for y := padT; y <= h-padB; y += 10 {
				svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`, padL, y, w-padR, y, gColor, gStroke*0.6)
			}
			svg += `</g>`
			svg += fmt.Sprintf(`<g opacity="%s">`, coarseA)
			for x := padL; x <= w-padR; x += 50 {
				svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`, x, padT, x, h-padB, gColor, gStroke)
			}
			for y := padT; y <= h-padB; y += 50 {
				svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`, padL, y, w-padR, y, gColor, gStroke)
			}
			svg += `</g>`
		} else {
			if gColor == "" {
				gColor = "#ffffff"
			}
			if gAlpha == 0 {
				gAlpha = 25
			}
			if gStroke == 0 {
				gStroke = 1.0
			}
			gridA := fmt.Sprintf("%.2f", float64(gAlpha)/100.0)
			svg += fmt.Sprintf(`<g opacity="%s">`, gridA)
			for i := 0; i <= 4; i++ {
				y := padT + chartH*float64(i)/4
				svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`, padL, y, w-padR, y, gColor, gStroke)
			}
			for i := 0; i <= 6; i++ {
				x := padL + chartW*float64(i)/6
				svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`, x, padT, x, h-padB, gColor, gStroke)
			}
			svg += `</g>`
			// Y axis labels (outside opacity group — always readable)
			for i := 0; i <= 4; i++ {
				y := padT + chartH*float64(i)/4
				val := maxY - (maxY-minY)*float64(i)/4
				lbl := strconv.FormatFloat(val, 'f', 0, 64)
				svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="22" fill="#aaa" text-anchor="end" dominant-baseline="central">%s</text>`, padL-10, y, lbl)
			}
		}
	}

	// ── Data line ─────────────────────────────────────────────────────
	if n > 0 {
		bufSize := e.bufferSize
		if bufSize < 2 {
			bufSize = kChartDefaultBuffer
		}

		var points []string
		var fillPoints []string

		if isSweep && n >= bufSize {
			for i := 0; i < bufSize; i++ {
				idx := (e.sweepIndex + i) % bufSize
				x := padL + float64(i)/float64(bufSize-1)*chartW
				y := mapY(e.buffer[idx])
				points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
			}
		} else {
			start := 0
			count := n
			if count > bufSize {
				start = count - bufSize
				count = bufSize
			}
			for i := 0; i < count; i++ {
				x := padL + float64(i)/float64(bufSize-1)*chartW
				y := mapY(e.buffer[start+i])
				pt := fmt.Sprintf("%.1f,%.1f", x, y)
				points = append(points, pt)
				if showFill {
					fillPoints = append(fillPoints, pt)
				}
			}
		}

		if len(points) > 1 {
			if showFill && len(fillPoints) > 1 {
				bottomY := fmt.Sprintf("%.1f", padT+chartH)
				firstX := strings.Split(fillPoints[0], ",")[0]
				lastX := strings.Split(fillPoints[len(fillPoints)-1], ",")[0]
				polyPoints := strings.Join(fillPoints, " ") + " " + lastX + "," + bottomY + " " + firstX + "," + bottomY
				svg += fmt.Sprintf(`<polygon points="%s" fill="%s"/>`, polyPoints, fillColor)
			}
			if e.glowLine {
				svg += fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="6" stroke-opacity="0.2" stroke-linejoin="round" stroke-linecap="round"/>`,
					strings.Join(points, " "), lineColor)
			}
			svg += fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="3" stroke-linejoin="round" stroke-linecap="round"/>`,
				strings.Join(points, " "), lineColor)
		}
	} else if !isSparkline {
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="26" fill="#555" text-anchor="middle" dominant-baseline="central">awaiting data</text>`,
			w/2, h/2, rulesDevice.KDeviceFontFamily)
	}

	// ── Chart border ──────────────────────────────────────────────────
	if !isSparkline {
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="none" stroke="#333" stroke-width="1"/>`,
			padL, padT, chartW, chartH)
	}

	// ── Info overlay (title, stats, timestamp) ────────────────────────
	if !isSparkline {
		// Top-left: title + unit
		if e.chartTitle != "" {
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="24" fill="#ddd" font-weight="bold">%s</text>`,
				padL+8, padT+22, rulesDevice.KDeviceFontFamily, e.chartTitle)
			if e.chartUnit != "" {
				svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="22" fill="#ddd">%s</text>`,
					padL+8+float64(len(e.chartTitle))*15, padT+22, rulesDevice.KDeviceFontFamily, e.chartUnit)
			}
		}

		// Top-right: current value (large)
		if n > 0 {
			lastVal := e.buffer[n-1]
			valStr := strconv.FormatFloat(lastVal, 'f', 1, 64)
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="36" fill="%s" text-anchor="end" font-weight="bold">%s</text>`,
				w-padR-8, padT+30, rulesDevice.KDeviceFontFamily, lineColor, valStr)
			if e.chartUnit != "" {
				svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="20" fill="#999" text-anchor="end">%s</text>`,
					w-padR-8, padT+50, rulesDevice.KDeviceFontFamily, e.chartUnit)
			}
		}

		// Bottom bar
		bottomY := h - 6.0

		// Stats: Min / Avg / Max
		if e.showStats && n > 0 {
			var sumV float64
			minV, maxV := e.buffer[0], e.buffer[0]
			for _, bv := range e.buffer {
				sumV += bv
				if bv < minV {
					minV = bv
				}
				if bv > maxV {
					maxV = bv
				}
			}
			avgV := sumV / float64(n)
			statsText := fmt.Sprintf("Min: %.1f   Avg: %.1f   Max: %.1f", minV, avgV, maxV)
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="20" fill="#999">%s</text>`,
				padL+4, bottomY, statsText)
		}

		// Timestamp
		if e.showTimestamp && e.lastUpdateTs > 0 {
			ts := time.Unix(e.lastUpdateTs, 0)
			timeStr := fmt.Sprintf("%02d:%02d:%02d", ts.Hour(), ts.Minute(), ts.Second())
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="18" fill="#888" text-anchor="end">Last: %s</text>`,
				w-padR-4, bottomY, timeStr)
		}

		// Mode badge (top-right, below current value)
		badge := strings.ToUpper(e.mode)
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="20" fill="#666" text-anchor="end">%s</text>`,
			w-padR, padT+68, rulesDevice.KDeviceFontFamily, badge)
	}

	// ── Hover tooltip ─────────────────────────────────────────────────
	// When hoverIndex >= 0, draw a vertical dashed line at the data point
	// and a value badge with the unit.
	if !isSparkline && e.hoverIndex >= 0 && n > 0 {
		hi := e.hoverIndex
		if hi >= n {
			hi = n - 1
		}

		bufSize := e.bufferSize
		if bufSize < 2 {
			bufSize = kChartDefaultBuffer
		}

		hoverVal := e.buffer[hi]
		start := 0
		count := n
		if count > bufSize {
			start = count - bufSize
			count = bufSize
		}
		localIdx := hi - start
		if localIdx < 0 {
			localIdx = 0
		}
		if localIdx >= count {
			localIdx = count - 1
		}

		hx := padL + float64(localIdx)/float64(bufSize-1)*chartW
		hy := mapY(hoverVal)

		// Vertical reference line (dashed, subtle)
		svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1" stroke-opacity="0.5" stroke-dasharray="4,3"/>`,
			hx, padT, hx, padT+chartH, lineColor)

		// Horizontal reference line (dashed, subtle)
		svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1" stroke-opacity="0.3" stroke-dasharray="4,3"/>`,
			padL, hy, padL+chartW, hy, lineColor)

		// Dot at the data point
		svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="6" fill="%s" stroke="#fff" stroke-width="2"/>`,
			hx, hy, lineColor)

		// Value badge
		valStr := strconv.FormatFloat(hoverVal, 'f', 1, 64)
		if e.chartUnit != "" {
			valStr += " " + e.chartUnit
		}
		badgeW := float64(len(valStr))*12 + 16
		badgeX := hx - badgeW/2
		badgeY := hy - 32
		// Keep badge inside the chart area
		if badgeY < padT {
			badgeY = hy + 14
		}
		if badgeX < padL {
			badgeX = padL
		}
		if badgeX+badgeW > padL+chartW {
			badgeX = padL + chartW - badgeW
		}

		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.0f" height="26" rx="4" fill="#1e1e2eee" stroke="%s" stroke-width="1.5"/>`,
			badgeX, badgeY, badgeW, lineColor)
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="18" fill="#fff" text-anchor="middle" dominant-baseline="central">%s</text>`,
			badgeX+badgeW/2, badgeY+13, valStr)
	}

	// Lock indicator
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="20" font-family="%s" font-size="18" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-12, rulesDevice.KDeviceFontFamily)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementChart) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementChart) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}
func (e *StatementChart) RefreshVisual() { e.recacheFrontend() }

// ── Buffer management ─────────────────────────────────────────────────

func (e *StatementChart) addPoint(val float64) {
	if e.mode == ChartModeSweep {
		for len(e.buffer) < e.bufferSize {
			e.buffer = append(e.buffer, 0)
		}
		e.buffer[e.sweepIndex] = val
		e.sweepIndex = (e.sweepIndex + 1) % e.bufferSize
	} else {
		e.buffer = append(e.buffer, val)
		if len(e.buffer) > e.bufferSize {
			e.buffer = e.buffer[len(e.buffer)-e.bufferSize:]
		}
	}
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementChart) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("chart")
	e.backendWidth = 120
	e.backendHeight = 36
	e.frontendWidth = 200
	e.frontendHeight = 120
	e.bufferSize = kChartDefaultBuffer
	e.buffer = make([]float64, 0, e.bufferSize)
	e.mode = ChartModeLine
	e.gridStyle = GridStandard
	e.autoScale = true
	e.minY = 0
	e.maxY = 100
	e.lineColor = kChartDefaultLineColor
	e.glowLine = false
	e.gridColor = ""
	e.gridOpacity = 0
	e.gridWidth = 0
	e.chartTitle = ""
	e.chartUnit = ""
	e.showStats = true
	e.showTimestamp = true
	e.hoverIndex = -1
	e.resizeLocked = false

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
		e.backendElem.SetMinSizeD(80, 36+backendChartLabelHeight)
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
		e.frontendElem.SetMinSizeD(100, 60)

		if e.resizerButton != nil {
			adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
			if err2 := e.frontendElem.SetResizeButtons(adapter); err2 != nil {
				log.Printf("[Chart] ERROR: SetResizeButtons failed: %v", err2)
			}
			e.frontendElem.ShowResizeButtons(false)
			e.frontendElem.SetResizeEnable(false)
		}

		e.wireFrontendEvents()
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

func (e *StatementChart) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}
		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendChartLabelHeight)
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

		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), connY,
			event.LocalX, event.LocalY) {
			go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, "current"), menuX, menuY)
			return
		}
		go e.backendCtxMenu.OpenForDevice(e, e.getBackendMenuItems(), menuX, menuY)
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

	e.backendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendChartLabelHeight)
		connY := boxH / 2.0
		if ly > boxH {
			return ""
		}
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), connY, lx, ly) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// =====================================================================
//  Frontend events — click opens context menu (Resize only)
// =====================================================================

func (e *StatementChart) wireFrontendEvents() {
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

	e.frontendElem.SetOnResizeStart(func(event sprite.ResizeEvent) {})
	e.frontendElem.SetOnResizeMove(func(event sprite.ResizeEvent) {})
	e.frontendElem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		wD, hD := e.frontendElem.GetSizeD()
		nw, nh := e.gridAdjust.AdjustCenterD(wD, hD)
		e.frontendElem.SetSizeD(nw, nh)
		e.frontendWidth = nw
		e.frontendHeight = nh
		e.SetResizeEnable(false)
		e.SetDragEnable(true)
		go func() {
			e.recacheFrontend()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	})

	// ── Hover tooltip — shows value + unit at cursor position ─────────
	// Create a small HTML div that floats over the canvas. CursorHitTest
	// positions it on every mouse move. The div is appended to document.body
	// and removed on Remove().
	e.createTooltip()

	e.frontendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		if e.mode == ChartModeSparkline || len(e.buffer) == 0 {
			e.hideTooltip()
			return ""
		}

		// Check if cursor is inside the chart area (1x element space)
		const scale = 2.0
		cPadL := 56.0 / scale
		cPadR := 16.0 / scale
		cPadT := 16.0 / scale
		cPadB := 32.0 / scale
		cW := e.frontendWidth.GetFloat() - cPadL - cPadR
		cH := e.frontendHeight.GetFloat() - cPadT - cPadB

		relX := lx - cPadL
		relY := ly - cPadT
		if relX < 0 || relX > cW || relY < 0 || relY > cH {
			e.hideTooltip()
			return ""
		}

		// Map X position to buffer index
		ratio := relX / cW
		bufSize := e.bufferSize
		n := len(e.buffer)
		start := 0
		count := n
		if count > bufSize {
			start = count - bufSize
			count = bufSize
		}
		idx := start + int(ratio*float64(count-1)+0.5)
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}

		val := e.buffer[idx]
		e.showTooltip(val, lx, ly)
		return "crosshair"
	})

	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.frontendCtxMenu == nil {
			return
		}
		ex, ey := e.frontendElem.GetPosition()
		clickWX, clickWY := ex+event.LocalX, ey+event.LocalY
		go e.frontendCtxMenu.OpenForDevice(e, e.frontendContextItems(), clickWX, clickWY)
	})

	// Hover: show value tooltip at the nearest data point.
	// CursorHitTest is called on every mouse move. We only re-render
	// the SVG when the hovered data index changes, keeping it efficient.
	e.frontendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		if e.mode == ChartModeSparkline || len(e.buffer) == 0 {
			return ""
		}

		const scale = 2.0
		padL := 56.0 / scale
		padR := 16.0 / scale
		padT := 16.0 / scale
		padB := 32.0 / scale
		chartW := e.frontendWidth.GetFloat() - padL - padR
		chartH := e.frontendHeight.GetFloat() - padT - padB

		relX := lx - padL
		relY := ly - padT

		// Outside chart area → clear hover
		if relX < 0 || relX > chartW || relY < 0 || relY > chartH {
			if e.hoverIndex != -1 {
				e.hoverIndex = -1
				go e.recacheFrontend()
			}
			return ""
		}

		ratio := relX / chartW
		bufSize := e.bufferSize
		n := len(e.buffer)
		start := 0
		count := n
		if count > bufSize {
			start = count - bufSize
			count = bufSize
		}
		idx := start + int(ratio*float64(count-1)+0.5)
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}

		if idx != e.hoverIndex {
			e.hoverIndex = idx
			go e.recacheFrontend()
		}

		return sprite.CursorPointer
	})
}

// createTooltip creates the hover tooltip HTML div.
func (e *StatementChart) createTooltip() {
	doc := js.Global().Get("document")

	tip := doc.Call("createElement", "div")
	tip.Get("style").Set("cssText",
		"position:fixed;display:none;pointer-events:none;z-index:9999;"+
			"background:#1e1e2eee;border:1px solid #444;border-radius:4px;"+
			"padding:4px 10px;font-family:monospace;font-size:14px;"+
			"color:#fff;white-space:nowrap;box-shadow:0 2px 8px rgba(0,0,0,0.5);")
	doc.Get("body").Call("appendChild", tip)
	e.tooltipEl = tip
}

// showTooltip positions and displays the tooltip near the cursor.
// val is the data value to display. lx/ly are element-local coordinates.
func (e *StatementChart) showTooltip(val float64, lx, ly float64) {
	if !e.tooltipEl.Truthy() {
		return
	}

	// Format value
	var text string
	if val == math.Trunc(val) {
		text = strconv.FormatInt(int64(val), 10)
	} else {
		text = strconv.FormatFloat(val, 'f', 1, 64)
	}
	if e.chartUnit != "" {
		text += " " + e.chartUnit
	}
	e.tooltipEl.Set("textContent", text)

	// Convert element-local coords to screen coords via canvas bounding rect.
	// This is approximate — doesn't account for camera zoom/pan — but works
	// well for the frontend canvas which is typically unzoomed.
	if e.canvasEl.Truthy() {
		rect := e.canvasEl.Call("getBoundingClientRect")
		ex, ey := e.frontendElem.GetPosition()
		screenX := rect.Get("left").Float() + ex + lx + 16
		screenY := rect.Get("top").Float() + ey + ly - 30
		e.tooltipEl.Get("style").Set("left", fmt.Sprintf("%.0fpx", screenX))
		e.tooltipEl.Get("style").Set("top", fmt.Sprintf("%.0fpx", screenY))
	}

	e.tooltipEl.Get("style").Set("display", "block")
}

// hideTooltip hides the hover tooltip.
func (e *StatementChart) hideTooltip() {
	if e.tooltipEl.Truthy() {
		e.tooltipEl.Get("style").Set("display", "none")
	}
}

// frontendContextItems returns the frontend menu list. For this
// device it is just Resize. Inspect is intentionally absent — it
// is a backend-only concept per decision D10.
//
// Português: Lista do menu frontend. Apenas Resize.
func (e *StatementChart) frontendContextItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.ResizeItem(func() {
			e.SetResizeEnable(!e.GetResizeEnable())
			log.Printf("[Chart:%s] resize toggled to %v", e.id, e.GetResizeEnable())
		}),
	}
}

// =====================================================================
//  Hex menu — backend only
// =====================================================================

// getBackendMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementChart) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Chart] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[Chart] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay
// =====================================================================

func (e *StatementChart) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementChart) GetInspectConfig() interface{} {
	lockVal := "false"
	if e.interactionLocked {
		lockVal = "true"
	}
	autoVal := "false"
	if e.autoScale {
		autoVal = "true"
	}
	glowVal := "false"
	if e.glowLine {
		glowVal = "true"
	}
	statsVal := "false"
	if e.showStats {
		statsVal = "true"
	}
	tsVal := "false"
	if e.showTimestamp {
		tsVal = "true"
	}

	return overlay.Config{
		Title: e.id,
		Width: "520px",
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
					{
						Key:         "comment",
						Label:       translate.T("propComment", "Comment"),
						Type:        overlay.FieldTextarea,
						Value:       e.comment,
						Placeholder: translate.T("propCommentPlaceholder", "Comment shown on hover..."),
						Rows:        3,
					},
					{Key: "chartTitle", Label: translate.T("propChartTitle", "Title"), Type: overlay.FieldText, Value: e.chartTitle, Placeholder: "Temperature"},
					{Key: "chartUnit", Label: translate.T("propChartUnit", "Unit"), Type: overlay.FieldText, Value: e.chartUnit, Placeholder: "°C"},
					{Key: "mode", Label: translate.T("propMode", "Mode"), Type: overlay.FieldSelect, Value: e.mode, Options: []overlay.Option{
						{Value: ChartModeLine, Label: "Line"},
						{Value: ChartModeArea, Label: "Area"},
						{Value: ChartModeSparkline, Label: "Sparkline"},
						{Value: ChartModeSweep, Label: "Sweep (ECG)"},
					}},
					{Key: "gridStyle", Label: translate.T("propGridStyle", "Grid"), Type: overlay.FieldSelect, Value: e.gridStyle, Options: []overlay.Option{
						{Value: GridStandard, Label: "Standard"},
						{Value: GridECG, Label: "ECG (green)"},
					}},
					{Key: "bufferSize", Label: translate.T("propBuffer", "Buffer"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.bufferSize), Min: "10", Max: "600", Placeholder: "60"},
					{Key: "autoScale", Label: translate.T("propAutoScale", "Auto Scale"), Type: overlay.FieldCheckbox, Value: autoVal},
					{Key: "minY", Label: "Min Y", Type: overlay.FieldNumber, Value: strconv.FormatFloat(e.minY, 'f', 1, 64), Placeholder: "0"},
					{Key: "maxY", Label: "Max Y", Type: overlay.FieldNumber, Value: strconv.FormatFloat(e.maxY, 'f', 1, 64), Placeholder: "100"},
					{Key: "lineColor", Label: translate.T("propLineColor", "Line Color"), Type: overlay.FieldColor, Value: e.lineColor},
					{Key: "glowLine", Label: translate.T("propGlow", "Glow Effect"), Type: overlay.FieldCheckbox, Value: glowVal},
					{Key: "gridColor", Label: translate.T("propGridColor", "Grid Color"), Type: overlay.FieldColor, Value: e.gridColor},
					{Key: "gridOpacity", Label: translate.T("propGridOpacity", "Grid Opacity %"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.gridOpacity), Min: "0", Max: "100", Placeholder: "0=auto"},
					{Key: "gridWidth", Label: translate.T("propGridWidth", "Grid Width"), Type: overlay.FieldNumber, Value: strconv.FormatFloat(e.gridWidth, 'f', 1, 64), Min: "0", Max: "5", Placeholder: "0=auto"},
					{Key: "showStats", Label: translate.T("propShowStats", "Show Min/Avg/Max"), Type: overlay.FieldCheckbox, Value: statsVal},
					{Key: "showTimestamp", Label: translate.T("propShowTimestamp", "Show Timestamp"), Type: overlay.FieldCheckbox, Value: tsVal},
					{Key: "interactionLocked", Label: translate.T("propLockInteraction", "Lock Interaction"), Type: overlay.FieldCheckbox, Value: lockVal},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementChart.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementChart) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
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
	if v, ok := values["chartTitle"]; ok && v != e.chartTitle {
		e.chartTitle = v
		changed = true
	}
	if v, ok := values["chartUnit"]; ok && v != e.chartUnit {
		e.chartUnit = v
		changed = true
	}
	if v, ok := values["mode"]; ok && v != e.mode {
		e.mode = v
		e.buffer = make([]float64, 0, e.bufferSize)
		e.sweepIndex = 0
		changed = true
	}
	if v, ok := values["gridStyle"]; ok && v != e.gridStyle {
		e.gridStyle = v
		changed = true
	}
	if v, ok := values["bufferSize"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 10 && n <= 600 && n != e.bufferSize {
			e.bufferSize = n
			if len(e.buffer) > n {
				e.buffer = e.buffer[len(e.buffer)-n:]
			}
			changed = true
		}
	}
	if v, ok := values["autoScale"]; ok {
		b := v == "true"
		if b != e.autoScale {
			e.autoScale = b
			changed = true
		}
	}
	if v, ok := values["minY"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n != e.minY {
			e.minY = n
			changed = true
		}
	}
	if v, ok := values["maxY"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n != e.maxY {
			e.maxY = n
			changed = true
		}
	}
	if v, ok := values["lineColor"]; ok && v != "" && v != e.lineColor {
		e.lineColor = v
		changed = true
	}
	if v, ok := values["glowLine"]; ok {
		b := v == "true"
		if b != e.glowLine {
			e.glowLine = b
			changed = true
		}
	}
	if v, ok := values["gridColor"]; ok && v != e.gridColor {
		e.gridColor = v
		changed = true
	}
	if v, ok := values["gridOpacity"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100 && n != e.gridOpacity {
			e.gridOpacity = n
			changed = true
		}
	}
	if v, ok := values["gridWidth"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 && n <= 5 && n != e.gridWidth {
			e.gridWidth = n
			changed = true
		}
	}
	if v, ok := values["showStats"]; ok {
		b := v == "true"
		if b != e.showStats {
			e.showStats = b
			changed = true
		}
	}
	if v, ok := values["showTimestamp"]; ok {
		b := v == "true"
		if b != e.showTimestamp {
			e.showTimestamp = b
			changed = true
		}
	}
	if v, ok := values["interactionLocked"]; ok {
		b := v == "true"
		if b != e.interactionLocked {
			e.interactionLocked = b
			changed = true
		}
	}
	if v, ok := values["frontendWidth"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			e.frontendWidth = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			changed = true
		}
	}
	if v, ok := values["frontendHeight"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			e.frontendHeight = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
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
//  Wire registration
// =====================================================================

func (e *StatementChart) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "current"},
		IsOutput:           false,
		AllowedTypes:       []string{"int", "float64"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     1,
		Label:              "current",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.backendElem.GetPosition()
			_, h := e.backendElem.GetSize()
			boxH := h - float64(backendChartLabelHeight)
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), boxH/2)
			return ex + ax, ey + ay
		},
	})
}

// =====================================================================
//  Live communication
// =====================================================================

func (e *StatementChart) LiveUpdate(port string, value []byte) error {
	if port != "current" {
		return fmt.Errorf("chart %s: unknown port %q", e.id, port)
	}
	var num float64
	if err := json.Unmarshal(value, &num); err != nil {
		var s string
		if err2 := json.Unmarshal(value, &s); err2 != nil {
			return fmt.Errorf("chart %s: cannot parse value: %w", e.id, err)
		}
		parsed, err3 := strconv.ParseFloat(s, 64)
		if err3 != nil {
			return fmt.Errorf("chart %s: cannot parse string: %w", e.id, err3)
		}
		num = parsed
	}
	e.addPoint(num)
	e.lastUpdateTs = time.Now().Unix()
	e.recacheBackend()
	e.recacheFrontend()
	return nil
}

func (e *StatementChart) SendValue(port string, value float64) {
	if e.SendFunc == nil || e.interactionLocked {
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  Serialization
// =====================================================================

func (e *StatementChart) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label":             e.label,
		"chartTitle":        e.chartTitle,
		"chartUnit":         e.chartUnit,
		"mode":              e.mode,
		"gridStyle":         e.gridStyle,
		"bufferSize":        e.bufferSize,
		"autoScale":         e.autoScale,
		"minY":              e.minY,
		"maxY":              e.maxY,
		"lineColor":         e.lineColor,
		"glowLine":          e.glowLine,
		"gridColor":         e.gridColor,
		"gridOpacity":       e.gridOpacity,
		"gridWidth":         e.gridWidth,
		"showStats":         e.showStats,
		"showTimestamp":     e.showTimestamp,
		"interactionLocked": e.interactionLocked,
		"frontendWidth":     e.frontendWidth.GetFloat(),
		"frontendHeight":    e.frontendHeight.GetFloat(),
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in the device's hover tooltip.
// Português: Retorna o comentário exibido no tooltip de hover do device.
func (e *StatementChart) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementChart) SetComment(c string) { e.comment = c }

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementChart) GetInitialized() bool   { return e.initialized }
func (e *StatementChart) GetID() string          { return e.id }
func (e *StatementChart) GetName() string        { return e.name }
func (e *StatementChart) GetSelected() bool      { return e.selected }
func (e *StatementChart) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementChart) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementChart) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementChart) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementChart) GetResize() bool        { return false }
func (e *StatementChart) GetResizeEnable() bool {
	if e.frontendElem != nil {
		return e.frontendElem.IsResizeEnabled()
	}
	return false
}
func (e *StatementChart) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}
func (e *StatementChart) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}
func (e *StatementChart) SetDragEnable(en bool) {
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
func (e *StatementChart) SetResizeEnable(enabled bool) {
	if e.resizeLocked || e.frontendElem == nil {
		return
	}
	if enabled {
		e.frontendElem.SetDragEnable(false)
		e.dragEnabled = false
		e.selected = false
		e.frontendElem.SetResizeEnable(true)
		e.frontendElem.ShowResizeButtons(true)
	} else {
		e.frontendElem.SetResizeEnable(false)
		e.frontendElem.ShowResizeButtons(false)
	}
}
func (e *StatementChart) SelectedInvert() { e.SetSelected(!e.selected) }
func (e *StatementChart) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementChart) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementChart) SetWidth(_ rulesDensity.Density)  {}
func (e *StatementChart) SetHeight(_ rulesDensity.Density) {}
func (e *StatementChart) SetSize(w, h rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendWidth = w
		e.frontendHeight = h
		e.frontendElem.SetSizeD(w, h)
	}
}
func (e *StatementChart) SetStatus(s int) { e.iconStatus = s }
func (e *StatementChart) GetStatus() int  { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementChart) GetIconName() string     { return "Chart" }
func (e *StatementChart) GetIconCategory() string { return "Display" }

func (e *StatementChart) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("〜").Fill(data.ColorIcon).
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

func (e *StatementChart) GetDeviceType() string { return "StatementChart" }
func (e *StatementChart) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementChart) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementChart) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementChart) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementChart) GetLabel() string         { return e.label }
func (e *StatementChart) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}
func (e *StatementChart) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementChart) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
