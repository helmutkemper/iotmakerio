// browser/eventProgress/typeEventProgress.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package eventProgress

type EventProgress int

func (el EventProgress) String() string {
	return eventProgressString[el]
}

var eventProgressString = [...]string{
	"error",
	"loadstart",
}

const (
	// KError
	// en: The event occurs when an error occurs while loading an external file
	KError EventProgress = iota

	// KLoadStart
	// en: The event occurs when the browser starts looking for the specified media
	KLoadStart
)
