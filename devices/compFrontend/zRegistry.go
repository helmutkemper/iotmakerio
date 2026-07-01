// /ide/devices/compFrontend/zregistry.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFrontend

// zregistry.go — Ordered z-index registry for frontend elements.
//
// Problem:
//
//	When multiple frontend elements coexist (background images, gauges, LEDs,
//	etc.), managing z-index with raw SetIndex(+1/-1) leads to collisions,
//	gaps, and unpredictable ordering. Two elements can end up with the same
//	z-index, and "Bring Forward" / "Send Backward" become unreliable.
//
// Solution:
//
//	A centralized ordered table where each frontend element registers itself.
//	The table position IS the z-index. Moving an element forward or backward
//	swaps it with its neighbor in the table, then ALL z-indices are reassigned
//	from position. This guarantees:
//	  - No z-index collisions (each element has a unique index)
//	  - No gaps (indices are contiguous: baseZ, baseZ+1, baseZ+2, ...)
//	  - Predictable ordering (table order = draw order)
//
// Usage:
//
//	// On Init:
//	FrontendZRegistry.Register(e.id, e.frontendElem)
//
//	// On Remove:
//	FrontendZRegistry.Unregister(e.id)
//
//	// Bring Forward (swap with element above):
//	FrontendZRegistry.MoveForward(e.id)
//
//	// Send Backward (swap with element below):
//	FrontendZRegistry.MoveBackward(e.id)
//
// The registry is a package-level singleton because there is only one
// frontend stage. When this needs to support multiple stages (e.g. split
// view), it can be refactored into a per-stage instance.
//
// Currently only StatementBackgroundImage registers. Other frontend
// components (Gauge, LED, etc.) can register in the future to get full
// z-order management across all element types.
//
// Português:
//
//	Tabela ordenada de z-index para elementos do frontend.
//	A posição na tabela É o z-index. Mover um elemento troca posição com
//	o vizinho e reatribui todos os z-indices. Garante: sem colisões, sem
//	gaps, e ordenação previsível.

import (
	"log"
	"sync"

	"github.com/helmutkemper/iotmakerio/rulesZIndex"
	"github.com/helmutkemper/iotmakerio/sprite"
)

// FrontendZRegistry is the package-level singleton for managing frontend
// element draw order. All frontend components that need z-order control
// should register here.
var FrontendZRegistry = &zIndexRegistry{
	baseZ: rulesZIndex.BackgroundFrontend,
}

// zEntry represents one element in the ordered z-index table.
type zEntry struct {
	id   string
	elem sprite.Element
}

// zIndexRegistry manages an ordered list of frontend elements.
// The draw order is determined by position in the list: element at
// position 0 has z-index = baseZ, position 1 has baseZ+1, etc.
type zIndexRegistry struct {
	mu      sync.Mutex
	entries []zEntry

	// baseZ is the starting z-index value for the first element.
	// All registered elements get z-indices from baseZ upward.
	// Default: rulesZIndex.BackgroundFrontend (5).
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
func (r *zIndexRegistry) Register(id string, elem sprite.Element) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicate.
	for _, e := range r.entries {
		if e.id == id {
			return
		}
	}

	r.entries = append(r.entries, zEntry{id: id, elem: elem})
	r.reassign()
	log.Printf("[ZRegistry] Registered %s (total: %d)", id, len(r.entries))
}

// Unregister removes an element from the table and reassigns all z-indices.
// Safe to call with an id that is not registered (no-op).
//
// Português: Remove um elemento da tabela e reatribui todos os z-indices.
func (r *zIndexRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.id == id {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			r.reassign()
			log.Printf("[ZRegistry] Unregistered %s (total: %d)", id, len(r.entries))
			return
		}
	}
}

// MoveForward swaps the element with the one above it in the table
// (higher z-index = drawn later = visually on top). If the element is
// already at the top, this is a no-op.
//
// After swapping, all z-indices are reassigned from position.
//
// Example:
//
//	Before: [A(z=5), B(z=6), C(z=7)]
//	MoveForward("B")
//	After:  [A(z=5), C(z=6), B(z=7)]
//
// Português: Troca o elemento com o que está acima na tabela (maior
// z-index = desenhado depois = visualmente por cima).
func (r *zIndexRegistry) MoveForward(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := r.indexOf(id)
	if idx < 0 || idx >= len(r.entries)-1 {
		// Not found or already at the top.
		return
	}

	// Swap with the element above (next position in the slice).
	r.entries[idx], r.entries[idx+1] = r.entries[idx+1], r.entries[idx]
	r.reassign()
	log.Printf("[ZRegistry] MoveForward %s: now at position %d", id, idx+1)
}

// MoveBackward swaps the element with the one below it in the table
// (lower z-index = drawn earlier = visually behind). If the element is
// already at the bottom, this is a no-op.
//
// Example:
//
//	Before: [A(z=5), B(z=6), C(z=7)]
//	MoveBackward("B")
//	After:  [B(z=5), A(z=6), C(z=7)]
//
// Português: Troca o elemento com o que está abaixo na tabela (menor
// z-index = desenhado antes = visualmente atrás).
func (r *zIndexRegistry) MoveBackward(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := r.indexOf(id)
	if idx <= 0 {
		// Not found or already at the bottom.
		return
	}

	// Swap with the element below (previous position in the slice).
	r.entries[idx], r.entries[idx-1] = r.entries[idx-1], r.entries[idx]
	r.reassign()
	log.Printf("[ZRegistry] MoveBackward %s: now at position %d", id, idx-1)
}

// reassign sets the z-index of every registered element based on its
// position in the table. Position 0 gets baseZ, position 1 gets baseZ+1,
// etc. This must be called after every mutation (register, unregister, swap).
//
// Português: Reatribui o z-index de cada elemento baseado na posição.
func (r *zIndexRegistry) reassign() {
	for i, e := range r.entries {
		z := r.baseZ + i
		e.elem.SetIndex(z)
	}
}

// indexOf returns the position of the element with the given id, or -1.
func (r *zIndexRegistry) indexOf(id string) int {
	for i, e := range r.entries {
		if e.id == id {
			return i
		}
	}
	return -1
}

// DebugPrint logs the current state of the registry table.
// Useful for debugging z-order issues from the browser console.
func (r *zIndexRegistry) DebugPrint() {
	r.mu.Lock()
	defer r.mu.Unlock()

	log.Printf("[ZRegistry] === Z-Index Table (%d entries) ===", len(r.entries))
	for i, e := range r.entries {
		z := r.baseZ + i
		log.Printf("[ZRegistry]   %d | z=%d | %s", i, z, e.id)
	}
	log.Printf("[ZRegistry] ===================================")
}
