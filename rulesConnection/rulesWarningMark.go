// rulesConnection/rulesWarningMark.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesConnection

import (
	"github.com/helmutkemper/iotmakerio/platform/factoryColor"
)

var (
	KTrafficSignBorderColor             = factoryColor.NewRed()
	KTrafficSignBackgroundColor         = factoryColor.NewWhite()
	KTrafficSignWarningExclamationColor = factoryColor.NewBlack()
)
