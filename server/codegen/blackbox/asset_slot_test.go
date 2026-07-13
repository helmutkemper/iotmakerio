// server/codegen/blackbox/asset_slot_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestParseC_AssetSlot pins the maker-file slot: a `const unsigned char *`
// parameter tagged `asset:page.` plus its positional `unsigned long`
// length become ONE AssetSlots entry and BOTH leave Inputs; the port int
// before them is untouched. A malformed pairing (missing length) keeps
// the ports and drops the directive, mirroring collapseSliceParams.
// Português: Pina o slot de arquivo do maker: ponteiro marcado + tamanho
// posicional viram UMA entrada de AssetSlots e AMBOS saem de Inputs.
// Pareamento malformado mantém as portas e derruba a diretiva.
func TestParseC_AssetSlot(t *testing.T) {
	src := []byte(`
// label:Web server.
void web_server_start(
	// connection:mandatory.
	int port,
	// asset:page.
	// doc:The page served at GET /.
	const unsigned char *page,
	unsigned long page_len
) { (void)port; (void)page; (void)page_len; }
`)
	def, err := ParseC(src, DefaultParserLimits())
	if err != nil {
		t.Logf("parse warnings: %v", err)
	}
	fn := def.Functions[0]
	if len(fn.AssetSlots) != 1 {
		t.Fatalf("AssetSlots = %+v, want 1", fn.AssetSlots)
	}
	s := fn.AssetSlots[0]
	if s.Slot != "page" || s.DataParam != "page" || s.LenParam != "page_len" ||
		s.DataIndex != 1 || s.LenIndex != 2 {
		t.Fatalf("slot = %+v", s)
	}
	if s.Doc != "The page served at GET /" {
		t.Fatalf("slot doc = %q", s.Doc)
	}
	if len(fn.Inputs) != 1 || fn.Inputs[0].Name != "port" {
		t.Fatalf("Inputs = %+v, want only port", fn.Inputs)
	}

	bad := []byte(`
// label:Broken.
void broken(
	// asset:page.
	const unsigned char *page
) { (void)page; }
`)
	def2, err2 := ParseC(bad, DefaultParserLimits())
	if err2 != nil {
		t.Logf("parse warnings: %v", err2)
	}
	fn2 := def2.Functions[0]
	if len(fn2.AssetSlots) != 0 {
		t.Fatalf("malformed must yield no slot: %+v", fn2.AssetSlots)
	}
	if len(fn2.Inputs) != 1 || fn2.Inputs[0].AssetSlot != "" {
		t.Fatalf("malformed must keep the port with the directive cleared: %+v", fn2.Inputs)
	}
}
