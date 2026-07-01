// /server/codegen/codeGen_c99_emit_c_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// c99AddDefsWithSource is c99AddDefs plus the authored C source on RawSource —
// the way store.LoadBlackBoxDefsForScene fills it for a C99 device, so the
// backend can inline the implementation.
func c99AddDefsWithSource() map[string]*blackbox.BlackBoxDef {
	const src = "" +
		"// add numbers.\n" +
		"// label:ADD.\n" +
		"#include <stdint.h>\n" +
		"int add(int a, int b) {\n" +
		"    return a + b;\n" +
		"}\n"
	def := &blackbox.BlackBoxDef{
		RawSource: src,
		Functions: []blackbox.NamedFuncDef{
			{
				Name: "add",
				FuncDef: blackbox.FuncDef{
					Inputs: []blackbox.PortDef{
						{Name: "a", GoType: "int", Connection: "mandatory"},
						{Name: "b", GoType: "int", Connection: "mandatory"},
					},
					Outputs: []blackbox.PortDef{{Name: "return", GoType: "int"}},
				},
			},
		},
	}
	return map[string]*blackbox.BlackBoxDef{"add": def}
}

// TestEmitC_C99FunctionDevice_CallAndSource is the fix for "the code was
// generated but ignored the device": with BB_CALL translated, main.c now
// contains the function call (inside main) AND the authored implementation
// inlined ahead of main, so the result compiles.
func TestEmitC_C99FunctionDevice_CallAndSource(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:        json.RawMessage(sceneC99Add),
		Language:     "c",
		BlackBoxDefs: c99AddDefsWithSource(),
	})

	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; diagnostics=%+v", resp.Diagnostics)
	}

	idx := strings.Index(mainC, "int main(void)")
	if idx < 0 {
		t.Fatalf("main.c has no main(); got:\n%s", mainC)
	}
	preamble, body := mainC[:idx], mainC[idx:]

	// The device is no longer ignored: the call appears inside main().
	if !strings.Contains(body, "add(") {
		t.Fatalf("expected add(...) call inside main(), got:\n%s", mainC)
	}

	// The authored implementation is inlined ahead of main() so the call
	// resolves and the file compiles on its own.
	if !strings.Contains(preamble, "int add") || !strings.Contains(preamble, "return a + b") {
		t.Fatalf("expected inlined add definition before main(), got:\n%s", mainC)
	}
	if !strings.Contains(preamble, "authored device sources") {
		t.Fatalf("expected the inlined-sources marker comment, got:\n%s", mainC)
	}
}
