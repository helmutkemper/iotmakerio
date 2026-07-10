// codeGen_print_test.go — Print sink devices: codegen end to end.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// StatementPrint{Int,String,Bool} wired to constants at the global scope:
//
//   - the graph builder reads the "prefix" and "format" properties;
//   - the IR emitter (emitPrint) resolves the "value" input and appends
//     OpPrint with Type + Meta{prefix,format};
//   - the Go backend renders fmt.Printf (the bool "onezero" variant exercises
//     the named-temp path — Go has no ternary);
//   - the C backend renders printf with widened casts (the bool variant
//     exercises the ternary path), validated by an actual gcc compile.
//
// The scene deliberately picks the three trickiest paths: int with a prefix
// (format-string assembly), string (no format variant), and bool "onezero"
// (multi-line emission in Go, ternary in C).
//
// Português: Devices Print ligados a constantes no escopo global, de ponta a
// ponta: builder lê "prefix"/"format", emitPrint resolve a entrada e emite
// OpPrint, o backend Go vira fmt.Printf (bool "onezero" exercita o temp
// nomeado) e o C vira printf com casts (bool exercita o ternário), validado
// por compilação real com gcc. A cena escolhe de propósito os três caminhos
// mais delicados: int com prefixo, string (sem variante) e bool "onezero".

package codegen

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const scenePrintSinks = `{
  "metadata": { "schemaVersion": "1.1", "camera": {"x":0,"y":0,"zoom":1}, "canvas":{"w":1200,"h":800} },
  "devices": [
    {
      "id": "constInt_1", "type": "StatementConstInt", "kind": "simple",
      "properties": { "value": 27 },
      "position": { "x": 40, "y": 60 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 40, "y": 60, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "int", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 160, "y": 97 },
          "connections": [{ "wireId": "w_int", "targetDevice": "printInt_1", "targetPort": "value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "printInt_1", "type": "StatementPrintInt", "kind": "simple",
      "properties": { "prefix": "temp", "format": "decimal" },
      "position": { "x": 300, "y": 60 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 300, "y": 60, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 300, "y": 97 },
          "connections": [{ "wireId": "w_int", "targetDevice": "constInt_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },

    {
      "id": "constStr_1", "type": "StatementConstString", "kind": "simple",
      "properties": { "value": "Ana" },
      "position": { "x": 40, "y": 200 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 40, "y": 200, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "string", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 160, "y": 237 },
          "connections": [{ "wireId": "w_str", "targetDevice": "printStr_1", "targetPort": "value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "printStr_1", "type": "StatementPrintString", "kind": "simple",
      "properties": { "prefix": "name", "format": "" },
      "position": { "x": 300, "y": 200 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 300, "y": 200, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "value", "dataType": "string", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 300, "y": 237 },
          "connections": [{ "wireId": "w_str", "targetDevice": "constStr_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },

    {
      "id": "constBool_1", "type": "StatementBool", "kind": "simple",
      "properties": { "value": true },
      "position": { "x": 40, "y": 340 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 40, "y": 340, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "output", "dataType": "bool", "isOutput": true, "acceptNotConnected": true,
          "position": { "x": 160, "y": 377 },
          "connections": [{ "wireId": "w_bool", "targetDevice": "printBool_1", "targetPort": "value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "printBool_1", "type": "StatementPrintBool", "kind": "simple",
      "properties": { "prefix": "alarm", "format": "onezero" },
      "position": { "x": 300, "y": 340 }, "size": { "width": 120, "height": 74 },
      "outerBBox": { "x": 300, "y": 340, "width": 120, "height": 74 }, "innerBBox": null,
      "connectors": [
        { "port": "value", "dataType": "bool", "isOutput": false, "acceptNotConnected": true,
          "position": { "x": 300, "y": 377 },
          "connections": [{ "wireId": "w_bool", "targetDevice": "constBool_1", "targetPort": "output" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    }
  ],
  "wires": [
    { "id": "w_int",  "from": { "device": "constInt_1",  "port": "output" }, "to": { "device": "printInt_1",  "port": "value" }, "dataType": "int" },
    { "id": "w_str",  "from": { "device": "constStr_1",  "port": "output" }, "to": { "device": "printStr_1",  "port": "value" }, "dataType": "string" },
    { "id": "w_bool", "from": { "device": "constBool_1", "port": "output" }, "to": { "device": "printBool_1", "port": "value" }, "dataType": "bool" }
  ]
}`

// TestPrintSinksGo asserts the Go backend renders one fmt.Printf per sink,
// with the prefix in the format string, and that the bool "onezero" variant
// produces the named temp. Syntactic validity via go/parser.
//
// Português: O backend Go emite um fmt.Printf por sink, com o prefixo na
// string de formato; a variante bool "onezero" produz o temp nomeado.
// Validade sintática via go/parser.
func TestPrintSinksGo(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scenePrintSinks),
		Language: "go",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors generating Go:\n%s", strings.Join(resp.Errors, "\n"))
	}

	for _, want := range []string{
		`fmt.Printf("temp %d\n", `,
		`fmt.Printf("name %s\n", `,
		"AsInt := 0",
		`fmt.Printf("alarm %d\n", `,
		`"fmt"`,
	} {
		if !strings.Contains(resp.Code, want) {
			t.Errorf("Go print missing %q in:\n%s", want, resp.Code)
		}
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", resp.Code, parser.AllErrors); err != nil {
		t.Errorf("generated Go does not parse: %v\n%s", err, resp.Code)
	}
}

// TestPrintSinksC asserts the C backend renders one printf per sink — the
// widened cast for int, the plain %s for string, the ternary for bool — and
// that <stdio.h> ships. When gcc is available, the unit must compile and link
// to a runnable binary.
//
// Português: O backend C emite um printf por sink — cast alargado no int, %s
// na string, ternário no bool — e o <stdio.h> viaja. Com gcc disponível, a
// unidade compila e linka.
func TestPrintSinksC(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scenePrintSinks),
		Language: "c",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors generating C:\n%s", strings.Join(resp.Errors, "\n"))
	}
	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; got %d entries", len(resp.Files))
	}

	for _, want := range []string{
		`printf("temp %ld\n", (long)`,
		`printf("name %s\n", `,
		` ? 1 : 0);`,
		"#include <stdio.h>",
	} {
		if !strings.Contains(mainC, want) {
			t.Errorf("C print missing %q in:\n%s", want, mainC)
		}
	}

	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skipf("gcc not available, skipping compile check: %v", err)
	}
	dir := t.TempDir()
	var cFiles []string
	for name, content := range resp.Files {
		p := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
			t.Fatalf("mkdir for %s: %v", name, mkErr)
		}
		if wErr := os.WriteFile(p, []byte(content), 0o644); wErr != nil {
			t.Fatalf("write %s: %v", name, wErr)
		}
		if strings.HasSuffix(name, ".c") {
			cFiles = append(cFiles, p)
		}
	}
	bin := filepath.Join(dir, "print_gen")
	args := append([]string{"-std=c99", "-Wall", "-Wextra", "-I", dir, "-o", bin}, cFiles...)
	out, err := exec.Command(gcc, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("gcc failed: %v\n%s\n--- main.c ---\n%s", err, out, mainC)
	}
}
