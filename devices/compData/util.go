// /ide/devices/compData/util.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compData

import "strings"

// escapeXml escapes the five XML special characters for safe embedding of
// user text (file names, labels) inside the device's SVG.
// Português: Escapa os cinco caracteres especiais de XML para embutir
// texto do usuário (nomes de arquivo, labels) no SVG do device.
func escapeXml(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	)
	return r.Replace(s)
}

// boolToStr renders a bool for the string-typed overlay/props plumbing.
// Português: Converte bool para o encanamento string do overlay/props.
func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
