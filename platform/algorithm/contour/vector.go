// platform/algorithm/contour/vector.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package contour

func (e *Contour) Vector(x, y int, degrees Degrees) {
	e.pSpin = int(degrees)
	e.verifyPoint(x, y)
}
