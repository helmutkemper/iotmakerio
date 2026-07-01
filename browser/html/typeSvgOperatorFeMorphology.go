// browser/html/typeSvgOperatorFeMorphology.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

type SvgOperatorFeMorphology string

func (e SvgOperatorFeMorphology) String() string {
	return string(e)
}

const (
	// KKSvgOperatorFeCompositeErode
	//
	// English:
	//
	// This value thins the source graphic defined in the in attribute.
	//
	// Português:
	//
	// Este valor afina o gráfico de origem definido no atributo in.
	KKSvgOperatorFeCompositeErode SvgOperatorFeMorphology = "erode"

	// KKSvgOperatorFeCompositeDilate
	//
	// English:
	//
	// This value fattens the source graphic defined in the in attribute.
	//
	// Português:
	//
	// Este valor engorda o gráfico de origem definido no atributo in.
	KKSvgOperatorFeCompositeDilate SvgOperatorFeMorphology = "dilate"
)
