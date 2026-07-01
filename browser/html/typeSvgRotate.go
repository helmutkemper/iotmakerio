// browser/html/typeSvgRotate.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgRotate string

func (e SvgRotate) String() string {
	return string(e)
}

const (
	KSvgRotateAuto        SvgRotate = "auto"
	KSvgRotateAutoReverse SvgRotate = "auto-reverse"
)
