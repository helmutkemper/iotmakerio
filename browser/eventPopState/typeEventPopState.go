// browser/eventPopState/typeEventPopState.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package eventPopState

type EventPopState int

func (el EventPopState) String() string {
	return eventPopStateString[el]
}

var eventPopStateString = [...]string{
	"popstate",
}

const (
	// KPopState
	// en: The event occurs when the window's history changes
	KPopState EventPopState = iota
)
