// browser/factoryBrowser/newStage.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/stage"
	"github.com/helmutkemper/iotmakerio/platform/globalEngine"
)

func NewStage() (ref *stage.Stage) {
	ref = &stage.Stage{}
	ref.Engine(globalEngine.Engine)
	ref.Init()

	return ref
}
