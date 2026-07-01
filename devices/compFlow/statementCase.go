// /ide/devices/compFlow/statementCase.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// statementCase.go — N-way Case container device (replaces StatementIfElse).
//
// A Complex device (kind = KindComplex) that splits its children into N
// ordered "cases" governed by a typed "selector" input on the left edge. It is
// the N-way generalization of StatementIfElse: a boolean selector with two
// cases is lowered to an if/else by the codegen (and uses the ifElseBorder
// ornament), while any other selector (int for v1) emits a switch.
//
// Case management (cases, selectedCase, defaultCaseID, selectCase,
// assignNewChildren, applyCaseVisibility) is orthogonal to the containment
// rule engine and lives entirely inside this file. It mirrors the if/else
// branch machinery one-for-one, generalised from two fixed branches to an
// ordered slice of cases.
//
// Serialization contract with the codegen (server/codegen/graph/builder.go):
//
//	properties = {
//	  "selectorType":  "int",          // "bool" lowers to if/else
//	  "selectedCase":  "<case id>",    // design-time only (active case)
//	  "cases": [ { "id", "label", "matchKind", "values": [...], "ids": [...] }, ... ],
//	  "defaultCaseId": "<case id>"     // "" = no default
//	}
//	connector port name = "selector"
//
// Slice scope: this is the device skeleton + serialization. The pill cycles
// through cases on click (a placeholder for the full case dropdown) and new
// children are assigned to the selected case. Case add/remove and value editing
// arrive with the inspector in a later slice.
//
// Português: Device container Case de N vias (substitui o StatementIfElse).
// Divide os filhos em N "cases" ordenados, governados por uma entrada
// "selector" tipada. Selector booleano com 2 cases vira if/else no codegen; os
// demais (int) viram switch. O pill cicla entre os cases ao clicar (placeholder
// do dropdown completo); novos filhos vão para o case selecionado.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/connection"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/hexagon"
	"github.com/helmutkemper/iotmakerio/ornament/caseBorder"
	"github.com/helmutkemper/iotmakerio/rulesContainer"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
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

// caseEntry is one case of a StatementCase device: the literal selector values
// it matches, the device IDs assigned to it, and whether it is the default.
//
// Português: Um case do StatementCase — os valores literais que casa, os IDs
// dos devices atribuídos e se é o default.
type caseEntry struct {
	id    string
	label string
	// matchKind selects how values is interpreted when testing the selector
	// against this case:
	//
	//	"is"      → selector equals values[0]
	//	"isAnyOf" → selector equals any element of values
	//	"between" → inclusive range, values[0] <= selector <= values[1]
	//	"gt"/"lt"/"gte"/"lte" → selector compared against values[0]
	//
	// "is" and "isAnyOf" lower to a switch `case`; the range/comparison kinds
	// force the whole Case onto the if/else-if codegen (see the codegen's
	// ir.BuildCaseCondition). An empty matchKind is treated as "is"/"isAnyOf"
	// by value count — the same backfill the codegen's extractCases applies to
	// legacy scenes — so an unset kind never breaks an older scene. Init and
	// RestoreCaseState keep this explicit so the (future) inspector overlay
	// never has to guess.
	matchKind string
	values    []string
	ids       []string
	isDefault bool
}

type StatementCase struct {
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

	pendingResizeEnable *bool
	pendingDragEnable   *bool
	pendingSelected     *bool

	resizerButton block.ResizeButton
	// [CTXMENU] linear context menu controller.
	ctxMenu *contextMenu.Controller
	wireMgr *wire.Manager

	defaultWidth          rulesDensity.Density
	defaultHeight         rulesDensity.Density
	horizontalMinimumSize rulesDensity.Density
	verticalMinimumSize   rulesDensity.Density

	ornamentDraw     *caseBorder.CaseBorder
	ornamentDrawIcon *caseBorder.CaseBorder

	id         string
	gridAdjust grid.Adjust
	iconStatus int

	sceneNotify func()
	onRemove    func(id string)
	sceneMgr    *scene.Serializer
	canvasEl    js.Value

	resizeChildBounds *rulesContainer.Rect
	resizeParentInner *rulesContainer.Rect

	dragStartX, dragStartY float64

	// Case management
	selectorType  string      // "int" for v1; "bool" lowers to if/else
	selectedCase  string      // id of the case currently shown/edited
	cases         []caseEntry // ordered cases
	defaultCaseID string      // id of the default case ("" = none)

	// codegenPreview renders this Case as source via the real codegen, for the
	// inspect panel's Preview tab. Injected by the factory from the Workspace;
	// nil until wired, in which case the inspector simply opens without a
	// Preview tab.
	codegenPreview CodegenPreviewFunc
}

// CodegenPreviewFunc renders a StatementCase as source in the project's
// language and returns the cross-case diagnostics. It is the device-side type
// of Workspace.previewCaseCode, injected via SetCodegenPreview so the device
// (in compFlow) need not depend on stageWorkspace. monacoLang is the Monaco
// highlight id for the rendered language; ok is false when no preview was
// produced (not logged in, error, or cancelled).
//
// Português: Renderiza um StatementCase como código na linguagem do projeto e
// devolve os diagnósticos cruzados. É o tipo, do lado do device, do
// Workspace.previewCaseCode, injetado via SetCodegenPreview — assim o device
// (em compFlow) não precisa depender de stageWorkspace. ok=false quando não
// houve preview.
type CodegenPreviewFunc func(scopeID, selectorType, casesJSON string) (code string, diags []overlay.Diagnostic, monacoLang string, ok bool)

// ── Dependency injection ─────────────────────────────────────────────

func (e *StatementCase) SetStage(stage sprite.Stage)       { e.stage = stage }
func (e *StatementCase) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementCase) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementCase) SetWireManager(mgr *wire.Manager)        { e.wireMgr = mgr }
func (e *StatementCase) SetCanvasEl(el js.Value)                 { e.canvasEl = el }
func (e *StatementCase) SetResizerButton(rb block.ResizeButton)  { e.resizerButton = rb }
func (e *StatementCase) SetDraggerButton(_ block.ResizeButton)   {}
func (e *StatementCase) SetGridAdjust(ga grid.Adjust)            { e.gridAdjust = ga }
func (e *StatementCase) SetOnRemove(fn func(id string))          { e.onRemove = fn }
func (e *StatementCase) SetCodegenPreview(fn CodegenPreviewFunc) { e.codegenPreview = fn }

// SetSceneNotify wraps the workspace callback so every scene change also
// re-evaluates case assignment and visibility.
func (e *StatementCase) SetSceneNotify(fn func()) {
	e.sceneNotify = func() {
		e.assignNewChildren()
		e.applyCaseVisibility()
		if fn != nil {
			fn()
		}
	}
}

// ── Lifecycle ────────────────────────────────────────────────────────

func (e *StatementCase) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementCase) Remove() {
	if e.onRemove != nil {
		e.onRemove(e.id)
	}
	if e.wireMgr != nil {
		e.wireMgr.UnregisterElement(e.id)
	}
	devices.BackendZRegistry.Unregister(e.id)
	if e.elem != nil {
		e.elem.SetVisible(false)
		elem := e.elem
		e.elem = nil
		go func() { time.Sleep(50 * time.Millisecond); elem.Destroy() }()
	}
}

// ── Scenegraph interface — Kinded + Padded ───────────────────────────

func (e *StatementCase) GetKind() scenegraph.Kind { return scenegraph.KindComplex }

func (e *StatementCase) GetContainerPadding() rulesContainer.Padding {
	// Reuse the if/else container padding: the case container has the same
	// rounded-rectangle shape and inner margins. A dedicated CasePadding can
	// be introduced later if the visuals diverge.
	return rulesContainer.IfElsePadding()
}

// ── Init ─────────────────────────────────────────────────────────────

func (e *StatementCase) Init() (err error) {
	if e.stage == nil {
		log.Println("Error: SetStage() must be called before Init()")
		return
	}

	_ = hexagon.KStageId

	if e.name == "" {
		e.SetName("stmCase")
	}

	e.defaultWidth = 400
	e.defaultHeight = 300
	e.defaultWidth, e.defaultHeight = e.gridAdjust.AdjustCenterD(e.defaultWidth, e.defaultHeight)

	e.horizontalMinimumSize = 200
	e.verticalMinimumSize = 150
	e.horizontalMinimumSize, e.verticalMinimumSize = e.gridAdjust.AdjustCenterD(e.horizontalMinimumSize, e.verticalMinimumSize)

	if e.width == 0 || e.height == 0 {
		e.width = e.defaultWidth
		e.height = e.defaultHeight
	}

	e.id = rulesSequentialId.GetIdFromBase(e.name)

	// Selector type and default cases for a fresh device. A loaded device gets
	// its cases re-applied by RestoreCaseState after import.
	if e.selectorType == "" {
		e.selectorType = "int"
	}
	if len(e.cases) == 0 {
		e.cases = []caseEntry{
			{id: e.id + "_c0", label: "case 0", matchKind: "is", values: []string{"0"}},
			{id: e.id + "_c1", label: "case 1", matchKind: "is", values: []string{"1"}},
		}
	}
	if e.selectedCase == "" {
		e.selectedCase = e.cases[0].id
	}

	e.ornamentDraw = new(caseBorder.CaseBorder)
	e.ornamentDrawIcon = new(caseBorder.CaseBorder)

	selectorButton := connection.Setup{
		FatherId:           e.id,
		Name:               "selectorButton",
		DataType:           e.selectorType,
		AcceptNotConnected: false,
		LookedUp:           false,
		IsADataInput:       true,
		ClickFunc: js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			data := this.Call("getConnData")
			log.Printf("SelectorButton FatherId: %v", data.Get("FatherId").String())
			return nil
		}),
	}
	if err = selectorButton.Verify(); err != nil {
		log.Printf("selectorButton.Verify: %v", err)
		return
	}

	e.ornamentDraw.SetSelectorType(e.selectorType)
	e.ornamentDraw.SelectorButtonSetup(selectorButton)
	e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())

	if err = e.ornamentDraw.GetConnectionError(); err != nil {
		return
	}

	_ = e.ornamentDraw.Init()
	_ = e.ornamentDrawIcon.Init()

	_ = e.ornamentDraw.Update(0, 0, e.width, e.height)
	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", e.width.GetInt())
	ornamentSvg.Call("setAttribute", "height", e.height.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)

	e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
		ID:         e.id,
		X:          0,
		Y:          0,
		Width:      e.width.GetFloat(),
		Height:     e.height.GetFloat(),
		Index:      1,
		DragEnable: false,
		SvgXml:     ornamentXml,
	})
	if err != nil {
		log.Printf("Failed to create sprite element: %v", err)
		return
	}

	e.elem.SetMinSizeD(e.horizontalMinimumSize, e.verticalMinimumSize)
	devices.BackendZRegistry.Register(e.id, e.elem)

	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		if rerr := e.elem.SetResizeButtons(adapter); rerr != nil {
			log.Printf("[SPRITE] ERROR: SetResizeButtons failed: %v", rerr)
		}
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
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

// ── Case management ──────────────────────────────────────────────────

// caseIndexByID returns the index of the case with the given id, or -1.
func (e *StatementCase) caseIndexByID(id string) int {
	for i := range e.cases {
		if e.cases[i].id == id {
			return i
		}
	}
	return -1
}

// activeCaseLabel returns the pill text for the currently-selected case.
func (e *StatementCase) activeCaseLabel() string {
	idx := e.caseIndexByID(e.selectedCase)
	if idx < 0 {
		return "case"
	}
	c := e.cases[idx]
	if c.isDefault {
		return c.label + " (default)"
	}
	return c.label
}

// caseMenuLabel returns the dropdown label for a case, mirroring
// activeCaseLabel: the default case is suffixed with " (default)".
//
// Português: Rótulo do case no dropdown, espelhando activeCaseLabel: o case
// default recebe o sufixo " (default)".
func (e *StatementCase) caseMenuLabel(c caseEntry) string {
	if c.isDefault {
		return c.label + " (default)"
	}
	return c.label
}

// caseSelectMenuItems builds the pill dropdown: one entry per case, in order.
// Clicking an entry makes that case the visible/edited one (selectCase). The
// currently-selected case carries the eye icon so the maker can see which case
// is on the stage. This replaces the Slice-2 cycle-on-click placeholder.
//
// Português: Monta o dropdown da pílula: uma entrada por case, na ordem.
// Clicar numa entrada torna aquele case o visível/editado (selectCase). O case
// selecionado leva o ícone de olho pra o maker ver qual está no stage.
// Substitui o placeholder de ciclar-ao-clicar da Slice-2.
func (e *StatementCase) caseSelectMenuItems() []contextMenu.Item {
	items := make([]contextMenu.Item, 0, len(e.cases))
	for i := range e.cases {
		c := e.cases[i]
		id := c.id // capture per iteration for the closure below
		item := contextMenu.Item{
			ID:           c.id,
			Label:        e.caseMenuLabel(c),
			HelpFallback: "Show this case on the stage.",
			OnClick:      func() { e.selectCase(id) },
		}
		if c.id == e.selectedCase {
			item.FontAwesomePath = rulesIcon.KFAEye
			item.ViewBox = "0 0 512 512"
		}
		items = append(items, item)
	}
	return items
}

// selectCase makes the case with the given id the visible/edited one. It first
// captures any devices the maker placed in the currently-visible case (so a
// switch never loses them), then swaps visibility and refreshes the pill. A
// click on the already-selected case is a no-op beyond that capture.
//
// This is the selection primitive behind the pill dropdown; it replaced the
// Slice-2 cycle-on-click placeholder.
//
// Português: Torna o case do id dado o visível/editado. Primeiro captura
// devices que o maker colocou no case visível atual (pra uma troca nunca os
// perder), depois troca a visibilidade e atualiza a pílula. Clicar no case já
// selecionado é no-op além dessa captura.
//
// É a primitiva de seleção por trás do dropdown da pílula; substituiu o
// placeholder cycleCase da Slice-2.
func (e *StatementCase) selectCase(id string) {
	e.assignNewChildren()
	if e.caseIndexByID(id) < 0 {
		return
	}
	if id == e.selectedCase {
		return
	}
	e.selectedCase = id

	log.Printf("[Case] select %q on %v", id, e.id)

	e.applyCaseVisibility()
	if e.ornamentDraw != nil {
		e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
	}
	go e.recacheOrnament()
}

// caseSelectorTypes are the data types a Case selector input accepts. The
// selector adopts whichever of these a connected wire carries (see
// OnWireConnected): bool lowers to if/else, int to a switch, string and float
// to an if/else-if chain (float is range/relational only — there is no float
// switch in C and exact float equality is fragile). float64/error and other
// types are rejected by the wire manager's AllowedTypes filter.
//
// Português: Tipos que a entrada "selector" do Case aceita. O seletor adota o
// tipo do fio conectado (ver OnWireConnected): bool vira if/else, int vira
// switch, string e float viram cadeia if/else-if (float é só faixa/relacional —
// não há switch de float em C e igualdade exata de float é frágil). float64,
// error e outros são rejeitados pelo filtro AllowedTypes do wire manager.
var caseSelectorTypes = []string{"bool", "int", "string", "float"}

// isCaseSelectorType reports whether t is a type the Case selector accepts.
//
// Português: Informa se t é um tipo aceito pela entrada "selector" do Case.
func isCaseSelectorType(t string) bool {
	for _, v := range caseSelectorTypes {
		if v == t {
			return true
		}
	}
	return false
}

// OnWireConnected reacts to a wire being connected to one of this device's
// input ports. When the wire lands on the "selector" port the Case adopts the
// wire's resolved data type as its selector type — so connecting a bool source
// turns it into a boolean (if/else) Case, a string source into a string Case,
// and so on. Connections to other ports, unsupported selector types, or a type
// equal to the current one are ignored. Disconnecting a wire never changes the
// type (the last inferred type stays until a different wire is connected).
//
// Português: Reage a um fio conectado numa porta de entrada deste device.
// Quando o fio chega na porta "selector" o Case adota o tipo resolvido do fio
// como tipo do seletor — conectar uma fonte bool o torna um Case booleano
// (if/else), string vira Case de string, etc. Conexões em outras portas, tipos
// de seletor não suportados, ou tipo igual ao atual são ignorados. Desconectar
// um fio nunca muda o tipo (o último tipo inferido permanece).
func (e *StatementCase) OnWireConnected(portName, dataType string) {
	if portName != "selector" {
		return
	}
	if !isCaseSelectorType(dataType) || dataType == e.selectorType {
		return
	}
	e.applySelectorType(dataType)
}

// applySelectorType switches the Case to a new selector type: it reshapes the
// cases for the new type (see transformCasesForType), updates the border
// ornament's type and pill label, refreshes child visibility, and re-caches the
// ornament so the new type/colour paint immediately.
//
// Português: Troca o Case para um novo tipo de seletor: remodela os cases para
// o novo tipo (ver transformCasesForType), atualiza o tipo e o label do
// ornamento, refaz a visibilidade dos filhos e recacheia o ornamento para a
// nova cor/tipo pintarem na hora.
func (e *StatementCase) applySelectorType(t string) {
	e.assignNewChildren() // capture current child membership before reshaping
	prev := e.selectorType
	e.selectorType = t
	e.transformCasesForType(prev, t)
	if e.ornamentDraw != nil {
		e.ornamentDraw.SetSelectorType(t)
		e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
	}
	e.applyCaseVisibility()
	log.Printf("[Case] selector type -> %q on %v", t, e.id)
	go e.recacheOrnament()
}

// transformCasesForType reshapes the case set when the selector type changes.
//
// bool is exhaustive, so it snaps to exactly two cases — true and false — and
// drops any default (see snapCasesToBool). For int, string and float the case
// structure is kept (slots, child membership and the default survive) and only
// the now-stale match VALUES are cleared, since values entered for the previous
// type rarely fit the new one. For float the exact-match kinds ("is"/"isAnyOf")
// are invalid, so they are reset to a relational default ("gt").
//
// The maker's own case labels are preserved across an int↔string↔float switch.
// The one exception is LEAVING bool: snapCasesToBool wrote the boolean-specific
// labels "true"/"false", which must not linger on an int/string/float Case — so
// when prev is bool the labels are reset to a generic "case N".
//
// Português: Remodela o conjunto de cases quando o tipo do seletor muda. bool é
// exaustivo: reduz a exatamente dois cases (true/false) e remove o default (ver
// snapCasesToBool). Para int, string e float a estrutura é mantida (slots,
// filhos e o default sobrevivem) e só os VALUES (agora obsoletos) são limpos.
// Para float os tipos de igualdade ("is"/"isAnyOf") são inválidos, então viram
// um relacional padrão ("gt"). Os labels do maker são preservados entre
// int↔string↔float; a exceção é SAIR de bool: o snapCasesToBool escreveu os
// labels "true"/"false" específicos de bool, que não podem permanecer num Case
// int/string/float — então, quando prev é bool, os labels viram "case N".
func (e *StatementCase) transformCasesForType(prev, t string) {
	if t == "bool" {
		e.snapCasesToBool()
		return
	}
	leavingBool := prev == "bool"
	for i := range e.cases {
		if e.cases[i].isDefault || e.cases[i].id == e.defaultCaseID {
			continue // a default/catch-all carries no match values
		}
		e.cases[i].values = nil
		if leavingBool {
			e.cases[i].label = fmt.Sprintf("case %d", i)
		}
		// Normalise the match kind to what the new type can express: a string
		// matches by equality only (is/isAnyOf), so any relational/range kind
		// becomes "is"; float is the opposite (relational only, never exact
		// equality), so is/isAnyOf become "gt". int keeps whatever was set.
		//
		// Português: Normaliza o matchKind ao que o novo tipo expressa: string
		// casa só por igualdade (is/isAnyOf), então relacional/faixa vira "is";
		// float é o oposto (só relacional, nunca igualdade exata), então
		// is/isAnyOf viram "gt". int mantém o que estava.
		switch t {
		case "string":
			switch e.cases[i].matchKind {
			case "between", "gt", "lt", "gte", "lte":
				e.cases[i].matchKind = "is"
			}
		case "float":
			switch e.cases[i].matchKind {
			case "is", "isAnyOf", "":
				e.cases[i].matchKind = "gt"
			}
		}
	}
}

// snapCasesToBool collapses the case set onto the two exhaustive boolean
// outcomes (true, false) and removes any default. The first two existing cases
// are reused — preserving their ids and child membership — so devices the maker
// already placed survive; children of any further cases are moved onto the
// "false" case so nothing is orphaned (the same no-orphan rule ApplyProperties
// uses when a case is deleted).
//
// Português: Reduz o conjunto de cases aos dois resultados booleanos (true,
// false) e remove o default. Reaproveita os dois primeiros cases — preservando
// ids e filhos — para os devices já colocados sobreviverem; filhos de cases
// extras vão para o case "false", para nada ficar órfão (mesma regra do
// ApplyProperties ao apagar um case).
func (e *StatementCase) snapCasesToBool() {
	trueCase := caseEntry{label: "true", matchKind: "is", values: []string{"true"}}
	falseCase := caseEntry{label: "false", matchKind: "is", values: []string{"false"}}
	if len(e.cases) > 0 {
		trueCase.id = e.cases[0].id
		trueCase.ids = e.cases[0].ids
	} else {
		trueCase.id = e.id + "_ctrue"
	}
	if len(e.cases) > 1 {
		falseCase.id = e.cases[1].id
		falseCase.ids = e.cases[1].ids
	} else {
		falseCase.id = e.id + "_cfalse"
	}
	for i := 2; i < len(e.cases); i++ {
		falseCase.ids = append(falseCase.ids, e.cases[i].ids...)
	}
	e.cases = []caseEntry{trueCase, falseCase}
	e.defaultCaseID = ""
	if e.caseIndexByID(e.selectedCase) < 0 {
		e.selectedCase = e.cases[0].id
	}
}

// assignNewChildren assigns newly-contained children to the selected case and
// drops IDs that left the container. Mirrors the if/else branch assignment,
// generalised to N cases.
//
// Português: Atribui novos filhos ao case selecionado e remove os que saíram.
func (e *StatementCase) assignNewChildren() {
	if e.sceneMgr == nil {
		return
	}

	containedIDs := e.sceneMgr.ChildrenOf(e.id)

	known := make(map[string]bool)
	for _, c := range e.cases {
		for _, id := range c.ids {
			known[id] = true
		}
	}

	selIdx := e.caseIndexByID(e.selectedCase)
	for _, id := range containedIDs {
		if !known[id] && selIdx >= 0 {
			e.cases[selIdx].ids = append(e.cases[selIdx].ids, id)
		}
	}

	containedSet := make(map[string]bool, len(containedIDs))
	for _, id := range containedIDs {
		containedSet[id] = true
	}
	for i := range e.cases {
		e.cases[i].ids = filterExisting(e.cases[i].ids, containedSet)
	}
}

// applyCaseVisibility shows the selected case's children and hides every other
// case's children, mirroring the if/else branch visibility (show one, hide the
// rest). Hidden devices also leave the wire layer and collision so the inactive
// cases show no orphan wires and are not cross-case connect targets.
//
// Português: Mostra os filhos do case selecionado e esconde os dos demais.
func (e *StatementCase) applyCaseVisibility() {
	if e.stage == nil {
		return
	}

	for i := range e.cases {
		show := e.cases[i].id == e.selectedCase
		for _, id := range e.cases[i].ids {
			if elem, found := e.stage.GetElement(id); found {
				elem.SetVisible(show)
			}
			if warnElem, found := e.stage.GetElement(id + "_warning"); found {
				if !show {
					warnElem.SetVisible(false)
				}
			}
			// Take inactive devices out of the wire layer and out of collision
			// (flag flips, reversed when the case is shown again). Registration
			// is preserved.
			if e.wireMgr != nil {
				e.wireMgr.SetElementHidden(id, !show)
			}
			if e.sceneMgr != nil {
				e.sceneMgr.SetHidden(id, !show)
			}
		}
	}
}

// ── Hex menu items ───────────────────────────────────────────────────

// getBodyMenuItems returns body context menu items for this container.
// Order: Delete first (canonical per D4), Inspect, Resize toggle,
// Forward/Backward z-ordering.
//
// Português: Itens do menu de contexto do corpo (Delete, Inspect, Resize,
// Forward, Backward).
func (e *StatementCase) getBodyMenuItems() []contextMenu.Item {
	resizeLabel := translate.T("menuDeviceResize", "Resize")
	resizeHelp := "Toggles corner handles so you can drag to resize the container."
	resizeAction := func() {
		e.resizeLocked = false
		e.SetResizeEnable(true)
	}
	if e.GetResizeEnable() {
		resizeLabel = translate.T("menuDeviceResizeCancel", "Lock Size")
		resizeHelp = "Locks the current size and hides the corner handles."
		resizeAction = func() { e.SetResizeEnable(false) }
	}

	return []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Case] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			log.Printf("[Case] inspect: opening cases overlay for %v", e.id)
			// Must run in a goroutine: overlay.Show loads help/preview assets
			// asynchronously, like every other device's inspect overlay.
			go e.showInspectOverlay()
		}),
		{
			ID:              "resize",
			Label:           resizeLabel,
			FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight,
			ViewBox:         "0 0 512 512",
			HelpFallback:    resizeHelp,
			OnClick:         resizeAction,
		},
		{
			ID:              "forward",
			Label:           translate.T("menuDeviceBringForward", "Forward"),
			FontAwesomePath: rulesIcon.KFAPlus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Brings this container above overlapping devices.",
			OnClick:         func() { devices.BackendZRegistry.MoveForward(e.id) },
		},
		{
			ID:              "backward",
			Label:           translate.T("menuDeviceSendBackward", "Backward"),
			FontAwesomePath: rulesIcon.KFAMinus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Sends this container below overlapping devices.",
			OnClick:         func() { devices.BackendZRegistry.MoveBackward(e.id) },
		},
	}
}

// ── Sprite event wiring ──────────────────────────────────────────────

func (e *StatementCase) wireEvents() {
	// Click — three hit regions: case pill, selector connector, body.
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		_, h := e.elem.GetSize()

		if event.LocalX >= caseBorder.KPillX && event.LocalX <= caseBorder.KPillX+caseBorder.KPillW &&
			event.LocalY >= caseBorder.KPillY && event.LocalY <= caseBorder.KPillY+caseBorder.KPillH {
			log.Printf("[Case] pill clicked on %v", e.id)
			if e.ctxMenu == nil {
				return
			}
			if e.ctxMenu.IsOpen() {
				e.ctxMenu.Close()
				return
			}
			elemX, elemY := e.elem.GetPosition()
			menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
			go e.ctxMenu.OpenAtWorld(e.caseSelectMenuItems(), menuX, menuY)
			return
		}

		connX := 5.0
		connY := h / 2
		connRadius := 12.0
		dx := event.LocalX - connX
		dy := event.LocalY - connY
		if dx*dx+dy*dy <= connRadius*connRadius {
			if e.ctxMenu != nil && e.wireMgr != nil {
				elemX, elemY := e.elem.GetPosition()
				menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
				go e.ctxMenu.OpenAtWorld(
					mainMenu.ConnectorMenu(e.wireMgr, e.id, "selector"),
					menuX, menuY,
				)
			}
			return
		}

		if e.ctxMenu == nil {
			return
		}
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
		go e.ctxMenu.OpenAtWorld(e.getBodyMenuItems(), menuX, menuY)
	})

	// Drag — scenegraph lifecycle.
	e.elem.SetOnDragStart(func(event sprite.DragEvent) {
		e.dragStartX, e.dragStartY = e.elem.GetPosition()
		if e.sceneMgr != nil {
			e.sceneMgr.BeginDrag(e.id)
		}
	})

	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.UpdateDrag(e.id)
		}
	})

	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		x, y := e.elem.GetPositionD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)

		finalX, finalY := e.elem.GetPosition()
		dx, dy := rulesContainer.DragChildDelta(e.dragStartX, e.dragStartY, finalX, finalY)
		if e.sceneMgr != nil {
			e.sceneMgr.EndDrag(e.id, dx, dy)
		}

		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		e.assignNewChildren()
		e.applyCaseVisibility()

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// Resize — scenegraph lifecycle + two-axis clamp.
	e.elem.SetOnResizeStart(func(event sprite.ResizeEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.BeginResize(e.id)
		}

		e.resizeChildBounds = nil
		e.resizeParentInner = nil

		if e.sceneMgr != nil {
			if bounds := e.sceneMgr.ChildrenBounds(e.id); bounds != nil {
				cb := rulesContainer.Rect{X: bounds.X, Y: bounds.Y, W: bounds.Width, H: bounds.Height}
				e.resizeChildBounds = &cb
			}
			if parentInner := e.sceneMgr.ParentInnerBBox(e.id); parentInner != nil {
				pi := rulesContainer.Rect{X: parentInner.X, Y: parentInner.Y, W: parentInner.Width, H: parentInner.Height}
				e.resizeParentInner = &pi
			}
		}
	})

	e.elem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		if e.resizeChildBounds != nil {
			loopX, loopY := e.elem.GetPosition()
			loopW, loopH := e.elem.GetSize()
			proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: loopW, H: loopH}
			pad := rulesContainer.IfElsePadding()
			clamped := rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)

			if clamped.X != loopX || clamped.Y != loopY {
				e.elem.SetPosition(clamped.X, clamped.Y)
			}
			if clamped.W != loopW || clamped.H != loopH {
				e.elem.SetSize(clamped.W, clamped.H)
			}
		}
	})

	e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		wD, hD := e.elem.GetSizeD()
		newW, newH := e.gridAdjust.AdjustCenterD(wD, hD)
		loopX, loopY := e.elem.GetPosition()
		proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: newW.GetFloat(), H: newH.GetFloat()}

		if e.resizeChildBounds != nil {
			pad := rulesContainer.IfElsePadding()
			proposed = rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)
		}
		if e.resizeParentInner != nil {
			proposed = rulesContainer.ClampToParent(proposed, *e.resizeParentInner)
		}

		e.resizeChildBounds = nil
		e.resizeParentInner = nil

		e.elem.SetPosition(proposed.X, proposed.Y)
		newW = rulesDensity.Density(proposed.W)
		newH = rulesDensity.Density(proposed.H)
		e.elem.SetSizeD(newW, newH)
		e.width = newW
		e.height = newH

		go e.recacheOrnament()

		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}
		if e.sceneMgr != nil {
			e.sceneMgr.EndResize(e.id)
		}

		e.SetResizeEnable(false)
		e.SetDragEnable(true)

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	e.elem.SetResizeRedrawInterval(1000)
	e.elem.SetOnResizeRedraw(func(event sprite.ResizeEvent) {
		go e.recacheOrnament()
	})

	e.elem.SetCursorHitTest(func(localX, localY float64) sprite.CursorStyle {
		_, h := e.elem.GetSize()
		if localX >= caseBorder.KPillX && localX <= caseBorder.KPillX+caseBorder.KPillW &&
			localY >= caseBorder.KPillY && localY <= caseBorder.KPillY+caseBorder.KPillH {
			return sprite.CursorPointer
		}
		connX := 5.0
		connY := h / 2
		dx := localX - connX
		dy := localY - connY
		if dx*dx+dy*dy <= 12.0*12.0 {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── SVG helpers ──────────────────────────────────────────────────────

func (e *StatementCase) recacheOrnament() {
	if e.elem == nil || e.ornamentDraw == nil {
		return
	}
	wD, hD := e.elem.GetSizeD()
	_ = e.ornamentDraw.Update(0, 0, wD, hD)
	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", wD.GetInt())
	ornamentSvg.Call("setAttribute", "height", hD.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)
	_ = e.elem.CacheFromSvg(ornamentXml)

	// CacheFromSvg loads the regenerated SVG into a new bitmap asynchronously
	// (it blocks this goroutine on the image's onload — which is why every
	// caller invokes recacheOrnament from a goroutine). When it returns the new
	// bitmap is ready, but nothing has asked the stage to repaint: callers
	// driven by a pointer interaction (e.g. selectCase, clicking the pill) get a
	// redraw "for free" from the event loop, but the inspector's Apply path does
	// not — so the on-stage pill kept showing the previous label (e.g. "case 0")
	// until the next stage redraw. Mark the stage dirty here, once the bitmap is
	// in place, so the new label paints immediately regardless of what triggered
	// the recache.
	//
	// Português: CacheFromSvg carrega o SVG regenerado num bitmap novo de forma
	// assíncrona (bloqueia esta goroutine no onload da imagem — por isso todos os
	// callers chamam recacheOrnament numa goroutine). Quando retorna, o bitmap
	// novo está pronto, mas ninguém pediu repaint do stage: callers vindos de uma
	// interação do ponteiro (ex. selectCase, clicar no pill) ganham um redraw "de
	// graça" pelo event loop, mas o caminho do Apply do inspetor não — então o
	// pill on-stage continuava mostrando o label anterior (ex. "case 0") até o
	// próximo redraw. Marca o stage como dirty aqui, com o bitmap já no lugar,
	// para o novo label pintar na hora, independente do que disparou o recache.
	if e.stage != nil {
		e.stage.MarkDirty()
	}
}

// ── Wire connectors ──────────────────────────────────────────────────

func (e *StatementCase) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "selector"},
		IsOutput:           false,
		AllowedTypes:       caseSelectorTypes,
		AcceptNotConnected: false,
		Locked:             false,
		MaxConnections:     1,
		Label:              "Selector",
		PositionFunc: func() (float64, float64) {
			ex, ey := e.elem.GetPosition()
			_, h := e.elem.GetSize()
			return ex + 5, ey + h/2
		},
	})

	// Register the container's live rect so the wire manager can draw a
	// LabVIEW-style tunnel marker where a wire crosses this container's border.
	// The closure returns the current geometry so the marker tracks drags and
	// resizes; a nil element yields a zero rect, which the renderer skips.
	e.wireMgr.RegisterContainer(e.id, func() (x, y, w, h float64) {
		if e.elem == nil {
			return 0, 0, 0, 0
		}
		ex, ey := e.elem.GetPosition()
		ew, eh := e.elem.GetSize()
		// Report the ornamental-border rect, NOT the outer (mathematical)
		// bounding box. The visible rounded border (caseBorder) is drawn inset
		// by margin = Density(10) from the element bounds, so the tunnel must
		// sit on that line. GetSize returns scaled (world) pixels and
		// Density(10).GetFloat() is the same scaled margin the ornament uses.
		m := rulesDensity.Density(10).GetFloat()
		return ex + m, ey + m, ew - 2*m, eh - 2*m
	})
}

// ── Identity ─────────────────────────────────────────────────────────

func (e *StatementCase) GetDeviceType() string   { return "StatementCase" }
func (e *StatementCase) GetIconName() string     { return "Case" }
func (e *StatementCase) GetIconCategory() string { return "Logic" }

// ── Scene geometry interface ────────────────────────────────────────

func (e *StatementCase) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementCase) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	pad := rulesContainer.IfElsePadding()
	return &scene.Rect{
		X:      x + pad.Left,
		Y:      y + pad.Top,
		Width:  w - pad.Left - pad.Right,
		Height: h - pad.Top - pad.Bottom,
	}
}

func (e *StatementCase) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

func (e *StatementCase) RefreshVisual() {
	e.recacheOrnament()
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// RefreshMembership re-evaluates which contained children belong to the active
// case and re-applies per-case visibility. The workspace calls this from the
// scenegraph's parent-changed hook, so a device dropped into — or dragged out
// of — this container is assigned to the active case (and hidden in the
// others) immediately, instead of leaking into every case until the next cycle
// or drag happened to run assignNewChildren. Mirrors the work the wrapped
// sceneNotify already does on the device's own edits, exposed so an external
// parentage change can trigger it too.
//
// Português: Reavalia quais filhos pertencem ao case ativo e re-aplica a
// visibilidade por case. O workspace chama isto pelo hook de "pai mudou" do
// scenegraph, então um device que entra — ou sai — deste container é atribuído
// ao case ativo (e escondido nos demais) na hora, em vez de vazar para todos os
// cases até o próximo cycle/arraste rodar o assignNewChildren. Espelha o que o
// sceneNotify embrulhado já faz nas edições do próprio device, exposto para uma
// mudança de parentesco externa também disparar.
func (e *StatementCase) RefreshMembership() {
	e.assignNewChildren()
	e.applyCaseVisibility()
	if e.stage != nil {
		e.stage.MarkDirty()
	}
}

// ── Case serialization ───────────────────────────────────────────────

func (e *StatementCase) GetProperties() map[string]interface{} {
	e.assignNewChildren()

	cases := make([]map[string]interface{}, 0, len(e.cases))
	for _, c := range e.cases {
		cases = append(cases, map[string]interface{}{
			"id":        c.id,
			"label":     c.label,
			"matchKind": c.matchKind,
			"values":    c.values,
			"ids":       c.ids,
		})
	}

	return map[string]interface{}{
		"selectorType":  e.selectorType,
		"selectedCase":  e.selectedCase,
		"cases":         cases,
		"defaultCaseId": e.defaultCaseID,
	}
}

// ApplyProperties restores the simple string properties (selectorType,
// selectedCase). The cases array carries []string members and import ID
// remapping, neither of which fit map[string]string, so it is restored via
// RestoreCaseState (see statementCase_caseRestore.go) — exactly as the if/else
// restores branch membership.
//
// Português: Restaura as properties simples (selectorType, selectedCase). O
// array de cases é restaurado via RestoreCaseState (igual ao if/else).
func (e *StatementCase) ApplyProperties(values map[string]string) {
	if t, ok := values["selectorType"]; ok && t != "" {
		e.selectorType = t
	}
	if sc, ok := values["selectedCase"]; ok && sc != "" {
		e.selectedCase = sc
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
		}
	}

	// Cases arrive from the inspector overlay's FieldCaseEditor as a JSON array
	// of case DEFINITIONS — {id,label,matchKind,values,isDefault} — WITHOUT the
	// child membership (ids), which the overlay never edits. Reconcile by id so
	// surviving cases keep their children, mint ids for cases the maker added
	// (they arrive with an empty id), adopt the incoming order (significant for
	// the if/else-if lowering), and move the children of any deleted case onto
	// the default so nothing is orphaned.
	//
	// On scene IMPORT this same method is invoked, but there the "cases"
	// property was flattened with fmt.Sprintf (not JSON), so json.Unmarshal
	// fails and we return early — restoreImportedCases, which alone performs the
	// import ID remapping, remains the authority on import.
	raw, ok := values["cases"]
	if !ok {
		return
	}
	var incoming []caseInspectRow
	if err := json.Unmarshal([]byte(raw), &incoming); err != nil {
		return // not the overlay payload (e.g. the import's Sprintf string)
	}

	oldByID := make(map[string]caseEntry, len(e.cases))
	for _, c := range e.cases {
		oldByID[c.id] = c
	}
	taken := make(map[string]bool, len(e.cases)+len(incoming))
	for id := range oldByID {
		taken[id] = true
	}

	rebuilt := make([]caseEntry, 0, len(incoming))
	kept := make(map[string]bool, len(incoming))
	defaultID := ""
	for _, in := range incoming {
		ce := caseEntry{
			id:        in.ID,
			label:     in.Label,
			matchKind: in.MatchKind,
			values:    append([]string(nil), in.Values...),
			isDefault: in.IsDefault,
		}
		if ce.matchKind == "" {
			if len(ce.values) > 1 {
				ce.matchKind = "isAnyOf"
			} else {
				ce.matchKind = "is"
			}
		}
		if ce.id == "" {
			ce.id = nextCaseID(e.id, taken)
		}
		taken[ce.id] = true
		if prev, found := oldByID[ce.id]; found {
			ce.ids = prev.ids // preserve the surviving case's child membership
		}
		if ce.isDefault {
			defaultID = ce.id
			ce.values = nil // a default/catch-all carries no match values
		}
		kept[ce.id] = true
		rebuilt = append(rebuilt, ce)
	}

	// Guard: never let the overlay leave the device with zero cases (a Case
	// must always have at least one). If that happens, keep the current cases.
	if len(rebuilt) == 0 {
		return
	}

	// Reassign children of removed cases to the default (or the first case when
	// there is no default), so deleting a case never orphans its devices.
	var orphaned []string
	for _, c := range e.cases {
		if !kept[c.id] {
			orphaned = append(orphaned, c.ids...)
		}
	}
	if len(orphaned) > 0 {
		sink := 0
		for i, c := range rebuilt {
			if c.id == defaultID {
				sink = i
				break
			}
		}
		rebuilt[sink].ids = append(rebuilt[sink].ids, orphaned...)
	}

	e.cases = rebuilt
	e.defaultCaseID = defaultID
	if !kept[e.selectedCase] && len(e.cases) > 0 {
		e.selectedCase = e.cases[0].id
	}

	if e.ornamentDraw != nil {
		e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
		// SetCaseLabel only stores the new pill text; the ornament's cached
		// bitmap still shows the previous label until it is regenerated. Without
		// this recache the pill keeps the stale label (e.g. "case 0") right after
		// Apply, refreshing only once selectCase happens to recache it. selectCase
		// already pairs SetCaseLabel with a recache for exactly this reason — do
		// the same here so the pill reflects the new label immediately on Apply.
		//
		// Português: SetCaseLabel só guarda o novo texto do pill; o bitmap em
		// cache do ornamento ainda mostra o label anterior até ser regerado. Sem
		// este recache o pill mantém o label velho (ex. "case 0") logo após o
		// Apply, só atualizando quando o selectCase recacheia. O selectCase já casa
		// SetCaseLabel com um recache por esse motivo — fazemos o mesmo aqui para
		// o pill refletir o novo label imediatamente no Apply.
		go e.recacheOrnament()
	}
	e.applyCaseVisibility()

	// Notify off the click thread, matching the other devices' apply pattern.
	go func() {
		time.Sleep(200 * time.Millisecond)
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	}()
}

// caseInspectRow is the JSON shape exchanged with the inspector overlay's
// FieldCaseEditor (ui/overlay/overlay_case_field.go). It mirrors caseEntry
// minus the child ids (membership), which the overlay does not edit.
//
// Português: Forma JSON trocada com o FieldCaseEditor do overlay — espelha o
// caseEntry sem os ids dos filhos (associação), que o overlay não edita.
type caseInspectRow struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	MatchKind string   `json:"matchKind"`
	Values    []string `json:"values"`
	IsDefault bool     `json:"isDefault"`
}

// nextCaseID returns the first "<baseID>_cN" identifier not present in taken.
// It mints ids for cases the maker adds in the overlay (which arrive with an
// empty id). taken must include the existing case ids AND any id minted earlier
// in the same reconciliation pass, so two freshly-added cases never collide.
//
// Português: Retorna o primeiro id "<baseID>_cN" ausente de taken. Cria ids
// para cases novos do overlay (que chegam com id vazio); taken inclui os ids
// existentes e os já criados na mesma passada, evitando colisão.
func nextCaseID(baseID string, taken map[string]bool) string {
	for n := 0; ; n++ {
		candidate := fmt.Sprintf("%s_c%d", baseID, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// showInspectOverlay opens the cases inspector for this device. Must be called
// in a goroutine — overlay.Show loads help/preview assets asynchronously, the
// same constraint every other device's inspect overlay has.
//
// Português: Abre o inspetor de cases. Deve rodar em goroutine — overlay.Show
// carrega assets de ajuda/preview de forma assíncrona.
// inspectCasesPayload serialises the device's cases into the JSON shape the
// inspector form and the codegen preview both consume (caseInspectRow), and
// resolves the selector type. Shared by GetInspectConfig (which seeds the form
// field) and showInspectOverlay (which sends the same payload to the Preview
// tab's codegen call) so the two never diverge.
//
// Português: Serializa os cases no formato JSON que o form e o preview usam
// (caseInspectRow) e resolve o selectorType. Compartilhado por GetInspectConfig
// e showInspectOverlay para os dois nunca divergirem.
func (e *StatementCase) inspectCasesPayload() (casesJSON, selectorType string) {
	rows := make([]caseInspectRow, 0, len(e.cases))
	for _, c := range e.cases {
		rows = append(rows, caseInspectRow{
			ID:        c.id,
			Label:     c.label,
			MatchKind: c.matchKind,
			Values:    c.values,
			IsDefault: c.isDefault || c.id == e.defaultCaseID,
		})
	}
	casesJSON = "[]"
	if buf, err := json.Marshal(rows); err == nil {
		casesJSON = string(buf)
	} else {
		log.Printf("[Case] %s: inspectCasesPayload marshal failed: %v", e.id, err)
	}

	selectorType = e.selectorType
	if selectorType == "" {
		selectorType = "int"
	}
	return casesJSON, selectorType
}

func (e *StatementCase) showInspectOverlay() {
	cfg := e.GetInspectConfig().(overlay.Config)

	// Live Preview tab. Render this Case as source through the real codegen —
	// the same authority that validates and generates it — fetched here in the
	// goroutine before the overlay opens (showInspectOverlay always runs under
	// `go`, so the blocking codegen round-trip does not freeze the UI). When no
	// preview hook is wired (e.g. before the Workspace injects it) or the run
	// produced nothing, the inspector still opens, just without the Preview tab.
	if e.codegenPreview != nil {
		casesJSON, selectorType := e.inspectCasesPayload()
		if code, diags, monacoLang, ok := e.codegenPreview(e.id, selectorType, casesJSON); ok {
			cfg.Tabs = append(cfg.Tabs, overlay.Tab{
				Label:    translate.T("tabPreview", "Preview"),
				Type:     overlay.TabMonaco,
				Language: monacoLang,
				ReadOnly: true,
				Content:  casePreviewContent(code, diags),
			})
		}
	}

	overlay.Show(cfg)
}

// casePreviewContent assembles the Preview tab body: any diagnostics first, as
// comment lines (so the maker reads them in context above the code), then a
// blank line, then the generated snippet. With no diagnostics it returns the
// snippet unchanged.
//
// Português: Monta o corpo da aba Preview: diagnósticos primeiro, como
// comentários, depois uma linha em branco e o snippet. Sem diagnósticos,
// devolve o snippet sem alteração.
func casePreviewContent(code string, diags []overlay.Diagnostic) string {
	if len(diags) == 0 {
		return code
	}
	var b strings.Builder
	for _, d := range diags {
		b.WriteString("// ")
		b.WriteString(d.Severity)
		b.WriteString(": ")
		b.WriteString(d.Message)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(code)
	return b.String()
}

// GetInspectConfig returns the overlay configuration for the StatementCase and,
// together with ApplyProperties, makes the device satisfy scene.Inspectable —
// which is what lets the inspect overlay open for it at all. The single
// FieldCaseEditor field carries the cases as JSON (definitions only, no child
// membership) and the selector type as ValueType; the edited JSON returns
// through ApplyProperties.
//
// Note on the import interaction: becoming Inspectable means the import path
// now calls ApplyProperties on this device too. That is safe — see the parse
// guard in ApplyProperties — and restoreImportedCases stays the authority for
// case membership on import.
//
// Português: Retorna a config do overlay e, com o ApplyProperties, faz o device
// satisfazer scene.Inspectable (o que permite o overlay abrir). O campo
// FieldCaseEditor leva os cases como JSON (só definições) e o tipo do selector
// em ValueType; o JSON editado volta pelo ApplyProperties. Tornar-se Inspectable
// faz o import chamar ApplyProperties aqui também — é seguro (ver o guard de
// parse) e o restoreImportedCases continua autoridade no import.
func (e *StatementCase) GetInspectConfig() interface{} {
	casesJSON, selectorType := e.inspectCasesPayload()

	return overlay.Config{
		Title: e.id,
		Width: "640px",
		// OnSave routes the edited form values to ApplyProperties, which
		// reconciles the cases and refreshes the stage. Without it the overlay
		// opens and edits visually, but Save is a no-op (renderForm's doSave
		// returns early when OnSave is nil) and e.cases never changes — which
		// is why the device's case dropdown stayed at the two seeded cases.
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
		// OnSaveReopen reopens the overlay after Save so the editor re-seeds
		// from the reconciled cases, crucially picking up the ids minted for
		// newly-added cases (whose rows were saved with an empty id). Without
		// the reopen, a second Save would see those rows as empty-id again,
		// mint fresh ids, and treat the just-added cases as removed — dropping
		// their child membership.
		OnSaveReopen: func() {
			go e.showInspectOverlay()
		},
		// ValidateBeforeSave gates Apply on the real codegen: if the EDITED
		// cases (values["cases"], about to be applied) would generate code with
		// an error-severity diagnostic — e.g. two cases claiming the same
		// switch value, a duplicate label that would not compile — the apply is
		// blocked and the maker is told why. Warnings do not block. The hook
		// may block on the codegen round-trip; the overlay runs it in a
		// goroutine, so the UI stays responsive.
		ValidateBeforeSave: func(values map[string]string) bool {
			return e.validateCasesBeforeApply(values["cases"])
		},
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:       "cases",
						Label:     translate.T("propCases", "Cases"),
						Type:      overlay.FieldCaseEditor,
						Value:     casesJSON,
						ValueType: selectorType,
					},
				},
			},
			{
				// Help is fetched from the server's static help tree (the same
				// /help/devices/<family>/<device>.md convention StatementAdd
				// uses). compFlow → "flow".
				Label:      translate.T("tabHelp", "Help"),
				Type:       overlay.TabMarkdown,
				ContentURL: "/help/devices/flow/statementCase.md",
			},
		},
	}
}

// validateCasesBeforeApply runs the about-to-be-saved cases through the real
// codegen and blocks Apply when it reports any error-severity diagnostic (e.g.
// two cases claiming the same switch value, a duplicate switch label that fails
// to compile). Warnings do not block. Returns true to allow the save.
//
// It fails OPEN: with no preview hook wired, no edited cases to check, or a
// codegen run that produced no result (not logged in, a server hiccup, or the
// user cancelling the validation's progress overlay), the save proceeds —
// validation is an extra guard, not a gate a transient infrastructure problem
// should slam shut. editedCasesJSON is the cases exactly as they are about to
// be applied (values["cases"] from the form), so the gate checks what
// ApplyProperties will store, not the currently-applied state.
//
// Português: Roda os cases prestes a serem salvos pelo codegen real e barra o
// Apply se houver diagnóstico de severidade "error" (ex.: dois cases com o
// mesmo valor de switch — label duplicado que não compila). Avisos não barram.
// Falha ABERTO: sem hook, sem cases, ou sem resultado (sem login, falha de
// servidor, ou cancelamento), o save prossegue. editedCasesJSON é o que o
// ApplyProperties vai gravar (values["cases"]).
func (e *StatementCase) validateCasesBeforeApply(editedCasesJSON string) bool {
	if e.codegenPreview == nil || editedCasesJSON == "" {
		return true
	}
	_, selectorType := e.inspectCasesPayload()
	_, diags, _, ok := e.codegenPreview(e.id, selectorType, editedCasesJSON)
	if !ok {
		return true
	}
	var errs []overlay.Diagnostic
	for _, d := range diags {
		if d.Severity == "error" {
			errs = append(errs, d)
		}
	}
	if len(errs) == 0 {
		return true
	}
	e.showCaseSaveBlocked(errs)
	return false
}

// showCaseSaveBlocked tells the maker which error-severity diagnostics prevent
// the Case from being applied, one per line, over the still-open inspector so
// they can fix the rows and retry.
//
// Português: Informa ao maker quais erros impedem o Case de ser aplicado, um
// por linha, sobre o inspetor ainda aberto, para corrigir e tentar de novo.
func (e *StatementCase) showCaseSaveBlocked(errs []overlay.Diagnostic) {
	var b strings.Builder
	b.WriteString(translate.T("caseSaveBlocked",
		"This Case can't be applied until these are fixed:"))
	for _, d := range errs {
		b.WriteString("\n• ")
		b.WriteString(d.Message)
	}
	overlay.ShowError(translate.T("caseSaveBlockedTitle", "Cannot apply Case"), b.String())
}

// ── Basic getters/setters ────────────────────────────────────────────

func (e *StatementCase) Get() *html.TagSvg    { return nil }
func (e *StatementCase) SetFatherId(_ string) {}

func (e *StatementCase) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}
func (e *StatementCase) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementCase) SetSize(w, h rulesDensity.Density) {
	e.width = w
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(w, h)
	}
}
func (e *StatementCase) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementCase) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}
func (e *StatementCase) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementCase) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementCase) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		y := e.elem.GetYD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}
func (e *StatementCase) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		x := e.elem.GetXD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}
func (e *StatementCase) SetWidth(width rulesDensity.Density) {
	e.width = width
	if e.elem != nil {
		h := e.elem.GetHeightD()
		newW, newH := e.gridAdjust.AdjustCenterD(width, h)
		e.elem.SetSizeD(newW, newH)
	}
}
func (e *StatementCase) SetHeight(height rulesDensity.Density) {
	e.height = height
	if e.elem != nil {
		w := e.elem.GetWidthD()
		newW, newH := e.gridAdjust.AdjustCenterD(w, height)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementCase) GetID() string          { return e.id }
func (e *StatementCase) GetName() string        { return e.name }
func (e *StatementCase) GetInitialized() bool   { return e.initialized }
func (e *StatementCase) GetSelected() bool      { return e.selected }
func (e *StatementCase) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementCase) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementCase) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementCase) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementCase) GetStatus() int         { return e.iconStatus }
func (e *StatementCase) SetStatus(s int)        { e.iconStatus = s }
func (e *StatementCase) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementCase) GetResize() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementCase) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

func (e *StatementCase) SetSelected(selected bool) {
	if e.selectLocked {
		e.selected = false
		return
	}
	e.selected = selected
	if e.elem == nil {
		e.pendingSelected = &selected
		return
	}
	if selected {
		e.elem.SetDragEnable(false)
		e.dragEnabled = false
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		e.ornamentDraw.SetSelected(true)
		go e.recacheOrnament()
	} else {
		e.ornamentDraw.SetSelected(false)
		go e.recacheOrnament()
	}
}

func (e *StatementCase) SetDragEnable(enabled bool) {
	if e.dragLocked {
		e.dragEnabled = false
		return
	}
	e.dragEnabled = enabled
	if e.elem == nil {
		e.pendingDragEnable = &enabled
		return
	}
	if enabled {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		e.selected = false
		e.elem.SetDragEnable(true)
	} else {
		e.elem.SetDragEnable(false)
	}
}

func (e *StatementCase) SetResizeEnable(enabled bool) {
	if e.resizeLocked {
		return
	}
	if e.elem == nil {
		e.pendingResizeEnable = &enabled
		return
	}
	if enabled {
		e.elem.SetDragEnable(false)
		e.dragEnabled = false
		e.selected = false

		if e.sceneMgr != nil {
			parentInner := e.sceneMgr.ParentInnerBBox(e.id)
			if parentInner != nil {
				elemX, elemY := e.elem.GetPosition()
				maxW := (parentInner.X + parentInner.Width) - elemX
				maxH := (parentInner.Y + parentInner.Height) - elemY
				if maxW < 0 {
					maxW = 0
				}
				if maxH < 0 {
					maxH = 0
				}
				e.elem.SetMaxSize(maxW, maxH)
			}
		}

		e.elem.SetResizeEnable(true)
		e.elem.ShowResizeButtons(true)
	} else {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
		e.elem.SetMaxSize(0, 0)
	}
}

func (e *StatementCase) onConnectionClick() {}

// ── Icon ─────────────────────────────────────────────────────────────

func (e *StatementCase) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)

	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())

	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).
		Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)

	xc := rulesIcon.Width / 4
	yc := rulesIcon.Height * 0.15
	wOrn := rulesIcon.Width / 2
	icon := e.ornamentDrawIcon.GetSvg().X(xc.GetInt()).Y(yc.GetInt())
	_ = e.ornamentDrawIcon.Update(0, 0, wOrn, wOrn)

	widthLabel, _ := utilsText.GetTextSize(
		data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight,
		rulesIcon.FontStyle, data.LabelFontSize.GetInt(),
	)
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).FontWeight(rulesIcon.FontWeight).
		FontStyle(rulesIcon.FontStyle).FontSize(data.LabelFontSize.GetInt()).
		Text(data.Label).Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - widthLabel/2).Y(data.LabelY.GetInt())

	svgIcon.Append(hexDraw, icon, label)

	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}
