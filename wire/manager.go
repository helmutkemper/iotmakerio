// wire/manager.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import (
	"fmt"
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
	containers map[string]func() (x, y, w, h float64)

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
	movingTunnel        bool
	moveTunnelFeeder    ConnectorID
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
		_, _, resolved := findCompatibleTypes(from.AllowedTypes, to.AllowedTypes, compat)
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
			outputTypes = source.AllowedTypes
			inputTypes = target.AllowedTypes
		} else {
			outputTypes = target.AllowedTypes
			inputTypes = source.AllowedTypes
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
		outputTypes = source.AllowedTypes
		inputTypes = target.AllowedTypes
	} else {
		fromID = targetID
		toID = sourceID
		outputTypes = target.AllowedTypes
		inputTypes = source.AllowedTypes
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
		outputTypes = fromConn.AllowedTypes
		inputTypes = toConn.AllowedTypes
	} else {
		outputTypes = toConn.AllowedTypes
		inputTypes = fromConn.AllowedTypes
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
	if tp, cid, ok := m.wireTunnelPoint(w, fromX, fromY, toX, toY); ok {
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
		if w.From.ElementID == id || w.To.ElementID == id {
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
		fromHidden := m.hiddenElements[w.From.ElementID]
		toHidden := m.hiddenElements[w.To.ElementID]

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
		drawTunnelMarker(m.ctx, w.Tunnel.Point.X, w.Tunnel.Point.Y, w.Style.StrokeColor, d)
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
				if len(source.AllowedTypes) > 0 {
					draftType = source.AllowedTypes[0]
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
