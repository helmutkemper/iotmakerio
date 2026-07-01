// browser/eventInput/typeEventInput.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package eventInput

type EventInput int

func (el EventInput) String() string {
	return eventInputString[el]
}

var eventInputString = [...]string{
	"input",
}

const (
	// KInput
	// en: The event occurs when an element gets user input
	KInput EventInput = iota
)
