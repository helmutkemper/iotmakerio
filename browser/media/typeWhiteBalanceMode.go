// browser/media/typeWhiteBalanceMode.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package media

type WhiteBalanceMode string

func (e WhiteBalanceMode) String() string {
	return string(e)
}

const (
	KWhiteBalanceModeNone       WhiteBalanceMode = "none"
	KWhiteBalanceModeManual     WhiteBalanceMode = "manual"
	KWhiteBalanceModeSingleShot WhiteBalanceMode = "single-shot"
	KWhiteBalanceModeContinuous WhiteBalanceMode = "continuous"
)
