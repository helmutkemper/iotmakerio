// browser/media/typeFocusMode.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package media

type FocusMode string

func (e FocusMode) String() string {
	return string(e)
}

const (
	KFocusModeNone       FocusMode = "none"
	KFocusModeManual     FocusMode = "manual"
	KFocusModeSingleShot FocusMode = "single-shot"
	KFocusModeContinuous FocusMode = "continuous"
)
