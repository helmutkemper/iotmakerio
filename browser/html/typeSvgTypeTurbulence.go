// browser/html/typeSvgTypeTurbulence.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgTypeTurbulence string

func (e SvgTypeTurbulence) String() string {
	return string(e)
}

const (
	KSvgTypeTurbulenceFractalNoise SvgTypeTurbulence = "fractalNoise"
	KSvgTypeTurbulenceTurbulence   SvgTypeTurbulence = "turbulence"
)
