// devices/compDebug/util.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compDebug

// util.go — Package-level helpers shared by all debug devices
// (Print, per type). Debug sinks are their own device family, separate
// from variables and constants — same structural pattern, own package.
//
// Português: Helpers de pacote compartilhados pelos devices de depuração
// (Print, por tipo). Sinks de debug são uma família própria, separada de
// variáveis e constantes — mesmo padrão estrutural, pacote próprio.

import "strings"

// escapeXml replaces the five XML special characters so that user-provided
// text (the prefix, the label) can be safely embedded inside SVG text
// elements without breaking the markup or creating injection vectors.
//
// Português: Escapa os cinco caracteres especiais de XML para embutir texto
// do usuário (prefixo, label) em SVG com segurança.
func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
