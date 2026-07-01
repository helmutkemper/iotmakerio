// browser/eventTransition/typeEventTransition.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package eventTransition

type EventTransition int

func (el EventTransition) String() string {
	return eventTransitionString[el]
}

var eventTransitionString = [...]string{
	"transitionend",
}

const (
	// KTransitionend
	// en: The event occurs when a CSS transition has completed
	KTransitionend EventTransition = iota
)
