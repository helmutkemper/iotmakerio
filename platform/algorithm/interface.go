// platform/algorithm/interface.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package algorithm

type CurveInterface interface {
	GetProcessed() (list *[]Point)
}
