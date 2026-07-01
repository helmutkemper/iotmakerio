// browser/stage/interfaces.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package stage

type Functions interface {
	AddHighLatencyFunctions(runnerFunc func()) (UId string, total int)
	DeleteHighLatencyFunctions(UId string)
	AddDrawFunctions(runnerFunc func()) (UId string, total int)
	DeleteDrawFunctions(UId string)
	AddMathFunctions(runnerFunc func()) (UId string, total int)
	DeleteMathFunctions(UId string)
}
