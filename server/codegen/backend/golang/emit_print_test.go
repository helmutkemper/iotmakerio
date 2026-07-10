// server/codegen/backend/golang/emit_print_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package golang

import (
	"strings"
	"testing"

	"server/codegen/ir"
)

// printInst builds one OpPrint instruction — the shape ir/emit.go's emitPrint
// produces (see OpPrint's table in ir/types.go).
//
// Português: Monta uma instrução OpPrint — a forma que o emitPrint do
// ir/emit.go produz (tabela do OpPrint em ir/types.go).
func printInst(irType, src, prefix, format string) ir.Instruction {
	return ir.Instruction{
		Op:   ir.OpPrint,
		Dest: "print1",
		Type: irType,
		Args: []string{src},
		Meta: map[string]string{"prefix": prefix, "format": format},
	}
}

// TestEmit_Print_Go pins every type × format variant of the Go rendering.
// Table-driven: each case checks the emitted Printf line (and, for the bool
// "onezero" variant, the named temp that replaces the missing ternary).
//
// Português: Pina cada variante tipo × formato da renderização Go. Cada caso
// confere a linha Printf emitida (e, no bool "onezero", o temp nomeado que
// substitui o ternário inexistente).
func TestEmit_Print_Go(t *testing.T) {
	cases := []struct {
		name string
		inst ir.Instruction
		want []string
	}{
		{
			name: "int decimal with prefix",
			inst: printInst("int", "%v1", "temp", "decimal"),
			want: []string{`fmt.Printf("temp %d\n", v1)`},
		},
		{
			name: "int hex",
			inst: printInst("int", "%v1", "", "hex"),
			want: []string{`fmt.Printf("0x%X\n", v1)`},
		},
		{
			name: "float natural",
			inst: printInst("float", "%v1", "", "float"),
			want: []string{`fmt.Printf("%v\n", v1)`},
		},
		{
			name: "float trunc — cast, never rounded",
			inst: printInst("float", "%v1", "", "trunc"),
			want: []string{`fmt.Printf("%d\n", int64(v1))`},
		},
		{
			name: "string",
			inst: printInst("string", "%v1", "name", ""),
			want: []string{`fmt.Printf("name %s\n", v1)`},
		},
		{
			name: "bool true-false",
			inst: printInst("bool", "%v1", "", "truefalse"),
			want: []string{`fmt.Printf("%t\n", v1)`},
		},
		{
			name: "bool onezero — named temp",
			inst: printInst("bool", "%v1", "", "onezero"),
			want: []string{
				"print1AsInt := 0",
				"if v1 {",
				"print1AsInt = 1",
				`fmt.Printf("%d\n", print1AsInt)`,
			},
		},
		{
			name: "byte hex default",
			inst: printInst("byte", "%v1", "", "hex"),
			want: []string{`fmt.Printf("0x%02X\n", v1)`},
		},
		{
			name: "byte decimal",
			inst: printInst("byte", "%v1", "", "decimal"),
			want: []string{`fmt.Printf("%d\n", v1)`},
		},
		{
			name: "bytes hex — space-separated pairs via the fmt flag",
			inst: printInst("[]byte", "%v1", "buf", "hex"),
			want: []string{`fmt.Printf("buf % X\n", v1)`},
		},
		{
			name: "bytes as text",
			inst: printInst("[]byte", "%v1", "", "text"),
			want: []string{`fmt.Printf("%s\n", v1)`},
		},
		{
			// A maker prefix carrying "%" must reach Printf as a literal —
			// doubled in the format string.
			// Português: Prefixo do maker com "%" chega ao Printf como
			// literal — dobrado na string de formato.
			name: "prefix with percent is doubled",
			inst: printInst("int", "%v1", "load %", "decimal"),
			want: []string{`fmt.Printf("load %% %d\n", v1)`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := &ir.Program{}
			prog.Append(tc.inst)
			out := Emit(prog)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			if !strings.Contains(out, `"fmt"`) && !strings.Contains(out, `import "fmt"`) {
				t.Errorf("fmt import missing in:\n%s", out)
			}
		})
	}
}
