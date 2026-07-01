// browser/html/typeSvgTypeFeColorMatrix.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgTypeFeColorMatrix string

func (e SvgTypeFeColorMatrix) String() string {
	return string(e)
}

const (
	KSvgTypeFeColorMatrixMatrix           SvgTypeFeColorMatrix = "matrix"
	KSvgTypeFeColorMatrixSaturate         SvgTypeFeColorMatrix = "saturate"
	KSvgTypeFeColorMatrixHueRotate        SvgTypeFeColorMatrix = "hueRotate"
	KSvgTypeFeColorMatrixLuminanceToAlpha SvgTypeFeColorMatrix = "luminanceToAlpha"
)
