// devices/compBlackBox/statementBlackBoxRun.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compBlackBox

// statementBlackBoxRun.go — Generic visual device for any non-Init black-box method.
//
// English:
//
//	StatementBlackBoxMethod is the single generic device for ALL non-Init
//	methods on a black-box struct (Run, Log, Step, Read, Write, …).
//
//	It replaces the old StatementBlackBoxRun. The key difference is that the
//	device is configured with the specific MethodDefClient at construction time
//	via SetMethod(). This lets the factory create one block per method without
//	any code duplication.
//
//	The scene device type string follows the pattern:
//	  "BlackBox{MethodName}:{StructName}"
//	  e.g. "BlackBoxRun:APDS9960", "BlackBoxLog:APDS9960"
//
//	This pattern is backward-compatible: existing scenes with "BlackBoxRun:X"
//	load correctly because the factory matches any "BlackBox*:X" prefix.
//
//	The type alias StatementBlackBoxRun = StatementBlackBoxMethod is kept
//	so any external code that references the old name still compiles.
//
// Português:
//
//	StatementBlackBoxMethod é o device genérico para TODOS os métodos não-Init
//	de um struct black-box. Substitui o antigo StatementBlackBoxRun.
//	O device é configurado com o MethodDefClient específico via SetMethod().
//	A string de tipo na cena segue o padrão "BlackBox{Método}:{Struct}".

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesDevice"
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

// StatementBlackBoxMethod is a generic device for any non-Init black-box method.
// One instance is created per method per component placement.
type StatementBlackBoxMethod struct {
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
	// [COMMENT] user comment — appears as `// ` lines above this device's
	// statement in the generated code and in the device's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste device no código gerado e no tooltip de hover.
	comment string

	// def is the full component definition. method is the specific method
	// this device represents (Run, Log, Step, …).
	def    *blackbox.BlackBoxDefClient
	method *blackbox.MethodDefClient // pointer into def.Methods

	instanceId string // shared with Init device and sibling method devices

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

	lastClickTime time.Time

	// executionOrderOverride is the per-instance "Execution order" set by the
	// maker in the Inspect panel. nil means "use the function/method default"
	// (the executionOrder:N directive in the source). A non-nil value
	// (including 0 = unordered) overrides that default for this block only.
	// Ordering precedence remains wire > executionOrder.
	//
	// Português: Override por instância da "Execution order" definido no
	// Inspect. nil = usar o padrão da diretiva (executionOrder:N). Valor
	// não-nil (inclusive 0 = sem ordem) sobrepõe o padrão só para este bloco.
	executionOrderOverride *int

	// callbackRefFn, when non-empty, marks this block as the C99 callback
	// REFERENCE variant (the "ƒ" device) of the named function. It changes ONLY
	// GetDeviceType (→ "CallbackRef:<fn>"); every other behaviour is driven by
	// the synthetic method the factory supplies (no inputs, one `callback`
	// output of the handler type). Empty for the normal callable function/method
	// block, which is therefore unaffected. Codegen treats a "CallbackRef:<fn>"
	// node as a reference — skips its call and passes the function name by
	// address. See the duality section of docs/CODEGEN_C99_CALLBACKS.md.
	callbackRefFn string
}

// StatementBlackBoxRun is a type alias kept for backward compatibility.
// All new code should use StatementBlackBoxMethod directly.
//
// Português: Alias de tipo mantido para compatibilidade. Novo código deve
// usar StatementBlackBoxMethod diretamente.
type StatementBlackBoxRun = StatementBlackBoxMethod

// =====================================================================
//  Setters
// =====================================================================

func (e *StatementBlackBoxMethod) SetStage(stage sprite.Stage)      { e.stage = stage }
func (e *StatementBlackBoxMethod) SetWireManager(mgr *wire.Manager) { e.wireMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementBlackBoxMethod) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementBlackBoxMethod) SetResizerButton(b block.ResizeButton) { e.resizerButton = b }
func (e *StatementBlackBoxMethod) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }
func (e *StatementBlackBoxMethod) SetOnRemove(fn func(id string))        { e.onRemove = fn }
func (e *StatementBlackBoxMethod) SetDef(def *blackbox.BlackBoxDefClient) {
	e.def = def
}

// SetMethod sets the specific method this device represents.
// Must be called after SetDef() and before Init().
//
// Português: Define o método específico que este device representa.
// Deve ser chamado após SetDef() e antes de Init().
func (e *StatementBlackBoxMethod) SetMethod(m *blackbox.MethodDefClient) { e.method = m }
func (e *StatementBlackBoxMethod) SetInstanceId(id string)               { e.instanceId = id }
func (e *StatementBlackBoxMethod) GetInstanceId() string                 { return e.instanceId }
func (e *StatementBlackBoxMethod) GetLabel() string                      { return e.label }
func (e *StatementBlackBoxMethod) SetLabel(l string) {
	e.label = l
	go e.recacheOrnament()
}
func (e *StatementBlackBoxMethod) GetID() string { return e.id }

// SetCallbackRef marks this block as the C99 callback REFERENCE variant of fn —
// it changes ONLY GetDeviceType (→ "CallbackRef:<fn>"). Empty fn = normal block.
func (e *StatementBlackBoxMethod) SetCallbackRef(fn string) { e.callbackRefFn = fn }

// effectiveExecutionOrder returns the Execution order in force for this block:
// the per-instance override when the maker set one in Inspect, otherwise the
// function/method default carried by the parsed definition (executionOrder:N).
// It feeds both the scene export (codegen) and the "(order N)" block label.
//
// Português: Retorna a Execution order vigente para este bloco — o override
// por instância (se definido no Inspect) ou o padrão da definição.
func (e *StatementBlackBoxMethod) effectiveExecutionOrder() int {
	if e.executionOrderOverride != nil {
		return *e.executionOrderOverride
	}
	if e.method != nil {
		return e.method.ExecutionOrder
	}
	return 0
}

// =====================================================================
//  Lifecycle
// =====================================================================

func (e *StatementBlackBoxMethod) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementBlackBoxMethod) Remove() {
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

func (e *StatementBlackBoxMethod) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}

func (e *StatementBlackBoxMethod) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}

func (e *StatementBlackBoxMethod) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}

func (e *StatementBlackBoxMethod) SetDragEnable(enabled bool) {
	e.dragEnabled = enabled
	if e.elem == nil {
		e.pendingDragEnable = &enabled
		return
	}
	e.elem.SetDragEnable(enabled)
}

// =====================================================================
//  Init (device lifecycle — not the black-box Init method)
// =====================================================================

func (e *StatementBlackBoxMethod) Init() (err error) {
	if e.stage == nil {
		return fmt.Errorf("SetStage() must be called before Init()")
	}
	if e.def == nil || e.method == nil {
		return fmt.Errorf("SetDef() and SetMethod() must be called before Init()")
	}

	// Base name: e.g. "apds9960Run", "apds9960Log"
	baseName := strings.ToLower(e.def.Name) + e.method.Name
	e.SetName(baseName)
	e.id = rulesSequentialId.GetIdFromBase(e.name)
	e.label = e.id

	if e.instanceId == "" {
		e.instanceId = rulesSequentialId.GetIdFromBase(strings.ToLower(e.def.Name))
	}

	inputCount := len(e.method.Inputs)
	outputCount := len(e.method.Outputs)
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

func (e *StatementBlackBoxMethod) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}

// =====================================================================
//  SVG Rendering
// =====================================================================

// renderSVG builds the complete SVG markup for this method device block.
//
// The visual layout mirrors StatementBlackBoxInit exactly — same header
// geometry, same port positions, different background colour to help the
// user visually distinguish Init blocks from method blocks.
//
// Header layout:
//
//	┌─────────────────────────────────┐  ← bbHeaderH tall
//	│          [FA icon 16×16]        │  ← centred at (w/2, bbIconCY)
//	│    StructLabel + MethodLabel    │  ← text baseline at bbLabelY
//	├─────────────────────────────────┤  ← divider at bbHeaderH
//	│ ● input   …                     │  ← port rows
//	│                output ●         │
//	└─────────────────────────────────┘
func (e *StatementBlackBoxMethod) renderSVG(w, h float64) string {
	bw := 2.0
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(h+float64(rulesDevice.KLabelHeight)),
	)

	// [PIN] the body is inset on BOTH sides by the pin length: the standard
	// connector pins (one per port, inputs left / outputs right) live in the
	// freed margins, protruding from the borders with the wires anchored at
	// their outer tips — the element's edges.
	// Português: O corpo recua dos DOIS lados o comprimento do pino: os
	// pinos padrão (um por porta, entradas à esquerda / saídas à direita)
	// vivem nas margens liberadas, saindo das bordas com os fios ancorados
	// nas pontas externas — as bordas do element.
	pin := rulesConnection.PinBodyInset()

	// ── Background — slightly different colour from Init ───────────────────
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="6" ry="6" fill="#1e2832" stroke="#55AA88" stroke-width="%.1f"/>`,
		pin+bw/2, bw/2, w-2*pin-bw, h-bw, bw,
	)

	// ── Header bar ────────────────────────────────────────────────────────
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="6" ry="6" fill="#283842"/>`,
		pin+bw, bw, w-2*pin-2*bw, bbHeaderH-bw,
	)
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#283842"/>`,
		pin+bw, bbHeaderH-8, w-2*pin-2*bw, 8.0,
	)

	// ── Icon ──────────────────────────────────────────────────────────────
	iconName := e.method.EffectiveIcon(e.def)
	svg += renderFAIconSVG(iconName, w/2, bbIconCY, bbIconSize, "#88DDBB")

	// ── Label text ────────────────────────────────────────────────────────
	// Format: "{StructLabel} {MethodLabel}" e.g. "APDS9960 log"
	headerText := e.def.EffectiveStructLabel() + " " + e.method.EffectiveLabel()
	svg += fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="%d" fill="#88DDBB" font-weight="bold" text-anchor="middle">%s</text>`,
		w/2, bbLabelY, bbHeaderFS, escapeXml(headerText),
	)

	// ── Divider ───────────────────────────────────────────────────────────
	svg += fmt.Sprintf(
		`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#3a4858" stroke-width="0.5"/>`,
		pin+bw, bbHeaderH, w-pin-bw, bbHeaderH,
	)

	// ── Input ports (left side) ───────────────────────────────────────────
	for i, port := range e.method.Inputs {
		cy := portCY(i)
		color := portColor(port.GoType, port.IsError)
		// [PIN] standard connector pin, port-type fill; the wire anchors at
		// its outer tip (the element's left edge).
		// Português: Pino padrão na cor do tipo da porta; o fio ancora na
		// ponta externa (borda esquerda do element).
		svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideLeft, pin, cy, color)
		// The wire-ƒ marker sits centred on the pin (pin/2 = its midpoint).
		// Português: O marcador ƒ fica centrado no pino (pin/2 = seu meio).
		svg += bbCallbackGlyph(port.CallbackType, pin/2, cy)
		svg += fmt.Sprintf(
			`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="%d" fill="#AABBCC" dominant-baseline="central">%s</text>`,
			pin+6, cy, bbFontSize, escapeXml(port.Name),
		)
	}

	// ── Output ports (right side) ─────────────────────────────────────────
	for i, port := range e.method.Outputs {
		cy := portCY(i)
		color := portColor(port.GoType, port.IsError)
		// [PIN] standard pin protruding RIGHT, wire anchored at the outer
		// tip (the element's right edge).
		// Português: Pino padrão saindo à DIREITA, fio ancorado na ponta
		// externa (borda direita do element).
		svg += rulesConnection.PinSVGFragment(rulesConnection.PinSideRight, w-pin, cy, color)
		// The wire-ƒ marker sits centred on the pin (w-pin/2 = the right
		// pin's midpoint) — outputs can be callback references too.
		// Português: O marcador ƒ fica centrado no pino (w-pin/2 = meio do
		// pino direito) — saídas também podem ser referências de callback.
		svg += bbCallbackGlyph(port.CallbackType, w-pin/2, cy)
		svg += fmt.Sprintf(
			`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="%d" fill="#AABBCC" dominant-baseline="central" text-anchor="end">%s</text>`,
			w-pin-6, cy, bbFontSize, escapeXml(port.Name),
		)
	}

	svg += `</svg>`
	return svg
}

// bbCallbackGlyph returns an SVG "ƒ" overlay centred on a connector dot when
// the port is a CALLBACK (wire-ƒ) port — the ƒ device's `callback` output or a
// callback input such as setDisplay.writer (callbackType is the function-
// pointer typedef it carries). Empty for ordinary ports. The glyph marks the
// port itself; the dashed stroke (wire.applyCallbackWireStyle) marks the wire.
//
// Português: Retorna um "ƒ" SVG sobre o dot do conector quando a porta é de
// CALLBACK (wire-ƒ). Vazio para portas comuns. Marca a porta; o tracejado
// marca o fio.
func bbCallbackGlyph(callbackType string, cx, cy float64) string {
	if callbackType == "" {
		return ""
	}
	// White bold ƒ (U+0192) centred on the standard pin. text-anchor=middle +
	// dominant-baseline=central keep it on the dot on either side. The default
	// type colour for a function-pointer typedef is blue, so white reads well.
	return fmt.Sprintf(
		`<text x="%.1f" y="%.1f" font-family="`+rulesDevice.KDeviceFontFamily+`" font-size="9" fill="#FFFFFF" font-weight="bold" text-anchor="middle" dominant-baseline="central">&#x192;</text>`,
		cx, cy,
	)
}

func (e *StatementBlackBoxMethod) injectLabel(svgXml string, ornH rulesDensity.Density) string {
	displayLabel := e.label
	if displayLabel == "" {
		displayLabel = e.id
	}
	// Append the effective Execution order as a tiebreaker hint, e.g.
	// "displayInit_1 (order 3)". Ordering precedence is wire > executionOrder,
	// so this is the tiebreaker value, not a guaranteed position — a wired
	// (order 3) can still run before an (order 1). Omitted when the block is
	// unordered (effective 0).
	if eff := e.effectiveExecutionOrder(); eff > 0 {
		displayLabel += fmt.Sprintf(" (order %d)", eff)
	}
	displayLabel = escapeXml(displayLabel)
	labelY := ornH.GetFloat() + 3
	labelSvg := fmt.Sprintf(rulesDevice.KDeviceLabel, labelY, displayLabel)
	return strings.Replace(svgXml, "</svg>", labelSvg+"</svg>", 1)
}

func (e *StatementBlackBoxMethod) recacheOrnament() {
	if e.elem == nil {
		return
	}
	svgXml := e.renderSVG(e.width.GetFloat(), e.height.GetFloat())
	svgXml = e.injectLabel(svgXml, e.height)
	_ = e.elem.CacheFromSvg(svgXml)
}

// =====================================================================
//  Wire Events
// =====================================================================

func (e *StatementBlackBoxMethod) wireEvents() {
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
		for i, port := range e.method.Inputs {
			cy := portCY(i)
			if rulesConnection.PinHit(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), cy,
				event.LocalX, event.LocalY) {
				items := mainMenu.ConnectorMenu(e.wireMgr, e.id, port.Name)
				go e.ctxMenu.OpenAtWorld(items, clickWX, clickWY)
				return
			}
		}

		// Hit-test output connectors
		for i, port := range e.method.Outputs {
			cy := portCY(i)
			if rulesConnection.PinHit(rulesConnection.PinSideRight,
				w-rulesConnection.PinBodyInset(), cy,
				event.LocalX, event.LocalY) {
				items := mainMenu.ConnectorMenu(e.wireMgr, e.id, port.Name)
				go e.ctxMenu.OpenAtWorld(items, clickWX, clickWY)
				return
			}
		}

		go e.ctxMenu.OpenForDevice(e, e.bodyMenuItems(), clickWX, clickWY)
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
		w, _ := e.elem.GetSize()
		for i := range e.method.Inputs {
			cy := portCY(i)
			if rulesConnection.PinHit(rulesConnection.PinSideLeft,
				rulesConnection.PinBodyInset(), cy, lx, ly) {
				return sprite.CursorPointer
			}
		}
		for i := range e.method.Outputs {
			cy := portCY(i)
			if rulesConnection.PinHit(rulesConnection.PinSideRight,
				w-rulesConnection.PinBodyInset(), cy, lx, ly) {
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
func (e *StatementBlackBoxMethod) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[BB-Method] delete: %v", e.id)
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

func (e *StatementBlackBoxMethod) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}

	for i, port := range e.method.Inputs {
		pp := port
		idx := i
		e.wireMgr.RegisterConnector(wire.ConnectorInfo{
			ID:                 wire.ConnectorID{ElementID: e.id, PortName: pp.Name},
			IsOutput:           false,
			AllowedTypes:       []string{portWireToken(pp)},
			CallbackType:       pp.CallbackType,
			AcceptNotConnected: pp.IsError,
			Locked:             false,
			MaxConnections:     1,
			Label:              pp.Name,
			PositionFunc: func() (float64, float64) {
				ex, ey := e.elem.GetPosition()
				ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
					rulesConnection.PinBodyInset(), portCY(idx))
				return ex + ax, ey + ay
			},
		})
	}

	for i, port := range e.method.Outputs {
		pp := port
		idx := i
		e.wireMgr.RegisterConnector(wire.ConnectorInfo{
			ID:                 wire.ConnectorID{ElementID: e.id, PortName: pp.Name},
			IsOutput:           true,
			AllowedTypes:       []string{portWireToken(pp)},
			CallbackType:       pp.CallbackType,
			AcceptNotConnected: true,
			Locked:             false,
			MaxConnections:     0,
			Label:              pp.Name,
			PositionFunc: func() (float64, float64) {
				ex, ey := e.elem.GetPosition()
				w, _ := e.elem.GetSize()
				ax, ay := rulesConnection.PinAnchor(rulesConnection.PinSideRight,
					w-rulesConnection.PinBodyInset(), portCY(idx))
				return ex + ax, ey + ay
			},
		})
	}
}

// =====================================================================
//  Inspect Overlay
// =====================================================================

func (e *StatementBlackBoxMethod) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)
	overlay.Show(cfg)
}

func (e *StatementBlackBoxMethod) GetInspectConfig() interface{} {
	// The Run/Method block owns only the visual label — all configurable props
	// belong to the Init block (StatementBlackBoxInit), which is the sole owner
	// of propValues. The Pinout tab therefore also lives only in Init.
	// Execution order field: pre-filled with the effective value (per-instance
	// override, else the directive default) so it shows the order in force,
	// e.g. "3". An empty field means "no specific order" (unordered); the
	// placeholder states that. Editing the value, or clearing it, persists as a
	// per-instance override (see OnSave).
	eoValue := ""
	if eff := e.effectiveExecutionOrder(); eff > 0 {
		eoValue = strconv.Itoa(eff)
	}
	eoPlaceholder := translate.T("propExecutionOrderUnordered", "no specific order")

	fields := []overlay.Field{
		{Key: "label", Label: translate.T("propLabel", "Label"), Type: overlay.FieldText, Value: e.label},
		{
			Key:         "comment",
			Label:       translate.T("propComment", "Comment"),
			Type:        overlay.FieldTextarea,
			Value:       e.comment,
			Placeholder: translate.T("propCommentPlaceholder", "Comment shown in generated code..."),
			Rows:        3,
		},
		{
			Key:         "executionOrder",
			Label:       translate.T("propExecutionOrder", "Execution order"),
			Type:        overlay.FieldNumber,
			Min:         "0",
			Value:       eoValue,
			Placeholder: eoPlaceholder,
		},
	}

	tabs := []overlay.Tab{
		{
			Label:  translate.T("tabProperties", "Properties"),
			Type:   overlay.TabForm,
			Fields: fields,
		},
	}

	// Help tab — markdown tabs from GitHub repo files (run.en.md, etc.),
	// then the GoDoc card always last.
	//
	// Language resolution: session preference → browser locale → "en".
	lang := helpSessionLang()
	mdTabs := e.def.HelpTabsFor(e.method.Name, lang)
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
		for _, p := range e.def.PagesFor(e.method.Name) {
			helpCards = append(helpCards, overlay.HelpCard{
				Name:     p.Name,
				Language: p.Language,
				Content:  p.Content,
			})
		}
	}

	if md := buildGodocMarkdown(e.def.Name, e.def.Doc, e.method.Name, e.method.Doc); md != "" {
		helpCards = append(helpCards, overlay.HelpCard{
			Name:     "source doc",
			Language: "en",
			Content:  md,
		})
	}

	if len(helpCards) > 0 {
		tabs = append(tabs, overlay.Tab{
			Label:     translate.T("tabHelp", "Help"),
			Type:      overlay.TabHelpDeck,
			HelpCards: helpCards,
		})
	}

	return overlay.Config{
		Title: fmt.Sprintf("%s %s — %s", e.def.Name, e.method.Name, e.id),
		Width: "540px",
		Tabs:  tabs,
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
			changed := false

			if v, ok := values["label"]; ok && v != "" && v != e.label {
				e.label = v
				changed = true
			}

			// Execution order override: empty clears it (back to the directive
			// default); a parseable integer (including 0 = unordered) sets it
			// for this block only.
			if raw, ok := values["executionOrder"]; ok {
				raw = strings.TrimSpace(raw)
				switch {
				case raw == "":
					// Empty = no specific order (unordered). Persist an explicit
					// 0 override so the choice survives a reload, deviating from a
					// possibly ordered default. (The field is pre-filled with the
					// effective value, so an empty field is a deliberate clear.)
					if e.executionOrderOverride == nil || *e.executionOrderOverride != 0 {
						v := 0
						e.executionOrderOverride = &v
						changed = true
					}
				default:
					if n, err := strconv.Atoi(raw); err == nil {
						if n < 0 {
							n = 0 // negative is meaningless here; clamp to 0 (unordered)
						}
						if e.executionOrderOverride == nil || *e.executionOrderOverride != n {
							v := n
							e.executionOrderOverride = &v
							changed = true
						}
					}
				}
			}

			if changed {
				go func() {
					time.Sleep(200 * time.Millisecond)
					e.recacheOrnament()
				}()
			}
		},
	}
}

// ApplyProperties restores per-instance properties when a saved scene is
// imported. The method device persists only the Execution order override
// (label and instanceId are handled elsewhere), so it reads that marker back
// here. Implementing this also makes the device satisfy scene.Inspectable, so
// importScene invokes it during stage reconstruction.
//
// Português: Restaura propriedades por instância ao importar uma cena salva.
// O device de método só persiste o override da Execution order; lê o marcador
// de volta aqui. Implementar isto também faz o device satisfazer
// scene.Inspectable, então o importScene o chama no carregamento.
func (e *StatementBlackBoxMethod) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	changed := false

	if raw, ok := values["executionOrderOverride"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			if n < 0 {
				n = 0 // negative is meaningless here; clamp to 0 (unordered)
			}
			if e.executionOrderOverride == nil || *e.executionOrderOverride != n {
				v := n
				e.executionOrderOverride = &v
				changed = true
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

// GetDeviceType returns the scene type string for this method device.
//
// Format: "BlackBox{MethodName}:{StructName}"
// Examples: "BlackBoxRun:APDS9960", "BlackBoxLog:APDS9960"
//
// This is the key that the codegen server uses to route nodes to the
// correct IR instruction (BB_METHOD with meta["method"] = MethodName).
//
// Português: Retorna a string de tipo da cena para este device de método.
// Formato: "BlackBox{NomeMétodo}:{NomeStruct}"
func (e *StatementBlackBoxMethod) GetDeviceType() string {
	// The C99 callback REFERENCE variant (the "ƒ" device) reports a dedicated,
	// non-BlackBox scene type so codegen routes it as a reference: it skips the
	// call and resolves the wire to the function name (passed by address). Set
	// by SetCallbackRef; empty for the normal callable block.
	if e.callbackRefFn != "" {
		return "CallbackRef:" + e.callbackRefFn
	}
	return "BlackBox" + e.method.Name + ":" + e.def.Name
}

func (e *StatementBlackBoxMethod) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementBlackBoxMethod) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}

func (e *StatementBlackBoxMethod) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementBlackBoxMethod) SetSceneNotify(fn func()) { e.sceneNotify = fn }

func (e *StatementBlackBoxMethod) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// GetComment returns the user comment shown in generated code and in the
// device's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do device.
func (e *StatementBlackBoxMethod) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementBlackBoxMethod) SetComment(c string) { e.comment = c }

func (e *StatementBlackBoxMethod) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		"instanceId": e.instanceId,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}

	// executionOrder carries the EFFECTIVE value (per-instance override, else
	// the directive default) so the codegen server can feed it to the
	// topological sort. 0 (unordered) is omitted — the server treats absent as
	// 0. Ordering precedence is wire > executionOrder.
	if eff := e.effectiveExecutionOrder(); eff > 0 {
		props["executionOrder"] = eff
	}

	// executionOrderOverride is the round-trip marker for the per-instance
	// value set in Inspect. Written ONLY when an override exists, and written
	// even when 0 so an explicit "unordered" survives a reload. ApplyProperties
	// reads this back; its absence means "use the directive default", which
	// keeps the default fresh if the source changes later.
	if e.executionOrderOverride != nil {
		props["executionOrderOverride"] = *e.executionOrderOverride
	}

	return props
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementBlackBoxMethod) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// portWireToken returns the connector token a port exposes on the stage:
// WireType when the parser set one (scalar-pointer family tokens), else the
// authored GoType — the pre-existing behaviour for every other port.
// Português: Retorna o token de conector que a porta expõe no stage:
// WireType quando o parser definiu (tokens de família ponteiro-escalar),
// senão o GoType autoral — o comportamento pré-existente de todas as
// outras portas.
func portWireToken(pp blackbox.PortDefClient) string {
	if pp.WireType != "" {
		return pp.WireType
	}
	return pp.GoType
}
