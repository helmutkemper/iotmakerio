// server/codegen/blackbox/include_check_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestMissingLocalIncludes_FieldReport reproduces the 2026-07-11 export
// exactly: the header tab typo'd as "porta_api.h" while portal_core.c
// includes "portal_api.h" (→ issue WITH suggestion), and logo.gif never
// attached while "logo_gif_data.h" is included (→ issue WITHOUT
// suggestion). The attached templates/portal.html asset must resolve its
// generated header, and angle includes must be ignored.
//
// Português: Reproduz o export de 2026-07-11: aba com typo "porta_api.h"
// enquanto o portal_core.c inclui "portal_api.h" (→ problema COM
// sugestão), e logo.gif nunca anexado com "logo_gif_data.h" incluído (→
// problema SEM sugestão). O asset templates/portal.html resolve seu header
// gerado, e includes de ângulo são ignorados.
func TestMissingLocalIncludes_FieldReport(t *testing.T) {
	def := &BlackBoxDef{
		Files: []FileEntry{
			{Path: "porta_api.h", Content: "#ifndef PORTAL_API_H\n#endif\n"},
			{Path: "portal_core.c", Content: "#include <stdint.h>\n" +
				"#include \"portal_api.h\"\n" +
				"#include \"templates/portal_html_data.h\"\n" +
				"#include \"logo_gif_data.h\"\n"},
		},
		Assets: []AssetEntry{{Path: "templates/portal.html"}},
	}

	issues := def.MissingLocalIncludes()
	if len(issues) != 2 {
		t.Fatalf("issues = %d (%+v), want 2", len(issues), issues)
	}

	if issues[0].Include != "portal_api.h" || issues[0].Line != 2 {
		t.Fatalf("first issue: %+v", issues[0])
	}
	if issues[0].Suggestion != "porta_api.h" {
		t.Fatalf("typo must suggest the near-miss: %+v", issues[0])
	}
	if issues[1].Include != "logo_gif_data.h" || issues[1].Suggestion != "" {
		t.Fatalf("missing asset header must have NO suggestion: %+v", issues[1])
	}
}

// TestMissingLocalIncludes_CleanProject pins the happy path: every quoted
// include resolves (authored file + asset header) → zero issues.
// Português: Pina o caminho feliz: tudo resolve → zero problemas.
func TestMissingLocalIncludes_CleanProject(t *testing.T) {
	def := &BlackBoxDef{
		Files: []FileEntry{
			{Path: "portal_api.h", Content: "#pragma once\n"},
			{Path: "portal_core.c", Content: "#include \"portal_api.h\"\n" +
				"#include \"templates/portal_html_data.h\"\n"},
		},
		Assets: []AssetEntry{{Path: "templates/portal.html"}},
	}
	if issues := def.MissingLocalIncludes(); len(issues) != 0 {
		t.Fatalf("clean project produced issues: %+v", issues)
	}
}
