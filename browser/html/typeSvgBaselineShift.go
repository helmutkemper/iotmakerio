// browser/html/typeSvgBaselineShift.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgBaselineShift string

func (e SvgBaselineShift) String() string {
	return string(e)
}

const (
	KSvgBaselineShiftAuto     SvgBaselineShift = "auto"
	KSvgBaselineShiftBaseline SvgBaselineShift = "baseline"
	KSvgBaselineShiftSuper    SvgBaselineShift = "super"
	KSvgBaselineShiftSub      SvgBaselineShift = "sub"
	KSvgBaselineShiftInherit  SvgBaselineShift = "inherit"
)
