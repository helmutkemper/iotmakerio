// devices/compBlackBox/statementBlackBoxInit.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compBlackBox

// statementBlackBoxInit.go — Generic visual device for black-box Init().
//
// English:
//
//	A single struct that works for ANY black-box device. Configured at
//	runtime from a BlackBoxDefClient fetched from the server. Ports are
//	dynamic: input connectors from Init() params, output connectors from
//	Init() returns. Properties from `prop` tags become Inspect panel fields.
//
//	SVG is generated programmatically — no ornament drawing needed.
//	The device renders as a dark rounded rectangle with an icon + label header
//	above the port rows, matching the layout used in the SPA preview.
//
//	Header layout (bbHeaderH pixels total):
//	  Row 1 — FontAwesome icon (bbIconSize × bbIconSize), centred horizontally.
//	  Row 2 — "{StructLabel} {InitLabel}" text, centred horizontally.
//
//	Port vertical position formula:
//	  cy = bbHeaderH + i*bbPortRowH + bbPortRowH/2
//	The "+bbPortRowH/4" offset was removed because it caused the last port
//	to clip through the bottom border when maxPorts*bbPortRowH == bbMinBodyH.
//
//	Icon rendering:
//	  Icons support three formats via rulesIcon.ParseIconValue():
//	    - Name  (e.g. "gear")      → SVG <path> from the registry, scaled inline.
//	    - Hex   (e.g. "f287")      → FA Solid glyph via <text> + webfont.
//	    - Brands (e.g. "f287:b")   → FA Brands glyph via <text> + webfont.
//
// Português:
//
//	Um único struct que funciona para QUALQUER device black-box. Configurado
//	em runtime a partir de um BlackBoxDefClient obtido do servidor. Portas
//	são dinâmicas. SVG gerado programaticamente.
//
//	Cabeçalho: ícone FontAwesome + texto "{StructLabel} {InitLabel}".
//	Ícone renderizado como <path> SVG inline com escala calculada do viewBox.

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/helmutkemper/iotmakerio/blackbox"
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
	"github.com/helmutkemper/iotmakerio/wire"
)

// Layout constants for black-box SVG rendering.
// All values in pixels. Centralised here so visual tweaks require a single edit.
const (
	bbWidth    = 160.0 // device width in pixels
	bbHeaderH  = 44.0  // header height: icon row (≈22px) + label row (≈22px)
	bbPortRowH = 22.0  // height allocated per port row
	bbMinBodyH = 44.0  // minimum body height (guarantees 2 rows minimum)
	bbConnR    = 5.0   // connector circle radius
	bbConnLeft = 8.0   // input connector X position (from left edge)
	bbFontSize = 10    // port label font size (pt)
	bbHeaderFS = 10    // header text font size (pt)
	bbIconSize = 16.0  // icon bounding box size in pixels
	bbIconCY   = 14.0  // vertical centre of the icon area within the header
	bbLabelY   = 37.0  // baseline Y of the label text within the header

	// bbPortPad is extra padding added to bodyH so the last port circle
	// never touches the border. Equal to one circle radius + stroke width.
	bbPortPad = bbConnR + 2.0
)

// portCY returns the vertical centre of port i within the device body.
// Uses bbPortRowH/2 so ports are centred in their row and never overflow
// the border regardless of port count.
func portCY(i int) float64 {
	return bbHeaderH + float64(i)*bbPortRowH + bbPortRowH/2
}

// StatementBlackBoxInit is a generic device for any black-box Init().
type StatementBlackBoxInit struct {
	stage sprite.Stage
	elem  sprite.Element

	name         string
	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool
	width        rulesDensity.Density
	height       rulesDensity.Density

	pendingDragEnable *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller.
	ctxMenu    *contextMenu.Controller
	wireMgr    *wire.Manager
	gridAdjust grid.Adjust

	id    string
	label string

	// Black-box definition (from server)
	def        *blackbox.BlackBoxDefClient
	instanceId string            // shared with Run device
	propValues map[string]string // current property values

	// Scene
	sceneNotify func()
	onRemove    func(id string)

	// Double-click
	lastClickTime time.Time

	// executionOrderOverride is the per-instance "Execution order" set by the
	// maker in the Inspect panel. nil means "use the Init default"
	// (executionOrder:N on Init in the source). A non-nil value (including 0 =
	// unordered) overrides it for this block only. Precedence: wire >
	// executionOrder.
	//
	// Português: Override por instância da "Execution order" definido no
	// Inspect. nil = usar o padrão do Init. Valor não-nil (inclusive 0)
	// sobrepõe só para este bloco.
	executionOrderOverride *int
}

// =====================================================================
//  Setters (called by factory before Init)
// =====================================================================

func (e *StatementBlackBoxInit) SetStage(stage sprite.Stage)      { e.stage = stage }
func (e *StatementBlackBoxInit) SetWireManager(mgr *wire.Manager) { e.wireMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementBlackBoxInit) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementBlackBoxInit) SetResizerButton(b block.ResizeButton) { e.resizerButton = b }
func (e *StatementBlackBoxInit) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }
func (e *StatementBlackBoxInit) SetOnRemove(fn func(id string))        { e.onRemove = fn }

func (e *StatementBlackBoxInit) SetDef(def *blackbox.BlackBoxDefClient) { e.def = def }
func (e *StatementBlackBoxInit) SetInstanceId(id string)                { e.instanceId = id }
func (e *StatementBlackBoxInit) GetInstanceId() string                  { return e.instanceId }
func (e *StatementBlackBoxInit) GetLabel() string                       { return e.label }
func (e *StatementBlackBoxInit) SetLabel(l string) {
	e.label = l
	go e.recacheOrnament()
}
func (e *StatementBlackBoxInit) GetID() string { return e.id }

// =====================================================================
//  Lifecycle
// =====================================================================

func (e *StatementBlackBoxInit) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementBlackBoxInit) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	if e.elem != nil {
		e.elem.SetVisible(false)
		elem := e.elem
		go func() {
			time.Sleep(50 * time.Millisecond)
			elem.Destroy()
		}()
		e.elem = nil
	}
}

func (e *StatementBlackBoxInit) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}

func (e *StatementBlackBoxInit) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}

func (e *StatementBlackBoxInit) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}

func (e *StatementBlackBoxInit) SetDragEnable(enabled bool) {
	e.dragEnabled = enabled
	if e.elem == nil {
		e.pendingDragEnable = &enabled
		return
	}
	e.elem.SetDragEnable(enabled)
}

// =====================================================================
//  Init
// =====================================================================

func (e *StatementBlackBoxInit) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("SetStage() must be called before Init()")
	}
	if e.def == nil || e.def.Init == nil {
		return fmt.Errorf("SetDef() must be called with a def that has Init()")
	}

	baseName := strings.ToLower(e.def.Name) + "Init"
	e.SetName(baseName)
	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id

	if e.instanceId == "" {
		e.instanceId = rulesSequentialId.GetIdFromBase(strings.ToLower(e.def.Name))
	}

	// Initialise property values with their defaults.
	e.propValues = make(map[string]string)
	for _, p := range e.def.Props {
		e.propValues[p.FieldName] = p.Default
	}

	// Calculate dimensions.
	// Extra bbPortPad prevents the last connector circle from clipping the border.
	inputCount := len(e.def.Init.Inputs)
	outputCount := len(e.def.Init.Outputs)
	maxPorts := inputCount
	if outputCount > maxPorts {
		maxPorts = outputCount
	}
	bodyH := float64(maxPorts)*bbPortRowH + bbPortPad
	if bodyH < bbMinBodyH {
		bodyH = bbMinBodyH
	}
	ornH := bbHeaderH + bodyH
	e.width = rulesDensity.Density(bbWidth)
	e.height = rulesDensity.Density(ornH)
	totalHeight := e.height + rulesDevice.KLabelHeight

	e.resizeLocked = true

	svgXml := e.renderSVG(e.width.GetFloat(), ornH)
	svgXml = e.injectLabel(svgXml, e.height)

	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:         e.id,
		X:          0,
		Y:          0,
		Width:      e.width.GetFloat(),
		Height:     totalHeight.GetFloat(),
		Index:      rulesZIndex.Math,
		DragEnable: false,
		SvgXml:     svgXml,
	})
	if err != nil {
		return fmt.Errorf("create element: %w", err)
	}

	e.elem.SetMinSizeD(e.width, totalHeight)
	e.wireEvents()

	e.initialized = true

	if e.pendingDragEnable != nil {
		e.SetDragEnable(*e.pendingDragEnable)
		e.pendingDragEnable = nil
	}

	return nil
}

func (e *StatementBlackBoxInit) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}

// =====================================================================
//  SVG Rendering
// =====================================================================

// renderSVG builds the complete SVG markup for this device block.
//
// Header layout:
//
//	┌─────────────────────────────────┐  ← bbHeaderH tall
//	│          [FA icon 16×16]        │  ← centred at (w/2, bbIconCY)
//	│      StructLabel + InitLabel    │  ← text baseline at bbLabelY
//	├─────────────────────────────────┤  ← divider at bbHeaderH
//	│ ● input   …                     │  ← port rows
//	│                output ●         │
//	└─────────────────────────────────┘
func (e *StatementBlackBoxInit) renderSVG(w, h float64) string {
	bw := 2.0
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(h+float64(rulesDevice.KLabelHeight)),
	)

	// ── Background rectangle ──────────────────────────────────────────────
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="6" ry="6" fill="#1e2838" stroke="#5588AA" stroke-width="%.1f"/>`,
		bw/2, bw/2, w-bw, h-bw, bw,
	)

	// ── Header bar ───────────────────────────────────────────────────────
	// Two rects: a rounded-top rect that covers the top corners, and a
	// flat-bottom fill to square off the header/body boundary.
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="6" ry="6" fill="#2a3848"/>`,
		bw, bw, w-2*bw, bbHeaderH-bw,
	)
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#2a3848"/>`,
		bw, bbHeaderH-8, w-2*bw, 8.0,
	)

	// ── Icon ─────────────────────────────────────────────────────────────
	iconName := e.def.Init.EffectiveIcon(e.def)
	svg += renderFAIconSVG(iconName, w/2, bbIconCY, bbIconSize, "#88BBDD")

	// ── Label text ────────────────────────────────────────────────────────
	// Format: "{StructLabel} {InitLabel}" e.g. "APDS9960 setup"
	headerText := e.def.EffectiveStructLabel() + " " + e.def.Init.EffectiveLabel()
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="Arial,sans-serif" font-size="%d" fill="#88BBDD" font-weight="bold" text-anchor="middle">%s</text>`,
		w/2, bbLabelY, bbHeaderFS, escapeXml(headerText),
	)

	// ── Divider ───────────────────────────────────────────────────────────
	svg += fmt.Sprintf(
		`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#3a4858" stroke-width="0.5"/>`,
		bw, bbHeaderH, w-bw, bbHeaderH,
	)

	// ── Input ports (left side) ───────────────────────────────────────────
	for i, port := range e.def.Init.Inputs {
		cy := portCY(i)
		color := portColor(port.GoType, port.IsError)
		svg += fmt.Sprintf(
			`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="#FFFFFF" stroke-width="1"/>`,
			bbConnLeft, cy, bbConnR, color,
		)
		svg += fmt.Sprintf(
			`<text x="%.1f" y="%.1f" font-family="Arial,sans-serif" font-size="%d" fill="#AABBCC" dominant-baseline="central">%s</text>`,
			bbConnLeft+bbConnR+4, cy, bbFontSize, escapeXml(port.Name),
		)
	}

	// ── Output ports (right side) ─────────────────────────────────────────
	connRight := w - bbConnLeft
	for i, port := range e.def.Init.Outputs {
		cy := portCY(i)
		color := portColor(port.GoType, port.IsError)
		svg += fmt.Sprintf(
			`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="#FFFFFF" stroke-width="1"/>`,
			connRight, cy, bbConnR, color,
		)
		svg += fmt.Sprintf(
			`<text x="%.1f" y="%.1f" font-family="Arial,sans-serif" font-size="%d" fill="#AABBCC" dominant-baseline="central" text-anchor="end">%s</text>`,
			connRight-bbConnR-4, cy, bbFontSize, escapeXml(port.Name),
		)
	}

	svg += `</svg>`
	return svg
}

func (e *StatementBlackBoxInit) injectLabel(svgXml string, ornH rulesDensity.Density) string {
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	// Append the effective Execution order as a tiebreaker hint, e.g.
	// "myInit_1 (order 2)". Precedence is wire > executionOrder, so this is the
	// tiebreaker value, not a guaranteed position. Omitted when unordered
	// (effective 0).
	if eff := e.effectiveExecutionOrder(); eff > 0 {
		displayLabel += fmt.Sprintf(" (order %d)", eff)
	}
	displayLabel = escapeXml(displayLabel)
	labelY := ornH.GetFloat() + 3
	labelSvg := fmt.Sprintf(rulesDevice.KDeviceLabel, labelY, displayLabel)
	return strings.Replace(svgXml, "</svg>", labelSvg+"</svg>", 1)
}

func (e *StatementBlackBoxInit) recacheOrnament() {
	if e.elem == nil {
		return
	}
	wD := e.width.GetFloat()
	ornH := e.height.GetFloat()
	svgXml := e.renderSVG(wD, ornH)
	svgXml = e.injectLabel(svgXml, e.height)
	_ = e.elem.CacheFromSvg(svgXml)
}

// =====================================================================
//  Wire Events (click → menu)
// =====================================================================

func (e *StatementBlackBoxInit) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		w, _ := e.elem.GetSize()
		elemX, elemY := e.elem.GetPosition()
		clickWX := elemX + event.LocalX
		clickWY := elemY + event.LocalY

		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}

		// Hit-test input connectors
		for i, port := range e.def.Init.Inputs {
			cy := portCY(i)
			dx := event.LocalX - bbConnLeft
			dy := event.LocalY - cy
			if dx*dx+dy*dy <= (bbConnR+4)*(bbConnR+4) {
				items := mainMenu.ConnectorMenu(e.wireMgr, e.id, port.Name)
				go e.ctxMenu.OpenAtWorld(items, clickWX, clickWY)
				return
			}
		}

		// Hit-test output connectors
		connRight := w - bbConnLeft
		for i, port := range e.def.Init.Outputs {
			cy := portCY(i)
			dx := event.LocalX - connRight
			dy := event.LocalY - cy
			if dx*dx+dy*dy <= (bbConnR+4)*(bbConnR+4) {
				items := mainMenu.ConnectorMenu(e.wireMgr, e.id, port.Name)
				go e.ctxMenu.OpenAtWorld(items, clickWX, clickWY)
				return
			}
		}

		// Body click → device menu
		go e.ctxMenu.OpenAtWorld(e.bodyMenuItems(), clickWX, clickWY)
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
		for i := range e.def.Init.Inputs {
			cy := portCY(i)
			dx := lx - bbConnLeft
			dy := ly - cy
			if dx*dx+dy*dy <= (bbConnR+4)*(bbConnR+4) {
				return sprite.CursorPointer
			}
		}
		connRight := w - bbConnLeft
		for i := range e.def.Init.Outputs {
			cy := portCY(i)
			dx := lx - connRight
			dy := ly - cy
			if dx*dx+dy*dy <= (bbConnR+4)*(bbConnR+4) {
				return sprite.CursorPointer
			}
		}
		return ""
	})
}

// bodyMenuItems returns body context menu items.
// Delete first (canonical per D4), Inspect second.
//
// Português: Itens do menu de contexto do corpo. Ordem D4.
func (e *StatementBlackBoxInit) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[BB-Init] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			go e.showInspectOverlay()
		}),
	}
}

// =====================================================================
//  Connector Registration
// =====================================================================

func (e *StatementBlackBoxInit) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}

	// Input ports (left side)
	for i, port := range e.def.Init.Inputs {
		pp := port
		idx := i
		e.wireMgr.RegisterConnector(wire.ConnectorInfo{
			ID:                 wire.ConnectorID{ElementID: e.id, PortName: pp.Name},
			IsOutput:           false,
			AllowedTypes:       []string{pp.GoType},
			AcceptNotConnected: pp.IsError,
			Locked:             false,
			MaxConnections:     1,
			Label:              pp.Name,
			PositionFunc: func() (float64, float64) {
				ex, ey := e.elem.GetPosition()
				return ex + bbConnLeft, ey + portCY(idx)
			},
		})
	}

	// Output ports (right side)
	for i, port := range e.def.Init.Outputs {
		pp := port
		idx := i
		e.wireMgr.RegisterConnector(wire.ConnectorInfo{
			ID:                 wire.ConnectorID{ElementID: e.id, PortName: pp.Name},
			IsOutput:           true,
			AllowedTypes:       []string{pp.GoType},
			AcceptNotConnected: true,
			Locked:             false,
			MaxConnections:     0, // unlimited outputs
			Label:              pp.Name,
			PositionFunc: func() (float64, float64) {
				ex, ey := e.elem.GetPosition()
				w, _ := e.elem.GetSize()
				return ex + w - bbConnLeft, ey + portCY(idx)
			},
		})
	}
}

// =====================================================================
//  Inspect Overlay
// =====================================================================

func (e *StatementBlackBoxInit) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementBlackBoxInit) GetInspectConfig() interface{} {
	// ── Editable fields (maker-facing) ────────────────────────────────────
	// Only Label and prop fields appear here. ID and instanceId are system
	// identifiers the maker has no reason to edit or see in the Properties
	// tab — they are internal codegen state. Removing them reduces noise
	// and leaves more space for the fields that actually matter.
	// Execution order field: pre-filled with the effective value (per-instance
	// override, else the Init default) so it shows the order in force, e.g.
	// "2". An empty field means "no specific order" (unordered); the
	// placeholder states that.
	eoValue := ""
	if eff := e.effectiveExecutionOrder(); eff > 0 {
		eoValue = strconv.Itoa(eff)
	}

	fields := []overlay.Field{
		{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
		{
			Key:         "executionOrder",
			Label:       translate.T("propExecutionOrder", "Execution order"),
			Type:        overlay.FieldNumber,
			Min:         "0",
			Value:       eoValue,
			Placeholder: translate.T("propExecutionOrderUnordered", "no specific order"),
		},
	}

	// Configurable prop fields — these are the only truly editable entries.
	for _, p := range e.def.Props {
		val := e.propValues[p.FieldName]
		if val == "" {
			val = p.Default
		}

		field := overlay.Field{
			Key:   "prop_" + p.FieldName,
			Label: p.Label,
			Value: val,
		}

		// When this prop is linked to a diagram element via connection: tag,
		// accent its border with a fallback colour. The actual colour from
		// the SVG palette will be resolved reactively after the SVG loads.
		if p.Connection != "" {
			field.ConnectionColor = overlay.ConnectionRoleFallbackColor()
			field.ConnectionRole = p.Connection
		}

		if len(p.Options) > 0 {
			field.Type = overlay.FieldSelect
			for _, opt := range p.Options {
				field.Options = append(field.Options, overlay.Option{Value: opt, Label: opt})
			}
		} else if p.Container == "map" {
			// Slice 2.2 — map[K]V props render as a row builder.
			// The renderer reads p.KeyType and p.ValueType to pick
			// per-column inputs. The renderer also handles the
			// "unsupported (KeyType, ValueType)" case by showing
			// an inert JSON preview.
			//
			// p.Default is conventionally a JSON literal like
			// `{"k":"v"}`. We pass it straight through; an empty
			// or invalid default falls back to "{}" inside
			// buildMapField.
			field.Type = overlay.FieldMap
			field.KeyType = p.KeyType
			field.ValueType = p.ValueType
		} else if p.Container == "slice" {
			// Slice 2.4 — []T props render as a row builder with
			// ↑/↓ reorder buttons. KeyType is unused (slices have
			// no key column). p.Default is conventionally a JSON
			// array literal like `[1,2,3]` or `["a","b"]`.
			field.Type = overlay.FieldSlice
			field.ValueType = p.ValueType
		} else {
			field.Type = overlay.FieldText
		}

		fields = append(fields, field)
	}

	// ── Help cards ───────────────────────────────────────────────────────
	// Build help cards BEFORE the tab list so we can scan for the embedded
	// control panel placeholder and decide whether to create a separate
	// Properties tab or embed the form inside the Help tab.
	//
	// Language resolution: session preference → browser locale → "en".
	lang := helpSessionLang()
	mdTabs := e.def.HelpTabsFor("init", lang)
	var helpCards []overlay.HelpCard

	for _, t := range mdTabs {
		helpCards = append(helpCards, overlay.HelpCard{
			Name:     t.Title,
			Language: lang,
			Content:  t.Content,
		})
	}

	// Legacy /* */ manual pages — only for components that predate the
	// GitHub markdown system and still have ManualPages populated.
	if len(mdTabs) == 0 {
		for _, p := range e.def.PagesFor("init") {
			helpCards = append(helpCards, overlay.HelpCard{
				Name:     p.Name,
				Language: p.Language,
				Content:  p.Content,
			})
		}
	}

	// [go doc] card — always last, always present.
	initDoc := ""
	if e.def.Init != nil {
		initDoc = e.def.Init.Doc
	}
	if md := buildGodocMarkdown(e.def.Name, e.def.Doc, "Init", initDoc); md != "" {
		helpCards = append(helpCards, overlay.HelpCard{
			Name:     "source doc",
			Language: "en",
			Content:  md,
		})
	}

	// ── Tab list ──────────────────────────────────────────────────────────
	// Check if any help card contains the embedded control panel placeholder.
	// When detected, the Properties tab is omitted and the form fields are
	// embedded inline inside the help markdown at the placeholder position.
	// This gives the specialist full control over the maker's experience:
	// documentation and configuration appear together as a single guided flow.
	hasEmbeddedPanel := false
	for _, hc := range helpCards {
		if strings.Contains(hc.Content, overlay.PlaceholderMarker) {
			hasEmbeddedPanel = true
			break
		}
	}

	var tabs []overlay.Tab
	if !hasEmbeddedPanel {
		// Normal mode: separate Properties tab with form fields.
		tabs = append(tabs, overlay.Tab{
			Label:  translate.T("tabProperties", "Properties"),
			Type:   overlay.TabForm,
			Fields: fields,
		})
	}

	// Build diagram activation data for interactive SVGs embedded in help markdown.
	// When a prop with a connection: tag references a diagram element (e.g. GP4),
	// the markdown renderer will highlight that element on any interactive SVG
	// image (e.g. ![](rp2040.svg)) found in the help content. The highlight
	// colour is resolved at render time from the SVG's data-palette attribute.
	var diagramProps []overlay.DiagramProp
	if e.def.Interactive != "" {
		for _, p := range e.def.Props {
			if p.Connection == "" {
				continue
			}
			val := e.propValues[p.FieldName]
			if val == "" {
				val = p.Default
			}
			if val == "" {
				continue
			}
			diagramProps = append(diagramProps, overlay.DiagramProp{
				ID:    val,
				Role:  p.Connection,
				Label: overlay.ConnectionRoleLabel(p.Connection),
				Color: "", // resolved from SVG palette at render time
			})
		}
	}

	if len(helpCards) > 0 {
		helpTab := overlay.Tab{
			Label:        translate.T("tabHelp", "Help"),
			Type:         overlay.TabHelpDeck,
			HelpCards:    helpCards,
			DiagramURL:   e.def.Interactive, // SVG URL for inline activation
			DiagramProps: diagramProps,      // elements to highlight in markdown SVGs
		}
		// In embedded mode, pass the form fields to the Help tab so
		// renderHelpDeck can inject them at the PlaceholderMarker position.
		// EmbeddedOnSave is set later by buildTabs() using cfg.OnSave
		// (which is already wrapped with the close+reopen+scroll logic).
		if hasEmbeddedPanel {
			helpTab.EmbeddedFields = fields
			helpTab.EmbeddedHeader = translate.T("tabProperties", "Properties")
		}
		tabs = append(tabs, helpTab)
	}

	return overlay.Config{
		Title: fmt.Sprintf("%s Init — %s", e.def.Name, e.id),
		Width: "540px",
		Tabs:  tabs,
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
		// After Save, close the overlay and reopen it with updated values.
		// This is how the interactive SVG in the Help tab reflects prop
		// changes: the panel rebuilds with new DiagramProps computed from
		// the saved prop values, and the SVG activates the new connections.
		OnSaveReopen: func() {
			e.showInspectOverlay()
		},
	}
}

func (e *StatementBlackBoxInit) ApplyProperties(values map[string]string) {
	changed := false

	if v, ok := values["label"]; ok && v != "" && v != e.label {
		e.label = v
		changed = true
	}

	for _, p := range e.def.Props {
		key := "prop_" + p.FieldName
		if v, ok := values[key]; ok && v != e.propValues[p.FieldName] {
			e.propValues[p.FieldName] = v
			changed = true
			log.Printf("[BB-Init] %s: %s = %q", e.id, p.FieldName, v)
		}
	}

	// Execution order. On import the values carry the round-trip marker
	// "executionOrderOverride" (authoritative; the sibling "executionOrder" is
	// the effective value and is ignored to keep the default fresh). On a form
	// save there is no marker (nor "instanceId"); the editable field
	// "executionOrder" carries the value, where empty = unordered. Negatives are
	// clamped to 0.
	if raw, ok := values["executionOrderOverride"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			if n < 0 {
				n = 0
			}
			if e.executionOrderOverride == nil || *e.executionOrderOverride != n {
				v := n
				e.executionOrderOverride = &v
				changed = true
			}
		}
	} else if _, isImport := values["instanceId"]; !isImport {
		if raw, ok := values["executionOrder"]; ok {
			raw = strings.TrimSpace(raw)
			switch {
			case raw == "":
				if e.executionOrderOverride == nil || *e.executionOrderOverride != 0 {
					v := 0
					e.executionOrderOverride = &v
					changed = true
				}
			default:
				if n, err := strconv.Atoi(raw); err == nil {
					if n < 0 {
						n = 0
					}
					if e.executionOrderOverride == nil || *e.executionOrderOverride != n {
						v := n
						e.executionOrderOverride = &v
						changed = true
					}
				}
			}
		}
	}

	if changed {
		go func() {
			time.Sleep(200 * time.Millisecond)
			e.recacheOrnament()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		}()
	}
}

// =====================================================================
//  Scene Export
// =====================================================================

func (e *StatementBlackBoxInit) GetDeviceType() string {
	return "BlackBoxInit:" + e.def.Name
}

func (e *StatementBlackBoxInit) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementBlackBoxInit) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}

func (e *StatementBlackBoxInit) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementBlackBoxInit) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementBlackBoxInit) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// effectiveExecutionOrder returns the Execution order in force for this Init
// block: the per-instance override when the maker set one in Inspect, otherwise
// the Init default carried by the parsed definition (executionOrder:N on Init).
// It feeds both the scene export (codegen) and the "(order N)" block label.
//
// Português: Retorna a Execution order vigente para este Init — o override por
// instância (se definido no Inspect) ou o padrão do Init.
func (e *StatementBlackBoxInit) effectiveExecutionOrder() int {
	if e.executionOrderOverride != nil {
		return *e.executionOrderOverride
	}
	if e.def != nil && e.def.Init != nil {
		return e.def.Init.ExecutionOrder
	}
	return 0
}

func (e *StatementBlackBoxInit) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"instanceId": e.instanceId,
	}

	// executionOrder carries the EFFECTIVE value (per-instance override, else
	// the Init default) so the codegen server can feed it to the topological
	// sort. 0 (unordered) is omitted — the server treats absent as 0.
	if eff := e.effectiveExecutionOrder(); eff > 0 {
		props["executionOrder"] = eff
	}

	// executionOrderOverride is the round-trip marker for the per-instance value
	// set in Inspect. Written ONLY when an override exists, and even when 0 so an
	// explicit "unordered" survives a reload. ApplyProperties reads it back;
	// absence means "use the Init default", keeping the default fresh.
	if e.executionOrderOverride != nil {
		props["executionOrderOverride"] = *e.executionOrderOverride
	}

	if len(e.propValues) > 0 {
		propsCopy := make(map[string]interface{})
		for k, v := range e.propValues {
			propsCopy[k] = v
		}
		props["props"] = propsCopy
	}
	return props
}

// =====================================================================
//  Helpers (shared with statementBlackBoxRun via same package)
// =====================================================================

// renderFAIconSVG returns SVG markup that draws a FontAwesome icon centred at
// (cx, cy) with the given bounding box size.
//
// All three `icon:` formats are supported:
//
//	"greater-than-equal"  → path from the registry (always works)
//	"f287"                → path from faIconByCodepoint (requires generator)
//	"f287:b"              → path from faIconByCodepoint (brands style)
//
// When a unicode codepoint is given but faIconsGenerated.go has not been
// generated yet, the codepoint resolves to the gear icon as a visible
// placeholder. After running the generator, the correct icon appears.
//
// The function never returns an empty string — a fallback is always provided.
func renderFAIconSVG(iconName string, cx, cy, size float64, fillColor string) string {
	iv := rulesIcon.ParseIconValue(iconName)

	// Resolve the icon — IconDefForValue handles both name and unicode lookup,
	// including the faIconByCodepoint table populated by the generator.
	def, _ := rulesIcon.IconDefForValue(iv)

	vbW, vbH := parseViewBox(def.ViewBox)
	if vbW <= 0 || vbH <= 0 {
		return ""
	}
	// Uniform scale: fit the longest dimension into size.
	scale := size / vbW
	if h := size / vbH; h < scale {
		scale = h
	}
	// Translate so the scaled icon is centred at (cx, cy).
	scaledW := vbW * scale
	scaledH := vbH * scale
	tx := cx - scaledW/2
	ty := cy - scaledH/2
	return fmt.Sprintf(
		`<path transform="translate(%.3f,%.3f) scale(%.6f)" d="%s" fill="%s"/>`,
		tx, ty, scale, def.Path, fillColor,
	)
}

// parseViewBox extracts the width and height from a "0 0 W H" viewBox string.
// Returns (0, 0) when the format is unexpected so the caller can skip rendering.
func parseViewBox(viewBox string) (w, h float64) {
	parts := strings.Fields(viewBox)
	if len(parts) != 4 {
		return 0, 0
	}
	fmt.Sscanf(parts[2], "%f", &w)
	fmt.Sscanf(parts[3], "%f", &h)
	return w, h
}

// portColor returns the connector circle fill colour based on Go type.
func portColor(goType string, isError bool) string {
	if isError {
		return "#CC4444" // red for error ports
	}
	switch {
	case strings.HasPrefix(goType, "*"):
		return "#44AA88" // teal for pointer/bus types
	case goType == "bool":
		return "#AA44AA" // purple for bool
	case goType == "uint16" || goType == "uint8" || goType == "int" ||
		goType == "int64" || goType == "uint32" || goType == "float64":
		return "#4488CC" // blue for numeric
	case goType == "string":
		return "#CCAA44" // yellow for string
	default:
		return "#4488CC" // blue default
	}
}

// escapeXml replaces XML special characters to prevent SVG injection.
func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
