// browser/html/typeSvgTypeTransform.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgTypeTransform string

func (e SvgTypeTransform) String() string {
	return string(e)
}

const (
	KSvgTypeTransformTranslate SvgTypeTransform = "translate"
	KSvgTypeTransformScale     SvgTypeTransform = "scale"
	KSvgTypeTransformRotate    SvgTypeTransform = "rotate"
	KSvgTypeTransformSkewX     SvgTypeTransform = "skewX"
	KSvgTypeTransformSkewY     SvgTypeTransform = "skewY"
)
