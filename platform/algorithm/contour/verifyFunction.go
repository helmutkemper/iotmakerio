// platform/algorithm/contour/verifyFunction.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package contour

func (e *Contour) VerifyFunction(f func(pMatrix *[][]any, x, y int) bool) {
	e.verifyFunction = f
}
