// browser/html/typeInterfaceCompatible.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

import "syscall/js"

type Compatible interface {
	Get() js.Value
}
