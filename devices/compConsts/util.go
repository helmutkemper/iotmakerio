// /ide/devices/compConsts/util.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compConsts

// util.go — Package-level helpers shared by all constant devices.
//
// Keeping these in a dedicated file prevents duplication across the four
// constant device files and makes them easy to find.

import "strings"

// kArrayMaxDisplayRunes caps the values text shown inside the body of the
// three constant-collection devices (ConstArrayInt / Float / String); longer
// content is truncated with "…". The full text is always available in the
// Inspect overlay. Shared here because all three siblings render the same
// brace-wrapped body.
//
// Português: Limite de runas do texto exibido no corpo dos três devices de
// coleção constante; o texto completo fica no Inspect.
const kArrayMaxDisplayRunes = 14

// escapeXml replaces the five XML special characters so that user-provided
// values can be safely embedded inside SVG text elements without breaking the
// SVG markup or creating injection vectors.
//
// Replacements (in order to avoid double-escaping):
//
//	&  →  &amp;
//	<  →  &lt;
//	>  →  &gt;
//	"  →  &quot;
//	'  →  &apos;
func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
