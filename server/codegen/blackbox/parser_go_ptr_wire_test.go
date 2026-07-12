// server/codegen/blackbox/parser_go_ptr_wire_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "testing"

// TestParseGo_PointerWireTokens pins the Go twin of the C pointer-wire
// contract: scalar-pointer params and returns expose the family token on
// the wire (GoType stays authored), struct pointers keep the handle idiom,
// and error is untouched.
//
// Português: Pina o gêmeo Go do contrato de fio ponteiro do C: params e
// retornos ponteiro-escalar expõem o token de família no fio (GoType fica
// autoral), ponteiros de struct mantêm o idioma handle, e error fica
// intocado.
func TestParseGo_PointerWireTokens(t *testing.T) {
	src := []byte(`
package sensor

// Probe device.
type Probe struct{}

// Init prepares the probe.
func (p *Probe) Init() error { return nil }

// Read samples the buffer.
func (p *Probe) Read(
	// connection: mandatory.
	data *int32,
	// connection: optional.
	state *Probe,
) (*int64, error) {
	_ = data
	_ = state
	return nil, nil
}
`)
	def, err := Parse(src, DefaultParserLimits())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var read *NamedFuncDef
	for i := range def.Methods {
		if def.Methods[i].Name == "Read" {
			read = &def.Methods[i]
		}
	}
	if read == nil {
		t.Fatalf("Read method missing; methods=%+v", def.Methods)
	}

	in := map[string]PortDef{}
	for _, p := range read.Inputs {
		in[p.Name] = p
	}
	if p := in["data"]; p.WireType != "int*" || p.GoType != "*int32" {
		t.Fatalf("data: %+v (want WireType int*, GoType *int32)", p)
	}
	if p := in["state"]; p.WireType != "" {
		t.Fatalf("struct pointer must keep the handle idiom: %+v", p)
	}
	for _, p := range read.Outputs {
		if p.IsError && p.WireType != "" {
			t.Fatalf("error output gained a WireType: %+v", p)
		}
		if p.GoType == "*int64" && p.WireType != "int*" {
			t.Fatalf("return *int64: %+v (want WireType int*)", p)
		}
	}
}
