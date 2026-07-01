// platform/algorithm/contour/f90.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package contour

func (e *Contour) f90(x, y int) (dx, dy int) {
	// y+1
	if y+1 <= e.yMax && e.verified[y+1][x] == false && e.verifyFunction(e.matrix, x, y+1) == true {
		return 0, 1
	}

	return 0, 0
}
