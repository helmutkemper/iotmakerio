// /ide/devices/backendZRegistry.go

package devices

// backendZRegistry.go — Ordered z-index registry for backend container devices.
//
// Problem:
//
//	When containers are nested (Loop inside IfElse, IfElse inside Loop, etc.),
//	they all share the same z-index constant (rulesZIndex.Container = 10).
//	This causes the inner container to be drawn behind the outer one, making
//	it impossible to click or interact with.
//
// Solution:
//
//	Same approach as compFrontend/zRegistry.go: a centralized ordered table
//	where each container registers itself. The table position determines
//	the z-index. "Bring Forward" / "Send Backward" swap positions and
//	reassign all z-indices. This guarantees:
//	  - No z-index collisions between containers
//	  - Inner containers are drawn on top of outer ones
//	  - User can manually reorder with menu items
//
// Usage (in StatementLoop.Init, StatementIfElse.Init, etc.):
//
//	devices.BackendZRegistry.Register(e.id, e.elem)
//
// Usage (in Remove):
//
//	devices.BackendZRegistry.Unregister(e.id)
//
// Usage (in hex menu):
//
//	devices.BackendZRegistry.MoveForward(e.id)
//	devices.BackendZRegistry.MoveBackward(e.id)
//
// Português:
//
//	Registro ordenado de z-index para containers do backend.
//	Mesmo padrão do compFrontend/zRegistry.go. Garante que containers
//	internos sejam desenhados por cima dos externos.

import (
	"log"
	"sync"

	"github.com/helmutkemper/iotmakerio/rulesZIndex"
	"github.com/helmutkemper/iotmakerio/sprite"
)

// BackendZRegistry is the package-level singleton for managing backend
// container draw order. All container devices (Loop, LoopDuration, IfElse)
// should register here during Init() and unregister during Remove().
var BackendZRegistry = &ZIndexRegistry{
	baseZ: rulesZIndex.Container,
}

// ZEntry represents one element in the ordered z-index table.
type ZEntry struct {
	id   string
	elem sprite.Element
}

// ZIndexRegistry manages an ordered list of elements with unique z-indices.
// The draw order is determined by position in the list: element at
// position 0 has z-index = baseZ, position 1 has baseZ+1, etc.
//
// Exported so it can be used by both backend containers and potentially
// other device categories that need ordered z-index management.
type ZIndexRegistry struct {
	mu      sync.Mutex
	entries []ZEntry

	// baseZ is the starting z-index value for the first element.
	// All registered elements get z-indices from baseZ upward.
	baseZ int
}

// Register adds an element to the end of the ordered table (top of the
// draw order among registered elements). The element receives a z-index
// equal to baseZ + its position in the table.
//
// If the element is already registered (same id), it is not added again.
//
// Português: Adiciona um elemento ao final da tabela (topo da ordem de
// desenho). Se já registrado, não adiciona novamente.
func (r *ZIndexRegistry) Register(id string, elem sprite.Element) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicate.
	for _, e := range r.entries {
		if e.id == id {
			return
		}
	}

	r.entries = append(r.entries, ZEntry{id: id, elem: elem})
	r.reassign()
	log.Printf("[BackendZRegistry] Registered %s (total: %d)", id, len(r.entries))
}

// Unregister removes an element from the table and reassigns all z-indices.
// Safe to call with an id that is not registered (no-op).
//
// Português: Remove um elemento da tabela e reatribui todos os z-indices.
func (r *ZIndexRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.id == id {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			r.reassign()
			log.Printf("[BackendZRegistry] Unregistered %s (total: %d)", id, len(r.entries))
			return
		}
	}
}

// MoveForward swaps the element with the one above it in the table
// (higher z-index = drawn later = visually on top). If the element is
// already at the top, this is a no-op.
//
// Português: Troca o elemento com o que está acima na tabela.
func (r *ZIndexRegistry) MoveForward(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := r.indexOf(id)
	if idx < 0 || idx >= len(r.entries)-1 {
		return
	}

	r.entries[idx], r.entries[idx+1] = r.entries[idx+1], r.entries[idx]
	r.reassign()
	log.Printf("[BackendZRegistry] MoveForward %s: now at position %d", id, idx+1)
}

// MoveBackward swaps the element with the one below it in the table
// (lower z-index = drawn earlier = visually behind). If the element is
// already at the bottom, this is a no-op.
//
// Português: Troca o elemento com o que está abaixo na tabela.
func (r *ZIndexRegistry) MoveBackward(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := r.indexOf(id)
	if idx <= 0 {
		return
	}

	r.entries[idx], r.entries[idx-1] = r.entries[idx-1], r.entries[idx]
	r.reassign()
	log.Printf("[BackendZRegistry] MoveBackward %s: now at position %d", id, idx-1)
}

// EnsureChildAbove guarantees that `childID` sits strictly above
// `parentID` in the z-order, so the child renders in front of its
// container. If the child is already above the parent, this is a no-op.
// Otherwise the child is moved to the slot immediately after the
// parent, preserving the relative order of every other entry.
//
// This is called by the scenegraph observer whenever a device becomes
// the child of a container, so visual nesting always matches logical
// nesting — a device dragged into a Complex container never ends up
// hidden behind it.
//
// Both IDs must be registered; if either is absent this is a no-op.
//
// Português:
//
//	Garante que o filho fique acima do pai no z-order, pra nunca ficar
//	escondido atrás do container. Se já está acima, não faz nada. Caso
//	contrário, move o filho pra logo depois do pai na tabela,
//	preservando a ordem dos outros elementos. Chamado pelo observer do
//	scenegraph sempre que a parentage muda.
func (r *ZIndexRegistry) EnsureChildAbove(childID, parentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	childIdx := r.indexOf(childID)
	parentIdx := r.indexOf(parentID)
	if childIdx < 0 || parentIdx < 0 {
		return
	}
	if childIdx > parentIdx {
		return // already above — nothing to do
	}

	// Pull child out, then re-insert right after parent. Because we
	// removed an earlier slot, the parent's post-removal index shifts
	// down by one, so the target insertion index is just parentIdx.
	entry := r.entries[childIdx]
	r.entries = append(r.entries[:childIdx], r.entries[childIdx+1:]...)
	insertAt := parentIdx // parent moved from parentIdx to parentIdx-1; +1 == parentIdx
	r.entries = append(r.entries[:insertAt], append([]ZEntry{entry}, r.entries[insertAt:]...)...)

	r.reassign()
	log.Printf("[BackendZRegistry] EnsureChildAbove %s > %s: child now at %d, parent at %d",
		childID, parentID, r.indexOf(childID), r.indexOf(parentID))
}

// reassign sets the z-index of every registered element based on its
// position in the table. Position 0 gets baseZ, position 1 gets baseZ+1, etc.
func (r *ZIndexRegistry) reassign() {
	for i, e := range r.entries {
		z := r.baseZ + i
		e.elem.SetIndex(z)
	}
}

// indexOf returns the position of the element with the given id, or -1.
func (r *ZIndexRegistry) indexOf(id string) int {
	for i, e := range r.entries {
		if e.id == id {
			return i
		}
	}
	return -1
}

// DebugPrint logs the current state of the registry table.
func (r *ZIndexRegistry) DebugPrint() {
	r.mu.Lock()
	defer r.mu.Unlock()

	log.Printf("[BackendZRegistry] === Z-Index Table (%d entries) ===", len(r.entries))
	for i, e := range r.entries {
		z := r.baseZ + i
		log.Printf("[BackendZRegistry]   %d | z=%d | %s", i, z, e.id)
	}
	log.Printf("[BackendZRegistry] ===================================")
}
