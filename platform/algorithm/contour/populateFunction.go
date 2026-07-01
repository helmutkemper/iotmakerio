// platform/algorithm/contour/populateFunction.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package contour

func (e *Contour) PopulateFunction(f func(pData *[][]any, x, y int)) {
	e.populateFunction = f
}
