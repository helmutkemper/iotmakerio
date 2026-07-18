// server/codegen/codeGen_data_blob_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"server/codegen/blackbox"
)

// TestDataBlob_C99EndToEnd: the maker-data devices reach the artefact.
// A Data · Text ("hi", null-terminated) and a Data · File (3 raw bytes)
// each feed one instance of a slice-port C function; main.c must carry
// the two FILE-SCOPE flash arrays (helpers block once, NUL present but
// never counted, `ul` lengths) and the two calls with the (pointer,
// length) pair expanded by the slice `#` protocol.
// Português: Os devices de dados do maker chegam ao artefato: dois arrays
// de flash em escopo de arquivo (helpers uma vez, NUL presente mas nunca
// contado, tamanhos `ul`) e as duas chamadas com o par expandido.
func TestDataBlob_C99EndToEnd(t *testing.T) {
	sinkSrc := []blackbox.FileEntry{
		{Path: "sink.c", Content: `
// label:Blob sink.
void blob_sink(
	// slice:n.
	const uint8_t *d,
	unsigned long n
) { (void)d; (void)n; }
`}}
	def, perr := blackbox.ParseCFiles(sinkSrc, blackbox.DefaultParserLimits())
	if perr != nil {
		t.Logf("parse warnings: %v", perr)
	}
	def.ID = "p"
	def.CodeID = "10"

	filePayload, _ := json.Marshal(map[string]string{
		"name":    "logo.bin",
		"dataUrl": "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
	})

	scene := `{
  "version": "1.0",
  "metadata": { "language": "c" },
  "devices": [
    {
      "id": "dataText_1", "type": "StatementDataText", "kind": "simple", "stage": "backend",
      "properties": { "text": "hi", "nullTerminated": "true", "language": "yaml" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "output", "dataType": "[]uint8", "isOutput": true,
          "connections": [{ "wireId": "w1", "targetDevice": "sink_1", "targetPort": "d" }] }
      ]
    },
    {
      "id": "dataFile_1", "type": "StatementDataFile", "kind": "simple", "stage": "backend",
      "properties": { "file": ` + string(mustJSON(t, string(filePayload))) + ` },
      "position": { "x": 0, "y": 40 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "output", "dataType": "[]uint8", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "sink_2", "targetPort": "d" }] }
      ]
    },
    {
      "id": "sink_1", "type": "BlackBoxblob_sink:", "kind": "simple", "stage": "backend",
      "properties": { "instanceId": "c99fn_0" },
      "position": { "x": 60, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "d", "dataType": "[]uint8", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "dataText_1", "targetPort": "output" }] }
      ]
    },
    {
      "id": "sink_2", "type": "BlackBoxblob_sink:", "kind": "simple", "stage": "backend",
      "properties": { "instanceId": "c99fn_1" },
      "position": { "x": 60, "y": 40 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "d", "dataType": "[]uint8", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "dataFile_1", "targetPort": "output" }] }
      ]
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "dataText_1", "port": "output" }, "to": { "device": "sink_1", "port": "d" }, "dataType": "[]uint8" },
    { "id": "w2", "from": { "device": "dataFile_1", "port": "output" }, "to": { "device": "sink_2", "port": "d" }, "dataType": "[]uint8" }
  ]
}`

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "c",
		BlackBoxDefs: map[string]*blackbox.BlackBoxDef{
			"blob_sink": def,
		},
	})
	for _, d := range resp.Diagnostics {
		t.Logf("diag [%s] %s", d.Severity, d.Message)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("Errors: %v", resp.Errors)
	}
	mainC := resp.Files["main.c"]
	if mainC == "" {
		mainC = resp.Files["main.go"]
	}
	t.Logf("main.c:\n%s", mainC)

	// Helpers block: exactly ONCE, file scope.
	if n := strings.Count(mainC, "#ifndef IOTM_ASSET_HELPERS"); n != 1 {
		t.Fatalf("helpers block count = %d, want 1", n)
	}

	// Text blob: 'h' 'i' NUL present, length counts TWO.
	assertContains(t, mainC, "dataText1[] IOTM_ASSET_ATTR = {")
	assertContains(t, mainC, "0x68, 0x69, 0x00,")
	assertContains(t, mainC, "dataText1_len = 2ul;")

	// File blob: the three raw bytes, length three, name in the comment.
	assertContains(t, mainC, `Data · File "logo.bin"`)
	assertContains(t, mainC, "0x01, 0x02, 0x03,")
	assertContains(t, mainC, "dataFile1_len = 3ul;")

	// Calls: the slice pair expanded, prefixed symbol.
	assertContains(t, mainC, "iotm_10_blob_sink(dataText1, dataText1_len);")
	assertContains(t, mainC, "iotm_10_blob_sink(dataFile1, dataFile1_len);")
}

// mustJSON re-marshals a string as a JSON string literal for embedding.
func mustJSON(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDataBlob_GoBackend — the Go twin of the C test above (Kemper
// audit 2026-07-18: "em C fizemos o embutido. Já replica em golang?").
// The mechanism DOES replicate — backend/golang/emit.go:emitDataBlob
// renders each maker-data asset as `var name = []byte{…}` (no _len
// companion: Go's len() serves) — but until this test the Go path had
// no safety net; only the C shape was pinned. Consumers here are the
// native StatementPrintByteArray (the C test's blob_sink is a C99
// black box with no Go twin — that ecosystem gap is recorded in
// docs/CLAUDE_KNOWN_ISSUES.md).
// Português: O gêmeo Go do teste C acima. O mecanismo replica —
// emitDataBlob gera `var nome = []byte{…}` (sem _len: len() do Go
// serve) — mas até este teste o caminho Go estava sem rede. O
// consumidor aqui é o StatementPrintByteArray nativo (o blob_sink do
// teste C é black-box C99 sem gêmeo Go — lacuna registrada no
// CLAUDE_KNOWN_ISSUES.md).
func TestDataBlob_GoBackend(t *testing.T) {
	filePayload, _ := json.Marshal(map[string]string{
		"name":    "logo.bin",
		"dataUrl": "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
	})

	scene := `{
  "version": "1.0",
  "metadata": { "language": "go" },
  "devices": [
    {
      "id": "dataText_1", "type": "StatementDataText", "kind": "simple", "stage": "backend",
      "properties": { "text": "hi", "nullTerminated": "true", "language": "yaml" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "output", "dataType": "[]uint8", "isOutput": true,
          "connections": [{ "wireId": "w1", "targetDevice": "print_1", "targetPort": "value" }] }
      ]
    },
    {
      "id": "dataFile_1", "type": "StatementDataFile", "kind": "simple", "stage": "backend",
      "properties": { "file": ` + string(mustJSON(t, string(filePayload))) + ` },
      "position": { "x": 0, "y": 40 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "output", "dataType": "[]uint8", "isOutput": true,
          "connections": [{ "wireId": "w2", "targetDevice": "print_2", "targetPort": "value" }] }
      ]
    },
    {
      "id": "print_1", "type": "StatementPrintByteArray", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 60, "y": 0 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "value", "dataType": "[]uint8", "isOutput": false,
          "connections": [{ "wireId": "w1", "targetDevice": "dataText_1", "targetPort": "output" }] }
      ]
    },
    {
      "id": "print_2", "type": "StatementPrintByteArray", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 60, "y": 40 }, "size": { "width": 10, "height": 10 },
      "connectors": [
        { "port": "value", "dataType": "[]uint8", "isOutput": false,
          "connections": [{ "wireId": "w2", "targetDevice": "dataFile_1", "targetPort": "output" }] }
      ]
    }
  ],
  "wires": [
    { "id": "w1", "from": { "device": "dataText_1", "port": "output" }, "to": { "device": "print_1", "port": "value" }, "dataType": "[]uint8" },
    { "id": "w2", "from": { "device": "dataFile_1", "port": "output" }, "to": { "device": "print_2", "port": "value" }, "dataType": "[]uint8" }
  ]
}`

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "go",
	})
	for _, d := range resp.Diagnostics {
		t.Logf("diag [%s] %s", d.Severity, d.Message)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("Errors: %v", resp.Errors)
	}
	mainGo := resp.Files["main.go"]
	if mainGo == "" {
		t.Fatalf("main.go absent from response files: %v", keysOf(resp.Files))
	}
	t.Logf("main.go:\n%s", mainGo)

	// Text blob: 'h' 'i' NUL as a Go byte-slice var (no _len companion —
	// Go's len() serves). Português: Blob de texto como var []byte, sem
	// companheiro _len.
	assertContains(t, mainGo, "var dataText1 = []byte{0x68, 0x69, 0x00}")

	// File blob: source name in the comment, the three raw bytes.
	assertContains(t, mainGo, `Data blob "logo.bin"`)
	assertContains(t, mainGo, "var dataFile1 = []byte{0x01, 0x02, 0x03}")

	// Consumption: each blob referenced beyond its declaration.
	if n := strings.Count(mainGo, "dataText1"); n < 2 {
		t.Fatalf("dataText1 referenced %d times, want >= 2 (declaration + use)", n)
	}
	if n := strings.Count(mainGo, "dataFile1"); n < 2 {
		t.Fatalf("dataFile1 referenced %d times, want >= 2 (declaration + use)", n)
	}
}

// keysOf lists a string map's keys for failure messages.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
