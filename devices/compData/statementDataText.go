// /ide/devices/compData/statementDataText.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compData

// statementDataText.go — Data · Text device: maker-authored text as bytes.
//
// The second device of the DATA category: the maker WRITES the content —
// yaml, xml, json, html, css, markdown or plain text — in an embedded
// Monaco editor (FieldMonaco), and the device emits it as a []uint8
// collection wire (UTF-8; optional trailing NUL for C-string consumers,
// default ON). The export embeds the bytes as a per-INSTANCE flash array
// on the maker's side, exactly like Data · File.
//
// Português: Segundo device da categoria DATA: o maker ESCREVE o conteúdo
// — yaml, xml, json, html, css, markdown ou texto puro — num Monaco
// embutido (FieldMonaco), e o device o emite como fio []uint8 (UTF-8;
// NUL final opcional para consumidores C-string, default LIGADO). O
// export embute os bytes como array de flash por INSTÂNCIA, exatamente
// como o Data · File.
//
// Visual design:
//
//	┌─────────────────────┐  ← 2px border, type color of []uint8
//	│ TEXT            ◉   │  ← 18px header; ◉ = output connector
//	├─────────────────────┤
//	│    yaml · 12 lines  │  ← language + size (or "empty")
//	└─────────────────────┘
//	dataText1               ← editable label
//
// Body click:      Delete · Inspect
// Connector click: Connect (output-only)
// Double-click:    Inspect overlay (FieldFile picker)

import (
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

// StatementDataText is the Data · File device.
// No inputs — single output connector that emits []uint8.
type StatementDataText struct {
	stage sprite.Stage
	elem  sprite.Element

	id   string
	name string
	// text is the maker-authored content (UTF-8). Português: O conteúdo
	// autorado pelo maker (UTF-8).
	text string
	// language is the Monaco language id for highlighting only — the
	// emitted bytes are language-agnostic. Português: Id de linguagem do
	// Monaco, só para highlight — os bytes emitidos são agnósticos.
	language string
	// nullTerminated appends a trailing NUL to the emitted array so C
	// consumers can strstr()/printf() it directly. Default ON (decision
	// 2026-07-12); the length port still reports the byte count WITHOUT
	// the NUL. Português: Anexa NUL final ao array para consumidores C
	// usarem como string. Default LIGADO; o tamanho reportado NÃO conta
	// o NUL.
	nullTerminated bool
	label          string
	comment        string

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

func (e *StatementDataText) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementDataText) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementDataText) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementDataText) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementDataText) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }
func (e *StatementDataText) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementDataText) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// bodySummary is the block's one-line body: "yaml · 12 lines" or "empty".
// Português: A linha do corpo do bloco: "yaml · 12 linhas" ou "empty".
func (e *StatementDataText) bodySummary() (s string, muted bool) {
	if strings.TrimSpace(e.text) == "" {
		return "empty", true
	}
	lines := strings.Count(e.text, "\n") + 1
	lang := e.language
	if lang == "" {
		lang = "text"
	}
	return fmt.Sprintf("%s \u00b7 %d line(s)", lang, lines), false
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementDataText) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementDataText) Remove() {
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

func (e *StatementDataText) renderSVG() string {
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
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">TEXT</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)

	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, bodyR-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Body: language + line count, or the empty-state hint.
	// Português: Linguagem + contagem de linhas, ou o estado vazio.
	display, muted := e.bodySummary()
	color := rulesDevice.KColorDeviceText
	if muted {
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

func (e *StatementDataText) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementDataText) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("dataText")
	e.resizeLocked = true
	// Fresh devices default to yaml + null-terminated (decision
	// 2026-07-12); loads overwrite via ApplyProperties. Português:
	// Devices novos nascem yaml + null-terminated; loads sobrescrevem.
	if e.language == "" {
		e.language = "yaml"
	}
	e.nullTerminated = true
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
		log.Printf("[DataText] warning: no context menu set \u2014 menus disabled")
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

func (e *StatementDataText) wireEvents() {
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

func (e *StatementDataText) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[DataText] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementDataText) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

// wireEditorConfig asks the wire layer who consumes this device's output
// and reads the Phase B editor config off the peer connector — LIVE at
// every open: the specialist updates the dictionary, the maker gets it on
// the next open; disconnect and the manual controls return. Nothing is
// serialized. Português: Pergunta à camada de fio quem consome a saída e
// lê a config de editor da Fase B do conector vizinho — AO VIVO a cada
// abertura: o especialista atualiza o dicionário, o maker recebe na
// próxima abertura; desconectou, voltam os controles manuais. Nada é
// serializado.
func (e *StatementDataText) wireEditorConfig() (lang, dictJSON string) {
	if e.wireMgr == nil {
		return "", ""
	}
	peer := e.wireMgr.ConnectedPeer(e.id, "output")
	if peer == nil {
		return "", ""
	}
	return peer.EditorLang, peer.EditorDictJSON
}

func (e *StatementDataText) inspectConfig() overlay.Config {
	// Wire-provided config LOCKS the manual controls (transparency for
	// the maker; the specialist's parser expects this language).
	// Português: Config vinda do fio TRAVA os controles manuais.
	wireLang, wireDict := e.wireEditorConfig()
	monacoLang := e.language
	langLocked := false
	if wireLang != "" {
		monacoLang = wireLang
		langLocked = true
	}
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:                "text",
						Label:              translate.T("propContent", "Content"),
						Type:               overlay.FieldMonaco,
						Value:              e.text,
						Language:           monacoLang,
						CompletionDictJSON: wireDict,
					},
					{
						Key:                  "language",
						Label:                langSelectLabel(langLocked),
						Type:                 overlay.FieldSelect,
						Value:                monacoLang,
						ReadOnly:             langLocked,
						MonacoLanguageTarget: "text",
						Options: []overlay.Option{
							{Value: "yaml", Label: "yaml"},
							{Value: "xml", Label: "xml"},
							{Value: "json", Label: "json"},
							{Value: "html", Label: "html"},
							{Value: "css", Label: "css"},
							{Value: "markdown", Label: "markdown"},
							{Value: "plaintext", Label: "plain text"},
						},
					},
					{
						Key:   "nullTerminated",
						Label: translate.T("propNullTerminated", "Null-terminated (C string)"),
						Type:  overlay.FieldCheckbox,
						Value: boolToStr(e.nullTerminated),
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
			{Label: "Help", Type: overlay.TabMarkdown, Content: dataTextHelp()},
		},
		OnSave: func(values map[string]string) {
			if v, ok := values["comment"]; ok {
				e.comment = v
			}
			if v, ok := values["text"]; ok {
				e.text = v
			}
			if v, ok := values["language"]; ok {
				e.language = v
			}
			if v, ok := values["nullTerminated"]; ok {
				e.nullTerminated = v == "true"
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

func (e *StatementDataText) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementDataText) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if v, ok := values["text"]; ok {
		e.text = v
	}
	if v, ok := values["language"]; ok {
		e.language = v
	}
	if v, ok := values["nullTerminated"]; ok {
		e.nullTerminated = v == "true"
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

func (e *StatementDataText) RegisterConnectors() {
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

func (e *StatementDataText) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementDataText) Get() *html.TagSvg { return nil }
func (e *StatementDataText) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementDataText) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementDataText) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementDataText) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementDataText) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementDataText) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementDataText) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementDataText) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementDataText) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementDataText) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementDataText) MoveBy(dx, dy float64) {
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

func (e *StatementDataText) GetInitialized() bool   { return e.initialized }
func (e *StatementDataText) GetID() string          { return e.id }
func (e *StatementDataText) GetName() string        { return e.name }
func (e *StatementDataText) GetSelected() bool      { return e.selected }
func (e *StatementDataText) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementDataText) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementDataText) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementDataText) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementDataText) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementDataText) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementDataText) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementDataText) GetStatus() int  { return e.iconStatus }
func (e *StatementDataText) SetStatus(s int) { e.iconStatus = s }
func (e *StatementDataText) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementDataText) SetSelected(sel bool) {
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

func (e *StatementDataText) SetDragEnable(en bool) {
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

func (e *StatementDataText) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementDataText) GetIconName() string     { return "DataText" }
func (e *StatementDataText) GetIconCategory() string { return "Data" }

func (e *StatementDataText) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("TEXT").Fill(data.ColorIcon).
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

func (e *StatementDataText) GetDeviceType() string { return "StatementDataText" }
func (e *StatementDataText) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"label":          e.label,
		"language":       e.language,
		"nullTerminated": boolToStr(e.nullTerminated),
	}
	if e.text != "" {
		props["text"] = e.text
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

func (e *StatementDataText) GetComment() string  { return e.comment }
func (e *StatementDataText) SetComment(c string) { e.comment = c }
func (e *StatementDataText) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementDataText) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementDataText) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementDataText) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help text ─────────────────────────────────────────────────────────────────

// langSelectLabel names the language select, flagging the wire lock.
// Português: Nomeia o select de linguagem, sinalizando a trava do fio.
func langSelectLabel(locked bool) string {
	if locked {
		return translate.T("propLanguageWired", "Language (from wire)")
	}
	return translate.T("propLanguage", "Language")
}

func dataTextHelp() string {
	return `# Data · Text

Maker-authored text — written in the embedded editor — emitted as
**[]uint8** bytes (UTF-8).

The export embeds the bytes into the generated app as a flash array
(per instance), and the connected device receives the (pointer,
length) pair. With **null-terminated** on (default), a trailing NUL
is appended so C code can treat the pointer as a string; the length
still reports the byte count WITHOUT the NUL.

## Properties

| Property        | Type   | Description                         |
|-----------------|--------|-------------------------------------|
| Content         | editor | The text (yaml, xml, json, ...)     |
| Language        | select | Editor highlighting only            |
| Null-terminated | bool   | Append trailing NUL (default on)    |
| Label           | string | Name shown below the device         |

## Output

| Port   | Type    |
|--------|---------|
| output | []uint8 |

## Tips

- Connect to a device port authored as a byte slice
  (` + "`const uint8_t *data` + `// slice:len.`" + ` in C99).
- The language dropdown changes highlighting on the next open.
`
}

func (e *StatementDataText) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementDataText) OpenInspect() { go e.showInspectOverlay() }
