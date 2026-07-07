// server/codegen/blackbox/parser_c_signature_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Pins the verbatim-signature contract of FuncDef.CReturnType / CParams: the
// C parser must carry the authored return type and parameter list exactly as
// written, because the multi-file header composes its prototypes from them
// (csurface.go) and the definition shipped in bb_<id>.c IS the authored one —
// any normalisation here would make the compiler's declaration check fight
// the source.
//
// Português: Fixa o contrato de assinatura verbatim (CReturnType/CParams): o
// parser C carrega retorno e parâmetros exatamente como escritos, porque o
// header multiarquivo compõe protótipos a partir deles e a definição
// embarcada é a autoral — normalizar aqui faria o check do compilador brigar
// com o fonte.

package blackbox

import "testing"

func TestParseC_FillsVerbatimSignature(t *testing.T) {
	src := `
typedef struct sht3x { int fd; } sht3x_t;

sht3x_t *sht3x_create(int bus) {
    (void)bus;
    return 0;
}

void sht3x_log(sht3x_t *dev, const char *msg, float value) {
    (void)dev; (void)msg; (void)value;
}

int sht3x_ping(void) {
    return 0;
}

int sht3x_zero() {
    return 0;
}
`
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	want := map[string]struct{ ret, params string }{
		"sht3x_create": {ret: "sht3x_t *", params: "int bus"},
		"sht3x_log":    {ret: "void", params: "sht3x_t *dev, const char *msg, float value"},
		// `(void)` keeps its literal "void" — the header must mirror the
		// source's prototype-ness exactly; `()` stays empty for the same
		// reason (see csurface.go's Header for the compatibility note).
		"sht3x_ping": {ret: "int", params: "void"},
		"sht3x_zero": {ret: "int", params: ""},
	}
	found := 0
	for i := range def.Functions {
		fn := &def.Functions[i]
		w, ok := want[fn.Name]
		if !ok {
			continue
		}
		found++
		if fn.CReturnType != w.ret {
			t.Errorf("%s: CReturnType = %q, want %q", fn.Name, fn.CReturnType, w.ret)
		}
		if fn.CParams != w.params {
			t.Errorf("%s: CParams = %q, want %q", fn.Name, fn.CParams, w.params)
		}
	}
	if found != len(want) {
		t.Fatalf("parsed %d of %d expected functions; def.Functions = %+v",
			found, len(want), def.Functions)
	}
}
