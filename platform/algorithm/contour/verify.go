// platform/algorithm/contour/verify.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package contour

func (e *Contour) Verify() {
	for {
		pass := false

		for y := 0; y != len(*e.matrix)-1; y += 1 {
			for x := 0; x != len((*e.matrix)[y])-1; x += 1 {
				if e.verified[y][x] == false && e.verifyFunction(e.matrix, x, y) == true {
					pass = true
					e.verifyPoint(x, y)
					return
				}
			}
		}

		if pass == false {
			break
		}
	}
}
