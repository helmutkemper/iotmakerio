// /ide/devices/compFlow/statementSequence.go
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
// Phase management (cases as PHASES, selectedCase as the ACTIVE phase,
// assignNewChildren, applyCaseVisibility) is orthogonal to the containment
// rule engine and lives entirely inside this file. It mirrors the if/else
// branch machinery one-for-one, generalised from two fixed branches to an
// ordered slice of cases.
//
// Serialization contract with the codegen (server/codegen/graph/builder.go):
//
//	properties = {
//	  "selectedCase":  "<case id>",    // design-time only (active case)
//	  "cases": [ { "id", "label", "matchKind", "values": [...], "ids": [...] }, ... ],
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
	"fmt"
	"log"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/devices"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/hexagon"
	"github.com/helmutkemper/iotmakerio/ornament/caseBorder" // KPill* layout constants (shared pill geometry)
	"github.com/helmutkemper/iotmakerio/ornament/seqBorder"
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

// seqIcon* resolve the Sequence menu icons ONCE from the generated
// FontAwesome registry (Kemper 2026-07-18: signs-post for ＋ Tunnel,
// plus for ＋ Add phase, minus for − Remove last phase). Package-var
// init is safe here: rulesIcon's own init() (which fills the registry)
// completes before this package's vars, per the Go initialization
// order. NEW names — the duplicated-sibling-constant trap of 2026-07-16
// does not apply. Português: Ícones do menu do Sequence resolvidos UMA
// vez do registro gerado. Nomes NOVOS — a armadilha da constante
// duplicada entre irmãos não se aplica.
var (
	seqIconTunnel = rulesIcon.IconByNameOrDefault("signs-post", "gear")
	seqIconPlus   = rulesIcon.IconByNameOrDefault("plus", "gear")
	seqIconMinus  = rulesIcon.IconByNameOrDefault("minus", "gear")
	seqIconPen    = rulesIcon.IconByNameOrDefault("pen", "gear")
)

// caseEntry is one case of a StatementSequence device: the literal selector values
// it matches, the device IDs assigned to it, and whether it is the default.
//
// Português: Um case do StatementSequence — os valores literais que casa, os IDs
// dos devices atribuídos e se é o default.

type StatementSequence struct {
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

	ornamentDraw     *seqBorder.SeqBorder
	ornamentDrawIcon *seqBorder.SeqBorder

	id         string
	gridAdjust grid.Adjust
	iconStatus int

	sceneNotify func()

	// createTunnelFn is injected by the factory (the SceneNotify
	// pattern): devices don't construct devices — the factory does; the
	// menu only pulls the cord. Português: Injetado pelo factory —
	// device não cria device; o menu só puxa a cordinha.
	createTunnelFn func(parentID, side string, x, y float64, natalCase string,
		judge func(cx, cy float64, side, selfID string) (float64, float64)) string

	// tunnelIDs tracks the manual phase-tunnels this Sequence gave birth
	// to — the judge's census. Português: Censo dos túneis manuais que
	// este Sequence pariu — a base do juiz.
	tunnelIDs []string
	// refreshingTunnels is the re-entrancy guard of refreshTunnelViews —
	// the clamping sibling (field freeze forensics 2026-07-18): the
	// refresh is the ONLY new node the phase-view feature adds to the
	// notify graph, so severing re-entry HERE severs any hidden cycle
	// through the hooks by construction. Português: Guarda de
	// reentrância do refreshTunnelViews — irmã do clamping: o refresh é
	// o ÚNICO nó novo no grafo de notificação, então cortar a reentrada
	// AQUI corta qualquer ciclo oculto por construção.
	refreshingTunnels bool

	// Y-delta tracking — twin of the Function's (field 2026-07-19).
	// Português: Rastreio de delta em Y — gêmeo do Function.
	lastOrnY   float64
	hasLastOrn bool
	onRemove   func(id string)
	sceneMgr   *scene.Serializer
	canvasEl   js.Value

	resizeChildBounds *rulesContainer.Rect
	resizeParentInner *rulesContainer.Rect

	dragStartX, dragStartY float64

	// Case management
	// [COMMENT] user comment — appears as `// ` lines above this container's
	// statement in the generated code and in the container's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste container no código gerado e no tooltip de hover.
	comment      string
	selectedCase string      // id of the case currently shown/edited
	cases        []caseEntry // ordered cases

	// codegenPreview renders this Case as source via the real codegen, for the
	// inspect panel's Preview tab. Injected by the factory from the Workspace;
	// nil until wired, in which case the inspector simply opens without a
	// Preview tab.
	codegenPreview CodegenPreviewFunc
}

// CodegenPreviewFunc renders a StatementSequence as source in the project's
// language and returns the cross-case diagnostics. It is the device-side type
// of Workspace.previewCaseCode, injected via SetCodegenPreview so the device
// (in compFlow) need not depend on stageWorkspace. monacoLang is the Monaco
// highlight id for the rendered language; ok is false when no preview was
// produced (not logged in, error, or cancelled).
//
// Português: Renderiza um StatementSequence como código na linguagem do projeto e
// devolve os diagnósticos cruzados. É o tipo, do lado do device, do
// Workspace.previewCaseCode, injetado via SetCodegenPreview — assim o device
// (em compFlow) não precisa depender de stageWorkspace. ok=false quando não
// houve preview.

// ── Dependency injection ─────────────────────────────────────────────

func (e *StatementSequence) SetStage(stage sprite.Stage)       { e.stage = stage }
func (e *StatementSequence) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementSequence) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementSequence) SetWireManager(mgr *wire.Manager)        { e.wireMgr = mgr }
func (e *StatementSequence) SetCanvasEl(el js.Value)                 { e.canvasEl = el }
func (e *StatementSequence) SetResizerButton(rb block.ResizeButton)  { e.resizerButton = rb }
func (e *StatementSequence) SetDraggerButton(_ block.ResizeButton)   {}
func (e *StatementSequence) SetGridAdjust(ga grid.Adjust)            { e.gridAdjust = ga }
func (e *StatementSequence) SetOnRemove(fn func(id string))          { e.onRemove = fn }
func (e *StatementSequence) SetCodegenPreview(fn CodegenPreviewFunc) { e.codegenPreview = fn }

// SetSceneNotify wraps the workspace callback so every scene change also
// re-evaluates case assignment and visibility.
// SetCreateTunnel injects the factory's tunnel constructor (the
// SceneNotify pattern: devices don't construct devices — the factory
// does; the menu only pulls the cord). The natalCase rides in the
// signature so the factory can stamp it BEFORE its NotifyChange fires —
// killing the mid-storm race where refreshTunnelViews saw an unstamped
// tunnel and seated it by the phase-0 fallback (field freeze forensics
// 2026-07-18). Português: Injeta o construtor de túnel do factory — o
// menu só puxa a cordinha. O natal viaja na assinatura para o factory
// carimbar ANTES do NotifyChange — mata a corrida do meio-da-tempestade.
func (e *StatementSequence) SetCreateTunnel(fn func(parentID, side string, x, y float64, natalCase string,
	judge func(cx, cy float64, side, selfID string) (float64, float64)) string) {
	e.createTunnelFn = fn
}

// tunnelJudge nudges a proposed center along the side's axis until it
// overlaps NOTHING — neither sibling manual tunnels nor the automatic
// dots (spec #6: any occlusion hides information). Português: Empurra o
// centro proposto pelo eixo do lado até não sobrepor NADA — nem irmãos
// manuais, nem dots automáticos.
// ornamentRect is the ONE rect that matters for border furniture — the
// visible rounded border, inset Density(10) from the element bounds
// (the same math RegisterContainer feeds the wire layer; field lesson
// 2026-07-17: birthing on the MATHEMATICAL box floated the tunnel
// outside the visible border). Português: O retângulo do ornamento —
// o mesmo que o RegisterContainer entrega; nascer na caixa matemática
// deixou o túnel flutuando fora da borda visível.
func (e *StatementSequence) ornamentRect() (x, y, w, h float64) {
	if e.elem == nil {
		return 0, 0, 0, 0
	}
	ex, ey := e.elem.GetPosition()
	ew, eh := e.elem.GetSize()
	m := rulesDensity.Density(10).GetFloat()
	return ex + m, ey + m, ew - 2*m, eh - 2*m
}

// tunnelJudge slides the proposed center ALONG ITS SIDE'S AXIS (top and
// bottom vary X; left and right vary Y — the 2026-07-17 rewrite; the
// first judge probed both axes and could push a tunnel off its rail)
// until it overlaps nothing — sibling manual tunnels nor automatic dots
// (spec #6) — staying within the ornament edge's extent.
// Português: Desliza o centro NO EIXO DO LADO (topo/base variam X;
// esquerda/direita variam Y) até não sobrepor nada, dentro da extensão
// da borda do ornamento.
func (e *StatementSequence) tunnelJudge(cx, cy float64, side, selfID string) (float64, float64) {
	const minGap = 24.0
	ox, oy, ow, oh := e.ornamentRect()
	if ow <= 0 || oh <= 0 {
		return cx, cy
	}

	var occupied [][2]float64
	if e.wireMgr != nil {
		for _, p := range e.wireMgr.TunnelPointsFor(e.id) {
			occupied = append(occupied, [2]float64{p.X, p.Y})
		}
	}
	// Sibling centers come from the WIRE LAYER now — shells have no
	// stage element (redesign 2026-07-17). Português: Centros dos irmãos
	// vêm da WIRE LAYER — cascas não têm elemento no stage.
	if e.wireMgr != nil {
		for _, id := range e.tunnelIDs {
			if id == selfID {
				continue
			}
			if p, ok := e.wireMgr.ManualTunnelPoint(id); ok {
				occupied = append(occupied, [2]float64{p.X, p.Y})
			}
		}
	}
	// SIDE-AGNOSTIC COLLISION (field 2026-07-18: a third tunnel landed
	// exactly over an out-sibling in the next phase). A sibling's SIDE
	// is ephemeral — every phase flip swaps left↔right and only the
	// along-axis coordinate (Y, for the vertical rails) survives — so
	// two tunnels sharing that coordinate WILL stack in whichever phase
	// poses them on the same border, even if they sit a container-width
	// apart right now. The old two-axis test (dx<gap && dy<gap) saw a
	// left-posed sibling as "far" from a right-border spawn and let the
	// stack happen. Only the along-axis distance decides; automatic
	// crossing points count the same way (conservative: a nudge past an
	// unrelated rail's coordinate is harmless, a stack is not).
	// Português: COLISÃO AGNÓSTICA DE LADO — o LADO de um irmão é
	// efêmero (cada troca de fase inverte esquerda↔direita e só a
	// coordenada do eixo, Y nos trilhos verticais, sobrevive): dois
	// túneis com o mesmo Y VÃO empilhar na fase que os pousar na mesma
	// borda, ainda que hoje estejam a um container de distância. O teste
	// antigo de dois eixos via o irmão da esquerda como "longe" do
	// nascimento à direita e deixava empilhar. Só a distância no eixo
	// decide; pontos automáticos contam do mesmo jeito (conservador:
	// um empurrão a mais é inofensivo, uma pilha não).
	horizontal := side == "top" || side == "bottom"
	collides := func(x, y float64) bool {
		for _, o := range occupied {
			d := y - o[1]
			if horizontal {
				d = x - o[0]
			}
			if d < 0 {
				d = -d
			}
			if d < minGap {
				return true
			}
		}
		return false
	}

	lo, hi := oy, oy+oh
	if horizontal {
		lo, hi = ox, ox+ow
	}
	probe := func(v float64) (float64, float64) {
		if horizontal {
			return v, cy
		}
		return cx, v
	}
	base := cy
	if horizontal {
		base = cx
	}
	if base < lo {
		base = lo
	}
	if base > hi {
		base = hi
	}
	for step := 0.0; step <= hi-lo; step += minGap {
		for _, s := range []float64{base + step, base - step} {
			if s < lo || s > hi {
				continue
			}
			x, y := probe(s)
			if !collides(x, y) {
				return x, y
			}
		}
	}
	return probe(base)
}

// createTunnel births a phase-tunnel (Kemper spec 2026-07-18): ONE
// tunnel per crossing, born as an INPUT on the RIGHT edge of the
// currently selected phase — its NATAL phase. It exists from that phase
// onward: viewing the natal phase shows it right-edge with an inward
// "in" pin; any later phase shows the SAME tunnel left-edge with an
// inward "out" pin; earlier phases don't show it at all. The side is no
// longer a maker choice — the phase relation decides. The natal phase
// is stamped on the SHELL (the serialization vehicle; it cascades into
// the manager record), NOT into cases[].ids: that append was illusory —
// filterExisting wiped it on the next membership pass, since shells
// never gain a scenegraph parent.
//
// Português: Pare o túnel (spec 2026-07-18): UM túnel por cruzamento,
// nascido como ENTRADA na borda DIREITA da fase selecionada — a fase
// NATAL. Ele existe daquela fase em diante: na natal aparece à direita
// com pino "in" para dentro; nas seguintes, o MESMO túnel aparece à
// esquerda com pino "out" para dentro; nas anteriores, não aparece. O
// lado deixou de ser escolha do maker — a relação de fase decide. O
// natal é carimbado na CASCA (veículo de serialização; cascateia para o
// registro do manager), NÃO em cases[].ids: aquele append era ilusório
// — o filterExisting o apagava no passe seguinte, pois cascas nunca
// ganham pai no scenegraph.
func (e *StatementSequence) createTunnel() {
	// Panic shield (field freeze forensics 2026-07-18): under wasm an
	// unrecovered panic kills the Go runtime and the whole IDE freezes
	// with it. If anything on this path blows, the console NAMES it
	// ([TUNNEL-PANIC]) and the app survives. The numbered [TUNNEL-CK]
	// checkpoints that bisected the TunnelPointsFor nil-deref dragon
	// retired on 2026-07-19 after the field confirmed calm; the shield
	// is cheap life insurance and STAYS. Português: Escudo de panic —
	// em wasm, panic não recuperado mata o runtime e congela a IDE;
	// aqui o console NOMEIA o estouro e o app sobrevive. Os checkpoints
	// numerados que bissectaram o dragão se aposentaram em 2026-07-19
	// com a calmaria confirmada em campo; o escudo é seguro de vida
	// barato e FICA.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[TUNNEL-PANIC] createTunnel: %v", r)
		}
	}()
	if e.createTunnelFn == nil || e.elem == nil {
		return
	}
	ox, oy, ow, oh := e.ornamentRect()
	cx, cy := ox+ow, oy+oh*0.25
	cx, cy = e.tunnelJudge(cx, cy, "right", "")
	id := e.createTunnelFn(e.id, "right", cx, cy, e.selectedCase, e.tunnelJudge)
	if id == "" {
		return
	}
	e.tunnelIDs = append(e.tunnelIDs, id)
	e.refreshTunnelViews()
	go e.recacheOrnament()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

func (e *StatementSequence) SetSceneNotify(fn func()) {
	e.sceneNotify = func() {
		e.assignNewChildren()
		e.applyCaseVisibility()
		if fn != nil {
			fn()
		}
	}
}

// ── Lifecycle ────────────────────────────────────────────────────────

func (e *StatementSequence) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementSequence) Remove() {
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

func (e *StatementSequence) GetKind() scenegraph.Kind { return scenegraph.KindComplex }

func (e *StatementSequence) GetContainerPadding() rulesContainer.Padding {
	// Reuse the if/else container padding: the case container has the same
	// rounded-rectangle shape and inner margins. A dedicated CasePadding can
	// be introduced later if the visuals diverge.
	return rulesContainer.IfElsePadding()
}

// ── Init ─────────────────────────────────────────────────────────────

func (e *StatementSequence) Init() (err error) {
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
	if len(e.cases) == 0 {
		// Two starter phases; labels are cosmetic — the pill and menu
		// render phases BY INDEX ("phase N"). Português: Duas fases
		// iniciais; rótulos são cosméticos — a UI renderiza por índice.
		e.cases = []caseEntry{
			{id: e.id + "_c0", label: "phase 0", matchKind: "is", values: []string{"0"}},
			{id: e.id + "_c1", label: "phase 1", matchKind: "is", values: []string{"1"}},
		}
	}
	if e.selectedCase == "" {
		e.selectedCase = e.cases[0].id
	}

	e.ornamentDraw = new(seqBorder.SeqBorder)
	e.ornamentDrawIcon = new(seqBorder.SeqBorder)

	// Sequence (2026-07-16): NO selector pin — the order device is
	// semantically transparent and takes no data input; the pill (active-
	// phase picker) is the whole left-edge UI. Português: SEM pino de
	// seletor — a pill é toda a UI da borda.
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
func (e *StatementSequence) caseIndexByID(id string) int {
	for i := range e.cases {
		if e.cases[i].id == id {
			return i
		}
	}
	return -1
}

// activeCaseLabel returns the pill text for the currently-selected
// case — the MAKER'S NAME first, position as the fallback. The old
// doctrine ("the position IS the name", Slice-2 era) predated the
// rename feature and made both painters ignore c.label — a rename
// wrote the data perfectly and the screen never showed it (field
// forensics 2026-07-19: "[RENAME] applied" in the console, "nada
// mudou" on the stage). Default labels are literal "phase N" strings
// kept position-synced by renumberDefaultLabels, so unrenamed phases
// still read exactly as before. Português: O texto da pílula — o NOME
// DO MAKER primeiro, posição como fallback. A doutrina antiga ("a
// posição É o nome") era pré-rename e fazia os dois pintores ignorarem
// c.label — o rename gravava e a tela nunca mostrava. Rótulos padrão
// são "phase N" literais mantidos em sincronia pelo
// renumberDefaultLabels, então fases não renomeadas leem igual.
func (e *StatementSequence) activeCaseLabel() string {
	idx := e.caseIndexByID(e.selectedCase)
	if idx < 0 {
		return "phase"
	}
	if l := e.cases[idx].label; l != "" {
		return l
	}
	return fmt.Sprintf("phase %d", idx)
}

// caseMenuLabel — the menu's painter, same label-first doctrine as
// activeCaseLabel above. Português: O pintor do menu — mesma doutrina
// nome-primeiro do activeCaseLabel.
func (e *StatementSequence) caseMenuLabel(c caseEntry) string {
	if c.label != "" {
		return c.label
	}
	if i := e.caseIndexByID(c.id); i >= 0 {
		return fmt.Sprintf("phase %d", i)
	}
	return c.id
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
func (e *StatementSequence) caseSelectMenuItems() []contextMenu.Item {
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

	// The Sequence's add/remove live HERE, on the pill — the v1 has no
	// inspect overlay (mirrors Loop). Removing keeps at least one phase;
	// the removed phase's children move to the new last phase (the
	// server-side stray policy is phase 0; device-side we keep them in
	// the nearest surviving phase so nothing jumps visually).
	// Português: ＋/− moram na pill — v1 sem overlay (espelha o Loop).
	// ONE tunnel entry (Kemper spec 2026-07-18, superseding the one-
	// session in/out pair): the side is no longer a choice — a tunnel is
	// born as an INPUT on the right edge of the phase it was created in,
	// and the SAME tunnel shows as an OUTPUT on the left edge in every
	// later phase. Português: UMA entrada (supersede o par in/out de uma
	// sessão): o lado deixou de ser escolha — nasce como ENTRADA à
	// direita da fase de criação e vira SAÍDA à esquerda nas seguintes.
	items = append(items, contextMenu.Item{
		ID:              "seq_tunnel",
		Label:           translate.T("seqAddTunnel", "＋ Tunnel"),
		FontAwesomePath: seqIconTunnel.Path,
		ViewBox:         seqIconTunnel.ViewBox,
		HelpFallback:    "Create a data tunnel: born as an input on the right edge of this phase, it becomes an output on the left edge in the following phases.",
		OnClick:         func() { e.createTunnel() },
	})
	items = append(items, contextMenu.Item{
		ID:              "seq_add",
		Label:           translate.T("seqAddPhase", "＋ Add phase"),
		FontAwesomePath: seqIconPlus.Path,
		ViewBox:         seqIconPlus.ViewBox,
		HelpFallback:    "Append a new phase after the last one.",
		OnClick:         func() { e.addPhase() },
	})
	items = append(items, contextMenu.Item{
		ID:              "seq_ins_before",
		Label:           translate.T("seqInsertBefore", "＋ Insert phase before"),
		FontAwesomePath: seqIconPlus.Path,
		ViewBox:         seqIconPlus.ViewBox,
		HelpKey:         "helpSeqInsertBefore",
		HelpFallback:    "Adds an empty phase immediately before the phase on screen.",
		OnClick:         func() { e.insertPhaseAt(e.caseIndexByID(e.selectedCase)) },
	})
	items = append(items, contextMenu.Item{
		ID:              "seq_ins_after",
		Label:           translate.T("seqInsertAfter", "＋ Insert phase after"),
		FontAwesomePath: seqIconPlus.Path,
		ViewBox:         seqIconPlus.ViewBox,
		HelpKey:         "helpSeqInsertAfter",
		HelpFallback:    "Adds an empty phase immediately after the phase on screen.",
		OnClick:         func() { e.insertPhaseAt(e.caseIndexByID(e.selectedCase) + 1) },
	})
	items = append(items, contextMenu.Item{
		ID:              "seq_rename",
		Label:           translate.T("seqRenamePhase", "Rename phase"),
		FontAwesomePath: seqIconPen.Path,
		ViewBox:         seqIconPen.ViewBox,
		HelpKey:         "helpSeqRenamePhase",
		HelpFallback:    "Gives the phase on screen a meaningful name (display only).",
		OnClick:         func() { go e.renameSelectedPhase() },
	})
	if len(e.cases) > 1 {
		items = append(items, contextMenu.Item{
			ID:              "seq_del",
			Label:           translate.T("seqRemovePhase", "− Remove this phase"),
			FontAwesomePath: seqIconMinus.Path,
			ViewBox:         seqIconMinus.ViewBox,
			HelpFallback:    "Removes the phase on screen; its devices move to the neighbor phase.",
			OnClick:         func() { e.removePhaseAt(e.caseIndexByID(e.selectedCase)) },
		})
	}
	return items
}

// addPhase appends a fresh phase and selects it for editing.
// Português: Anexa uma fase nova e a seleciona para edição.
func (e *StatementSequence) addPhase() {
	e.assignNewChildren()
	taken := map[string]bool{}
	for _, c := range e.cases {
		taken[c.id] = true
	}
	idx := len(e.cases)
	e.cases = append(e.cases, caseEntry{
		id:        nextCaseID(e.id, taken),
		label:     fmt.Sprintf("phase %d", idx),
		matchKind: "is",
		values:    []string{fmt.Sprintf("%d", idx)},
	})
	e.selectCase(e.cases[idx].id)
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// insertPhaseAt splices a fresh phase at idx (0 = before the first;
// len = append) and selects it. Ids come from nextCaseID — position
// never mints an id, so tunnels' natal references and the removal sets
// stay valid across any surgery. Default "phase N" labels renumber to
// match their new positions; custom names are preserved.
// Português: Insere uma fase nova em idx e a seleciona. Ids vêm do
// nextCaseID — posição nunca cunha id, então as referências de natal e
// os conjuntos de remoção dos túneis seguem válidos em qualquer
// cirurgia. Rótulos padrão "phase N" renumeram; nomes customizados são
// preservados.
func (e *StatementSequence) insertPhaseAt(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx > len(e.cases) {
		idx = len(e.cases)
	}
	e.assignNewChildren()
	taken := map[string]bool{}
	for _, c := range e.cases {
		taken[c.id] = true
	}
	fresh := caseEntry{
		id:        nextCaseID(e.id, taken),
		label:     fmt.Sprintf("phase %d", idx),
		matchKind: "is",
		values:    []string{fmt.Sprintf("%d", idx)},
	}
	e.cases = append(e.cases, caseEntry{})
	copy(e.cases[idx+1:], e.cases[idx:])
	e.cases[idx] = fresh
	e.renumberDefaultLabels()
	e.selectCase(fresh.id)
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// removePhaseAt removes the phase at idx. Its member devices move to
// the NEIGHBOR phase (previous; next when removing the first) — never
// deleted: overlaps there show as honest conflicts and the maker
// redistributes, the same doctrine as the membership self-heal. Tunnels
// whose NATAL was the removed phase are re-stamped to the same neighbor
// (manager record + shell, so persistence follows), and the dead phase
// id is scrubbed from every tunnel's removal set — the pre-tunnel-era
// removeLastPhase silently orphaned natal references; this fixes that
// path too. Português: Remove a fase em idx. Os membros vão para a fase
// VIZINHA (anterior; a próxima ao remover a primeira) — nunca apagados:
// sobreposições viram conflitos honestos e o maker redistribui, a mesma
// doutrina da autocura de filiação. Túneis com NATAL na fase removida
// são re-carimbados para a mesma vizinha (registro + casca), e o id
// morto sai de todo conjunto de remoção — o removeLastPhase da era
// pré-túnel orfanava referências de natal em silêncio; isto cura esse
// caminho também.
func (e *StatementSequence) removePhaseAt(idx int) {
	if len(e.cases) < 2 || idx < 0 || idx >= len(e.cases) {
		return
	}
	e.assignNewChildren()
	removedID := e.cases[idx].id
	neighbor := idx - 1
	if neighbor < 0 {
		neighbor = idx + 1
	}
	neighborID := e.cases[neighbor].id
	e.cases[neighbor].ids = append(e.cases[neighbor].ids, e.cases[idx].ids...)

	if e.wireMgr != nil {
		for _, tid := range e.wireMgr.ManualTunnelIDsFor(e.id) {
			if e.wireMgr.ManualTunnelNatal(tid) == removedID {
				e.wireMgr.SetManualTunnelNatal(tid, neighborID)
				if e.sceneMgr != nil {
					if t, ok := e.sceneMgr.FindDevice(tid).(*StatementTunnel); ok {
						t.SetNatalCase(neighborID)
					}
				}
			}
			e.wireMgr.ClearManualTunnelRemovedCase(tid, removedID)
		}
	}

	e.cases = append(e.cases[:idx], e.cases[idx+1:]...)
	e.renumberDefaultLabels()
	if e.selectedCase == removedID {
		e.selectCase(neighborID)
	} else {
		e.applyCaseVisibility()
	}
	if e.ornamentDraw != nil {
		e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
	}
	go e.recacheOrnament()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// renumberDefaultLabels keeps machine-given "phase N" labels aligned
// with their positions after surgery; anything the maker renamed is
// left untouched. Português: Realinha rótulos automáticos "phase N" com
// as posições após cirurgia; nomes dados pelo maker ficam intactos.
func (e *StatementSequence) renumberDefaultLabels() {
	for i := range e.cases {
		rest, ok := strings.CutPrefix(e.cases[i].label, "phase ")
		if !ok {
			continue
		}
		if _, err := strconv.Atoi(rest); err != nil {
			continue
		}
		e.cases[i].label = fmt.Sprintf("phase %d", i)
	}
}

// renameSelectedPhase opens the house form (the Function-rename
// pattern) for the phase on screen. Labels are display-only — the
// server's CaseDef.Label carries no execution meaning — so this is
// pure legibility. Português: Abre o formulário da casa para a fase em
// cena. Rótulos são só exibição — legibilidade pura.
func (e *StatementSequence) renameSelectedPhase() {
	idx := e.caseIndexByID(e.selectedCase)
	if idx < 0 {
		return
	}
	caseID := e.cases[idx].id
	overlay.Show(overlay.Config{
		Title: translate.T("seqRenameTitle", "Rename phase"),
		Width: "420px",
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:         "label",
						Label:       translate.T("seqPhaseLabel", "Phase name"),
						Type:        overlay.FieldText,
						Value:       e.cases[idx].label,
						Placeholder: translate.T("seqPhasePlaceholder", "read sensors"),
					},
				},
			},
		},
		OnSave: func(values map[string]string) {
			v := strings.TrimSpace(values["label"])
			if v == "" {
				return
			}
			i := e.caseIndexByID(caseID)
			if i < 0 {
				return
			}
			e.cases[i].label = v
			if e.ornamentDraw != nil {
				e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
			}
			go e.recacheOrnament()
			if e.sceneNotify != nil {
				e.sceneNotify()
			}
		},
	})
}

// removeLastPhase drops the last phase, moving its children to the new
// last phase so no device is orphaned on the device side.
// Português: Remove a última fase; os filhos migram para a nova última.
// removeLastPhase — kept as a thin wrapper for any external caller;
// removePhaseAt carries the tunnel care the original lacked.
// Português: Mantida como casca fina; o removePhaseAt carrega o cuidado
// com túneis que a original não tinha.
func (e *StatementSequence) removeLastPhase() {
	e.removePhaseAt(len(e.cases) - 1)
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
func (e *StatementSequence) selectCase(id string) {
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

// isCaseSelectorType reports whether t is a type the Case selector accepts.
//
// Português: Informa se t é um tipo aceito pela entrada "selector" do Case.

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
func (e *StatementSequence) snapCasesToBool() {
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
	if e.caseIndexByID(e.selectedCase) < 0 {
		e.selectedCase = e.cases[0].id
	}
}

// assignNewChildren assigns newly-contained children to the selected case and
// drops IDs that left the container. Mirrors the if/else branch assignment,
// generalised to N cases.
//
// Português: Atribui novos filhos ao case selecionado e remove os que saíram.
func (e *StatementSequence) assignNewChildren() {
	// Delegates to the shared resilient pass — see maintainCaseMembership
	// in util.go for the 2026-07-18 collapse forensics and the doctrine.
	// Português: Delega ao passe resiliente compartilhado (util.go).
	maintainCaseMembership(e.sceneMgr, e.id, e.GetInnerBBox(),
		e.cases, e.caseIndexByID(e.selectedCase))
}

// applyCaseVisibility shows the selected case's children and hides every other
// case's children, mirroring the if/else branch visibility (show one, hide the
// rest). Hidden devices also leave the wire layer and collision so the inactive
// cases show no orphan wires and are not cross-case connect targets.
//
// Português: Mostra os filhos do case selecionado e esconde os dos demais.
func (e *StatementSequence) applyCaseVisibility() {
	if e.stage == nil {
		return
	}

	// LabVIEW STACKED-FRAME OCCLUSION, restored (field report 2026-07-18,
	// "o label mudou, mas nada aconteceu"). The G2-era inversion — every
	// phase always visible — existed because tunnel markers were minted
	// by WIRES back then: hiding a phase hid the wire, and the marker
	// died with it (field report 2026-07-16, "quando eu vou ver o
	// túnel?"). G3 dissolved that reason: the manual tunnel is a wire-
	// layer record, drawn from the manager every frame, and its filled
	// state (hasWireOnElement) ignores hidden flags — the square
	// persists, violet and filled, across every phase flip while the
	// feed wire hides with its source. Both field reports are now
	// satisfied at once. The body below mirrors the Case's proven pass.
	//
	// Português: Oclusão de quadros estilo LabVIEW, restaurada (campo
	// 2026-07-18, "o label mudou, mas nada aconteceu"). A inversão da
	// G2 — todas as fases sempre visíveis — existia porque o marcador
	// era cunhado pelo FIO: esconder a fase matava o marcador (campo
	// 2026-07-16, "quando eu vou ver o túnel?"). A G3 dissolveu o
	// motivo: o túnel manual é registro da wire layer, desenhado a cada
	// frame, e o preenchido (hasWireOnElement) ignora hidden — o
	// quadrado persiste, violeta e cheio, em toda troca de fase,
	// enquanto o fio de alimentação some com a fonte. Os dois relatos
	// de campo ficam satisfeitos ao mesmo tempo. Corpo espelha o Case.
	for i := range e.cases {
		show := e.cases[i].id == e.selectedCase
		for _, id := range e.cases[i].ids {
			// Border furniture is TOTALLY exempt — not just spatially.
			// A tunnel id must never be touched in ANY dimension here:
			// visual (the shell has no elem), wire-hidden (would cull
			// its feed and drop it from connect targeting), or spatial
			// (SetHidden(false) revived the straddle conflict, field
			// 2026-07-17). Hidden-from-birth shells never gain a parent,
			// so they never land in ids — this is the seat-belt.
			// Português: Móvel de borda é TOTALMENTE isento — nunca
			// tocar túnel em nenhuma dimensão: visual (casca sem elem),
			// wire-hidden (cortaria o feed e o tiraria do connect) ou
			// espacial (SetHidden(false) ressuscitou o straddle).
			// Cascas nunca ganham pai, então nunca caem em ids — isto
			// é o cinto de segurança.
			if strings.HasPrefix(id, "tunnel") {
				continue
			}
			if elem, found := e.stage.GetElement(id); found {
				elem.SetVisible(show)
			}
			if warnElem, found := e.stage.GetElement(id + "_warning"); found {
				if !show {
					warnElem.SetVisible(false)
				}
			}
			// Inactive phases leave the wire layer and collision (flag
			// flips, reversed on show) — no orphan wires, no cross-phase
			// connect targets. Português: Fases inativas saem da wire
			// layer e da colisão — sem fio órfão, sem alvo entre fases.
			if e.wireMgr != nil {
				e.wireMgr.SetElementHidden(id, !show)
			}
			if e.sceneMgr != nil {
				e.sceneMgr.SetHidden(id, !show)
			}
		}
	}

	// Tunnels are phase-view furniture (Kemper spec 2026-07-18): every
	// path that changes which phase is on screen already funnels through
	// this function, so the tunnel re-seat lives here too — one choke
	// point, zero extra call sites. Português: Todo caminho que troca a
	// fase em cena já afunila aqui; o re-assento dos túneis mora junto.
	e.refreshTunnelViews()
}

// refreshTunnelViews re-seats every manual tunnel of this Sequence for
// the phase currently on screen (Kemper spec 2026-07-18). Relation to
// the tunnel's NATAL phase decides everything: earlier → the tunnel does
// not exist yet (phase-hidden); natal → INPUT on the RIGHT border, pin
// pointing inward-left; later → OUTPUT on the LEFT border, pin pointing
// inward-right. The maker's Y (drag along the rail) is preserved across
// side flips — only X and the role change; distinct Ys on one rail stay
// distinct on the other, so the creation-time judge spacing survives the
// flip. Enumeration comes from the MANAGER (restored tunnels never pass
// through createTunnel, so e.tunnelIDs alone would miss them). Old
// scenes without a natal stamp fall back to phase 0: visible everywhere,
// input on the first phase — the least surprising reading of a file
// that predates the concept.
//
// Português: Re-assenta cada túnel manual para a fase em cena. A
// relação com a fase NATAL decide tudo: anterior → o túnel ainda não
// existe; natal → ENTRADA na borda DIREITA, pino para dentro; posterior
// → SAÍDA na borda ESQUERDA, pino para dentro. O Y do maker sobrevive à
// troca de lado — só X e papel mudam. A enumeração vem do MANAGER
// (túneis restaurados não passam pelo createTunnel). Cena antiga sem
// natal cai na fase 0.
func (e *StatementSequence) refreshTunnelViews() {
	// Re-entrancy guard — the clamping sibling: this function is the
	// only node the phase-view feature added to the notify graph
	// (applyCaseVisibility funnels here, and the workspace hooks funnel
	// into applyCaseVisibility), so if any hidden cycle exists through
	// RefreshAll/OnParentChanged/RefreshMembership, it necessarily
	// passes through HERE — and re-entry dies at this door. Panic
	// shield for the same field reason as createTunnel. Português:
	// Guarda de reentrância — irmã do clamping: esta função é o único
	// nó novo no grafo de notificação; qualquer ciclo oculto passa por
	// AQUI, e a reentrada morre nesta porta. Escudo de panic pelo mesmo
	// motivo de campo do createTunnel.
	if e.refreshingTunnels {
		return
	}
	e.refreshingTunnels = true
	defer func() {
		e.refreshingTunnels = false
		if r := recover(); r != nil {
			log.Printf("[TUNNEL-PANIC] refreshTunnelViews: %v", r)
		}
	}()
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	selIdx := e.caseIndexByID(e.selectedCase)
	if selIdx < 0 {
		return
	}
	ox, oy, ow, oh := e.ornamentRect()
	dy := 0.0
	if e.hasLastOrn {
		dy = oy - e.lastOrnY
	}
	e.lastOrnY, e.hasLastOrn = oy, true
	for _, id := range e.wireMgr.ManualTunnelIDsFor(e.id) {
		natalIdx := e.caseIndexByID(e.wireMgr.ManualTunnelNatal(id))
		if natalIdx < 0 {
			natalIdx = 0
		}
		p, ok := e.wireMgr.ManualTunnelPoint(id)
		if !ok {
			continue
		}
		y := p.Y + dy
		if y < oy {
			y = oy
		}
		if y > oy+oh {
			y = oy + oh
		}
		side, px, role, hidden := "right", ox+ow, "in", false
		switch {
		case selIdx < natalIdx:
			// Pre-natal: the tunnel does not exist yet. Keep the natal
			// (right/in) pose so un-hiding never jumps. Português:
			// Pré-natal: ainda não existe; pose natal mantida.
			hidden = true
		case selIdx == natalIdx:
			// Natal defaults set above. Português: Padrões da natal.
		default:
			// Post-natal: the maker may have HIDDEN this exit here
			// ("remove dessa fase" / "remove das próximas fases").
			// SELF-HEAL: a WIRED phase can never stay hidden — hiding a
			// live connection would leave data flowing invisibly, the
			// exact state the unhideable-when-wired rule forbids. If a
			// removal slipped in anyway (a save from before the rule,
			// or any census hole like the inverted-wire one of
			// 2026-07-17), the mark is dropped ON SIGHT and the tunnel
			// shows. Hidden wired phases heal when visited; the menu
			// gates prevent new ones. Português: Pós-natal — pode estar
			// OCULTA aqui. AUTOCURA: fase LIGADA nunca fica oculta —
			// esconder conexão viva deixaria dado fluindo invisível. Se
			// uma remoção passou (save anterior à regra, ou furo de
			// censo como o do fio invertido de 2026-07-17), a marca cai
			// AO SER VISTA e o túnel aparece. Fases ligadas ocultas se
			// curam ao serem visitadas; os portões do menu impedem
			// novas.
			side, px, role = "left", ox, "out"
			hidden = e.wireMgr.ManualTunnelRemovedHas(id, e.selectedCase)
			if hidden && e.tunnelConnectedInPhase(id, selIdx) {
				e.wireMgr.ClearManualTunnelRemovedCase(id, e.selectedCase)
				hidden = false
			}
		}
		e.wireMgr.SetManualTunnelView(id, side, px, y, role, hidden)
		// A phase-hidden tunnel's WIRES must vanish with it. PhaseHidden
		// alone hid the square and gated the connectors, but the wires
		// kept drawing from the parked anchors — and the parked "out"
		// anchor sits OUTSIDE the border, so the router minted a border
		// crossing and the renderer painted a phantom square with a wire
		// looping around the invisible tunnel (field 2026-07-18, "os
		// túneis aparecem nela e aparecem wires que não existem,
		// circulando o túnel"). Mirroring the flag into the element-
		// hidden set reuses the proven wire cull — including wires to
		// devices OUTSIDE the sequence. refreshTunnelViews is the ONLY
		// writer of this flag for tunnel ids (applyCaseVisibility's
		// seat-belt skips them). Português: Os FIOS de um túnel oculto
		// por fase somem com ele. Só o PhaseHidden escondia o quadrado e
		// travava conectores, mas os fios seguiam desenhando das âncoras
		// estacionadas — e a âncora "out" estacionada fica FORA da
		// borda, então o roteador cunhava uma travessia e o renderer
		// pintava um quadrado fantasma com fio dando a volta no túnel
		// invisível (campo 2026-07-18). Espelhar a flag no conjunto de
		// elementos escondidos reusa o cull comprovado — inclusive fios
		// para devices FORA do sequence. refreshTunnelViews é o ÚNICO
		// escritor desta flag para ids de túnel (o cinto do
		// applyCaseVisibility os pula).
		e.wireMgr.SetElementHidden(id, hidden)
		e.wireMgr.RecalculateForElement(id)
	}
}

// ── Tunnel phase-hiding (Kemper 2026-07-18: "assim um túnel pode ser
// ocultado") ─────────────────────────────────────────────────────────
//
// A tunnel's exit can be hidden per phase. Semantics chosen and
// declared for the field to veto: the NATAL phase is the irremovable
// HOME (hiding it too would strand the tunnel — it is the handle the
// maker restores from); "próximas fases" means the phases AFTER the one
// on screen (on the natal view, that is every post-natal phase); the
// restore clears EVERYTHING ("retorna todos"); and a phase created
// AFTER a removal is new territory — the tunnel shows there.
//
// Português: A saída de um túnel pode ser ocultada por fase. Semânticas
// escolhidas e declaradas para veto de campo: a fase NATAL é o LAR
// irremovível (ocultá-la também deixaria o túnel inalcançável — é a
// alça de restauração); "próximas fases" = as fases APÓS a que está em
// cena (na natal, todas as pós-natais); o restore limpa TUDO ("retorna
// todos"); e fase criada DEPOIS de uma remoção é território novo — o
// túnel aparece nela.

// TunnelRemoveFromCurrentPhase hides the tunnel's exit in the phase on
// screen. Refuses on the natal phase (the home) and on a WIRED phase
// (Kemper 2026-07-18: "eu não deveria poder ocultar um túnel que tem
// ligação de wire na fase onde estou" — hiding a live connection would
// leave data flowing invisibly). The menu only offers it when
// TunnelCanRemoveFromCurrentPhase says so; these are the belts.
// Português: Oculta a saída na fase em cena. Recusa na natal (o lar) e
// em fase LIGADA — ocultar conexão viva deixaria dado fluindo
// invisível. O menu só oferece quando o Can* permite; isto é o cinto.
func (e *StatementSequence) TunnelRemoveFromCurrentPhase(tunnelID string) {
	if e.wireMgr == nil || !e.TunnelCanRemoveFromCurrentPhase(tunnelID) {
		return
	}
	e.wireMgr.AddManualTunnelRemovedCases(tunnelID, e.selectedCase)
	e.refreshTunnelViews()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// TunnelRemoveFromNextPhases hides the tunnel's exit in every phase
// AFTER the one on screen — SKIPPING wired phases (the unhideable-when-
// wired rule) and phases already removed. Declutters the unused exits,
// keeps the live ones by law. Português: Oculta a saída em toda fase
// APÓS a em cena — PULANDO fases ligadas (regra do inocultável) e as já
// removidas. Limpa as saídas sem uso, preserva as vivas por lei.
func (e *StatementSequence) TunnelRemoveFromNextPhases(tunnelID string) {
	if e.wireMgr == nil {
		return
	}
	next := e.hideableNextPhases(tunnelID)
	if len(next) == 0 {
		return
	}
	e.wireMgr.AddManualTunnelRemovedCases(tunnelID, next...)
	e.refreshTunnelViews()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// TunnelRestorePhases clears every removal — "retorna para as próximas
// fases retorna todos". Português: Limpa todas as remoções — retorna
// todos.
func (e *StatementSequence) TunnelRestorePhases(tunnelID string) {
	if e.wireMgr == nil {
		return
	}
	e.wireMgr.SetManualTunnelRemovedCases(tunnelID, nil)
	e.refreshTunnelViews()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// TunnelHasRemovals reports whether any phase removal is active — the
// menu shows the restore entry when true. Português: Diz se há remoção
// ativa — o menu mostra o restore quando sim.
func (e *StatementSequence) TunnelHasRemovals(tunnelID string) bool {
	if e.wireMgr == nil {
		return false
	}
	return len(e.wireMgr.ManualTunnelRemovedCases(tunnelID)) > 0
}

// TunnelCanRemoveFromCurrentPhase — the menu gate for "remove dessa
// fase": only on a post-natal view AND only when the tunnel has no wire
// landing in this phase. Português: Portão do menu — só em visão
// pós-natal E sem fio pousando nesta fase.
func (e *StatementSequence) TunnelCanRemoveFromCurrentPhase(tunnelID string) bool {
	if e.wireMgr == nil {
		return false
	}
	selIdx := e.caseIndexByID(e.selectedCase)
	natalIdx := e.caseIndexByID(e.wireMgr.ManualTunnelNatal(tunnelID))
	if natalIdx < 0 {
		natalIdx = 0
	}
	if selIdx <= natalIdx {
		return false
	}
	return !e.tunnelConnectedInPhase(tunnelID, selIdx)
}

// TunnelCanRemoveFromNextPhases — the menu gate for "remove das
// próximas": at least one later phase must be hideable (not wired, not
// already removed). Português: Portão do menu — ao menos uma fase
// posterior ocultável.
func (e *StatementSequence) TunnelCanRemoveFromNextPhases(tunnelID string) bool {
	if e.wireMgr == nil {
		return false
	}
	return len(e.hideableNextPhases(tunnelID)) > 0
}

// hideableNextPhases lists the case ids after the phase on screen that
// the unhideable-when-wired rule allows hiding: not wired in that
// phase, not already removed. Português: Fases após a em cena que a
// regra permite ocultar: sem fio nelas, ainda não removidas.
func (e *StatementSequence) hideableNextPhases(tunnelID string) []string {
	selIdx := e.caseIndexByID(e.selectedCase)
	if selIdx < 0 {
		return nil
	}
	var out []string
	for i := selIdx + 1; i < len(e.cases); i++ {
		if e.wireMgr.ManualTunnelRemovedHas(tunnelID, e.cases[i].id) {
			continue
		}
		if e.tunnelConnectedInPhase(tunnelID, i) {
			continue
		}
		out = append(out, e.cases[i].id)
	}
	return out
}

// tunnelConnectedInPhase reports whether any consumer wired to the
// tunnel's "out" lands in the given phase. A consumer that belongs to
// NO phase (a device outside the Sequence, or a chained tunnel — cascas
// have no membership) counts as connected in EVERY phase: its wire is
// visible wherever the tunnel is, so the conservative reading of the
// rule blocks hiding anywhere. Membership is refreshed first so the
// verdict never reads a stale census. Português: Diz se algum
// consumidor ligado ao "out" pousa na fase dada. Consumidor sem fase
// (device fora do Sequence, ou túnel encadeado — cascas não têm
// membership) conta como ligado em TODAS: seu fio aparece onde o túnel
// estiver, então a leitura conservadora bloqueia ocultar em qualquer
// uma. A membership é atualizada antes do veredito.
func (e *StatementSequence) tunnelConnectedInPhase(tunnelID string, caseIdx int) bool {
	consumers := e.wireMgr.ManualTunnelConsumers(tunnelID)
	if len(consumers) == 0 || caseIdx < 0 || caseIdx >= len(e.cases) {
		return false
	}
	e.assignNewChildren()
	inPhase := make(map[string]bool, len(e.cases[caseIdx].ids))
	for _, id := range e.cases[caseIdx].ids {
		inPhase[id] = true
	}
	anyPhase := make(map[string]bool)
	for _, c := range e.cases {
		for _, id := range c.ids {
			anyPhase[id] = true
		}
	}
	for _, c := range consumers {
		if inPhase[c] || !anyPhase[c] {
			return true
		}
	}
	return false
}

// ── Hex menu items ───────────────────────────────────────────────────

// getBodyMenuItems returns body context menu items for this container.
// Order: Delete first (canonical per D4), Resize toggle,
// Forward/Backward z-ordering. No Inspect in v1 — phases live on the
// pill (mirrors Loop). Português: Delete, Resize, Forward, Backward;
// sem Inspect na v1 — fases moram na pill (espelha o Loop).
func (e *StatementSequence) getBodyMenuItems() []contextMenu.Item {
	// Phase actions live on the pill AND here (field lesson 2026-07-16:
	// the attached screenshot showed a maker in the body menu hunting
	// for them) — discoverability beats purity. Português: As ações de
	// fase moram na pill E aqui — descoberta vence pureza.
	phase := []contextMenu.Item{
		// Same single entry as the pill (see caseSelectMenuItems): the
		// phase relation decides the side, not the maker. Português:
		// Mesma entrada única da pill — a relação de fase decide o lado.
		contextMenu.Item{
			ID:              "seq_tunnel_body",
			Label:           translate.T("seqAddTunnel", "＋ Tunnel"),
			FontAwesomePath: seqIconTunnel.Path,
			ViewBox:         seqIconTunnel.ViewBox,
			HelpFallback:    "Create a data tunnel: born as an input on the right edge of this phase, it becomes an output on the left edge in the following phases.",
			OnClick:         func() { e.createTunnel() },
		},
		{
			ID:              "seq_add_body",
			Label:           translate.T("seqAddPhase", "＋ Add phase"),
			FontAwesomePath: seqIconPlus.Path,
			ViewBox:         seqIconPlus.ViewBox,
			HelpFallback:    "Append a new phase after the last one.",
			OnClick:         func() { e.addPhase() },
		},
	}
	if len(e.cases) > 1 {
		phase = append(phase, contextMenu.Item{
			ID:              "seq_del_body",
			Label:           translate.T("seqRemovePhase", "− Remove this phase"),
			FontAwesomePath: seqIconMinus.Path,
			ViewBox:         seqIconMinus.ViewBox,
			HelpFallback:    "Removes the phase on screen; its devices move to the neighbor phase.",
			OnClick:         func() { e.removePhaseAt(e.caseIndexByID(e.selectedCase)) },
		})
		phase = append(phase, contextMenu.Item{
			ID:              "seq_ins_before_body",
			Label:           translate.T("seqInsertBefore", "＋ Insert phase before"),
			FontAwesomePath: seqIconPlus.Path,
			ViewBox:         seqIconPlus.ViewBox,
			HelpKey:         "helpSeqInsertBefore",
			HelpFallback:    "Adds an empty phase immediately before the phase on screen.",
			OnClick:         func() { e.insertPhaseAt(e.caseIndexByID(e.selectedCase)) },
		})
		phase = append(phase, contextMenu.Item{
			ID:              "seq_ins_after_body",
			Label:           translate.T("seqInsertAfter", "＋ Insert phase after"),
			FontAwesomePath: seqIconPlus.Path,
			ViewBox:         seqIconPlus.ViewBox,
			HelpKey:         "helpSeqInsertAfter",
			HelpFallback:    "Adds an empty phase immediately after the phase on screen.",
			OnClick:         func() { e.insertPhaseAt(e.caseIndexByID(e.selectedCase) + 1) },
		})
		phase = append(phase, contextMenu.Item{
			ID:              "seq_rename_body",
			Label:           translate.T("seqRenamePhase", "Rename phase"),
			FontAwesomePath: seqIconPen.Path,
			ViewBox:         seqIconPen.ViewBox,
			HelpKey:         "helpSeqRenamePhase",
			HelpFallback:    "Gives the phase on screen a meaningful name (display only).",
			OnClick:         func() { go e.renameSelectedPhase() },
		})
	}
	return append(phase, e.bodyMenuTail()...)
}

func (e *StatementSequence) bodyMenuTail() []contextMenu.Item {
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
		// Sequence v1 (2026-07-16): no inspect overlay — mirrors Loop.
		// ＋/− phase live on the pill. Português: v1 sem overlay de
		// inspect (espelha o Loop); ＋/− moram na pill.
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
			OnClick: func() {
				devices.BackendZRegistry.MoveForward(e.id)
				// Z became SEMANTIC (stacking law 2026-07-19): the gesture
				// must summon a re-parent pass NOW — without the notify,
				// RefreshAll never ran and the law stayed dormant (field:
				// "continua com o problema"). Português: Z virou SEMÂNTICO —
				// o gesto convoca o re-parenting AGORA; sem o notify, o
				// RefreshAll não rodava e a lei ficava dormente.
				if e.sceneNotify != nil {
					e.sceneNotify()
				}
			},
		},
		{
			ID:              "backward",
			Label:           translate.T("menuDeviceSendBackward", "Backward"),
			FontAwesomePath: rulesIcon.KFAMinus,
			ViewBox:         "0 0 448 512",
			HelpFallback:    "Sends this container below overlapping devices.",
			OnClick: func() {
				devices.BackendZRegistry.MoveBackward(e.id)
				// Z became SEMANTIC (stacking law 2026-07-19): the gesture
				// must summon a re-parent pass NOW — without the notify,
				// RefreshAll never ran and the law stayed dormant (field:
				// "continua com o problema"). Português: Z virou SEMÂNTICO —
				// o gesto convoca o re-parenting AGORA; sem o notify, o
				// RefreshAll não rodava e a lei ficava dormente.
				if e.sceneNotify != nil {
					e.sceneNotify()
				}
			},
		},
	}
}

// ── Sprite event wiring ──────────────────────────────────────────────

func (e *StatementSequence) wireEvents() {
	// Latent twin of the Function's drag-follow fix (field 2026-07-19,
	// discovered by absence: the Sequence never wired drag/resize
	// hooks, so its tunnels never followed either — nobody had dragged
	// a tunneled Sequence yet). Pose-only refresh per frame; membership
	// and crossings stay the scenegraph's job. Português: Gêmeo latente
	// da cura do Function (descoberto pela ausência: o Sequence nunca
	// ligou os ganchos, então seus túneis também não seguiam). Refresh
	// só de pose por frame; filiação e travessias seguem com o
	// scenegraph.
	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		e.refreshTunnelViews()
	})
	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		e.refreshTunnelViews()
	})
	e.elem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		e.refreshTunnelViews()
	})
	e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		e.refreshTunnelViews()
	})

	// Click — three hit regions: case pill, selector connector, body.
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if event.LocalX >= caseBorder.KPillX && event.LocalX <= caseBorder.KPillX+caseBorder.KPillW &&
			event.LocalY >= caseBorder.KPillY && event.LocalY <= caseBorder.KPillY+caseBorder.KPillH {
			log.Printf("[Case] pill clicked on %v", e.id)
			if e.ctxMenu == nil {
				return
			}
			// Always open — Open() self-replaces (closes any live menu
			// first), so the old IsOpen/Close/return toggle is gone: it
			// was unreachable on the happy path (the menu's fullscreen
			// overlay swallows canvas clicks while open) and a stuck
			// IsOpen turned it into a silent click-eater (field
			// 2026-07-19: "o menu contextual parou de funcionar perto
			// do dropdown"). The shield logs the IsOpen state and names
			// any item-building panic instead of killing the runtime.
			// Português: Sempre abrir — o Open() se substitui; a dança
			// IsOpen/Close/return era inalcançável no caminho feliz (o
			// overlay do menu engole cliques do canvas enquanto aberto)
			// e um IsOpen preso a tornava um comedor de cliques. O
			// escudo loga o estado e nomeia panic de montagem de itens.
			elemX, elemY := e.elem.GetPosition()
			menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[MENU-PANIC] sequence pill: %v", r)
					}
				}()
				if e.ctxMenu.IsOpen() {
					log.Printf("[MENU] sequence pill: replacing an already-open menu")
				}
				e.ctxMenu.OpenAtWorld(e.caseSelectMenuItems(), menuX, menuY)
			}()
			return
		}

		// (no pin — the selector click-zone was ghost #2, caught by the
		// field screenshot 2026-07-16; the three-layer removal now has a
		// FOURTH member: the click-cascade branch. Português: A zona de
		// clique do seletor era o fantasma nº 2 — a remoção de pino tem
		// um QUARTO membro: o ramo da cascata de clique.)

		if e.ctxMenu == nil {
			return
		}
		// Same always-open doctrine as the pill branch above.
		// Português: Mesma doutrina de sempre-abrir do ramo da pílula.
		elemX, elemY := e.elem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[MENU-PANIC] sequence body: %v", r)
				}
			}()
			if e.ctxMenu.IsOpen() {
				log.Printf("[MENU] sequence body: replacing an already-open menu")
			}
			e.ctxMenu.OpenForDevice(e, e.getBodyMenuItems(), menuX, menuY)
		}()
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
		if localX >= caseBorder.KPillX && localX <= caseBorder.KPillX+caseBorder.KPillW &&
			localY >= caseBorder.KPillY && localY <= caseBorder.KPillY+caseBorder.KPillH {
			return sprite.CursorPointer
		}
		return ""
	})
}

// ── SVG helpers ──────────────────────────────────────────────────────

func (e *StatementSequence) recacheOrnament() {
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

func (e *StatementSequence) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	// Sequence: NO wire-layer connector. The field caught the phantom
	// (2026-07-16, "ele tem conexão de entrada e não faz sentido"): the
	// wire layer draws its OWN pin for every registered connector —
	// removing the ornament pin and the connection.Setup was not enough;
	// this third registration was the survivor. The container-rect
	// registration below stays: it is what gives crossing wires their
	// tunnel markers. Português: SEM conector na camada de fios — a
	// camada desenha o próprio pino para cada registro; este era o
	// sobrevivente fantasma. O RegisterContainer abaixo FICA (é ele que
	// dá os marcadores de túnel aos fios que cruzam).

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

func (e *StatementSequence) GetDeviceType() string   { return "StatementSequence" }
func (e *StatementSequence) GetIconName() string     { return "Case" }
func (e *StatementSequence) GetIconCategory() string { return "Logic" }

// ── Scene geometry interface ────────────────────────────────────────

func (e *StatementSequence) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementSequence) GetInnerBBox() *scene.Rect {
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

func (e *StatementSequence) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

func (e *StatementSequence) RefreshVisual() {
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
func (e *StatementSequence) RefreshMembership() {
	e.assignNewChildren()
	e.applyCaseVisibility()
	if e.stage != nil {
		e.stage.MarkDirty()
	}
}

// ── Case serialization ───────────────────────────────────────────────

func (e *StatementSequence) GetProperties() map[string]interface{} {
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

	props := map[string]interface{}{
		"selectedCase": e.selectedCase,
		"cases":        cases,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// container's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do container.
func (e *StatementSequence) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementSequence) SetComment(c string) { e.comment = c }

// ApplyProperties restores the v1 surface: comment + the ACTIVE phase.
// Phase structure lives on the pill. Português: Restaura a superfície v1 —
// comentário + fase ativa; a estrutura mora na pill.
func (e *StatementSequence) ApplyProperties(values map[string]string) {
	// v1 surface: comment + active phase. Phase STRUCTURE is edited on
	// the pill (add/remove); there is no FieldCaseEditor path to
	// reconcile — the Case's whole reconciliation block does not apply
	// here. Português: Superfície v1 — comentário + fase ativa; a
	// ESTRUTURA das fases é editada na pill, sem reconciliação.
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if sc, ok := values["selectedCase"]; ok && sc != "" {
		e.selectedCase = sc
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
		}
	}
}

// nextCaseID returns the first "<baseID>_cN" identifier not present in taken.
// It mints ids for cases the maker adds in the overlay (which arrive with an
// empty id). taken must include the existing case ids AND any id minted earlier
// in the same reconciliation pass, so two freshly-added cases never collide.
//
// Português: Retorna o primeiro id "<baseID>_cN" ausente de taken. Cria ids
// para cases novos do overlay (que chegam com id vazio); taken inclui os ids
// existentes e os já criados na mesma passada, evitando colisão.

// in a goroutine — overlay.Show loads help/preview assets asynchronously, the
// same constraint every other device's inspect overlay has.
//
// showCaseSaveBlocked tells the maker which error-severity diagnostics prevent
// the Case from being applied, one per line, over the still-open inspector so
// they can fix the rows and retry.
//
// Português: Informa ao maker quais erros impedem o Case de ser aplicado, um
// por linha, sobre o inspetor ainda aberto, para corrigir e tentar de novo.
func (e *StatementSequence) showCaseSaveBlocked(errs []overlay.Diagnostic) {
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

func (e *StatementSequence) Get() *html.TagSvg    { return nil }
func (e *StatementSequence) SetFatherId(_ string) {}

func (e *StatementSequence) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}
func (e *StatementSequence) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}
func (e *StatementSequence) SetSize(w, h rulesDensity.Density) {
	e.width = w
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(w, h)
	}
}
func (e *StatementSequence) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementSequence) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}
func (e *StatementSequence) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementSequence) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementSequence) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		y := e.elem.GetYD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}
func (e *StatementSequence) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		x := e.elem.GetXD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}
func (e *StatementSequence) SetWidth(width rulesDensity.Density) {
	e.width = width
	if e.elem != nil {
		h := e.elem.GetHeightD()
		newW, newH := e.gridAdjust.AdjustCenterD(width, h)
		e.elem.SetSizeD(newW, newH)
	}
}
func (e *StatementSequence) SetHeight(height rulesDensity.Density) {
	e.height = height
	if e.elem != nil {
		w := e.elem.GetWidthD()
		newW, newH := e.gridAdjust.AdjustCenterD(w, height)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementSequence) GetID() string          { return e.id }
func (e *StatementSequence) GetName() string        { return e.name }
func (e *StatementSequence) GetInitialized() bool   { return e.initialized }
func (e *StatementSequence) GetSelected() bool      { return e.selected }
func (e *StatementSequence) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementSequence) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementSequence) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementSequence) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementSequence) GetStatus() int         { return e.iconStatus }
func (e *StatementSequence) SetStatus(s int)        { e.iconStatus = s }
func (e *StatementSequence) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementSequence) GetResize() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementSequence) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

func (e *StatementSequence) SetSelected(selected bool) {
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

func (e *StatementSequence) SetDragEnable(enabled bool) {
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

func (e *StatementSequence) SetResizeEnable(enabled bool) {
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

func (e *StatementSequence) onConnectionClick() {}

// ── Icon ─────────────────────────────────────────────────────────────

func (e *StatementSequence) getIcon(data rulesIcon.Data) js.Value {
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
