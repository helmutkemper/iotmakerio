// browser/html/typeSvgFontStretch.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgFontStretch string

func (e SvgFontStretch) String() string {
	return string(e)
}

const (
	KSvgFontStretchNormal         SvgFontStretch = "normal"
	KSvgFontStretchUltraCondensed SvgFontStretch = "ultra-condensed"
	KSvgFontStretchExtraCondensed SvgFontStretch = "extra-condensed"
	KSvgFontStretchCondensed      SvgFontStretch = "condensed"
	KSvgFontStretchSemiCondensed  SvgFontStretch = "semi-condensed"
	KSvgFontStretchSemiExpanded   SvgFontStretch = "semi-expanded"
	KSvgFontStretchExpanded       SvgFontStretch = "expanded"
	KSvgFontStretchExtraExpanded  SvgFontStretch = "extra-expanded"
	KSvgFontStretchUltraExpanded  SvgFontStretch = "ultra-expanded"
)
