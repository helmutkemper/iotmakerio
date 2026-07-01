// /ide/devices/compVars/util.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compVars

// util.go — Package-level helpers shared by all variable devices
// (GetVar / SetVar, per type). Variables are their own device family,
// separate from constants — same structural pattern, own package.
//
// Português: Helpers de pacote compartilhados pelos devices de variável
// (GetVar / SetVar, por tipo). Variáveis são uma família própria, separada
// das constantes — mesmo padrão estrutural, pacote próprio.

import "strings"

// escapeXml replaces the five XML special characters so that user-provided
// names can be safely embedded inside SVG text elements without breaking the
// markup or creating injection vectors.
//
// Português: Escapa os cinco caracteres especiais de XML para embutir nomes
// do usuário em SVG com segurança.
func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
