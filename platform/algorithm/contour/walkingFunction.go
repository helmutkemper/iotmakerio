// platform/algorithm/contour/walkingFunction.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package contour

type walkingFunction func(x, y int) (dx, dy int)
