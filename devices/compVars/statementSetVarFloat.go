// /ide/devices/compVars/statementSetVarFloat.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compVars

// statementSetVarFloat.go — Set-variable device (int).
//
// Writes the value wired into its input to a user-declared project variable of
// type int. At code generation a SetVar emits an assignment (OpAssign) of the
// wired register to the variable's identifier (see emitSetVar in
// server/codegen/ir/emit.go). One input, no output — it is a pure sink.
//
// Visual design (Option A tint):
//
//	┌─────────────────────┐  ← 2px border, 5px corner radius, type color (float = peach)
//	│ SET             float │  ← 18px header: "SET" tag (left), type label (right)
//	├─────────────────────┤  ← divider
//	│                     │
//	│   ◉   counter       │  ← variable name, 16px bold, centered; ◉ = input connector
//	│                     │
//	└─────────────────────┘
//	setVarFloat1              ← editable label, 12px muted
//
// Body click:      Inspect · Delete
// Connector click: Connect (input-only)
// Double-click:    Inspect overlay
//
// Português: Device "gravar variável" (int). Grava o valor ligado na entrada
// numa variável de projeto do tipo float. No codegen, um SetVar emite uma
// atribuição (OpAssign) do registrador ligado para o identificador da
// variável. Uma entrada, sem saída — é um sink puro.

import (
	"fmt"
	"log"
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

// StatementSetVarFloat is a set-variable device for type int.
// Single input connector "value" — assigns the wired value to the variable.
type StatementSetVarFloat struct {
	stage sprite.Stage
	elem  sprite.Element

	id      string
	name    string
	varName string // the project variable this device writes to (its codegen identifier)
	label   string // editable name shown below ornament (defaults to id)
	// [COMMENT] user comment — appears as `// ` lines above this device's
	// statement in the generated code, in the Code Preview, and in the
	// device's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste device no código gerado, no Code Preview e no
	// tooltip de hover do device.
	comment string

	// e.height is ornament height only. Total element = e.height + KLabelHeight.
	width  rulesDensity.Density
	height rulesDensity.Density

	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool // always true: constants do not resize

	pendingSelected     *bool
	pendingDragEnable   *bool
	pendingResizeEnable *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller.
	ctxMenu    *contextMenu.Controller
	wireMgr    *wire.Manager
	gridAdjust grid.Adjust

	iconStatus  int
	lastClick   time.Time
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
}

// ── Dependency injection ──────────────────────────────────────────────────────

func (e *StatementSetVarFloat) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementSetVarFloat) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementSetVarFloat) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementSetVarFloat) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementSetVarFloat) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementSetVarFloat) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementSetVarFloat) SetVarName(v string) {
	e.varName = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementSetVarFloat) GetVarName() string             { return e.varName }
func (e *StatementSetVarFloat) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementSetVarFloat) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementSetVarFloat) Remove() {
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

// renderSVG builds the complete SVG for this device including the label area.
// Total height = ornament height + KLabelHeight.
func (e *StatementSetVarFloat) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat() // ornament height only
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	// [PIN] the body is inset on the LEFT by the pin length: the standard
	// connector pin lives in the freed margin, protruding from the border
	// with the wire anchored at its outer tip — the element's left edge. The
	// element size itself is unchanged, so grid layout and saved scenes are
	// unaffected.
	// Português: O corpo recua à ESQUERDA o comprimento do pino: o pino
	// padrão vive na margem liberada, saindo da borda com o fio ancorado na
	// ponta externa — a borda esquerda do element. O tamanho do element não
	// muda — grid e cenas salvas não são afetados.
	pin := rulesConnection.PinBodyInset()
	ts := rulesDevice.TypeStyleFor("float64")

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect — border color = type color
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		pin+bw/2, bw/2, w-pin-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header — rounded top, flat bottom
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		pin+bw, bw, w-pin-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		pin+bw, bw+hh/2, w-pin-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// "SET" tag (left) + type label (right)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" font-weight="bold" dominant-baseline="middle">SET</text>`,
		pin+bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="middle">float</text>`,
		w-bw-6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		pin+bw, bw+hh, w-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Variable name (body)
	displayName := e.varName
	if displayName == "" {
		displayName = "?"
	}
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		(w+pin)/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, escapeXml(displayName),
	)

	// Input connector
	// [PIN] the standard connector pin at the body's left border, filled
	// with the type color so pin and wire read as one continuous piece; the
	// wire anchors at the pin's outer tip (see RegisterConnectors).
	// Português: O pino padrão na borda esquerda do corpo, preenchido com a
	// cor do tipo para pino e fio lerem como uma peça contínua; o fio ancora
	// na ponta externa do pino (ver RegisterConnectors).
	svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideLeft, pin, h/2, ts.Color)

	// Label
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementSetVarFloat) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementSetVarFloat) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("setVarFloat")
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
		log.Printf("[SetVarFloat] warning: no context menu set — menus disabled")
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

func (e *StatementSetVarFloat) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if e.ctxMenu == nil {
			return
		}
		// Close-then-return: first click dismisses, next click
		// decides which new menu to open. The linear renderer has
		// no ghost-click window — the overlay absorbs backdrop
		// clicks itself and only reports them after the close
		// finishes.
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX := elemX + event.LocalX
		menuY := elemY + event.LocalY

		// Double-click → Inspect.
		now := time.Now()
		if now.Sub(e.lastClick) < 300*time.Millisecond {
			e.lastClick = time.Time{}
			go e.showInspectOverlay()
			return
		}
		e.lastClick = now

		// Connector hit test.
		// [PIN] standard pin hit box — same edge point the renderer draws
		// and the wire anchors to, so click, drawing and wire agree.
		// Português: Caixa de clique do pino padrão — mesmo edge point que o
		// renderer desenha e onde o fio ancora; clique, desenho e fio
		// concordam.
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), e.height.GetFloat()/2,
			event.LocalX, event.LocalY) {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "value"), menuX, menuY)
			return
		}

		go e.ctxMenu.OpenForDevice(e, e.bodyMenuItems(), menuX, menuY)
	})

	// [SCENE] real-time conflict feedback — notify scene
	// on every drag step so the stage-level overlay reacts
	// to position changes immediately, not only on release.
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

	e.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		// [PIN] same hit box as the click handler — one geometry source.
		// Português: Mesma caixa do handler de clique — uma fonte de
		// geometria só.
		if rulesConnection.PinHit(rulesConnection.PinSideLeft,
			rulesConnection.PinBodyInset(), e.height.GetFloat()/2, lx, ly) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── Menu ──────────────────────────────────────────────────────────────────────

// bodyMenuItems returns the body context menu for this constant.
// Delete first, Inspect second — canonical order per decision D4,
// fixing the original inversion present on every compConsts device.
//
// Português: Itens do menu de contexto do corpo. Delete primeiro,
// Inspect depois — ordem canônica conforme decisão D4.
func (e *StatementSetVarFloat) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[SetVarFloat] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementSetVarFloat) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementSetVarFloat) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{Key: "varName", Label: translate.T("propVariable", "Variable"), Type: overlay.FieldText, Value: e.varName, Placeholder: "counter", InputFilter: "identifier"},
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
			{
				Label: "Code Preview", Type: overlay.TabMonaco,
				Content:  devices.CommentPrefix(e.comment) + fmt.Sprintf("// Set variable: %s\n// Assigns the wired value to the variable (%s = <input>).", e.varName, e.varName),
				Language: "go", ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: setVarFloatHelp()},
		},
		OnSave: func(values map[string]string) {
			// [COMMENT] the form's comment must be stored here too: this
			// OnSave handles its keys inline (it does not route through
			// ApplyProperties, unlike the math family), so without this
			// line the typed comment would be silently dropped.
			// Português: O comentário do formulário precisa ser gravado
			// aqui também: este OnSave trata suas chaves inline (não roteia
			// pelo ApplyProperties, diferente da família math), então sem
			// esta linha o comentário digitado seria perdido em silêncio.
			if v, ok := values["comment"]; ok {
				e.comment = v
			}
			if v, ok := values["varName"]; ok {
				e.varName = v
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

func (e *StatementSetVarFloat) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementSetVarFloat) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if v, ok := values["varName"]; ok {
		e.varName = v
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

func (e *StatementSetVarFloat) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "value"},
		IsOutput:           false,
		AllowedTypes:       []string{"float", "float64"},
		AcceptNotConnected: true,
		MaxConnections:     0,
		Label:              "Value",
		PositionFunc: func() (float64, float64) {
			// [PIN] the wire anchors at the OUTER TIP of the standard pin —
			// exactly the element's left edge, vertically centered on the
			// ornament.
			// Português: O fio ancora na PONTA EXTERNA do pino padrão —
			// exatamente a borda esquerda do element, centrado verticalmente
			// no ornamento.
			ex, ey := e.elem.GetPosition()
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), e.height.GetFloat()/2)
			return ex + ax, ey + ay
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementSetVarFloat) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementSetVarFloat) Get() *html.TagSvg { return nil }
func (e *StatementSetVarFloat) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementSetVarFloat) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementSetVarFloat) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementSetVarFloat) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementSetVarFloat) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementSetVarFloat) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementSetVarFloat) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementSetVarFloat) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementSetVarFloat) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementSetVarFloat) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementSetVarFloat) MoveBy(dx, dy float64) {
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

func (e *StatementSetVarFloat) GetInitialized() bool   { return e.initialized }
func (e *StatementSetVarFloat) GetID() string          { return e.id }
func (e *StatementSetVarFloat) GetName() string        { return e.name }
func (e *StatementSetVarFloat) GetSelected() bool      { return e.selected }
func (e *StatementSetVarFloat) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementSetVarFloat) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementSetVarFloat) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementSetVarFloat) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementSetVarFloat) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementSetVarFloat) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementSetVarFloat) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementSetVarFloat) GetStatus() int  { return e.iconStatus }
func (e *StatementSetVarFloat) SetStatus(s int) { e.iconStatus = s }
func (e *StatementSetVarFloat) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementSetVarFloat) SetSelected(sel bool) {
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

func (e *StatementSetVarFloat) SetDragEnable(en bool) {
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

func (e *StatementSetVarFloat) SetResizeEnable(_ bool) {
	// Constant devices never resize — resizeLocked is always true.
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementSetVarFloat) GetIconName() string     { return "SetVarFloat" }
func (e *StatementSetVarFloat) GetIconCategory() string { return "Variables" }

func (e *StatementSetVarFloat) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 3).
		Text("SET").Fill(data.ColorIcon).
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

func (e *StatementSetVarFloat) GetDeviceType() string { return "StatementSetVarFloat" }
func (e *StatementSetVarFloat) GetProperties() map[string]interface{} {
	props := map[string]interface{}{"varName": e.varName, "label": e.label}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// device's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do device.
func (e *StatementSetVarFloat) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementSetVarFloat) SetComment(c string) { e.comment = c }

// GetVariableDecl implements scene.VariableDeclarer: this device declares the
// integer variable it names. The serializer collects it into the scene's
// top-level "variables" array (Path A) so the codegen emits one zero-init
// declaration (float <name> = 0.0f; / var <name> float64). An empty name (device
// placed but not yet bound) contributes nothing.
//
// Português: Implementa scene.VariableDeclarer — declara a variável float que
// nomeia. O serializer a coleta no array "variables" (Path A) para o codegen
// emitir uma declaração zero-init. Nome vazio (ainda não vinculado) não
// contribui.
func (e *StatementSetVarFloat) GetVariableDecl() (name string, typ string) {
	return e.varName, "float"
}
func (e *StatementSetVarFloat) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementSetVarFloat) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementSetVarFloat) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementSetVarFloat) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help text ─────────────────────────────────────────────────────────────────

func setVarFloatHelp() string {
	return `# SetVarFloat — Set Float Variable

Writes the value wired into its input to a user-declared project **variable** of
type **int**.

At code generation a SetVar emits an **assignment** — the variable receives the
wired value (counter = input). Declare variables in the **Variables** panel,
then drag a chip onto the stage to place a Get or Set.

## Properties

| Property | Type   | Description                        |
|----------|--------|------------------------------------|
| Variable | string | Name of the float variable to write  |
| Label    | string | Name shown below the device        |

## Input

| Port  | Type |
|-------|------|
| value | float |

## Tips

- **Double-click** the device body to open Properties quickly.
- Wire a **Const**, **Add**, **Mul**, **Div**, or **GetVarInt** output into the input.
- Pair with **GetVarInt** to read the same variable elsewhere on the stage.
`
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementSetVarFloat) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
