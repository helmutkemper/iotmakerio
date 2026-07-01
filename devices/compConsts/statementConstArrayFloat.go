// /ide/devices/compConsts/statementConstArrayFloat.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compConsts

// statementConstArrayFloat.go — Constant fixed-size FLOAT collection device.
//
// Visual design:
//
//	┌─────────────────────┐  ← border color matches precision (the float
//	│ []F64           ◉   │    family): []F32 green, []F64 teal — the wire
//	├─────────────────────┤    is the thick variant of it
//	│                     │
//	│   {0.5, 1.5}        │  ← values, truncated with "…" if long
//	│                     │
//	└─────────────────────┘
//	constArrayFloat1
//
// One of THREE sibling collection devices (Int / Float / String), mirroring
// how the scalar constants are separate devices. Like its scalar sibling
// StatementConstFloat, the PRECISION (float32 / float64) is selected in the
// Inspect overlay — changing it re-registers the connector type, so the port
// flips between "[]float32" and "[]float64".
//
// The device holds a COMPILE-TIME literal collection (e.g. []float32{0.5, 1.5}):
// the size is fixed at design time, so the generated code never touches the
// heap — Go emits a slice literal, C emits a fixed array plus an explicit
// `_len` length companion (see ir.OpConstArray for the exact backend forms).
//
// THIN by design: the device only holds data (the values text exactly as
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
	"strings"
	"syscall/js"
	"time"
	"unicode/utf8"

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

// StatementConstArrayFloat is a constant fixed-size float collection device.
// No inputs — single output connector typed "[]float32" or "[]float64",
// following the precision selected in Inspect (reuses the package-level
// FloatPrecision type shared with the scalar StatementConstFloat).
type StatementConstArrayFloat struct {
	stage sprite.Stage
	elem  sprite.Element

	id   string
	name string

	// precision selects the element type: float32 or float64. Default
	// Float64 (full precision) — same default as the scalar sibling.
	// Changing it in Inspect re-registers the connector with the new
	// collection type.
	precision FloatPrecision

	// values holds the collection content EXACTLY as typed in the Inspect
	// text field ("0.5, 1.5"). Stored raw on purpose: the scene round-trip
	// is then a plain string copy (the reload path stringifies properties
	// with %v), and the IR emitter already accepts this comma-separated
	// shape and does the real parsing/validation/warnings.
	values string

	label string

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
	onRemove    func(id string)
}

// ── Dependency injection ──────────────────────────────────────────────────────

func (e *StatementConstArrayFloat) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementConstArrayFloat) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementConstArrayFloat) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementConstArrayFloat) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementConstArrayFloat) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementConstArrayFloat) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}

// SetValues replaces the collection content (the raw text, e.g. "0.5, 1.5").
func (e *StatementConstArrayFloat) SetValues(v string) {
	e.values = v
	if e.initialized {
		go e.recacheSVG()
	}
}
func (e *StatementConstArrayFloat) GetValues() string              { return e.values }
func (e *StatementConstArrayFloat) SetPrecision(p FloatPrecision)  { e.precision = p }
func (e *StatementConstArrayFloat) GetPrecision() FloatPrecision   { return e.precision }
func (e *StatementConstArrayFloat) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// portType returns the collection type advertised on the output port —
// "[]float32" or "[]float64" per the precision select (plan §6 token
// convention, matched by types.Classify identity and by the wire registry's
// thick slice styles).
func (e *StatementConstArrayFloat) portType() string { return "[]" + string(e.precision) }

// previewGoType mirrors the Go backend for the Code Preview tab: a CONCRETE
// float type passes through goTypeName verbatim — unlike the abstract int of
// the sibling device, the maker's precision is honoured exactly (same stance
// as the scalar StatementConstFloat).
func (e *StatementConstArrayFloat) previewGoType() string { return string(e.precision) }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementConstArrayFloat) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementConstArrayFloat) Remove() {
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

// truncatedValues returns the display string for the device body: the values
// text wrapped in braces — the initializer syntax of BOTH target languages
// (Go `[]int{1, 2, 3}`, C `{1, 2, 3}`) — truncated with "…" if it exceeds
// kArrayMaxDisplayRunes. The full text is always available in Inspect.
func (e *StatementConstArrayFloat) truncatedValues() string {
	v := strings.TrimSpace(e.values)
	if utf8.RuneCountInString(v) > kArrayMaxDisplayRunes {
		runes := []rune(v)
		v = string(runes[:kArrayMaxDisplayRunes]) + "…"
	}
	return "{" + v + "}"
}

func (e *StatementConstArrayFloat) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h + float64(rulesDevice.KLabelHeight)

	bw := rulesDevice.KDeviceBorderWidth
	rx := rulesDevice.KDeviceCornerRadius
	ts := rulesDevice.TypeStyleFor(e.portType())

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))

	// Outer rect
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, h-bw, rx, rx,
		rulesDevice.KColorDeviceBg, ts.Color, bw,
	)

	// Header
	hh := rulesDevice.KDeviceHeaderHeight
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" ry="%.1f" fill="%s"/>`,
		bw, bw, w-2*bw, hh, rx, rx, rulesDevice.KColorDeviceHeader)
	svg += fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		bw, bw+hh/2, w-2*bw, hh/2, rulesDevice.KColorDeviceHeader)

	// Type tag ([]INT — TypeStyleFor derives slice styles from the element
	// type, same color family as the scalar)
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" dominant-baseline="middle">%s</text>`,
		bw+6, bw+hh/2+float64(rulesDevice.KDeviceFontSizeTypeTag)/2,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeTypeTag, ts.Color, ts.Tag,
	)

	// Divider
	svg += fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="0.5"/>`,
		bw, bw+hh, w-bw, bw+hh, rulesDevice.KColorDeviceDivider)

	// Values
	bodyTop := bw + hh
	bodyCY := bodyTop + (h-bw-hh)/2
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="%s" font-size="%d" fill="%s" text-anchor="middle" dominant-baseline="central" font-weight="bold">%s</text>`,
		w/2, bodyCY,
		rulesDevice.KDeviceFontFamily, rulesDevice.KDeviceFontSizeValue,
		rulesDevice.KColorDeviceText, escapeXml(e.truncatedValues()),
	)

	// Output connector
	svg += fmt.Sprintf(
		`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="%s" stroke-width="1"/>`,
		w-rulesDevice.KConnectorOffsetRight, h/2,
		rulesDevice.KConnectorRadius, ts.Color, rulesDevice.KColorConnectorStroke,
	)

	// Label
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	svg += fmt.Sprintf(rulesDevice.KDeviceLabel, h+3, escapeXml(displayLabel))

	svg += `</svg>`
	return svg
}

func (e *StatementConstArrayFloat) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementConstArrayFloat) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("constArrayFloat")
	e.resizeLocked = true
	if e.precision == "" {
		e.precision = Float64 // default to full precision, like the scalar sibling
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
	minH := rulesDensity.Density(rulesDevice.KConstMinHeight) + rulesDevice.KLabelHeight
	e.elem.SetMinSizeD(rulesDevice.KConstMinWidth, minH)
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		_ = e.elem.SetResizeButtons(adapter)
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}
	if e.ctxMenu == nil {
		log.Printf("[ConstArrayFloat] warning: no context menu set — menus disabled")
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

func (e *StatementConstArrayFloat) wireEvents() {
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

		w, _ := e.elem.GetSize()
		dx := event.LocalX - (w - rulesDevice.KConnectorOffsetRight)
		dy := event.LocalY - e.height.GetFloat()/2
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "output"), menuX, menuY)
			return
		}

		go e.ctxMenu.OpenAtWorld(e.bodyMenuItems(), menuX, menuY)
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
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
		w, _ := e.elem.GetSize()
		dx := lx - (w - rulesDevice.KConnectorOffsetRight)
		dy := ly - e.height.GetFloat()/2
		if dx*dx+dy*dy <= rulesDevice.KConnectorHitRadius*rulesDevice.KConnectorHitRadius {
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
func (e *StatementConstArrayFloat) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[ConstArrayFloat] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementConstArrayFloat) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementConstArrayFloat) inspectConfig() overlay.Config {
	previewValues := strings.TrimSpace(e.values)
	precValue := string(e.precision)
	cElem := "float"
	cSuffix := "f"
	if e.precision == Float64 {
		cElem = "double"
		cSuffix = ""
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
						Key:         "values",
						Label:       translate.T("propValues", "Values"),
						Type:        overlay.FieldText,
						Value:       e.values,
						Placeholder: "0.5, 1.5, 3.25",
					},
					{
						Key:   "precision",
						Label: translate.T("propPrecision", "Precision"),
						Type:  overlay.FieldSelect,
						Value: precValue,
						Options: []overlay.Option{
							{Value: "float32", Label: "float32 — 32-bit (RP2040 friendly)"},
							{Value: "float64", Label: "float64 — 64-bit (full precision)"},
						},
					},
					{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
					{Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id, ReadOnly: true},
				},
			},
			{
				Label: "Code Preview",
				Type:  overlay.TabMonaco,
				// The preview mirrors the REAL generator output: a concrete
				// float precision passes through verbatim (Go) and maps to
				// float/double with per-element suffixing (C); the `_len`
				// companion survives pointer decay at call sites.
				Content: fmt.Sprintf(
					"// Generated code (Go):\n%s := []%s{%s}\n\n// Generated code (C):\n// %s %s[] = {…%s};\n// const size_t %s_len = N;",
					e.id, e.previewGoType(), previewValues,
					cElem, e.id, cSuffix, e.id,
				),
				Language: "go",
				ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: constArrayFloatHelp()},
		},
		OnSave: func(values map[string]string) {
			if v, ok := values["values"]; ok {
				// Stored exactly as typed — the device is THIN: the IR
				// emitter parses, formats and warns (empty list, garbage
				// tokens). The Code Preview makes mistakes visible here.
				e.values = v
			}
			if p, ok := values["precision"]; ok {
				newPrec := FloatPrecision(p)
				if newPrec != e.precision {
					e.precision = newPrec
					// Re-register the connector with the updated collection
					// type — mirrors the scalar StatementConstFloat's
					// precision change. Existing wires of the other float
					// width will break, by design (exact-match typing).
					if e.wireMgr != nil {
						e.wireMgr.UnregisterElement(e.id)
						e.RegisterConnectors()
					}
				}
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

func (e *StatementConstArrayFloat) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementConstArrayFloat) ApplyProperties(values map[string]string) {
	if v, ok := values["values"]; ok {
		e.values = v
	}
	// Remember the precision the output connector currently reflects (the
	// factory registered it at creation with the default float64) so a real
	// change below can be detected and the connector refreshed.
	prevPrecision := e.precision
	if p, ok := values["elementType"]; ok && (p == "float32" || p == "float64") {
		// Scene reload path: the exported "elementType" property carries the
		// precision token — accept it back. (Inspect saves use "precision";
		// both spellings land on the same field.)
		e.precision = FloatPrecision(p)
	}
	if p, ok := values["precision"]; ok && (p == "float32" || p == "float64") {
		e.precision = FloatPrecision(p)
	}
	if lbl, ok := values["label"]; ok {
		e.label = lbl
	}

	// Re-register the output connector when the reload changed the precision.
	//
	// The connector advertises "[]"+precision (portType). The factory
	// registers it at creation with the DEFAULT precision (float64); the scene
	// reload above only updated e.precision and, without this, never refreshed
	// the connector — so a float32 collection kept advertising []float64. The
	// importer's wire pass then dropped the saved wire as type-incompatible and
	// the port could no longer be re-wired (StartConnect finds zero compatible
	// targets and silently cancels). The live Inspect-save path (OnSave) already
	// re-registers on precision change; ApplyProperties is its reload twin and
	// must do the same.
	//
	// This MUST stay synchronous — NOT inside the recacheSVG goroutine below.
	// The importer wires devices immediately after applying properties
	// (stageWorkspace import: properties, a short settle sleep, then the wire
	// pass), so a deferred re-registration would land after the wires are
	// reconnected and the wire would still be dropped. Both e.wireMgr and
	// e.elem are already set at this point (the factory sets the manager and
	// Init creates the element before its RegisterConnectors call), so
	// RegisterConnectors is not a no-op here.
	//
	// Português: Re-registra o connector de saída quando o reload mudou a
	// precisão. O connector é criado em float64 (default) e o reload só ajusta
	// e.precision; sem isto, uma coleção float32 continua anunciando []float64
	// e o importador descarta o fio por tipo incompatível (e a porta não pode
	// mais ser religada). Tem de ser SÍNCRONO (não no goroutine do recacheSVG):
	// o importador liga os fios logo após aplicar as properties.
	if e.precision != prevPrecision && e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
		e.RegisterConnectors()
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

func (e *StatementConstArrayFloat) RegisterConnectors() {
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
			ex, ey := e.elem.GetPosition()
			w := e.elem.GetWidthD().GetFloat()
			return ex + w - rulesDevice.KConnectorOffsetRight, ey + e.height.GetFloat()/2
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementConstArrayFloat) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementConstArrayFloat) Get() *html.TagSvg { return nil }
func (e *StatementConstArrayFloat) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementConstArrayFloat) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstArrayFloat) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementConstArrayFloat) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementConstArrayFloat) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementConstArrayFloat) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementConstArrayFloat) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementConstArrayFloat) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementConstArrayFloat) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstArrayFloat) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h+rulesDevice.KLabelHeight)
	}
}
func (e *StatementConstArrayFloat) MoveBy(dx, dy float64) {
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

func (e *StatementConstArrayFloat) GetInitialized() bool   { return e.initialized }
func (e *StatementConstArrayFloat) GetID() string          { return e.id }
func (e *StatementConstArrayFloat) GetName() string        { return e.name }
func (e *StatementConstArrayFloat) GetSelected() bool      { return e.selected }
func (e *StatementConstArrayFloat) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementConstArrayFloat) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementConstArrayFloat) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementConstArrayFloat) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementConstArrayFloat) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementConstArrayFloat) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementConstArrayFloat) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementConstArrayFloat) GetStatus() int  { return e.iconStatus }
func (e *StatementConstArrayFloat) SetStatus(s int) { e.iconStatus = s }
func (e *StatementConstArrayFloat) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementConstArrayFloat) SetSelected(sel bool) {
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

func (e *StatementConstArrayFloat) SetDragEnable(en bool) {
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

func (e *StatementConstArrayFloat) SetResizeEnable(_ bool) {
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementConstArrayFloat) GetIconName() string     { return "ConstArrayFloat" }
func (e *StatementConstArrayFloat) GetIconCategory() string { return "Constants" }

func (e *StatementConstArrayFloat) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 4).
		Text("{.5}").Fill(data.ColorIcon).
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

func (e *StatementConstArrayFloat) GetDeviceType() string { return "StatementConstArrayFloat" }
func (e *StatementConstArrayFloat) GetProperties() map[string]interface{} {
	return map[string]interface{}{
		// elementType carries the PRECISION token ("float32"/"float64") —
		// the IR emitter reads it to type the CONST_ARRAY instruction, and
		// the scene reload path feeds it back through ApplyProperties.
		"elementType": string(e.precision),
		"values":      e.values,
		"label":       e.label,
	}
}
func (e *StatementConstArrayFloat) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementConstArrayFloat) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementConstArrayFloat) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementConstArrayFloat) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help ──────────────────────────────────────────────────────────────────────

func constArrayFloatHelp() string {
	return `# ConstArrayFloat — Constant Float Collection

Outputs a fixed **floating-point collection literal** (e.g. ` + "`[]float32{0.5, 1.5}`" + `)
whose size is known at design time — the generated code never allocates on
the heap, which makes it safe for embedded targets.

## Precision

| Setting | Element type | Notes                                     |
|---------|--------------|-------------------------------------------|
| float32 | float32      | 32-bit — efficient on RP2040 hardware FPU |
| float64 | float64      | 64-bit — default, full precision          |

Changing precision **re-registers the connector type** (` + "`[]float32`" + ` ↔
` + "`[]float64`" + `), so existing wires will break if the connected device
expects the other width.

## Generated code

| Language | Output                                                  |
|----------|---------------------------------------------------------|
| Go       | ` + "`constArrayFloat1 := []float32{0.5, 1.5}`" + `              |
| C        | ` + "`float constArrayFloat1[] = {0.5f, 1.5f};`" + ` + ` + "`const size_t constArrayFloat1_len = 2;`" + ` |

float64 maps to C ` + "`double`" + ` with plain decimal literals (no suffix). The
explicit **length companion** (` + "`_len`" + `) survives pointer decay when the
array is passed to a function taking ` + "`(const T*, size_t)`" + `.

## Properties

| Property  | Type   | Description                                       |
|-----------|--------|---------------------------------------------------|
| Values    | text   | Comma-separated numbers, e.g. ` + "`0.5, 1.5, 3.25`" + ` |
| Precision | select | float32 or float64                                |
| Label     | string | Name shown below the device                       |

## Output

| Port   | Type                     |
|--------|--------------------------|
| output | []float32 or []float64   |

Collection wires are drawn **thicker** than scalar wires, in the float
family color. Sibling devices exist for **int** and **string** collections.

## Rules

- **The output must be connected.** A dangling collection is flagged as an
  error before code generation.
- An **empty Values field** generates an empty collection and a warning;
  fill it before exporting.
- Use **float32** when targeting RP2040 to avoid slow emulated float64.

## Tips

- **Double-click** the device to open Properties.
- Wire the output into a function/black-box parameter that takes a float
  collection.
`
}
