// /ide/scenegraph/graph.go

package scenegraph

// graph.go — the live scenegraph.
//
// English:
//
//	The Graph is the single source of truth for spatial relationships on
//	the stage. It holds a map of Nodes keyed by device ID, and a parallel
//	map of DeviceRefs the nodes mirror. Lifecycle methods (Register,
//	Unregister, BeginDrag, UpdateDrag, EndDrag, BeginResize, UpdateResize,
//	EndResize) are the public surface; queries (FindParent, FindConflicts,
//	ChildrenOf, ChildrenBounds, ParentInnerRect, Snapshot) are the way
//	consumers read state.
//
//	Not goroutine-safe. The IDE runs single-threaded in a WASM main loop;
//	if threading is added later, callers should synchronise at the public
//	method boundary.
//
// Português:
//
//	O Graph é a fonte única de verdade das relações espaciais da stage.
//	Não é seguro para goroutines.

// Graph is the live scenegraph. One instance per stage; the IDE creates
// it once at startup and keeps it until the stage is destroyed.
type Graph struct {
	nodes    map[string]*Node     // every registered device
	refs     map[string]DeviceRef // same keys as nodes
	order    []string             // registration order; used by Snapshot
	observer Observer             // may be nil (treated as NoopObserver)
}

// NewGraph creates an empty graph with a NoopObserver. Install a real
// observer via SetObserver before the first Register.
//
// Português: Cria um grafo vazio com um NoopObserver.
func NewGraph() *Graph {
	return &Graph{
		nodes:    make(map[string]*Node),
		refs:     make(map[string]DeviceRef),
		order:    make([]string, 0),
		observer: NoopObserver{},
	}
}

// SetObserver installs the observer. Passing nil reinstalls NoopObserver.
//
// Português: Instala o observer. nil volta ao NoopObserver.
func (g *Graph) SetObserver(o Observer) {
	if o == nil {
		g.observer = NoopObserver{}
		return
	}
	g.observer = o
}

// =====================================================================
//  Lifecycle — register and unregister
// =====================================================================

// Register adds a device to the graph. Called once per device, after
// the device's Init has completed and its initial geometry is readable
// via the DeviceRef. Computes the initial parent and conflicts; notifies
// the observer of both.
//
// If a device with the same ID is already registered, Register is a
// silent no-op.
//
// Português: Registra um device no grafo. Chamado uma vez por device,
// após Init. Computa pai inicial e conflitos; notifica o observer.
func (g *Graph) Register(ref DeviceRef) {
	id := ref.ID()
	if _, exists := g.nodes[id]; exists {
		return
	}

	node := &Node{
		ID:          id,
		Kind:        ref.Kind(),
		Outer:       ref.OuterRect(),
		Inner:       ref.InnerRect(),
		ChildrenIDs: make([]string, 0),
	}
	g.nodes[id] = node
	g.refs[id] = ref
	g.order = append(g.order, id)

	// Compute initial parent and link it.
	parentID := g.computeParent(id)
	if parentID != "" {
		node.ParentID = parentID
		g.appendChild(parentID, id)
		g.observer.OnParentChanged(id, "", parentID)
	}

	// Fire initial conflicts for the new device.
	g.recomputeAndNotifyConflicts(id)

	// A newly-placed device can create conflicts for existing peers
	// that didn't have any. Re-check every peer; notifyConflictsIfChanged
	// will dedupe the no-op cases.
	for peerID := range g.nodes {
		if peerID == id {
			continue
		}
		g.recomputeAndNotifyConflicts(peerID)
	}
}

// Unregister removes a device from the graph. Cleans up parent/children
// links. Any children of the removed device are reassigned to the
// removed device's parent (or become root if it had none); a
// ParentChanged event fires for each.
//
// Also: any peer device whose conflict list referenced the removed
// device has its conflicts re-notified so that red borders clear.
//
// Português: Remove um device do grafo. Filhos do device removido são
// reatribuídos ao avô (ou viram raiz). Peers com conflitos contra o
// removido têm suas listas re-notificadas.
func (g *Graph) Unregister(deviceID string) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}

	// Re-parent children of the removed device to its own parent.
	grandparent := node.ParentID
	for _, childID := range node.ChildrenIDs {
		child := g.nodes[childID]
		if child == nil {
			continue
		}
		oldParent := child.ParentID
		child.ParentID = grandparent
		if grandparent != "" {
			g.appendChild(grandparent, childID)
		}
		g.observer.OnParentChanged(childID, oldParent, grandparent)
	}

	// Remove from parent's children list.
	if grandparent != "" {
		g.removeChild(grandparent, deviceID)
	}

	// Collect peers that currently list deviceID as a conflict; we'll
	// re-notify them after removal.
	var affected []string
	for peerID, peer := range g.nodes {
		if peerID == deviceID {
			continue
		}
		for _, c := range peer.lastNotifiedConflicts {
			if c.With == deviceID {
				affected = append(affected, peerID)
				break
			}
		}
	}

	// Delete the node.
	delete(g.nodes, deviceID)
	delete(g.refs, deviceID)
	for i, id := range g.order {
		if id == deviceID {
			g.order = append(g.order[:i], g.order[i+1:]...)
			break
		}
	}

	// Re-notify conflicts on affected peers — this will clear red
	// borders that were raised against the removed device.
	for _, peerID := range affected {
		g.recomputeAndNotifyConflicts(peerID)
	}
}

// =====================================================================
//  Drag lifecycle
// =====================================================================

// BeginDrag marks the device as Dragging. If the device is Complex, the
// flat list of descendants (children, grandchildren, ...) present at
// this moment is captured; EndDrag will translate every one of them by
// the cumulative (dx, dy) of the gesture.
//
// Português: Marca o device como Dragging. Se for Complex, captura a
// lista plana de todos os descendentes presentes agora.
func (g *Graph) BeginDrag(deviceID string) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}
	node.Interaction = Dragging
	node.dragSnapshot = nil
	if node.Kind == KindComplex {
		node.dragSnapshot = g.flattenDescendants(deviceID)
	}
}

// UpdateDrag refreshes the device's mirrored geometry and recomputes
// its conflicts, notifying the observer on change. Also re-checks every
// peer's conflict set, because a move of A changes B's list just as it
// changes A's. Safe to call every frame.
//
// Parent/children links are NOT updated during UpdateDrag — that
// happens only in EndDrag.
//
// Português: Atualiza a geometria e recomputa conflitos (do device e
// dos peers). Pai e filhos não são mexidos; isso é feito em EndDrag.
func (g *Graph) UpdateDrag(deviceID string) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}
	if node.Interaction != Dragging {
		return
	}
	g.refreshGeometry(deviceID)
	g.recomputeAndNotifyConflicts(deviceID)
	for peerID := range g.nodes {
		if peerID == deviceID {
			continue
		}
		g.recomputeAndNotifyConflicts(peerID)
	}
}

// EndDrag finalises a drag gesture. In order:
//
//  1. If the device is Fitting, apply snap (currently no-op).
//  2. Translate every device in the drag snapshot by (dx, dy) via MoveBy,
//     then refresh their geometry in the graph.
//  3. Refresh the device's own geometry (in case MoveBy on self was
//     implied).
//  4. Recompute conflicts. If empty, recompute and relink the parent.
//     If non-empty, keep the old parent and leave red borders for the
//     user to resolve.
//  5. Clear the drag snapshot and set Interaction back to Idle.
//
// Português: Finaliza um drag. Translada descendentes, recomputa
// conflitos, religa o pai se estiver tudo limpo.
func (g *Graph) EndDrag(deviceID string, dx, dy float64) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}

	// 1. Fitting snap (future).
	if node.Kind == KindFitting {
		_, _ = applyFittingSnap(node, g.candidates())
	}

	// 2. Move descendants (only Complex devices have a snapshot).
	if (dx != 0 || dy != 0) && len(node.dragSnapshot) > 0 {
		for _, descID := range node.dragSnapshot {
			ref := g.refs[descID]
			if ref == nil {
				continue
			}
			ref.MoveBy(dx, dy)
			g.refreshGeometry(descID)
		}
	}
	node.dragSnapshot = nil

	// 3. Refresh the dragged device's own geometry.
	g.refreshGeometry(deviceID)

	// 4. Parent reassignment only if conflicts are clean.
	conflicts := findConflicts(deviceID, g.candidates())
	g.notifyConflictsIfChanged(deviceID, conflicts)

	// Peers may have gained/lost conflicts from the move too.
	for peerID := range g.nodes {
		if peerID == deviceID {
			continue
		}
		g.recomputeAndNotifyConflicts(peerID)
	}

	if len(conflicts) == 0 {
		newParentID := findParent(node.Outer, deviceID, g.candidates())
		if newParentID != node.ParentID {
			oldParent := node.ParentID
			if oldParent != "" {
				g.removeChild(oldParent, deviceID)
			}
			node.ParentID = newParentID
			if newParentID != "" {
				g.appendChild(newParentID, deviceID)
			}
			g.observer.OnParentChanged(deviceID, oldParent, newParentID)
		}
	}

	node.Interaction = Idle
}

// =====================================================================
//  Resize lifecycle
// =====================================================================

// BeginResize marks a Complex device as Resizing and caches the two
// constraint rectangles:
//
//   - resizeFloor: union of every child's border 1. The container's
//     border 3 cannot shrink past this (else it would excise a child).
//     nil when the container has no children.
//
//   - resizeCeiling: the parent's border 3, if any. The container's
//     border 1 cannot grow past this (else it would burst through its
//     own container).
//     nil when the container has no parent.
//
// No-op when the device is not Complex.
//
// Português: Marca um Complex como Resizing e cacheia piso (filhos) e
// teto (pai). Sem efeito se o device não for Complex.
func (g *Graph) BeginResize(deviceID string) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}
	if node.Kind != KindComplex {
		return
	}
	node.Interaction = Resizing

	node.resizeFloor = nil
	if len(node.ChildrenIDs) > 0 {
		var floor Rect
		first := true
		for _, childID := range node.ChildrenIDs {
			child := g.nodes[childID]
			if child == nil {
				continue
			}
			if first {
				floor = child.Outer
				first = false
			} else {
				floor = floor.Union(child.Outer)
			}
		}
		if !first {
			node.resizeFloor = &floor
		}
	}

	node.resizeCeiling = nil
	if node.ParentID != "" {
		parent := g.nodes[node.ParentID]
		if parent != nil && parent.Inner != nil {
			c := *parent.Inner
			node.resizeCeiling = &c
		}
	}
}

// EndResize drops the resize caches and returns the device to Idle.
// No conflict re-check is needed because every UpdateResize already
// clamped against the floor and ceiling.
//
// Português: Limpa os caches de resize e volta a Idle.
func (g *Graph) EndResize(deviceID string) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}
	node.resizeFloor = nil
	node.resizeCeiling = nil
	node.Interaction = Idle

	// Refresh geometry in case the device's inner changed (padding,
	// etc.), so subsequent queries see the final rectangle.
	g.refreshGeometry(deviceID)
}

// =====================================================================
//  Queries
// =====================================================================

// FindParent returns the ID of the smallest Complex whose border 3 fully
// contains outer, ignoring the device with excludeID. Returns "" if no
// container qualifies.
//
// Português: Retorna o ID do menor Complex cuja border 3 contém outer
// totalmente, ignorando excludeID.
func (g *Graph) FindParent(outer Rect, excludeID string) string {
	return findParent(outer, excludeID, g.candidates())
}

// FindConflicts returns every current conflict involving deviceID.
// Pure function of current geometry.
//
// Português: Retorna todos os conflitos atuais envolvendo deviceID.
func (g *Graph) FindConflicts(deviceID string) []Conflict {
	if _, ok := g.nodes[deviceID]; !ok {
		return nil
	}
	return findConflicts(deviceID, g.candidates())
}

// SetHidden marks (or unmarks) a device as hidden. A hidden device is
// excluded from every spatial query — conflict detection, parent finding,
// and fitting snap — via candidates(); its parent/child links and geometry
// are left untouched. Used by container devices (IfElse) to take the
// inactive branch's devices out of collision while keeping them in the tree
// so they still drag rigidly with the container.
//
// Toggling recomputes conflicts for the device and every peer: hiding it
// clears its own conflict markers and any peer marker that referenced only
// it; showing it surfaces any real conflict. notifyConflictsIfChanged
// dedupes the no-ops. No-op if the device is unknown or already in the
// requested state.
//
// Português: Marca/desmarca um device como escondido. Um device escondido é
// excluído de toda consulta espacial (colisão, parent, snap) via
// candidates(); vínculos pai/filho e geometria ficam intactos. Usado por
// containers (IfElse) para tirar a branch inativa da colisão mantendo-a na
// árvore (ainda arrasta junto). Recomputa conflitos do device e de cada peer.
func (g *Graph) SetHidden(deviceID string, hidden bool) {
	node, ok := g.nodes[deviceID]
	if !ok || node.hidden == hidden {
		return
	}
	node.hidden = hidden

	g.recomputeAndNotifyConflicts(deviceID)
	for peerID := range g.nodes {
		if peerID == deviceID {
			continue
		}
		g.recomputeAndNotifyConflicts(peerID)
	}
}

// ConflictHighlights returns one entry per device currently in
// conflict — the graph's read model for any visual feedback layer.
//
// Each entry carries the offending device's own outer rect so the
// renderer can draw a highlight around the device the maker needs
// to move. When the conflict is a Straddle or PiercedOuter, the
// entry also carries the container's ID and inner rect, so the
// renderer can additionally draw the area the device was supposed
// to fit into. For pure Simple-Simple Overlap the container fields
// are empty.
//
// The method walks the already-cached conflict state — it does NOT
// re-run the geometric classification — so a render loop calling this
// every frame is cheap: iteration is proportional to the number of
// conflicting devices (usually 0 or 1).
//
// When a device has multiple conflicts, the entry uses the most
// severe Kind (Straddle > PiercedOuter > Overlap) and, for container-
// involving kinds, picks the container from that most-severe record.
//
// Português: Retorna uma entrada por device em conflito. DeviceRect
// é sempre o outer do próprio device. Pra Straddle/PiercedOuter,
// também vem o container e seu inner. Overlap deixa o container
// vazio. Lê do cache — barato o suficiente pra render loop todo frame.
func (g *Graph) ConflictHighlights() []ConflictHighlight {
	if len(g.nodes) == 0 {
		return nil
	}

	out := make([]ConflictHighlight, 0, 4)

	// Walk in registration order so the result is deterministic —
	// stable across runs for tests and logs.
	for _, id := range g.order {
		node, ok := g.nodes[id]
		if !ok {
			continue
		}
		if len(node.lastNotifiedConflicts) == 0 {
			continue
		}

		// Pick the most severe conflict on this device. For container-
		// involving kinds we also resolve the container from that same
		// record, so the two halves of the highlight always match.
		severe := node.lastNotifiedConflicts[0]
		for _, c := range node.lastNotifiedConflicts[1:] {
			if rankKind(c.Kind) > rankKind(severe.Kind) {
				severe = c
			}
		}

		h := ConflictHighlight{
			DeviceID:   id,
			DeviceRect: node.Outer,
			Kind:       severe.Kind,
		}

		// For container-kinds, resolve the counterpart as the
		// container and copy its inner rect. Guard against the
		// counterpart not being a Complex with an Inner — in that
		// case the conflict is informally a container kind but the
		// peer can't provide a useful rect; leave container fields
		// empty rather than faking it.
		if severe.Kind == ConflictStraddle || severe.Kind == ConflictPiercedOuter {
			if peer, ok := g.nodes[severe.With]; ok && peer.Kind == KindComplex && peer.Inner != nil {
				innerCopy := *peer.Inner
				h.ContainerID = peer.ID
				h.ContainerRect = &innerCopy
			}
		}

		out = append(out, h)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// rankKind gives an ordering to conflict severities so when a device
// has multiple conflicts we pick the strongest to report.
func rankKind(k ConflictKind) int {
	switch k {
	case ConflictStraddle:
		return 3
	case ConflictPiercedOuter:
		return 2
	case ConflictOverlap:
		return 1
	default:
		return 0
	}
}

// ChildrenOf returns the IDs of the direct children of a Complex device,
// in the order in which they were attached. Returns nil if the device
// is not Complex or not registered.
//
// Português: Retorna os filhos diretos de um Complex, na ordem de
// anexação.
func (g *Graph) ChildrenOf(deviceID string) []string {
	node, ok := g.nodes[deviceID]
	if !ok || node.Kind != KindComplex {
		return nil
	}
	result := make([]string, len(node.ChildrenIDs))
	copy(result, node.ChildrenIDs)
	return result
}

// Descendants returns the flat list of direct and indirect descendants
// of a Complex device, depth-first. Returns nil if the device is not
// Complex or not registered.
//
// Português: Retorna descendentes diretos e indiretos, em profundidade.
func (g *Graph) Descendants(deviceID string) []string {
	node, ok := g.nodes[deviceID]
	if !ok || node.Kind != KindComplex {
		return nil
	}
	return g.flattenDescendants(deviceID)
}

// ParentOf returns the ID of the current parent, or "" if the device is
// root or not registered.
//
// Português: Retorna o pai atual, ou "" se raiz ou não registrado.
func (g *Graph) ParentOf(deviceID string) string {
	node, ok := g.nodes[deviceID]
	if !ok {
		return ""
	}
	return node.ParentID
}

// ChildrenBounds returns the bounding box around all direct children
// of a Complex device, or nil when the device has no children or is not
// Complex. Used by container devices to show a visual resize floor.
//
// Português: Retorna a bounding box dos filhos diretos de um Complex,
// ou nil se não tem filhos. Usada pelos containers para overlay visual.
func (g *Graph) ChildrenBounds(deviceID string) *Rect {
	node, ok := g.nodes[deviceID]
	if !ok || node.Kind != KindComplex || len(node.ChildrenIDs) == 0 {
		return nil
	}
	var bounds Rect
	first := true
	for _, childID := range node.ChildrenIDs {
		child := g.nodes[childID]
		if child == nil {
			continue
		}
		if first {
			bounds = child.Outer
			first = false
		} else {
			bounds = bounds.Union(child.Outer)
		}
	}
	if first {
		return nil
	}
	return &bounds
}

// ParentInnerRect returns the parent's border 3, or nil if the device
// has no parent or the parent has no inner. Used by container devices
// that need to know the ceiling for their own resize limit.
//
// Português: Retorna a border 3 do pai, ou nil. Usada pelos filhos
// Complex para saber seu teto de resize.
func (g *Graph) ParentInnerRect(deviceID string) *Rect {
	node, ok := g.nodes[deviceID]
	if !ok || node.ParentID == "" {
		return nil
	}
	parent := g.nodes[node.ParentID]
	if parent == nil || parent.Inner == nil {
		return nil
	}
	c := *parent.Inner
	return &c
}

// Snapshot returns read-only views of all registered nodes, in
// registration order, with conflicts computed fresh. Used by the
// serializer at export time.
//
// Português: Retorna visões somente-leitura de todos os nós, com
// conflitos computados. Usada pelo serializer na exportação.
func (g *Graph) Snapshot() []NodeView {
	cands := g.candidates()
	views := make([]NodeView, 0, len(g.order))
	for _, id := range g.order {
		node, ok := g.nodes[id]
		if !ok {
			continue
		}
		view := NodeView{
			ID:          node.ID,
			Kind:        node.Kind,
			Outer:       node.Outer,
			ParentID:    node.ParentID,
			ChildrenIDs: append([]string(nil), node.ChildrenIDs...),
			Conflicts:   findConflicts(id, cands),
		}
		if node.Inner != nil {
			innerCopy := *node.Inner
			view.Inner = &innerCopy
		}
		views = append(views, view)
	}
	return views
}

// RefreshAll re-reads geometry for every registered device,
// re-checks conflicts (firing OnConflictsChanged on change), AND
// re-evaluates parent assignments for any device whose conflict set
// is clean. Firing OnParentChanged when parentage changes.
//
// Used by the Serializer in NotifyChange to keep the graph in sync
// with devices whose movements don't go through BeginDrag/UpdateDrag
// (e.g. Simple devices that call sceneNotify at drag end without
// touching the graph lifecycle directly). Call this sparingly — it
// is O(n^2) in the number of devices.
//
// Parentage is reassigned only when the device has NO current
// conflicts, matching the invariant that a conflicted device keeps
// its old parent until the user resolves the conflict.
//
// Português: Re-lê geometria, re-verifica conflitos e re-avalia pai
// para devices com conflict list limpa. Usado para sincronização
// passiva quando o device não passou por UpdateDrag.
func (g *Graph) RefreshAll() {
	for _, id := range g.order {
		g.refreshGeometry(id)
	}
	for _, id := range g.order {
		g.recomputeAndNotifyConflicts(id)
	}
	// Re-parent clean devices. Iterate a snapshot because the parent
	// links can change during the loop.
	ids := make([]string, len(g.order))
	copy(ids, g.order)
	for _, id := range ids {
		node := g.nodes[id]
		if node == nil {
			continue
		}
		if len(node.lastNotifiedConflicts) > 0 {
			continue // conflicted — keep old parent
		}
		newParentID := findParent(node.Outer, id, g.candidates())
		if newParentID == node.ParentID {
			continue
		}
		oldParent := node.ParentID
		if oldParent != "" {
			g.removeChild(oldParent, id)
		}
		node.ParentID = newParentID
		if newParentID != "" {
			g.appendChild(newParentID, id)
		}
		g.observer.OnParentChanged(id, oldParent, newParentID)
	}
}

// Has reports whether a device is registered.
//
// Português: Informa se um device está registrado.
func (g *Graph) Has(deviceID string) bool {
	_, ok := g.nodes[deviceID]
	return ok
}

// =====================================================================
//  Internal helpers
// =====================================================================

// candidates returns a fresh slice describing every registered node,
// suitable for feeding into findParent and findConflicts. Called from
// public methods whenever rules need the full graph state.
//
// Not cached: the slice is rebuilt on every call. This is cheap (a
// handful of field copies per node) and keeps the rule engine stateless.
func (g *Graph) candidates() []candidate {
	result := make([]candidate, 0, len(g.nodes))
	for _, node := range g.nodes {
		// Hidden nodes (e.g. the inactive IfElse branch) take part in no
		// spatial query — conflict detection, parent finding, or snap.
		if node.hidden {
			continue
		}
		c := candidate{
			ID:    node.ID,
			Kind:  node.Kind,
			Outer: node.Outer,
		}
		if node.Inner != nil {
			innerCopy := *node.Inner
			c.Inner = &innerCopy
		}
		result = append(result, c)
	}
	return result
}

// refreshGeometry re-reads outer and inner from the device ref and
// stores them on the node. Called whenever the graph knows geometry
// might have changed (UpdateDrag, after MoveBy on a descendant,
// EndResize).
func (g *Graph) refreshGeometry(deviceID string) {
	node, ok := g.nodes[deviceID]
	if !ok {
		return
	}
	ref, ok := g.refs[deviceID]
	if !ok {
		return
	}
	node.Outer = ref.OuterRect()
	if node.Kind == KindComplex {
		node.Inner = ref.InnerRect()
	} else {
		node.Inner = nil
	}
}

// computeParent returns the ID that findParent would assign, given
// the current candidates and the node's current outer rectangle.
func (g *Graph) computeParent(deviceID string) string {
	node, ok := g.nodes[deviceID]
	if !ok {
		return ""
	}
	return findParent(node.Outer, deviceID, g.candidates())
}

// flattenDescendants performs a depth-first walk starting at deviceID
// and returns the flat list of descendants (direct children first, then
// their children, etc.). The starting device itself is NOT included.
func (g *Graph) flattenDescendants(deviceID string) []string {
	node, ok := g.nodes[deviceID]
	if !ok {
		return nil
	}
	result := make([]string, 0)
	for _, childID := range node.ChildrenIDs {
		result = append(result, childID)
		result = append(result, g.flattenDescendants(childID)...)
	}
	return result
}

// appendChild adds childID to parentID's ChildrenIDs if not already
// present. Invariant: called only when node.ParentID has been set to
// parentID.
func (g *Graph) appendChild(parentID, childID string) {
	parent := g.nodes[parentID]
	if parent == nil {
		return
	}
	for _, id := range parent.ChildrenIDs {
		if id == childID {
			return
		}
	}
	parent.ChildrenIDs = append(parent.ChildrenIDs, childID)
}

// removeChild removes childID from parentID's ChildrenIDs if present.
func (g *Graph) removeChild(parentID, childID string) {
	parent := g.nodes[parentID]
	if parent == nil {
		return
	}
	for i, id := range parent.ChildrenIDs {
		if id == childID {
			parent.ChildrenIDs = append(parent.ChildrenIDs[:i], parent.ChildrenIDs[i+1:]...)
			return
		}
	}
}

// recomputeAndNotifyConflicts computes the current conflict set for
// deviceID and fires the observer if it differs from the last reported
// set.
func (g *Graph) recomputeAndNotifyConflicts(deviceID string) {
	conflicts := findConflicts(deviceID, g.candidates())
	g.notifyConflictsIfChanged(deviceID, conflicts)
}

// notifyConflictsIfChanged fires OnConflictsChanged if the given set
// differs from lastNotifiedConflicts. Stores the new set as the baseline
// for future comparisons.
func (g *Graph) notifyConflictsIfChanged(deviceID string, conflicts []Conflict) {
	node := g.nodes[deviceID]
	if node == nil {
		return
	}
	if conflictsEqual(node.lastNotifiedConflicts, conflicts) {
		return
	}
	node.lastNotifiedConflicts = conflicts
	g.observer.OnConflictsChanged(deviceID, conflicts)
}
