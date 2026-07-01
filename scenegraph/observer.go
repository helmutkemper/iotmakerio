// /ide/scenegraph/observer.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scenegraph

// Observer receives notifications when the graph's observable state
// changes. All methods are invoked synchronously from within Graph
// operations; implementations must be fast and must not re-enter the
// Graph (no Register, Unregister, Begin*, Update*, End*).
//
// English:
//
//	Two events are reported:
//
//	  - OnConflictsChanged fires when the conflict set of a device differs
//	    from the last set reported for that device. conflicts is the full
//	    current list, not a diff — empty means the device is clean.
//	    Consumers compare with what they painted before and update their
//	    state accordingly.
//
//	  - OnParentChanged fires when the Graph detects a change in the
//	    parent-child relationship of a device. oldParent and newParent are
//	    "" for root. The event fires only on EndDrag when the device has
//	    no conflicts — mid-drag movements do NOT emit this event.
//
// Português:
//
//	Observer recebe notificações quando o estado observável do grafo muda.
//	Implementações devem ser rápidas e não podem chamar métodos do Graph.
type Observer interface {
	OnConflictsChanged(deviceID string, conflicts []Conflict)
	OnParentChanged(deviceID, oldParent, newParent string)
}

// NoopObserver is an Observer that does nothing. Useful as a default and
// in tests.
//
// Português: Observer que não faz nada; útil como padrão e em testes.
type NoopObserver struct{}

// OnConflictsChanged implements Observer.
func (NoopObserver) OnConflictsChanged(string, []Conflict) {}

// OnParentChanged implements Observer.
func (NoopObserver) OnParentChanged(string, string, string) {}
