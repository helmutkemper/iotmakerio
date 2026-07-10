// /ide/devices/compFrontend/statementTextDisplay.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

import (
	"encoding/json"
	"fmt"
	"log"
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

// =====================================================================
//  Frontend context menu — lightweight HTML popup (not hex menu)
//
//  The hex menu belongs to the backend canvas coordinate system and
//  renders at the wrong position when triggered from the frontend stage.
//  Instead, the frontend uses a simple HTML context menu with the same
//  visual style as the slider overlay (gs-* classes).
//
//  The menu has two items:
//    - Resize: toggles resize handles on the frontend element
//    - Inspect: opens the Inspect overlay (Properties + Help)
//
//  Português: O menu hex pertence ao canvas do backend. O frontend usa
//  um menu HTML leve com os mesmos estilos do slider overlay.
// =====================================================================

// StatementTextDisplay — dual device: backend data node + frontend text viewer.
//
// Backend: compact box with 1 input connector (current, string type).
//
//	Click opens hex menu with Delete + Inspect.
//
// Frontend: resizable monospace text preview on the canvas.
//
//	Click opens a lightweight HTML context menu with Resize + Inspect.
//	Resize mode toggles corner handles (same pattern as Loop).
//
// Inspect panel has 2 tabs:
//   - Properties: ID, Label, Text (textarea), Lock Interaction
//   - Help: markdown documentation
type StatementTextDisplay struct {
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

	text string

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

	SendFunc func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementTextDisplay) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementTextDisplay) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementTextDisplay) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementTextDisplay) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementTextDisplay) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage — body clicks and port clicks route through this.
func (e *StatementTextDisplay) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}

// SetFrontendContextMenu injects the controller for the frontend
// stage — frontend element taps (Resize, Z-order) route through
// this. May be nil in backend-only compile targets.
func (e *StatementTextDisplay) SetFrontendContextMenu(c *contextMenu.Controller) {
	e.frontendCtxMenu = c
}
func (e *StatementTextDisplay) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementTextDisplay) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementTextDisplay) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementTextDisplay) Remove() {
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

func (e *StatementTextDisplay) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementTextDisplay) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementTextDisplay) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementTextDisplay) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementTextDisplay) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementTextDisplay) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementTextDisplay) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementTextDisplay) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementTextDisplay) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// =====================================================================
//  Backend SVG
// =====================================================================

const backendTextLabelHeight = 18

func (e *StatementTextDisplay) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + backendTextLabelHeight
}

func (e *StatementTextDisplay) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(backendTextLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	connY := boxH / 2.0
	borderColor := rulesDevice.KColorTypeString

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
	svg += fmt.Sprintf(`<text x="18" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">TXT</text>`,
		connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted)

	preview := e.text
	if len(preview) > 15 {
		preview = preview[:15] + "…"
	}
	if preview == "" {
		preview = "(empty)"
	}
	preview = escapeXML(preview)
	svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`,%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="central">%s</text>`,
		w-12, connY, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizePort, rulesDevice.KColorDeviceText, preview)

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

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// =====================================================================
//  Frontend SVG — resizable monospace text preview
// =====================================================================

func (e *StatementTextDisplay) renderFrontendSVG() string {
	const scale = 2.0
	w := e.frontendWidth.GetFloat() * scale
	h := e.frontendHeight.GetFloat() * scale

	lineH := 28.0
	padTop := 40.0
	padLeft := 50.0
	padRight := 16.0
	fontSize := 22.0

	availH := h - padTop - 16
	maxLines := int(availH / lineH)
	if maxLines < 1 {
		maxLines = 1
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))
	svg += fmt.Sprintf(`<rect width="%d" height="%d" rx="8" ry="8" fill="#0d1117"/>`, int(w), int(h))
	svg += fmt.Sprintf(`<rect width="%d" height="%.0f" rx="8" ry="8" fill="#161b22"/>`, int(w), padTop-4)
	svg += fmt.Sprintf(`<rect y="20" width="%d" height="%.0f" fill="#161b22"/>`, int(w), padTop-24)

	svg += fmt.Sprintf(`<text x="12" y="26" font-family="%s" font-size="18" fill="#555" font-weight="bold">TEXT</text>`,
		rulesDevice.KDeviceFontFamily)
	svg += fmt.Sprintf(`<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#222" stroke-width="1"/>`,
		padLeft-8, padTop, padLeft-8, h-8)

	lines := strings.Split(e.text, "\n")
	for i := 0; i < maxLines && i < len(lines); i++ {
		y := padTop + float64(i)*lineH + lineH*0.75
		svg += fmt.Sprintf(`<text x="%.0f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="%.0f" fill="#444" text-anchor="end">%d</text>`,
			padLeft-14, y, fontSize*0.8, i+1)

		line := lines[i]
		maxChars := int((w - padLeft - padRight) / (fontSize * 0.6))
		if maxChars < 1 {
			maxChars = 1
		}
		if len(line) > maxChars {
			line = line[:maxChars] + "…"
		}
		svg += fmt.Sprintf(`<text x="%.0f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="%.0f" fill="#c9d1d9">%s</text>`,
			padLeft, y, fontSize, escapeXML(line))
	}

	if len(lines) > maxLines {
		y := padTop + float64(maxLines)*lineH + lineH*0.5
		svg += fmt.Sprintf(`<text x="%.0f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamilyMono+`" font-size="%.0f" fill="#555">⋮ +%d lines</text>`,
			padLeft, y, fontSize*0.8, len(lines)-maxLines)
	}

	if e.interactionLocked {
		svg += fmt.Sprintf(`<text x="%.1f" y="26" font-family="%s" font-size="18" fill="#FF8833" text-anchor="end">🔒</text>`,
			w-16, rulesDevice.KDeviceFontFamily)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementTextDisplay) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}
func (e *StatementTextDisplay) recacheFrontend() {
	if e.frontendElem != nil {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendSVG())
	}
}

// RefreshVisual re-renders the frontend SVG after a resize or import.
func (e *StatementTextDisplay) RefreshVisual() {
	e.recacheFrontend()
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementTextDisplay) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("text")
	e.backendWidth = 140
	e.backendHeight = 36
	e.frontendWidth = 200
	e.frontendHeight = 160
	e.text = ""
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
		e.backendElem.SetMinSizeD(100, 36+backendTextLabelHeight)
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
		e.frontendElem.SetMinSizeD(120, 80)

		// Resize handle buttons (same pattern as Loop)
		if e.resizerButton != nil {
			adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
			if err2 := e.frontendElem.SetResizeButtons(adapter); err2 != nil {
				log.Printf("[TextDisplay] ERROR: SetResizeButtons failed: %v", err2)
			} else {
				log.Printf("[TextDisplay] SetResizeButtons: 8 handles cached OK")
			}
			e.frontendElem.ShowResizeButtons(false)
			e.frontendElem.SetResizeEnable(false)
		} else {
			log.Printf("[TextDisplay] WARNING: resizerButton is nil, no visual resize handles")
		}

		e.wireFrontendEvents()
	}

	if e.backendCtxMenu == nil {
		log.Printf("[TextDisplay] Warning: no shared hex menu set, menus disabled")
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

// =====================================================================
//  Backend events — hex menu with Delete + Inspect
// =====================================================================

func (e *StatementTextDisplay) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}

		_, h := e.backendElem.GetSize()
		boxH := h - float64(backendTextLabelHeight)
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
		boxH := h - float64(backendTextLabelHeight)
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
//  Frontend events — HTML context menu + resize handles
//
//  Click opens a lightweight HTML context menu with two items:
//    - Resize: toggles resize handles (same as Loop pattern)
//    - Inspect: opens the Inspect overlay (Properties + Help)
//
//  The context menu is a simple HTML overlay, not the hex menu (which
//  belongs to the backend canvas and would render at wrong coordinates).
// =====================================================================

func (e *StatementTextDisplay) wireFrontendEvents() {
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

	// ── Resize handlers ───────────────────────────────────────────────
	e.frontendElem.SetOnResizeStart(func(event sprite.ResizeEvent) {
		log.Printf("[TextDisplay:%s] resizeStart: size=(%.0f,%.0f)", e.id, event.OldWidth, event.OldHeight)
	})

	e.frontendElem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		// No child-bounds clamping needed (TextDisplay has no children).
	})

	e.frontendElem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		log.Printf("[TextDisplay:%s] resizeEnd: new=(%.0f,%.0f)", e.id, event.NewWidth, event.NewHeight)

		wD, hD := e.frontendElem.GetSizeD()
		nw, nh := e.gridAdjust.AdjustCenterD(wD, hD)
		e.frontendElem.SetSizeD(nw, nh)

		e.frontendWidth = nw
		e.frontendHeight = nh

		// Exit resize mode and re-enable drag automatically.
		e.SetResizeEnable(false)
		e.SetDragEnable(true)

		go func() {
			e.recacheFrontend()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	})

	// ── Click opens context menu ──────────────────────────────────────
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
func (e *StatementTextDisplay) frontendContextItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.ResizeItem(func() {
			e.SetResizeEnable(!e.GetResizeEnable())
			log.Printf("[TextDisplay:%s] resize toggled to %v", e.id, e.GetResizeEnable())
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
func (e *StatementTextDisplay) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[TextDisplay] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[TextDisplay] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Inspect overlay — 2 tabs: Properties, Help
// =====================================================================

func (e *StatementTextDisplay) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementTextDisplay) GetInspectConfig() interface{} {
	lockValue := "false"
	if e.interactionLocked {
		lockValue = "true"
	}

	return overlay.Config{
		Title: fmt.Sprintf("%s", e.id),
		Width: "600px",
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
						Key:   "current",
						Label: translate.T("propText", "Text"),
						Type:  overlay.FieldTextarea,
						Value: e.text,
						Rows:  10,
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
				ContentURL: "/help/devices/display/statementTextDisplay.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementTextDisplay) ApplyProperties(values map[string]string) {
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
		log.Printf("[TextDisplay] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}
	if v, ok := values["current"]; ok && v != e.text {
		e.text = v
		changed = true
	}

	// Frontend dimensions (restored from scene JSON)
	if v, ok := values["frontendWidth"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil && rulesDensity.Density(n) != e.frontendWidth {
			e.frontendWidth = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			changed = true
		}
	}
	if v, ok := values["frontendHeight"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil && rulesDensity.Density(n) != e.frontendHeight {
			e.frontendHeight = rulesDensity.Density(n)
			if e.frontendElem != nil {
				e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
			}
			changed = true
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
//  Wire registration
// =====================================================================

func (e *StatementTextDisplay) RegisterConnectors() {
	if e.wireMgr == nil || e.backendElem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "current"},
		IsOutput:           false,
		AllowedTypes:       []string{"string"},
		AcceptNotConnected: true,
		Locked:             false,
		MaxConnections:     1,
		Label:              "current",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.backendElem.GetPosition()
			_, h := e.backendElem.GetSize()
			boxH := h - float64(backendTextLabelHeight)
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), boxH/2)
			return ex + ax, ey + ay
		},
	})
}

// =====================================================================
//  Live communication
// =====================================================================

func (e *StatementTextDisplay) LiveUpdate(port string, value []byte) error {
	if port != "current" {
		return fmt.Errorf("textdisplay %s: unknown port %q", e.id, port)
	}
	var str string
	if err := json.Unmarshal(value, &str); err != nil {
		str = string(value)
	}
	e.text = str
	log.Printf("[TextDisplay:%s] LiveUpdate len=%d", e.id, len(str))
	e.recacheBackend()
	e.recacheFrontend()
	return nil
}

func (e *StatementTextDisplay) SendValue(port string, value string) {
	if e.SendFunc == nil || e.interactionLocked {
		return
	}
	e.SendFunc(e.id, port, value)
}

// =====================================================================
//  Serialization
// =====================================================================

func (e *StatementTextDisplay) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label":             e.label,
		"current":           e.text,
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
func (e *StatementTextDisplay) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementTextDisplay) SetComment(c string) { e.comment = c }

// =====================================================================
//  State accessors
// =====================================================================

func (e *StatementTextDisplay) GetInitialized() bool   { return e.initialized }
func (e *StatementTextDisplay) GetID() string          { return e.id }
func (e *StatementTextDisplay) GetName() string        { return e.name }
func (e *StatementTextDisplay) GetSelected() bool      { return e.selected }
func (e *StatementTextDisplay) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementTextDisplay) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementTextDisplay) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementTextDisplay) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementTextDisplay) GetResize() bool        { return false }
func (e *StatementTextDisplay) GetResizeEnable() bool {
	if e.frontendElem != nil {
		return e.frontendElem.IsResizeEnabled()
	}
	return false
}
func (e *StatementTextDisplay) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementTextDisplay) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementTextDisplay) SetDragEnable(en bool) {
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

// SetResizeEnable toggles the resize handles on the frontend element.
// When resize is enabled, drag is disabled (same pattern as Loop).
func (e *StatementTextDisplay) SetResizeEnable(enabled bool) {
	if e.resizeLocked || e.frontendElem == nil {
		return
	}
	if enabled {
		e.frontendElem.SetDragEnable(false)
		e.dragEnabled = false
		e.selected = false
		e.frontendElem.SetResizeEnable(true)
		e.frontendElem.ShowResizeButtons(true)
		log.Printf("[TextDisplay:%s] resize enabled", e.id)
	} else {
		e.frontendElem.SetResizeEnable(false)
		e.frontendElem.ShowResizeButtons(false)
		log.Printf("[TextDisplay:%s] resize disabled", e.id)
	}
}

func (e *StatementTextDisplay) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementTextDisplay) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementTextDisplay) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementTextDisplay) SetWidth(_ rulesDensity.Density)  {}
func (e *StatementTextDisplay) SetHeight(_ rulesDensity.Density) {}
func (e *StatementTextDisplay) SetSize(w, h rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendWidth = w
		e.frontendHeight = h
		e.frontendElem.SetSizeD(w, h)
	}
}
func (e *StatementTextDisplay) SetStatus(s int) { e.iconStatus = s }
func (e *StatementTextDisplay) GetStatus() int  { return e.iconStatus }

// =====================================================================
//  Icon
// =====================================================================

func (e *StatementTextDisplay) GetIconName() string     { return "TextDisplay" }
func (e *StatementTextDisplay) GetIconCategory() string { return "Display" }

func (e *StatementTextDisplay) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamilyMono + "," + rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 5).
		Text("Aa").Fill(data.ColorIcon).
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

func (e *StatementTextDisplay) GetDeviceType() string { return "StatementTextDisplay" }
func (e *StatementTextDisplay) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementTextDisplay) GetInnerBBox() *scene.Rect {
	if e.backendElem == nil {
		return nil
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementTextDisplay) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementTextDisplay) SetSceneNotify(fn func()) { e.sceneNotify = fn }
func (e *StatementTextDisplay) GetLabel() string         { return e.label }
func (e *StatementTextDisplay) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

func (e *StatementTextDisplay) MoveBy(dx, dy float64) {
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
func (e *StatementTextDisplay) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
