// /ide/devices/compConsts/statementConstArrayString.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compConsts

// statementConstArrayString.go — Constant fixed-size STRING collection device.
//
// Visual design:
//
//	┌─────────────────────┐  ← border in the string family color (amber) —
//	│ []STR           ◉   │    the wire is the thick variant of it
//	├─────────────────────┤
//	│                     │
//	│  {"red", "gre…}     │  ← values joined for display, truncated
//	│                     │
//	└─────────────────────┘
//	constArrayStr1
//
// One of THREE sibling collection devices (Int / Float / String), mirroring
// how the scalar constants are separate devices. The element type here is
// FIXED to string.
//
// AUTHORING — ONE ELEMENT PER LINE (plan decision C): the Inspect field is a
// TEXTAREA and each line is one element. A comma is legitimate CONTENT
// inside a string ("hello, world"), so comma-separation would corrupt
// elements — newlines never are, which makes the line split unambiguous.
// The IR emitter applies the same rule (ir.emitConstArray splits string
// collections on newlines). Quotes are added by the code generator; the
// maker types bare text.
//
// The device holds a COMPILE-TIME literal collection (e.g.
// []string{"red", "green"}): the size is fixed at design time, so the
// generated code never allocates — Go emits a slice literal, C emits a
// fixed `const char*` array plus an explicit `_len` length companion (see
// ir.OpConstArray for the exact backend forms).
//
// THIN by design: the device only holds data (the multiline text exactly as
// typed) and advertises the output port. All parsing, formatting and
// validation live in the offline-tested codegen (ir.emitConstArray) — see
// docs/claude_const_array_plan.md.
//
// UNWIRED = IDE ERROR (plan decision 5): the output port registers with
// AcceptNotConnected: false, so stage validation flags a dangling collection
// BEFORE codegen — it never reaches the compiler as an unused variable.
//
// Body click:      Inspect · Delete
// Connector click: Connect (output-only)
// Double-click:    Inspect overlay

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"syscall/js"
	"time"
	"unicode/utf8"

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

// kConstArrayStringType is the FIXED bare element type of this device. The
// port advertises "[]"+kConstArrayStringType and GetProperties exports it as
// "elementType" for the IR emitter (ir.emitConstArray reads it).
const kConstArrayStringType = "string"

// StatementConstArrayString is a constant fixed-size string collection
// device. No inputs — single output connector typed "[]string".
type StatementConstArrayString struct {
	stage sprite.Stage
	elem  sprite.Element

	id   string
	name string

	// values holds the collection content EXACTLY as typed in the Inspect
	// TEXTAREA — multiline, one element per line. Stored raw on purpose:
	// the scene round-trip is then a plain string copy (the reload path
	// stringifies properties with %v), and the IR emitter applies the same
	// line-split rule and does the real parsing/validation/warnings.
	values string

	label string
	// [COMMENT] user comment — appears as `// ` lines above this device's
	// statement in the generated code, in the Code Preview, and in the
	// device's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste device no código gerado, no Code Preview e no
	// tooltip de hover do device.
	comment string

	width  rulesDensity.Density
	height rulesDensity.Density

	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
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

func (e *StatementConstArrayString) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementConstArrayString) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementConstArrayString) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementConstArrayString) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementConstArrayString) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementConstArrayString) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}

// SetValues replaces the collection content (the raw MULTILINE text — one
// element per line).
func (e *StatementConstArrayString) SetValues(v string) {
	e.values = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementConstArrayString) GetValues() string              { return e.values }
func (e *StatementConstArrayString) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// portType returns the collection type advertised on the output port —
// "[]string", the token convention of the whole feature (plan §6, matched
// by types.Classify identity and by the wire registry's thick slice styles).
func (e *StatementConstArrayString) portType() string { return "[]" + kConstArrayStringType }

// previewGoType: a concrete element type passes through the Go backend
// verbatim — strings have no widening.
func (e *StatementConstArrayString) previewGoType() string { return kConstArrayStringType }

// elementLines splits the multiline values text into its elements — one per
// line, trimmed, blanks skipped — the SAME rule the IR emitter applies
// (ir.emitConstArray, string separator). Used only for display/preview; the
// authoritative parse happens in the codegen.
func (e *StatementConstArrayString) elementLines() []string {
	var out []string
	for _, line := range strings.Split(e.values, "\n") {
		line = strings.TrimSpace(line) // also drops the \r of CRLF input
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementConstArrayString) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementConstArrayString) Remove() {
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

// truncatedValues returns the display string for the device body: the line
// elements joined as a quoted, comma-separated preview wrapped in braces —
// the initializer look of both target languages — truncated with "…" if it
// exceeds kArrayMaxDisplayRunes. The full text is always available in
// Inspect.
func (e *StatementConstArrayString) truncatedValues() string {
	quoted := make([]string, 0, 4)
	for _, line := range e.elementLines() {
		quoted = append(quoted, `"`+line+`"`)
	}
	v := strings.Join(quoted, ", ")
	if utf8.RuneCountInString(v) > kArrayMaxDisplayRunes {
		runes := []rune(v)
		v = string(runes[:kArrayMaxDisplayRunes]) + "…"
	}
	return "{" + v + "}"
}

func (e *StatementConstArrayString) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	// [PIN] the body is inset on the right by the pin length: the standard
	// connector pin lives in the freed margin, protruding from the border
	// with the wire anchored at its outer tip. The element size itself is
	// unchanged, so grid layout and saved scenes are unaffected.
	// Português: O corpo recua à direita o comprimento do pino: o pino
	// padrão vive na margem liberada, saindo da borda com o fio ancorado na
	// ponta externa. O tamanho do element não muda — grid e cenas salvas não
	// são afetados.
	pin := rulesConnection.PinBodyInset()
	bodyR := w - pin
	ts := rulesDevice.TypeStyleFor(e.portType())

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, bodyR-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, bodyR-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, bodyR-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// Type tag ([]INT — TypeStyleFor derives slice styles from the element
	// type, same color family as the scalar)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">%s</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color, ts.Tag,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, bodyR-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Values
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		bodyR/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, escapeXml(e.truncatedValues()),
	)

	// Output connector
	// [PIN] the standard connector pin at the body's right border, filled
	// with the type color so pin and wire read as one continuous piece; the
	// wire anchors at the pin's outer tip (see RegisterConnectors).
	// Português: O pino padrão na borda direita do corpo, preenchido com a
	// cor do tipo para pino e fio lerem como uma peça contínua; o fio ancora
	// na ponta externa do pino (ver RegisterConnectors).
	svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideRight, bodyR, h/2, ts.Color)

	// Label
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementConstArrayString) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementConstArrayString) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("constArrayStr")
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
		log.Printf("[ConstArrayString] warning: no context menu set — menus disabled")
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

func (e *StatementConstArrayString) wireEvents() {
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

		now := time.Now()
		if now.Sub(e.lastClick) < 300*time.Millisecond {
			e.lastClick = time.Time{}
			go e.showInspectOverlay()
			return
		}
		e.lastClick = now

		// [PIN] standard pin hit box — same edge point the renderer draws
		// and the wire anchors to, so click, drawing and wire agree.
		// Português: Caixa de clique do pino padrão — mesmo edge point que o
		// renderer desenha e onde o fio ancora; clique, desenho e fio
		// concordam.
		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2,
			event.LocalX, event.LocalY) {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "output"), menuX, menuY)
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
		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2, lx, ly) {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── Menu ──────────────────────────────────────────────────────────────────────

// bodyMenuItems returns the body context menu for this constant.
// Delete first, Inspect second — canonical order per decision D4.
//
// Português: Itens do menu de contexto do corpo. Delete primeiro,
// Inspect depois — ordem canônica conforme decisão D4.
func (e *StatementConstArrayString) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[ConstArrayString] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementConstArrayString) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementConstArrayString) inspectConfig() overlay.Config {
	// Preview: same split-and-quote the generator performs, so the maker
	// sees exactly what will be emitted (quotes are added automatically —
	// the textarea takes bare text, one element per line).
	quoted := make([]string, 0, 4)
	for _, line := range e.elementLines() {
		quoted = append(quoted, strconv.Quote(line))
	}
	previewValues := strings.Join(quoted, ", ")

	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:   "values",
						Label: translate.T("propValuesPerLine", "Values (one per line)"),
						// TEXTAREA — plan decision C: one element per line.
						// Enter adds a newline; Ctrl+Enter saves (overlay
						// behaviour). Commas inside a line are CONTENT.
						Type:        overlay.FieldTextarea,
						Value:       e.values,
						Placeholder: "red\ngreen\nblue",
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
				Label: "Code Preview",
				Type:  overlay.TabMonaco,
				// The preview mirrors the REAL generator output: each line
				// becomes one quoted element; C maps string to const char*
				// and adds the explicit `_len` companion.
				Content: devices.CommentPrefix(e.comment) + fmt.Sprintf(
					"// Generated code (Go):\n%s := []string{%s}\n\n// Generated code (C):\n// const char* %s[] = {%s};\n// const size_t %s_len = N;",
					e.id, previewValues,
					e.id, previewValues, e.id,
				),
				Language: "go",
				ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: constArrayStringHelp()},
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
			if v, ok := values["values"]; ok {
				// Stored exactly as typed (multiline) — the device is THIN:
				// the IR emitter splits the lines, quotes each element and
				// warns on an empty list. The Code Preview makes mistakes
				// visible here.
				e.values = v
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

func (e *StatementConstArrayString) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementConstArrayString) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if v, ok := values["values"]; ok {
		e.values = v
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

func (e *StatementConstArrayString) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:           wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput:     true,
		AllowedTypes: []string{e.portType()},
		// Plan decision 5 — UNWIRED = IDE ERROR: a dangling constant
		// collection is an authoring mistake the stage must flag BEFORE
		// codegen, so it never reaches the compiler as an unused variable.
		// The collection devices are the only compConsts with this stance:
		// scalar consts tolerate being parked unconnected, a collection
		// does not (its whole purpose is feeding a function parameter).
		//
		// Português: Decisão 5 do plano — coleção solta é erro de autoria
		// que a stage sinaliza ANTES do codegen.
		AcceptNotConnected: false,
		MaxConnections:     0,
		Label:              "Output",
		PositionFunc: func() (float64, float64) {
			// [PIN] the wire anchors at the OUTER TIP of the standard pin —
			// exactly the element's right edge, vertically centered on the
			// ornament.
			// Português: O fio ancora na PONTA EXTERNA do pino padrão —
			// exatamente a borda direita do element, centrado verticalmente
			// no ornamento.
			ex, ey := e.elem.GetPosition()
			w := e.elem.GetWidthD().GetFloat()
			ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideRight,
				w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2)
			return ex + ax, ey + ay
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementConstArrayString) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementConstArrayString) Get() *html.TagSvg { return nil }
func (e *StatementConstArrayString) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementConstArrayString) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstArrayString) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementConstArrayString) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementConstArrayString) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementConstArrayString) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementConstArrayString) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementConstArrayString) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementConstArrayString) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstArrayString) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstArrayString) MoveBy(dx, dy float64) {
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

func (e *StatementConstArrayString) GetInitialized() bool   { return e.initialized }
func (e *StatementConstArrayString) GetID() string          { return e.id }
func (e *StatementConstArrayString) GetName() string        { return e.name }
func (e *StatementConstArrayString) GetSelected() bool      { return e.selected }
func (e *StatementConstArrayString) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementConstArrayString) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementConstArrayString) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementConstArrayString) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementConstArrayString) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementConstArrayString) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementConstArrayString) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementConstArrayString) GetStatus() int  { return e.iconStatus }
func (e *StatementConstArrayString) SetStatus(s int) { e.iconStatus = s }
func (e *StatementConstArrayString) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementConstArrayString) SetSelected(sel bool) {
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

func (e *StatementConstArrayString) SetDragEnable(en bool) {
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

func (e *StatementConstArrayString) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementConstArrayString) GetIconName() string     { return "ConstArrayString" }
func (e *StatementConstArrayString) GetIconCategory() string { return "Constants" }

func (e *StatementConstArrayString) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text(`{"a"}`).Fill(data.ColorIcon).
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

func (e *StatementConstArrayString) GetDeviceType() string { return "StatementConstArrayString" }
func (e *StatementConstArrayString) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"elementType": kConstArrayStringType,
		"values":      e.values,
		"label":       e.label,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// device's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do device.
func (e *StatementConstArrayString) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementConstArrayString) SetComment(c string) { e.comment = c }
func (e *StatementConstArrayString) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementConstArrayString) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementConstArrayString) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementConstArrayString) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help ──────────────────────────────────────────────────────────────────────

func constArrayStringHelp() string {
	return `# ConstArrayString — Constant String Collection

Outputs a fixed **string collection literal** (e.g. ` + "`[]string{\"red\", \"green\"}`" + `)
whose size is known at design time — the generated code never allocates on
the heap, which makes it safe for embedded targets.

## Authoring — one element per line

Type **one element per line** in the Values box (Enter adds a line,
Ctrl+Enter saves). Quotes are added automatically by the code generator —
type bare text. A **comma is content**, not a separator:

` + "```" + `
hello, world
red
` + "```" + `

produces a TWO-element collection: ` + "`\"hello, world\"`" + ` and ` + "`\"red\"`" + `.
Blank lines are skipped.

## Generated code

| Language | Output                                                       |
|----------|--------------------------------------------------------------|
| Go       | ` + "`constArrayStr1 := []string{\"red\", \"green\"}`" + `           |
| C        | ` + "`const char* constArrayStr1[] = {\"red\", \"green\"};`" + ` + ` + "`const size_t constArrayStr1_len = 2;`" + ` |

The explicit **length companion** (` + "`_len`" + `) survives pointer decay when
the array is passed to a function taking ` + "`(const char**, size_t)`" + `.

## Properties

| Property | Type     | Description                          |
|----------|----------|--------------------------------------|
| Values   | textarea | One element per line                 |
| Label    | string   | Name shown below the device          |

## Output

| Port   | Type     |
|--------|----------|
| output | []string |

Collection wires are drawn **thicker** than scalar wires, in the string
color (amber). Sibling devices exist for **int** and **float** collections.

## Rules

- **The output must be connected.** A dangling collection is flagged as an
  error before code generation.
- An **empty Values box** generates an empty collection and a warning;
  fill it before exporting.

## Tips

- **Double-click** the device to open Properties.
- Wire the output into a function/black-box parameter that takes a string
  collection.
`
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementConstArrayString) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
