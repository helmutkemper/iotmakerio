// grid/grid.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package grid

import "github.com/helmutkemper/iotmakerio/rulesDensity"

// Adjust
//
// English:
//
//	Interface for snapping coordinates to the hexagonal grid. Any struct that
//	can convert arbitrary (x, y) coordinates to the nearest hex center satisfies
//	this interface.
//
//	AdjustCenter works with raw int pixel values (physical pixels).
//	AdjustCenterD works with Density values (logical units), correctly applying
//	the density factor in both directions.
//
// Português:
//
//	Interface para ajustar coordenadas ao grid hexagonal. Qualquer struct que
//	consiga converter coordenadas (x, y) arbitrárias para o centro do hexágono
//	mais próximo satisfaz esta interface.
//
//	AdjustCenter trabalha com valores int em pixels brutos (pixels físicos).
//	AdjustCenterD trabalha com valores Density (unidades lógicas), aplicando
//	corretamente o fator de densidade em ambas as direções.
type Adjust interface {
	// AdjustCenter
	//
	// English:
	//
	//  Recalculates and returns the adjusted pixel coordinates (cx, cy) for the
	//  center of the nearest hexagon based on pixel inputs (x, y).
	//  Both input and output are in physical pixel space.
	//
	// Português:
	//
	//  Recalcula e retorna as coordenadas em pixels ajustadas (cx, cy) para o
	//  centro do hexágono mais próximo com base nas entradas em pixels (x, y).
	//  Tanto entrada quanto saída estão no espaço de pixels físicos.
	AdjustCenter(x, y int) (cx, cy int)

	// AdjustCenterD
	//
	// English:
	//
	//  Recalculates and returns the adjusted coordinates for the center of the
	//  nearest hexagon, using Density values. The density factor is applied
	//  automatically: logical → physical for the calculation, physical → logical
	//  for the result.
	//
	// Português:
	//
	//  Recalcula e retorna as coordenadas ajustadas para o centro do hexágono
	//  mais próximo, usando valores Density. O fator de densidade é aplicado
	//  automaticamente: lógico → físico para o cálculo, físico → lógico para
	//  o resultado.
	AdjustCenterD(x, y rulesDensity.Density) (cx, cy rulesDensity.Density)
}
