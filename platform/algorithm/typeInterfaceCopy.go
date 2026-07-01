// platform/algorithm/typeInterfaceCopy.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package algorithm

type CopyInterface interface {
	GetProcessed() (list *[]Point)
	GetOriginal() (list *[]Point)
}
