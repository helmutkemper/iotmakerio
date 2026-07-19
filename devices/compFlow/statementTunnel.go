// /ide/devices/compFlow/statementTunnel.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// statementTunnel.go — pass-through waypoint (the LabVIEW
// sequence-local as a device; chameleon v2, ALL wire types including
// []T pointer+len slices — Kemper 2026-07-18, retiring the int-only
// v1 of 2026-07-16).
//
// One In, one Out; codegen emits a single honest line, tunnel_1 = <src>
// — transparent by the Sequence's own law, yet NAMEABLE: the crossing
// gets an address on the border. Born from the Sequence's pill/body
// menus; the maker drags it where the crossing should live.
//
// Português: O waypoint de passagem — In/Out; o codegen emite uma linha
// honesta (tunnel_1 = <fonte>), transparente pela lei do Sequence, mas
// NOMEÁVEL: o cruzamento ganha endereço na borda.

import (
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
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesDevice"
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

// StatementTunnel is a set-variable device for type int.
// Single input connector "value" — assigns the wired value to the variable.
type StatementTunnel struct {
	stage sprite.Stage
	elem  sprite.Element

	id    string
	name  string
	label string // editable name shown below ornament (defaults to id)
	// [COMMENT] user comment — appears as `// ` lines above this device's
	// statement in the generated code, in the Code Preview, and in the
	// device's hover tooltip.
	// Português: Comentário do usuário — vira linhas `// ` acima do
	// statement deste device no código gerado, no Code Preview e no
	// tooltip de hover do device.
	comment string

	// Phase-tunnel state (redesign 2026-07-17): parentID+side pin it to a
	// Sequence border (side is the BIRTH edge and the drag rail — the
	// existing external-tunnel behaviour, per spec #2); fresh drives the
	// red blink until the FIRST interaction of any kind (spec #3);
	// hasWire flips outline→filled. Português: parentID+lado prendem à
	// borda (o lado é o trilho do arraste); fresh pisca até a primeira
	// interação; hasWire alterna vazado→preenchido.
	parentID   string
	side       string
	pendingX   float64
	pendingY   float64
	hasPending bool
	fresh      bool
	clamping   bool
	dropJudge  func(cx, cy float64, side, selfID string) (float64, float64)
	hasWire    bool
	// natalCase is the Sequence phase (case id) this tunnel was born in
	// (Kemper spec 2026-07-18) — persisted as "tunnelNatal"; role and
	// phase-visibility are derived from it by the owning Sequence.
	// Português: Fase natal — persistida como "tunnelNatal"; papel e
	// visibilidade por fase derivam dela no Sequence dono.
	natalCase string
	// pendingRemoved carries the persisted phase-removal set ("assim um
	// túnel pode ser ocultado", Kemper 2026-07-18) from ApplyProperties
	// to the manager record — ONE-SHOT: consumed on the first sync so a
	// later position sync never clobbers runtime menu edits with a stale
	// restore snapshot. nil = nothing pending. Português: Conjunto de
	// remoções persistido, do ApplyProperties ao registro — UM TIRO:
	// consumido no primeiro sync para um sync de posição posterior não
	// sobrescrever edições de menu com o snapshot velho do restore.
	pendingRemoved []string

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

func (e *StatementTunnel) SetStage(s sprite.Stage)               { e.stage = s }
func (e *StatementTunnel) SetWireManager(m *wire.Manager)        { e.wireMgr = m }
func (e *StatementTunnel) SetResizerButton(r block.ResizeButton) { e.resizerButton = r }
func (e *StatementTunnel) SetDraggerButton(_ block.ResizeButton) {}
func (e *StatementTunnel) SetGridAdjust(g grid.Adjust)           { e.gridAdjust = g }

// SetContextMenu injects the linear context menu controller.
// Português: Injeta o controller do menu de contexto linear.
func (e *StatementTunnel) SetContextMenu(c *contextMenu.Controller) {
	e.ctxMenu = c
}
func (e *StatementTunnel) SetOnRemove(fn func(id string)) { e.onRemove = fn }

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (e *StatementTunnel) Append() {
	if e.elem != nil {
		e.elem.SetVisible(true)
	}
}

func (e *StatementTunnel) Remove() {
	if e.wireMgr != nil {
		e.wireMgr.RemoveManualTunnel(e.id)
	}
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
// renderSVG draws the phase-tunnel marker square — same footprint as
// the element (no label strip; the border is the address).
// Português: Desenha o quadrado-marker — mesmo footprint do elemento.
func (e *StatementTunnel) renderSVG() string {
	w := e.width.GetFloat()
	h := e.height.GetFloat()
	totalH := h
	// (stale left-pin prose removed with the redesign 2026-07-17: the
	// element size itself is unchanged, so grid layout and saved scenes are
	// unaffected.
	// Português: O corpo recua à ESQUERDA o comprimento do pino: o pino
	// padrão vive na margem liberada, saindo da borda com o fio ancorado na
	// ponta externa — a borda esquerda do element. O tamanho do element não
	// muda — grid e cenas salvas não são afetados.
	// PHASE-TUNNEL LOOK (redesign 2026-07-17, Kemper's spec): the same
	// square grammar as the automatic border dots (drawTunnelMarker:
	// filled square + dark stroke), in the INTERNAL palette — violet,
	// unmistakable next to any wire-type colour. States: fresh = blink
	// violet↔red until the first interaction; unwired = outlined;
	// wired = filled. The junction dot sits at the CENTER — one point,
	// two directions (starting a wire there grabs the Out, landing one
	// there finds the In).
	// Português: A MESMA gramática do dot automático, na paleta INTERNA
	// (violeta). Fresco = pisca até a primeira interação; sem fio =
	// vazado; com fio = preenchido. A junção mora no CENTRO — um ponto,
	// duas direções.
	stroke := kTunnelViolet
	fill := "none"
	if e.hasWire {
		fill = kTunnelViolet
	}
	if e.fresh {
		stroke = kTunnelBlinkRed
	}
	inset := 1.5
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		int(w), int(totalH))
	svg += fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" stroke="%s" stroke-width="2"/>`,
		inset, inset, w-2*inset, h-2*inset, fill, stroke,
	)
	// junction dot (center)
	dotFill := kTunnelViolet
	if e.fresh {
		dotFill = kTunnelBlinkRed
	}
	svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="3.2" fill="%s" stroke="#1e1e1e" stroke-width="0.8"/>`,
		w/2, h/2, dotFill,
	)
	svg += `</svg>`
	return svg
}

func (e *StatementTunnel) recacheSVG() {
	if e.elem != nil {
		_ = e.elem.CacheFromSvg(e.renderSVG())
	}
}

// clampToRail projects the square back onto its birth edge of the parent
// container — the SAME side-locked slide the external tunnels practice
// (spec #2: "podemos usar o mesmo código dele"; the math mirrors
// wire.projectToEdge). No parent bound → free device, no clamp.
// Português: Projeta o quadrado de volta ao trilho — o MESMO deslize
// preso-ao-lado dos túneis externos; sem pai vinculado, sem clamp.
func (e *StatementTunnel) clampToRail() {
	if e.clamping {
		return
	}
	e.clamping = true
	defer func() { e.clamping = false }()
	if e.parentID == "" || e.stage == nil || e.elem == nil {
		return
	}
	parent, found := e.stage.GetElement(e.parentID)
	if !found {
		return
	}
	// The rail is the ORNAMENTAL border — inset Density(10) from the
	// element bounds, the same rect RegisterContainer feeds the wire
	// layer (field lesson 2026-07-17: clamping to the mathematical box
	// floated the square outside the visible border). Português: O
	// trilho é a borda do ORNAMENTO — inset Density(10), o mesmo rect
	// do RegisterContainer.
	px, py := parent.GetPosition()
	pw, ph := parent.GetSize()
	m := rulesDensity.Density(10).GetFloat()
	px, py, pw, ph = px+m, py+m, pw-2*m, ph-2*m
	x, y := e.elem.GetPosition()
	s := e.width.GetFloat()
	cx, cy := x+s/2, y+s/2
	clamp := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	switch e.side {
	case "top":
		cx, cy = clamp(cx, px, px+pw), py
	case "bottom":
		cx, cy = clamp(cx, px, px+pw), py+ph
	case "right":
		cx, cy = px+pw, clamp(cy, py, py+ph)
	default: // left
		cx, cy = px, clamp(cy, py, py+ph)
	}
	if e.dropJudge != nil {
		cx, cy = e.dropJudge(cx, cy, e.side, e.id)
	}
	// No-op guard: if the clamped-and-judged spot is where we already
	// are (sub-pixel), touch NOTHING — the convergence anchor that makes
	// any re-entry chain terminate. Português: Se o destino é onde já
	// estamos (sub-pixel), não toca em NADA — a âncora de convergência.
	curX, curY := e.elem.GetPosition()
	nx, ny := cx-s/2, cy-s/2
	dx, dy := nx-curX, ny-curY
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx < 0.5 && dy < 0.5 {
		return
	}
	e.SetPosition(rulesDensity.Density(nx), rulesDensity.Density(ny))
	e.recacheSVG()
}

// SetDropJudge injects the Sequence's anti-occlusion judge; SideLen
// exposes the square's edge for cradle math. Português: Injeta o juiz
// anti-sobreposição; SideLen expõe o lado do quadrado.
func (e *StatementTunnel) SetDropJudge(j func(cx, cy float64, side, selfID string) (float64, float64)) {
	e.dropJudge = j
}
func (e *StatementTunnel) SideLen() float64 { return kTunnelSide.GetFloat() }

// SetRail binds the tunnel to its parent container's edge (creation
// path). Português: Prende o túnel ao trilho do pai (criação).
func (e *StatementTunnel) SetRail(parentID, side string) {
	e.parentID = parentID
	e.side = side
	e.syncManagerRecord(false)
}

// MarkFresh arms the birth blink (violet↔red) — called ONLY by the
// creation path, never by restore, so reloaded scenes stay calm. Any
// interaction (spec #3: drag, click, double-click, wire, secondary
// click) calls touch() and the blink dies. Português: Arma o pisca de
// nascimento — só o caminho de criação chama; qualquer interação mata.
// MarkFresh: BISECTION BUILD 2026-07-17 — the freshness signal is a
// SOLID red square (no ticker goroutine at all) until the first
// interaction. The field froze the tab twice; this build removes every
// loop this device owns so the survivors can be named if it freezes
// again. The pulsing animation returns via the engine ticker precedent
// (CommStatus) once the field is calm. Português: Sinal de nascimento =
// vermelho SÓLIDO, sem goroutine — build de bisseção; a pulsação volta
// pelo ticker do engine quando o campo acalmar.
func (e *StatementTunnel) MarkFresh() {
	e.fresh = true
	e.recacheSVG()
}

func (e *StatementTunnel) touch() {
	if !e.fresh {
		return
	}
	e.fresh = false
	e.recacheSVG()
}

// OnWireConnected — the wire manager's hook: a wire on either port
// counts as interaction AND flips the square to filled.
// Português: Fio em qualquer porta conta como interação E preenche.
func (e *StatementTunnel) OnWireConnected(portName string, dataType string) {
	e.hasWire = true
	e.touch()
	e.recacheSVG()
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (e *StatementTunnel) Init() (err error) {
	// SHELL DEVICE (redesign 2026-07-17, "copie o túnel original"): the
	// phase-tunnel's body LIVES IN THE WIRE LAYER as a ManualTunnel —
	// same square, same move mode, same menu as the field-proven
	// automatic markers. This device is the DATA shell only: identity,
	// connectors (anchored on the manager's live point), properties,
	// serialization, codegen. NO sprite element — the freezing drag
	// pipeline ceases to exist by construction; every inherited handler
	// below is elem-guarded and therefore inert.
	//
	// Português: DEVICE-CASCA — o corpo do túnel de fase VIVE NA WIRE
	// LAYER como ManualTunnel (mesmo quadrado, mesmo mover, mesmo menu
	// dos marcadores automáticos). Este device é só a casca de dados;
	// SEM elemento sprite — o pipeline de arraste que congelava deixa
	// de existir por construção.
	if e.stage == nil {
		return fmt.Errorf("stage not set")
	}
	e.id = rulesSequentialId.GetIdFromBase("tunnel")
	if e.name == "" {
		e.name = e.id
	}
	if e.label == "" {
		e.label = e.id
	}
	e.width, e.height = kTunnelSide, kTunnelSide
	e.initialized = true
	return nil
}

// ── Events ────────────────────────────────────────────────────────────────────

func (e *StatementTunnel) wireEvents() {
	e.elem.SetOnClick(func(event sprite.PointerEvent) {
		e.touch()
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
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "in"), menuX, menuY)
			return
		}

		w, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			w-rulesConnection.PinBodyInset(), e.height.GetFloat()/2,
			event.LocalX, event.LocalY) {
			go e.ctxMenu.OpenAtWorld(mainMenu.ConnectorConnectMenu(e.wireMgr, e.id, "out"), menuX, menuY)
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
		// RAIL-NATIVE drop (field freeze forensics 2026-07-17, "second
		// tunnel + drag"): the inherited pipeline ran the GRID SNAP
		// *after* the rail clamp — AdjustCenterD yanked the square off
		// the just-clamped rail point (and, with a sibling nearby, could
		// park it ON the neighbour: the exact ingredient only a second
		// tunnel brings). Rails are not grids: the snap is GONE, the
		// clamp is the only authority, and it carries a re-entrancy
		// latch + a no-op guard so no event semantics can ping-pong it.
		// sceneMgr.EndDrag was dead weight (nil by the sibling-creator
		// rule). Português: Drop NATIVO DE TRILHO — o snap de grade
		// rodava DEPOIS do clamp e arrancava o quadrado do trilho (com
		// vizinho, estacionava em cima dele). Trilho não é grade: snap
		// removido; o clamp é a única autoridade, com trava de
		// reentrada e no-op-guard.
		e.touch()
		e.clampToRail()
		if e.wireMgr != nil {
			e.wireMgr.RecalculateForElement(e.id)
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
		wcur, _ := e.elem.GetSize()
		if rulesConnection.PinHit(rulesConnection.PinSideRight,
			wcur-rulesConnection.PinBodyInset(), e.height.GetFloat()/2, lx, ly) {
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
func (e *StatementTunnel) bodyMenuItems() []contextMenu.Item {
	return []contextMenu.Item{
		mainMenu.DeleteItem(func() { log.Printf("[Tunnel] delete: %s", e.id); e.Remove() }),
		mainMenu.InspectItem(func() { go e.showInspectOverlay() }),
	}
}

// ── Inspect overlay ───────────────────────────────────────────────────────────

func (e *StatementTunnel) showInspectOverlay() { overlay.Show(e.inspectConfig()) }

// ShowInspect — the exported door the workspace's manual-tunnel menu
// uses (the shell's own bodyMenuItems retired with the G3 conversion).
// Português: A porta exportada que o menu do workspace usa (o
// bodyMenuItems da casca aposentou na conversão G3).
func (e *StatementTunnel) ShowInspect() { e.showInspectOverlay() }

func (e *StatementTunnel) inspectConfig() overlay.Config {
	return overlay.Config{
		Title: e.id,
		Width: "480px",
		Tabs: []overlay.Tab{
			{
				Label: "Properties",
				Type:  overlay.TabForm,
				Fields: []overlay.Field{
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
				Content:  devices.CommentPrefix(e.comment) + "// Tunnel: named pass-through waypoint (out = in).\n// A place where data crosses a boundary — clickable, nameable, movable.",
				Language: "go", ReadOnly: true,
			},
			{Label: "Help", Type: overlay.TabMarkdown, Content: tunnelHelp()},
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
				if e.wireMgr != nil {
					e.wireMgr.SetManualTunnelComment(e.id, e.comment)
				}
			}
			if lbl, ok := values["label"]; ok {
				e.label = lbl
				if e.wireMgr != nil {
					e.wireMgr.SetManualTunnelLabel(e.id, e.label)
				}
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

func (e *StatementTunnel) GetInspectConfig() interface{} { return e.inspectConfig() }
func (e *StatementTunnel) ApplyProperties(values map[string]string) {
	if v, ok := values["comment"]; ok {
		e.comment = v
	}
	if lbl, ok := values["label"]; ok {
		e.label = lbl
	}
	if p, ok := values["tunnelParent"]; ok {
		e.parentID = p
	}
	if s, ok := values["tunnelSide"]; ok {
		e.side = s
	}
	// Natal phase (Kemper spec 2026-07-18): the case id the tunnel was
	// born in — order-proof like pendingX/Y: stored here, pushed to the
	// manager record by syncManagerRecord whenever parent+point are
	// known. Old scenes without the key restore with "" and the owning
	// Sequence falls back to phase 0. Português: Fase natal — à prova de
	// ordem como pendingX/Y; cenas antigas caem no fallback (fase 0).
	if n, ok := values["tunnelNatal"]; ok {
		e.natalCase = n
	}
	// Phase-removal set — comma-joined case ids (the map[string]string
	// channel carries no arrays). Parsed into the ONE-SHOT pending;
	// syncManagerRecord pushes it once the record exists. Português:
	// Conjunto de remoções — ids separados por vírgula (o canal
	// map[string]string não leva arrays). Vai para o pendente de UM
	// TIRO; o syncManagerRecord empurra quando o registro existir.
	if r, ok := values["tunnelRemoved"]; ok && r != "" {
		e.pendingRemoved = strings.Split(r, ",")
	}
	e.syncManagerRecord(false)
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.recacheSVG()
		if e.sceneNotify != nil {
			e.sceneNotify()
		}
	}()
}

// ── Wire connectors ───────────────────────────────────────────────────────────

func (e *StatementTunnel) RegisterConnectors() {
	// SHELL: no element — the inherited elem-guard was silently skipping
	// registration, leaving the tunnel INVISIBLE to the whole connect
	// system (field 2026-07-17, "não ligou int" + the truthful-but-
	// misleading No-compatible-target toast). The anchors read the
	// manager point; only the wire manager is required. Português: A
	// guarda de elem herdada pulava o registro em silêncio — o túnel
	// ficava INVISÍVEL para o sistema de conexão. As âncoras leem o
	// ponto do manager; só o wire manager é necessário.
	//
	// Phase-view anchors (Kemper spec 2026-07-18) follow the MathAdd
	// standard — the wire attaches at the PIN'S OUTER TIP
	// (rulesConnection.PinAnchor), and both pins point INTO the
	// sequence: "in" lives on the natal phase's RIGHT-border square, pin
	// protruding LEFT (a source's output reaches it in a straight
	// left-to-right line); "out" lives on the later phases' LEFT-border
	// square, pin protruding RIGHT (it reaches a consumer's input in a
	// straight line). Which port is offered when is the wire manager's
	// phase gating; here we only anchor. In takes ONE feed (MathAdd
	// input standard, MaxConnections 1 — one crossing, one value); Out
	// fans out unlimited. Both stay AcceptNotConnected: a maker
	// mid-build has dangling tunnels constantly.
	//
	// CHAMELEON v2 (Kemper 2026-07-18, retiring the int-only v1 of
	// 2026-07-16): AllowedTypes is the wildcard "*" — the tunnel ADOPTS
	// the first concrete type wired into it, any type the wire layer
	// knows: scalars, strings, bools, probe pointers, []T slices (the
	// C99 pointer+len family) and complex black-box handles. The live
	// narrowing (stamped tunnel offers exactly its type, promotions
	// included) happens in the manager's effectiveTypes — where the
	// wires live — so nothing here re-registers on connect/disconnect.
	// Português: Âncoras por fase no padrão MathAdd — fio na PONTA
	// EXTERNA do pino, pinos PARA DENTRO. CAMALEÃO v2 (aposenta o
	// int-only v1): AllowedTypes é o curinga "*" — o túnel ADOTA o
	// primeiro tipo concreto ligado nele, qualquer tipo da wire layer:
	// escalares, strings, bools, sondas, fatias []T (a família
	// ponteiro+len do C99) e handles complexos. O estreitamento vivo
	// mora no effectiveTypes do manager — onde moram os fios.
	if e.wireMgr == nil {
		return
	}
	half := kTunnelSide.GetFloat() / 2
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "in"},
		IsOutput:           false,
		AllowedTypes:       []string{"*"},
		AcceptNotConnected: true,
		MaxConnections:     1,
		Label:              "In",
		PositionFunc: func() (float64, float64) {
			// Left face of the square, then the standard pin tip.
			// Português: Face esquerda do quadrado, ponta do pino padrão.
			if p, ok := e.wireMgr.ManualTunnelPoint(e.id); ok {
				return rulesConnection.PinAnchor(rulesConnection.PinSideLeft,
					p.X-half, p.Y)
			}
			return 0, 0
		},
	})
	e.wireMgr.RegisterConnector(wire.ConnectorInfo{
		ID:                 wire.ConnectorID{ElementID: e.id, PortName: "out"},
		IsOutput:           true,
		AllowedTypes:       []string{"*"},
		AcceptNotConnected: true,
		MaxConnections:     0,
		Label:              "Out",
		PositionFunc: func() (float64, float64) {
			// Right face of the square, then the standard pin tip.
			// Português: Face direita do quadrado, ponta do pino padrão.
			if p, ok := e.wireMgr.ManualTunnelPoint(e.id); ok {
				return rulesConnection.PinAnchor(rulesConnection.PinSideRight,
					p.X+half, p.Y)
			}
			return 0, 0
		},
	})
}

// ── Geometry ──────────────────────────────────────────────────────────────────

func (e *StatementTunnel) SetName(n string)  { e.name = rulesSequentialId.GetIdFromBase(n) }
func (e *StatementTunnel) Get() *html.TagSvg { return nil }

// SetPosition (scene import path) stores the desired CENTER and syncs
// the manager record when the rail is known — order-proof against any
// ApplyProperties/SetPosition sequence. Português: Guarda o CENTRO
// desejado e sincroniza o registro quando o trilho é conhecido — imune
// à ordem ApplyProperties/SetPosition do import.
func (e *StatementTunnel) SetPosition(x, y rulesDensity.Density) {
	s := kTunnelSide.GetFloat()
	e.pendingX = x.GetFloat() + s/2
	e.pendingY = y.GetFloat() + s/2
	e.hasPending = true
	e.syncManagerRecord(false)
}

// syncManagerRecord pushes the pending point into the wire layer once
// parent+side are known. fresh only on the CREATION path. The natal
// phase rides along AFTER the point: AddManualTunnel may create the
// record on this very call, and SetManualTunnelNatal is a no-op on a
// missing record — so the order is load-bearing. Português: Empurra o
// ponto pendente para a wire layer; o natal vai DEPOIS do ponto (a
// ordem importa: o registro pode nascer nesta chamada).
func (e *StatementTunnel) syncManagerRecord(fresh bool) {
	if e.wireMgr == nil || e.parentID == "" || !e.hasPending {
		return
	}
	e.wireMgr.AddManualTunnel(e.id, e.parentID, e.side,
		wire.Point{X: e.pendingX, Y: e.pendingY}, fresh)
	if e.natalCase != "" {
		e.wireMgr.SetManualTunnelNatal(e.id, e.natalCase)
	}
	// The label rides every sync AND has a live setter path (the
	// inspect form's OnSave) — the renderer paints from the record.
	// Português: O rótulo vai em todo sync E tem via de setter vivo
	// (OnSave do formulário) — o renderer pinta do registro.
	e.wireMgr.SetManualTunnelLabel(e.id, e.label)
	e.wireMgr.SetManualTunnelComment(e.id, e.comment)
	// One-shot: the restore snapshot is pushed exactly once, then
	// forgotten — later position syncs must not overwrite menu edits.
	// Português: Um tiro — o snapshot do restore entra uma vez e é
	// esquecido; syncs de posição posteriores não sobrescrevem o menu.
	if e.pendingRemoved != nil {
		e.wireMgr.SetManualTunnelRemovedCases(e.id, e.pendingRemoved)
		e.pendingRemoved = nil
	}
}

// SetNatalCase stamps the birth phase on the shell (the serialization
// vehicle) and syncs it into the manager record. Called by the owning
// Sequence right after creation. Português: Carimba a fase natal na
// casca (o veículo de serialização) e sincroniza no registro.
func (e *StatementTunnel) SetNatalCase(caseID string) {
	e.natalCase = caseID
	if e.wireMgr != nil && caseID != "" {
		e.wireMgr.SetManualTunnelNatal(e.id, caseID)
	}
}
func (e *StatementTunnel) SetSize(w, h rulesDensity.Density) {
	e.width, e.height = w, h
	if e.elem != nil {
		e.elem.SetSizeD(w, h)
	}
}
func (e *StatementTunnel) GetWidth() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetWidthD()
	}
	return e.width
}
func (e *StatementTunnel) GetHeight() rulesDensity.Density { return e.height }
func (e *StatementTunnel) GetX() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetXD()
	}
	return 0
}
func (e *StatementTunnel) GetY() rulesDensity.Density {
	if e.elem != nil {
		return e.elem.GetYD()
	}
	return 0
}
func (e *StatementTunnel) SetX(x rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(x, e.elem.GetYD())
	}
}
func (e *StatementTunnel) SetY(y rulesDensity.Density) {
	if e.elem != nil {
		e.elem.SetPositionD(e.elem.GetXD(), y)
	}
}
func (e *StatementTunnel) SetWidth(w rulesDensity.Density) {
	e.width = w
	if e.elem != nil {
		e.elem.SetSizeD(w, e.height)
	}
}
func (e *StatementTunnel) SetHeight(h rulesDensity.Density) {
	e.height = h
	if e.elem != nil {
		e.elem.SetSizeD(e.elem.GetWidthD(), h)
	}
}
func (e *StatementTunnel) MoveBy(dx, dy float64) {
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

func (e *StatementTunnel) GetInitialized() bool   { return e.initialized }
func (e *StatementTunnel) GetID() string          { return e.id }
func (e *StatementTunnel) GetName() string        { return e.name }
func (e *StatementTunnel) GetSelected() bool      { return e.selected }
func (e *StatementTunnel) GetSelectBlocked() bool { return e.selectLocked }
func (e *StatementTunnel) GetDragEnable() bool    { return e.dragEnabled }
func (e *StatementTunnel) GetDragBlocked() bool   { return e.dragLocked }
func (e *StatementTunnel) GetResizeEnable() bool {
	if e.elem != nil {
		return e.elem.IsResizeEnabled()
	}
	return false
}
func (e *StatementTunnel) GetResize() bool        { return e.GetResizeEnable() }
func (e *StatementTunnel) GetResizeBlocked() bool { return e.resizeLocked }
func (e *StatementTunnel) GetZIndex() int {
	if e.elem != nil {
		return e.elem.GetIndex()
	}
	return 0
}
func (e *StatementTunnel) GetStatus() int  { return e.iconStatus }
func (e *StatementTunnel) SetStatus(s int) { e.iconStatus = s }
func (e *StatementTunnel) SelectedInvert() { e.SetSelected(!e.selected) }

func (e *StatementTunnel) SetSelected(sel bool) {
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

func (e *StatementTunnel) SetDragEnable(en bool) {
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

func (e *StatementTunnel) SetResizeEnable(_ bool) {
	// Constant devices never resize — resizeLocked is always true.
	if e.elem != nil {
		e.elem.SetResizeEnable(false)
		e.elem.ShowResizeButtons(false)
	}
}

// ── Icon ──────────────────────────────────────────────────────────────────────

func (e *StatementTunnel) GetIconName() string     { return "Tunnel" }
func (e *StatementTunnel) GetIconCategory() string { return "Variables" }

func (e *StatementTunnel) getIcon(data rulesIcon.Data) js.Value {
	data = rulesIcon.DataVerifyElementIcon(data)
	svgIcon := factoryBrowser.NewTagSvg().
		X(rulesIcon.Width.GetInt() / 2).Y(rulesIcon.Height.GetInt() / 2).
		Width(rulesIcon.Width.GetInt()).Height(rulesIcon.Height.GetInt())
	hexPath := utilsDraw.PolygonPath(6, rulesIcon.Width/2, rulesIcon.Width/2, rulesIcon.Width/2, 0)
	hexDraw := factoryBrowser.NewTagSvgPath().
		StrokeWidth(rulesIcon.BorderWidth.GetInt()).Stroke(data.ColorBorder).Fill(data.ColorBackground).D(hexPath)
	labelIcon := factoryBrowser.NewTagSvgText().
		FontFamily(rulesDevice.KDeviceFontFamily).FontWeight("bold").FontSize(rulesIcon.Width.GetInt() / 3).
		Text("⇄").Fill(data.ColorIcon).
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

func (e *StatementTunnel) GetDeviceType() string { return "StatementTunnel" }
func (e *StatementTunnel) GetProperties() map[string]interface{} {
	props := map[string]interface{}{"label": e.label}
	if e.parentID != "" {
		props["tunnelParent"] = e.parentID
		props["tunnelSide"] = e.side
	}
	// Natal phase — the persisted half of the phase-view state (role and
	// hidden are re-derived on load by the Sequence). Português: A
	// metade persistida do estado de visão-por-fase.
	if e.natalCase != "" {
		props["tunnelNatal"] = e.natalCase
	}
	// Phase-removal set — read LIVE from the manager record (the single
	// runtime truth the menu edits); the pending is only the pre-sync
	// restore edge. Comma-joined for the flat property channel.
	// Português: Conjunto de remoções — lido AO VIVO do registro (a
	// verdade única que o menu edita); o pendente só cobre a borda
	// pré-sync do restore. Vírgulas para o canal plano.
	removed := e.pendingRemoved
	if e.wireMgr != nil {
		if live := e.wireMgr.ManualTunnelRemovedCases(e.id); live != nil {
			removed = live
		}
	}
	if len(removed) > 0 {
		props["tunnelRemoved"] = strings.Join(removed, ",")
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
func (e *StatementTunnel) GetComment() string { return e.comment }

// SetComment sets the user comment.
// Português: Define o comentário do usuário.
func (e *StatementTunnel) SetComment(c string) { e.comment = c }

func (e *StatementTunnel) tunnelRect() scene.Rect {
	s := kTunnelSide.GetFloat()
	if e.wireMgr != nil {
		if p, ok := e.wireMgr.ManualTunnelPoint(e.id); ok {
			return scene.Rect{X: p.X - s/2, Y: p.Y - s/2, Width: s, Height: s}
		}
	}
	return scene.Rect{X: e.pendingX - s/2, Y: e.pendingY - s/2, Width: s, Height: s}
}

func (e *StatementTunnel) GetOuterBBox() scene.Rect {
	return e.tunnelRect()
}

func (e *StatementTunnel) legacyOuterBBox() scene.Rect {
	if e.elem == nil {
		return scene.Rect{}
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	return scene.Rect{X: x, Y: y, Width: w, Height: h}
}
func (e *StatementTunnel) GetInnerBBox() *scene.Rect {
	if e.elem == nil {
		return nil
	}
	x, y := e.elem.GetPosition()
	w, h := e.elem.GetSize()
	p := 4.0
	return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}
func (e *StatementTunnel) GetKind() scenegraph.Kind { return scenegraph.KindSimple }
func (e *StatementTunnel) SetSceneNotify(fn func()) { e.sceneNotify = fn }

// ── Help text ─────────────────────────────────────────────────────────────────

func tunnelHelp() string {
	return `# Tunnel — pass-through waypoint

A **named point where data crosses a boundary** — the LabVIEW
sequence-local, as a device. One **In**, one **Out**. The tunnel is a
**chameleon**: it adopts the type of the first wire you connect —
any type, including slices and black-box handles.

At code generation it emits one honest line — tunnel_1 = <source> —
transparent by construction: the program equals the same program
without it. What you gain is a **clickable, nameable, movable place**
on the border: wire phase 0 into **In**, **Out** into phase 1, and the
crossing has an address.

Created from the Sequence pill or body menu (**+ Tunnel**); drag it
onto the container border where the crossing should live.

*Portugues:* Um **ponto nomeado por onde o dado cruza uma fronteira**
— o sequence-local do LabVIEW como device. Emite tunnel_1 = <fonte> —
transparente por construcao; o que voce ganha e um **lugar** clicavel
e arrastavel na borda.
`
}

// SetSceneMgr receives the scene serializer — called by
// scene.Serializer.Register via interface assertion at registration time.
// Português: Recebe o serializer de cena — chamado pelo
// scene.Serializer.Register por assertion no registro.
func (e *StatementTunnel) SetSceneMgr(mgr *scene.Serializer) { e.sceneMgr = mgr }
