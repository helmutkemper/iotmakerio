// browser/html/typeSvgTypeFeFunc.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgTypeFeFunc string

func (e SvgTypeFeFunc) String() string {
	return string(e)
}

const (
	KSvgTypeFeFuncIdentity SvgTypeFeFunc = "identity"
	KSvgTypeFeFuncTable    SvgTypeFeFunc = "table"
	KSvgTypeFeFuncDiscrete SvgTypeFeFunc = "discrete"
	KSvgTypeFeFuncLinear   SvgTypeFeFunc = "linear"
	KSvgTypeFeFuncGamma    SvgTypeFeFunc = "gamma"
)
