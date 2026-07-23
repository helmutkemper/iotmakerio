// /ide/devices/compConsts/statementConstDuration.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compConsts

// statementConstDuration.go — Duration constant device (time.Duration).
//
// Visual design:
//
//	┌─────────────────────────────┐  ← 2px border, 5px corner radius, cyan (#00CCCC)
//	│ DUR                     ◉   │  ← 18px header; ◉ = output connector (cyan)
//	├─────────────────────────────┤  ← divider
//	│                             │
//	│         5 Second            │  ← value, 16px bold, centered
//	│                             │
//	└─────────────────────────────┘
//	constDuration1                  ← editable label, 12px muted (#8899AA)
//
// Internal storage:
//   value int64 — nanoseconds (single source of truth)
//   unit  string — last selected unit in dropdown (UI hint for Inspect panel)
//
// The dropdown offers: Nanosecond, Microsecond, Millisecond, Second, Minute, Hour.
// On Save, the selected amount × unit multiplier produces the nanos value.
// On load (Inspect open), nanos ÷ unit multiplier recovers the display amount.
//
// Body click:      Inspect · Delete
// Connector click: Connect (output-only)
// Double-click:    Inspect overlay

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

// ── Duration unit definitions ────────────────────────────────────────────────
// Maps unit name → nanosecond multiplier, mirroring Go's time package constants.

type durationUnit struct {
	Label string // display label (e.g. "Second")
	Nanos int64  // nanoseconds per unit
}

// durationUnits defines the available units in the Inspect dropdown, ordered
// from smallest to largest. This ordering matches the Go time package.
var durationUnits = []durationUnit{
	{"Nanosecond", int64(time.Nanosecond)},
	{"Microsecond", int64(time.Microsecond)},
	{"Millisecond", int64(time.Millisecond)},
	{"Second", int64(time.Second)},
	{"Minute", int64(time.Minute)},
	{"Hour", int64(time.Hour)},
}

// unitNanos returns the nanosecond multiplier for a unit name.
// Falls back to Nanosecond if the name is unknown.
func unitNanos(name string) int64 {
	for _, u := range durationUnits {
		if u.Label == name {
			return u.Nanos
		}
	}
	return 1 // fallback: Nanosecond
}

// decomposeNanos converts a raw nanosecond value back into (amount, unit) for
// display. Picks the largest unit that divides evenly. If nothing divides
// evenly, uses Nanosecond.
func decomposeNanos(nanos int64) (amount int64, unit string) {
	// Walk backwards (largest first)
	for i := len(durationUnits) - 1; i >= 0; i-- {
		u := durationUnits[i]
		if nanos != 0 && nanos%u.Nanos == 0 {
			return nanos / u.Nanos, u.Label
		}
	}
	return nanos, "Nanosecond"
}

// formatDuration returns a human-readable string for the SVG display.
// E.g. "5 Second", "100 ms", "1 Hour".
func formatDuration(nanos int64) string {
	amount, unit := decomposeNanos(nanos)
	// Shorten common units for compact display
	short := map[string]string{
		"Nanosecond":  "ns",
		"Microsecond": "µs",
		"Millisecond": "ms",
		"Second":      "s",
		"Minute":      "min",
		"Hour":        "h",
	}
	if s, ok := short[unit]; ok {
		return fmt.Sprintf("%d %s", amount, s)
	}
	return fmt.Sprintf("%d %s", amount, unit)
}

// ── StatementConstDuration ──────────────────────────────────────────────────

// StatementConstDuration is a duration constant device.
// No inputs — single output connector that emits time.Duration (int64 nanos).
type StatementConstDuration struct {
	stage sprite.Stage
	elem  sprite.Element

	id    string
	name  string
	value int64  // nanoseconds — single source of truth
	unit  string // last selected unit in dropdown (UI hint)
	label string // editable name shown below ornament (defaults to id)
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

func (e *StatementConstDuration) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementConstDuration) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementConstDuration) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementConstDuration) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementConstDuration) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementConstDuration) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementConstDuration) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// SetValue sets the duration in nanoseconds.
func (e *StatementConstDuration) SetValue(nanos int64) {
	e.value = nanos
	if e.initialized {
		go e.recacheSVG()
	}
}

// GetValue returns the duration in nanoseconds.
func (e *StatementConstDuration) GetValue() int64 { return e.value }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementConstDuration) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementConstDuration) Remove() {
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

func (e *StatementConstDuration) renderSVG() string {
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
	ts := rulesDevice.TypeStyleFor("time.Duration")

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect — border color = type color (cyan)
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, bodyR-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header — rounded top, flat bottom
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, bodyR-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, bodyR-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// Type tag
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">%s</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color, ts.Tag,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, bodyR-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Value — human-readable duration
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	displayText := formatDuration(e.value)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		bodyR/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, escapeXml(displayText),
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

func (e *StatementConstDuration) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementConstDuration) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("constDuration")
	e.resizeLocked = true
	if e.width == 0 {
		e.width = rulesDevice.KConstDurationDefaultWidth
	}
	if e.height == 0 {
		e.height = rulesDevice.KConstDefaultHeight
	}
	// Default value: 1 Second (most common use case for beginners)
	if e.value == 0 {
		e.value = int64(time.Second)
		e.unit = "Second"
	}
	if e.unit == "" {
		_, e.unit = decomposeNanos(e.value)
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
		log.Printf("[ConstDuration] warning: no context menu set — menus disabled")
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

func (e *StatementConstDuration) wireEvents() {
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
// Delete first, Inspect second — canonical order per decision D4,
// fixing the original inversion present on every compConsts device.
//
// Português: Itens do menu de contexto do corpo. Delete primeiro,
// Inspect depois — ordem canônica conforme decisão D4.
func (e *StatementConstDuration) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[ConstDuration] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementConstDuration) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementConstDuration) inspectConfig() overlay.Config {
	// Decompose current nanos into amount + unit for the form fields.
	currentAmount, currentUnit := decomposeNanos(e.value)

	// Build unit dropdown options.
	unitOptions := make([]overlay.Option, len(durationUnits))
	for i, u := range durationUnits {
		unitOptions[i] = overlay.Option{Value: u.Label, Label: u.Label}
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
						Key:         "amount",
						Label:       translate.T("propAmount", "Amount"),
						Type:        overlay.FieldNumber,
						Value:       strconv.FormatInt(currentAmount, 10),
						Placeholder: "1",
						Min:         "0",
					},
					{
						Key:     "unit",
						Label:   translate.T("propUnit", "Unit"),
						Type:    overlay.FieldSelect,
						Value:   currentUnit,
						Options: unitOptions,
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
				Label:    "Code Preview",
				Type:     overlay.TabMonaco,
				Content:  devices.CommentPrefix(e.comment) + fmt.Sprintf("// Generated code:\n%s := time.Duration(%d) // %s", e.id, e.value, formatDuration(e.value)),
				Language: "go",
				ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: constDurationHelp()},
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
			var amount int64 = 1
			unit := "Second"
			if v, ok := values["amount"]; ok {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
					amount = n
				}
			}
			if u, ok := values["unit"]; ok {
				unit = u
			}
			e.value = amount * unitNanos(unit)
			e.unit = unit
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

func (e *StatementConstDuration) GetInspectConfig() interface{} { return e.inspectConfig() }

func (e *StatementConstDuration) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if v, ok := values["value"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			e.value = n
		}
	}
	if u, ok := values["unit"]; ok {
		e.unit = u
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

func (e *StatementConstDuration) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "output"},
		IsOutput:           true,
		AllowedTypes:       []string{"time.Duration"},
		AcceptNotConnected: true,
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

func (e *StatementConstDuration) SetName(n string)     { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementConstDuration) Get() *html.TagSvg    { return nil }
func (e *StatementConstDuration) SetFatherId(_ string) {}
func (e *StatementConstDuration) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementConstDuration) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstDuration) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementConstDuration) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementConstDuration) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementConstDuration) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementConstDuration) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementConstDuration) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementConstDuration) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstDuration) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstDuration) MoveBy(dx, dy float64) {
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

func (e *StatementConstDuration) GetInitialized() bool   { return e.initialized }
func (e *StatementConstDuration) GetID() string          { return e.id }
func (e *StatementConstDuration) GetName() string        { return e.name }
func (e *StatementConstDuration) GetSelected() bool      { return e.selected }
func (e *StatementConstDuration) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementConstDuration) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementConstDuration) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementConstDuration) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementConstDuration) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementConstDuration) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementConstDuration) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementConstDuration) GetStatus() int    { return e.iconStatus }
func (e *StatementConstDuration) SetStatus(s int)   { e.iconStatus = s }
func (e *StatementConstDuration) SetWarning(_ bool) {}
func (e *StatementConstDuration) GetWarning() bool  { return false }

func (e *StatementConstDuration) SetSelected(sel bool) {
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

func (e *StatementConstDuration) SetDragEnable(en bool) {
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

func (e *StatementConstDuration) SetResizeEnable(_ bool) {
	// Constant devices never resize.
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementConstDuration) GetIconName() string     { return "ConstDuration" }
func (e *StatementConstDuration) GetIconCategory() string { return "Constants" }

func (e *StatementConstDuration) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	// Hourglass icon text as symbol in the hex menu icon
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("⏳").Fill(data.ColorIcon).
		X((rulesIcon.Width / 2).GetInt() - 10).Y((rulesIcon.Height / 2).GetInt() + 6)
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

func (e *StatementConstDuration) GetDeviceType() string { return "StatementConstDuration" }
func (e *StatementConstDuration) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"value": e.value,
		"unit":  e.unit,
		"label": e.label,
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
func (e *StatementConstDuration) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementConstDuration) SetComment(c string) { e.comment = c }
func (e *StatementConstDuration) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementConstDuration) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementConstDuration) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementConstDuration) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// RefreshVisual regenerates the SVG and recalculates wire positions.
func (e *StatementConstDuration) RefreshVisual() {
	e.recacheSVG()
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// ── Help text ─────────────────────────────────────────────────────────────────

func constDurationHelp() string {
	return `# ConstDuration — Duration Constant

Outputs a fixed **time.Duration** value to any connected device.

## Properties

| Property | Type   | Description                           |
|----------|--------|---------------------------------------|
| Amount   | int64  | Numeric value (e.g. 5)                |
| Unit     | select | Time unit: Nanosecond → Hour          |
| Label    | string | Name shown below the device           |

## Output

| Port   | Type          |
|--------|---------------|
| output | time.Duration |

## Duration Units

| Unit         | Nanoseconds       |
|-------------|--------------------|
| Nanosecond  | 1                  |
| Microsecond | 1,000              |
| Millisecond | 1,000,000          |
| Second      | 1,000,000,000      |
| Minute      | 60,000,000,000     |
| Hour        | 3,600,000,000,000  |

## Tips

- **Double-click** the device body to open Properties quickly.
- Connect to a **LoopDuration** interval input to set the loop cadence.
- The internal value is stored in nanoseconds (single source of truth).
- The display shows a human-readable form (e.g. "5 s", "100 ms").
`
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementConstDuration) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// OpenInspect opens this device's inspect overlay — the double-click
// contract (P1, Kemper 2026-07-23): the factory wires every element's
// double-click to this method. Português: Abre o inspect deste device
// — o contrato do duplo-clique, ligado pela factory em todo elemento.
func (e *StatementConstDuration) OpenInspect() { go e.showInspectOverlay() }
