// /ide/devices/compFrontend/statementButton.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

import (
	"encoding/json"
	"fmt"
	"log"
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
	kButtonDefaultActiveColor = "#4488CC" // pressed state — accent blue
	kButtonDefaultIdleColor   = "#252a3e" // released state — dark background
	kButtonDefaultTextColor   = "#DDEEFF" // button label text
)

// StatementButton — dual device: backend output node + frontend push button.
//
// This is an OUTPUT device: the user presses the button on the frontend and
// the value is sent to hardware via WebSocket AND available as an output
// connector for other devices on the backend canvas.
//
// Backend: compact box with 1 OUTPUT connector (current, bool).
//
//	The connector sits on the RIGHT side (same convention as Bool constant).
//	Other devices can wire to this output to read the button state.
//
// Frontend: a styled push button. Each click toggles the state (true/false).
//
//	When state is true the button appears "pressed" (active color, inset shadow).
//	When state is false the button appears "raised" (idle color, outset shadow).
//
// Português: Dispositivo de SAÍDA — o usuário pressiona o botão no frontend
// e o valor é enviado ao hardware via WebSocket E disponibilizado como
// connector de saída para outros devices no backend.
type StatementButton struct {
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

	// state is the current button state. true = pressed, false = released.
	state bool

	// buttonText is the label displayed on the frontend button face.
	// Editable via Inspect panel. Defaults to "PUSH".
	buttonText string

	// Visual customization
	activeColor string // fill when pressed (state=true)
	idleColor   string // fill when released (state=false)

	interactionLocked bool

	// momentary when true makes the button auto-release after a short press.
	// Click sets state=true, sends to hardware, waits 200ms, then sets
	// state=false and sends again. Like a doorbell or a physical push button
	// that springs back. When false, each click toggles (default behavior).
	//
	// Português: Quando true, o botão solta automaticamente após um clique
	// curto. Click→true→200ms→false. Como uma campainha.
	momentary bool

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
	sceneNotify func()
	onRemove    func(id string)

	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementButton) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementButton) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementButton) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementButton) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementButton) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage. This device has no frontend context menu — taps on
// its frontend element trigger value interaction directly.
func (e *StatementButton) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}
func (e *StatementButton) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementButton) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementButton) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementButton) Remove() {
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

func (e *StatementButton) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementButton) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementButton) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementButton) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementButton) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementButton) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementButton) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementButton) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementButton) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG — compact box with 1 OUTPUT connector (right side)
//
//	┌───────────────────┐  ← orange border (#FF8833 = bool type)
//	│ BTN  OFF        ◉ │  ← ◉ = output connector (right side)
//	└───────────────────┘
//	btn1
// =====================================================================

const backendBtnLabelHeight = 18

func (e *StatementButton) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendBtnLabelHeight
}

func (e *StatementButton) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendBtnLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	connY := boxH / 2.0
	borderColor := rulesDevice.KColorTypeBool

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))

	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, boxH-bw,
		rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, borderColor, bw)

	// Output connector circle — RIGHT side
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.0f" fill="%s" stroke="%s" stroke-width="1"/>`,
		w-rulesDevice.KConnectorOffsetRight, connY,
		rulesDevice.KConnectorRadius, borderColor, rulesDevice.KColorConnectorStroke)

	// Type tag
	svg += fmt.Sprintf(`<text x="8" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">BTN</text>`,
		connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted)

	// State text
	stateText := "OFF"
	stateColor := rulesDevice.KColorDeviceTextMuted
	if e.state {
		stateText = "ON"
		stateColor = rulesDevice.KColorTypeBool
	}
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">%s</text>`,
		w/2, connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue, stateColor, stateText)

	// Lock icon
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
//  Frontend SVG — push button with 3D press/release visual
//
//  Rendered at 3x for crisp edges.
//  Released: raised look (light top edge, dark bottom edge, outset feel)
//  Pressed:  inset look (dark top edge, lighter bottom, shifted label)
// =====================================================================

func (e *StatementButton) renderFrontendSVG() string {
	const scale = 3.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	pad := 8.0
	cornerR := 18.0

	// Determine colors based on state
	fillColor := e.idleColor
	if fillColor == "" {
		fillColor = kButtonDefaultIdleColor
	}
	if e.state {
		fillColor = e.activeColor
		if fillColor == "" {
			fillColor = kButtonDefaultActiveColor
		}
	}

	textColor := kButtonDefaultTextColor
	btnText := e.buttonText
	if btnText == "" {
		btnText = "PUSH"
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

	// Background
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="12" ry="12" fill="#1a1a2e"/>`, int(w), int(h))

	// Button face
	btnX := pad
	btnY := pad
	btnW := w - 2*pad
	btnH := h - 2*pad

	if e.state {
		// Pressed: inset effect — darker fill, dark top highlight, light bottom
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s"/>`,
			btnX, btnY+2, btnW, btnH-2, cornerR, cornerR, fillColor)
		// Top shadow (inset)
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="4" rx="%.0f" ry="%.0f" fill="rgba(0,0,0,0.3)"/>`,
			btnX, btnY+2, btnW, cornerR, cornerR)
		// Label (shifted down 2px for press effect)
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="36" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
			w/2, h/2+2, rulesDevice.KDeviceFontFamily, textColor, btnText)
	} else {
		// Released: outset effect — light top highlight, dark bottom shadow
		// Bottom shadow
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="rgba(0,0,0,0.4)"/>`,
			btnX, btnY+4, btnW, btnH-2, cornerR, cornerR)
		// Main face
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s"/>`,
			btnX, btnY, btnW, btnH-4, cornerR, cornerR, fillColor)
		// Top highlight
		svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="4" rx="%.0f" ry="%.0f" fill="rgba(255,255,255,0.08)"/>`,
			btnX+2, btnY, btnW-4, cornerR, cornerR)
		// Label
		svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="%s" font-size="36" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
			w/2, h/2-1, rulesDevice.KDeviceFontFamily, textColor, btnText)
	}

	// Border ring
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="none" stroke="#333" stroke-width="2"/>`,
		btnX, btnY, btnW, btnH, cornerR, cornerR)

	// Lock indicator
	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="24" font-family="%s" font-size="22" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-16, rulesDevice.KDeviceFontFamily)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementButton) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementButton) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementButton) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("btn")
	e.backendWidth = 110
	e.backendHeight = 36
	e.frontendWidth = 100
	e.frontendHeight = 60
	e.state = false
	e.buttonText = "PUSH"
	e.activeColor = kButtonDefaultActiveColor
	e.idleColor = kButtonDefaultIdleColor
	e.resizeLocked = true

	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id

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
		e.backendElem.SetMinSizeD(80, 36+backendBtnLabelHeight)
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
		e.frontendElem.SetMinSizeD(60, 40)
		e.wireFrontendEvents()
	}

	if e.backendCtxMenu == nil {
		log.Printf("[Button] Warning: no shared hex menu set, menus disabled")
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

// =====================================================================
//  Backend events — hex menu with connector hit-test on RIGHT side
// =====================================================================

func (e *StatementButton) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}

		_, h := e.backendElem.GetSize()
		w, _ := e.backendElem.GetSize()
		boxH := h - float64(backendBtnLabelHeight)
		connY := boxH / 2.0
		connX := w - rulesDevice.KConnectorOffsetRight
		elemX, elemY := e.backendElem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY

		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}

		if event.LocalY > boxH {
			return
		}

		// Hit-test output connector (right side)
		dx := event.LocalX - connX
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
		w, h := e.backendElem.GetSize()
		boxH := h - float64(backendBtnLabelHeight)
		connY := boxH / 2.0
		connX := w - rulesDevice.KConnectorOffsetRight

		if ly > boxH {
			return ""
		}
		dx := lx - connX
		dy := ly - connY
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			return sprite.CursorPointer
		}
		return ""
	})
}

// =====================================================================
//  Frontend events — click toggles state and sends to hardware
// =====================================================================

func (e *StatementButton) wireFrontendEvents() {
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

	// Click behavior depends on the momentary flag:
	//   momentary=false (default): each click toggles true↔false
	//   momentary=true:  click → true → 200ms → false (auto-release)
	e.frontendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.interactionLocked {
			log.Printf("[Button:%s] interaction locked — ignoring click", e.id)
			return
		}

		if e.momentary {
			// Momentary mode: press and auto-release
			e.state = true
			log.Printf("[Button:%s] momentary press", e.id)
			e.SendValue("current", true)

			go func() {
				e.recacheFrontend()
				e.recacheBackend()

				time.Sleep(200 * time.Millisecond)

				e.state = false
				e.SendValue("current", false)
				e.recacheFrontend()
				e.recacheBackend()
				log.Printf("[Button:%s] momentary release", e.id)
			}()
		} else {
			// Toggle mode: each click alternates
			e.state = !e.state
			log.Printf("[Button:%s] toggled to %v", e.id, e.state)
			e.SendValue("current", e.state)

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
func (e *StatementButton) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Button] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[Button] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay
// =====================================================================

func (e *StatementButton) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementButton) GetInspectConfig() interface{} {
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
					{Key: "buttonText", Label: translate.T("propButtonText", "Button Text"), Type: overlay.FieldText, Value: e.buttonText, Placeholder: "PUSH"},
					{Key: "activeColor", Label: translate.T("propActiveColor", "Active Color"), Type: overlay.FieldColor, Value: e.activeColor},
					{Key: "idleColor", Label: translate.T("propIdleColor", "Idle Color"), Type: overlay.FieldColor, Value: e.idleColor},
					{Key: "momentary", Label: translate.T("propMomentary", "Momentary"), Type: overlay.FieldCheckbox, Value: func() string {
						if e.momentary {
							return "true"
						}
						return "false"
					}()},
					{Key: "interactionLocked", Label: translate.T("propLockInteraction", "Lock Interaction"), Type: overlay.FieldCheckbox, Value: lockValue},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementButton.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementButton) ApplyProperties(values map[string]string) {
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
		log.Printf("[Button] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}
	if v, ok := values["buttonText"]; ok && v != e.buttonText {
		e.buttonText = v
		changed = true
	}
	if v, ok := values["activeColor"]; ok && v != "" && v != e.activeColor {
		e.activeColor = v
		changed = true
	}
	if v, ok := values["idleColor"]; ok && v != "" && v != e.idleColor {
		e.idleColor = v
		changed = true
	}
	if v, ok := values["momentary"]; ok {
		newMom := v == "true"
		if newMom != e.momentary {
			e.momentary = newMom
			changed = true
			log.Printf("[Button] %s: momentary set to %v", e.id, newMom)
		}
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
//  Wire registration — 1 OUTPUT port (bool, right side)
// =====================================================================

func (e *StatementButton) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}

	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "current"},
		IsOutput:           true,
		AllowedTypes:       []string{"bool"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     0, // unlimited outputs
		Label:              "current",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.backendElem.GetPosition()
			w, h := e.backendElem.GetSize()
			boxH := h - float64(backendBtnLabelHeight)
			return ex + w - rulesDevice.KConnectorOffsetRight, ey + boxH/2
		},
	})
}

// =====================================================================
//  Live communication
// =====================================================================

// LiveUpdate receives a value from external hardware (e.g. a physical
// button on the device). Works regardless of interactionLocked.
func (e *StatementButton) LiveUpdate(port string, value []byte) error {
	if port != "current" {
		return fmt.Errorf("button %s: unknown port %q", e.id, port)
	}

	var boolVal bool
	if err := json.Unmarshal(value, &boolVal); err == nil {
		e.state = boolVal
	} else {
		var num float64
		if err2 := json.Unmarshal(value, &num); err2 == nil {
			e.state = num != 0
		} else {
			var s string
			if err3 := json.Unmarshal(value, &s); err3 == nil {
				e.state = s == "true" || s == "1"
			} else {
				return fmt.Errorf("button %s: cannot parse value: %w", e.id, err)
			}
		}
	}

	log.Printf("[Button:%s] LiveUpdate state=%v", e.id, e.state)
	e.recacheBackend()
	e.recacheFrontend()
	return nil
}

// SendValue sends the button state to external hardware.
// Respects interactionLocked.
func (e *StatementButton) SendValue(port string, value bool) {
	if e.SendFunc == nil {
		return
	}
	if e.interactionLocked {
		log.Printf("[Button:%s] SendValue blocked — interaction locked", e.id)
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  Serialization
// =====================================================================

func (e *StatementButton) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"label":             e.label,
		"current":           e.state,
		"buttonText":        e.buttonText,
		"activeColor":       e.activeColor,
		"idleColor":         e.idleColor,
		"momentary":         e.momentary,
		"interactionLocked": e.interactionLocked,
	}
}

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementButton) GetInitialized() bool   { return e.initialized }
func (e *StatementButton) GetID() string          { return e.id }
func (e *StatementButton) GetName() string        { return e.name }
func (e *StatementButton) GetSelected() bool      { return e.selected }
func (e *StatementButton) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementButton) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementButton) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementButton) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementButton) GetResize() bool        { return false }
func (e *StatementButton) GetResizeEnable() bool  { return false }
func (e *StatementButton) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementButton) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementButton) SetDragEnable(en bool) {
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

func (e *StatementButton) SetResizeEnable(_ bool) {}
func (e *StatementButton) SelectedInvert()        { e.SetSelected(!e.selected) }

func (e *StatementButton) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementButton) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementButton) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementButton) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementButton) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementButton) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementButton) GetStatus() int                                         { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementButton) GetIconName() string     { return "Button" }
func (e *StatementButton) GetIconCategory() string { return "Display" }

func (e *StatementButton) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	// Play/push icon
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("▶").Fill(data.ColorIcon).
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

func (e *StatementButton) GetDeviceType() string { return "StatementButton" }
func (e *StatementButton) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementButton) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementButton) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementButton) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementButton) GetLabel() string         { return e.label }
func (e *StatementButton) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementButton) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}
