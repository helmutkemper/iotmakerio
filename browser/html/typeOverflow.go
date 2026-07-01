// browser/html/typeOverflow.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type Overflow string

func (e Overflow) String() string {
	return string(e)
}

const (
	KOverflowVisible Overflow = "visible"
	KOverflowHidden  Overflow = "hidden"
	KOverflowScroll  Overflow = "scroll"
	KOverflowAuto    Overflow = "auto"
)
