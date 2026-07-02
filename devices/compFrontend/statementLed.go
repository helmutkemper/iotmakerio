// /ide/devices/compFrontend/statementLED.go
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
//  Default colors — can be customized per instance via Inspect panel.
// =====================================================================

const (
	// kLEDDefaultOnColor is the default fill when the LED state is true.
	// Bright green — universally recognized as "ON" in hardware dashboards.
	kLEDDefaultOnColor = "#44DD88"

	// kLEDDefaultOffColor is the default fill when the LED state is false.
	// Dark grey — clearly distinguishable from any "on" color.
	kLEDDefaultOffColor = "#333344"
)

// StatementLED — dual device: backend data node + frontend LED indicator.
//
// Backend: a compact box with a single bool input connector ("state").
// Frontend: a colored circle that changes between onColor and offColor.
//
// When the user clicks the frontend LED (and interactionLocked is false),
// the state toggles and the new value is sent to hardware via SendFunc.
// This allows the LED to work both as an indicator and as a toggle button.
//
// Português: Dispositivo dual — nó de dados no backend + indicador LED
// visual no frontend. Clique no frontend alterna o estado e envia para
// o hardware (bloqueável via Lock Interaction).
type StatementLED struct {
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

	// Frontend dimensions (square — the circle fits inside)
	frontendWidth  rulesDensity.Density
	frontendHeight rulesDensity.Density

	pendingDragEnable *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller for the
	// backend stage. Devices without a frontend menu only
	// need this one.
	backendCtxMenu *contextMenu.Controller
	wireMgr        *wire.Manager // always the BACKEND wire manager

	label    string
	canvasEl js.Value

	// state is the current boolean value of the LED.
	// true = ON (shows onColor), false = OFF (shows offColor).
	state bool

	// onColor and offColor are CSS hex colors for the LED circle.
	// Customizable via the Inspect panel.
	onColor  string
	offColor string

	// interactionLocked prevents the end-user from toggling the LED
	// by clicking on the frontend element. Standard compFrontend pattern.
	// See readme.md for the full specification.
	interactionLocked bool

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
	sceneNotify func()
	onRemove    func(id string)

	// SendFunc is the callback for sending values to external hardware via
	// the live WebSocket connection. Set by the factory after initialization.
	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementLED) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementLED) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementLED) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementLED) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementLED) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementLED) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementLED) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementLED) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementLED) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

// Remove removes from BOTH workspaces.
func (e *StatementLED) Remove() {
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

func (e *StatementLED) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementLED) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementLED) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}

func (e *StatementLED) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementLED) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}

func (e *StatementLED) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementLED) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementLED) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementLED) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG: compact box with 1 input connector + state label
//
//  Visual design:
//
//	┌───────────────────┐  ← 2px border, orange (#FF8833 = bool color)
//	│ ◉ LED       OFF   │  ← ◉ = input connector; state text right-aligned
//	└───────────────────┘
//	led1                   ← editable label
//
// =====================================================================

const backendLEDLabelHeight = 18

func (e *StatementLED) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendLEDLabelHeight
}

func (e *StatementLED) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendLEDLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	connY := boxH / 2.0

	// The border color matches the bool type — orange.
	borderColor := rulesDevice.KColorTypeBool

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))

	// Outer rectangle
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, boxH-bw,
		rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, borderColor, bw)

	// Input connector circle (left side, vertically centered)
	svg += fmt.Sprintf(`<circle cx="%.0f" cy="%.1f" r="%.0f" fill="%s" stroke="%s" stroke-width="1"/>`,
		rulesDevice.KConnectorOffsetLeft, connY,
		rulesDevice.KConnectorRadius, borderColor, rulesDevice.KColorConnectorStroke)

	// Type tag "LED" (left, after connector)
	svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">LED</text>`,
		connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted)

	// State text (right-aligned)
	stateText := "OFF"
	stateColor := e.offColor
	if e.state {
		stateText = "ON"
		stateColor = e.onColor
	}
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central" font-weight="bold">%s</text>`,
		w-12, connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue, stateColor, stateText)

	// Lock icon
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="12" font-family="%s" font-size="10" fill="#FF8833" text-anchor="end">🔒</text>`, w-4, rulesDevice.KDeviceFontFamily)
	}

	// Editable label below the box
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, boxH+3, displayLabel)

	svg += `</svg>`
	return svg
}

// =====================================================================
//  Frontend SVG: glowing circle indicator
//
//  Renders at 3x for crisp edges (same technique as the gauge).
//  The circle has a subtle glow effect when ON using an SVG filter.
// =====================================================================

func (e *StatementLED) renderFrontendSVG() string {
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	cx := w / 2
	cy := h / 2
	r := w * 0.35

	fillColor := e.offColor
	glowOpacity := "0"
	if e.state {
		fillColor = e.onColor
		glowOpacity = "1"
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="12" ry="12" fill="#1a1a2e"/>`, int(w), int(h))

	// Glow filter definition — only visible when state is ON.
	// The filter blurs a copy of the circle and places it behind, creating
	// a soft halo effect that makes the LED look like it emits light.
	//
	// Português: Filtro de brilho — visível apenas quando state=ON.
	// Blura uma cópia do círculo e coloca atrás, criando um halo suave.
	svg += `<defs><filter id="led-glow" x="-50%" y="-50%" width="200%" height="200%">`
	svg += `<feGaussianBlur in="SourceGraphic" stdDeviation="12" result="blur"/>`
	svg += `<feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>`
	svg += `</filter></defs>`

	// Glow layer (behind the main circle)
	if e.state {
		svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" opacity="%s" filter="url(#led-glow)"/>`,
			cx, cy, r*0.9, fillColor, glowOpacity)
	}

	// Main LED circle — outer ring is always visible for shape
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="#555555" stroke-width="2"/>`,
		cx, cy, r, fillColor)

	// Inner highlight (specular reflection) — small white ellipse at top-left
	// gives the LED a 3D glass-like appearance.
	svg += fmt.Sprintf(`<ellipse cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f" fill="rgba(255,255,255,0.15)"/>`,
		cx-r*0.25, cy-r*0.3, r*0.35, r*0.2)

	// Label below the circle
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="22" fill="#88AACC" text-anchor="middle">LED</text>`,
		cx, h-16, rulesDevice.KDeviceFontFamily)

	// Lock indicator
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="28" font-family="%s" font-size="22" fill="#FF8833" text-anchor="end">🔒</text>`, w-12, rulesDevice.KDeviceFontFamily)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementLED) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementLED) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// =====================================================================
//  Init — creates elements on BOTH stages
// =====================================================================

func (e *StatementLED) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("led")
	e.backendWidth = 110
	e.backendHeight = 36
	e.frontendWidth = 80
	e.frontendHeight = 80
	e.state = false
	e.onColor = kLEDDefaultOnColor
	e.offColor = kLEDDefaultOffColor

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
		e.backendElem.SetMinSizeD(80, 36+backendLEDLabelHeight)
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
		e.frontendElem.SetMinSizeD(60, 60)
		e.wireFrontendEvents()
	}

	if e.backendCtxMenu == nil {
		log.Printf("[LED] Warning: no shared hex menu set, menus disabled")
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

func (e *StatementLED) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}

		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendLEDLabelHeight)
		connY := boxH / 2.0
		elemX, elemY := e.backendElem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY

		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}

		// Ignore clicks in the label area
		if event.LocalY > boxH {
			return
		}

		// Hit-test the input connector
		dx := event.LocalX - rulesDevice.KConnectorOffsetLeft
		dy := event.LocalY - connY
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			go e.backendCtxMenu.OpenAtWorld(mainMenu.ConnectorMenu(e.wireMgr, e.id, "current"), menuX, menuY)
			return
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
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.backendElem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendLEDLabelHeight)
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
//  Frontend events
// =====================================================================

func (e *StatementLED) wireFrontendEvents() {
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

	// Click toggles the LED state and sends the new value to hardware.
	// Blocked when interactionLocked is true.
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.interactionLocked {
			log.Printf("[LED:%s] interaction locked — ignoring click", e.id)
			return
		}

		e.state = !e.state
		log.Printf("[LED:%s] toggled to %v", e.id, e.state)

		e.SendValue("state", e.state)

		go func() {
			e.recacheFrontend()
			e.recacheBackend()
		}()
	})
}

// =====================================================================
//  Hex menu
// =====================================================================

// getBodyMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementLED) getBodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[LED] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[LED] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay — properties panel
// =====================================================================

func (e *StatementLED) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementLED) GetInspectConfig() interface{} {
	lockValue := "false"
	if e.interactionLocked {
		lockValue = "true"
	}
	stateValue := "false"
	if e.state {
		stateValue = "true"
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
						Key:   "current",
						Label: translate.T("propState", "State"),
						Type:  overlay.FieldCheckbox,
						Value: stateValue,
					},
					{
						Key:   "onColor",
						Label: translate.T("propOnColor", "On Color"),
						Type:  overlay.FieldColor,
						Value: e.onColor,
					},
					{
						Key:   "offColor",
						Label: translate.T("propOffColor", "Off Color"),
						Type:  overlay.FieldColor,
						Value: e.offColor,
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
				ContentURL: "/help/devices/display/statementLED.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementLED) ApplyProperties(values map[string]string) {
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
		log.Printf("[LED] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}

	if v, ok := values["current"]; ok {
		newState := v == "true"
		if newState != e.state {
			e.state = newState
			changed = true
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

	// ── Interaction lock (standard compFrontend field) ──
	if v, ok := values["interactionLocked"]; ok {
		newLocked := v == "true"
		if newLocked != e.interactionLocked {
			e.interactionLocked = newLocked
			changed = true
			log.Printf("[LED] %s: interactionLocked set to %v", e.id, newLocked)
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
//  Label editing (same pattern as gauge)
// =====================================================================

func (e *StatementLED) GetLabel() string { return e.label }

func (e *StatementLED) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementLED) showLabelEditor() {
	if e.backendElem == nil || e.canvasEl.IsUndefined() || e.canvasEl.IsNull() {
		return
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.createLabelInput()
	}()
}

func (e *StatementLED) createLabelInput() {
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
	style.Set("height", fmt.Sprintf("%dpx", backendLEDLabelHeight))
	style.Set("zIndex", "10000")
	style.Set("background", "#1a1a2e")
	style.Set("color", "#AABBCC")
	style.Set("border", "1px solid "+rulesDevice.KColorTypeBool)
	style.Set("borderRadius", "2px")
	style.Set("fontFamily", rulesDevice.KDeviceFontFamily)
	style.Set("fontSize", strconv.Itoa(rulesDevice.KDeviceFontSizeLabel)+"px")
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
//  Wire registration
// =====================================================================

func (e *StatementLED) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}

	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "current"},
		IsOutput:           false,
		AllowedTypes:       []string{"bool"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     1,
		Label:              "state",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.backendElem.GetPosition()
			_, h := e.backendElem.GetSize()
			boxH := h - float64(backendLEDLabelHeight)
			return ex + rulesDevice.KConnectorOffsetLeft, ey + boxH/2
		},
	})
}

// =====================================================================
//  Live communication (scene.LiveUpdatable)
// =====================================================================

// LiveUpdate receives a real-time boolean value from external hardware.
// Works regardless of interactionLocked — the lock only prevents sending.
//
// Accepted value formats:
//   - JSON bool:   true / false
//   - JSON number: 0 = false, anything else = true
//   - JSON string: "true"/"1" = true, anything else = false
func (e *StatementLED) LiveUpdate(port string, value []byte) error {
	if port != "current" {
		return fmt.Errorf("led %s: unknown port %q", e.id, port)
	}

	// Try bool first
	var boolVal bool
	if err := json.Unmarshal(value, &boolVal); err == nil {
		e.state = boolVal
	} else {
		// Try number (0 = false, non-zero = true)
		var num float64
		if err2 := json.Unmarshal(value, &num); err2 == nil {
			e.state = num != 0
		} else {
			// Try string
			var s string
			if err3 := json.Unmarshal(value, &s); err3 == nil {
				e.state = s == "true" || s == "1"
			} else {
				return fmt.Errorf("led %s: cannot parse value for port %q: %w", e.id, port, err)
			}
		}
	}

	log.Printf("[LED:%s] LiveUpdate state=%v", e.id, e.state)

	e.recacheBackend()
	e.recacheFrontend()

	return nil
}

// SendValue sends the current LED state to external hardware.
// Respects interactionLocked.
func (e *StatementLED) SendValue(port string, value bool) {
	if e.SendFunc == nil {
		return
	}
	if e.interactionLocked {
		log.Printf("[LED:%s] SendValue blocked — interaction locked", e.id)
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementLED) GetInitialized() bool   { return e.initialized }
func (e *StatementLED) GetID() string          { return e.id }
func (e *StatementLED) GetName() string        { return e.name }
func (e *StatementLED) GetSelected() bool      { return e.selected }
func (e *StatementLED) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementLED) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementLED) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementLED) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementLED) GetResize() bool        { return false }
func (e *StatementLED) GetResizeEnable() bool  { return false }
func (e *StatementLED) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementLED) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementLED) SetDragEnable(en bool) {
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

func (e *StatementLED) SetResizeEnable(_ bool) {}
func (e *StatementLED) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementLED) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementLED) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementLED) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementLED) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementLED) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementLED) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementLED) GetStatus() int                                         { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementLED) GetIconName() string     { return "LED" }
func (e *StatementLED) GetIconCategory() string { return "Display" }

func (e *StatementLED) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	// Filled circle icon symbol — represents the LED
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("●").Fill(data.ColorIcon).
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
//  Scene export (backend element)
// =====================================================================

func (e *StatementLED) GetDeviceType() string { return "StatementLED" }
func (e *StatementLED) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementLED) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementLED) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementLED) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementLED) MoveBy(dx, dy float64) {
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
func (e *StatementLED) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"label":             e.label,
		"current":           e.state,
		"onColor":           e.onColor,
		"offColor":          e.offColor,
		"interactionLocked": e.interactionLocked,
	}
}
