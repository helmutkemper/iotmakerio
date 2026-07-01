// browser/css/_typeShadow.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package css

import (
	"strings"
)

type Shadow []BoxShadow

func (s *Shadow) Add(shadow BoxShadow) {
	*s = append(*s, shadow)
}

func (s Shadow) String() string {
	var shadows = make([]string, 0)

	for _, shadow := range s {
		shadows = append(shadows, shadow.String())
	}
	return strings.Join(shadows, ",\n") + ";"
}
