// /ide/devices/compData/statementDataFile.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compData

// statementDataFile.go — Data · File device: a maker-uploaded file as bytes.
//
// The first device of the DATA category: the maker uploads ONE file
// (template, image, blob — the wizard's asset whitelist) and the device
// emits it as a []uint8 collection wire. The export embeds the bytes as a
// per-INSTANCE flash array on the maker's side (IOTM_ASSET_ATTR: PROGMEM
// on AVR) and feeds the (pointer, length) pair to the connected function's
// slice-collapsed parameter. No filesystem at runtime — the maker's own
// page/logo/config rides inside the binary.
//
// Português: Primeiro device da categoria DATA: o maker envia UM arquivo
// (template, imagem, blob — a whitelist de assets do wizard) e o device o
// emite como fio de coleção []uint8. O export embute os bytes como array
// de flash por INSTÂNCIA no lado do maker (IOTM_ASSET_ATTR: PROGMEM no
// AVR) e entrega o par (ponteiro, tamanho) ao parâmetro colapsado por
// slice: da função conectada. Sem filesystem em runtime.
//
// Visual design:
//
//	┌─────────────────────┐  ← 2px border, type color of []uint8
//	│ []U8            ◉   │  ← 18px header; ◉ = output connector
//	├─────────────────────┤
//	│    📄 portal.html   │  ← chosen file name (or "no file")
//	└─────────────────────┘
//	dataFile1               ← editable label
//
// Body click:      Delete · Inspect
// Connector click: Connect (output-only)
// Double-click:    Inspect overlay (FieldFile picker)

import (
	"encoding/json"
	"fmt"
	"log"
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

// dataFileAccept is the maker-upload whitelist — the SAME set the wizard
// accepts for device assets, kept in sync by hand with
// server/handler/projectapi (textAssetExts + binaryAssetExts).
// Português: Whitelist de upload do maker — o MESMO conjunto do wizard,
// mantido em sincronia à mão com o server.
const dataFileAccept = ".html,.htm,.tmpl,.txt,.json,.csv,.svg,.md,.css," +
	".gif,.png,.jpg,.jpeg,.bin,.dat,.ico"

// dataFileMaxBytes mirrors the server's per-asset cap (512 KB decoded) so
// the maker's error lands at the pick, not at export.
// Português: Espelha o teto por asset do server (512 KB decodificado).
const dataFileMaxBytes = 512 * 1024

// StatementDataFile is the Data · File device.
// No inputs — single output connector that emits []uint8.
type StatementDataFile struct {
	stage sprite.Stage
	elem  sprite.Element

	id   string
	name string
	// filePayload is the FieldFile StoreName value: JSON
	// {"name","dataUrl"} — the whole file rides the scene inside it.
	// Português: O valor StoreName do FieldFile: JSON {"name","dataUrl"}
	// — o arquivo inteiro viaja na cena dentro dele.
	filePayload string
	// fileName is the render cache parsed from filePayload.
	// Português: Cache de render extraído do filePayload.
	fileName string
	label    string
	comment  string

	width  rulesDensity.Density
	height rulesDensity.Density

	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool // always true: data devices do not resize

	pendingSelected     *bool
	pendingDragEnable   *bool
	pendingResizeEnable *bool

	resizerButton block.ResizeButton
	ctxMenu       *contextMenu.Controller
	wireMgr       *wire.Manager
	gridAdjust    grid.Adjust

	iconStatus  int
	lastClick   time.Time
	sceneNotify func()
	sceneMgr    *scene.Serializer
	onRemove    func(id string)
}

// ── Dependency injection ──────────────────────────────────────────────────────

func (e *StatementDataFile) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementDataFile) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementDataFile) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementDataFile) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementDataFile) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }
func (e *StatementDataFile) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementDataFile) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// setFilePayload stores the FieldFile value and refreshes the render cache.
// Português: Grava o valor do FieldFile e atualiza o cache de render.
func (e *StatementDataFile) setFilePayload(payload string) {
	e.filePayload = payload
	e.fileName = ""
	if payload != "" {
		var v struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(payload), &v) == nil {
			e.fileName = v.Name
		}
	}
	if e.initialized {
		go e.recacheSVG()
	}
}

// renameFile rewrites the NAME inside the stored payload — the maker
// uploads "Screenshot_2026-07-13_at_10_47_05.png" and calls it "img.png"
// for the block and the generated code's comment. The bytes are
// untouched; only the JSON's "name" changes, so the rename survives the
// scene round-trip for free (it lives inside properties.file).
// Português: Reescreve o NOME dentro do payload — o maker sobe um nome
// complicado e o chama de "img.png" para o bloco e para o comentário do
// código gerado. Os bytes ficam intactos; só o "name" do JSON muda, e o
// rename sobrevive ao round-trip da cena de graça (vive dentro de
// properties.file).
func (e *StatementDataFile) renameFile(name string) {
	if e.filePayload == "" {
		return
	}
	var v struct {
		Name    string `json:"name"`
		DataURL string `json:"dataUrl"`
	}
	if json.Unmarshal([]byte(e.filePayload), &v) != nil {
		return
	}
	v.Name = name
	if b, err := json.Marshal(v); err == nil {
		e.filePayload = string(b)
		e.fileName = name
		if e.initialized {
			go e.recacheSVG()
		}
	}
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementDataFile) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementDataFile) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	if e.elem != nil {
		e.elem.SetVisible(false)
		elem := e.elem
		e.elem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
}

// ── SVG rendering ─────────────────────────────────────────────────────────────

func (e *StatementDataFile) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	pin := rulesConnection.PinBodyInset()
	bodyR := w - pin
	ts := rulesDevice.TypeStyleFor("[]uint8")

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, bodyR-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, bodyR-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, bodyR-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// Header tag: the DATA identity, colored by the wire type.
	// Português: A identidade DATA, na cor do tipo do fio.
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">FILE</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)

	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, bodyR-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Body: the chosen file name (truncated) or the empty-state hint.
	// Português: O nome do arquivo escolhido (truncado) ou o estado vazio.
	display := e.fileName
	color := rulesDevice.KColorDeviceText
	if display == "" {
		display = "no file"
		color = "#8899AA" // muted — matches KDeviceLabel
	}
	if len(display) > 18 {
		display = display[:17] + "\u2026"
	}
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		bodyR/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue-2,
		color, escapeXml(display),
	)

	svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideRight, bodyR, h/2, ts.Color)

	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementDataFile) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementDataFile) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("dataFile")
	e.resizeLocked = true
	if e.width == 0 {
		e.width = rulesDevice.KConstDefaultWidth
	}
	if e.height == 0 {
		e.height = rulesDevice.KConstDefaultHeight
	}
	totalH := e.height + rulesDevice.KLabelHeight
	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:     e.id,
		Width:  e.width.GetFloat(),
		Height: totalH.GetFloat(),
		Index:  rulesZIndex.Constant,
		SvgXml: e.renderSVG(),
	})
	if err != nil {
		return fmt.Errorf("create element: %w", err)
	}
	minH := rulesDensity.Density(rulesDevice.KConstMinHeight) + rulesDevice.KLabelHeight
	e.elem.SetMinSizeD(rulesDevice.KConstMinWidth, minH)
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		_ = e.elem.SetResizeButtons(adapter)
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}
	if e.ctxMenu == nil {
		log.Printf("[DataFile] warning: no context menu set \u2014 menus disabled")
	}
	e.wireEvents()
	e.initialized = true
	if e.pendingSelected != nil {
		e.SetSelected(*e.pendingSelected)
		e.pendingSelected = nil
	}
	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}
	if e.pendingResizeEnable != nil {
		e.SetResizeEnable(*e.pendingResizeEnable)
		e.pendingResizeEnable = nil
	}
	return nil
}

// ── Events ────────────────────────────────────────────────────────────────────

func (e *StatementDataFile) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if e.ctxMenu == nil {
			return
		}
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX := elemX + event.LocalX
		menuY := elemY + event.LocalY

		now := time.Now()
		if now.Sub(e.lastClick) < 300*time.Millisecond {
			e.lastClick = time.Time{}
			go e.showInspectOverlay()
			return
		}
		e.lastClick = now

		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2,
			event.LocalX, event.LocalY) {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "output"), menuX, menuY)
			return
		}

		go e.ctxMenu.OpenForDevice(e, e.bodyMenuItems(), menuX, menuY)
	})

	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.elem.GetPositionD()
		nx, ny := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(nx, ny)
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
		if e.sceneMgr != nil {
			e.sceneMgr.EndDrag(e.id, 0, 0)
		}
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2, lx, ly) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── Menu ──────────────────────────────────────────────────────────────────────

func (e *StatementDataFile) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[DataFile] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementDataFile) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementDataFile) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:         "file",
						Label:       translate.T("propFile", "File"),
						Type:        overlay.FieldFile,
						Value:       e.filePayload,
						Accept:      dataFileAccept,
						MaxBytes:    dataFileMaxBytes,
						StoreName:   true,
						Placeholder: translate.T("propFileHint", "Embedded into the generated app as bytes"),
					},
					{
						// Empty = keep the uploaded name; typed = rename.
						// Born empty so picking a NEW file never gets its
						// fresh name overridden by stale text (field
						// report 2026-07-13). Português: Vazio = mantém o
						// nome do upload; digitado = renomeia. Nasce vazio
						// para a troca de arquivo nunca ter o nome novo
						// atropelado por texto velho.
						Key:         "rename",
						Label:       translate.T("propFileName", "File name"),
						Type:        overlay.FieldText,
						Value:       "",
						Placeholder: e.renamePlaceholder(),
					},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
					{
						Key:         "comment",
						Label:       translate.T("propComment", "Comment"),
						Type:        overlay.FieldTextarea,
						Value:       e.comment,
						Placeholder: translate.T("propCommentPlaceholder", "Comment shown in generated code..."),
						Rows:        3,
					},
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id, ReadOnly: true},
				},
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: dataFileHelp()},
		},
		OnSave: func(values map[string]string) {
			if v, ok := values["comment"]; ok {
				e.comment = v
			}
			if v, ok := values["file"]; ok {
				e.setFilePayload(v)
			}
			// Rename AFTER the payload: a fresh pick keeps its own name
			// unless the maker typed one. Português: Renomeia DEPOIS do
			// payload: upload novo mantém o próprio nome, salvo se o
			// maker digitou um.
			if v, ok := values["rename"]; ok && strings.TrimSpace(v) != "" {
				e.renameFile(strings.TrimSpace(v))
			}
			if lbl, ok := values["label"]; ok {
				e.label = lbl
			}
			go func() {
				time.Sleep(200 * time.Millisecond)
				e.recacheSVG()
				if e.sceneNotify != nil {
					e.sceneNotify()
				}
			}()
		},
	}
}

func (e *StatementDataFile) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementDataFile) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if v, ok := values["file"]; ok {
		e.setFilePayload(v)
	}
	if v, ok := values["rename"]; ok && strings.TrimSpace(v) != "" {
		e.renameFile(strings.TrimSpace(v))
	}
	if lbl, ok := values["label"]; ok {
		e.label = lbl
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.recacheSVG()
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	}()
}

// ── Wire connectors ───────────────────────────────────────────────────────────

func (e *StatementDataFile) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:       wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput: true,
		// []uint8 is the token of slice-collapsed C ports (`slice:` on a
		// byte pointer) — the natural plug for maker data; []byte covers
		// the Go spelling. Português: []uint8 é o token das portas C
		// colapsadas por slice: — o plugue natural; []byte cobre o Go.
		AllowedTypes:       []string{"[]uint8", "[]byte"},
		AcceptNotConnected: true,
		MaxConnections:     0,
		Label:              "Output",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			w := e.elem.GetWidthD().GetFloat()
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideRight,
				w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2)
			return ex + ax, ey + ay
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementDataFile) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementDataFile) Get() *html.TagSvg { return nil }
func (e *StatementDataFile) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementDataFile) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementDataFile) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementDataFile) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementDataFile) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementDataFile) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementDataFile) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementDataFile) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementDataFile) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementDataFile) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementDataFile) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// ── State ─────────────────────────────────────────────────────────────────────

func (e *StatementDataFile) GetInitialized() bool   { return e.initialized }
func (e *StatementDataFile) GetID() string          { return e.id }
func (e *StatementDataFile) GetName() string        { return e.name }
func (e *StatementDataFile) GetSelected() bool      { return e.selected }
func (e *StatementDataFile) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementDataFile) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementDataFile) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementDataFile) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementDataFile) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementDataFile) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementDataFile) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementDataFile) GetStatus() int  { return e.iconStatus }
func (e *StatementDataFile) SetStatus(s int) { e.iconStatus = s }
func (e *StatementDataFile) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementDataFile) SetSelected(sel bool) {
	if e.selectLocked {
		e.selected = false
		return
	}
	e.selected = sel
	if e.elem == nil {
		e.pendingSelected = &sel
		return
	}
	if sel {
		e.SetDragEnable(true)
	} else {
		e.SetDragEnable(false)
	}
	e.elem.ShowResizeButtons(false)
}

func (e *StatementDataFile) SetDragEnable(en bool) {
	if e.dragLocked {
		e.dragEnabled = false
		return
	}
	e.dragEnabled = en
	if e.elem == nil {
		e.pendingDragEnable = &en
		return
	}
	e.elem.SetDragEnable(en)
	e.elem.ShowResizeButtons(false)
}

func (e *StatementDataFile) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementDataFile) GetIconName() string     { return "DataFile" }
func (e *StatementDataFile) GetIconCategory() string { return "Data" }

func (e *StatementDataFile) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("FILE").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 16).Y((rulesIcon.Height / 2).GetInt() + 4)
	wl, _ := utilsText.GetTextSize(data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight, rulesIcon.FontStyle, data.LabelFontSize.GetInt())
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).FontWeight(rulesIcon.FontWeight).FontStyle(rulesIcon.FontStyle).
		FontSize(data.LabelFontSize.GetInt()).Text(data.Label).Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - wl/2).Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, labelIcon, label)
	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}

// ── Scene export ──────────────────────────────────────────────────────────────

func (e *StatementDataFile) GetDeviceType() string { return "StatementDataFile" }
func (e *StatementDataFile) GetProperties() map[string]interface{} {
	props := map[string]interface{}{"label": e.label}
	if e.filePayload != "" {
		props["file"] = e.filePayload
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

func (e *StatementDataFile) GetComment() string  { return e.comment }
func (e *StatementDataFile) SetComment(c string) { e.comment = c }
func (e *StatementDataFile) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementDataFile) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementDataFile) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementDataFile) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help text ─────────────────────────────────────────────────────────────────

// renamePlaceholder is the rename field's hint: the current name when a
// file exists, an instruction otherwise. Português: Dica do campo de
// renomear: o nome atual quando há arquivo; instrução quando não há.
func (e *StatementDataFile) renamePlaceholder() string {
	if e.fileName != "" {
		return e.fileName + " \u2014 " + translate.T("propFileNameKeep", "leave empty to keep")
	}
	return translate.T("propFileNameHint", "rename after upload (e.g. img.png)")
}

func dataFileHelp() string {
	return `# Data · File

Carries ONE uploaded file and emits it as **[]uint8** bytes.

The export embeds the bytes into the generated app as a flash array
(per instance — two File devices carry two different files), and the
connected device receives the (pointer, length) pair. No filesystem
is needed at runtime.

## Properties

| Property | Type   | Description                          |
|----------|--------|--------------------------------------|
| File     | upload | The file to embed (max 512 KB)       |
| Label    | string | Name shown below the device          |

## Output

| Port   | Type    |
|--------|---------|
| output | []uint8 |

## Tips

- Connect to a device port authored as a byte slice
  (` + "`const uint8_t *data` + `// slice:len.`" + ` in C99).
- Templates, images, configs — anything in the asset whitelist.
`
}

func (e *StatementDataFile) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementDataFile) OpenInspect() { go e.showInspectOverlay() }
