// /ide/devices/compFlow/statementFunction.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// statementLoop.go — Loop container device.
//
// English:
//
//	A Complex device (kind = KindComplex) that provides a visual "for"
//	loop on the stage. The ornament is drawn by doubleLoopArrow and
//	carries a stop button (a bool-typed input connector) in the bottom-
//	right corner. Children placed inside the loop's border 3 become
//	statements of the loop body; codegen emits them inside a for {}.
//
//	This file wires the Loop into the scenegraph:
//	  - Declares KindComplex via the Kinded interface.
//	  - Declares its Padded container padding (LoopPadding).
//	  - Translates sprite drag/resize events to graph lifecycle calls
//	    so the scenegraph can move children rigidly, clamp resize, and
//	    raise conflicts in real time.
//	  - Subscribes to its own conflict events so the warning mark
//	    lights up the instant a child straddles the loop's border 3 or
//	    another device overlaps its border 1.
//
//	Responsibilities OUT of this file:
//	  - Containment math → scenegraph + rulesContainer.
//	  - Conflict detection → scenegraph.
//	  - JSON export → scene.Serializer.
//
// Português:
//
//	Device Complex (KindComplex) que representa um "for" visual na
//	stage. O desenho é feito por doubleLoopArrow e tem um botão de
//	parada (conector bool) no canto inferior-direito. Filhos colocados
//	na border 3 viram statements do corpo do loop.

import (
	"encoding/json"
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
	"github.com/helmutkemper/iotmakerio/hexagon"
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

type StatementFunction struct {
	// Sprite/DOM ----------------------------------------------------
	stage sprite.Stage
	elem  sprite.Element

	// Local state (formerly inside block.Block) ---------------------
	name         string
	initialized  bool
	selected     bool
	selectLocked bool
	dragEnabled  bool
	dragLocked   bool
	resizeLocked bool
	// [COMMENT] user comment — appears as `// ` lines above this container's
	// statement in the generated code and in the container's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste container no código gerado e no tooltip de hover.
	comment string

	// functionName is THE datum of the linkage device: emitted VERBATIM
	// as the function's name (doctrine 1 — prefix-exempt; the server
	// validates it as a C identifier and refuses politely otherwise).
	// The pill on the border is its badge. Português: O nome — emitido
	// verbatim (doutrina 1); o servidor valida como identificador C; a
	// pill é o crachá.
	functionName string
	width        rulesDensity.Density
	height       rulesDensity.Density

	// Pending state applied at the end of Init() --------------------
	pendingResizeEnable *bool
	pendingDragEnable   *bool
	pendingSelected     *bool
	resizerButton       block.ResizeButton

	// Shared hex menu instance --------------------------------------
	// [CTXMENU] linear context menu controller.
	ctxMenu *contextMenu.Controller

	// Wire manager for stop port ------------------------------------
	wireMgr *wire.Manager

	// createTunnelFn is the factory's tunnel constructor (the same
	// CreateTunnelFor the Sequence uses — host-agnostic, verified
	// 2026-07-19). natalCase rides empty: a Function has no phases.
	// Português: O construtor de túnel do factory (o mesmo do
	// Sequence — host-agnóstico); natal viaja vazio: Function não tem
	// fases.
	createTunnelFn func(parentID, side string, x, y float64, natalCase string,
		judge func(cx, cy float64, side, selfID string) (float64, float64)) string

	// refreshingTunnels — re-entrancy guard, sibling of the Sequence's.
	refreshingTunnels bool

	// saveToMyItems — the workspace's capture+POST action (P3).
	// Português: A ação de captura+POST do workspace.
	saveToMyItems func(functionID string)

	// LAYERS (L1, Kemper 2026-07-19): the function's body organises
	// into ORDERED layers — Sequence-phase semantics inside the
	// function. The dropdown/menu lists them; members belong to the
	// layer that was selected when they were dropped; only the
	// selected layer's members are visible/collidable. Serialised as
	// "layers"/"selectedLayer" (mirroring the Case's "cases" keys).
	// Português: CAMADAS — o corpo se organiza em camadas ORDENADAS
	// (semântica de fases do Sequence dentro da função); membresia por
	// camada selecionada no drop; só a camada ativa fica visível.
	layers        []functionLayer
	selectedLayer string

	// lastOrnY/hasLastOrn track the ornament's top between refresher
	// passes: Y follows by DELTA (field 2026-07-19, "esqueceu o eixo
	// y") — px recomputes absolutely from the rail, but a tunnel's Y
	// is its position ALONG the rail, meaningful only relative to the
	// container; the delta carries it on vertical drags, and a
	// bottom-edge resize (oy unchanged) leaves it put, clamp guarding.
	// Português: Y segue por DELTA — px é absoluto do trilho, mas o Y
	// é posição AO LONGO do trilho, relativa ao container; o delta o
	// carrega no drag vertical, e resize pela borda de baixo (oy
	// intacto) o deixa quieto, com o clamp segurando.
	lastOrnY   float64
	hasLastOrn bool

	debugSelected bool

	// Size defaults and constraints ---------------------------------
	defaultWidth          rulesDensity.Density
	defaultHeight         rulesDensity.Density
	horizontalMinimumSize rulesDensity.Density
	verticalMinimumSize   rulesDensity.Density

	// Ornament drawings (body + icon) -------------------------------
	ornamentDraw     *seqBorder.SeqBorder
	ornamentDrawIcon *seqBorder.SeqBorder

	id         string
	debugMode  bool
	gridAdjust grid.Adjust
	iconStatus int

	// Scene integration ---------------------------------------------
	sceneMgr    *scene.Serializer
	sceneNotify func()
	onRemove    func(id string)

	// Canvas DOM element (for HTML overlay coordinate transforms) ---
	canvasEl js.Value

	// Dashed overlay shown during resize — drawn at the minimum
	// containment area so the user can see why the handle can't go
	// further.
	childBoundsDiv js.Value

	// Cached rectangles during an active resize gesture. Populated in
	// SetOnResizeStart, consumed in SetOnResizeMove/End, cleared in
	// SetOnResizeEnd. Using a *rulesContainer.Rect keeps the type
	// identical to what rulesContainer.ClampResize takes.
	resizeChildBounds *rulesContainer.Rect
	resizeParentInner *rulesContainer.Rect

	// dragStartPos remembers where the Loop's top-left was when the
	// drag began, so at EndDrag we can compute the total (dx, dy) and
	// hand it to the scenegraph to translate descendants.
	dragStartX, dragStartY float64
}

// =====================================================================
//  Dependency injection — called by the factory before Init()
// =====================================================================

func (e *StatementFunction) SetStage(stage sprite.Stage)       { e.stage = stage }
func (e *StatementFunction) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }

// SetContextMenu injects the linear context menu controller.
func (e *StatementFunction) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementFunction) SetWireManager(mgr *wire.Manager) { e.wireMgr = mgr }

// functionLayer is one ordered layer of the function's body.
// Português: Uma camada ordenada do corpo da função.
type functionLayer struct {
	id    string
	label string
	ids   []string
}

// SetSaveToMyItems injects the workspace's "Save to My Items" action.
// Português: Injeta a ação "Salvar em My Items" do workspace.
func (e *StatementFunction) SetSaveToMyItems(fn func(functionID string)) {
	e.saveToMyItems = fn
}

// SetCreateTunnel injects the factory's tunnel constructor — mirror of
// the Sequence's cord. Português: Injeta o construtor — espelho da
// cordinha do Sequence.
func (e *StatementFunction) SetCreateTunnel(fn func(parentID, side string, x, y float64, natalCase string,
	judge func(cx, cy float64, side, selfID string) (float64, float64)) string) {
	e.createTunnelFn = fn
}

// ornamentRect mirrors the Sequence's: the Density(10) ornamental
// inset — border furniture pins to THIS rect, never the mathematical
// bbox (the border-furniture law). Português: Espelho do Sequence — a
// mobília de borda pina NESTE retângulo, nunca no bbox matemático.
func (e *StatementFunction) ornamentRect() (x, y, w, h float64) {
	if e.elem == nil {
		return 0, 0, 0, 0
	}
	ex, ey := e.elem.GetPosition()
	ew, eh := e.elem.GetSize()
	m := rulesDensity.Density(10).GetFloat()
	return ex + m, ey + m, ew - 2*m, eh - 2*m
}

// funcTunnelJudge — the Function's drop judge. Unlike the Sequence's
// side-agnostic judge (side is ephemeral there), this one FORCES the x
// onto the requested rail: F2 makes the side IDENTITY (left =
// parameter, right = return), so the judge IS the side lock — it
// travels in the creation signature and every re-seat passes through
// it. Collision stays Y-only, minGap 24, shift-down. Português: O juiz
// do Function FORÇA o x no trilho pedido — F2 torna o lado IDENTIDADE
// (esquerda = parâmetro, direita = retorno), então o juiz É a trava.
// Colisão só em Y, minGap 24, empurra para baixo.
func (e *StatementFunction) funcTunnelJudge(cx, cy float64, side, selfID string) (float64, float64) {
	const minGap = 24.0
	ox, oy, ow, oh := e.ornamentRect()
	if ow <= 0 || oh <= 0 {
		return cx, cy
	}
	if side == "left" {
		cx = ox
	} else {
		cx = ox + ow
	}
	var occupied []float64
	if e.wireMgr != nil {
		for _, p := range e.wireMgr.TunnelPointsFor(e.id) {
			occupied = append(occupied, p.Y)
		}
		for _, id := range e.wireMgr.ManualTunnelIDsFor(e.id) {
			if id == selfID {
				continue
			}
			if p, ok := e.wireMgr.ManualTunnelPoint(id); ok {
				occupied = append(occupied, p.Y)
			}
		}
	}
	for moved := true; moved; {
		moved = false
		for _, y := range occupied {
			if d := cy - y; d > -minGap && d < minGap {
				cy = y + minGap
				moved = true
			}
		}
	}
	if cy < oy {
		cy = oy
	}
	if cy > oy+oh {
		cy = oy + oh
	}
	return cx, cy
}

// createFunctionTunnel births a signature tunnel on the given rail:
// "left" = parameter, "right" = return (F2). The label the maker gives
// it (tunnel menu → Rename) becomes the parameter/return NAME in the
// generated code (Fatia C). Panic-shielded like every tunnel path.
// Português: Pare um túnel de assinatura no trilho dado — esquerda =
// parâmetro, direita = retorno (F2). O rótulo vira o NOME no código
// gerado (Fatia C). Escudo de panic como todo caminho de túnel.
func (e *StatementFunction) createFunctionTunnel(side string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[FN-TUNNEL-PANIC] createFunctionTunnel: %v", r)
		}
	}()
	if e.createTunnelFn == nil || e.elem == nil {
		return
	}
	ox, oy, ow, oh := e.ornamentRect()
	cx := ox
	if side == "right" {
		cx = ox + ow
	}
	cy := oy + oh*0.25
	cx, cy = e.funcTunnelJudge(cx, cy, side, "")
	id := e.createTunnelFn(e.id, side, cx, cy, "", e.funcTunnelJudge)
	if id == "" {
		return
	}
	e.refreshFunctionTunnelViews()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// refreshFunctionTunnelViews is the Function's pose keeper AND mover:
// px comes from the FRESH ornamentRect on every pass, so tunnels
// follow the container (the same mechanism that moves the Sequence's).
// The poses are FIXED by the rail (F2): left → role "out" (body
// consumers read the parameter), right → role "in" (a body producer
// feeds the return) — exactly the two poses the renderer already
// draws. No phases, so hidden is always false. Português: Guardião de
// pose E movedor — px vem do ornamento FRESCO a cada passe (o mesmo
// mecanismo que move os do Sequence). Poses FIXAS pelo trilho:
// esquerda → "out", direita → "in" — as duas poses que o renderer já
// desenha. Sem fases, hidden sempre falso.
func (e *StatementFunction) refreshFunctionTunnelViews() {
	if e.refreshingTunnels {
		return
	}
	e.refreshingTunnels = true
	defer func() {
		e.refreshingTunnels = false
		if r := recover(); r != nil {
			log.Printf("[FN-TUNNEL-PANIC] refreshFunctionTunnelViews: %v", r)
		}
	}()
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	ox, oy, ow, oh := e.ornamentRect()
	if ow <= 0 || oh <= 0 {
		return
	}
	dy := 0.0
	if e.hasLastOrn {
		dy = oy - e.lastOrnY
	}
	e.lastOrnY, e.hasLastOrn = oy, true
	for _, id := range e.wireMgr.ManualTunnelIDsFor(e.id) {
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
		side := e.wireMgr.ManualTunnelSide(id)
		px, role := ox, "out"
		if side != "left" {
			side, px, role = "right", ox+ow, "in"
		}
		e.wireMgr.SetManualTunnelView(id, side, px, y, role, false)
	}
}

// RefreshMembership rides the workspace's container-refresh assertion
// (the same hook the Sequence rides): every notify pass re-pins the
// tunnels to the fresh ornament. The name is the interface's, the job
// here is pose-keeping. Português: Pega carona na asserção do
// workspace — todo passe de notify re-pina os túneis no ornamento
// fresco. O nome é da interface; o trabalho aqui é guardar pose.
// ensureDefaultLayer guarantees at least one layer so legacy scenes
// (pre-L1) keep behaving: every existing member adopts into it.
// Português: Garante ao menos uma camada — cenas antigas seguem
// funcionando, com todos os membros adotados nela.
func (e *StatementFunction) ensureDefaultLayer() {
	if len(e.layers) > 0 {
		if e.selectedLayer == "" {
			e.selectedLayer = e.layers[0].id
		}
		return
	}
	e.layers = []functionLayer{{id: "layer_1"}}
	e.selectedLayer = "layer_1"
}

// freshLayerID mints an unused "layer_N". Português: Cunha id livre.
func (e *StatementFunction) freshLayerID() string {
	for n := len(e.layers) + 1; ; n++ {
		id := fmt.Sprintf("layer_%d", n)
		taken := false
		for i := range e.layers {
			if e.layers[i].id == id {
				taken = true
				break
			}
		}
		if !taken {
			return id
		}
	}
}

// layerMenuLabel — display name, "layer N" fallback by position.
// Português: Nome de exibição; "layer N" pela posição.
func (e *StatementFunction) layerMenuLabel(i int) string {
	if e.layers[i].label != "" {
		return e.layers[i].label
	}
	return fmt.Sprintf("layer %d", i+1)
}

// layerSelectMenuItems builds the layer section for the function's
// menu: one entry per layer in order (the selected one wears the eye),
// plus "New layer". Reordering is L3. Português: A seção de camadas do
// menu — uma entrada por camada (a ativa leva o olho) + "New layer".
func (e *StatementFunction) layerSelectMenuItems() []contextMenu.Item {
	e.ensureDefaultLayer()
	items := make([]contextMenu.Item, 0, len(e.layers)+1)
	for i := range e.layers {
		id := e.layers[i].id
		item := contextMenu.Item{
			ID:           "fn_layer_" + id,
			Label:        e.layerMenuLabel(i),
			HelpFallback: "Show this layer on the stage; new devices join the visible layer.",
			OnClick:      func() { e.selectLayer(id) },
		}
		if id == e.selectedLayer {
			item.FontAwesomePath = rulesIcon.KFAEye
			item.ViewBox = "0 0 512 512"
		}
		items = append(items, item)
	}
	items = append(items, contextMenu.Item{
		ID:              "fn_layer_add",
		Label:           translate.T("funcLayerAdd", "New layer"),
		FontAwesomePath: seqIconPlus.Path,
		ViewBox:         seqIconPlus.ViewBox,
		HelpFallback:    "Add an ordered layer; layers run one after the other in the generated code.",
		OnClick:         func() { e.addLayer() },
	})
	return items
}

// addLayer appends a fresh layer and selects it. Português: Anexa uma
// camada nova e a seleciona.
func (e *StatementFunction) addLayer() {
	e.ensureDefaultLayer()
	fresh := functionLayer{id: e.freshLayerID()}
	e.layers = append(e.layers, fresh)
	e.selectLayer(fresh.id)
}

// selectLayer makes a layer the visible/edited one. Português: Torna a
// camada a visível/editada.
func (e *StatementFunction) selectLayer(id string) {
	e.selectedLayer = id
	e.maintainLayerMembership()
	e.applyLayerVisibility()
	if e.sceneNotify != nil {
		e.sceneNotify()
	}
}

// maintainLayerMembership adopts unassigned children into the selected
// layer and prunes ids that left the function. Border furniture
// (tunnels) never joins — the seat-belt. Português: Adota filhos sem
// camada na selecionada e poda ids que saíram; túneis nunca entram.
func (e *StatementFunction) maintainLayerMembership() {
	if e.sceneMgr == nil {
		return
	}
	e.ensureDefaultLayer()
	children := map[string]bool{}
	for _, id := range e.sceneMgr.ChildrenOf(e.id) {
		if strings.HasPrefix(id, "tunnel") {
			continue
		}
		children[id] = true
	}
	known := map[string]bool{}
	for i := range e.layers {
		kept := e.layers[i].ids[:0]
		for _, id := range e.layers[i].ids {
			if children[id] {
				kept = append(kept, id)
				known[id] = true
			}
		}
		e.layers[i].ids = kept
	}
	sel := 0
	for i := range e.layers {
		if e.layers[i].id == e.selectedLayer {
			sel = i
			break
		}
	}
	for id := range children {
		if !known[id] {
			e.layers[sel].ids = append(e.layers[sel].ids, id)
		}
	}
}

// applyLayerVisibility — the Sequence's proven pass, layer edition:
// only the selected layer's members stay visible, in the wire layer
// and in collision; tunnels are TOTALLY exempt (the seat-belt); every
// path that changes the visible layer funnels here, so the tunnel
// re-seat rides along. Português: O passe provado do Sequence, edição
// camadas — só a camada ativa fica visível, na wire layer e na
// colisão; túneis totalmente isentos; o re-assento dos túneis pega
// carona no funil.
func (e *StatementFunction) applyLayerVisibility() {
	if e.stage == nil {
		return
	}
	for i := range e.layers {
		show := e.layers[i].id == e.selectedLayer
		for _, id := range e.layers[i].ids {
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
			if e.wireMgr != nil {
				e.wireMgr.SetElementHidden(id, !show)
			}
			if e.sceneMgr != nil {
				e.sceneMgr.SetHidden(id, !show)
			}
		}
	}
	e.refreshFunctionTunnelViews()
}

func (e *StatementFunction) RefreshMembership() {
	e.maintainLayerMembership()
	e.applyLayerVisibility()
}
func (e *StatementFunction) SetCanvasEl(el js.Value)                { e.canvasEl = el }
func (e *StatementFunction) SetResizerButton(rb block.ResizeButton) { e.resizerButton = rb }
func (e *StatementFunction) SetDraggerButton(_ block.ResizeButton)  {}
func (e *StatementFunction) SetGridAdjust(ga grid.Adjust)           { e.gridAdjust = ga }
func (e *StatementFunction) SetOnRemove(fn func(id string))         { e.onRemove = fn }

// SetSceneNotify installs the scene-change callback fired at the end of
// any interactive gesture. The workspace wires this to
// sceneMgr.NotifyChange.
func (e *StatementFunction) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// =====================================================================
//  Lifecycle
// =====================================================================

func (e *StatementFunction) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

// Remove destroys the Loop: its visual elements, its wire registrations,
// its z-index entry, and its scenegraph subscription.
func (e *StatementFunction) Remove() {
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

// =====================================================================
//  Scenegraph interface — Kinded + Padded
// =====================================================================

// GetKind tells the scenegraph this is a container. Satisfies
// scene.Kinded.
func (e *StatementFunction) GetKind() scenegraph.Kind { return scenegraph.KindComplex }

// GetContainerPadding returns the padding used to derive border 3 from
// border 1. Satisfies scene.Padded.
func (e *StatementFunction) GetContainerPadding() rulesContainer.Padding {
	return rulesContainer.LoopPadding()
}

// =====================================================================
//  Scene geometry interface
// =====================================================================

func (e *StatementFunction) GetDeviceType() string { return "StatementFunction" }

func (e *StatementFunction) GetOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

// GetInnerBBox returns the internal usable area — border 3 — derived
// from border 1 by applying LoopPadding. Single source of truth for
// Loop's padding lives in rulesContainer.
func (e *StatementFunction) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	pad := rulesContainer.LoopPadding()
	return &scene.Rect{
		X:      x + pad.Left,
		Y:      y + pad.Top,
		Width:  w - pad.Left - pad.Right,
		Height: h - pad.Top - pad.Bottom,
	}
}

// MoveBy shifts the Loop's outer rectangle by (dx, dy). Called by the
// scenegraph when an ancestor container is being dragged — not when
// the Loop itself is being dragged (the sprite handles that directly).
func (e *StatementFunction) MoveBy(dx, dy float64) {
	if e.elem == nil {
		return
	}
	x, y := e.elem.GetPosition()
	e.elem.SetPosition(x+dx, y+dy)
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// RefreshVisual regenerates the SVG ornament and recalculates wire
// positions. Called by the import system after SetSize so the visual
// matches the restored dimensions.
func (e *StatementFunction) RefreshVisual() {
	e.recacheOrnament()
	if e.wireMgr != nil {
		e.wireMgr.RecalculateForElement(e.id)
	}
}

// =====================================================================
//  Basic getters/setters
// =====================================================================

func (e *StatementFunction) Get() (container *html.TagSvg) { return nil }

func (e *StatementFunction) SetFatherId(_ string) {}

func (e *StatementFunction) SetName(name string) {
	e.name = rulesSequentialId.GetIdFromBase(name)
}

func (e *StatementFunction) GetID() string   { return e.id }
func (e *StatementFunction) GetName() string { return e.name }

func (e *StatementFunction) SetPosition(x, y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, y)
	}
}

func (e *StatementFunction) SetSize(w, h rulesDensity.Density) {
	e.width = w
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(w, h)
	}
}

func (e *StatementFunction) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}

func (e *StatementFunction) GetHeight() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetHeightD()
	}
	return e.height
}

func (e *StatementFunction) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}

func (e *StatementFunction) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}

func (e *StatementFunction) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		y := e.elem.GetYD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}

func (e *StatementFunction) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		x := e.elem.GetXD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)
	}
}

func (e *StatementFunction) SetWidth(width rulesDensity.Density) {
	e.width = width
	if e.elem != nil {
		h := e.elem.GetHeightD()
		newW, newH := e.gridAdjust.AdjustCenterD(width, h)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementFunction) SetHeight(height rulesDensity.Density) {
	e.height = height
	if e.elem != nil {
		w := e.elem.GetWidthD()
		newW, newH := e.gridAdjust.AdjustCenterD(w, height)
		e.elem.SetSizeD(newW, newH)
	}
}

func (e *StatementFunction) SetSelected(selected bool) {
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

func (e *StatementFunction) SetDragEnable(enabled bool) {
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

func (e *StatementFunction) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}

// SetResizeEnable toggles resize mode. When entering resize on a Loop
// that is nested inside another Complex, the sprite's built-in max
// size is set to the parent's inner bbox so the Loop can't grow past
// its container. This uses the scenegraph's ParentInnerBBox query.
func (e *StatementFunction) SetResizeEnable(enabled bool) {
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

func (e *StatementFunction) GetInitialized() bool { return e.initialized }
func (e *StatementFunction) GetDragBlocked() bool { return e.dragLocked }
func (e *StatementFunction) GetDragEnable() bool  { return e.dragEnabled }
func (e *StatementFunction) GetResize() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementFunction) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementFunction) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementFunction) GetSelected() bool      { return e.selected }
func (e *StatementFunction) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}

func (e *StatementFunction) onConnectionClick() {}

// =====================================================================
//  Init — build the sprite, wire events, register connectors
// =====================================================================

// Init — NOTE: blocks during image caching. MUST be called from a
// goroutine, not the main thread.
func (e *StatementFunction) Init() error {
	if e.stage == nil {
		log.Println("Error: SetStage() must be called before Init()")
		return nil
	}

	_ = hexagon.KStageId // keep import reference

	if e.name == "" {
		e.SetName("stmLoop")
	}

	e.defaultWidth = 400
	e.defaultHeight = 300
	e.defaultWidth, e.defaultHeight = e.gridAdjust.AdjustCenterD(e.defaultWidth, e.defaultHeight)

	e.horizontalMinimumSize = 150
	e.verticalMinimumSize = 150
	e.horizontalMinimumSize, e.verticalMinimumSize = e.gridAdjust.AdjustCenterD(e.horizontalMinimumSize, e.verticalMinimumSize)

	if e.width == 0 || e.height == 0 {
		e.width = e.defaultWidth
		e.height = e.defaultHeight
	}

	e.id = rulesSequentialId.GetIdFromBase(e.name)

	e.ornamentDraw = new(seqBorder.SeqBorder)
	e.ornamentDrawIcon = new(seqBorder.SeqBorder)

	// Function: NO stop pin — the linkage device has no data inputs at
	// all; the pill (name badge) is the whole left-edge UI. All THREE pin
	// layers removed (ornament, connection, wire — the Sequence's
	// phantom-pin lesson, 2026-07-16). Português: SEM pino de stop — as
	// TRÊS camadas removidas (lição do pino-fantasma do Sequence).
	if err := e.ornamentDraw.GetConnectionError(); err != nil {
		return err
	}
	if e.functionName == "" {
		e.functionName = "my_function"
	}
	_ = e.ornamentDraw.Init()
	_ = e.ornamentDrawIcon.Init()
	e.ornamentDraw.SetCaseLabel(e.functionName)

	// Build sprite element from the ornament SVG.
	_ = e.ornamentDraw.Update(0, 0, e.width, e.height)
	ornamentSvg := e.ornamentDraw.GetSvg().Get()
	ornamentSvg.Call("setAttribute", "width", e.width.GetInt())
	ornamentSvg.Call("setAttribute", "height", e.height.GetInt())
	ornamentXml := devices.SerializeSvgToXml(ornamentSvg)

	var err error
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
		return err
	}

	e.elem.SetMinSizeD(e.horizontalMinimumSize, e.verticalMinimumSize)
	devices.BackendZRegistry.Register(e.id, e.elem)

	// Resize handle buttons (via template adapter).
	if e.resizerButton != nil {
		adapter := &devices.HexagonSpriteAdapter{Template: e.resizerButton}
		if setErr := e.elem.SetResizeButtons(adapter); setErr != nil {
			log.Printf("[SPRITE] ERROR: SetResizeButtons failed: %v", setErr)
		}
		e.elem.ShowResizeButtons(false)
		e.elem.SetResizeEnable(false)
	}

	// Sprite → local state event wiring.
	e.wireEvents()

	// Warning mark element (starts hidden).

	e.initialized = true

	// Apply any state queued before Init().
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

// =====================================================================
//  Hex menu items
// =====================================================================

// getBodyMenuItems returns body context menu items for this container.
// Order: Delete first (canonical per D4), Inspect, Resize toggle,
// Forward/Backward z-ordering.
//
// Português: Itens do menu de contexto do corpo.
// Ordem canonizada: Delete, Inspect, Resize, Forward, Backward.
func (e *StatementFunction) getBodyMenuItems() []contextMenu.Item {
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

	// The LAYER section leads the function's menu (L1): the dropdown
	// opens this same menu, so the layers live exactly where the maker
	// clicks the pill. Português: A seção de CAMADAS abre o menu — o
	// dropdown cai aqui, então as camadas moram onde o maker clica.
	items := e.layerSelectMenuItems()
	items = append(items, []contextMenu.Item{
		mainMenu.DeleteItem(func() {
			log.Printf("[Loop] delete: %v", e.id)
			e.Remove()
		}),
		mainMenu.InspectItem(func() {
			go e.showInspectOverlay()
		}),
		{
			ID:              "fn_add_param",
			Label:           translate.T("funcAddParam", "\uFF0B Parameter tunnel"),
			FontAwesomePath: seqIconPlus.Path,
			ViewBox:         seqIconPlus.ViewBox,
			HelpKey:         "helpFuncAddParam",
			HelpFallback:    "Adds an input tunnel on the LEFT border. Its name (Rename on the tunnel menu) becomes the parameter name in the generated code.",
			OnClick:         func() { e.createFunctionTunnel("left") },
		},
		{
			ID:              "fn_save_myitems",
			Label:           translate.T("funcSaveMyItems", "Save to My Items"),
			FontAwesomePath: rulesIcon.KFAFloppyDisk,
			ViewBox:         "0 0 448 512",
			HelpKey:         "helpFuncSaveMyItems",
			HelpFallback:    "Publishes this function to My Items so it can be dropped on any scene as a block. Published items are frozen — evolve by saving under a new name.",
			OnClick: func() {
				if e.saveToMyItems != nil {
					e.saveToMyItems(e.id)
				}
			},
		},
		{
			ID:              "fn_add_return",
			Label:           translate.T("funcAddReturn", "\uFF0B Return tunnel"),
			FontAwesomePath: seqIconPlus.Path,
			ViewBox:         seqIconPlus.ViewBox,
			HelpKey:         "helpFuncAddReturn",
			HelpFallback:    "Adds an output tunnel on the RIGHT border. Its name becomes the return name; wire a value into it from inside.",
			OnClick:         func() { e.createFunctionTunnel("right") },
		},
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
	}...)
	return items
}

// =====================================================================
//  Child bounds overlay — dashed rectangle shown during resize
// =====================================================================

func (e *StatementFunction) showChildBoundsOverlay(bounds rulesContainer.Rect) {
	if e.canvasEl.IsUndefined() || e.canvasEl.IsNull() {
		return
	}
	margin := rulesContainer.LoopChildMargin

	innerX := bounds.X - margin
	innerY := bounds.Y - margin
	innerW := bounds.W + 2*margin
	innerH := bounds.H + 2*margin

	cam := e.stage.GetCamera()
	zoom := 1.0
	offsetX, offsetY := 0.0, 0.0
	if cam != nil {
		zoom = cam.Zoom
		if zoom <= 0 {
			zoom = 1
		}
		offsetX = cam.OffsetX
		offsetY = cam.OffsetY
	}

	cpxX := (innerX - offsetX) * zoom
	cpxY := (innerY - offsetY) * zoom
	cpxW := innerW * zoom
	cpxH := innerH * zoom

	rect := e.canvasEl.Call("getBoundingClientRect")
	canvasW := e.canvasEl.Get("width").Float()
	canvasH := e.canvasEl.Get("height").Float()
	cssScaleX := rect.Get("width").Float() / canvasW
	cssScaleY := rect.Get("height").Float() / canvasH

	screenX := rect.Get("left").Float() + cpxX*cssScaleX
	screenY := rect.Get("top").Float() + cpxY*cssScaleY
	screenW := cpxW * cssScaleX
	screenH := cpxH * cssScaleY

	doc := js.Global().Get("document")
	div := doc.Call("createElement", "div")
	s := div.Get("style")
	s.Set("position", "fixed")
	s.Set("left", fmt.Sprintf("%.0fpx", screenX))
	s.Set("top", fmt.Sprintf("%.0fpx", screenY))
	s.Set("width", fmt.Sprintf("%.0fpx", screenW))
	s.Set("height", fmt.Sprintf("%.0fpx", screenH))
	s.Set("border", "2px dashed rgba(255, 100, 100, 0.8)")
	s.Set("borderRadius", "4px")
	s.Set("background", "rgba(255, 100, 100, 0.05)")
	s.Set("pointerEvents", "none")
	s.Set("zIndex", "9999")
	s.Set("boxSizing", "border-box")

	doc.Get("body").Call("appendChild", div)
	e.childBoundsDiv = div
}

func (e *StatementFunction) hideChildBoundsOverlay() {
	if e.childBoundsDiv.IsUndefined() || e.childBoundsDiv.IsNull() {
		return
	}
	doc := js.Global().Get("document")
	doc.Get("body").Call("removeChild", e.childBoundsDiv)
	e.childBoundsDiv = js.Undefined()
}

// =====================================================================
//  Sprite event wiring
// =====================================================================

// wireEvents connects sprite events (click, drag, resize) to local
// state handlers AND to the scenegraph lifecycle.
//
// Drag flow (container drag):
//   - DragStart → sceneMgr.BeginDrag(id) (graph snapshots descendants)
//   - DragMove  → sceneMgr.UpdateDrag(id) (graph refreshes geometry,
//     fires conflicts in real time)
//   - DragEnd   → sceneMgr.EndDrag(id, dx, dy) (graph moves descendants
//     rigidly, reassigns parentage if clean)
//
// Resize flow:
//   - ResizeStart → cache child bounds and parent inner; show overlay;
//     sceneMgr.BeginResize
//   - ResizeMove  → clamp against child bounds (minimum size)
//   - ResizeEnd   → grid-snap; clamp against both bounds; sceneMgr.EndResize
func (e *StatementFunction) wireEvents() {
	// -----------------------------------------------------------------
	//  Click — hit-test the stop button; otherwise open the hex menu.
	// -----------------------------------------------------------------
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		if e.ctxMenu == nil {
			return
		}
		if e.ctxMenu.IsOpen() {
			e.ctxMenu.Close()
			return
		}
		elemX, elemY := e.elem.GetPosition()
		menuX, menuY := elemX+event.LocalX, elemY+event.LocalY
		go e.ctxMenu.OpenForDevice(e, e.getBodyMenuItems(), menuX, menuY)
	})

	// -----------------------------------------------------------------
	//  Drag — hand the gesture over to the scenegraph.
	// -----------------------------------------------------------------
	e.elem.SetOnDragStart(func(event sprite.DragEvent) {
		e.dragStartX, e.dragStartY = e.elem.GetPosition()
		if e.sceneMgr != nil {
			e.sceneMgr.BeginDrag(e.id)
		}
	})

	// DragMove fires on every frame while the mouse is held. We use it
	// to give the graph a chance to re-check conflicts in real time,
	// so red borders pop on/off against the devices the Loop is
	// passing over as the user drags.
	e.elem.SetOnDragMove(func(event sprite.DragEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.UpdateDrag(e.id)
		}
		// Signature tunnels are wire-layer citizens with no elem — the
		// drag machinery cannot carry them; the refresher re-pins them
		// to the FRESH ornament every frame (field 2026-07-19: "os
		// túneis não são arrastados juntos"). Português: Túneis de
		// assinatura são cidadãos da wire layer, sem elem — o drag não
		// os carrega; o refresher re-pina no ornamento FRESCO a cada
		// frame.
		e.refreshFunctionTunnelViews()
	})

	e.elem.SetOnDragEnd(func(event sprite.DragEvent) {
		// Grid snap the Loop's final position.
		x, y := e.elem.GetPositionD()
		newX, newY := e.gridAdjust.AdjustCenterD(x, y)
		e.elem.SetPositionD(newX, newY)

		e.refreshFunctionTunnelViews()
		// Compute the total delta of the gesture and hand it to the
		// scenegraph, which translates every descendant by (dx, dy)
		// and reassigns parentage if conflicts are clean.
		finalX, finalY := e.elem.GetPosition()
		dx, dy := rulesContainer.DragChildDelta(e.dragStartX, e.dragStartY, finalX, finalY)
		if e.sceneMgr != nil {
			e.sceneMgr.EndDrag(e.id, dx, dy)
		}

		// The Loop's own stop-port wire needs re-routing after the move.
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
		}

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// -----------------------------------------------------------------
	//  Resize — cache floor (children bounds) and ceiling (parent
	//  inner); clamp during move; apply both clamps at end.
	// -----------------------------------------------------------------
	e.elem.SetOnResizeStart(func(event sprite.ResizeEvent) {
		if e.sceneMgr != nil {
			e.sceneMgr.BeginResize(e.id)
		}

		e.resizeChildBounds = nil
		if e.sceneMgr != nil {
			if b := e.sceneMgr.ChildrenBounds(e.id); b != nil {
				cb := rulesContainer.Rect{X: b.X, Y: b.Y, W: b.Width, H: b.Height}
				e.resizeChildBounds = &cb
				e.showChildBoundsOverlay(cb)
			}
		}

		e.resizeParentInner = nil
		if e.sceneMgr != nil {
			if p := e.sceneMgr.ParentInnerBBox(e.id); p != nil {
				pi := rulesContainer.Rect{X: p.X, Y: p.Y, W: p.Width, H: p.Height}
				e.resizeParentInner = &pi
			}
		}
	})

	e.elem.SetOnResizeMove(func(event sprite.ResizeEvent) {
		// Only the minimum-size clamp (children floor) runs in move.
		// The maximum-size clamp (parent ceiling) runs at end — doing
		// it mid-move fights the sprite's internal size tracking and
		// makes the element jump to the parent's maximum.
		if e.resizeChildBounds != nil {
			loopX, loopY := e.elem.GetPosition()
			loopW, loopH := e.elem.GetSize()
			proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: loopW, H: loopH}
			pad := rulesContainer.LoopPadding()
			clamped := rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)

			if clamped.X != loopX || clamped.Y != loopY {
				e.elem.SetPosition(clamped.X, clamped.Y)
			}
			if clamped.W != loopW || clamped.H != loopH {
				e.elem.SetSize(clamped.W, clamped.H)
			}
		}
		// Rails move with the border — re-pin per resize frame too.
		// Português: Trilhos acompanham a borda — re-pina por frame.
		e.refreshFunctionTunnelViews()
	})

	e.elem.SetOnResizeEnd(func(event sprite.ResizeEvent) {
		e.hideChildBoundsOverlay()
		e.refreshFunctionTunnelViews()

		// Grid snap the new size.
		wD, hD := e.elem.GetSizeD()
		newW, newH := e.gridAdjust.AdjustCenterD(wD, hD)
		loopX, loopY := e.elem.GetPosition()
		proposed := rulesContainer.Rect{X: loopX, Y: loopY, W: newW.GetFloat(), H: newH.GetFloat()}

		// Minimum clamp: can't shrink past children.
		if e.resizeChildBounds != nil {
			pad := rulesContainer.LoopPadding()
			proposed = rulesContainer.ClampResize(proposed, *e.resizeChildBounds, pad, rulesContainer.LoopChildMargin)
		}

		// Maximum clamp: can't grow past parent container.
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

		// Exit resize mode; re-enable drag automatically. Without
		// this, the element stays in resize mode after the handle is
		// released, making it undraggable.
		e.SetResizeEnable(false)
		e.SetDragEnable(true)

		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	})

	// Periodic resize redraw while the handle is held.
	e.elem.SetResizeRedrawInterval(1000)
	e.elem.SetOnResizeRedraw(func(event sprite.ResizeEvent) {
		go e.recacheOrnament()
	})

	// Cursor hit-test: pointer only over the stop button; default
	// cursor everywhere else (drag is the default).
	e.elem.SetCursorHitTest(func(localX, localY float64) sprite.CursorStyle {
		return ""
	})
}

// =====================================================================
//  Rendering helpers
// =====================================================================

func (e *StatementFunction) recacheOrnament() {
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
}

// =====================================================================
//  Icon (for hardware menu / palette)
// =====================================================================

func (e *StatementFunction) GetIconName() string     { return "Loop" }
func (e *StatementFunction) GetIconCategory() string { return "Loop" }
func (e *StatementFunction) SetStatus(status int)    { e.iconStatus = status }
func (e *StatementFunction) GetStatus() int          { return e.iconStatus }

func (e *StatementFunction) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)

	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).
		Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).
		Height(rulesIcon.Height.GetInt())

	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).
		Stroke(data.ColorBorder).
		Fill(data.ColorBackground).
		D(hexPath)

	xc := rulesIcon.Width / 4
	yc := rulesIcon.Height * 0.15
	wOrn := rulesIcon.Width / 2
	icon := e.ornamentDrawIcon.GetSvg().
		X(xc.GetInt()).
		Y(yc.GetInt())
	_ = e.ornamentDrawIcon.Update(0, 0, wOrn, wOrn)

	widthLabel, _ := utilsText.GetTextSize(
		data.Label, rulesIcon.FontFamily, rulesIcon.FontWeight,
		rulesIcon.FontStyle, data.LabelFontSize.GetInt(),
	)
	label := factoryBrowser.NewTagSvgText().
		FontFamily(rulesIcon.FontFamily).
		FontWeight(rulesIcon.FontWeight).
		FontStyle(rulesIcon.FontStyle).
		FontSize(data.LabelFontSize.GetInt()).
		Text(data.Label).
		Fill(data.ColorLabel).
		X((rulesIcon.Width / 2).GetInt() - widthLabel/2).
		Y(data.LabelY.GetInt())
	svgIcon.Append(hexDraw, icon, label)

	w := rulesIcon.Width * rulesIcon.SizeRatio
	h := rulesIcon.Height * rulesIcon.SizeRatio
	return svgIcon.ToCanvas(html.CanvasData{Width: w.GetInt(), Height: h.GetInt()})
}

// =====================================================================
//  Wire connectors
// =====================================================================

// RegisterConnectors registers the stop port as a wire input. true on
// the stop port breaks the loop.
func (e *StatementFunction) RegisterConnectors() {
	if e.wireMgr == nil || e.elem == nil {
		return
	}
	// (no wire-layer connector — see the three-layers note in Init)

	// Register the container's live rect so the wire manager can draw a
	// LabVIEW-style tunnel marker where a wire crosses this container's border,
	// matching StatementCase. The closure returns the current geometry so the
	// marker tracks drags and resizes; a nil element yields a zero rect, which
	// the renderer skips. The SeqBorder is drawn inset by
	// margin = Density(10) — the same inset caseBorder uses — so the tunnel sits
	// on that visible border line, not on the outer (mathematical) bounding box.
	// This is frontend-only: the tunnel is an overlay on an existing wire and
	// changes nothing in codegen, the serializer, or persistence.
	//
	// Português: Registra o rect vivo do container para o wire manager desenhar
	// o marcador de túnel (estilo LabVIEW) onde um fio cruza a borda, igual ao
	// StatementCase. O closure devolve a geometria atual para o marcador
	// acompanhar arrasto e resize; elemento nil dá rect zero, que o renderer
	// ignora. A borda SeqBorder é desenhada com inset margin = Density(10)
	// (o mesmo do caseBorder), então o túnel senta nessa linha visível, não na
	// bbox externa. É só frontend: o túnel é overlay sobre um fio existente e não
	// muda nada no codegen, no serializer nem na persistência.
	e.wireMgr.RegisterContainer(e.id, func() (x, y, w, h float64) {
		if e.elem == nil {
			return 0, 0, 0, 0
		}
		ex, ey := e.elem.GetPosition()
		ew, eh := e.elem.GetSize()
		m := rulesDensity.Density(10).GetFloat()
		return ex + m, ey + m, ew - 2*m, eh - 2*m
	})
}

// showInspectOverlay opens the container's Inspect: a Properties tab whose
// only editable datum is the universal COMMENT (containers have no other
// form-editable state — membership and geometry live on the stage).
// Português: Abre o Inspect do container: aba Properties cujo único dado
// editável é o COMENTÁRIO universal (containers não têm outro estado
// editável por formulário — membros e geometria vivem no stage).
func (e *StatementFunction) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

func (e *StatementFunction) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: translate.T("tabProperties", "Properties"),
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
					{
						Key:         "functionName",
						Label:       translate.T("propFunctionName", "Function name"),
						Type:        overlay.FieldText,
						Value:       e.functionName,
						Placeholder: "my_function",
					},
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
		},
		OnSave: func(values map[string]string) {
			e.ApplyProperties(values)
		},
	}
}

// ApplyProperties restores the comment — the container's only form-editable
// datum. Membership and size are restored by the scene import machinery.
// Português: Restaura o comentário — único dado editável por formulário do
// container. Membros e tamanho são restaurados pelo import da cena.
func (e *StatementFunction) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if s := values["layers"]; s != "" {
		type layerWire struct {
			ID    string   `json:"id"`
			Label string   `json:"label"`
			IDs   []string `json:"ids"`
		}
		var ls []layerWire
		if err := json.Unmarshal([]byte(s), &ls); err == nil {
			e.layers = e.layers[:0]
			for _, l := range ls {
				if l.ID != "" {
					e.layers = append(e.layers, functionLayer{
						id: l.ID, label: l.Label, ids: l.IDs,
					})
				}
			}
		}
	}
	if v := values["selectedLayer"]; v != "" {
		e.selectedLayer = v
	}
	if v, ok := values["functionName"]; ok && v != "" {
		e.functionName = v
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetCaseLabel(e.functionName)
		}
		go e.recacheOrnament()
	}
}

// GetProperties exports the container's scene properties. Until the comment
// existed, loops exported none — which is why this function is new: the
// serializer collects it via interface assertion, so its presence alone
// makes the comment flow scene → graph → IR (the LOOP_BEGIN stamp).
// Português: Exporta as properties de cena do container. Até o comentário
// existir, loops não exportavam nenhuma — por isso esta função é nova: o
// serializer a coleta por assertion, então a presença dela já faz o
// comentário fluir cena → graph → IR (o carimbo do LOOP_BEGIN).
func (e *StatementFunction) GetProperties() map[string]interface{} {
	props := map[string]interface{}{
		// Always exported — the server-side builder reads it into
		// Scope.FunctionName. Português: Sempre exportado — o builder lê
		// para Scope.FunctionName.
		"functionName": e.functionName,
	}
	if e.comment != "" {
		props["comment"] = e.comment
	}
	if len(e.layers) > 0 {
		// Layers travel as a JSON STRING: ReplayProperties flattens
		// every value through fmt.Sprintf, which would mangle a slice
		// — a pre-marshaled string passes intact (the house pattern
		// for structured props). Português: Camadas viajam como STRING
		// JSON — o achatador do ReplayProperties destruiria um slice;
		// a string pré-serializada passa intacta (padrão da casa).
		type layerWire struct {
			ID    string   `json:"id"`
			Label string   `json:"label,omitempty"`
			IDs   []string `json:"ids,omitempty"`
		}
		ls := make([]layerWire, 0, len(e.layers))
		for k := range e.layers {
			ls = append(ls, layerWire{
				ID:    e.layers[k].id,
				Label: e.layers[k].label,
				IDs:   append([]string(nil), e.layers[k].ids...),
			})
		}
		if b, err := json.Marshal(ls); err == nil {
			props["layers"] = string(b)
			props["selectedLayer"] = e.selectedLayer
		}
	}
	return props
}

// GetComment returns the user comment shown in generated code and in the
// container's hover tooltip.
// Português: Retorna o comentário do usuário exibido no código gerado e
// no tooltip de hover do container.
func (e *StatementFunction) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementFunction) SetComment(c string) { e.comment = c }
