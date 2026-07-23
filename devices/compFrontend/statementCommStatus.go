// /ide/devices/compFrontend/statementCommStatus.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

// statementCommStatus.go — Dual device: backend data node + frontend communication status.
//
// Purpose:
//
//	Visual watchdog timer for network/hardware communication health.
//	The component monitors the time since the last received value and
//	displays a traffic-light status indicator on the frontend canvas:
//
//	  🟢 Green  — communication active (value received within yellowTimeout)
//	  🟡 Yellow — attention: no value received for yellowTimeout seconds
//	  🔴 Red    — no communication: no value received for redTimeout seconds
//
//	When ANY value arrives on the "heartbeat" input connector (or via
//	LiveUpdate from external hardware), the timer resets to zero and the
//	status returns to green immediately.
//
// Backend view:
//
//	Compact box with one input connector ("heartbeat") that accepts any
//	data type. The connector name and type are deliberately generic — the
//	user can wire any periodic signal to it. Click opens hex menu with
//	Delete + Inspect.
//
//	┌─────────────────────────┐
//	│ ◉ CommStatus     🟢 OK │
//	└─────────────────────────┘
//	comm1
//
// Frontend view:
//
//	A circle with a network-wired icon (FontAwesome) inside. The circle color
//	changes based on the current status. Same visual weight as the LED
//	component.
//
// Timer:
//
//	A background goroutine ticks every 500ms and compares time.Since(lastReceived)
//	against the configured thresholds. The goroutine is stopped on Remove()
//	via a done channel to prevent leaks.
//
// Português:
//
//	Watchdog visual para saúde de comunicação de rede/hardware.
//	Três estados: verde (OK), amarelo (atenção), vermelho (sem comunicação).
//	Qualquer valor recebido reseta o timer. Tempos configuráveis no Inspect.

import (
	"fmt"
	"log"
	"strconv"
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

// ── Status constants ─────────────────────────────────────────────────

const (
	commStatusGreen  = 0 // communication active
	commStatusYellow = 1 // attention: approaching timeout
	commStatusRed    = 2 // no communication
)

// ── Default colors and timeouts ──────────────────────────────────────

const (
	kCommGreenColor  = "#44DD88"
	kCommYellowColor = "#DDCC44"
	kCommRedColor    = "#DD4444"
	kCommOffColor    = "#333344"

	kCommBackendWidth   = 140
	kCommBackendHeight  = 36
	kCommFrontendWidth  = 80
	kCommFrontendHeight = 80
	kCommLabelHeight    = 18

	// Default timeout thresholds (seconds).
	kCommDefaultYellowTimeout = 5.0
	kCommDefaultRedTimeout    = 10.0

	// Timer tick interval for checking elapsed time.
	kCommTickInterval = 500 * time.Millisecond
)

// StatementCommStatus — dual device: backend data node + frontend comm status.
type StatementCommStatus struct {
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

	// status is the current communication state (green/yellow/red).
	status int

	// yellowTimeout is the number of seconds before status changes to yellow.
	yellowTimeout float64

	// redTimeout is the number of seconds before status changes to red.
	redTimeout float64

	// lastReceived is the timestamp of the last received value (wire or live).
	// Initialized to time.Now() on Init so the component starts green.
	lastReceived time.Time

	// stopCh signals the timer goroutine to stop on Remove.
	stopCh chan struct{}

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

func (e *StatementCommStatus) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementCommStatus) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementCommStatus) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementCommStatus) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementCommStatus) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementCommStatus) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementCommStatus) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementCommStatus) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementCommStatus) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementCommStatus) Remove() {
	// Stop the timer goroutine first.
	if e.stopCh != nil {
		close(e.stopCh)
		e.stopCh = nil
	}
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	FrontendZRegistry.Unregister(e.id)
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

func (e *StatementCommStatus) SetName(n string) {
	e.name = rulesSequentialId.GetIdFromBase(n)
}
func (e *StatementCommStatus) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementCommStatus) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementCommStatus) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementCommStatus) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementCommStatus) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementCommStatus) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementCommStatus) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementCommStatus) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// ── Backend SVG ──────────────────────────────────────────────────────

func (e *StatementCommStatus) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + kCommLabelHeight
}

func (e *StatementCommStatus) statusColor() string {
	switch e.status {
	case commStatusYellow:
		return kCommYellowColor
	case commStatusRed:
		return kCommRedColor
	default:
		return kCommGreenColor
	}
}

func (e *StatementCommStatus) statusText() string {
	switch e.status {
	case commStatusYellow:
		return "WARN"
	case commStatusRed:
		return "FAIL"
	default:
		return "OK"
	}
}

func (e *StatementCommStatus) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(kCommLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	connY := boxH / 2.0
	borderColor := "#6688AA"
	stColor := e.statusColor()

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))

	// Box
	// [PIN] the body is inset on the LEFT by the pin length: the standard
	// connector pins live in the freed margin, protruding from the border
	// with the wires anchored at their outer tips — the element's left edge.
	// Português: O corpo recua à ESQUERDA o comprimento do pino: os pinos
	// padrão vivem na margem liberada, saindo da borda com os fios ancorados
	// nas pontas externas — a borda esquerda do element.
	pin := rulesConnection.PinBodyInset()
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		pin+bw/2, bw/2, w-pin-bw, boxH-bw,
		rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, borderColor, bw)

	// Input connector circle (left side)
	svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideLeft, pin, connY, borderColor)

	// Type label
	svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">Comm</text>`,
		connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted)

	// Status indicator (right side)
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="6" fill="%s"/>`, w-20, connY, stColor)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central" font-weight="bold">%s</text>`,
		w-30, connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue, stColor, e.statusText())

	// Label
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, boxH+3, displayLabel)

	svg += `</svg>`
	return svg
}

// ── Frontend SVG — network-wired icon with status color ──────────────

func (e *StatementCommStatus) renderFrontendSVG() string {
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	cx := w / 2
	cy := h / 2
	r := w * 0.35
	fillColor := e.statusColor()

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="12" ry="12" fill="#1a1a2e"/>`, int(w), int(h))

	// Glow filter (visible when green or yellow)
	svg += `<defs><filter id="comm-glow" x="-50%" y="-50%" width="200%" height="200%">`
	svg += `<feGaussianBlur in="SourceGraphic" stdDeviation="10" result="blur"/>`
	svg += `<feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>`
	svg += `</filter></defs>`

	// Glow layer
	if e.status != commStatusRed {
		svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" opacity="0.4" filter="url(#comm-glow)"/>`,
			cx, cy, r*0.9, fillColor)
	}

	// Main circle
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="#555555" stroke-width="2"/>`,
		cx, cy, r, fillColor)

	// Network-wired icon (FontAwesome) — scaled to fit inside the circle.
	// The icon's original viewBox is 576×512. We scale it to ~55% of the
	// circle diameter and center it using a <g transform="translate scale">.
	iconColor := "#1a1a2e"
	if e.status == commStatusRed {
		iconColor = "#664444"
	}
	iconSize := r * 1.1
	iconScale := iconSize / 576.0
	tx := cx - (576.0*iconScale)/2.0
	ty := cy - (512.0*iconScale)/2.0
	svg += fmt.Sprintf(`<g transform="translate(%.1f,%.1f) scale(%.4f)">`, tx, ty, iconScale)
	svg += fmt.Sprintf(`<path d="M248 88l80 0 0 48-80 0 0-48zm-8-56c-26.5 0-48 21.5-48 48l0 64c0 26.5 21.5 48 48 48l16 0 0 32-224 0c-17.7 0-32 14.3-32 32s14.3 32 32 32l96 0 0 32-16 0c-26.5 0-48 21.5-48 48l0 64c0 26.5 21.5 48 48 48l96 0c26.5 0 48-21.5 48-48l0-64c0-26.5-21.5-48-48-48l-16 0 0-32 192 0 0 32-16 0c-26.5 0-48 21.5-48 48l0 64c0 26.5 21.5 48 48 48l96 0c26.5 0 48-21.5 48-48l0-64c0-26.5-21.5-48-48-48l-16 0 0-32 96 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l-224 0 0-32 16 0c26.5 0 48-21.5 48-48l0-64c0-26.5-21.5-48-48-48l-96 0zM448 376l8 0 0 48-80 0 0-48l72 0zm-256 0l8 0 0 48-80 0 0-48l72 0z" fill="%s"/>`, iconColor)
	svg += `</g>`

	// Red X overlay when status is red (no communication)
	if e.status == commStatusRed {
		xSize := r * 0.4
		svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#FFFFFF" stroke-width="5" stroke-linecap="round"/>`,
			cx-xSize, cy-xSize, cx+xSize, cy+xSize)
		svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#FFFFFF" stroke-width="5" stroke-linecap="round"/>`,
			cx+xSize, cy-xSize, cx-xSize, cy+xSize)
	}

	// Specular highlight
	svg += fmt.Sprintf(`<ellipse cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f" fill="rgba(255,255,255,0.12)"/>`,
		cx-r*0.25, cy-r*0.3, r*0.35, r*0.2)

	// Status label
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="20" fill="#88AACC" text-anchor="middle">%s</text>`,
		cx, h-14, rulesDevice.KDeviceFontFamily, e.statusText())

	svg += `</svg>`
	return svg
}

func (e *StatementCommStatus) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementCommStatus) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// ── Heartbeat — resets the timer ─────────────────────────────────────

// heartbeat resets the last-received timestamp and updates visual state
// to green if it was yellow or red. Called by LiveUpdate and can also
// be triggered by a wired value arriving on the "heartbeat" connector.
func (e *StatementCommStatus) heartbeat() {
	e.lastReceived = time.Now()
	if e.status != commStatusGreen {
		e.status = commStatusGreen
		e.recacheBackend()
		e.recacheFrontend()
	}
}

// checkStatus compares elapsed time against thresholds and updates
// visual state if needed. Called by the timer goroutine every tick.
func (e *StatementCommStatus) checkStatus() {
	elapsed := time.Since(e.lastReceived).Seconds()

	newStatus := commStatusGreen
	if elapsed >= e.redTimeout {
		newStatus = commStatusRed
	} else if elapsed >= e.yellowTimeout {
		newStatus = commStatusYellow
	}

	if newStatus != e.status {
		e.status = newStatus
		e.recacheBackend()
		e.recacheFrontend()
		log.Printf("[CommStatus:%s] status → %s (elapsed: %.1fs)", e.id, e.statusText(), elapsed)
	}
}

// startTimer launches the background goroutine that periodically checks
// the elapsed time and updates the visual status.
func (e *StatementCommStatus) startTimer() {
	e.stopCh = make(chan struct{})
	go func() {
		ticker := time.NewTicker(kCommTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.checkStatus()
			}
		}
	}()
}

// ── Init ─────────────────────────────────────────────────────────────

func (e *StatementCommStatus) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("comm")
	e.backendWidth = kCommBackendWidth
	e.backendHeight = kCommBackendHeight
	e.frontendWidth = kCommFrontendWidth
	e.frontendHeight = kCommFrontendHeight
	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id
	e.resizeLocked = true
	e.status = commStatusGreen
	e.yellowTimeout = kCommDefaultYellowTimeout
	e.redTimeout = kCommDefaultRedTimeout
	e.lastReceived = time.Now()

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
		e.backendElem.SetMinSizeD(100, kCommBackendHeight+kCommLabelHeight)
		e.wireBackendEvents()
	}

	// --- Frontend element ---
	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front", X: 100, Y: 100,
			Width: e.frontendWidth.GetFloat(), Height: e.frontendHeight.GetFloat(),
			Index: rulesZIndex.DisplayFrontend, DragEnable: false,
			SvgXml: e.renderFrontendSVG(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(60, 60)

		if e.resizerButton != nil {
			adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
			if err2 := e.frontendElem.SetResizeButtons(adapter); err2 != nil {
				log.Printf("[CommStatus] ERROR: SetResizeButtons failed: %v", err2)
			}
			e.frontendElem.ShowResizeButtons(false)
			e.frontendElem.SetResizeEnable(false)
		}

		e.wireFrontendEvents()
		FrontendZRegistry.Register(e.id, e.frontendElem)
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	// Start the watchdog timer.
	e.startTimer()

	return nil
}

// ── Backend events ───────────────────────────────────────────────────

func (e *StatementCommStatus) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}
		_, h := e.backendElem.GetSize()
		boxH := h - float64(kCommLabelHeight)
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

		// Hit-test the input connector
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), connY,
			event.LocalX, event.LocalY) {
			go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, "heartbeat"), menuX, menuY)
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
		boxH := h - float64(kCommLabelHeight)
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

// ── Frontend events ──────────────────────────────────────────────────

func (e *StatementCommStatus) wireFrontendEvents() {
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

// ── Hex menu ─────────────────────────────────────────────────────────

// getBackendMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementCommStatus) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[CommStatus] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[CommStatus] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// ── Inspect overlay ──────────────────────────────────────────────────

func (e *StatementCommStatus) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementCommStatus) GetInspectConfig() interface{} {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
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
					{
						Key:         "yellowTimeout",
						Label:       translate.T("propYellowTimeout", "Warning (s)"),
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatFloat(e.yellowTimeout, 'f', 1, 64),
						Min:         "1",
						Max:         "3600",
						Placeholder: "seconds",
					},
					{
						Key:         "redTimeout",
						Label:       translate.T("propRedTimeout", "Fail (s)"),
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatFloat(e.redTimeout, 'f', 1, 64),
						Min:         "2",
						Max:         "7200",
						Placeholder: "seconds",
					},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementCommStatus.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementCommStatus) ApplyProperties(values map[string]string) {
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
		log.Printf("[CommStatus] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}

	if v, ok := values["yellowTimeout"]; ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 1 {
			if f != e.yellowTimeout {
				e.yellowTimeout = f
				changed = true
			}
		}
	}

	if v, ok := values["redTimeout"]; ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 2 {
			if f != e.redTimeout {
				e.redTimeout = f
				changed = true
			}
		}
	}

	// Ensure red > yellow.
	if e.redTimeout <= e.yellowTimeout {
		e.redTimeout = e.yellowTimeout + 1
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

// ── Wire registration ────────────────────────────────────────────────

func (e *StatementCommStatus) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}

	// Single input connector that accepts any common type.
	// When ANY value arrives via wire, the heartbeat resets.
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "heartbeat"},
		IsOutput:           false,
		AllowedTypes:       []string{"bool", "int", "float32", "float64", "string"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     1,
		Label:              "heartbeat",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.backendElem.GetPosition()
			_, h := e.backendElem.GetSize()
			boxH := h - float64(kCommLabelHeight)
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), boxH/2)
			return ex + ax, ey + ay
		},
	})
}

// ── LiveUpdate — receives values from external hardware ──────────────

// LiveUpdate accepts any value on the "heartbeat" port and resets the
// communication timer. The value content is ignored — only the fact
// that something arrived matters.
func (e *StatementCommStatus) LiveUpdate(port string, value []byte) error {
	if port != "heartbeat" {
		return fmt.Errorf("commstatus %s: unknown port %q", e.id, port)
	}
	e.heartbeat()
	return nil
}

// ── State accessors ──────────────────────────────────────────────────

func (e *StatementCommStatus) GetInitialized() bool   { return e.initialized }
func (e *StatementCommStatus) GetID() string          { return e.id }
func (e *StatementCommStatus) GetName() string        { return e.name }
func (e *StatementCommStatus) GetSelected() bool      { return e.selected }
func (e *StatementCommStatus) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementCommStatus) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementCommStatus) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementCommStatus) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementCommStatus) GetResize() bool        { return false }
func (e *StatementCommStatus) GetResizeEnable() bool  { return false }
func (e *StatementCommStatus) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementCommStatus) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementCommStatus) SetDragEnable(en bool) {
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

func (e *StatementCommStatus) SetResizeEnable(_ bool) {}
func (e *StatementCommStatus) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementCommStatus) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementCommStatus) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementCommStatus) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementCommStatus) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementCommStatus) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementCommStatus) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementCommStatus) GetStatus() int                                         { return e.iconStatus }
func (e *StatementCommStatus) GetLabel() string                                       { return e.label }
func (e *StatementCommStatus) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

// ── Icon ─────────────────────────────────────────────────────────────

func (e *StatementCommStatus) GetIconName() string     { return "CommStatus" }
func (e *StatementCommStatus) GetIconCategory() string { return "Display" }

func (e *StatementCommStatus) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("📶").Fill(data.ColorIcon).
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

// ── Scene export ─────────────────────────────────────────────────────

func (e *StatementCommStatus) GetDeviceType() string { return "StatementCommStatus" }

func (e *StatementCommStatus) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementCommStatus) GetInnerBBox() *scene.Rect { return nil }
func (e *StatementCommStatus) GetKind() scenegraph.Kind  { return scenegraph.KindSimple }
func (e *StatementCommStatus) SetSceneNotify(fn func())  { e.sceneNotify = fn }

func (e *StatementCommStatus) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

func (e *StatementCommStatus) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label":         e.label,
		"yellowTimeout": e.yellowTimeout,
		"redTimeout":    e.redTimeout,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in the device's hover tooltip.
// Português: Retorna o comentário exibido no tooltip de hover do device.
func (e *StatementCommStatus) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementCommStatus) SetComment(c string) { e.comment = c }

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementCommStatus) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementCommStatus) OpenInspect() { go e.showInspectOverlay() }
