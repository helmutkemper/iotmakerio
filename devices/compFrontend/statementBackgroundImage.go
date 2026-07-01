// /ide/devices/compFrontend/statementBackgroundImage.go

package compFrontend

// statementBackgroundImage.go — Dual device: backend config node + frontend background image.
//
// Backend: compact box with Delete + Inspect hex menu. No connectors.
// Frontend: uploaded PNG/SVG rendered below all interactive elements.
//   - Click → HTML context menu (Bring Forward, Send Backward, Resize)
//   - Resize via toggle (same red ball markers as other devices)
//   - Opacity adjustable via Inspect panel
//   - Image auto-sizes to natural pixel dimensions on upload
//
// Português: Dispositivo dual para upload de imagem de fundo no canvas frontend.

import (
	"encoding/base64"
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

const (
	kBgImgBackendWidth   = 150
	kBgImgBackendHeight  = 36
	kBgImgFrontendWidth  = 200
	kBgImgFrontendHeight = 150
	kBgImgLabelHeight    = 18
)

type StatementBackgroundImage struct {
	backendStage   sprite.Stage
	frontendStage  sprite.Stage
	backendElem    sprite.Element
	frontendElem   sprite.Element
	name           string
	initialized    bool
	selected       bool
	selectLocked   bool
	dragEnabled    bool
	dragLocked     bool
	resizeLocked   bool
	backendWidth   rulesDensity.Density
	backendHeight  rulesDensity.Density
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
	label           string
	canvasEl        js.Value
	imageData       string

	// opacity controls the transparency of the frontend image.
	// 100 = fully opaque (default), 0 = fully transparent.
	// Stored as 0–100; divided by 100 when applied to the sprite element.
	opacity float64

	id          string
	gridAdjust  grid.Adjust
	iconStatus  int
	sceneNotify func()
	onRemove    func(id string)
	SendFunc    func(deviceID, port string, value interface{})
}

// ── Dependency injection ──────────────────────────────────────────────

func (e *StatementBackgroundImage) SetBackendStage(s sprite.Stage)        { e.backendStage = s }
func (e *StatementBackgroundImage) SetFrontendStage(s sprite.Stage)       { e.frontendStage = s }
func (e *StatementBackgroundImage) SetWireManager(_ *wire.Manager)        {}
func (e *StatementBackgroundImage) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementBackgroundImage) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetBackendContextMenu injects the controller for the backend
// stage — body clicks and port clicks route through this.
func (e *StatementBackgroundImage) SetBackendContextMenu(c *contextMenu.Controller) {
	e.backendCtxMenu = c
}

// SetFrontendContextMenu injects the controller for the frontend
// stage — frontend element taps (Resize, Z-order) route through
// this. May be nil in backend-only compile targets.
func (e *StatementBackgroundImage) SetFrontendContextMenu(c *contextMenu.Controller) {
	e.frontendCtxMenu = c
}
func (e *StatementBackgroundImage) SetCanvasEl(el js.Value)        { e.canvasEl = el }
func (e *StatementBackgroundImage) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────

func (e *StatementBackgroundImage) Append() {
	if e.backendElem != nil {
		e.backendElem.SetVisible(true)
	}
	if e.frontendElem != nil {
		e.frontendElem.SetVisible(true)
	}
}

func (e *StatementBackgroundImage) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	// Unregister from z-index registry before destroying elements.
	FrontendZRegistry.Unregister(e.id)

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

func (e *StatementBackgroundImage) SetName(n string) {
	e.name = rulesSequentialId.GetIdFromBase(n)
}
func (e *StatementBackgroundImage) Get() *html.TagSvg { return nil }

// ── Position ──────────────────────────────────────────────────────────

func (e *StatementBackgroundImage) SetPosition(x, y rulesDensity.Density) {
	if e.backendElem != nil {
		e.backendElem.SetPositionD(x, y)
	}
}
func (e *StatementBackgroundImage) SetFrontendPosition(x, y rulesDensity.Density) {
	if e.frontendElem != nil {
		e.frontendElem.SetPositionD(x, y)
	}
}

// GetFrontendPosition returns the frontend (dashboard) node's x,y. It is the
// read counterpart of SetFrontendPosition and lets the scene serializer persist
// the dashboard node's own position — distinct from the backend node, which the
// scenegraph already captures — so a dual device restores both nodes where the
// maker placed them. Returns (0,0) before the frontend element exists.
func (e *StatementBackgroundImage) GetFrontendPosition() (float64, float64) {
	if e.frontendElem != nil {
		fx, fy := e.frontendElem.GetPositionD()
		return float64(fx), float64(fy)
	}
	return 0, 0
}
func (e *StatementBackgroundImage) GetWidth() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetWidthD()
	}
	return e.backendWidth
}
func (e *StatementBackgroundImage) GetHeight() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetHeightD()
	}
	return e.backendHeight
}
func (e *StatementBackgroundImage) GetX() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetXD()
	}
	return 0
}
func (e *StatementBackgroundImage) GetY() rulesDensity.Density {
	if e.backendElem != nil {
		return e.backendElem.GetYD()
	}
	return 0
}

// ── Backend SVG ──────────────────────────────────────────────────────

func (e *StatementBackgroundImage) backendTotalHeight() rulesDensity.Density {
	return e.backendHeight + kBgImgLabelHeight
}

func (e *StatementBackgroundImage) renderBackendSVG() string {
	w := e.backendWidth.GetFloat()
	boxH := e.backendHeight.GetFloat()
	totalH := boxH + float64(kBgImgLabelHeight)
	bw := rulesDevice.KDeviceBorderWidth
	borderColor := "#6688AA"

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(totalH))
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.0f" ry="%.0f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, boxH-bw,
		rulesDevice.KDeviceCornerRadius, rulesDevice.KDeviceCornerRadius,
		rulesDevice.KColorDeviceBg, borderColor, bw)
	svg += fmt.Sprintf(`<text x="10" y="%.1f" font-family="%s" font-size="14" fill="%s" dominant-baseline="central">📷</text>`,
		boxH/2, rulesDevice.KDeviceFontFamily, "#6688AA")

	statusText := "No Image"
	if e.imageData != "" {
		statusText = fmt.Sprintf("Image ✓  α=%.0f%%", e.opacity)
	}
	svg += fmt.Sprintf(`<text x="30" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="central" font-weight="bold">%s</text>`,
		boxH/2, rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorDeviceTextMuted, statusText)

	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, boxH+3, displayLabel)
	svg += `</svg>`
	return svg
}

// ── Frontend rendering ───────────────────────────────────────────────

func (e *StatementBackgroundImage) renderFrontendPlaceholder() string {
	w := int(e.frontendWidth.GetFloat())
	h := int(e.frontendHeight.GetFloat())
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, w, h)
	svg += fmt.Sprintf(`<rect width="%d" height="%d" fill="#1a1a2e"/>`, w, h)
	svg += `<defs><pattern id="checker" width="20" height="20" patternUnits="userSpaceOnUse">`
	svg += `<rect width="10" height="10" fill="#22223a"/><rect x="10" y="10" width="10" height="10" fill="#22223a"/>`
	svg += `</pattern></defs>`
	svg += fmt.Sprintf(`<rect width="%d" height="%d" fill="url(#checker)" opacity="0.5"/>`, w, h)
	svg += fmt.Sprintf(`<text x="%d" y="%d" font-family="%s" font-size="14" fill="#556677" text-anchor="middle" dominant-baseline="central">Upload image via Inspect</text>`,
		w/2, h/2, rulesDevice.KDeviceFontFamily)
	svg += fmt.Sprintf(`<rect x="1" y="1" width="%d" height="%d" fill="none" stroke="#334455" stroke-width="1" stroke-dasharray="6,4" rx="4"/>`, w-2, h-2)
	svg += `</svg>`
	return svg
}

func (e *StatementBackgroundImage) applyFrontendImage() {
	if e.frontendElem == nil || e.imageData == "" {
		return
	}
	if strings.Contains(e.imageData, "image/svg+xml") {
		svgXml := decodeSvgDataURL(e.imageData)
		if svgXml != "" {
			if err := e.frontendElem.CacheFromSvg(svgXml); err != nil {
				log.Printf("[BackgroundImage:%s] CacheFromSvg error: %v", e.id, err)
			}
			return
		}
	}
	if err := e.frontendElem.CacheFromImageSrc(e.imageData); err != nil {
		log.Printf("[BackgroundImage:%s] CacheFromImageSrc error: %v", e.id, err)
	}
}

// applyOpacity sets the frontend element's opacity to the current value.
// The sprite API expects 0.0–1.0, so we divide the stored 0–100 value.
func (e *StatementBackgroundImage) applyOpacity() {
	if e.frontendElem != nil {
		e.frontendElem.SetOpacity(e.opacity / 100.0)
	}
}

// detectImageSize loads the data URL into a temporary <img> element and
// reads naturalWidth/naturalHeight. On success, resizes the frontend element
// to match the image's real pixel dimensions.
func (e *StatementBackgroundImage) detectImageSize(dataURL string) {
	doc := js.Global().Get("document")
	img := doc.Call("createElement", "img")

	var onLoad js.Func
	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onLoad.Release()
		nw := img.Get("naturalWidth").Int()
		nh := img.Get("naturalHeight").Int()
		if nw <= 0 || nh <= 0 {
			return nil
		}
		e.frontendWidth = rulesDensity.Density(nw)
		e.frontendHeight = rulesDensity.Density(nh)
		if e.frontendElem != nil {
			e.frontendElem.SetSizeD(e.frontendWidth, e.frontendHeight)
		}
		log.Printf("[BackgroundImage:%s] detected image size: %dx%d", e.id, nw, nh)
		return nil
	})

	var onError js.Func
	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onError.Release()
		log.Printf("[BackgroundImage:%s] failed to detect image size", e.id)
		return nil
	})

	img.Call("addEventListener", "load", onLoad)
	img.Call("addEventListener", "error", onError)
	img.Set("src", dataURL)
}

func decodeSvgDataURL(dataURL string) string {
	if idx := strings.Index(dataURL, ";base64,"); idx >= 0 {
		encoded := dataURL[idx+8:]
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[BackgroundImage] base64 decode error: %v", err)
			return ""
		}
		return string(decoded)
	}
	if idx := strings.Index(dataURL, ","); idx >= 0 {
		return dataURL[idx+1:]
	}
	return ""
}

func (e *StatementBackgroundImage) recacheBackend() {
	if e.backendElem != nil {
		_ = e.backendElem.CacheFromSvg(e.renderBackendSVG())
	}
}

func (e *StatementBackgroundImage) recacheFrontend() {
	if e.frontendElem == nil {
		return
	}
	if e.imageData == "" {
		_ = e.frontendElem.CacheFromSvg(e.renderFrontendPlaceholder())
	} else {
		e.applyFrontendImage()
	}
	e.applyOpacity()
}

// ── Init ─────────────────────────────────────────────────────────────

func (e *StatementBackgroundImage) Init() (err error) {
	if e.backendStage == nil && e.frontendStage == nil {
		return fmt.Errorf("at least one stage must be set")
	}

	e.SetName("bg")
	e.backendWidth = kBgImgBackendWidth
	e.backendHeight = kBgImgBackendHeight
	e.frontendWidth = kBgImgFrontendWidth
	e.frontendHeight = kBgImgFrontendHeight
	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id
	e.resizeLocked = false
	e.opacity = 100

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
		e.backendElem.SetMinSizeD(100, kBgImgBackendHeight+kBgImgLabelHeight)
		e.wireBackendEvents()
	}

	// --- Frontend element ---
	if e.frontendStage != nil {
		e.frontendElem, err = e.frontendStage.CreateElement(sprite.ElementConfig{
			ID: e.id + "_front", X: 100, Y: 100,
			Width: e.frontendWidth.GetFloat(), Height: e.frontendHeight.GetFloat(),
			Index:      rulesZIndex.BackgroundFrontend,
			DragEnable: false,
			SvgXml:     e.renderFrontendPlaceholder(),
		})
		if err != nil {
			return fmt.Errorf("frontend element: %w", err)
		}
		e.frontendElem.SetMinSizeD(50, 50)

		// Register resize button markers (red balls) — same pattern as Chart.
		// Without this call, ShowResizeButtons(true) has nothing to display.
		if e.resizerButton != nil {
			adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
			if err2 := e.frontendElem.SetResizeButtons(adapter); err2 != nil {
				log.Printf("[BackgroundImage] ERROR: SetResizeButtons failed: %v", err2)
			}
			e.frontendElem.ShowResizeButtons(false)
			e.frontendElem.SetResizeEnable(false)
		}

		e.wireFrontendEvents()

		// Register in the z-index registry so Bring Forward / Send Backward
		// work correctly across all background image instances.
		FrontendZRegistry.Register(e.id, e.frontendElem)
	}

	e.initialized = true
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}
	return nil
}

// ── Backend events — hex menu (Delete + Inspect) ─────────────────────

func (e *StatementBackgroundImage) wireBackendEvents() {
	e.backendElem.SetOnClick(func(event sprite.PointerEvent) {
		if e.backendCtxMenu == nil {
			return
		}
		_, h := e.backendElem.GetSize()
		boxH := h - float64(kBgImgLabelHeight)
		elemX, elemY := e.backendElem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY

		if e.backendCtxMenu.IsOpen() {
			e.backendCtxMenu.Close()
			return
		}
		if event.LocalY > boxH {
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
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})
}

// ── Frontend events — HTML context menu ──────────────────────────────

func (e *StatementBackgroundImage) wireFrontendEvents() {
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
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// ── Resize handlers (same pattern as TextDisplay/Loop) ───────────
	e.frontendElem.SetOnResizeStart(func(event sprite.ResizeEvent) {
		log.Printf("[BackgroundImage:%s] resizeStart: size=(%.0f,%.0f)", e.id, event.OldWidth, event.OldHeight)
	})

	e.frontendElem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		// No child-bounds clamping needed (BackgroundImage has no children).
	})

	e.frontendElem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		log.Printf("[BackgroundImage:%s] resizeEnd: new=(%.0f,%.0f)", e.id, event.NewWidth, event.NewHeight)

		// Grid-adjust the final size.
		wD, hD := e.frontendElem.GetSizeD()
		nw, nh := e.gridAdjust.AdjustCenterD(wD, hD)
		e.frontendElem.SetSizeD(nw, nh)

		e.frontendWidth = nw
		e.frontendHeight = nh

		// Exit resize mode and re-enable drag automatically.
		// This hides the red ball markers after the resize is complete.
		e.SetResizeEnable(false)
		e.SetDragEnable(true)

		go func() {
			if e.imageData == "" {
				e.recacheFrontend()
			}
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
		go e.frontendCtxMenu.OpenAtWorld(e.frontendContextItems(), clickWX, clickWY)
	})
}

// frontendContextItems returns the frontend menu list.
// BackgroundImage offers z-ordering (Forward/Backward) and a Resize
// toggle. No Inspect — Inspect is backend-only per decision D10.
//
// Português: Lista do menu frontend. Forward, Backward e Resize.
func (e *StatementBackgroundImage) frontendContextItems() []contextMenu.Item {
	return []contextMenu.Item{
		{
			ID:              "forward",
			Label:           translate.T("menuBringForward", "Bring Forward"),
			FontAwesomePath: rulesIcon.KFAPlus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Moves this image one step above the element currently above it. Z-indices are reassigned from the new order — no collisions or gaps.",
			OnClick:         func() { FrontendZRegistry.MoveForward(e.id) },
		},
		{
			ID:              "backward",
			Label:           translate.T("menuSendBackward", "Send Backward"),
			FontAwesomePath: rulesIcon.KFAMinus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Moves this image one step below the element currently below it.",
			OnClick:         func() { FrontendZRegistry.MoveBackward(e.id) },
		},
		{
			ID:              "resize",
			Label:           translate.T("menuDeviceResize", "Resize"),
			FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight,
			ViewBox:         "0 0 512 512",
			HelpFallback:    "Toggles corner handles so you can drag to resize the image. Tap again to hide the handles.",
			OnClick: func() {
				e.SetResizeEnable(!e.GetResizeEnable())
				log.Printf("[BackgroundImage:%s] resize toggled to %v", e.id, e.GetResizeEnable())
			},
		},
	}
}

// ── Backend hex menu items ───────────────────────────────────────────

// getBackendMenuItems returns body context menu items: Delete first
// (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem canônica D4.
func (e *StatementBackgroundImage) getBackendMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[BackgroundImage] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[BackgroundImage] inspect: id=%v", e.id)
			go e.showInspectOverlay()
		}),
	}
}

// ── Inspect overlay ──────────────────────────────────────────────────

func (e *StatementBackgroundImage) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementBackgroundImage) GetInspectConfig() interface{} {
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
					{Key: "imageData", Label: translate.T("propImage", "Image"), Type: overlay.FieldFile, Value: e.imageData, Accept: "image/png,image/svg+xml"},
					{
						Key:         "opacity",
						Label:       translate.T("propOpacity", "Opacity"),
						Type:        overlay.FieldNumber,
						Value:       strconv.Itoa(int(e.opacity)),
						Min:         "0",
						Max:         "100",
						Placeholder: "0 - 100",
					},
				},
			},
			{
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/display/statementBackgroundImage.md",
			},
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

func (e *StatementBackgroundImage) ApplyProperties(values map[string]string) {
	changed := false
	imageChanged := false

	if v, ok := values["id"]; ok && v != "" && v != e.id {
		oldID := e.id
		e.id = v
		if e.label == oldID {
			e.label = v
		}
		changed = true
		log.Printf("[BackgroundImage] ID changed: %s → %s", oldID, v)
	}

	if v, ok := values["label"]; ok && v != e.label {
		e.label = v
		changed = true
	}

	if v, ok := values["imageData"]; ok && v != e.imageData {
		e.imageData = v
		changed = true
		imageChanged = true
		log.Printf("[BackgroundImage:%s] image updated (%d bytes)", e.id, len(v))
	}

	if v, ok := values["opacity"]; ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			if f < 0 {
				f = 0
			}
			if f > 100 {
				f = 100
			}
			if f != e.opacity {
				e.opacity = f
				changed = true
				log.Printf("[BackgroundImage:%s] opacity → %.0f%%", e.id, f)
			}
		}
	}

	if changed {
		go func() {
			time.Sleep(200 * time.Millisecond)

			if imageChanged && e.imageData != "" {
				e.detectImageSize(e.imageData)
			}

			e.recacheBackend()
			e.recacheFrontend()

			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	}
}

// ── Label ────────────────────────────────────────────────────────────

func (e *StatementBackgroundImage) GetLabel() string { return e.label }
func (e *StatementBackgroundImage) SetLabel(label string) {
	e.label = label
	e.recacheBackend()
}

// ── Wire registration — none ─────────────────────────────────────────

func (e *StatementBackgroundImage) RegisterConnectors() {}

// ── State accessors ──────────────────────────────────────────────────

func (e *StatementBackgroundImage) GetInitialized() bool   { return e.initialized }
func (e *StatementBackgroundImage) GetID() string          { return e.id }
func (e *StatementBackgroundImage) GetName() string        { return e.name }
func (e *StatementBackgroundImage) GetSelected() bool      { return e.selected }
func (e *StatementBackgroundImage) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementBackgroundImage) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementBackgroundImage) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementBackgroundImage) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementBackgroundImage) GetResize() bool        { return false }
func (e *StatementBackgroundImage) GetResizeEnable() bool {
	if e.frontendElem != nil {
		return e.frontendElem.IsResizeEnabled()
	}
	return false
}
func (e *StatementBackgroundImage) GetZIndex() int {
	if e.backendElem != nil {
		return e.backendElem.GetIndex()
	}
	return 0
}

func (e *StatementBackgroundImage) SetSelected(sel bool) {
	e.selected = sel
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
}

func (e *StatementBackgroundImage) SetDragEnable(en bool) {
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
// When resize is enabled, drag is disabled (same pattern as TextDisplay/Loop).
func (e *StatementBackgroundImage) SetResizeEnable(enabled bool) {
	if e.resizeLocked || e.frontendElem == nil {
		return
	}
	if enabled {
		e.frontendElem.SetDragEnable(false)
		e.dragEnabled = false
		e.selected = false
		e.frontendElem.SetResizeEnable(true)
		e.frontendElem.ShowResizeButtons(true)
		log.Printf("[BackgroundImage:%s] resize enabled", e.id)
	} else {
		e.frontendElem.SetResizeEnable(false)
		e.frontendElem.ShowResizeButtons(false)
		log.Printf("[BackgroundImage:%s] resize disabled", e.id)
	}
}

func (e *StatementBackgroundImage) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementBackgroundImage) SetX(x rulesDensity.Density) {
	if e.backendElem != nil {
		y := e.backendElem.GetYD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementBackgroundImage) SetY(y rulesDensity.Density) {
	if e.backendElem != nil {
		x := e.backendElem.GetXD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.backendElem.SetPositionD(nx, ny)
	}
}
func (e *StatementBackgroundImage) SetWidth(_ rulesDensity.Density)                        {}
func (e *StatementBackgroundImage) SetHeight(_ rulesDensity.Density)                       {}
func (e *StatementBackgroundImage) SetSize(_ rulesDensity.Density, _ rulesDensity.Density) {}
func (e *StatementBackgroundImage) SetStatus(s int)                                        { e.iconStatus = s }
func (e *StatementBackgroundImage) GetStatus() int                                         { return e.iconStatus }

// ── Icon ─────────────────────────────────────────────────────────────

func (e *StatementBackgroundImage) GetIconName() string     { return "Background" }
func (e *StatementBackgroundImage) GetIconCategory() string { return "Display" }

func (e *StatementBackgroundImage) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	iconLabel := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("🖼").Fill(data.ColorIcon).
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

func (e *StatementBackgroundImage) GetDeviceType() string { return "StatementBackgroundImage" }

func (e *StatementBackgroundImage) GetOuterBBox() scene.Rect {
	if e.backendElem == nil {
		return scene.Rect{}
	}
	x, y := e.backendElem.GetPosition()
	w, h := e.backendElem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementBackgroundImage) GetInnerBBox() *scene.Rect { return nil }
func (e *StatementBackgroundImage) GetKind() scenegraph.Kind  { return scenegraph.KindSimple }
func (e *StatementBackgroundImage) SetSceneNotify(fn func())  { e.sceneNotify = fn }

func (e *StatementBackgroundImage) MoveBy(dx, dy float64) {
	if e.backendElem == nil {
		return
	}
	x, y := e.backendElem.GetPosition()
	e.backendElem.SetPosition(x+dx, y+dy)
}

func (e *StatementBackgroundImage) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		"label":          e.label,
		"imageData":      e.imageData,
		"opacity":        e.opacity,
		"frontendWidth":  int(e.frontendWidth),
		"frontendHeight": int(e.frontendHeight),
	}
}
