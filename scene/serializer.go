// scene/serializer.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scene

// serializer.go
//
// English:
//
//	The Serializer is the single API surface the workspace and devices
//	use to interact with the scene system. Internally it owns a
//	*scenegraph.Graph (the fact-of-record) and a wire.Manager (for
//	connector info in JSON).
//
//	Responsibilities:
//	  - Register / Unregister devices (forwarded to the graph).
//	  - Drag and resize lifecycle events (forwarded to the graph).
//	  - Queries about the graph (ChildrenOf, Descendants, ParentInner,
//	    ChildrenBounds, FindParent) — thin wrappers around the graph.
//	  - JSON export of the current stage state.
//	  - Download of the JSON as a file (browser Blob + anchor click).
//
//	The Serializer translates between scene.Rect (the public stage API)
//	and scenegraph.Rect (the engine's internal type). Devices see only
//	scene.Rect.
//
// Português:
//
//	O Serializer é a superfície única de API que o workspace e os devices
//	usam para interagir com o sistema de cena. Internamente mantém um
//	*scenegraph.Graph (fonte de verdade) e um wire.Manager.

import (
	"encoding/json"
	"log"
	"sort"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/scenegraph"
	"github.com/helmutkemper/iotmakerio/wire"
)

// Serializer manages scene export and forwards lifecycle events to the
// scenegraph.
type Serializer struct {
	graph   *scenegraph.Graph
	wireMgr *wire.Manager

	// refs keeps the DeviceRef adapters alive so that the graph can
	// call back into them during EndDrag (MoveBy). Keyed by device ID.
	refs map[string]*sceneDeviceRef

	// lastJSON holds the most recently generated JSON string.
	lastJSON string

	// stage is the workspace identity this serializer belongs to
	// ("backend" / "frontend"). It is stamped onto every DeviceJSON.Stage
	// at export time so the combined scene document records which stage
	// each device lives on, letting importScene recreate a device only on
	// the matching workspace. Empty for legacy / un-scoped serializers,
	// which keeps old scenes (no stage tag) loading unchanged.
	//
	// Português: identidade do workspace deste serializer ("backend" /
	// "frontend"). Carimbada em cada DeviceJSON.Stage no export, pra o
	// import recriar o device só no workspace certo. Vazio = legado.
	stage string

	// OnExport is called every time the JSON is regenerated.
	OnExport func(jsonStr string)

	// cameraFunc returns (offsetX, offsetY, zoom) at call time.
	cameraFunc func() (float64, float64, float64)

	// canvasSizeFunc returns (width, height) at call time.
	canvasSizeFunc func() (int, int)

	// targetFunc returns the selected hardware-target id at export time — the
	// board the maker picked in the dropdown, held by the workspace. Injected by
	// the workspace, mirroring cameraFunc/canvasSizeFunc, so the Serializer stays
	// below the workspace in the import graph and never reaches up for the value.
	// Nil, or an empty return, writes no target, which the codegen treats as the
	// default (Arduino UNO).
	//
	// Português: Retorna o id do target de hardware selecionado no export — a
	// placa que o maker escolheu no dropdown, guardada no workspace. Injetada
	// pelo workspace, espelhando cameraFunc/canvasSizeFunc, então o Serializer
	// fica abaixo do workspace no grafo de import e nunca busca o valor pra cima.
	// Nil, ou retorno vazio, não escreve target — o codegen trata como default
	// (Arduino UNO).
	targetFunc func() string

	// bufferSizeFunc returns the string-buffer override, in bytes, at export
	// time — the value from the selected board's advanced panel, held by the
	// workspace. Injected like targetFunc, so the Serializer never reaches up.
	// Zero writes no override (the codegen keeps the board's default).
	//
	// Português: Retorna o override do buffer, em bytes, no export — o valor do
	// painel avançado da placa selecionada, guardado no workspace. Injetada como
	// a targetFunc. Zero não escreve override (o codegen mantém o default).
	bufferSizeFunc func() int

	// observer is installed on the graph and bridges graph events to
	// the Serializer's OnExport callback and to external consumers.
	observer *graphObserver

	// onParentChanged is an optional external hook fired whenever a
	// device's containment parent changes in the scenegraph. Signatures:
	// (deviceID, oldParentID, newParentID) where "" means root level.
	//
	// The Serializer sits below the devices package in the import graph,
	// so it cannot call BackendZRegistry directly. The workspace sets
	// this callback at bootstrap to reorder the z-registry so children
	// always render in front of their container parents.
	//
	// Português: Hook externo disparado quando a parentage de um device
	// muda. Usado pelo workspace pra reordenar o registry de z-index,
	// já que scene não pode importar devices.
	onParentChanged func(deviceID, oldParentID, newParentID string)

	// onSceneChanged is an optional external hook fired whenever the
	// scene is modified — drag end, resize end, wire connect, device
	// register / unregister, import. A single "something happened"
	// signal that consumers use to invalidate caches derived from the
	// previous scene state.
	//
	// The workspace uses this to drop codegen diagnostic highlights
	// the moment the user starts editing: the diagnostics were produced
	// for the old scene and become misleading once geometry changes.
	//
	// Português: Hook externo disparado a cada mudança de cena. Usado
	// pelo workspace pra descartar highlights de codegen obsoletos.
	onSceneChanged func()

	// codegenDiagnosticDevices is the set of device IDs that should be
	// highlighted because of errors reported by the server's codegen
	// pipeline — separate from geometric conflicts which the scenegraph
	// maintains on its own. The render callback draws a red border
	// around each of these, reusing the same visual vocabulary as
	// geometric conflicts so the maker sees both classes of error the
	// same way.
	//
	// Lifetime is controlled by the workspace:
	//   - Set when a codegen run returns diagnostics.
	//   - Cleared on the first scene change (onSceneChanged fires).
	//   - Never written outside SetCodegenDiagnosticDevices.
	//
	// Português: IDs de devices marcados por erros de codegen (não
	// conflitos geométricos). Preenchido quando o codegen retorna
	// erros, zerado na primeira mudança de cena.
	codegenDiagnosticDevices map[string]bool
}

// NewSerializer creates a fresh Serializer with an empty graph.
//
// Português: Cria um Serializer vazio com um grafo limpo.
func NewSerializer() *Serializer {
	s := &Serializer{
		graph: scenegraph.NewGraph(),
		refs:  make(map[string]*sceneDeviceRef),
	}
	s.observer = &graphObserver{serializer: s}
	s.graph.SetObserver(s.observer)
	return s
}

// =====================================================================
//  Injections
// =====================================================================

// SetStage records the workspace identity ("backend" / "frontend") this
// serializer belongs to. The value is stamped onto every exported
// DeviceJSON.Stage so the combined scene document is self-describing and
// importScene can route each device to the correct stage. Called once by
// the workspace right after NewSerializer, before any export.
//
// Português: Define a identidade do workspace ("backend" / "frontend").
// Carimbada em cada DeviceJSON.Stage no export. Chamada uma vez pelo
// workspace logo após NewSerializer.
func (s *Serializer) SetStage(stage string) {
	s.stage = stage
}

// SetWireManager injects the wire manager for connector and wire info.
func (s *Serializer) SetWireManager(mgr *wire.Manager) {
	s.wireMgr = mgr
}

// SetCameraFunc sets the function used to read camera state at export.
func (s *Serializer) SetCameraFunc(fn func() (float64, float64, float64)) {
	s.cameraFunc = fn
}

// SetCanvasSizeFunc sets the function used to read canvas dimensions.
func (s *Serializer) SetCanvasSizeFunc(fn func() (int, int)) {
	s.canvasSizeFunc = fn
}

// SetTargetFunc installs the callback that returns the selected hardware-target
// id at export time (see targetFunc). The workspace wires this to its held
// selectedTarget, so buildSceneJSON stamps Metadata.Target on every export.
//
// Português: Instala o callback que retorna o id do target selecionado no
// export (ver targetFunc). O workspace liga isto ao seu selectedTarget, então o
// buildSceneJSON carimba Metadata.Target em todo export.
func (s *Serializer) SetTargetFunc(fn func() string) {
	s.targetFunc = fn
}

// SetBufferSizeFunc installs the callback returning the string-buffer override
// (bytes) at export time (see bufferSizeFunc). The workspace wires it to its
// held override, so buildSceneJSON stamps Metadata.StringBufferSize.
//
// Português: Instala o callback que retorna o override do buffer (bytes) no
// export (ver bufferSizeFunc). O workspace o liga ao override guardado.
func (s *Serializer) SetBufferSizeFunc(fn func() int) {
	s.bufferSizeFunc = fn
}

// SetOnConflictsChanged installs a callback that fires whenever a
// device's conflict set changes. The sprite layer uses this to paint
// red borders. Passing nil clears the callback.
//
// Português: Instala callback para mudanças de conflito. A camada de
// sprite usa para pintar bordas vermelhas.
func (s *Serializer) SetOnConflictsChanged(fn func(deviceID string, conflicts []Conflict)) {
	s.observer.onConflictsChanged = fn
}

// SetOnParentChanged installs a callback that fires whenever a device's
// parent container changes. Used by the workspace to reorder the z-index
// registry so children always render in front of their parents.
//
// The callback receives (deviceID, oldParentID, newParentID); empty
// string means root level.
//
// Português: Instala callback para mudanças de parentage. Usado pelo
// workspace para reordenar z-index — filhos sempre na frente do pai.
func (s *Serializer) SetOnParentChanged(fn func(deviceID, oldParentID, newParentID string)) {
	s.onParentChanged = fn
}

// SetOnSceneChanged installs a callback that fires on any scene
// modification (end drag, end resize, wire connect/disconnect, device
// register/unregister, scene import). Meant as an invalidation signal
// for state derived from the scene outside the scenegraph — e.g.
// codegen diagnostics which become stale once geometry moves.
//
// Passing nil clears the callback.
//
// Português: Instala callback para mudanças de cena. Serve como
// invalidação pra estados derivados (ex.: diagnósticos de codegen).
func (s *Serializer) SetOnSceneChanged(fn func()) {
	s.onSceneChanged = fn
}

// SetCodegenDiagnosticDevices replaces the set of devices highlighted
// as having a codegen-pipeline error. Passing nil or an empty slice
// clears the set. The render callback picks these up through
// ConflictHighlights and draws the same red border used for geometric
// conflicts — one visual vocabulary for both classes of error.
//
// Typical usage:
//
//	w.SceneMgr.SetCodegenDiagnosticDevices(ids)        // show errors
//	// ... user drags a device ...
//	w.SceneMgr.SetCodegenDiagnosticDevices(nil)        // wipe on change
//
// Português: Define o conjunto de devices com erro de codegen. Passe
// nil para limpar. Render callback pinta com a mesma borda vermelha
// dos conflitos geométricos.
func (s *Serializer) SetCodegenDiagnosticDevices(ids []string) {
	if len(ids) == 0 {
		s.codegenDiagnosticDevices = nil
		return
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			set[id] = true
		}
	}
	s.codegenDiagnosticDevices = set
}

// =====================================================================
//  Device registration (forwarded to the graph)
// =====================================================================

// Register adds a device to the scene. The Serializer wraps it in a
// DeviceRef adapter and hands the adapter to the graph.
func (s *Serializer) Register(dev SceneDevice) {
	id := dev.GetID()
	if _, exists := s.refs[id]; exists {
		return
	}
	ref := &sceneDeviceRef{dev: dev}
	s.refs[id] = ref
	s.graph.Register(ref)
}

// Unregister removes a device from the scene. Graph removes it; the
// Serializer drops the adapter and any conflict subscription.
func (s *Serializer) Unregister(deviceID string) {
	s.graph.Unregister(deviceID)
	delete(s.refs, deviceID)
}

// FindDevice returns the registered SceneDevice matching the given ID,
// or nil. Used by the live communication client to dispatch incoming
// messages to the right device.
func (s *Serializer) FindDevice(deviceID string) SceneDevice {
	ref, ok := s.refs[deviceID]
	if !ok {
		return nil
	}
	return ref.dev
}

// =====================================================================
//  Drag and resize lifecycle (forwarded to the graph)
// =====================================================================

// BeginDrag — see scenegraph.Graph.BeginDrag.
func (s *Serializer) BeginDrag(deviceID string) { s.graph.BeginDrag(deviceID) }

// UpdateDrag — see scenegraph.Graph.UpdateDrag.
func (s *Serializer) UpdateDrag(deviceID string) { s.graph.UpdateDrag(deviceID) }

// EndDrag — see scenegraph.Graph.EndDrag.
func (s *Serializer) EndDrag(deviceID string, dx, dy float64) {
	s.graph.EndDrag(deviceID, dx, dy)
}

// BeginResize — see scenegraph.Graph.BeginResize.
func (s *Serializer) BeginResize(deviceID string) { s.graph.BeginResize(deviceID) }

// EndResize — see scenegraph.Graph.EndResize.
func (s *Serializer) EndResize(deviceID string) { s.graph.EndResize(deviceID) }

// SetHidden — see scenegraph.Graph.SetHidden. Used by container devices
// (IfElse) to take the inactive branch's devices out of collision while
// keeping them in the graph so they still drag with the container.
func (s *Serializer) SetHidden(deviceID string, hidden bool) {
	s.graph.SetHidden(deviceID, hidden)
}

// =====================================================================
//  Queries (forwarded to the graph, converting Rect types)
// =====================================================================

// ChildrenOf returns direct children of a Complex device.
func (s *Serializer) ChildrenOf(deviceID string) []string {
	return s.graph.ChildrenOf(deviceID)
}

// Descendants returns direct and indirect descendants, depth-first.
func (s *Serializer) Descendants(deviceID string) []string {
	return s.graph.Descendants(deviceID)
}

// ParentOf returns the parent ID, or "" if root.
func (s *Serializer) ParentOf(deviceID string) string {
	return s.graph.ParentOf(deviceID)
}

// ChildrenBounds returns the union of the children's border 1, as a
// scene.Rect. Returns nil if the device has no children or is not
// Complex.
func (s *Serializer) ChildrenBounds(deviceID string) *Rect {
	r := s.graph.ChildrenBounds(deviceID)
	if r == nil {
		return nil
	}
	return &Rect{X: r.X, Y: r.Y, Width: r.W, Height: r.H}
}

// ParentInnerBBox returns the parent's border 3 as a scene.Rect, or
// nil if the device is at root.
func (s *Serializer) ParentInnerBBox(deviceID string) *Rect {
	r := s.graph.ParentInnerRect(deviceID)
	if r == nil {
		return nil
	}
	return &Rect{X: r.X, Y: r.Y, Width: r.W, Height: r.H}
}

// FindConflicts returns the current conflicts for a device.
func (s *Serializer) FindConflicts(deviceID string) []Conflict {
	gc := s.graph.FindConflicts(deviceID)
	return fromGraphConflicts(gc)
}

// MoveDevicesByID is retained as a convenience: some import paths may
// want to move a set of devices by a delta without going through a
// full drag cycle. The underlying MoveBy call is forwarded to each
// device's adapter.
//
// Português: Move um conjunto de devices por um delta, sem drag.
func (s *Serializer) MoveDevicesByID(ids []string, dx, dy float64) {
	for _, id := range ids {
		ref, ok := s.refs[id]
		if !ok {
			continue
		}
		ref.MoveBy(dx, dy)
	}
}

// =====================================================================
//  JSON export
// =====================================================================

// Export generates the full scene JSON string.
func (s *Serializer) Export() string {
	sc := s.buildSceneJSON()
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		log.Printf("[SCENE] JSON marshal error: %v", err)
		return "{}"
	}
	return string(data)
}

// ExportBytes generates the scene JSON as bytes.
func (s *Serializer) ExportBytes() []byte {
	sc := s.buildSceneJSON()
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		log.Printf("[SCENE] JSON marshal error: %v", err)
		return []byte("{}")
	}
	return data
}

// NotifyChange refreshes the graph's geometry view (so conflicts
// reflect the latest device positions even when the device didn't go
// through BeginDrag/UpdateDrag), regenerates the JSON, and calls
// OnExport. Should be called whenever any device moves, resizes, or a
// wire changes.
//
// Português: Sincroniza o grafo com a geometria atual dos devices,
// regenera o JSON, dispara OnExport.
func (s *Serializer) NotifyChange() {
	s.graph.RefreshAll()
	s.lastJSON = s.Export()
	if s.OnExport != nil {
		s.OnExport(s.lastJSON)
	}
	if s.onSceneChanged != nil {
		s.onSceneChanged()
	}
}

// GetLastJSON returns the most recently generated JSON.
func (s *Serializer) GetLastJSON() string { return s.lastJSON }

// DownloadJSON triggers a browser download of the current scene JSON.
func (s *Serializer) DownloadJSON() {
	jsonStr := s.Export()
	doc := js.Global().Get("document")
	blob := js.Global().Get("Blob").New(
		js.Global().Get("Array").New(js.ValueOf(jsonStr)),
		map[string]interface{}{"type": "application/json"},
	)
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	a := doc.Call("createElement", "a")
	a.Set("href", url)
	a.Set("download", "scene.json")
	doc.Get("body").Call("appendChild", a)
	a.Call("click")
	doc.Get("body").Call("removeChild", a)
	js.Global().Get("URL").Call("revokeObjectURL", url)
	log.Printf("[SCENE] JSON downloaded (%d bytes)", len(jsonStr))
}

// =====================================================================
//  Stage import helpers
// =====================================================================

// RemoveAll removes every device currently registered. Each device
// that implements Remove() will have its visual elements destroyed,
// its connectors and wires unregistered, and its scene entry removed.
//
// Português: Remove todos os devices registrados. Devices com Remove()
// têm seus elementos visuais destruídos.
func (s *Serializer) RemoveAll() {
	// Iterate a snapshot because Remove() calls Unregister() which
	// mutates s.refs.
	ids := make([]string, 0, len(s.refs))
	for id := range s.refs {
		ids = append(ids, id)
	}
	for _, id := range ids {
		ref, ok := s.refs[id]
		if !ok {
			continue
		}
		if rem, ok := ref.dev.(interface{ Remove() }); ok {
			rem.Remove()
		}
	}
	// Safety: ensure the serializer's maps are empty even if some
	// devices did not go through Remove().
	s.refs = make(map[string]*sceneDeviceRef)
	s.graph = scenegraph.NewGraph()
	s.observer = &graphObserver{serializer: s}
	s.graph.SetObserver(s.observer)
	log.Printf("[SCENE] RemoveAll: stage cleared")
}

// LastDeviceID returns the ID of the most recently registered device,
// or "" if none. Used by import to pair a factory call with the ID
// that was assigned inside the device's Init.
func (s *Serializer) LastDeviceID() string {
	// refs is a map without order; use the graph's registration order.
	snap := s.graph.Snapshot()
	if len(snap) == 0 {
		return ""
	}
	return snap[len(snap)-1].ID
}

// LastDevice returns the most recently registered SceneDevice, or nil.
func (s *Serializer) LastDevice() SceneDevice {
	id := s.LastDeviceID()
	if id == "" {
		return nil
	}
	ref, ok := s.refs[id]
	if !ok {
		return nil
	}
	return ref.dev
}

// GetWorldBounds returns the tight bounding box around every device on
// the stage, or nil if the stage is empty. Used by image export to
// FitAll before capturing.
func (s *Serializer) GetWorldBounds() *Rect {
	snap := s.graph.Snapshot()
	if len(snap) == 0 {
		return nil
	}
	first := snap[0].Outer
	minX, minY := first.X, first.Y
	maxX, maxY := first.X+first.W, first.Y+first.H
	for _, nv := range snap[1:] {
		o := nv.Outer
		if o.X < minX {
			minX = o.X
		}
		if o.Y < minY {
			minY = o.Y
		}
		if o.X+o.W > maxX {
			maxX = o.X + o.W
		}
		if o.Y+o.H > maxY {
			maxY = o.Y + o.H
		}
	}
	return &Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

// =====================================================================
//  Conflict type exposed to consumers
// =====================================================================

// Conflict is the scene-level view of a spatial conflict, mirroring
// scenegraph.Conflict but using the string form of the kind for easier
// consumption by non-Go callers (JS observers via syscall/js).
//
// Português: Visão de conflito no nível scene, usando a forma string
// do kind para facilitar o consumo pelo JS.
type Conflict struct {
	With string
	Kind string // "overlap", "straddle", "pierced_outer"
}

// ConflictHighlights returns one highlight per device currently in
// conflict. This is the adapter the workspace (or any future
// renderer) calls every frame to find out what to paint.
//
// The scenegraph package owns the logic; this method only converts
// scenegraph.Rect → scene.Rect and ConflictKind → its string form, so
// callers never import scenegraph directly.
//
// Português: Retorna os highlights por device em conflito.
// Adaptador fino sobre o scenegraph — só converte tipos na fronteira.
func (s *Serializer) ConflictHighlights() []ConflictHighlight {
	raw := s.graph.ConflictHighlights()

	// Start with an estimate sized for both sources so append below
	// rarely reallocates.
	out := make([]ConflictHighlight, 0, len(raw)+len(s.codegenDiagnosticDevices))

	// Pass 1 — geometric conflicts from the scenegraph. Also remember
	// which devices are already highlighted so a device that happens
	// to have BOTH a geometric conflict and a codegen diagnostic is
	// not double-drawn.
	alreadyHighlighted := make(map[string]bool, len(raw))
	for _, h := range raw {
		item := ConflictHighlight{
			DeviceID: h.DeviceID,
			DeviceRect: Rect{
				X:      h.DeviceRect.X,
				Y:      h.DeviceRect.Y,
				Width:  h.DeviceRect.W,
				Height: h.DeviceRect.H,
			},
			ContainerID: h.ContainerID,
			Kind:        h.Kind.String(),
		}
		if h.ContainerRect != nil {
			item.ContainerRect = &Rect{
				X:      h.ContainerRect.X,
				Y:      h.ContainerRect.Y,
				Width:  h.ContainerRect.W,
				Height: h.ContainerRect.H,
			}
		}
		out = append(out, item)
		alreadyHighlighted[h.DeviceID] = true
	}

	// Pass 2 — codegen diagnostic devices. No container component
	// because these errors are not geometric; we just want the red
	// border around the device the user needs to look at. Skip any
	// that the geometric pass already covered.
	if len(s.codegenDiagnosticDevices) > 0 {
		// Iterate in a deterministic order (sorted by ID) so the
		// resulting slice is stable across calls.
		ids := make([]string, 0, len(s.codegenDiagnosticDevices))
		for id := range s.codegenDiagnosticDevices {
			if !alreadyHighlighted[id] {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			dev := s.findDevice(id)
			if dev == nil {
				continue
			}
			bbox := dev.GetOuterBBox()
			out = append(out, ConflictHighlight{
				DeviceID: id,
				DeviceRect: Rect{
					X:      bbox.X,
					Y:      bbox.Y,
					Width:  bbox.Width,
					Height: bbox.Height,
				},
				Kind: "codegen_error",
			})
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// findDevice is the internal lookup used by ConflictHighlights to
// resolve an ID to a SceneDevice for reading its current bbox. Uses
// the same refs map the public FindDevice relies on.
//
// Português: Busca interna por ID para ler a geometria atual.
func (s *Serializer) findDevice(id string) SceneDevice {
	if ref, ok := s.refs[id]; ok && ref != nil {
		return ref.dev
	}
	return nil
}

// fromGraphConflicts converts a slice of scenegraph.Conflict to the
// scene.Conflict form.
func fromGraphConflicts(cs []scenegraph.Conflict) []Conflict {
	result := make([]Conflict, 0, len(cs))
	for _, c := range cs {
		result = append(result, Conflict{With: c.With, Kind: c.Kind.String()})
	}
	return result
}

// =====================================================================
//  Internal: JSON building from graph snapshot
// =====================================================================

func (s *Serializer) buildSceneJSON() SceneJSON {
	sc := SceneJSON{
		Version: "1.0",
		Devices: make([]DeviceJSON, 0),
		Wires:   make([]WireJSON, 0),
	}

	// Metadata
	sc.Metadata.Density = 1
	if s.targetFunc != nil {
		// The hardware target the maker picked (empty when none). The C codegen
		// resolves it to a type profile + string-buffer size; empty is the
		// Arduino UNO default.
		sc.Metadata.Target = s.targetFunc()
	}
	if s.bufferSizeFunc != nil {
		// The maker's string-buffer override, in bytes; 0 means none, and the
		// codegen keeps the board's default.
		sc.Metadata.StringBufferSize = s.bufferSizeFunc()
	}
	if s.canvasSizeFunc != nil {
		sc.Metadata.CanvasWidth, sc.Metadata.CanvasHeight = s.canvasSizeFunc()
	}
	if s.cameraFunc != nil {
		ox, oy, z := s.cameraFunc()
		sc.Metadata.Camera = CameraJSON{OffsetX: ox, OffsetY: oy, Zoom: z}
	} else {
		sc.Metadata.Camera = CameraJSON{Zoom: 1}
	}

	// Per-stage camera: the combined document carries each stage's camera
	// keyed by stage name so every workspace restores its OWN viewport on
	// import, instead of inheriting the other stage's zoom/offset (which is
	// what made a frontend dashboard load with the backend's zoom). The legacy
	// single Camera above stays populated for back-compat; importScene prefers
	// Cameras[stage] when present and falls back to Camera for old scenes.
	if s.stage != "" {
		sc.Metadata.Cameras = map[string]CameraJSON{s.stage: sc.Metadata.Camera}
	}

	// Devices — walk the graph snapshot, translate each view.
	//
	// In the same pass, collect user-variable declarations (Path A): every
	// device implementing VariableDeclarer contributes a {name, type} entry to
	// the scene's top-level "variables" array, deduplicated by name (a Get and a
	// Set that name the same variable declare it once). The codegen pipeline
	// reads this array to emit one zero-initialised declaration per distinct
	// variable. Order follows the snapshot for determinism.
	//
	// Português: Na mesma passada, coleta as declarações de variáveis (Path A):
	// cada device que implementa VariableDeclarer contribui {name, type} para o
	// array "variables" no topo da cena, deduplicado por nome (um Get e um Set
	// que nomeiam a mesma variável a declaram uma vez só).
	varSeen := make(map[string]bool)
	for _, nv := range s.graph.Snapshot() {
		sc.Devices = append(sc.Devices, s.buildDeviceJSON(nv))

		ref, ok := s.refs[nv.ID]
		if !ok {
			continue
		}
		vd, ok := ref.dev.(VariableDeclarer)
		if !ok {
			continue
		}
		name, typ := vd.GetVariableDecl()
		if name == "" || varSeen[name] {
			continue
		}
		varSeen[name] = true
		sc.Variables = append(sc.Variables, VariableJSON{Name: name, Type: typ})
	}

	// Wires
	if s.wireMgr != nil {
		sc.Wires = s.buildWiresJSON()
	}

	return sc
}

func (s *Serializer) buildDeviceJSON(nv scenegraph.NodeView) DeviceJSON {
	ref, ok := s.refs[nv.ID]
	if !ok {
		// Defensive: the graph has a node for which we lost the ref.
		// Produce a minimal entry and log.
		log.Printf("[SCENE] buildDeviceJSON: no ref for %q", nv.ID)
		return DeviceJSON{ID: nv.ID, Kind: nv.Kind.String(), Stage: s.stage}
	}
	dev := ref.dev

	dj := DeviceJSON{
		ID:    nv.ID,
		Type:  dev.GetDeviceType(),
		Kind:  nv.Kind.String(),
		Stage: s.stage,
		Position: PointJSON{
			X: nv.Outer.X,
			Y: nv.Outer.Y,
		},
		Size: SizeJSON{
			Width:  nv.Outer.W,
			Height: nv.Outer.H,
		},
		OuterBBox: RectJSON{
			X:      nv.Outer.X,
			Y:      nv.Outer.Y,
			Width:  nv.Outer.W,
			Height: nv.Outer.H,
		},
		Connectors: make([]ConnectorJSON, 0),
		Containment: ContainmentJSON{
			IsContainer: nv.Kind == scenegraph.KindComplex,
			Children:    append([]string(nil), nv.ChildrenIDs...),
			Parent:      nv.ParentID,
		},
	}

	if nv.Inner != nil {
		dj.InnerBBox = &RectJSON{
			X:      nv.Inner.X,
			Y:      nv.Inner.Y,
			Width:  nv.Inner.W,
			Height: nv.Inner.H,
		}
	}

	// Frontend (dashboard) position for dual devices. Position above holds the
	// node this serializer's stage owns (the backend node for a backend-stage
	// device); a dual device ALSO has an independent dashboard node whose
	// coordinates the scenegraph snapshot does not carry here. Capture it via
	// the optional GetFrontendPosition so importScene can place the dashboard
	// node where the maker left it. Non-dual devices do not implement it and
	// are skipped, leaving FrontendPosition nil (omitted from the JSON).
	if fp, ok := dev.(interface {
		GetFrontendPosition() (float64, float64)
	}); ok {
		fx, fy := fp.GetFrontendPosition()
		dj.FrontendPosition = &PointJSON{X: fx, Y: fy}
	}

	// Containment status — derived from graph state:
	//   len(Conflicts)>0 → "error"   (geometric violation; blocks codegen)
	//   ParentID != ""  → "contained" (has a container parent; may ALSO be
	//                                   a container itself — isContainer flag
	//                                   carries that orthogonal fact)
	//   Kind == Complex → "container"
	//   else            → "free"
	//
	// Note: "container" is the default for root-level Complex devices.
	// Nested Complex devices (a Loop inside another Loop) are "contained"
	// — they live inside their parent's scope. The isContainer flag still
	// tells downstream consumers (codegen) that the device opens a new
	// scope of its own.
	switch {
	case len(nv.Conflicts) > 0:
		dj.Containment.Status = "error"
	case nv.ParentID != "":
		dj.Containment.Status = "contained"
	case nv.Kind == scenegraph.KindComplex:
		dj.Containment.Status = "container"
	default:
		dj.Containment.Status = "free"
	}

	// Conflicts list (mirrored into the JSON for external consumers).
	if len(nv.Conflicts) > 0 {
		conflictList := make([]ConflictJSON, 0, len(nv.Conflicts))
		for _, c := range nv.Conflicts {
			conflictList = append(conflictList, ConflictJSON{
				With: c.With,
				Kind: c.Kind.String(),
			})
		}
		dj.Containment.Conflicts = conflictList
	}

	// Label — optional.
	if lbl, ok := dev.(Labeled); ok {
		dj.Label = lbl.GetLabel()
	}

	// Properties — optional.
	if pp, ok := dev.(Propertied); ok {
		dj.Properties = pp.GetProperties()
	}

	// Connectors — read from wire manager.
	if s.wireMgr != nil {
		dj.Connectors = s.buildConnectorsJSON(nv.ID)
	}

	return dj
}

func (s *Serializer) buildConnectorsJSON(elementID string) []ConnectorJSON {
	if s.wireMgr == nil {
		return nil
	}
	connectors := s.wireMgr.GetConnectorsForElement(elementID)
	result := make([]ConnectorJSON, 0, len(connectors))

	for _, conn := range connectors {
		var px, py float64
		if conn.PositionFunc != nil {
			px, py = conn.PositionFunc()
		}
		dataType := ""
		if len(conn.AllowedTypes) > 0 {
			dataType = conn.AllowedTypes[0]
		}
		cj := ConnectorJSON{
			Port:               conn.ID.PortName,
			DataType:           dataType,
			IsOutput:           conn.IsOutput,
			AcceptNotConnected: conn.AcceptNotConnected,
			Position:           PointJSON{X: px, Y: py},
			Connections:        make([]ConnectionRefJSON, 0),
		}
		connID := wire.ConnectorID{ElementID: elementID, PortName: conn.ID.PortName}
		for _, w := range s.wireMgr.GetWiresForConnector(connID) {
			var target wire.ConnectorID
			if w.From == connID {
				target = w.To
			} else {
				target = w.From
			}
			cj.Connections = append(cj.Connections, ConnectionRefJSON{
				WireID:       w.ID,
				TargetDevice: target.ElementID,
				TargetPort:   target.PortName,
			})
		}
		result = append(result, cj)
	}
	return result
}

func (s *Serializer) buildWiresJSON() []WireJSON {
	if s.wireMgr == nil {
		return nil
	}
	all := s.wireMgr.GetAllWires()
	result := make([]WireJSON, 0, len(all))
	for _, w := range all {
		result = append(result, WireJSON{
			ID:       w.ID,
			From:     EndpointJSON{Device: w.From.ElementID, Port: w.From.PortName},
			To:       EndpointJSON{Device: w.To.ElementID, Port: w.To.PortName},
			DataType: w.DataType,
		})
	}
	return result
}

// =====================================================================
//  DeviceRef adapter
// =====================================================================

// sceneDeviceRef adapts a SceneDevice to scenegraph.DeviceRef. It lives
// in this file because it bridges the scene.Rect / scenegraph.Rect
// boundary.
//
// Português: Adapta SceneDevice para scenegraph.DeviceRef. Faz a ponte
// entre scene.Rect e scenegraph.Rect.
type sceneDeviceRef struct {
	dev SceneDevice
}

func (r *sceneDeviceRef) ID() string { return r.dev.GetID() }

// Kind returns the device's explicit Kind if it implements Kinded;
// otherwise it defaults to KindSimple. This lets non-container
// devices (constants, math, logic, frontend widgets) stay in the
// project without implementing Kinded — they are all Simple anyway.
func (r *sceneDeviceRef) Kind() scenegraph.Kind {
	if k, ok := r.dev.(Kinded); ok {
		return k.GetKind()
	}
	return scenegraph.KindSimple
}

func (r *sceneDeviceRef) OuterRect() scenegraph.Rect {
	o := r.dev.GetOuterBBox()
	return scenegraph.Rect{X: o.X, Y: o.Y, W: o.Width, H: o.Height}
}

func (r *sceneDeviceRef) InnerRect() *scenegraph.Rect {
	i := r.dev.GetInnerBBox()
	if i == nil {
		return nil
	}
	return &scenegraph.Rect{X: i.X, Y: i.Y, W: i.Width, H: i.Height}
}

func (r *sceneDeviceRef) MoveBy(dx, dy float64) {
	if m, ok := r.dev.(Movable); ok {
		m.MoveBy(dx, dy)
	}
}

// =====================================================================
//  Graph observer — bridges graph events to external callbacks
// =====================================================================

// graphObserver subscribes to scenegraph events and fans them out.
//
// Currently:
//   - OnConflictsChanged → if set, forwarded to onConflictsChanged
//     callback on the Serializer (sprite layer uses this to paint red
//     borders).
//   - OnParentChanged → triggers a NotifyChange so the JSON backup
//     picks up the new parent/children layout.
type graphObserver struct {
	serializer         *Serializer
	onConflictsChanged func(deviceID string, conflicts []Conflict)
}

func (o *graphObserver) OnConflictsChanged(deviceID string, conflicts []scenegraph.Conflict) {
	if o.onConflictsChanged != nil {
		o.onConflictsChanged(deviceID, fromGraphConflicts(conflicts))
	}
}

func (o *graphObserver) OnParentChanged(deviceID, oldParent, newParent string) {
	// Don't fire NotifyChange here — the device's drag-end handler
	// already calls NotifyChange explicitly. Double-firing would cost
	// an extra backup for every drag that changes parentage.

	// Forward to the external hook (installed by the workspace) so
	// the z-index registry can keep children rendered above parents.
	if o.serializer != nil && o.serializer.onParentChanged != nil {
		o.serializer.onParentChanged(deviceID, oldParent, newParent)
	}
}
