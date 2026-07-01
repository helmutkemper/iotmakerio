// browser/eventClipBoard/typeEvenetClipBoard.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package eventClipBoard

type EventClipBoard int

func (el EventClipBoard) String() string {
	return eventClipBoardString[el]
}

var eventClipBoardString = [...]string{
	"copy",
	"cut",
	"paste",
}

const (
	// KCopy
	// en: The event occurs when the user copies the content of an element
	KCopy EventClipBoard = iota

	// KCut
	// en: The event occurs when the user cuts the content of an element
	KCut

	// KPaste
	// en: The event occurs when the user pastes some content in an element
	KPaste
)
