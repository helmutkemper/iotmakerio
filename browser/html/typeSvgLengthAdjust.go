// browser/html/typeSvgLengthAdjust.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgLengthAdjust string

func (e SvgLengthAdjust) String() string {
	return string(e)
}

const (
	KSvgLengthAdjustSpacing          SvgLengthAdjust = "spacing"
	KSvgLengthAdjustSpacingAndGlyphs SvgLengthAdjust = "spacingAndGlyphs"
)
