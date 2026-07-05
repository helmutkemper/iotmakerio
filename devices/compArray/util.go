// devices/compArray/util.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compArray

// util.go — Package-level helpers shared by the array devices.

import "strings"

// escapeXml replaces the five XML special characters so text can be safely
// embedded inside SVG text elements without breaking the markup or creating
// injection vectors. Package-local, mirroring the identical helper in the
// sibling device packages (there is no shared exported version).
//
// Replacements (in order, to avoid double-escaping):
//
//	&  →  &amp;
//	<  →  &lt;
//	>  →  &gt;
//	"  →  &quot;
//	'  →  &apos;
//
// Português: Escapa os cinco caracteres especiais de XML para embutir texto com
// segurança em elementos <text> de SVG. Local do pacote, espelhando o mesmo
// helper nos pacotes irmãos.
func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
