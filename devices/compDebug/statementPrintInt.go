// devices/compDebug/statementPrintInt.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compDebug

// statementPrintInt.go — Debug-print sink device (int).
//
// Prints the value wired into its input to standard output. At code generation
// it emits OpPrint (see emitPrint in server/codegen/ir/emit.go), which the Go
// backend renders as fmt.Printf and the C99 backend as printf — unlike display
// widgets (OpOutput), the print ALWAYS lands in the generated program. One
// input, no output — it is a pure sink.
//
// Visual design (Option A tint):
//
//	┌─────────────────────┐  ← 2px border, 5px corner radius, Debug family orange (type color stays on the pin)
//	│ DEBUG           int │  ← 18px header: "PRINT" tag (left), type label (right)
//	├─────────────────────┤  ← divider
//	│                     │
//	│   ◉   temp          │  ← prefix (or "print"), 16px bold, centered; ◉ = input pin
//	│                     │
//	└─────────────────────┘
//	printInt1               ← editable label, 12px muted
//
// Body click:      Inspect · Delete
// Connector click: Connect (input-only)
// Double-click:    Inspect overlay
//
// Português: Device sink "imprimir" (int). Imprime o valor ligado na entrada
// no stdout. No codegen emite OpPrint — fmt.Printf no Go, printf no C99;
// diferente dos widgets de display (OpOutput), o print SEMPRE vira código.
// Uma entrada, sem saída — é um sink puro.

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

// StatementPrintInt is a debug-print sink device for type int.
// Single input connector "value" — at code generation it emits a print of the
// wired value to standard output (fmt.Printf in Go, printf in C99), with an
// optional text prefix and a format variant (decimal or hexadecimal).
//
// Português: Device sink de print de depuração para int. Conector único
// "value" — na geração de código emite um print do valor no stdout
// (fmt.Printf no Go, printf no C99), com prefixo de texto opcional e
// variante de formato (decimal ou hexadecimal).
type StatementPrintInt struct {
	stage sprite.Stage
	elem  sprite.Element

	id   string
	name string
	// prefix is maker-typed free text printed BEFORE the value ("temp 27").
	// format selects the per-type variant — for int: "decimal" | "hex".
	// Both travel to the server as node properties and reach the backends
	// through OpPrint's Meta (see server/codegen/ir/types.go).
	//
	// Português: prefix é texto livre impresso ANTES do valor ("temp 27").
	// format escolhe a variante por tipo — para int: "decimal" | "hex". Os
	// dois viajam como propriedades do node e chegam aos backends pelo Meta
	// do OpPrint (server/codegen/ir/types.go).
	prefix string
	format string
	label  string // editable name shown below ornament (defaults to id)
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
	// resizeLocked stays false: the Debug family resizes HORIZONTALLY so a
	// long prefix fits on the body. Height is immutable — the resize
	// handlers pin it and SetSize ignores it.
	// Português: resizeLocked fica false: a família Debug redimensiona na
	// HORIZONTAL para um prefixo longo caber no corpo. A altura é imutável
	// — os handlers a pinam e o SetSize a ignora.
	resizeLocked bool

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

func (e *StatementPrintInt) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementPrintInt) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementPrintInt) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementPrintInt) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementPrintInt) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementPrintInt) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementPrintInt) SetPrefix(v string) {
	e.prefix = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementPrintInt) GetPrefix() string { return e.prefix }
func (e *StatementPrintInt) SetFormat(v string) {
	e.format = v
}
func (e *StatementPrintInt) GetFormat() string              { return e.format }
func (e *StatementPrintInt) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementPrintInt) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementPrintInt) Remove() {
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
func (e *StatementPrintInt) renderSVG() string {
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
	ts := rulesDevice.TypeStyleFor("int64")

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect — border color = the Debug FAMILY color (burnt orange),
	// not the value type's. The family identity lives on the box (border +
	// DEBUG tag); the value type keeps its color where the wire meets the
	// device: the pin and the type label on the right.
	// Português: Borda na cor da FAMÍLIA Debug (laranja queimado), não na
	// do tipo. A identidade da família fica na caixa (borda + tag DEBUG);
	// o tipo mantém a cor onde o fio encontra o device: no pino e no label
	// de tipo à direita.
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		pin+bw/2, bw/2, w-pin-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, rulesDevice.KColorFamilyDebug, bw,
	)

	// Header — rounded top, flat bottom
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		pin+bw, bw, w-pin-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		pin+bw, bw+hh/2, w-pin-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// "SET" tag (left) + type label (right)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" font-weight="bold" dominant-baseline="middle">DEBUG</text>`,
		pin+bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, rulesDevice.KColorFamilyDebug,
	)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="end" dominant-baseline="middle">int</text>`,
		w-bw-6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		pin+bw, bw+hh, w-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Body text — the prefix (what the maker will see before each value) or
	// the family name when no prefix was typed yet.
	// Português: Texto do corpo — o prefixo (o que o maker verá antes de
	// cada valor) ou o nome da família quando ainda não há prefixo.
	displayName := e.prefix
	if displayName == "" {
		displayName = "print"
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

func (e *StatementPrintInt) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementPrintInt) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("printInt")
	e.resizeLocked = false
	// The format has a per-type default so a freshly placed device already
	// generates valid code without a visit to Inspect.
	// Português: O formato tem default por tipo para um device recém
	// colocado já gerar código válido sem passar pelo Inspect.
	if e.format == "" {
		e.format = "decimal"
	}
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
	// [RESIZE] horizontal-only floors: width can grow to fit long prefixes
	// and shrink back, never below the default; the height floor sits at
	// the fixed total and the resize handlers pin it there — floor + pin
	// together make the height immutable.
	// Português: Pisos do resize horizontal: a largura cresce para caber
	// prefixos longos e volta, nunca abaixo do padrão; o piso da altura
	// fica no total fixo e os handlers a pinam lá — piso + pino tornam a
	// altura imutável.
	e.elem.SetMinSizeD(rulesDevice.KConstDefaultWidth, totalH)
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		_ = e.elem.SetResizeButtons(adapter)
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}
	if e.ctxMenu == nil {
		log.Printf("[PrintInt] warning: no context menu set — menus disabled")
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

func (e *StatementPrintInt) wireEvents() {
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

	// [RESIZE] horizontal-only resize. Move re-pins the height on every
	// step so the live feedback never bulges vertically; End snaps the
	// width to the grid, re-renders, saves, and turns the handles off —
	// one resize per menu toggle, the Chart family's UX. The LEFT handle
	// moves the element origin, so the pin's WORLD position changes even
	// though its local x stays 0 — every step and the final snap must ask
	// the wire manager to recalculate (PositionFunc reads the element
	// live; only the polylines need refreshing).
	// Português: Resize só horizontal. Move re-pina a altura a cada passo
	// para o feedback ao vivo nunca inchar na vertical; End ajusta a
	// largura ao grid, re-renderiza, salva e apaga os handles — um resize
	// por toggle do menu, a UX da família Chart. O handle ESQUERDO move a
	// origem do element, então a posição de MUNDO do pino muda mesmo com
	// x local 0 — cada passo e o snap final precisam pedir o recálculo ao
	// wire manager (o PositionFunc lê o element ao vivo; só as polilinhas
	// precisam de refresh).
	fixedTotalH := e.height + rulesDevice.KLabelHeight
	e.elem.SetOnResizeStart(func(event sprite.ResizeEvent) {})
	e.elem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		wD, _ := e.elem.GetSizeD()
		e.elem.SetSizeD(wD, fixedTotalH)
		// [WIRE] live follow: the left handle moves the origin, so the
		// wire must be re-routed at every step.
		// Português: Fio segue ao vivo: o handle esquerdo move a origem,
		// então o fio precisa ser re-roteado a cada passo.
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
	})
	e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		wD, _ := e.elem.GetSizeD()
		newW, _ := e.gridAdjust.AdjustCenterD(wD, fixedTotalH)
		if newW < rulesDevice.KConstDefaultWidth {
			newW = rulesDevice.KConstDefaultWidth
		}
		e.elem.SetSizeD(newW, fixedTotalH)
		e.width = newW
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
		e.SetResizeEnable(false)
		e.SetDragEnable(true)
		go func() {
			e.recacheSVG()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
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
func (e *StatementPrintInt) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[PrintInt] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
		mainMenu.ResizeItem(func() {
			e.SetResizeEnable(!e.GetResizeEnable())
		}),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementPrintInt) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementPrintInt) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{Key: "prefix", Label: translate.T("propPrintPrefix", "Prefix"), Type: overlay.FieldText, Value: e.prefix, Placeholder: "temp"},
					{
						Key:   "format",
						Label: translate.T("propPrintFormat", "Format"),
						Type:  overlay.FieldSelect,
						Value: e.format,
						Options: []overlay.Option{
							{Value: "decimal", Label: "Decimal (42)"},
							{Value: "hex", Label: "Hexadecimal (0x2A)"},
						},
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
			{
				Label: "Code Preview", Type: overlay.TabMonaco,
				Content:  e.codePreview(),
				Language: "go", ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: printIntHelp()},
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
			if v, ok := values["prefix"]; ok {
				e.prefix = v
			}
			if v, ok := values["format"]; ok && v != "" {
				e.format = v
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

func (e *StatementPrintInt) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementPrintInt) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if v, ok := values["prefix"]; ok {
		e.prefix = v
	}
	if v, ok := values["format"]; ok && v != "" {
		e.format = v
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

func (e *StatementPrintInt) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "value"},
		IsOutput:           false,
		AllowedTypes:       []string{"int", "int64", "int*"},
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

func (e *StatementPrintInt) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementPrintInt) Get() *html.TagSvg { return nil }
func (e *StatementPrintInt) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}

// SetSize applies a saved size. The Debug family resizes HORIZONTALLY
// only, so the width is honoured (floored at the default) and the height
// parameter is deliberately ignored — the ornament height is fixed.
// Ignoring h also sidesteps the importer's outer-vs-ornament ambiguity
// (the saved outerBBox includes the label strip; see the size-restore
// comment in stageWorkspace's import).
// Português: Aplica um tamanho salvo. A família Debug redimensiona só na
// HORIZONTAL: a largura vale (piso = padrão) e a altura é ignorada de
// propósito — a altura do ornamento é fixa. Ignorar h também evita a
// ambiguidade outer-vs-ornamento do importador (o outerBBox salvo inclui
// a faixa do label).
func (e *StatementPrintInt) SetSize(w, _ rulesDensity.Density) {
	if w < rulesDevice.KConstDefaultWidth {
		w = rulesDevice.KConstDefaultWidth
	}
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
		go e.recacheSVG()
	}
}
func (e *StatementPrintInt) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementPrintInt) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementPrintInt) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementPrintInt) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementPrintInt) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementPrintInt) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementPrintInt) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementPrintInt) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementPrintInt) MoveBy(dx, dy float64) {
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

func (e *StatementPrintInt) GetInitialized() bool   { return e.initialized }
func (e *StatementPrintInt) GetID() string          { return e.id }
func (e *StatementPrintInt) GetName() string        { return e.name }
func (e *StatementPrintInt) GetSelected() bool      { return e.selected }
func (e *StatementPrintInt) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementPrintInt) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementPrintInt) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementPrintInt) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementPrintInt) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementPrintInt) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementPrintInt) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementPrintInt) GetStatus() int  { return e.iconStatus }
func (e *StatementPrintInt) SetStatus(s int) { e.iconStatus = s }
func (e *StatementPrintInt) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementPrintInt) SetSelected(sel bool) {
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

func (e *StatementPrintInt) SetDragEnable(en bool) {
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

// SetResizeEnable toggles the horizontal-resize handles. While resizing,
// dragging is suspended (handles and drag would fight for the same
// pointer); OnResizeEnd re-enables dragging and turns the handles off.
// Português: Liga/desliga os handles do resize horizontal. Durante o
// resize o drag fica suspenso (handles e drag disputariam o mesmo
// ponteiro); o OnResizeEnd religa o drag e apaga os handles.
func (e *StatementPrintInt) SetResizeEnable(enabled bool) {
	if e.elem == nil {
		e.pendingResizeEnable = &enabled
		return
	}
	if enabled {
		e.SetDragEnable(false)
		e.elem.SetResizeEnable(true)
		e.elem.ShowResizeButtons(true)
		return
	}
	e.elem.SetResizeEnable(false)
	e.elem.ShowResizeButtons(false)
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementPrintInt) GetIconName() string     { return "PrintInt" }
func (e *StatementPrintInt) GetIconCategory() string { return "Debug" }

func (e *StatementPrintInt) getIcon(data rulesIcon.Data) js.Value {
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

func (e *StatementPrintInt) GetDeviceType() string { return "StatementPrintInt" }
func (e *StatementPrintInt) GetProperties() map[string]interface{} {
	props := map[string]interface{}{"prefix": e.prefix, "format": e.format, "label": e.label}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// device's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do device.
func (e *StatementPrintInt) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementPrintInt) SetComment(c string) { e.comment = c }

// codePreview builds the Inspect preview: the exact Printf line this device
// will contribute, with the current prefix and format applied.
//
// Português: Monta o preview do Inspect: a linha Printf exata que este device
// vai contribuir, com prefixo e formato atuais aplicados.
func (e *StatementPrintInt) codePreview() string {
	verb := "%d"
	if e.format == "hex" {
		verb = "0x%X"
	}
	lead := ""
	if e.prefix != "" {
		lead = e.prefix + " "
	}
	return devices.CommentPrefix(e.comment) + fmt.Sprintf("// Print (int)\nfmt.Printf(%q, value)", lead+verb+"\n")
}
func (e *StatementPrintInt) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementPrintInt) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementPrintInt) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementPrintInt) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help text ─────────────────────────────────────────────────────────────────

func printIntHelp() string {
	return `# PrintInt — Print Integer

Prints the value wired into its input to **standard output**, once per
execution of its scope (every loop turn when placed inside a loop).

At code generation it becomes ` + "`fmt.Printf`" + ` in Go and ` + "`printf`" + ` in C99 —
this device ALWAYS lands in the generated program, unlike display widgets.

## Pointer wires

This device is the stage's **universal probe**: besides plain ` + "`int`" + `
wires it also accepts ` + "`int*`" + ` — the DASHED wires produced by
black-box functions that return or take a pointer to this type. The
generated code reads **through** the pointer automatically; when the
pointer is null, the program prints ` + "`null pointer`" + ` (with your
prefix) instead of crashing — a null is debug information, not an error.

## Properties

| Property | Type   | Description                                    |
|----------|--------|------------------------------------------------|
| Prefix   | string | Free text printed before the value (optional)  |
| Format   | select | Decimal (42) or Hexadecimal (0x2A)             |
| Label    | string | Name shown below the device                    |

## Input

| Port  | Type |
|-------|------|
| value | int  |

## Tips

- **Double-click** the device body to open Properties quickly.
- With prefix ` + "`temp`" + ` and a wired value of 27, the program prints ` + "`temp 27`" + `.
- Place it inside a **Loop** to watch a value change over time.
`
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementPrintInt) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementPrintInt) OpenInspect() { go e.showInspectOverlay() }
