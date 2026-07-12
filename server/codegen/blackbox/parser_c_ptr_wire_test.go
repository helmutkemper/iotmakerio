// server/codegen/blackbox/parser_c_ptr_wire_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestParseC_PointerWireTokens pins the WireType contract for scalar
// pointers: returns and plain inputs expose the abstract family token on
// the wire while GoType stays the authored C type; slice-paired pointers,
// direction:out pointers, and `char *` are untouched.
//
// Português: Pina o contrato do WireType para ponteiros escalares:
// retornos e entradas simples expõem o token abstrato da família no fio
// enquanto GoType fica o tipo C autoral; ponteiros pareados por slice:,
// com direction:out e `char *` ficam intocados.
func TestParseC_PointerWireTokens(t *testing.T) {
	src := []byte(`
// label:Buffer probe.
int32_t *get_buffer(
	// doc:Raw readings feed.
	const int32_t *data,
	// slice:count.
	const float *samples,
	size_t count,
	// direction:out.
	int32_t *written,
	// doc:Message.
	const char *msg
) {
	(void)data; (void)samples; (void)count; (void)msg;
	*written = 0;
	return 0;
}
`)
	def, err := ParseC(src, DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	if len(def.Functions) != 1 {
		t.Fatalf("functions: %d", len(def.Functions))
	}
	fn := def.Functions[0]

	// Return: authored GoType kept, family token on the wire.
	var ret *PortDef
	for i := range fn.Outputs {
		if fn.Outputs[i].Name == "return" {
			ret = &fn.Outputs[i]
		}
	}
	if ret == nil {
		t.Fatalf("no return output; outputs=%+v", fn.Outputs)
	}
	if ret.WireType != "int*" {
		t.Fatalf("return WireType = %q", ret.WireType)
	}
	if ret.GoType == "int*" {
		t.Fatalf("return GoType lost the authored C type: %q", ret.GoType)
	}

	byName := map[string]PortDef{}
	for _, p := range fn.Inputs {
		byName[p.Name] = p
	}
	if p, ok := byName["data"]; !ok || p.WireType != "int*" {
		t.Fatalf("data: %+v (want WireType int*)", p)
	}
	if p, ok := byName["msg"]; ok && p.WireType != "" {
		t.Fatalf("msg (char*) must keep the string VALUE convention: %+v", p)
	}
	for _, p := range fn.Inputs {
		if p.SliceLenName != "" && p.WireType != "" {
			t.Fatalf("slice-paired pointer gained a WireType: %+v", p)
		}
	}
	for _, p := range fn.Outputs {
		// Out-params carry the VALUE token: the pointer is calling
		// convention, the wire is the value (2026-07-11 semantics).
		// Português: Out-params carregam o token de VALOR: o ponteiro é
		// convenção de chamada, o fio é o valor.
		if p.Name == "written" && p.WireType != "int32" {
			t.Fatalf("out-param must expose the VALUE token: %+v", p)
		}
	}
}

// TestParseC_PlainIntPointer pins the K&R plain-type coverage that the
// 2026-07-11 field report exposed: an `int *` out-param must split into a
// VALUE output of wire type "int" (connecting straight into PrintInt), and
// an `int *` return must expose the "int*" family token.
//
// Português: Pina a cobertura de tipos K&R do report de 2026-07-11: um
// out-param `int *` deve virar output de VALOR com fio "int" (ligando
// direto no PrintInt), e um retorno `int *` deve expor o token "int*".
func TestParseC_PlainIntPointer(t *testing.T) {
	src := []byte(`
// label:Portal page size.
int *plain_probe(
	// direction:out.
	int *size_bytes,
	// doc:Raw feed.
	const long *feed
) {
	*size_bytes = 0;
	(void)feed;
	return 0;
}
`)
	def, err := ParseC(src, DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	fn := def.Functions[0]

	for _, p := range fn.Outputs {
		switch p.Name {
		case "size_bytes":
			if p.WireType != "int" {
				t.Fatalf("out-param wire must be the VALUE token, got %+v", p)
			}
			if p.GoType != "int *" {
				t.Fatalf("out-param GoType must stay authored, got %q", p.GoType)
			}
		case "return":
			if p.WireType != "int*" {
				t.Fatalf("plain int* return WireType = %q", p.WireType)
			}
		}
	}
	for _, p := range fn.Inputs {
		if p.Name == "feed" && p.WireType != "int*" {
			t.Fatalf("const long* input WireType = %q (%+v)", p.WireType, p)
		}
	}
}
