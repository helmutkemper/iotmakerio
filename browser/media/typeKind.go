// browser/media/typeKind.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package media

type Kind string

func (e Kind) String() string {
	return string(e)
}

const (
	KKindVideoInput  Kind = "videoinput"
	KKindAudioInput  Kind = "audioinput"
	KKindAudioOutput Kind = "audiooutput"
)
