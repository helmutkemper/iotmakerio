// browser/css/typeInset.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package css

type Inset string

const (
	KInsetInherit     Inset = "inherit"
	KInsetInitial     Inset = "initial"
	KInsetRevert      Inset = "revert"
	KInsetRevertLayer Inset = "revert-layer"
	KInsetUnset       Inset = "unset"
)
