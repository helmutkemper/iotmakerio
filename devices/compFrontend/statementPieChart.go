// /ide/devices/compFrontend/statementPieChart.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

// statementPieChart.go — Dual device: backend data node + frontend pie chart.
//
// Each input series becomes one slice of the pie. The slice size is
// proportional to the LAST received value on that port relative to the
// sum of all series' latest values.
//
// Backend view:
//
//	Compact box with N input connectors (one per slice), stacked vertically.
//	Same visual pattern as ChartPro.
//
// Frontend view:
//
//	Resizable SVG pie (or donut) chart with legend. Each slice is colored
//	to match its series. Hover shows the slice value and percentage.
//
// Configuration (Inspect):
//
//	- Slice count (1–8)
//	- Donut mode (toggle: pie vs donut)
//	- Show legend (toggle)
//	- Show percentages on slices (toggle)
//	- Per-slice: label, color

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
	"github.com/helmutkemper/iotmakerio/rulesFrontend"
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

const (
	kPieMaxSlices                         = 8
	kPieMinSlices                         = 1
	kPieBackendWidth rulesDensity.Density = 130
	kPieConnSpacing                       = 20.0
	kPiePadTop                            = 16.0
	kPiePadBottom                         = 8.0
	kPieDefaultBg                         = "#0d1117"
	kPieRenderMs                          = 100
)

// pieSlice holds per-slice state.
type pieSlice struct {
	Label string
	Color string
	Value float64 // last received value
}

type StatementPieChart struct {
	backendStage      sprite.Stage
	frontendStage     sprite.Stage
	backendElem       sprite.Element
	frontendElem      sprite.Element
	name              string
	initialized       bool
	selected          bool
	selectLocked      bool
	dragEnabled       bool
	dragLocked        bool
	resizeLocked      bool
	backendWidth      rulesDensity.Density
	backendHeight     rulesDensity.Density
	frontendWidth     rulesDensity.Density
	frontendHeight    rulesDensity.Density
	pendingDragEnable *bool
	resizerButton     block.ResizeButton
	// [CTXMENU] linear context menu controllers for backend
	// and frontend stages. Dual devices open menus on their
	// respective stage; the factory wires both via
	// SetBackendContextMenu and SetFrontendContextMenu.
	backendCtxMenu  *contextMenu.Controller
	frontendCtxMenu *contextMenu.Controller
	wireMgr         *wire.Manager
	label           string
	// [COMMENT] user comment — shown in the device's hover tooltip and kept
	// in the scene. Dashboard widgets emit no code statement, so unlike the
	// backend devices this never reaches the generated source — it is stage
	// documentation.
	// Português: Comentário do usuário — exibido no tooltip de hover e
	// gravado na cena. Widgets de dashboard não emitem statement, então
	// diferente dos devices de backend isto nunca chega ao código gerado —
	// é documentação do stage.
	comment           string
	canvasEl          js.Value
	slices            []pieSlice
	sliceCount        int
	donut             bool // true = donut, false = pie
	showLegend        bool
	showPercent       bool // show % labels on slices
	chartTitle        string
	interactionLocked bool
	lastRenderMs      int64
	id                string
	gridAdjust        grid.Adjust
	iconStatus        int
	sceneNotify       func()
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

func (e *StatementPieChart) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementPieChart) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementPieChart) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementPieChart) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementPieChart) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage — body clicks and port clicks route through this.
func (e *StatementPieChart) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}

// SetFrontendContextMenu injects the controller for the frontend
// stage — frontend element taps (Resize, Z-order) route through
// this. May be nil in backend-only compile targets.
func (e *StatementPieChart) SetFrontendContextMenu(c *contextMenu.Controller) {
	e.frontendCtxMenu = c
}
func (e *StatementPieChart) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementPieChart) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementPieChart) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementPieChart) Remove() {
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

func (e *StatementPieChart) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementPieChart) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementPieChart) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementPieChart) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementPieChart) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementPieChart) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementPieChart) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementPieChart) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementPieChart) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// ── Backend geometry ──────────────────────────────────────────────────

func (e *StatementPieChart) backendBodyHeight() float64 {
	return kPiePadTop + float64(e.sliceCount)*kPieConnSpacing + kPiePadBottom
}
func (e *StatementPieChart) backendTotalHeight() float64 {
	return e.backendBodyHeight() + float64(rulesDevice.KLabelHeight)
}
func (e *StatementPieChart) connectorY(i int) float64 {
	return kPiePadTop + float64(i)*kPieConnSpacing + kPieConnSpacing/2
}
func piePortName(i int) string { return fmt.Sprintf("s%d", i) }
func (e *StatementPieChart) getSliceColor(i int) string {
	if i < len(e.slices) && e.slices[i].Color != "" {
		return e.slices[i].Color
	}
	return rulesFrontend.KSeriesPalette[i%rulesFrontend.KSeriesMax]
}

// ── Backend SVG ───────────────────────────────────────────────────────

func (e *StatementPieChart) renderBackendSVG() string {
	w := float64(kPieBackendWidth)
	bodyH := e.backendBodyHeight()
	totalH := e.backendTotalHeight()
	bw := rulesDevice.KDeviceBorderWidth
	accent := e.getSliceColor(0)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))

	// [PIN] the body is inset on the LEFT by the pin length: one standard
	// pin per slice lives in the freed margin, wires anchored at the outer
	// tips — the element's left edge.
	// Português: O corpo recua à ESQUERDA o comprimento do pino: um pino
	// padrão por fatia vive na margem liberada, fios ancorados nas pontas
	// externas — a borda esquerda do element.
	pin := rulesConnection.PinBodyInset()
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		pin+bw/2, bw/2, w-pin-bw, bodyH-bw, rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, accent, bw)
	svg += fmt.Sprintf(`<text x="%.1f" y="14" font-family="%s" font-size="%d" fill="%s" text-anchor="end" font-weight="bold">PIE</text>`,
		w-12, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted)

	for i := 0; i < e.sliceCount; i++ {
		cy := e.connectorY(i)
		color := e.getSliceColor(i)
		svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideLeft, pin, cy, color)
		lbl := piePortName(i)
		if i < len(e.slices) && e.slices[i].Label != "" {
			lbl = e.slices[i].Label
			if len(lbl) > 12 {
				lbl = lbl[:12] + "…"
			}
		}
		svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central">%s</text>`,
			cy, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizePort, color, lbl)
	}

	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="12" font-family="%s" font-size="10" fill="#FF8833" text-anchor="end">🔒</text>`, w-4, rulesDevice.KDeviceFontFamily)
	}
	dl := e.label
	if dl == "" {
		dl = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, bodyH+3, dl)
	svg += `</svg>`
	return svg
}

// ── Frontend SVG — pie/donut ──────────────────────────────────────────

func (e *StatementPieChart) renderFrontendSVG() string {
	const scale = 2.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="6" ry="6" fill="%s"/>`, int(w), int(h), kPieDefaultBg)

	// Title
	titleH := 0.0
	if e.chartTitle != "" {
		titleH = 30.0
		svg += fmt.Sprintf(`<text x="%.1f" y="22" font-family="%s" font-size="24" fill="#ddd" font-weight="bold" text-anchor="middle">%s</text>`,
			w/2, rulesDevice.KDeviceFontFamily, e.chartTitle)
	}

	// Legend space
	legendH := 0.0
	if e.showLegend {
		legendH = 30.0
	}

	// Pie geometry — fit in available space
	availH := h - titleH - legendH - 16
	availW := w - 32
	radius := availW / 2
	if availH/2 < radius {
		radius = availH / 2
	}
	if radius < 20 {
		radius = 20
	}
	cx := w / 2
	cy := titleH + 8 + availH/2

	innerRadius := 0.0
	if e.donut {
		innerRadius = radius * 0.55
	}

	// Compute total
	total := 0.0
	for _, s := range e.slices {
		if s.Value > 0 {
			total += s.Value
		}
	}

	if total <= 0 {
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="26" fill="#555" text-anchor="middle" dominant-baseline="central">awaiting data</text>`,
			w/2, cy, rulesDevice.KDeviceFontFamily)
		if e.interactionLocked {
			svg += fmt.Sprintf(`<text x="%.1f" y="20" font-family="%s" font-size="18" fill="#FF8833" text-anchor="end">🔒</text>`, w-12, rulesDevice.KDeviceFontFamily)
		}
		svg += `</svg>`
		return svg
	}

	// Draw slices
	startAngle := -math.Pi / 2 // start at top
	for i, s := range e.slices {
		if s.Value <= 0 {
			continue
		}
		fraction := s.Value / total
		sweepAngle := fraction * 2 * math.Pi
		endAngle := startAngle + sweepAngle

		color := e.getSliceColor(i)
		largeArc := 0
		if sweepAngle > math.Pi {
			largeArc = 1
		}

		// Outer arc
		x1 := cx + radius*math.Cos(startAngle)
		y1 := cy + radius*math.Sin(startAngle)
		x2 := cx + radius*math.Cos(endAngle)
		y2 := cy + radius*math.Sin(endAngle)

		if e.donut {
			// Donut: draw arc path with inner hole
			ix1 := cx + innerRadius*math.Cos(startAngle)
			iy1 := cy + innerRadius*math.Sin(startAngle)
			ix2 := cx + innerRadius*math.Cos(endAngle)
			iy2 := cy + innerRadius*math.Sin(endAngle)

			path := fmt.Sprintf("M%.1f,%.1f A%.1f,%.1f 0 %d,1 %.1f,%.1f L%.1f,%.1f A%.1f,%.1f 0 %d,0 %.1f,%.1f Z",
				x1, y1, radius, radius, largeArc, x2, y2,
				ix2, iy2, innerRadius, innerRadius, largeArc, ix1, iy1)
			svg += fmt.Sprintf(`<path d="%s" fill="%s" stroke="%s" stroke-width="2"/>`, path, color, kPieDefaultBg)
		} else {
			// Pie: triangle slice from center
			path := fmt.Sprintf("M%.1f,%.1f L%.1f,%.1f A%.1f,%.1f 0 %d,1 %.1f,%.1f Z",
				cx, cy, x1, y1, radius, radius, largeArc, x2, y2)
			svg += fmt.Sprintf(`<path d="%s" fill="%s" stroke="%s" stroke-width="2"/>`, path, color, kPieDefaultBg)
		}

		// Percentage label on the slice — centered on the radius midpoint
		// with font size proportional to the pie radius for clean scaling.
		if e.showPercent && fraction >= 0.05 {
			labelR := radius / 2
			if e.donut {
				labelR = (radius + innerRadius) / 2
			}
			midAngle := startAngle + sweepAngle/2
			lx := cx + labelR*math.Cos(midAngle)
			ly := cy + labelR*math.Sin(midAngle)
			pct := fmt.Sprintf("%.0f%%", fraction*100)
			fontSize := radius * 0.18
			if fontSize < 10 {
				fontSize = 10
			}
			if fontSize > 36 {
				fontSize = 36
			}
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#fff" font-weight="bold" text-anchor="middle" dominant-baseline="central">%s</text>`,
				lx, ly, rulesDevice.KDeviceFontFamily, fontSize, pct)
		}

		startAngle = endAngle
	}

	// Donut center: show total with font proportional to inner radius
	if e.donut {
		centerFont := innerRadius * 0.45
		if centerFont < 12 {
			centerFont = 12
		}
		if centerFont > 48 {
			centerFont = 48
		}
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%.0f" fill="#ddd" font-weight="bold" text-anchor="middle" dominant-baseline="central">%.0f</text>`,
			cx, cy, rulesDevice.KDeviceFontFamily, centerFont, total)
	}

	// Legend
	if e.showLegend {
		ly := h - legendH/2 - 4
		xc := 16.0
		for i, s := range e.slices {
			if s.Value <= 0 {
				continue
			}
			color := e.getSliceColor(i)
			lbl := piePortName(i)
			if s.Label != "" {
				lbl = s.Label
				if len(lbl) > 10 {
					lbl = lbl[:10] + "…"
				}
			}
			svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="5" fill="%s"/>`, xc+5, ly, color)
			svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="18" fill="#bbb" dominant-baseline="central">%s</text>`,
				xc+14, ly, rulesDevice.KDeviceFontFamily, lbl)
			xc += 14 + float64(len(lbl))*10 + 16
		}
	}

	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="20" font-family="%s" font-size="18" fill="#FF8833" text-anchor="end">🔒</text>`, w-12, rulesDevice.KDeviceFontFamily)
	}
	svg += `</svg>`
	return svg
}

func (e *StatementPieChart) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementPieChart) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}
func (e *StatementPieChart) RefreshVisual() { e.recacheFrontend() }

func (e *StatementPieChart) initSlices() {
	e.slices = make([]pieSlice, e.sliceCount)
	for i := range e.slices {
		e.slices[i] = pieSlice{Color: rulesFrontend.KSeriesPalette[i%rulesFrontend.KSeriesMax], Value: 0}
	}
}

// ── Init ──────────────────────────────────────────────────────────────

func (e *StatementPieChart) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}
	e.SetName("pie")
	e.backendWidth = kPieBackendWidth
	e.frontendWidth = 200
	e.frontendHeight = 200
	e.sliceCount = 3
	e.donut = false
	e.showLegend = true
	e.showPercent = true
	e.initSlices()
	e.backendHeight = rulesDensity.Density(e.backendBodyHeight())
	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id

	if e.backendStage != nil {
		e.backendElem, err = e.backendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_back", Width: float64(kPieBackendWidth), Height: e.backendTotalHeight(),
			Index: rulesZIndex.Display, SvgXml: e.renderBackendSVG(),
		})
		if err != nil {
			return fmt.Errorf("backend element: %w", err)
		}
		e.backendElem.SetMinSizeD(100, rulesDensity.Density(e.backendTotalHeight()))
		e.wireBackendEvents()
	}
	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front", X: 100, Y: 100,
			Width: e.frontendWidth.GetFloat(), Height: e.frontendHeight.GetFloat(),
			Index: rulesZIndex.DisplayFrontend, SvgXml: e.renderFrontendSVG(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(100, 100)
		if e.resizerButton != nil {
			adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
			if err2 := e.frontendElem.SetResizeButtons(adapter); err2 != nil {
				log.Printf("[Pie] ERROR: SetResizeButtons: %v", err2)
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

// ── Backend events ────────────────────────────────────────────────────

func (e *StatementPieChart) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}
		ex, ey := e.backendElem.GetPosition()
		mx, my := ex+event.LocalX, ey+event.LocalY
		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}
		for i := 0; i < e.sliceCount; i++ {
			cy := e.connectorY(i)
			if rulesConnection.PinHit(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), cy,
				event.LocalX, event.LocalY) {
				go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, piePortName(i)), mx, my)
				return
			}
		}
		if event.LocalY <= e.backendBodyHeight() {
			go e.backendCtxMenu.OpenForDevice(e, e.getBackendMenuItems(), mx, my)
		}
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
		if ly > e.backendBodyHeight() {
			return ""
		}
		for i := 0; i < e.sliceCount; i++ {
			cy := e.connectorY(i)
			if rulesConnection.PinHit(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), cy, lx, ly) {
				return sprite.CursorPointer
			}
		}
		return ""
	})
}

// ── Frontend events ───────────────────────────────────────────────────

func (e *StatementPieChart) wireFrontendEvents() {
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
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.frontendCtxMenu == nil {
			return
		}
		ex, ey := e.frontendElem.GetPosition()
		clickWX, clickWY := ex+event.LocalX, ey+event.LocalY
		go e.frontendCtxMenu.OpenForDevice(e, e.frontendContextItems(), clickWX, clickWY)
	})
}

// frontendContextItems returns the frontend menu list. For this
// device it is just Resize. Inspect is intentionally absent — it
// is a backend-only concept per decision D10.
//
// Português: Lista do menu frontend. Apenas Resize.
func (e *StatementPieChart) frontendContextItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.ResizeItem(func() {
			e.SetResizeEnable(!e.GetResizeEnable())
			log.Printf("[PieChart:%s] resize toggled to %v", e.id, e.GetResizeEnable())
		}),
	}
}

// ── Hex menu ──────────────────────────────────────────────────────────

// getBackendMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementPieChart) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[PieChart] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[PieChart] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// ── Inspect ───────────────────────────────────────────────────────────

func (e *StatementPieChart) showInspectOverlay() { overlay.Show(e.GetInspectConfig().(overlay.Config)) }

func (e *StatementPieChart) GetInspectConfig() interface{} {
	bs := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}
	saveFn := func(v map[string]string) { e.ApplyProperties(v) }

	globalFields := []overlay.Field{
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
		{Key: "chartTitle", Label: translate.T("propChartTitle", "Title"), Type: overlay.FieldText, Value: e.chartTitle, Placeholder: "Distribution"},
		{Key: "sliceCount", Label: translate.T("propSliceCount", "Slice Count"), Type: overlay.FieldNumber, Value: strconv.Itoa(e.sliceCount), Min: strconv.Itoa(kPieMinSlices), Max: strconv.Itoa(kPieMaxSlices)},
		{Key: "donut", Label: translate.T("propDonut", "Donut Mode"), Type: overlay.FieldCheckbox, Value: bs(e.donut)},
		{Key: "showLegend", Label: translate.T("propShowLegend", "Show Legend"), Type: overlay.FieldCheckbox, Value: bs(e.showLegend)},
		{Key: "showPercent", Label: translate.T("propShowPercent", "Show Percentages"), Type: overlay.FieldCheckbox, Value: bs(e.showPercent)},
		{Key: "interactionLocked", Label: translate.T("propLockInteraction", "Lock Interaction"), Type: overlay.FieldCheckbox, Value: bs(e.interactionLocked)},
	}

	var sliceFields []overlay.Field
	for i := 0; i < e.sliceCount; i++ {
		p := fmt.Sprintf("slice_%d_", i)
		sl, sc := "", rulesFrontend.KSeriesPalette[i%rulesFrontend.KSeriesMax]
		if i < len(e.slices) {
			sl = e.slices[i].Label
			if e.slices[i].Color != "" {
				sc = e.slices[i].Color
			}
		}
		sliceFields = append(sliceFields,
			overlay.Field{Key: p + "header", Label: fmt.Sprintf("Slice %d", i), Type: overlay.FieldText, Value: fmt.Sprintf("port: %s", piePortName(i)), ReadOnly: true},
			overlay.Field{Key: p + "label", Label: translate.T("propSeriesLabel", "Label"), Type: overlay.FieldText, Value: sl, Placeholder: fmt.Sprintf("Slice %d", i)},
			overlay.Field{Key: p + "color", Label: translate.T("propSeriesColor", "Color"), Type: overlay.FieldColor, Value: sc},
		)
	}

	return overlay.Config{Title: e.id, Width: "480px", Tabs: []overlay.Tab{
		{Label: translate.T("tabProperties", "Properties"), Type: overlay.TabForm, Fields: globalFields},
		{Label: translate.T("tabSlices", "Slices"), Type: overlay.TabForm, Fields: sliceFields},
		{Label: translate.T("tabHelp", "Help"), Type: overlay.TabMarkdown, ContentURL: "/help/devices/display/statementPieChart.md"},
	}, OnSave: saveFn}
}

func (e *StatementPieChart) ApplyProperties(v map[string]string) {
	if val, ok := v["comment"]; ok {
		e.comment = val
	}
	ch, rc := false, false
	if val, ok := v["id"]; ok && val != "" && val != e.id {
		old := e.id
		if e.wireMgr != nil {
			e.wireMgr.UnregisterElement(old)
		}
		e.id = val
		if e.label == old {
			e.label = val
		}
		rc = true
		ch = true
	}
	if val, ok := v["label"]; ok && val != e.label {
		e.label = val
		ch = true
	}
	if val, ok := v["chartTitle"]; ok && val != e.chartTitle {
		e.chartTitle = val
		ch = true
	}
	if val, ok := v["sliceCount"]; ok {
		if n, err := strconv.Atoi(val); err == nil && n >= kPieMinSlices && n <= kPieMaxSlices && n != e.sliceCount {
			e.sliceCount = n
			e.initSlices()
			rc = true
			ch = true
			e.backendHeight = rulesDensity.Density(e.backendBodyHeight())
			if e.backendElem != nil {
				e.backendElem.SetSizeD(kPieBackendWidth, rulesDensity.Density(e.backendTotalHeight()))
			}
		}
	}
	if val, ok := v["donut"]; ok {
		b := val == "true"
		if b != e.donut {
			e.donut = b
			ch = true
		}
	}
	if val, ok := v["showLegend"]; ok {
		b := val == "true"
		if b != e.showLegend {
			e.showLegend = b
			ch = true
		}
	}
	if val, ok := v["showPercent"]; ok {
		b := val == "true"
		if b != e.showPercent {
			e.showPercent = b
			ch = true
		}
	}
	if val, ok := v["interactionLocked"]; ok {
		b := val == "true"
		if b != e.interactionLocked {
			e.interactionLocked = b
			ch = true
		}
	}
	if val, ok := v["frontendWidth"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			e.frontendWidth = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			ch = true
		}
	}
	if val, ok := v["frontendHeight"]; ok {
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			e.frontendHeight = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			ch = true
		}
	}

	for i := 0; i < e.sliceCount && i < len(e.slices); i++ {
		p := fmt.Sprintf("slice_%d_", i)
		if val, ok := v[p+"label"]; ok && val != e.slices[i].Label {
			e.slices[i].Label = val
			ch = true
		}
		if val, ok := v[p+"color"]; ok && val != "" && val != e.slices[i].Color {
			e.slices[i].Color = val
			ch = true
		}
	}
	if rc {
		e.RegisterConnectors()
	}
	if ch {
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

// ── Wire registration ─────────────────────────────────────────────────

func (e *StatementPieChart) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}
	e.wireMgr.UnregisterElement(e.id)
	for i := 0; i < e.sliceCount; i++ {
		idx := i
		pn := piePortName(i)
		e.wireMgr.RegisterConnector(wire.ConnectorInfo{
			ID: wire.ConnectorID{ElementID: e.id, PortName: pn}, IsOutput: false,
			AllowedTypes: []string{"int", "float64"}, AcceptNotConnected: true, MaxConnections: 1, Label: pn,
			PositionFunc: func() (float64, float64) {
				ex, ey := e.backendElem.GetPosition()
				ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
					rulesConnection.PinBodyInset(), e.connectorY(idx))
				return ex + ax, ey + ay
			},
		})
	}
}

// ── Live communication ────────────────────────────────────────────────

func (e *StatementPieChart) LiveUpdate(port string, value []byte) error {
	if !strings.HasPrefix(port, "s") {
		return fmt.Errorf("pie %s: unknown port %q", e.id, port)
	}
	idx, err := strconv.Atoi(port[1:])
	if err != nil || idx < 0 || idx >= e.sliceCount {
		return fmt.Errorf("pie %s: invalid slice in port %q", e.id, port)
	}
	var num float64
	if err := json.Unmarshal(value, &num); err != nil {
		var s string
		if err2 := json.Unmarshal(value, &s); err2 != nil {
			return fmt.Errorf("pie %s: parse: %w", e.id, err)
		}
		p, err3 := strconv.ParseFloat(s, 64)
		if err3 != nil {
			return fmt.Errorf("pie %s: parse string: %w", e.id, err3)
		}
		num = p
	}
	if idx < len(e.slices) {
		e.slices[idx].Value = num
	}
	now := time.Now().UnixMilli()
	if now-e.lastRenderMs >= kPieRenderMs {
		e.lastRenderMs = now
		e.recacheBackend()
		e.recacheFrontend()
	}
	return nil
}

func (e *StatementPieChart) SendValue(port string, value float64) {
	if e.SendFunc == nil || e.interactionLocked {
		return
	}
	e.SendFunc(e.id, port, value)
}

// ── Serialization ─────────────────────────────────────────────────────

// GetComment returns the user comment shown in the device's hover tooltip.
// Português: Retorna o comentário exibido no tooltip de hover do device.
func (e *StatementPieChart) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementPieChart) SetComment(c string) { e.comment = c }

func (e *StatementPieChart) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label": e.label, "chartTitle": e.chartTitle, "sliceCount": e.sliceCount,
		"donut": e.donut, "showLegend": e.showLegend, "showPercent": e.showPercent,
		"interactionLocked": e.interactionLocked, "frontendWidth": e.frontendWidth.GetFloat(), "frontendHeight": e.frontendHeight.GetFloat(),
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	for i, s := range e.slices {
		p := fmt.Sprintf("slice_%d_", i)
		props[p+"label"] = s.Label
		props[p+"color"] = s.Color
	}
	return props
}

// ── State accessors ───────────────────────────────────────────────────

func (e *StatementPieChart) GetInitialized() bool   { return e.initialized }
func (e *StatementPieChart) GetID() string          { return e.id }
func (e *StatementPieChart) GetName() string        { return e.name }
func (e *StatementPieChart) GetSelected() bool      { return e.selected }
func (e *StatementPieChart) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementPieChart) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementPieChart) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementPieChart) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementPieChart) GetResize() bool        { return false }
func (e *StatementPieChart) GetResizeEnable() bool {
	if e.frontendElem != nil {
		return e.frontendElem.IsResizeEnabled()
	}
	return false
}
func (e *StatementPieChart) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}
func (e *StatementPieChart) SetSelected(sel bool) { e.selected = sel; e.SetDragEnable(sel) }
func (e *StatementPieChart) SetDragEnable(en bool) {
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
func (e *StatementPieChart) SetResizeEnable(enabled bool) {
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
func (e *StatementPieChart) SelectedInvert() { e.SetSelected(!e.selected) }
func (e *StatementPieChart) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementPieChart) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementPieChart) SetWidth(_ rulesDensity.Density)  {}
func (e *StatementPieChart) SetHeight(_ rulesDensity.Density) {}
func (e *StatementPieChart) SetSize(w, h rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendWidth = w
		e.frontendHeight = h
		e.frontendElem.SetSizeD(w, h)
	}
}
func (e *StatementPieChart) SetStatus(s int) { e.iconStatus = s }
func (e *StatementPieChart) GetStatus() int  { return e.iconStatus }

// ── Icon ──────────────────────────────────────────────────────────────

func (e *StatementPieChart) GetIconName() string     { return "Pie Chart" }
func (e *StatementPieChart) GetIconCategory() string { return "Display" }
func (e *StatementPieChart) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	iconLabel := factoryBrowser.NewTagSvgText().FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).Text("◔").Fill(data.ColorIcon).X((rulesIcon.Width / 2).GetInt() - 8).Y((rulesIcon.Height / 2).GetInt() + 5)
	wl, _ := utilsText.GetTextSize(data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight, rulesIcon.FontStyle, data.LabelFontSize.GetInt())
	label := factoryBrowser.NewTagSvgText().FontFamily(rulesIcon.FontFamily).FontWeight(rulesIcon.FontWeight).FontStyle(rulesIcon.FontStyle).FontSize(data.LabelFontSize.GetInt()).Text(data.Label).Fill(data.ColorLabel).X((rulesIcon.Width / 2).GetInt() - wl/2).Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, iconLabel, label)
	rw := rulesIcon.Width * rulesIcon.SizeRatio
	rh := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: rw.GetInt(), Height: rh.GetInt()})
}

// ── Scene export ──────────────────────────────────────────────────────

func (e *StatementPieChart) GetDeviceType() string { return "StatementPieChart" }
func (e *StatementPieChart) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementPieChart) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementPieChart) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementPieChart) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementPieChart) GetLabel() string         { return e.label }
func (e *StatementPieChart) SetLabel(label string)    { e.label = label; e.recacheBackend() }
func (e *StatementPieChart) MoveBy(dx, dy float64) {
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
func (e *StatementPieChart) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
