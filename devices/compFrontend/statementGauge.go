// /ide/devices/compFrontend/statementGauge.go
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

// StatementGauge — dual device: backend data node + frontend gauge visualization.
// Creating or deleting affects both workspaces. Positions are independent.
//
// The frontend element renders an interactive speedometer gauge. When the user
// clicks the gauge, a slider overlay appears allowing them to send values back
// to external hardware. This interaction can be disabled via the "Lock
// Interaction" checkbox in the Inspect panel — useful for dashboards where the
// gauge should be read-only.
//
// Português: Elemento dual: nó de dados no backend + gauge visual no frontend.
// A interação do usuário (slider) pode ser bloqueada via "Lock Interaction"
// no painel de inspeção — útil para dashboards somente leitura.
type StatementGauge struct {
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

	// Backend dimensions (box only, without label)
	backendWidth  rulesDensity.Density
	backendHeight rulesDensity.Density

	// Frontend dimensions
	frontendWidth  rulesDensity.Density
	frontendHeight rulesDensity.Density

	pendingDragEnable *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller for the
	// backend stage. Devices without a frontend menu only
	// need this one.
	backendCtxMenu *contextMenu.Controller
	wireMgr        *wire.Manager // always the BACKEND wire manager

	// Editable label displayed below the backend element.
	// Defaults to the device id. Edited via double-click.
	//
	// Português: Label editável exibido abaixo do elemento backend.
	// Padrão é o id do device. Editado via duplo-clique.
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
	canvasEl js.Value // <canvas> DOM element for positioning the input overlay

	// Values
	minValue     int64
	maxValue     int64
	currentValue int64

	// interactionLocked prevents the end-user from opening the slider overlay
	// on the frontend gauge. When true, the gauge is read-only — it still
	// receives LiveUpdate values from hardware, but the user cannot send
	// values back. Controlled via the "Lock Interaction" checkbox in the
	// Inspect panel.
	//
	// This is a standard pattern for all compFrontend components: every
	// component that allows the user to send data back to hardware must
	// expose this lock in its Inspect panel.
	//
	// Português: Quando true, o gauge é somente leitura — ainda recebe
	// LiveUpdate do hardware, mas o usuário não pode enviar valores de volta.
	// Controlado via checkbox "Lock Interaction" no painel de inspeção.
	// Padrão para todos os componentes compFrontend.
	interactionLocked bool

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

	// SendFunc is the callback for sending values to external hardware via
	// the live WebSocket connection. Set by the factory after initialization.
	// Signature: func(deviceID, port string, value interface{})
	//
	// Português: Callback para enviar valores ao hardware externo via WebSocket.
	// Definido pelo factory após inicialização.
	SendFunc func(deviceID, port string, value interface{})
}

func (e *StatementGauge) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementGauge) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementGauge) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementGauge) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementGauge) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementGauge) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementGauge) SetCanvasEl(el js.Value) { e.canvasEl = el }

func (e *StatementGauge) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

// Remove removes from BOTH workspaces.
func (e *StatementGauge) Remove() {
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

// SetOnRemove sets the callback invoked when the device is removed.
func (e *StatementGauge) SetOnRemove(fn func(id string)) {
	e.onRemove = fn
}

func (e *StatementGauge) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementGauge) Get() *html.TagSvg { return nil }

// SetPosition sets the BACKEND element position.
func (e *StatementGauge) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}

// SetFrontendPosition sets the FRONTEND element position.
func (e *StatementGauge) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementGauge) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}

func (e *StatementGauge) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementGauge) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementGauge) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementGauge) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG: rectangle with 3 horizontal sections + input connectors
// =====================================================================

// backendLabelHeight is the pixel height reserved for the editable label below the box.
const backendLabelHeight = 18

// backendTotalHeight returns the total SVG/element height including the label area.
func (e *StatementGauge) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendLabelHeight
}

func (e *StatementGauge) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + backendLabelHeight
	rowH := boxH / 3.0
	bw := 2.0

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))

	// [PIN] the body is inset on the LEFT by the pin length: the three
	// standard pins (max/current/min rows) live in the freed margin, wires
	// anchored at their outer tips — the element's left edge.
	// Português: O corpo recua à ESQUERDA o comprimento do pino: os três
	// pinos padrão (linhas max/current/min) vivem na margem liberada, fios
	// ancorados nas pontas externas — a borda esquerda do element.
	pin := rulesConnection.PinBodyInset()

	// Outer rectangle (box only, not including label area)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="4" ry="4" fill="#2a3040" stroke="#88AACC" stroke-width="%.1f"/>`, pin+bw/2, bw/2, w-pin-bw, boxH-bw, bw)

	// Horizontal divider lines
	svg += fmt.Sprintf(`<line x1="2" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#555" stroke-width="0.5"/>`, rowH, w-2, rowH)
	svg += fmt.Sprintf(`<line x1="2" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#555" stroke-width="0.5"/>`, 2*rowH, w-2, 2*rowH)

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
		// [PIN] standard connector pin per row, gauge accent fill.
		// Português: Pino padrão por linha, na cor de destaque do gauge.
		svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideLeft, pin, r.y, "#4488CC")
		// Label
		svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="10" fill="#AAAAAA" dominant-baseline="central">%s</text>`, r.y, r.label)
		// Value
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="12" fill="#FFFFFF" text-anchor="end" dominant-baseline="central" font-weight="bold">%d</text>`, w-12, r.y, r.value)
	}

	// Lock icon indicator — when interaction is locked, show a small lock
	// symbol in the top-right corner of the backend box so the maker knows
	// at a glance that the frontend is read-only.
	//
	// Português: Indicador de cadeado — quando a interação está bloqueada,
	// mostra um símbolo de cadeado no canto superior direito da caixa.
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="12" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="10" fill="#FF8833" text-anchor="end">🔒</text>`, w-4)
	}

	// Editable label below the box (left-aligned)
	// Português: Label editável abaixo da caixa (alinhado à esquerda).
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(
		rulesDevice.KDeviceLabel,
		boxH+3,
		displayLabel,
	)

	svg += `</svg>`
	return svg
}

// =====================================================================
//  Frontend SVG: semicircular gauge (speedometer)
// =====================================================================

func (e *StatementGauge) renderFrontendSVG() string {
	// Render at 3x internal resolution for crisp arc edges when the bitmap
	// is displayed on the canvas. The element size on the canvas is still
	// controlled by frontendWidth/frontendHeight — only the SVG rasterization
	// benefits from the higher pixel count.
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	cx := w / 2
	cy := h * 0.68
	r := w * 0.38
	strokeW := 24.0

	// Background arc endpoints (full semicircle left to right)
	bgStartX := cx - r
	bgEndX := cx + r

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

	// Value arc endpoint — both arcs share the same center, radius, and start point.
	// The value arc sweeps clockwise from the left endpoint towards the right,
	// covering ratio * 180°. Since this never exceeds 180°, the SVG large-arc-flag
	// is always 0 (short arc). Setting it to 1 would draw the arc the wrong way
	// around the circle (going down instead of up).
	angle := math.Pi * (1 - ratio)
	valEndX := cx + r*math.Cos(angle)
	valEndY := cy - r*math.Sin(angle)

	// Color based on ratio
	arcColor := "#44CC44" // green
	if ratio > 0.75 {
		arcColor = "#CC4444"
	} else if ratio > 0.5 {
		arcColor = "#CCAA44"
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="12" ry="12" fill="#1a1a2e"/>`, int(w), int(h))

	// Label
	svg += fmt.Sprintf(`<text x="%.1f" y="36" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="28" fill="#88AACC" text-anchor="middle">GAUGE</text>`, cx)

	// Background arc (gray) — full semicircle from left to right
	svg += fmt.Sprintf(`<path d="M %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f" fill="none" stroke="#333333" stroke-width="%.1f" stroke-linecap="round"/>`,
		bgStartX, cy, r, r, bgEndX, cy, strokeW)

	// Value arc (colored) — same start point, same radius, partial sweep
	if ratio > 0.01 {
		svg += fmt.Sprintf(`<path d="M %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f" fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round"/>`,
			bgStartX, cy, r, r, valEndX, valEndY, arcColor, strokeW)
	}

	// Current value text
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="60" fill="#FFFFFF" text-anchor="middle" font-weight="bold">%d</text>`, cx, cy-8, e.currentValue)

	// Min / Max labels
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="24" fill="#666666">%d</text>`, bgStartX-4, cy+40, e.minValue)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="24" fill="#666666" text-anchor="end">%d</text>`, bgEndX+4, cy+40, e.maxValue)

	// Lock indicator on the frontend gauge — subtle lock icon at top-right
	// so the end-user knows the gauge is read-only and won't respond to clicks.
	//
	// Português: Indicador de cadeado no gauge frontend — ícone sutil no
	// canto superior direito para que o usuário saiba que o gauge é somente leitura.
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="36" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="22" fill="#FF8833" text-anchor="end">🔒</text>`, w-12)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementGauge) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementGauge) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// =====================================================================
//  Init — creates elements on BOTH stages
// =====================================================================

func (e *StatementGauge) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("gauge")
	e.backendWidth = 130
	e.backendHeight = 90
	e.frontendWidth = 160
	e.frontendHeight = 120
	e.maxValue = 100
	e.minValue = 0
	e.currentValue = 50

	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id // default label is the device id
	e.resizeLocked = true

	// --- Backend element (only if backend stage exists) ---
	// Height includes label area below the box.
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
		e.backendElem.SetMinSizeD(100, 70+backendLabelHeight)
		e.wireBackendEvents()
	}

	// --- Frontend element (only if frontend stage exists) ---
	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front", X: 100, Y: 100,
			Width: e.frontendWidth.GetFloat(), Height: e.frontendHeight.GetFloat(),
			Index: rulesZIndex.DisplayFrontend, DragEnable: false, SvgXml: e.renderFrontendSVG(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(120, 90)
		e.wireFrontendEvents()
	}

	// Hex menu must be injected via SetHexMenu() before Init().
	if e.backendCtxMenu == nil {
		log.Printf("[Gauge] Warning: no shared hex menu set, menus disabled")
	}

	e.initialized = true

	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

func (e *StatementGauge) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}

		_, h := e.backendElem.GetSize()
		boxH := h - backendLabelHeight
		rowH := boxH / 3.0
		elemX, elemY := e.backendElem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY

		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}

		// Ignore clicks in the label area (no connectors there)
		if event.LocalY > boxH {
			return
		}

		// [PIN] hit-test the 3 standard pins — same edge points the renderer
		// draws and the wire anchors use.
		// Português: Testa os 3 pinos padrão — mesmos edge points que o
		// renderer desenha e os fios ancoram.
		ports := []string{"max", "current", "min"}
		centers := []float64{rowH / 2, rowH + rowH/2, 2*rowH + rowH/2}
		for i, cy := range centers {
			if rulesConnection.PinHit(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), cy,
				event.LocalX, event.LocalY) {
				go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, ports[i]), menuX, menuY)
				return
			}
		}

		go e.backendCtxMenu.OpenForDevice(e, e.getBodyMenuItems(), menuX, menuY)
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
		boxH := h - backendLabelHeight
		rowH := boxH / 3.0

		if ly > boxH {
			return ""
		}

		centers := []float64{rowH / 2, rowH + rowH/2, 2*rowH + rowH/2}
		for _, cy := range centers {
			if rulesConnection.PinHit(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), cy, lx, ly) {
				return sprite.CursorPointer
			}
		}
		return ""
	})
}

func (e *StatementGauge) wireFrontendEvents() {
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

	// Click on the frontend gauge opens an interactive slider overlay
	// so the end-user can send values back to external hardware.
	// Blocked when interactionLocked is true.
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.interactionLocked {
			log.Printf("[Gauge:%s] interaction locked — ignoring click", e.id)
			return
		}
		go e.showFrontendSlider()
	})
}

// =====================================================================
//  Frontend interactive slider — HTML overlay for end-user value input.
// =====================================================================

const frontendSliderCSS = "gauge-slider-css"
const frontendSliderID = "gauge-slider-overlay"

func (e *StatementGauge) showFrontendSlider() {
	doc := js.Global().Get("document")

	existing := doc.Call("getElementById", frontendSliderID)
	if existing.Truthy() {
		existing.Get("parentNode").Call("removeChild", existing)
	}

	e.injectSliderCSS()

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

func (e *StatementGauge) injectSliderCSS() {
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
//  Label editing
// =====================================================================

func (e *StatementGauge) GetLabel() string { return e.label }

func (e *StatementGauge) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementGauge) showLabelEditor() {
	if e.backendElem == nil {
		log.Printf("[Gauge] showLabelEditor: backendElem is nil")
		return
	}
	if e.canvasEl.IsUndefined() || e.canvasEl.IsNull() {
		log.Printf("[Gauge] showLabelEditor: canvasEl not set")
		return
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.createLabelInput()
	}()
}

func (e *StatementGauge) createLabelInput() {
	doc := js.Global().Get("document")

	elemX, elemY := e.backendElem.GetPosition()
	labelWorldX := elemX + 2
	labelWorldY := elemY + e.backendHeight.GetFloat()

	cam := e.backendStage.GetCamera()
	zoom := 1.0
	offsetX := 0.0
	offsetY := 0.0
	if cam != nil {
		zoom = cam.Zoom
		if zoom <= 0 {
			zoom = 1.0
		}
		offsetX = cam.OffsetX
		offsetY = cam.OffsetY
	}

	canvasPxX := labelWorldX*zoom + offsetX
	canvasPxY := labelWorldY*zoom + offsetY
	canvasPxW := e.backendWidth.GetFloat() * zoom

	rect := e.canvasEl.Call("getBoundingClientRect")
	canvasW := e.canvasEl.Get("width").Float()
	canvasH := e.canvasEl.Get("height").Float()
	rectW := rect.Get("width").Float()
	rectH := rect.Get("height").Float()
	rectLeft := rect.Get("left").Float()
	rectTop := rect.Get("top").Float()

	cssScaleX := rectW / canvasW
	cssScaleY := rectH / canvasH
	if canvasW == 0 || canvasH == 0 {
		cssScaleX = 1
		cssScaleY = 1
	}

	screenX := rectLeft + canvasPxX*cssScaleX
	screenY := rectTop + canvasPxY*cssScaleY
	inputW := canvasPxW * cssScaleX

	input := doc.Call("createElement", "input")
	input.Set("type", "text")
	input.Set("value", e.label)

	style := input.Get("style")
	style.Set("position", "fixed")
	style.Set("left", fmt.Sprintf("%.0fpx", screenX))
	style.Set("top", fmt.Sprintf("%.0fpx", screenY))
	style.Set("width", fmt.Sprintf("%.0fpx", inputW))
	style.Set("height", fmt.Sprintf("%dpx", backendLabelHeight))
	style.Set("zIndex", "10000")
	style.Set("background", "#1a1a2e")
	style.Set("color", "#AABBCC")
	style.Set("border", "1px solid #4488CC")
	style.Set("borderRadius", "2px")
	style.Set("fontFamily", rulesDevice.KDeviceFontFamily)
	style.Set("fontSize", "11px")
	style.Set("padding", "0 4px")
	style.Set("outline", "none")
	style.Set("boxSizing", "border-box")

	doc.Get("body").Call("appendChild", input)
	input.Call("focus")
	input.Call("select")

	committed := false
	commit := func() {
		if committed {
			return
		}
		committed = true

		newLabel := input.Get("value").String()
		if newLabel != "" {
			e.label = newLabel
		}

		parent := input.Get("parentNode")
		if !parent.IsNull() && !parent.IsUndefined() {
			parent.Call("removeChild", input)
		}

		go func() {
			e.recacheBackend()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	}

	input.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			key := args[0].Get("key").String()
			if key == "Enter" {
				args[0].Call("preventDefault")
				commit()
			} else if key == "Escape" {
				committed = true
				parent := input.Get("parentNode")
				if !parent.IsNull() && !parent.IsUndefined() {
					parent.Call("removeChild", input)
				}
			}
			return nil
		}),
	)

	input.Call("addEventListener", "blur",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			commit()
			return nil
		}),
	)
}

// =====================================================================
//  Hex menu items (backend)
// =====================================================================

// getBodyMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementGauge) getBodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Gauge] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[Gauge] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay — properties panel
// =====================================================================

func (e *StatementGauge) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

// GetInspectConfig returns the overlay configuration for this device.
// Implements scene.Inspectable.
//
// The "Lock Interaction" checkbox is a standard field for all compFrontend
// components. When checked, the end-user cannot interact with the frontend
// element (e.g. no slider overlay for the gauge). The component still
// receives LiveUpdate data from hardware — it becomes read-only.
func (e *StatementGauge) GetInspectConfig() interface{} {
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
					{
						Key:   "id",
						Label: "ID",
						Type:  overlay.FieldText,
						Value: e.id,
					},
					{
						Key:   "label",
						Label: translate.T("propLabel", "Label"),
						Type:  overlay.FieldText,
						Value: e.label,
					},
					{
						Key:         "comment",
						Label:       translate.T("propComment", "Comment"),
						Type:        overlay.FieldTextarea,
						Value:       e.comment,
						Placeholder: translate.T("propCommentPlaceholder", "Comment shown on hover..."),
						Rows:        3,
					},
					{
						Key:         "min",
						Label:       "Min",
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatInt(e.minValue, 10),
						Placeholder: "0",
					},
					{
						Key:         "max",
						Label:       "Max",
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatInt(e.maxValue, 10),
						Placeholder: "100",
					},
					{
						Key:         "current",
						Label:       translate.T("propValue", "Value"),
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatInt(e.currentValue, 10),
						Placeholder: "50",
					},
					{
						Key:   "interactionLocked",
						Label: translate.T("propLockInteraction", "Lock Interaction"),
						Type:  overlay.FieldCheckbox,
						Value: lockValue,
					},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementGauge.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

// ApplyProperties applies the values from the inspect form to this device.
func (e *StatementGauge) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	changed := false

	// ── ID change ──
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
		log.Printf("[Gauge] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
		log.Printf("[Gauge] %s: label set to %q", e.id, v)
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

	// ── Interaction lock (standard compFrontend field) ──
	if v, ok := values["interactionLocked"]; ok {
		newLocked := v == "true"
		if newLocked != e.interactionLocked {
			e.interactionLocked = newLocked
			changed = true
			log.Printf("[Gauge] %s: interactionLocked set to %v", e.id, newLocked)
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

func (e *StatementGauge) RegisterConnectors() {
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
				rowH := h / 3.0
				ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
					rulesConnection.PinBodyInset(), pp.rowIdx*rowH+rowH/2)
				return ex + ax, ey + ay
			},
		})
	}
}

// =====================================================================
//  Live communication (scene.LiveUpdatable)
// =====================================================================

// LiveUpdate receives a real-time value from external hardware via the
// live WebSocket connection and updates the gauge accordingly.
//
// NOTE: LiveUpdate works regardless of interactionLocked. The lock only
// prevents the user from SENDING values — receiving is always allowed.
func (e *StatementGauge) LiveUpdate(port string, value []byte) error {
	var num float64
	if err := json.Unmarshal(value, &num); err != nil {
		var s string
		if err2 := json.Unmarshal(value, &s); err2 != nil {
			return fmt.Errorf("gauge %s: cannot parse value for port %q: %w", e.id, port, err)
		}
		parsed, err3 := strconv.ParseFloat(s, 64)
		if err3 != nil {
			return fmt.Errorf("gauge %s: cannot parse string value %q: %w", e.id, port, err3)
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
		return fmt.Errorf("gauge %s: unknown port %q", e.id, port)
	}

	log.Printf("[Gauge:%s] LiveUpdate port=%s value=%d", e.id, port, intVal)

	e.recacheBackend()
	e.recacheFrontend()

	return nil
}

// SendValue sends the current gauge value to external hardware via the
// live WebSocket connection. Respects interactionLocked.
func (e *StatementGauge) SendValue(port string, value int64) {
	if e.SendFunc == nil {
		return
	}
	if e.interactionLocked {
		log.Printf("[Gauge:%s] SendValue blocked — interaction locked", e.id)
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================

func (e *StatementGauge) GetInitialized() bool   { return e.initialized }
func (e *StatementGauge) GetID() string          { return e.id }
func (e *StatementGauge) GetName() string        { return e.name }
func (e *StatementGauge) GetSelected() bool      { return e.selected }
func (e *StatementGauge) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementGauge) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementGauge) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementGauge) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementGauge) GetResize() bool        { return false }
func (e *StatementGauge) GetResizeEnable() bool  { return false }
func (e *StatementGauge) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementGauge) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementGauge) SetDragEnable(en bool) {
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

func (e *StatementGauge) SetResizeEnable(_ bool) {}
func (e *StatementGauge) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementGauge) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementGauge) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementGauge) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementGauge) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementGauge) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementGauge) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementGauge) GetStatus() int                                         { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementGauge) GetIconName() string     { return "Gauge" }
func (e *StatementGauge) GetIconCategory() string { return "Display" }

func (e *StatementGauge) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("◔").Fill(data.ColorIcon).
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
//  Scene export (backend element)
// =====================================================================

func (e *StatementGauge) GetDeviceType() string { return "StatementGauge" }
func (e *StatementGauge) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementGauge) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementGauge) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementGauge) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementGauge) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// GetProperties returns the codegen-relevant and persist-relevant properties
// for scene serialization. Implements scene.Propertied.
//
// These values are saved in the scene JSON and restored via ApplyProperties
// when the stage is imported.

// GetComment returns the user comment shown in the device's hover tooltip.
// Português: Retorna o comentário exibido no tooltip de hover do device.
func (e *StatementGauge) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementGauge) SetComment(c string) { e.comment = c }

func (e *StatementGauge) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label":             e.label,
		"min":               e.minValue,
		"max":               e.maxValue,
		"current":           e.currentValue,
		"interactionLocked": e.interactionLocked,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementGauge) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementGauge) OpenInspect() { go e.showInspectOverlay() }
