// browser/media/typeExposureMode.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package media

type ExposureMode string

func (e ExposureMode) String() string {
	return string(e)
}

const (
	KExposureModeNone       ExposureMode = "none"
	KExposureModeManual     ExposureMode = "manual"
	KExposureModeSingleShot ExposureMode = "single-shot"
	KExposureModeContinuous ExposureMode = "continuous"
)
