// wire/manager.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import (
	"fmt"
	"log"
	"sort"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// =====================================================================
//  Wire ID Generator | Gerador de ID de Fio
// =====================================================================

var wireIDCounter uint64

func generateWireID() string {
	wireIDCounter++
	return fmt.Sprintf("wire_%d", wireIDCounter)
}

// =====================================================================
//  Manager | Gerenciador
// =====================================================================

// Manager is the central coordinator for the wiring system. It owns all
// registered connectors and wires, handles the connection workflow, renders
// wires on the canvas, and provides hit-testing for wire interaction.
//
// [CAMERA-FIX] Wires are stored in world coordinates. Draw() applies the
// camera transform before rendering, and HitTest() converts screen→world
// before testing.
//
// [DENSITY-FIX] All visual sizes (stroke width, corner radius, dash pattern,
// connector dots) are automatically scaled by rulesDensity.GetDensity().
//
// Usage:
//
//	mgr := wire.NewManager()
//	mgr.SetRenderContext(ctx)
//	mgr.SetCameraFunc(func() (float64, float64, float64) {
//	    return cam.OffsetX, cam.OffsetY, cam.Zoom
//	})
//
// Português:
//
//	Manager é o coordenador central do sistema de fiação.
//
//	[CAMERA-FIX] Fios são armazenados em coordenadas mundo. Draw() aplica a
//	transformação da câmera antes de renderizar.
//
//	[DENSITY-FIX] Todos os tamanhos visuais são automaticamente escalados
//	por rulesDensity.GetDensity().
type Manager struct {
	// Connector registry: all known connectors indexed by their key.
	// Português: Registro de conectores indexados por chave.
	connectors map[string]*ConnectorInfo

	// containers maps a container element's ID to a function returning its
	// live world rect (x, y, w, h). Registered by container devices (e.g.
	// StatementCase) so the renderer can place a LabVIEW-style tunnel marker
	// where a wire crosses the container border. The func returns the LIVE
	// rect so the marker tracks the container as it moves or resizes; a
	// zero-size rect (e.g. after the element is gone) is skipped. Lazily
	// initialised by RegisterContainer.
	//
	// Português: Mapeia o ID de um container para uma função que retorna seu
	// retângulo de mundo atual (x, y, w, h). Registrado pelos containers para o
	// renderer desenhar o túnel na borda onde um fio a cruza. Inicializado sob
	// demanda.
	containers           map[string]func() (x, y, w, h float64)
	manualTunnels        map[string]*ManualTunnel
	markerOccluder       func(containerID string, x, y float64) bool
	wireOccluder         func(deviceID string, x, y float64) bool
	onManualTunnelDelete func(id string)

	// All active wires.
	// Português: Todos os fios ativos.
	wires []*Wire

	// Type style overrides. Falls back to DefaultTypeStyles.
	// Português: Sobrescritas de estilo por tipo.
	typeStyles map[string]WireStyle

	// Compatibility matrix override. Falls back to DefaultCompatibility.
	// Português: Sobrescrita da matriz de compatibilidade.
	compatibility map[string][]string

	// Connection mode state.
	// Português: Estado do modo de conexão.
	connectMode   ConnectMode
	connectSource *ConnectorID

	// Visual connect mode: tracks pointer position and caches candidates
	// for rendering draft wire + connector highlights during interactive
	// connect-by-clicking. Updated by workspace via SetDraftEndpoint().
	//
	// Português: Modo visual de conexão: rastreia posição do ponteiro e
	// cacheia candidatos para renderizar wire provisório + highlight dos
	// conectores durante conexão interativa por clique.
	draftEndpoint    Point
	visualCandidates []Candidate

	// Tunnel move mode: while active the user drags a tunnel marker along the
	// container border it sits on. moveTunnelEdge constrains the drag to one
	// axis — vertical on the left/right edges, horizontal on top/bottom — so the
	// tunnel slides along its frame like a LabVIEW tunnel. The dragged position
	// is kept for the session (Wire.Tunnel.Pinned); it is not serialized, so it
	// re-derives on reload, matching the tunnel's frontend-only model.
	//
	// Português: Modo de mover túnel: enquanto ativo, o usuário arrasta o
	// marcador de um túnel ao longo da borda do container. moveTunnelEdge prende
	// o arraste a um eixo — vertical nas bordas esquerda/direita, horizontal nas
	// de topo/base — então o túnel desliza pela moldura como no LabVIEW. A
	// posição arrastada vale na sessão (Wire.Tunnel.Pinned); não é serializada,
	// então re-deriva no reload, fiel ao modelo frontend-only do túnel.
	movingTunnel     bool
	moveTunnelFeeder ConnectorID
	// moveManualID, when non-empty, says the ACTIVE move targets a
	// MANUAL phase-tunnel instead of a wire-derived one (same state
	// machine, same workspace flow — "copie o túnel original", Kemper
	// 2026-07-17). Português: Quando não-vazio, o mover ativo é de um
	// túnel MANUAL — mesma máquina de estados, mesmo fluxo.
	moveManualID        string
	moveTunnelContainer string
	moveTunnelEdge      int

	// Rendering.
	// Português: Renderização.
	ctx   js.Value
	layer WireLayer

	// [CAMERA-FIX] Camera function returns the current camera state.
	// Returns (offsetX, offsetY, zoom). When nil, wires are drawn without
	// camera transformation (backward compatible).
	//
	// Português: Função da câmera retorna o estado atual. Quando nil,
	// fios são desenhados sem transformação (retrocompatível).
	cameraFunc func() (offsetX, offsetY, zoom float64)

	// Hit-testing tolerance in density-independent pixels. Default: 10.
	//
	// Português: Tolerância de hit-testing em pixels independentes de densidade. Padrão: 10.
	hitTolerance float64

	// MarkDirtyFunc is called when the wire manager needs the canvas to re-render.
	// Português: Chamada quando o wire manager precisa re-renderizar.
	MarkDirtyFunc func()

	// OnWireCreated is called after a new wire is successfully created.
	// Português: Chamada após um novo fio ser criado.
	OnWireCreated func(w *Wire)

	// OnWireDeleted is called after a wire is deleted.
	// Português: Chamada após um fio ser excluído.
	OnWireDeleted func(w *Wire)

	// OnWireRetyped is called after an existing wire's resolved DataType
	// changes in place — e.g. an upstream device switched its output type
	// (int -> string) while the wire stayed connected. Consumers that infer
	// from the wire type (StatementCase) re-react through this, the same way
	// they react to OnWireCreated. The wire identity does not change.
	//
	// Português: Chamada quando o DataType resolvido de um fio existente muda
	// no lugar — ex.: um device a montante trocou o tipo de saída (int ->
	// string) com o fio ainda conectado. Consumidores que inferem pelo tipo
	// do fio (StatementCase) re-reagem por aqui, como reagem ao OnWireCreated.
	// A identidade do fio não muda.
	OnWireRetyped func(w *Wire)

	// OnChange is called after any wire is created or deleted.
	// Português: Chamada após qualquer fio ser criado ou excluído.
	OnChange func()

	// OnConnectStart is called when visual connect mode begins (after StartConnect
	// finds candidates). The workspace uses this to create a transparent overlay
	// that intercepts clicks during connect mode.
	//
	// Português: Chamada quando o modo visual de conexão começa. O workspace usa
	// para criar um overlay transparente que intercepta cliques.
	OnConnectStart func()

	// OnConnectEnd is called when visual connect mode ends (FinishConnect,
	// CancelConnect, or no candidates). The workspace uses this to destroy
	// the overlay.
	//
	// Português: Chamada quando o modo visual de conexão termina. O workspace
	// usa para destruir o overlay.
	OnConnectEnd func()

	// OnConnectRejected is called when a connect attempt finds no compatible
	// target (StartConnect returns zero candidates and self-cancels). The
	// workspace uses it to surface feedback (a toast) so the click is not a
	// silent dead-end. The string is a short human-readable reason. Optional.
	//
	// Português: Chamada quando uma tentativa de conexão não acha alvo
	// compatível (zero candidatos). O workspace usa para mostrar feedback
	// (toast), evitando o clique silencioso. Opcional.
	OnConnectRejected func(reason string)

	// hiddenElements holds element IDs that are currently hidden (e.g. the
	// inactive branch of an IfElse container). Their wires are not drawn and
	// their connectors are not offered as connect candidates, so a hidden
	// branch can neither show an orphan wire nor be wired across from the
	// visible branch. Registration stays intact (wires are preserved, just not
	// rendered), so toggling back is a flag flip. Driven by SetElementHidden.
	//
	// Português: IDs de elementos atualmente escondidos (ex: branch inativa de
	// um IfElse). Seus wires não são desenhados e seus conectores não viram
	// candidatos — a branch escondida não mostra wire órfão nem pode ser ligada
	// a partir da branch visível. Registro intacto (wires preservados, só não
	// renderizados). Controlado por SetElementHidden.
	hiddenElements map[string]bool
}

// NewManager creates a new wire Manager with default settings.
//
// Português: Cria um novo wire Manager com configurações padrão.
func NewManager() *Manager {
	return &Manager{
		connectors:     make(map[string]*ConnectorInfo),
		wires:          make([]*Wire, 0),
		typeStyles:     make(map[string]WireStyle),
		compatibility:  nil,
		connectMode:    ConnectModeIdle,
		layer:          WireLayerAbove,
		hitTolerance:   10.0,
		ctx:            js.Undefined(),
		hiddenElements: make(map[string]bool),
	}
}

// =====================================================================
//  Configuration | Configuração
// =====================================================================

// SetRenderContext sets the canvas 2D rendering context used for drawing wires.
//
// Português: Define o contexto de renderização 2D do canvas.
func (m *Manager) SetRenderContext(ctx js.Value) {
	m.ctx = ctx
}

// SetElementHidden marks (or unmarks) an element as hidden in the wire layer.
// A hidden element's wires are skipped by Draw (no orphan wire on screen) and
// its connectors are skipped by StartConnect (no cross-branch candidate). The
// element's connectors and wires stay registered — they are only not rendered
// and not offered — so showing it again is a flag flip. Used by container
// devices (IfElse) when toggling between branches. Triggers a redraw.
//
// Português: Marca/desmarca um elemento como escondido na camada de wires. Os
// wires do elemento escondido não são desenhados (sem wire órfão) e seus
// conectores não viram candidatos (sem conexão cruzando branch). Registro
// intacto — só não renderiza/oferece. Usado por containers (IfElse) ao trocar
// de branch. Dispara redraw.
func (m *Manager) SetElementHidden(elementID string, hidden bool) {
	if m.hiddenElements == nil {
		m.hiddenElements = make(map[string]bool)
	}
	if hidden {
		m.hiddenElements[elementID] = true
	} else {
		delete(m.hiddenElements, elementID)
	}
	m.markDirty()
}

// SetCameraFunc sets a function that returns the current camera state.
// The function must return (offsetX, offsetY, zoom).
// Used to transform world→screen during rendering, and screen→world during hit-testing.
//
// Português:
//
//	SetCameraFunc define uma função que retorna o estado atual da câmera.
//	Deve retornar (offsetX, offsetY, zoom).
//	Usado para transformar mundo→tela na renderização, e tela→mundo no hit-testing.
func (m *Manager) SetCameraFunc(fn func() (offsetX, offsetY, zoom float64)) {
	m.cameraFunc = fn
}

// SetLayer sets whether wires are drawn above or below components.
//
// Português: Define se os fios são desenhados acima ou abaixo dos componentes.
func (m *Manager) SetLayer(layer WireLayer) {
	m.layer = layer
}

// GetLayer returns the current wire layer setting.
//
// Português: Retorna a configuração atual de camada dos fios.
func (m *Manager) GetLayer() WireLayer {
	return m.layer
}

// SetHitTolerance sets the pixel tolerance for wire hit-testing (density-independent).
// Recommended: 6-8 for mouse, 12-15 for touch.
//
// Português: Define a tolerância em pixels para hit-testing (independente de densidade).
func (m *Manager) SetHitTolerance(tolerance float64) {
	m.hitTolerance = tolerance
}

// SetTypeStyle overrides the visual style for a specific data type.
// Values are density-independent base values; scaled automatically at draw time.
//
// Português: Sobrescreve o estilo visual para um tipo de dado específico.
// Valores são base independente de densidade; escalados automaticamente.
func (m *Manager) SetTypeStyle(dataType string, style WireStyle) {
	m.typeStyles[dataType] = style
}

// SetCompatibility overrides the compatibility matrix for a specific output type.
//
// Português: Sobrescreve a matriz de compatibilidade para um tipo de saída específico.
func (m *Manager) SetCompatibility(outputType string, acceptedInputTypes []string) {
	if m.compatibility == nil {
		m.compatibility = make(map[string][]string)
		for k, v := range DefaultCompatibility {
			cp := make([]string, len(v))
			copy(cp, v)
			m.compatibility[k] = cp
		}
	}
	m.compatibility[outputType] = acceptedInputTypes
}

func (m *Manager) getTypeStyle(dataType string) WireStyle {
	// Check per-instance overrides first (exact type name).
	// Português: Verifica sobrescritas por instância primeiro.
	if style, ok := m.typeStyles[dataType]; ok {
		return style
	}

	// Check the default table with the exact type name.
	// Português: Verifica a tabela padrão com o nome exato do tipo.
	if style, ok := DefaultTypeStyles[dataType]; ok {
		return style
	}

	// deriveTypeStyle covers everything the table does not list verbatim:
	// collections ("[]T" → element color, thicker stroke) and complex Go
	// types (pointer / package-qualified → the violet "struct" entry). This
	// is what keeps []byte, []float64 and slices of hardware types from
	// falling through to the error-signalling grey dashed line.
	//
	// Português: deriveTypeStyle cobre o que a tabela não lista literal:
	// coleções ("[]T" → cor do elemento, traço mais grosso) e tipos
	// complexos (ponteiro / qualificado por pacote → entry violeta
	// "struct"). É o que impede []byte, []float64 e slices de tipos de
	// hardware de caírem na linha cinza tracejada de erro.
	if style, ok := deriveTypeStyle(dataType); ok {
		return style
	}

	return DefaultUnknownStyle
}

func (m *Manager) getCompatibility() map[string][]string {
	if m.compatibility != nil {
		return m.compatibility
	}
	return DefaultCompatibility
}

func (m *Manager) markDirty() {
	if m.MarkDirtyFunc != nil {
		m.MarkDirtyFunc()
	}
}

// RefreshElementWires re-resolves the DataType of every wire touching the
// given element against the connectors' current AllowedTypes, updates the
// wire color, and fires OnWireRetyped for any wire whose resolved type
// actually changed. Call this after a device mutates its connector types in
// place (e.g. StatementAdd.SetDataType int -> string) so that an already
// connected consumer — like a StatementCase inferring its selector type —
// re-reacts to the new type instead of keeping the stale type captured at
// connect time.
//
// A wire whose new resolution is empty (the type change made the two ends
// incompatible) is left exactly as-is: this matches the pre-existing
// behavior where an in-place type change never disconnected wires, so this
// method only ever fixes types that became more accurate, never severs.
//
// Português: Re-resolve o DataType de todo fio que toca o elemento dado
// contra os AllowedTypes atuais dos conectores, atualiza a cor do fio e
// dispara OnWireRetyped para os fios cujo tipo resolvido de fato mudou.
// Chame isto depois que um device altera os tipos dos conectores no lugar
// (ex.: StatementAdd.SetDataType int -> string) para que um consumidor já
// conectado — como um StatementCase inferindo o tipo do seletor — re-reaja
// ao novo tipo em vez de manter o tipo obsoleto capturado no connect.
//
// Um fio cuja nova resolução é vazia (a mudança de tipo deixou as duas
// pontas incompatíveis) é deixado exatamente como está: isso espelha o
// comportamento pré-existente em que uma troca de tipo no lugar nunca
// desconectava fios, então este método só corrige tipos que ficaram mais
// precisos, nunca corta.
func (m *Manager) RefreshElementWires(elementID string) {
	compat := m.getCompatibility()
	changed := false
	for _, w := range m.wires {
		if w.From.ElementID != elementID && w.To.ElementID != elementID {
			continue
		}
		from := m.GetConnector(w.From)
		to := m.GetConnector(w.To)
		if from == nil || to == nil {
			continue
		}
		_, _, resolved := findCompatibleTypes(m.effectiveTypes(from), m.effectiveTypes(to), compat)
		if resolved == "" || resolved == w.DataType {
			// Incompatible after the change (leave as-is) or no change.
			// Português: Incompatível após a mudança (deixa como está) ou sem mudança.
			continue
		}
		w.DataType = resolved
		w.Style = m.getTypeStyle(resolved)
		changed = true
		if m.OnWireRetyped != nil {
			m.OnWireRetyped(w)
		}
	}
	if changed {
		m.markDirty()
	}
}

// =====================================================================
//  Connector Registration | Registro de Conectores
// =====================================================================

// RegisterConnector adds a connector to the registry.
//
// Português: Adiciona um conector ao registro.
func (m *Manager) RegisterConnector(info ConnectorInfo) {
	key := info.ID.Key()
	m.connectors[key] = &info
}

// UnregisterConnector removes a connector and all wires connected to it.
//
// Português: Remove um conector e todos os fios conectados a ele.
func (m *Manager) UnregisterConnector(id ConnectorID) {
	key := id.Key()
	delete(m.connectors, key)
	m.removeWiresForConnector(id)
}

// UnregisterElement removes all connectors and wires for a given element ID.
//
// Português: Remove todos os conectores e fios para um dado ID de elemento.
func (m *Manager) UnregisterElement(elementID string) {
	var toDelete []string
	for key, info := range m.connectors {
		if info.ID.ElementID == elementID {
			toDelete = append(toDelete, key)
		}
	}
	for _, key := range toDelete {
		delete(m.connectors, key)
	}

	remaining := make([]*Wire, 0, len(m.wires))
	for _, w := range m.wires {
		if w.From.ElementID != elementID && w.To.ElementID != elementID {
			remaining = append(remaining, w)
		} else if m.OnWireDeleted != nil {
			m.OnWireDeleted(w)
		}
	}
	m.wires = remaining
	delete(m.containers, elementID)
	m.markDirty()
}

// RegisterContainer registers (or replaces) a container's live world-rect
// provider, keyed by the container element's ID. The renderer uses it to place
// LabVIEW-style tunnel markers where wires cross the container border. Pass a
// func returning the current rect so the marker follows the container as it
// moves or resizes.
//
// Português: Registra (ou substitui) o provedor de retângulo de mundo de um
// container, pela ID do elemento. O renderer usa para posicionar os túneis na
// borda. Passe uma func que retorna o retângulo atual para o marcador
// acompanhar o container.
func (m *Manager) RegisterContainer(id string, rectFunc func() (x, y, w, h float64)) {
	if m.containers == nil {
		m.containers = make(map[string]func() (x, y, w, h float64))
	}
	m.containers[id] = rectFunc
	// Recompute existing wires so any that cross this container's border pick
	// up their tunnel immediately. Loaded wires keep their saved route until
	// recomputed, and a wire may have been created before the container
	// registered — without this, recalculateWire never runs with the container
	// present and the tunnel never appears. RecalculateAll marks dirty for us.
	m.RecalculateAll()
}

// UnregisterContainer removes a container's rect provider (on device removal).
// Safe to call for IDs that were never registered.
//
// Português: Remove o provedor de retângulo de um container (na remoção do
// device). Seguro chamar para IDs nunca registrados.
func (m *Manager) UnregisterContainer(id string) {
	// CASCADE (field 2026-07-19, "túnel apagado continua logicamente
	// existindo"): a container's death takes its manual tunnels along —
	// each rides the SAME delete cord the menu uses (record + shell +
	// SceneMgr unregister), so no orphan shell survives to export and
	// warn "not connected" over a corpse. Snapshot the ids first: the
	// cord mutates the map mid-walk. Português: CASCATA — a morte do
	// container leva seus túneis manuais junto, cada um pela MESMA
	// cordinha do menu (registro + casca + unregister), para nenhuma
	// casca órfã sobreviver ao export. Ids fotografados antes: a
	// cordinha muta o mapa no meio da caminhada.
	if len(m.manualTunnels) > 0 {
		var doomed []string
		for tid, t := range m.manualTunnels {
			if t.ContainerID == id {
				doomed = append(doomed, tid)
			}
		}
		for _, tid := range doomed {
			log.Printf("[TUNNEL-DEL] cascade: container %s takes tunnel %s", id, tid)
			m.RequestManualTunnelDelete(tid)
		}
	}

	if m.containers == nil {
		return
	}
	if _, ok := m.containers[id]; ok {
		delete(m.containers, id)
		m.markDirty()
	}
}

// GetConnector returns the connector info for a given ID, or nil if not found.
//
// Português: Retorna as informações do conector ou nil.
func (m *Manager) GetConnector(id ConnectorID) *ConnectorInfo {
	return m.connectors[id.Key()]
}

// =====================================================================
//  Connection Workflow | Fluxo de Conexão
// =====================================================================

// StartConnect begins the connection workflow from a source connector.
// Returns compatible target candidates.
//
// Português: Inicia o fluxo de conexão. Retorna candidatos compatíveis.
// ConnectedPeer returns the ConnectorInfo at the OTHER END of the first
// wire touching (elementID, port), or nil when unwired. Born for the
// Phase B live lookup: a Data · Text opens its properties and asks who
// consumes its output, reading EditorLang/EditorDictJSON off the peer —
// always fresh (the def updates, the maker gets it on the next open),
// nothing serialized. Português: Retorna o ConnectorInfo da OUTRA PONTA
// do primeiro fio tocando (elementID, port), ou nil sem fio. Nascido
// para a consulta viva da Fase B — sempre fresco, nada serializado.
func (m *Manager) ConnectedPeer(elementID, port string) *ConnectorInfo {
	// Bare map reads, like GetConnector above: the Manager lives on the
	// WASM main thread — the house has no wire-layer mutex to hold.
	// Português: Leitura crua como o GetConnector: o Manager vive na
	// thread principal do WASM — a casa não tem mutex na camada de fio.
	self := ConnectorID{ElementID: elementID, PortName: port}
	for _, w := range m.wires {
		var peer ConnectorID
		switch {
		case w.From == self:
			peer = w.To
		case w.To == self:
			peer = w.From
		default:
			continue
		}
		if info, ok := m.connectors[peer.Key()]; ok {
			c := *info
			return &c
		}
		return nil
	}
	return nil
}

func (m *Manager) StartConnect(sourceID ConnectorID) (candidates []Candidate) {
	source := m.GetConnector(sourceID)
	if source == nil {
		return nil
	}
	if source.Locked {
		return nil
	}

	m.connectMode = ConnectModeSelectingTarget
	m.connectSource = &sourceID

	compat := m.getCompatibility()

	for _, target := range m.connectors {
		if target.ID.ElementID == sourceID.ElementID {
			continue
		}
		// A hidden element (e.g. the inactive IfElse branch) is not a valid
		// connect target — skip it so a visible-branch port cannot wire across
		// to a device the maker cannot even see.
		if m.hiddenElements[target.ID.ElementID] {
			continue
		}
		// Phase-view gating: a manual tunnel offers only its role port
		// ("in" on the natal phase, "out" later) and nothing while
		// phase-hidden. Português: Gating por fase — só a porta do papel.
		if m.manualTunnelPortBlocked(target.ID) {
			continue
		}
		if source.IsOutput == target.IsOutput {
			continue
		}
		if target.Locked {
			continue
		}
		if target.MaxConnections > 0 {
			count := m.countWiresOnConnector(target.ID)
			if count >= target.MaxConnections {
				continue
			}
		}
		if m.isConnected(sourceID, target.ID) {
			continue
		}

		var outputTypes, inputTypes []string
		if source.IsOutput {
			outputTypes = m.effectiveTypes(source)
			inputTypes = m.effectiveTypes(target)
		} else {
			outputTypes = m.effectiveTypes(target)
			inputTypes = m.effectiveTypes(source)
		}

		// findCompatibleTypes now returns (outputType, inputType, resolvedType).
		// An empty resolvedType means no compatible pair was found.
		// Português: findCompatibleTypes retorna (outputType, inputType, resolvedType).
		// resolvedType vazio significa que nenhum par compatível foi encontrado.
		_, _, resolvedType := findCompatibleTypes(outputTypes, inputTypes, compat)
		if resolvedType == "" {
			continue
		}

		label := fmt.Sprintf("%s · %s (%s)", target.ID.ElementID, target.ID.PortName, resolvedType)
		candidates = append(candidates, Candidate{
			Connector:    *target,
			ResolvedType: resolvedType,
			Label:        label,
		})
	}

	if len(candidates) == 0 {
		m.CancelConnect()
		// Surface why the click did nothing instead of failing silently.
		if m.OnConnectRejected != nil {
			m.OnConnectRejected(fmt.Sprintf("No compatible target for %s.%s",
				sourceID.ElementID, sourceID.PortName))
		}
	} else {
		m.visualCandidates = candidates
		// Render the candidate highlights immediately on this click. OnConnectStart
		// only flips the cursor to a crosshair; it does not schedule a redraw, so
		// without this the glow rings (and the draft wire) would not appear until
		// the next markDirty — which the user triggers by moving the pointer,
		// making the click feel unresponsive. Scheduling the redraw here lands the
		// visual feedback on the click itself. Applies to every connect entry
		// point (hex menu, tunnel, …), not just one.
		m.markDirty()
		if m.OnConnectStart != nil {
			m.OnConnectStart()
		}
	}
	return
}

// FinishConnect completes the connection to the target connector.
//
// Português: Completa a conexão para o conector de destino.
func (m *Manager) FinishConnect(targetID ConnectorID) (w *Wire, err error) {
	if m.connectMode != ConnectModeSelectingTarget || m.connectSource == nil {
		err = fmt.Errorf("wire: no connection in progress")
		return
	}

	sourceID := *m.connectSource
	m.connectMode = ConnectModeIdle
	m.connectSource = nil
	m.visualCandidates = nil
	m.draftEndpoint = Point{}

	if m.OnConnectEnd != nil {
		m.OnConnectEnd()
	}

	source := m.GetConnector(sourceID)
	target := m.GetConnector(targetID)
	if source == nil || target == nil {
		err = fmt.Errorf("wire: connector not found")
		return
	}

	var fromID, toID ConnectorID
	var outputTypes, inputTypes []string

	if source.IsOutput {
		fromID = sourceID
		toID = targetID
		outputTypes = m.effectiveTypes(source)
		inputTypes = m.effectiveTypes(target)
	} else {
		fromID = targetID
		toID = sourceID
		outputTypes = m.effectiveTypes(target)
		inputTypes = m.effectiveTypes(source)
	}

	compat := m.getCompatibility()
	_, _, resolvedType := findCompatibleTypes(outputTypes, inputTypes, compat)
	if resolvedType == "" {
		err = fmt.Errorf("wire: incompatible types")
		return
	}

	fromConn := m.GetConnector(fromID)
	toConn := m.GetConnector(toID)
	fromX, fromY := fromConn.PositionFunc()
	toX, toY := toConn.PositionFunc()

	// [DENSITY-FIX] Scale stub length by density.
	// Português: Escala comprimento do stub por densidade.
	d := rulesDensity.GetDensity()
	stubLen := defaultStubLength * d

	w = &Wire{
		ID:        generateWireID(),
		From:      fromID,
		To:        toID,
		DataType:  resolvedType,
		Style:     m.getTypeStyle(resolvedType),
		Waypoints: ComputeManhattanRouteWithStub(Point{fromX, fromY}, Point{toX, toY}, stubLen),
	}
	// Mark + dash the wire when it carries a function reference (the wire-ƒ).
	applyCallbackWireStyle(w, fromConn, toConn)

	// Route through a container-border tunnel if this wire crosses one (sets
	// w.Tunnel and the two-segment path). The struct above seeded a straight
	// route; recalculateWire refines it now that the wire exists — this is what
	// makes the tunnel appear at creation, not only after a later move.
	m.recalculateWire(w)

	m.wires = append(m.wires, w)
	m.markDirty()

	if m.OnWireCreated != nil {
		m.OnWireCreated(w)
	}
	if m.OnChange != nil {
		m.OnChange()
	}
	return
}

// applyCallbackWireStyle marks a wire as a callback (wire-ƒ) connection and
// gives it a dashed stroke when either endpoint is a callback connector — the
// ƒ device's `callback` output or a callback input such as setDisplay.writer.
// The CallbackType marker lets the renderer distinguish function-reference
// wires from data wires (it does NOT affect connection compatibility, which
// the exact-match type rule in resolveCompatibleType already enforces). A
// no-op for ordinary wires.
//
// Português: Marca o fio como conexão de callback (wire-ƒ) e o deixa tracejado
// quando uma das pontas é conector de callback. Não altera compatibilidade
// (a regra de igualdade exata já cuida disso). No-op para fios comuns.
func applyCallbackWireStyle(w *Wire, fromConn, toConn *ConnectorInfo) {
	if w == nil {
		return
	}
	cb := ""
	if fromConn != nil && fromConn.CallbackType != "" {
		cb = fromConn.CallbackType
	} else if toConn != nil && toConn.CallbackType != "" {
		cb = toConn.CallbackType
	}
	if cb == "" {
		return
	}
	w.CallbackType = cb
	// Dashed stroke distinguishes the function-reference wire from a normal
	// data wire (mirrors the ƒ semantics on the stage).
	w.Style.DashPattern = []float64{6, 4}
}

// CancelConnect aborts the current connection workflow.
//
// Português: Cancela o fluxo de conexão atual.
func (m *Manager) CancelConnect() {
	wasConnecting := m.connectMode == ConnectModeSelectingTarget
	m.connectMode = ConnectModeIdle
	m.connectSource = nil
	m.visualCandidates = nil
	m.draftEndpoint = Point{}
	m.markDirty()

	if wasConnecting && m.OnConnectEnd != nil {
		m.OnConnectEnd()
	}
}

// ConnectDirect creates a wire between two connectors programmatically,
// bypassing the interactive StartConnect/FinishConnect workflow.
// Used by the stage import system to reconstruct saved wires.
//
// The caller must ensure both connectors are already registered.
// Type compatibility is checked — incompatible types return an error.
//
// Português: Cria um fio entre dois conectores programaticamente,
// sem passar pelo fluxo interativo. Usado pelo import de stage.
// ConnectionsAmong lists every wire whose BOTH endpoints belong to the
// given id set — the deep-copy path replays these onto the clone's
// remapped connectors (a container's internal logic IS its wires,
// Kemper 2026-07-19). Português: Todo fio com AS DUAS pontas no
// conjunto — a cópia profunda os replica nos conectores remapeados (a
// lógica interna de um container SÃO seus fios).
func (m *Manager) ConnectionsAmong(ids map[string]bool) [][2]ConnectorID {
	var out [][2]ConnectorID
	for _, w := range m.wires {
		if w == nil {
			continue
		}
		if ids[w.From.ElementID] && ids[w.To.ElementID] {
			out = append(out, [2]ConnectorID{w.From, w.To})
		}
	}
	return out
}

func (m *Manager) ConnectDirect(fromID, toID ConnectorID) (*Wire, error) {
	fromConn := m.GetConnector(fromID)
	toConn := m.GetConnector(toID)
	if fromConn == nil {
		return nil, fmt.Errorf("wire: source connector %v not found", fromID)
	}
	if toConn == nil {
		return nil, fmt.Errorf("wire: target connector %v not found", toID)
	}

	// Determine output/input direction.
	var outputTypes, inputTypes []string
	if fromConn.IsOutput {
		outputTypes = m.effectiveTypes(fromConn)
		inputTypes = m.effectiveTypes(toConn)
	} else {
		outputTypes = m.effectiveTypes(toConn)
		inputTypes = m.effectiveTypes(fromConn)
	}

	compat := m.getCompatibility()
	_, _, resolvedType := findCompatibleTypes(outputTypes, inputTypes, compat)
	if resolvedType == "" {
		return nil, fmt.Errorf("wire: incompatible types between %v and %v", fromID, toID)
	}

	fromX, fromY := fromConn.PositionFunc()
	toX, toY := toConn.PositionFunc()

	d := rulesDensity.GetDensity()
	stubLen := defaultStubLength * d

	w := &Wire{
		ID:        generateWireID(),
		From:      fromID,
		To:        toID,
		DataType:  resolvedType,
		Style:     m.getTypeStyle(resolvedType),
		Waypoints: ComputeManhattanRouteWithStub(Point{fromX, fromY}, Point{toX, toY}, stubLen),
	}
	// Mark + dash the wire when it carries a function reference (the wire-ƒ).
	applyCallbackWireStyle(w, fromConn, toConn)

	// Route through a container-border tunnel if this wire crosses one (sets
	// w.Tunnel and the two-segment path). The struct above seeded a straight
	// route; recalculateWire refines it now that the wire exists — this is what
	// makes the tunnel appear at creation (e.g. on scene load), not only after
	// a later move.
	m.recalculateWire(w)

	m.wires = append(m.wires, w)
	m.markDirty()

	if m.OnWireCreated != nil {
		m.OnWireCreated(w)
	}

	return w, nil
}

// IsConnecting returns true if a connection workflow is in progress.
//
// Português: Retorna true se um fluxo de conexão está em andamento.
func (m *Manager) IsConnecting() bool {
	return m.connectMode == ConnectModeSelectingTarget
}

// GetConnectSource returns the source connector of the active connection,
// or nil if not connecting.
//
// Português: Retorna o conector de origem da conexão ativa, ou nil se não
// estiver conectando.
func (m *Manager) GetConnectSource() *ConnectorID {
	return m.connectSource
}

// GetVisualCandidates returns the cached compatible targets from the last
// StartConnect call. Empty when not in connect mode.
//
// Português: Retorna os alvos compatíveis cacheados do último StartConnect.
// Vazio quando não está em modo de conexão.
func (m *Manager) GetVisualCandidates() []Candidate {
	return m.visualCandidates
}

// SetDraftEndpoint updates the pointer position in world coordinates for
// rendering the draft wire preview during visual connect mode.
//
// Português: Atualiza a posição do ponteiro em coordenadas mundo para
// renderizar o fio provisório durante o modo visual de conexão.
func (m *Manager) SetDraftEndpoint(worldX, worldY float64) {
	m.draftEndpoint = Point{worldX, worldY}
	m.markDirty()
}

// HitTestConnector checks if a point in world coordinates is close enough
// to any candidate connector to be considered a "hit". Returns the connector
// ID if hit, nil otherwise.
//
// [DENSITY-FIX] Tolerance is scaled by density automatically.
//
// Português: Verifica se um ponto em coordenadas mundo está próximo o suficiente
// de qualquer conector candidato. Retorna o ID do conector se atingido, nil caso
// contrário.
// ConnectorNear reports whether ANY registered connector sits within pin
// grab distance of the point — mode-agnostic, unlike HitTestConnector
// (which only answers while picking a connect TARGET). Born for the click
// interceptor's pin-over-corridor priority: the very FIRST click of a
// connection happens in idle mode, exactly when HitTestConnector is mute.
// Português: Diz se QUALQUER conector registrado está a distância de
// agarre do ponto — agnóstico a modo, ao contrário do HitTestConnector
// (que só responde escolhendo ALVO). Nasceu para a prioridade
// pino-sobre-corredor do interceptador: o PRIMEIRO clique de uma conexão
// acontece em modo ocioso, exatamente quando o HitTestConnector é mudo.
func (m *Manager) ConnectorNear(worldX, worldY float64) bool {
	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * 1.5
	for _, c := range m.connectors {
		if c == nil || c.PositionFunc == nil {
			continue
		}
		cx, cy := c.PositionFunc()
		dx := worldX - cx
		dy := worldY - cy
		if dx*dx+dy*dy <= tolerance*tolerance {
			return true
		}
	}
	return false
}

func (m *Manager) HitTestConnector(worldX, worldY float64) *ConnectorID {
	if m.connectMode != ConnectModeSelectingTarget {
		return nil
	}

	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * 1.5 // slightly larger for connectors

	for _, c := range m.visualCandidates {
		cx, cy := c.Connector.PositionFunc()
		dx := worldX - cx
		dy := worldY - cy
		if dx*dx+dy*dy <= tolerance*tolerance {
			id := c.Connector.ID
			return &id
		}
	}

	// A tunnel marker stands in for the source that feeds it: while picking a
	// connect target, a click on the tunnel resolves to that feeder, so a
	// device input can be wired straight to the tunnel instead of having to
	// reach the off-container source connector. We only resolve it when the
	// feeder is itself a candidate for the current source — that reuses the
	// full type/direction/duplicate filtering done in StartConnect and rejects,
	// for free, a tunnel whose feeder is incompatible or already connected.
	// Real candidate dots above take priority, so an overlapping dot still wins.
	//
	// Português: O marcador do túnel representa a fonte que o alimenta: ao
	// escolher um alvo, clicar no túnel resolve para esse feeder, permitindo
	// ligar a entrada de um device direto no túnel em vez de alcançar o conector
	// da fonte fora do container. Só resolve quando o feeder já é um candidato
	// para a origem atual — reaproveita toda a filtragem de tipo/direção/
	// duplicata feita no StartConnect e rejeita de graça um túnel cujo feeder
	// seja incompatível ou já conectado. Os pontos de candidato reais acima têm
	// prioridade, então um ponto sobreposto continua ganhando.
	if feeder, _, ok := m.TunnelAt(worldX, worldY); ok {
		for _, c := range m.visualCandidates {
			if c.Connector.ID == feeder {
				id := feeder
				return &id
			}
		}
	}
	return nil
}

// HitTestAnyConnector reports the connector under the pointer regardless of
// connect mode — the HOVER path (connector tooltip). Unlike HitTestConnector,
// which only runs while picking a wire target and only scans the filtered
// candidate set, this scans EVERY registered connector of every VISIBLE
// element and returns the full ConnectorInfo, so the caller can present the
// port's label and data type. Same tolerance math as the click path, so what
// highlights on hover is exactly what would accept the click.
//
// Português: Informa o conector sob o ponteiro independente do modo de
// conexão — o caminho de HOVER (tooltip de conector). Diferente do
// HitTestConnector, que só roda escolhendo alvo de fio e só varre os
// candidatos filtrados, este varre TODO conector registrado de elemento
// VISÍVEL e retorna o ConnectorInfo completo, para o chamador apresentar
// label e tipo da porta. Mesma tolerância do caminho de clique: o que
// destaca no hover é exatamente o que aceitaria o clique.
func (m *Manager) HitTestAnyConnector(worldX, worldY float64) *ConnectorInfo {
	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * 1.5

	for _, c := range m.connectors {
		if c.PositionFunc == nil {
			continue
		}
		if m.hiddenElements[c.ID.ElementID] {
			continue
		}
		// Phase-view gating — same rule as the candidate scan above.
		// Português: Mesmo gating por fase do scan de candidatos.
		if m.manualTunnelPortBlocked(c.ID) {
			continue
		}
		cx, cy := c.PositionFunc()
		dx := worldX - cx
		dy := worldY - cy
		if dx*dx+dy*dy <= tolerance*tolerance {
			return c
		}
	}
	return nil
}

// TunnelAt returns the feeder source connector and container ID of a tunnel
// whose marker is at (worldX, worldY) within tolerance, with ok=true. Only a
// tunnel whose feed is visible (source not hidden) is reported. It backs the
// tunnel context menu: Connect starts a wire from feeder, Delete removes every
// wire through the tunnel (see DeleteTunnelWires).
//
// The shared tunnel point is the same for every wire from a given source (see
// wireTunnelPoint), so the first matching wire yields the right feeder.
//
// Português: Retorna o conector de origem (feeder) e o ID do container de um
// túnel cujo marcador está em (worldX, worldY) dentro da tolerância (ok=true).
// Só túneis com feed visível. Sustenta o menu de contexto do túnel: Connect
// inicia um fio a partir de feeder, Delete remove todos os fios do túnel.
func (m *Manager) TunnelAt(worldX, worldY float64) (feeder ConnectorID, containerID string, ok bool) {
	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * 1.5
	for _, w := range m.wires {
		if w.Tunnel == nil {
			continue
		}
		// Only a tunnel whose feed is drawn (its source is visible) is a live
		// click target.
		if m.hiddenElements[w.From.ElementID] {
			continue
		}
		dx := worldX - w.Tunnel.Point.X
		dy := worldY - w.Tunnel.Point.Y
		if dx*dx+dy*dy <= tolerance*tolerance {
			return w.From, w.Tunnel.ContainerID, true
		}
	}
	return ConnectorID{}, "", false
}

// =====================================================================
//  Tunnel move | Mover Túnel
// =====================================================================

// Border-edge identifiers, used to lock a tunnel drag to the axis of the edge
// the tunnel sits on.
//
// Português: Identificadores de borda, usados para travar o arraste do túnel ao
// eixo da borda onde ele está.
const (
	edgeLeft = iota
	edgeRight
	edgeTop
	edgeBottom
)

// tunnelEdgeOf returns the border edge of the rect a point lies on (the nearest
// one): edgeLeft/Right/Top/Bottom.
//
// Português: Retorna a borda do rect onde o ponto está (a mais próxima).
func tunnelEdgeOf(rx, ry, rw, rh, px, py float64) int {
	abs := func(v float64) float64 {
		if v < 0 {
			return -v
		}
		return v
	}
	best := abs(px - rx)
	edge := edgeLeft
	if d := abs(px - (rx + rw)); d < best {
		best = d
		edge = edgeRight
	}
	if d := abs(py - ry); d < best {
		best = d
		edge = edgeTop
	}
	if d := abs(py - (ry + rh)); d < best {
		edge = edgeBottom
	}
	return edge
}

// TunnelPointsFor returns the automatic tunnel points currently minted
// on the given container's border — one per feeder group. The Sequence
// uses it as part of the no-overlap judge for MANUAL phase-tunnels
// (spec #6, 2026-07-17: occlusion hides information; any tunnel counts,
// automatic or manual).
//
// THE 2026-07-18 DRAGON LIVED HERE: Wire.Tunnel is a POINTER, nil on
// every wire that does not cross a container border, and this loop
// dereferenced it bare. The judge only meets wires once the maker has
// both tunnels AND wires in the scene — so the panic slept through
// every early test (empty m.wires) and woke exactly when the field got
// real: creating a tunnel with wired siblings killed the wasm runtime,
// and the whole IDE froze with it ("a criação de túnel continua
// travando"). The createTunnel panic shield caught it, the checkpoints
// bisected it to this window, and every sibling loop in this file
// already carried the guard — this one was born without it.
//
// Português: Pontos de túnel AUTOMÁTICOS na borda do container — o
// Sequence os usa no juiz anti-sobreposição. O DRAGÃO DE 2026-07-18
// MORAVA AQUI: Wire.Tunnel é PONTEIRO, nil em todo fio sem travessia de
// borda, e este loop o dereferenciava sem guarda. O juiz só encontra
// fios quando o maker já tem túneis E fios — o panic dormiu nos testes
// iniciais (m.wires vazio) e acordou quando o campo ficou real: criar
// túnel com irmãos ligados matava o runtime wasm e congelava a IDE
// inteira. O escudo o capturou, os checkpoints o bissectaram até esta
// janela, e todo loop irmão neste arquivo já tinha a guarda — este
// nasceu sem.
func (m *Manager) TunnelPointsFor(containerID string) []Point {
	var pts []Point
	for _, w := range m.wires {
		if w.Tunnel != nil && w.Tunnel.ContainerID == containerID {
			pts = append(pts, w.Tunnel.Point)
		}
	}
	return pts
}

// ManualTunnel is a maker-created phase-tunnel: the SAME on-canvas
// object as the automatic wire-derived marker (square on a container
// border, slides along its locked edge, Connect/Move/Delete menu), but
// with its own identity — it exists before any wire, carries the
// StatementTunnel device id, and lives in the internal (violet)
// palette. Born red (Fresh) until the first interaction.
//
// Português: Túnel de fase criado pelo maker — o MESMO objeto de tela
// do marcador automático, com identidade própria: existe antes de
// qualquer fio, carrega o id do device e usa a paleta interna
// (violeta). Nasce vermelho (Fresh) até a primeira interação.
type ManualTunnel struct {
	ID          string
	ContainerID string
	Edge        int
	Point       Point
	Fresh       bool

	// Phase-view state (Kemper spec 2026-07-18): ONE tunnel, presentation
	// decided by the phase being viewed. NatalCase is the Sequence phase
	// (case id) the tunnel was born in — the only PERSISTED field of the
	// three (the shell serializes it as "tunnelNatal"). Role and
	// PhaseHidden are DERIVED and pushed by the owning Sequence on every
	// visibility pass: viewing the natal phase → Role "in" (square on the
	// RIGHT border, pin pointing inward-left); any later phase → Role
	// "out" (square on the LEFT border, pin pointing inward-right); any
	// earlier phase → PhaseHidden (the tunnel does not exist yet there).
	// The wire package stores this as dumb data — it never reaches into
	// device types to compute it (no import cycle).
	//
	// Português: Estado de visão-por-fase (spec Kemper 2026-07-18): UM
	// túnel, apresentação decidida pela fase em exibição. NatalCase é a
	// fase de nascimento — único campo PERSISTIDO (a casca serializa como
	// "tunnelNatal"). Role e PhaseHidden são DERIVADOS e empurrados pelo
	// Sequence dono a cada passe de visibilidade: fase natal → "in"
	// (quadrado na borda DIREITA, pino para dentro-esquerda); fase
	// posterior → "out" (borda ESQUERDA, pino para dentro-direita); fase
	// anterior → PhaseHidden (o túnel ainda não existe lá). O pacote wire
	// guarda como dado burro — nunca alcança devices para computar.
	NatalCase   string
	Role        string
	PhaseHidden bool

	// Label is the maker's name for this tunnel ("sensor temp"),
	// painted by the renderer on the square's OUTSIDE face — but only
	// when it differs from the device id: the default "tunnel_N" would
	// be stage noise, so renaming is what makes the name appear
	// (declared 2026-07-19; doubles as Fatia B groundwork — a function
	// tunnel's label becomes its parameter name). Português: O nome
	// dado pelo maker, pintado na face EXTERNA do quadrado — só quando
	// difere do id: o "tunnel_N" padrão seria ruído; renomear faz o
	// nome aparecer. Também é a base da Fatia B (rótulo → nome de
	// parâmetro na função).
	Label string

	// Comment is the maker's free-text note, shown with the name in the
	// hover tip. Português: Nota livre do maker, exibida com o nome no
	// tooltip de hover.
	Comment string

	// RemovedCases holds the phases (case ids) the maker HID this
	// tunnel's exit from (Kemper 2026-07-18: "assim um túnel pode ser
	// ocultado"). Persisted by the shell as "tunnelRemoved". The natal
	// phase is never in this set — it is the tunnel's HOME, the handle
	// you restore from; removing it too would make the tunnel
	// unreachable. Português: Fases (ids) das quais o maker OCULTOU a
	// saída deste túnel. Persistido pela casca como "tunnelRemoved". A
	// fase natal nunca entra no conjunto — é o LAR do túnel, a alça de
	// onde se restaura.
	RemovedCases map[string]bool
}

// manualEdgeOf maps the Sequence's side vocabulary to the edge consts.
func manualEdgeOf(side string) int {
	switch side {
	case "top":
		return edgeTop
	case "bottom":
		return edgeBottom
	case "right":
		return edgeRight
	default:
		return edgeLeft
	}
}

// AddManualTunnel registers (or re-seats) a manual phase-tunnel. A
// RE-SEAT (record already present) preserves the phase-view state
// (NatalCase/Role/PhaseHidden): position syncs must never clobber what
// the Sequence or the restore path already stamped — order-proofing, the
// same discipline as the shell's pendingX/Y.
// Português: Registra (ou re-assenta) um túnel manual. RE-ASSENTAR
// preserva o estado de visão-por-fase: sincronizações de posição nunca
// podem sobrescrever o que o Sequence ou o restore já carimbaram.
func (m *Manager) AddManualTunnel(id, containerID, side string, p Point, fresh bool) {
	if m.manualTunnels == nil {
		m.manualTunnels = map[string]*ManualTunnel{}
	}
	if prev, ok := m.manualTunnels[id]; ok {
		prev.ContainerID = containerID
		prev.Edge = manualEdgeOf(side)
		prev.Point = p
		if fresh {
			prev.Fresh = true
		}
		m.markDirty()
		return
	}
	m.manualTunnels[id] = &ManualTunnel{
		ID: id, ContainerID: containerID,
		Edge: manualEdgeOf(side), Point: p, Fresh: fresh,
	}
	m.markDirty()
}

// SetManualTunnelNatal stamps the tunnel's birth phase. Called by the
// Sequence right after creation and by the shell's restore sync.
// Português: Carimba a fase natal — na criação e no restore.
func (m *Manager) SetManualTunnelNatal(id, natalCase string) {
	if t, ok := m.manualTunnels[id]; ok && natalCase != "" {
		t.NatalCase = natalCase
	}
}

// ManualTunnelNatal reads the birth phase ("" when unknown — old scenes).
// Português: Lê a fase natal ("" quando desconhecida — cenas antigas).
// ManualTunnelSide returns the tunnel's current rail side ("left" or
// "right"). For Function tunnels the side is IDENTITY (F2: left =
// parameter, right = return); for Sequence tunnels it is the ephemeral
// view pose. Português: O lado atual do trilho — identidade nos túneis
// de Function (F2), pose efêmera nos de Sequence.
// ManualTunnelLabel returns the maker's name for the tunnel — the
// deep-copy path replays it onto the clone. Português: O nome dado
// pelo maker — a cópia profunda o replica no clone.
func (m *Manager) ManualTunnelLabel(id string) string {
	if t, ok := m.manualTunnels[id]; ok {
		return t.Label
	}
	return ""
}

// ManualTunnelComment returns the maker's note for the tunnel — the
// hover tip includes it on Function pins so BOTH faces (and both hover
// stages) show it. Português: A nota do maker — o tip a inclui nos
// pinos de Function, para as duas faces mostrarem.
func (m *Manager) ManualTunnelComment(id string) string {
	if t, ok := m.manualTunnels[id]; ok {
		return t.Comment
	}
	return ""
}

func (m *Manager) ManualTunnelSide(id string) string {
	t, ok := m.manualTunnels[id]
	if !ok {
		return ""
	}
	switch t.Edge {
	case edgeRight:
		return "right"
	case edgeTop:
		return "top"
	case edgeBottom:
		return "bottom"
	default:
		return "left"
	}
}

func (m *Manager) ManualTunnelNatal(id string) string {
	if t, ok := m.manualTunnels[id]; ok {
		return t.NatalCase
	}
	return ""
}

// ManualTunnelRole reads the current derived role ("in"/"out"; "" before
// the first view push). Português: Papel derivado atual.
func (m *Manager) ManualTunnelRole(id string) string {
	if t, ok := m.manualTunnels[id]; ok {
		return t.Role
	}
	return ""
}

// ManualTunnelIDsFor lists the manual tunnels pinned to a container —
// the Sequence enumerates through HERE, not through its own bookkeeping,
// so restored tunnels (which never pass through createTunnel) are
// covered too. Português: Lista os túneis do container — o Sequence
// enumera por AQUI, cobrindo também os restaurados.
func (m *Manager) ManualTunnelIDsFor(containerID string) []string {
	var ids []string
	for id, t := range m.manualTunnels {
		if t.ContainerID == containerID {
			ids = append(ids, id)
		}
	}
	return ids
}

// SetManualTunnelView pushes one tunnel's phase presentation: side + X
// re-seated by the owner (Y is the maker's coordinate, preserved by the
// caller), role and phase-hidden flags. This is the write half of the
// Sequence→manager push; the renderer and the connect gating read it.
// Português: Empurra a apresentação de fase de um túnel: lado + X
// re-assentados pelo dono (Y é do maker), papel e escondido-por-fase.
// manualTunnelPhaseHidden reports whether id is a manual tunnel whose
// view is phase-hidden — the wire draw gate treats such an endpoint as
// hidden. Português: Informa se id é túnel manual com a vista
// fase-oculta — o portão de desenho trata a ponta como oculta.
func (m *Manager) manualTunnelPhaseHidden(id string) bool {
	if t, ok := m.manualTunnels[id]; ok && t != nil {
		return t.PhaseHidden
	}
	return false
}

func (m *Manager) SetManualTunnelView(id, side string, x, y float64, role string, phaseHidden bool) {
	t, ok := m.manualTunnels[id]
	if !ok {
		return
	}
	// A pose change MOVES the wire anchor — the attached wires' cached
	// waypoints must reroute to the new face, or they keep pointing at
	// the OLD one (field 2026-07-21: "o wire se comporta como se o
	// túnel estivesse do outro lado", vanishing wires on phase
	// navigation, the post-restore chaos — one cause). Same mechanism
	// element moves already use. Português: Mudança de pose MOVE a
	// âncora — os waypoints em cache dos fios anexados precisam
	// rerotear para a nova face, senão seguem apontando a ANTIGA (a
	// causa única dos fios "do outro lado", dos sumiços ao navegar e
	// do caos pós-restore). Mesmo mecanismo do move de elementos.
	moved := t.Point.X != x || t.Point.Y != y || t.PhaseHidden != phaseHidden
	t.Edge = manualEdgeOf(side)
	t.Point = Point{X: x, Y: y}
	t.Role = role
	t.PhaseHidden = phaseHidden
	if moved {
		m.RecalculateForElement(id)
	}
	m.markDirty()
}

// AddManualTunnelRemovedCases hides the tunnel's exit in the given
// phases (case ids) — "remove dessa fase" adds one, "remove das
// próximas fases" adds the tail after the phase on screen. The natal
// phase must never be passed here (the Sequence guards it): it is the
// handle the maker restores from. Português: Oculta a saída do túnel
// nas fases dadas. A natal nunca chega aqui (o Sequence guarda): é a
// alça de restauração.
func (m *Manager) AddManualTunnelRemovedCases(id string, cases ...string) {
	t, ok := m.manualTunnels[id]
	if !ok || len(cases) == 0 {
		return
	}
	if t.RemovedCases == nil {
		t.RemovedCases = map[string]bool{}
	}
	for _, c := range cases {
		if c != "" {
			t.RemovedCases[c] = true
		}
	}
	m.markDirty()
}

// SetManualTunnelRemovedCases replaces the whole removal set — the
// restore path ("retorna para as próximas fases retorna todos") passes
// nil/empty to clear everything, and the shell's scene restore passes
// the persisted list. Português: Substitui o conjunto inteiro — o
// restore passa vazio ("retorna todos"), e a casca passa a lista
// persistida ao recarregar a cena.
func (m *Manager) SetManualTunnelRemovedCases(id string, cases []string) {
	t, ok := m.manualTunnels[id]
	if !ok {
		return
	}
	if len(cases) == 0 {
		t.RemovedCases = nil
		m.markDirty()
		return
	}
	set := make(map[string]bool, len(cases))
	for _, c := range cases {
		if c != "" {
			set[c] = true
		}
	}
	t.RemovedCases = set
	m.markDirty()
}

// ManualTunnelRemovedCases lists the removal set, SORTED so the shell's
// serialization is deterministic across saves. Português: Lista o
// conjunto ORDENADO — serialização determinística.
func (m *Manager) ManualTunnelRemovedCases(id string) []string {
	t, ok := m.manualTunnels[id]
	if !ok || len(t.RemovedCases) == 0 {
		return nil
	}
	out := make([]string, 0, len(t.RemovedCases))
	for c := range t.RemovedCases {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// ManualTunnelRemovedHas reports whether the tunnel's exit is hidden in
// the given phase — refreshTunnelViews consults this per phase flip.
// Português: Diz se a saída está oculta na fase dada.
func (m *Manager) ManualTunnelRemovedHas(id, caseID string) bool {
	t, ok := m.manualTunnels[id]
	return ok && t.RemovedCases[caseID]
}

// ManualTunnelContainer returns the owning container id — the menu uses
// it to reach the Sequence that owns the phase semantics. Português:
// Devolve o container dono — o menu chega ao Sequence por aqui.
func (m *Manager) ManualTunnelContainer(id string) string {
	if t, ok := m.manualTunnels[id]; ok {
		return t.ContainerID
	}
	return ""
}

// SetMarkerOccluder injects the z-order occlusion predicate the
// workspace builds over the ZIndexRegistry: a HIGHER-Z visible device
// covering a tunnel's point hides its border furniture — and by the
// phantom-pin law the same verdict gates every hit path (what does not
// paint must not click). nil = no occlusion (tests, headless).
// Português: Injeta o predicado de oclusão por z-order: device VISÍVEL
// de z maior cobrindo o ponto esconde a mobília do túnel — e pela lei
// do pino fantasma o mesmo veredito trava todo hit. nil = sem oclusão.
func (m *Manager) SetMarkerOccluder(fn func(containerID string, x, y float64) bool) {
	m.markerOccluder = fn
}

// SetWireOccluder injects the endpoint-level occlusion predicate: given
// a DEVICE id and a world point, is the device's owning container
// buried under a higher-z visible sibling there? A wire hides when
// EITHER endpoint answers yes — a buried Sequence takes its outgoing
// wires down with it (field 2026-07-19: "o const, o wire ... ficam
// visíveis dentro do loop"). The workspace resolves the container
// (manual-tunnel record first, scenegraph parent second). Português:
// Injeta o predicado por ponta: dado um DEVICE e um ponto, o container
// dono dele está enterrado ali? O fio some quando QUALQUER ponta diz
// sim — Sequence enterrado leva seus fios junto. O workspace resolve o
// container (registro do túnel primeiro, pai do scenegraph depois).
func (m *Manager) SetWireOccluder(fn func(deviceID string, x, y float64) bool) {
	m.wireOccluder = fn
}

// wireEndpointOccluded applies the wire predicate to one endpoint.
// Português: Aplica o predicado do fio a uma ponta.
func (m *Manager) wireEndpointOccluded(deviceID string, p Point) bool {
	return m.wireOccluder != nil && m.wireOccluder(deviceID, p.X, p.Y)
}

// tunnelOccluded applies the predicate to a tunnel's point.
// Português: Aplica o predicado ao ponto do túnel.
func (m *Manager) tunnelOccluded(t *ManualTunnel) bool {
	return m.markerOccluder != nil && m.markerOccluder(t.ContainerID, t.Point.X, t.Point.Y)
}

// SetManualTunnelComment — the live setter twin of SetManualTunnelLabel.
// Português: O gêmeo vivo do SetManualTunnelLabel para o comentário.
func (m *Manager) SetManualTunnelComment(id, comment string) {
	if t, ok := m.manualTunnels[id]; ok && t.Comment != comment {
		t.Comment = comment
	}
}

// ManualTunnelHoverInfo returns the name and comment of the visible,
// unoccluded manual tunnel under the pointer — the hover machine's
// tunnel stage. Português: Nome e comentário do túnel visível e não
// ocluso sob o ponteiro — o estágio de túnel da máquina de hover.
func (m *Manager) ManualTunnelHoverInfo(worldX, worldY float64) (label, comment string, ok bool) {
	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * 1.5
	for _, t := range m.manualTunnels {
		if t.PhaseHidden || m.tunnelOccluded(t) {
			continue
		}
		dx := worldX - t.Point.X
		dy := worldY - t.Point.Y
		if dx*dx+dy*dy <= tolerance*tolerance {
			label = t.Label
			if label == "" {
				label = t.ID
			}
			return label, t.Comment, true
		}
	}
	return "", "", false
}

// SetManualTunnelLabel updates the maker's name for the tunnel — a
// LIVE setter (labels change at runtime via the inspect form), unlike
// the one-shot restore pendings. Português: Atualiza o nome do túnel —
// setter VIVO (muda em runtime pelo formulário), diferente dos
// pendentes de um tiro do restore.
func (m *Manager) SetManualTunnelLabel(id, label string) {
	if t, ok := m.manualTunnels[id]; ok && t.Label != label {
		t.Label = label
		m.markDirty()
	}
}

// ManualTunnelConsumers lists the element ids wired to the tunnel's
// "out" port — the Sequence maps them to phases to enforce the
// unhideable-when-wired rule (Kemper 2026-07-18: "eu não deveria poder
// ocultar um túnel que tem ligação de wire na fase onde estou").
//
// BOTH wire orientations count. Wires are usually stored output→input,
// but a draft started from the CONSUMER'S INPUT can land inverted
// (From=consumer.input → To=tunnel.out) — and a census that only read
// From==tunnel missed it, letting a wired tunnel be hidden (field
// 2026-07-17, screenshot: "eu ocultei um túnel que havia wire
// conectado"). Português: Ids ligados à porta "out". As DUAS
// orientações contam — fio iniciado pelo INPUT do consumidor pode ficar
// invertido, e o censo que só lia From==túnel deixou ocultar túnel
// ligado (campo 2026-07-17).
func (m *Manager) ManualTunnelConsumers(id string) []string {
	var out []string
	for _, w := range m.wires {
		switch {
		case w.From.ElementID == id && w.From.PortName == "out":
			out = append(out, w.To.ElementID)
		case w.To.ElementID == id && w.To.PortName == "out":
			out = append(out, w.From.ElementID)
		}
	}
	return out
}

// ClearManualTunnelRemovedCase drops ONE phase from the removal set —
// the self-heal op: refreshTunnelViews calls it when it finds a WIRED
// phase marked removed (legacy saves, or any future census hole), so
// the invariant "wired is visible" repairs itself on sight. Português:
// Tira UMA fase do conjunto — a autocura: o refreshTunnelViews chama ao
// encontrar fase LIGADA marcada como removida, e o invariante "ligado é
// visível" se conserta ao ser visto.
func (m *Manager) ClearManualTunnelRemovedCase(id, caseID string) {
	t, ok := m.manualTunnels[id]
	if !ok || t.RemovedCases == nil {
		return
	}
	if t.RemovedCases[caseID] {
		delete(t.RemovedCases, caseID)
		if len(t.RemovedCases) == 0 {
			t.RemovedCases = nil
		}
		m.markDirty()
	}
}

// ManualTunnelWireType returns the tunnel's stamped type — the concrete
// type of its FEED wire (the value's true type; taps may carry PROMOTED
// types, e.g. an int feed with a float-promoted tap), falling back to
// the first tap when no feed exists yet. "" = unwired, still chameleon.
// Both wire orientations are read (the inverted-storage lesson of
// 2026-07-17). Português: O tipo carimbado do túnel — o tipo do FEED
// (a verdade do valor; taps podem vir PROMOVIDOS), com fallback no
// primeiro tap. "" = sem fio, ainda camaleão. As duas orientações
// contam (lição do fio invertido).
func (m *Manager) ManualTunnelWireType(id string) string {
	if _, ok := m.manualTunnels[id]; !ok {
		return ""
	}
	fallback := ""
	for _, w := range m.wires {
		feed := (w.To.ElementID == id && w.To.PortName == "in") ||
			(w.From.ElementID == id && w.From.PortName == "in")
		touches := feed ||
			w.From.ElementID == id || w.To.ElementID == id
		if !touches || w.DataType == "" || w.DataType == "*" {
			continue
		}
		if feed {
			return w.DataType
		}
		if fallback == "" {
			fallback = w.DataType
		}
	}
	return fallback
}

// effectiveTypes returns the type list a compatibility check must use
// for a connector: a manual tunnel's connectors NARROW dynamically —
// once any wire stamps the tunnel, both ports offer exactly that type
// (so the matrix keeps enforcing and promoting, same as a direct wire);
// unwired, they stay the registered chameleon ["*"]. Every other
// connector passes through unchanged. The narrowing lives HERE, where
// the wires live — no re-registration dance, no disconnect hooks, the
// truth is always current. Português: A lista de tipos que a checagem
// de compatibilidade deve usar: conectores de túnel ESTREITAM
// dinamicamente — carimbado, os dois lados oferecem exatamente aquele
// tipo (a matriz segue valendo e promovendo, igual a fio direto); sem
// fio, fica o camaleão ["*"] registrado. O estreitamento mora AQUI,
// onde moram os fios — sem re-registro, sem gancho de desconexão.
func (m *Manager) effectiveTypes(c *ConnectorInfo) []string {
	if c == nil {
		return nil
	}
	if _, ok := m.manualTunnels[c.ID.ElementID]; !ok {
		return c.AllowedTypes
	}
	if wt := m.ManualTunnelWireType(c.ID.ElementID); wt != "" {
		return []string{wt}
	}
	return c.AllowedTypes
}

// manualTunnelOwns reports whether elementID is a manual phase-tunnel
// pinned to containerID — recalculateWire uses it to suppress automatic
// crossing markers on the tunnel's own border stubs. Português: Diz se
// elementID é túnel manual preso a containerID — o recalculateWire usa
// para suprimir travessias automáticas nos tocos da própria borda.
func (m *Manager) manualTunnelOwns(elementID, containerID string) bool {
	t, ok := m.manualTunnels[elementID]
	return ok && t.ContainerID == containerID
}

// manualTunnelPortBlocked gates the INTERACTIVE connect paths (candidate
// scan + hit-test) by phase view: a phase-hidden tunnel offers nothing;
// a visible one offers ONLY its role port — "in" while viewing the natal
// phase (you feed it there), "out" in later phases (you tap it there).
// ConnectDirect (the import's wire restore) never passes through here on
// purpose: saved wires must reconnect regardless of which phase happens
// to be on screen. Português: Gating dos caminhos INTERATIVOS por visão
// de fase: túnel escondido não oferece nada; visível oferece SÓ a porta
// do papel. O ConnectDirect (restore) não passa aqui de propósito.
func (m *Manager) manualTunnelPortBlocked(id ConnectorID) bool {
	t, ok := m.manualTunnels[id.ElementID]
	if !ok {
		return false
	}
	if t.PhaseHidden {
		return true
	}
	// SIGNATURE tunnels (no natal) expose BOTH faces: the inner one
	// serves the body, the outer one is the definition's own implicit
	// call-site — LabVIEW semantics (field 2026-07-20, "os túneis de
	// parâmetros e retorno não se conectam ao que está fora"). Phase
	// tunnels keep the single-face-per-pose law. Português: Túneis de
	// ASSINATURA expõem as DUAS faces — a interna serve o corpo, a
	// externa é o call-site implícito da definição; túneis de fase
	// mantêm a lei de face única por pose.
	if t.NatalCase == "" {
		return false
	}
	switch t.Role {
	case "in":
		return id.PortName != "in"
	case "out":
		return id.PortName != "out"
	}
	return false
}

// RemoveManualTunnel forgets a manual tunnel (device removal path).
func (m *Manager) RemoveManualTunnel(id string) {
	delete(m.manualTunnels, id)
	if m.moveManualID == id {
		m.movingTunnel = false
		m.moveManualID = ""
	}
	m.markDirty()
}

// SetOnManualTunnelDelete installs the device-removal callback (the
// factory owns device lifecycles; the menu only pulls this cord).
// Português: Instala o callback de remoção do device — o factory é dono
// do ciclo de vida; o menu só puxa a cordinha.
func (m *Manager) SetOnManualTunnelDelete(fn func(id string)) {
	m.onManualTunnelDelete = fn
}

// RequestManualTunnelDelete pulls the cord (nil-safe).
func (m *Manager) RequestManualTunnelDelete(id string) {
	if m.onManualTunnelDelete != nil {
		log.Printf("[TUNNEL-DEL] request: %s -> cord", id)
		m.onManualTunnelDelete(id)
		return
	}
	// No cord installed — the record dies here but the SHELL survives
	// registered: the exact ghost of the field report. Loud, so the
	// console names this path if it ever runs. Português: Sem cordinha
	// — o registro morre mas a CASCA sobrevive registrada: o fantasma
	// exato do relato. Barulhento para o console nomear.
	log.Printf("[TUNNEL-DEL] request: %s but NO CORD installed — removing record only (ghost shell risk!)", id)
	m.RemoveManualTunnel(id)
}

// ManualTunnelPoint returns the live junction point — the anchor the
// device's connectors report. Português: O ponto vivo da junção — a
// âncora que os conectores do device reportam.
// Ghost-census counters (2026-07-21): cheap truth for the RemoveAll
// audit. Português: Contadores do censo de fantasmas.
func (m *Manager) WireCount() int         { return len(m.wires) }
func (m *Manager) ConnectorCount() int    { return len(m.connectors) }
func (m *Manager) ManualTunnelCount() int { return len(m.manualTunnels) }

// ClearManualTunnelFresh drops the birth-red highlight — for
// programmatic creations (deep copy) that are not fresh gestures.
// Português: Apaga o vermelho de nascimento — criações programáticas.
func (m *Manager) ClearManualTunnelFresh(id string) {
	if t, ok := m.manualTunnels[id]; ok && t != nil {
		t.Fresh = false
		m.markDirty()
	}
}

func (m *Manager) ManualTunnelPoint(id string) (Point, bool) {
	t, ok := m.manualTunnels[id]
	if !ok {
		return Point{}, false
	}
	return t.Point, true
}

// TouchManualTunnel clears the birth-red on any interaction (spec #3).
func (m *Manager) TouchManualTunnel(id string) {
	if t, ok := m.manualTunnels[id]; ok && t.Fresh {
		t.Fresh = false
		m.markDirty()
	}
}

// ManualTunnelAt hit-tests manual tunnels — same tolerance as TunnelAt.
func (m *Manager) ManualTunnelAt(worldX, worldY float64) (id string, ok bool) {
	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * 1.5
	for _, t := range m.manualTunnels {
		// A tunnel does not exist before its natal phase — no hit, no
		// menu. Português: Antes da fase natal o túnel não existe.
		if t.PhaseHidden || m.tunnelOccluded(t) {
			continue
		}
		dx := worldX - t.Point.X
		dy := worldY - t.Point.Y
		if dx*dx+dy*dy <= tolerance*tolerance {
			return t.ID, true
		}
	}
	return "", false
}

// BeginMoveManualTunnel enters the SAME move mode the original tunnels
// use — the workspace flow (pointermove slides, click drops) needs no
// change. Português: Entra no MESMO modo-mover dos túneis originais.
func (m *Manager) BeginMoveManualTunnel(id string) bool {
	t, ok := m.manualTunnels[id]
	if !ok {
		return false
	}
	if _, hasRect := m.containers[t.ContainerID]; !hasRect {
		return false
	}
	m.movingTunnel = true
	m.moveManualID = id
	m.moveTunnelContainer = t.ContainerID
	m.moveTunnelEdge = t.Edge
	t.Fresh = false
	m.markDirty()
	return true
}

// hasWireOnElement reports whether any wire touches the given element —
// the manual tunnel's outline→filled state. Português: Algum fio toca o
// elemento — decide vazado→preenchido.
func (m *Manager) hasWireOnElement(id string) bool {
	for _, w := range m.wires {
		if w.From.ElementID == id || w.To.ElementID == id {
			return true
		}
	}
	return false
}

// projectToEdge projects (px,py) onto the given edge of the rect, clamped to the
// edge's extent. On the left/right edges only Y varies (vertical drag); on the
// top/bottom edges only X varies (horizontal drag) — exactly the constraint the
// tunnel drag enforces.
//
// Português: Projeta (px,py) na borda dada, preso à extensão da borda. Nas
// bordas esquerda/direita só Y varia (arraste vertical); nas de topo/base só X
// varia (arraste horizontal) — exatamente a restrição do arraste do túnel.
func projectToEdge(rx, ry, rw, rh float64, edge int, px, py float64) Point {
	clamp := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	switch edge {
	case edgeRight:
		return Point{rx + rw, clamp(py, ry, ry+rh)}
	case edgeTop:
		return Point{clamp(px, rx, rx+rw), ry}
	case edgeBottom:
		return Point{clamp(px, rx, rx+rw), ry + rh}
	default: // edgeLeft
		return Point{rx, clamp(py, ry, ry+rh)}
	}
}

// pinnedTunnelPoint returns the pinned position of the tunnel shared by wires
// from `feeder` into `containerID`, if any wire of that group has been dragged.
// A tunnel is shared by every wire from the same source into the same container,
// so a newly-created wire adopts the group's pinned position instead of deriving
// its own nearest-border point and splitting off as a second tunnel.
//
// Português: Retorna a posição fixada do túnel compartilhado por fios de
// `feeder` para `containerID`, se algum fio do grupo foi arrastado. Um túnel é
// compartilhado por todo fio da mesma fonte para o mesmo container, então um fio
// recém-criado adota a posição fixada do grupo em vez de derivar seu próprio
// ponto e virar um segundo túnel.
func (m *Manager) pinnedTunnelPoint(feeder ConnectorID, containerID string) (Point, bool) {
	for _, w := range m.wires {
		if w.Tunnel != nil && w.Tunnel.Pinned && w.Tunnel.ContainerID == containerID && w.From == feeder {
			return w.Tunnel.Point, true
		}
	}
	return Point{}, false
}

// StartMoveTunnel enters tunnel-move mode for the tunnel fed by `feeder` on
// `containerID`. The drag is locked to the axis of the border edge the tunnel
// currently sits on. Returns false if no such tunnel is live. The workspace
// drives UpdateMoveTunnel on pointer move and EndMoveTunnel on the next click.
//
// Português: Entra no modo de mover túnel para o túnel alimentado por `feeder`
// em `containerID`, travando o arraste ao eixo da borda atual. Retorna false se
// não houver túnel vivo. O workspace chama UpdateMoveTunnel no pointer move e
// EndMoveTunnel no próximo clique.
func (m *Manager) StartMoveTunnel(feeder ConnectorID, containerID string) bool {
	rectFunc, ok := m.containers[containerID]
	if !ok {
		return false
	}
	var pt Point
	found := false
	for _, w := range m.wires {
		if w.Tunnel != nil && w.Tunnel.ContainerID == containerID && w.From == feeder {
			pt = w.Tunnel.Point
			found = true
			break
		}
	}
	if !found {
		return false
	}
	rx, ry, rw, rh := rectFunc()
	m.movingTunnel = true
	m.moveTunnelFeeder = feeder
	m.moveTunnelContainer = containerID
	m.moveTunnelEdge = tunnelEdgeOf(rx, ry, rw, rh, pt.X, pt.Y)
	m.markDirty()
	return true
}

// UpdateMoveTunnel moves the active tunnel to the pointer position, projected
// onto its locked edge. Every wire sharing the tunnel (the feed and every tap)
// is pinned to the new point and re-routed from there, so the shared tunnel
// slides together as one.
//
// Português: Move o túnel ativo para a posição do ponteiro, projetada na borda
// travada. Todo fio que compartilha o túnel (feed + taps) é fixado no novo ponto
// e re-roteado, então o túnel compartilhado desliza junto.
func (m *Manager) UpdateMoveTunnel(worldX, worldY float64) {
	if !m.movingTunnel {
		return
	}
	rectFunc, ok := m.containers[m.moveTunnelContainer]
	if !ok {
		return
	}
	rx, ry, rw, rh := rectFunc()
	p := projectToEdge(rx, ry, rw, rh, m.moveTunnelEdge, worldX, worldY)
	if m.moveManualID != "" {
		// Manual branch: one record, wires re-anchor for free (their
		// endpoints read the device connectors, whose PositionFunc is
		// ManualTunnelPoint). Português: Ramo manual — um registro; os
		// fios re-ancoram de graça (os conectores leem este ponto).
		if t, ok := m.manualTunnels[m.moveManualID]; ok {
			t.Point = p
			m.RecalculateForElement(m.moveManualID)
		}
		m.markDirty()
		return
	}
	for _, w := range m.wires {
		if w.Tunnel != nil && w.Tunnel.ContainerID == m.moveTunnelContainer && w.From == m.moveTunnelFeeder {
			w.Tunnel.Point = p
			w.Tunnel.Pinned = true
			m.recalculateWire(w)
		}
	}
	m.markDirty()
}

// EndMoveTunnel leaves tunnel-move mode. The pinned position stays in effect for
// the session.
//
// Português: Sai do modo de mover túnel. A posição fixada continua valendo na
// sessão.
func (m *Manager) EndMoveTunnel() {
	if !m.movingTunnel {
		return
	}
	m.movingTunnel = false
	m.moveManualID = ""
	m.markDirty()
}

// IsMovingTunnel reports whether a tunnel is currently being dragged.
//
// Português: Informa se um túnel está sendo arrastado no momento.
func (m *Manager) IsMovingTunnel() bool { return m.movingTunnel }

// GetWire returns the wire with the given ID, or nil if not found.
func (m *Manager) GetWire(id string) *Wire {
	for _, w := range m.wires {
		if w.ID == id {
			return w
		}
	}
	return nil
}

// GetWiresForConnector returns all wires connected to a given connector.
func (m *Manager) GetWiresForConnector(id ConnectorID) []*Wire {
	var result []*Wire
	for _, w := range m.wires {
		if w.From == id || w.To == id {
			result = append(result, w)
		}
	}
	return result
}

// GetConnectorsForElement returns all connectors registered for the given element ID.
//
// Português: Retorna todos os conectores registrados para o dado ID de elemento.
func (m *Manager) GetConnectorsForElement(elementID string) []*ConnectorInfo {
	var result []*ConnectorInfo
	for _, info := range m.connectors {
		if info.ID.ElementID == elementID {
			result = append(result, info)
		}
	}
	return result
}

// GetAllWires returns all active wires (alias for GetWires).
//
// Português: Retorna todos os fios ativos (alias para GetWires).
func (m *Manager) GetAllWires() []*Wire { return m.wires }

// =====================================================================
//  Wire CRUD | CRUD de Fios
// =====================================================================

// SelectWire selects a wire and deselects all others.
func (m *Manager) SelectWire(id string) {
	for _, w := range m.wires {
		w.Selected = (w.ID == id)
	}
	m.markDirty()
}

// DeselectAll deselects all wires.
func (m *Manager) DeselectAll() {
	for _, w := range m.wires {
		w.Selected = false
	}
	m.markDirty()
}

// DeleteSelected deletes all selected wires.
func (m *Manager) DeleteSelected() (count int) {
	remaining := make([]*Wire, 0, len(m.wires))
	for _, w := range m.wires {
		if w.Selected {
			if m.OnWireDeleted != nil {
				m.OnWireDeleted(w)
			}
			count++
		} else {
			remaining = append(remaining, w)
		}
	}
	m.wires = remaining
	if count > 0 {
		m.markDirty()
		if m.OnChange != nil {
			m.OnChange()
		}
	}
	return
}

// DeleteWire removes a wire by its ID.
func (m *Manager) DeleteWire(id string) (deleted bool) {
	// Forensic confession (2026-07-21): every deletion names itself —
	// the field's vanishing wires get a timestamped culprit line.
	// Português: Confissão forense — toda deleção se nomeia.
	log.Printf("[WIRE] DeleteWire: %s", id)
	for i, w := range m.wires {
		if w.ID == id {
			if m.OnWireDeleted != nil {
				m.OnWireDeleted(w)
			}
			m.wires = append(m.wires[:i], m.wires[i+1:]...)
			m.markDirty()
			if m.OnChange != nil {
				m.OnChange()
			}
			deleted = true
			return
		}
	}
	return
}

// DeleteTunnelWires removes every wire routed through the tunnel identified by
// (feeder source, container): all source→device wires from that source that
// cross that container. Because the tunnel is derived from those wires, it
// disappears once they are gone. Returns the number removed. Backs the tunnel
// context menu's Delete action.
//
// Português: Remove todos os fios que passam pelo túnel (fonte feeder +
// container): todo fonte→device daquela fonte que cruza o container. Como o
// túnel é derivado desses fios, ele some quando eles vão embora. Retorna quantos
// foram removidos. Sustenta a ação Delete do menu do túnel.
func (m *Manager) DeleteTunnelWires(feeder ConnectorID, containerID string) (count int) {
	remaining := make([]*Wire, 0, len(m.wires))
	for _, w := range m.wires {
		if w.Tunnel != nil && w.Tunnel.ContainerID == containerID && w.From == feeder {
			if m.OnWireDeleted != nil {
				m.OnWireDeleted(w)
			}
			count++
		} else {
			remaining = append(remaining, w)
		}
	}
	m.wires = remaining
	if count > 0 {
		m.markDirty()
		if m.OnChange != nil {
			m.OnChange()
		}
	}
	return
}

// DisconnectConnector removes all wires from a specific connector.
func (m *Manager) DisconnectConnector(id ConnectorID) (count int) {
	remaining := make([]*Wire, 0, len(m.wires))
	for _, w := range m.wires {
		if w.From == id || w.To == id {
			if m.OnWireDeleted != nil {
				m.OnWireDeleted(w)
			}
			count++
		} else {
			remaining = append(remaining, w)
		}
	}
	m.wires = remaining
	if count > 0 {
		m.markDirty()
		if m.OnChange != nil {
			m.OnChange()
		}
	}
	return
}

// =====================================================================
//  Wire Update | Atualização de Fios
// =====================================================================

// RecalculateAll recalculates the route of all wires.
//
// Português: Recalcula a rota de todos os fios.
func (m *Manager) RecalculateAll() {
	for _, w := range m.wires {
		m.recalculateWire(w)
	}
	m.markDirty()
}

// RecalculateForElement recalculates wires connected to the given element.
//
// Português: Recalcula fios conectados ao elemento dado.
func (m *Manager) RecalculateForElement(elementID string) {
	// A container's border affects every wire that CROSSES it (its tunnel
	// point on that border), not only wires attached to the container. So when
	// a container moves or resizes, recompute all wires — otherwise tunnels
	// would not follow the container.
	if _, isContainer := m.containers[elementID]; isContainer {
		m.RecalculateAll()
		return
	}
	changed := false
	for _, w := range m.wires {
		if w.From.ElementID == elementID || w.To.ElementID == elementID {
			m.recalculateWire(w)
			changed = true
		}
	}
	if changed {
		m.markDirty()
	}
}

func (m *Manager) recalculateWire(w *Wire) {
	fromConn := m.GetConnector(w.From)
	toConn := m.GetConnector(w.To)
	if fromConn == nil || toConn == nil {
		return
	}

	fromX, fromY := fromConn.PositionFunc()
	toX, toY := toConn.PositionFunc()

	// [DENSITY-FIX] Scale stub length by density.
	d := rulesDensity.GetDensity()
	stubLen := defaultStubLength * d

	// Tunnel routing (LabVIEW-style). If this wire crosses a registered
	// container's border, route it THROUGH a tunnel point on that border — two
	// Manhattan segments joined at the tunnel (conexão → túnel → conexão) —
	// instead of cutting straight across. The marker is then on the wire by
	// construction. The wire stays one logical connection, so codegen is
	// unaffected.
	//
	// EXCEPT for a manual phase-tunnel's own wires against its OWN
	// container: the manual tunnel IS the border furniture there, and
	// its pin anchors legitimately sit just outside the border — minting
	// an automatic crossing for that stub painted a second, phantom
	// square with the wire looping around the manual one (field
	// 2026-07-18). A manual tunnel's wire crossing SOME OTHER container
	// still mints normally. Português: EXCETO fios do próprio túnel
	// manual contra o PRÓPRIO container: ali o túnel manual É o móvel de
	// borda, e as pontas dos pinos ficam legitimamente um pouco fora —
	// cunhar travessia automática nesse toco pintava um segundo quadrado
	// fantasma com o fio dando a volta no manual (campo 2026-07-18).
	// Fio de túnel manual cruzando OUTRO container cunha normalmente.
	if tp, cid, ok := m.wireTunnelPoint(w, fromX, fromY, toX, toY); ok &&
		!m.manualTunnelOwns(w.From.ElementID, cid) &&
		!m.manualTunnelOwns(w.To.ElementID, cid) {
		// A tunnel is shared by every wire from the same source into the same
		// container. If the user has pinned that shared tunnel (dragged it), keep
		// EVERY wire of the group — including a freshly-created one — on the
		// pinned point, re-projected onto the container's current border so it
		// tracks moves/resizes. Without this, a new wire derives its own
		// nearest-border point and splits off as a second tunnel. Auto-placed
		// (un-pinned) groups keep using the derived point.
		//
		// Português: Um túnel é compartilhado por todo fio da mesma fonte para o
		// mesmo container. Se o usuário fixou esse túnel (arrastou), mantém TODO
		// fio do grupo — inclusive um recém-criado — no ponto fixado, re-projetado
		// na borda atual para acompanhar movimento/resize. Sem isto, um fio novo
		// deriva seu próprio ponto e vira um segundo túnel. Grupos não-fixados
		// seguem usando o ponto derivado.
		pinned := false
		if pt, has := m.pinnedTunnelPoint(w.From, cid); has {
			if rectFunc, found := m.containers[cid]; found {
				rx, ry, rw, rh := rectFunc()
				edge := tunnelEdgeOf(rx, ry, rw, rh, pt.X, pt.Y)
				tp = projectToEdge(rx, ry, rw, rh, edge, pt.X, pt.Y)
				pinned = true
			}
		}
		seg1 := ComputeManhattanRouteWithStub(Point{fromX, fromY}, tp, stubLen)
		seg2 := ComputeManhattanRouteWithStub(tp, Point{toX, toY}, stubLen)
		// Join the two segments, dropping seg2's duplicated leading point
		// (== tp == seg1's last point). The tunnel point lands at index
		// len(seg1)-1, which the renderer uses to split feed from tap.
		w.Tunnel = &WireTunnel{ContainerID: cid, Point: tp, SplitIndex: len(seg1) - 1, Pinned: pinned}
		w.Waypoints = append(seg1, seg2[1:]...)
		return
	}

	w.Tunnel = nil
	w.Waypoints = ComputeManhattanRouteWithStub(Point{fromX, fromY}, Point{toX, toY}, stubLen)
}

// wireTunnelPoint returns the tunnel point (on a container's border) for a wire
// that crosses a container, plus that container's ID. For an input tunnel (the
// source is outside) the point is the border point NEAREST the source, so it is
// independent of the consumer device — every wire from the same source shares
// one tunnel — and the feed approaches the border perpendicular. For an output
// tunnel (source inside) it falls back to the straight source→target crossing.
// ok is false when the wire crosses no registered container; a wire that
// connects to a container's OWN border port (e.g. the StatementCase "?"
// selector) is skipped for that container, since that is not a crossing.
//
// Português: Ponto do túnel (na borda de um container) para um fio que o cruza,
// e o ID do container. Túnel de entrada (fonte fora): o ponto da borda mais
// próximo da fonte — independe do device (fios da mesma fonte compartilham um
// túnel) e o feed chega perpendicular. Túnel de saída (fonte dentro): cai no
// cruzamento reto fonte→destino. ok=false se não
// cruza nenhum container; fios ligados à porta da própria borda (ex.: o "?") são
// ignorados para aquele container.
func (m *Manager) wireTunnelPoint(w *Wire, fromX, fromY, toX, toY float64) (Point, string, bool) {
	for id, rectFunc := range m.containers {
		// Skip only when the wire LEAVES its own frame (From == id): the
		// output pin sits ON the border and a crossing minted there is a
		// phantom square on the pin. A wire ENTERING its own destination
		// frame (To == id, e.g. paramTunnel → loop.stop) is the LabVIEW
		// picture the maker expects — the !fromIn branch anchors at the
		// SOURCE's clamp, far from the pin, so the entry marker is real
		// and safe (field 2026-07-19: "não há túnel entre o túnel de
		// entrada e o laço"). Português: Pula só quando o fio SAI da
		// própria moldura; fio ENTRANDO na moldura de destino cunha o
		// marcador de entrada — a âncora vem do clamp da FONTE, longe
		// do pino.
		if w.From.ElementID == id {
			continue
		}
		rx, ry, rw, rh := rectFunc()
		if rw <= 0 || rh <= 0 {
			continue
		}
		fromIn := pointInRect(rx, ry, rw, rh, fromX, fromY)
		toIn := pointInRect(rx, ry, rw, rh, toX, toY)
		if fromIn == toIn {
			continue // not a crossing (both inside or both outside)
		}
		if !fromIn {
			// Input tunnel: the source (output) is OUTSIDE. Anchor the tunnel at
			// the border point nearest the source — clamp the source position to
			// the rect. This makes the tunnel independent of the consumer device
			// (so every wire from this source shares ONE tunnel point, the basis
			// for the shared input port) and makes the feed (source→tunnel)
			// approach the border perpendicular.
			tx := fromX
			if tx < rx {
				tx = rx
			} else if tx > rx+rw {
				tx = rx + rw
			}
			ty := fromY
			if ty < ry {
				ty = ry
			} else if ty > ry+rh {
				ty = ry + rh
			}
			return Point{tx, ty}, id, true
		}
		// Output tunnel (source inside): fall back to the straight source→target
		// crossing for now (per-wire). A device-independent shared exit tunnel
		// for outputs is a later refinement.
		if px, py, ok := borderCrossing(rx, ry, rw, rh, fromX, fromY, toX, toY); ok {
			return Point{px, py}, id, true
		}
	}
	return Point{}, "", false
}

// =====================================================================
//  Rendering | Renderização
// =====================================================================

// Draw renders all wires onto the canvas.
//
// [CAMERA-FIX] If a camera function is set, Draw() applies the camera
// transform (save → setTransform → draw → restore) so that wire waypoints
// (stored in world coordinates) appear correctly on screen.
//
// [DENSITY-FIX] All visual sizes are scaled by rulesDensity.GetDensity().
//
// Português:
//
//	Draw renderiza todos os fios no canvas.
//
//	[CAMERA-FIX] Se câmera definida, aplica save → setTransform → draw → restore.
//	[DENSITY-FIX] Todos os tamanhos visuais são escalados por densidade.
func (m *Manager) Draw() {
	if m.ctx.IsUndefined() {
		return
	}

	d := rulesDensity.GetDensity()

	// [CAMERA-FIX] Apply camera transform so world-coordinate waypoints
	// map to screen-coordinate pixels.
	// Português: Aplica transformação da câmera.
	if m.cameraFunc != nil {
		offX, offY, zoom := m.cameraFunc()
		m.ctx.Call("save")
		m.ctx.Call("setTransform",
			zoom, 0,
			0, zoom,
			-offX*zoom,
			-offY*zoom,
		)
	}

	for _, w := range m.wires {
		// A wire endpoint that is a PHASE-HIDDEN manual tunnel counts
		// as hidden too (field 2026-07-20, "as conexões estão vazando
		// entre fases"): member hiding never covers tunnel↔tunnel
		// wires — tunnels are membership-exempt — so a wire between a
		// hidden phase tunnel and a signature tunnel leaked across
		// every phase. The record's Hidden is the same state the
		// hosts' refreshers already maintain. Português: Ponta que é
		// túnel FASE-OCULTO conta como oculta — o esconder-membros
		// nunca cobre fio túnel↔túnel (túneis são isentos de
		// membresia) e o fio vazava entre fases; o Hidden do registro
		// é o estado que os refreshers já mantêm.
		fromHidden := m.hiddenElements[w.From.ElementID] || m.manualTunnelPhaseHidden(w.From.ElementID)
		toHidden := m.hiddenElements[w.To.ElementID] || m.manualTunnelPhaseHidden(w.To.ElementID)

		// Z-burial: either endpoint's owning container covered by a
		// higher-z sibling hides the whole wire — the ensemble sinks
		// with its container. Português: Enterro por z — qualquer ponta
		// com o container coberto esconde o fio inteiro; o conjunto
		// afunda com o container.
		if len(w.Waypoints) > 0 {
			if m.wireEndpointOccluded(w.From.ElementID, w.Waypoints[0]) ||
				m.wireEndpointOccluded(w.To.ElementID, w.Waypoints[len(w.Waypoints)-1]) {
				continue
			}
		}

		// Tunnelled wire with only the consumer hidden (its case is inactive):
		// draw just the feed (source→tunnel) so the const→tunnel connection
		// stays visible across case switches — the tap (tunnel→consumer) hides
		// with its case. The tunnel marker itself is drawn in the pass below.
		if w.Tunnel != nil && !fromHidden && toHidden {
			idx := w.Tunnel.SplitIndex
			if idx >= 1 && idx < len(w.Waypoints) {
				feed := w.Waypoints[:idx+1]
				drawWirePolyline(m.ctx, feed, w.Style, w.Selected, d)
				drawConnectorDot(m.ctx, feed[0].X, feed[0].Y, w.Style.StrokeColor, 3.0*d)
			}
			continue
		}

		// Otherwise skip wires that touch a hidden element (e.g. the inactive
		// IfElse branch) — the wire would be left floating with no visible
		// endpoints (the orphan-wire bug).
		if fromHidden || toHidden {
			continue
		}
		drawWire(m.ctx, w, d)

		// Draw connector dots at endpoints.
		if len(w.Waypoints) >= 2 {
			first := w.Waypoints[0]
			last := w.Waypoints[len(w.Waypoints)-1]
			drawConnectorDot(m.ctx, first.X, first.Y, w.Style.StrokeColor, 3.0*d)
			drawConnectorDot(m.ctx, last.X, last.Y, w.Style.StrokeColor, 3.0*d)
		}
	}

	// Tunnel markers (LabVIEW-style). A wire that crosses a container border is
	// routed THROUGH a tunnel point on that border (see recalculateWire); draw
	// the tunnel square there, in the wire's data-type colour. The marker shows
	// whenever the feed (source→tunnel) is visible — i.e. the source is visible
	// — even when the consumer's case is hidden, so the tunnel persists across
	// case switches like a LabVIEW input tunnel. Only skip if the source itself
	// is hidden.
	for _, w := range m.wires {
		if w.Tunnel == nil {
			continue
		}
		if m.hiddenElements[w.From.ElementID] {
			continue
		}
		// Automatic markers sink with their container too (same z-burial
		// rule as the manual squares). Português: Marcadores automáticos
		// afundam com o container (mesma regra de enterro por z).
		if m.markerOccluder != nil &&
			m.markerOccluder(w.Tunnel.ContainerID, w.Tunnel.Point.X, w.Tunnel.Point.Y) {
			continue
		}
		drawTunnelMarker(m.ctx, w.Tunnel.Point.X, w.Tunnel.Point.Y, w.Style.StrokeColor, d)
	}

	// Manual phase-tunnels — the maker-created siblings of the markers
	// above ("copie o túnel original", 2026-07-17): same square, internal
	// palette, outline until wired, red until first interaction. The
	// phase view (Kemper spec 2026-07-18) decides existence and pin:
	// hidden before the natal phase; "in" on the natal phase (right
	// border, pin inward-left); "out" later (left border, pin
	// inward-right). Português: Túneis manuais — a visão de fase decide
	// existência e pino: escondido antes da natal; "in" na natal; "out"
	// depois.
	for _, t := range m.manualTunnels {
		if t.PhaseHidden || m.tunnelOccluded(t) {
			continue
		}
		// Pin colour = the stamped type's wire colour (chameleon v2) —
		// slices, probes and handles included; violet while unwired.
		// Português: Cor do pino = cor do tipo carimbado; violeta sem fio.
		// Unwired base color tells the tunnel SPECIES apart (Kemper
		// 2026-07-20): SIGNATURE tunnels stay violet, PHASE tunnels
		// (natal stamped) wear the house peach — the moment of
		// creation/organisation is when the maker needs the
		// distinction. Once wired, the TYPE color takes over for both
		// (data-type readability is sacred). Português: A cor
		// sem-fio separa as ESPÉCIES — assinatura violeta, fase
		// (natal carimbado) no pêssego da casa; com fio, a cor do
		// TIPO assume para ambos (legibilidade de tipo é sagrada).
		baseColor := manualTunnelViolet
		if m.ManualTunnelNatal(t.ID) != "" {
			baseColor = manualTunnelPhasePeach
		}
		pinColor := baseColor
		if wt := m.ManualTunnelWireType(t.ID); wt != "" {
			pinColor = m.getTypeStyle(wt).StrokeColor
		}
		// Only a CUSTOM name paints — the default "tunnel_N" id would
		// be stage noise. Português: Só nome CUSTOMIZADO pinta.
		lbl := t.Label
		if lbl == t.ID {
			lbl = ""
		}
		drawManualTunnelMarker(m.ctx, t.Point.X, t.Point.Y,
			m.hasWireOnElement(t.ID), t.Fresh, t.NatalCase == "", t.Role, pinColor, baseColor, lbl, d)
	}

	// Visual connect mode: highlight candidate connectors + draft wire.
	//
	// Português: Modo visual de conexão: destaca conectores candidatos + fio provisório.
	if m.connectMode == ConnectModeSelectingTarget && m.connectSource != nil {
		source := m.GetConnector(*m.connectSource)
		if source != nil {
			fromX, fromY := source.PositionFunc()

			// Highlight candidate connectors with larger, brighter dots.
			// Português: Destaca conectores candidatos com pontos maiores e mais brilhantes.
			for _, c := range m.visualCandidates {
				cx, cy := c.Connector.PositionFunc()
				style := m.getTypeStyle(c.ResolvedType)

				// Outer glow ring
				drawConnectorDot(m.ctx, cx, cy, style.SelectedColor, 8.0*d)
				// Inner solid dot
				drawConnectorDot(m.ctx, cx, cy, style.StrokeColor, 5.0*d)
			}

			// A visible tunnel whose feeder is one of the candidates is also a
			// live drop target (HitTestConnector resolves a click on it to that
			// feeder). Glow it the same way so the maker sees they can drop the
			// wire on the tunnel instead of hunting for the off-container source
			// dot. The off-container dot still glows above, so both spots read as
			// valid — they complete the very same connection.
			//
			// Português: Um túnel visível cujo feeder é um dos candidatos também é
			// um alvo válido (o HitTestConnector resolve um clique nele para esse
			// feeder). Destaca do mesmo jeito para o maker ver que pode soltar o
			// fio no túnel em vez de procurar o conector da fonte fora do
			// container. O conector externo continua destacado acima, então os
			// dois pontos valem — completam exatamente a mesma conexão.
			for _, tw := range m.wires {
				if tw.Tunnel == nil || m.hiddenElements[tw.From.ElementID] {
					continue
				}
				for _, c := range m.visualCandidates {
					if c.Connector.ID == tw.From {
						style := m.getTypeStyle(c.ResolvedType)
						drawConnectorDot(m.ctx, tw.Tunnel.Point.X, tw.Tunnel.Point.Y, style.SelectedColor, 8.0*d)
						drawConnectorDot(m.ctx, tw.Tunnel.Point.X, tw.Tunnel.Point.Y, style.StrokeColor, 5.0*d)
						break
					}
				}
			}

			// Draw draft wire from source connector to pointer position.
			// Português: Desenha fio provisório do conector de origem até a posição do ponteiro.
			if m.draftEndpoint.X != 0 || m.draftEndpoint.Y != 0 {
				var draftType string
				if et := m.effectiveTypes(source); len(et) > 0 {
					draftType = et[0]
				}
				style := m.getTypeStyle(draftType)
				drawDraftWire(m.ctx, Point{fromX, fromY}, m.draftEndpoint, style, d)
			}
		}
	}

	if m.cameraFunc != nil {
		m.ctx.Call("restore")
	}
}

// =====================================================================
//  Hit Testing | Teste de Colisão
// =====================================================================

// HitTest returns the wire at the given screen coordinates, or nil if none.
//
// [CAMERA-FIX] Screen coordinates are converted to world coordinates.
// [DENSITY-FIX] Tolerance is scaled by density.
//
// Português:
//
//	HitTest retorna o fio nas coordenadas de tela, ou nil.
//	[CAMERA-FIX] Coordenadas de tela são convertidas para mundo.
//	[DENSITY-FIX] Tolerância é escalada por densidade.
func (m *Manager) HitTest(canvasX float64, canvasY float64) *Wire {
	return m.hitTestTol(canvasX, canvasY, 1)
}

// HitTestFat is the "invisible thicker layer" over every wire: identical
// geometry, but a 3× tolerance corridor, so the line is comfortable to
// click even at thin stroke widths. Used by the stage click interceptor,
// which gives wires priority over elements — this is what makes a wire
// selectable/deletable when it crosses a container (loop) body.
// Português: A "camada invisível mais grossa" sobre cada wire: mesma
// geometria, corredor de tolerância 3×, confortável de clicar mesmo com
// traço fino. Usado pelo interceptador de clique do stage, que dá
// prioridade aos wires sobre elements — é o que torna um wire selecionável
// ou apagável quando cruza o corpo de um container (loop).
func (m *Manager) HitTestFat(canvasX float64, canvasY float64) *Wire {
	return m.hitTestTol(canvasX, canvasY, 3)
}

func (m *Manager) hitTestTol(canvasX, canvasY, toleranceMult float64) *Wire {
	d := rulesDensity.GetDensity()
	tolerance := m.hitTolerance * d * toleranceMult

	// [CAMERA-FIX] Convert screen → world.
	// screenX = (worldX - offsetX) * zoom  →  worldX = screenX/zoom + offsetX
	worldX, worldY := canvasX, canvasY
	if m.cameraFunc != nil {
		offX, offY, zoom := m.cameraFunc()
		if zoom != 0 {
			worldX = canvasX/zoom + offX
			worldY = canvasY/zoom + offY
		}
	}

	for i := len(m.wires) - 1; i >= 0; i-- {
		if hitTestWire(m.wires[i], worldX, worldY, tolerance) {
			return m.wires[i]
		}
	}
	return nil
}

// =====================================================================
//  Validation | Validação
// =====================================================================

// ValidateConnections checks all connectors for missing required connections.
//
// Português: Verifica todos os conectores para conexões obrigatórias ausentes.
func (m *Manager) ValidateConnections() (errors []ConnectorID) {
	for _, info := range m.connectors {
		if info.AcceptNotConnected {
			continue
		}
		count := m.countWiresOnConnector(info.ID)
		if count == 0 {
			errors = append(errors, info.ID)
		}
	}
	return
}

// =====================================================================
//  Private Helpers | Helpers Privados
// =====================================================================

func (m *Manager) countWiresOnConnector(id ConnectorID) int {
	count := 0
	for _, w := range m.wires {
		if w.From == id || w.To == id {
			count++
		}
	}
	return count
}

func (m *Manager) isConnected(a ConnectorID, b ConnectorID) bool {
	for _, w := range m.wires {
		if (w.From == a && w.To == b) || (w.From == b && w.To == a) {
			return true
		}
	}
	return false
}

func (m *Manager) removeWiresForConnector(id ConnectorID) {
	remaining := make([]*Wire, 0, len(m.wires))
	for _, w := range m.wires {
		if w.From == id || w.To == id {
			if m.OnWireDeleted != nil {
				m.OnWireDeleted(w)
			}
		} else {
			remaining = append(remaining, w)
		}
	}
	m.wires = remaining
	m.markDirty()
}
