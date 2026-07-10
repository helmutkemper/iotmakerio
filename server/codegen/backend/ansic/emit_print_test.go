// server/codegen/backend/ansic/emit_print_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

import (
	"strings"
	"testing"

	"server/codegen/blackbox"
	"server/codegen/ir"
)

// cPrintInst builds one OpPrint instruction — the shape ir/emit.go's
// emitPrint produces (see OpPrint's table in ir/types.go).
//
// Português: Monta uma instrução OpPrint — a forma que o emitPrint do
// ir/emit.go produz (tabela do OpPrint em ir/types.go).
func cPrintInst(irType, src, prefix, format string) ir.Instruction {
	return ir.Instruction{
		Op:   ir.OpPrint,
		Dest: "print1",
		Type: irType,
		Args: []string{src},
		Meta: map[string]string{"prefix": prefix, "format": format},
	}
}

// TestEmit_Print_C pins every type × format variant of the C rendering,
// including the cast widening that keeps the printf length modifiers correct
// on every profile, and the `<src>_len` loop the []byte hex variant uses.
//
// Português: Pina cada variante tipo × formato da renderização C, incluindo
// o alargamento por cast que mantém os modificadores do printf corretos em
// qualquer profile, e o loop `<src>_len` do []byte hex.
func TestEmit_Print_C(t *testing.T) {
	cases := []struct {
		name string
		inst ir.Instruction
		want []string
	}{
		{
			name: "int decimal with prefix — widened cast",
			inst: cPrintInst("int", "%v1", "temp", "decimal"),
			want: []string{`printf("temp %ld\n", (long)v1);`},
		},
		{
			name: "int hex",
			inst: cPrintInst("int", "%v1", "", "hex"),
			want: []string{`printf("0x%lX\n", (unsigned long)v1);`},
		},
		{
			name: "float natural — variadic double",
			inst: cPrintInst("float", "%v1", "", "float"),
			want: []string{`printf("%g\n", (double)v1);`},
		},
		{
			name: "float trunc — cast, never rounded",
			inst: cPrintInst("float", "%v1", "", "trunc"),
			want: []string{`printf("%ld\n", (long)v1);`},
		},
		{
			name: "string",
			inst: cPrintInst("string", "%v1", "name", ""),
			want: []string{`printf("name %s\n", v1);`},
		},
		{
			name: "bool true-false — ternary",
			inst: cPrintInst("bool", "%v1", "", "truefalse"),
			want: []string{`printf("%s\n", v1 ? "true" : "false");`},
		},
		{
			name: "bool onezero — ternary",
			inst: cPrintInst("bool", "%v1", "", "onezero"),
			want: []string{`printf("%d\n", v1 ? 1 : 0);`},
		},
		{
			name: "byte hex default",
			inst: cPrintInst("byte", "%v1", "", "hex"),
			want: []string{`printf("0x%02X\n", (unsigned)v1);`},
		},
		{
			name: "byte decimal",
			inst: cPrintInst("byte", "%v1", "", "decimal"),
			want: []string{`printf("%u\n", (unsigned)v1);`},
		},
		{
			name: "bytes hex — one-line loop over the _len companion",
			inst: cPrintInst("[]byte", "%v1", "buf", "hex"),
			want: []string{
				`printf("buf ");`,
				`for (size_t i = 0; i < v1_len; i++) { printf(i ? " %02X" : "%02X", (unsigned)v1[i]); }`,
				`printf("\n");`,
			},
		},
		{
			name: "bytes as text — length-bounded, no NUL required",
			inst: cPrintInst("[]byte", "%v1", "", "text"),
			want: []string{`printf("%.*s\n", (int)v1_len, (const char *)v1);`},
		},
		{
			// A maker prefix carrying "%" must reach printf as a literal —
			// doubled in the format string.
			// Português: Prefixo do maker com "%" chega ao printf como
			// literal — dobrado na string de formato.
			name: "prefix with percent is doubled",
			inst: cPrintInst("int", "%v1", "load %", "decimal"),
			want: []string{`printf("load %% %ld\n", (long)v1);`},
		},
		{
			// Quotes and backslashes in the prefix must survive as valid C
			// string-literal escapes.
			// Português: Aspas e barras no prefixo sobrevivem como escapes
			// válidos do literal C.
			name: "prefix with quote and backslash escaped",
			inst: cPrintInst("string", "%v1", `path "C:\x"`, ""),
			want: []string{`printf("path \"C:\\x\" %s\n", v1);`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := &ir.Program{}
			prog.Append(tc.inst)
			files := Emit(prog, ProfilePortable, blackbox.Naming{})
			mainC := files["main.c"]
			for _, want := range tc.want {
				if !strings.Contains(mainC, want) {
					t.Errorf("missing %q in:\n%s", want, mainC)
				}
			}
			// OpPrint commits the output to stdio — the include must ship.
			// Português: OpPrint compromete a saída com stdio — o include
			// tem que viajar.
			if !strings.Contains(mainC, "#include <stdio.h>") {
				t.Errorf("<stdio.h> missing in:\n%s", mainC)
			}
		})
	}
}
