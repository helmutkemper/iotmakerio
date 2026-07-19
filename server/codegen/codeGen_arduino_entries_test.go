// server/codegen/codeGen_arduino_entries_test.go
//
// Reserved-name entry points (Arduino model): on a profile with
// ProvidesEntryPoints, functions named setup/loop/serialEvent drop
// `static` (external linkage — the core calls them), silence the
// uncalled warning (the runtime IS the caller), forbid tunnel
// signatures, and omit main() when the trunk is empty. On any other
// profile they are ordinary functions. Português: Pontos de entrada de
// nome reservado no modelo Arduino — sem static, sem aviso de
// uncalled, sem assinatura de túnel, e sem main() com tronco vazio;
// nos demais profiles são funções comuns.
package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"server/codegen/diagnostics"
)

// arduinoEntryScene fabricates setup+loop function containers. The
// metadata fields select the profile path (empty target = the Arduino
// UNO preset, the house default). withParamTunnel adds a left tunnel to
// setup — the forbidden shape. Português: Fabrica setup+loop; metadata
// escolhe o profile (target vazio = preset Arduino UNO, o default da
// casa); withParamTunnel adiciona o túnel proibido ao setup.
func arduinoEntryScene(metadata string, withParamTunnel bool) string {
	tunnelDev := ""
	tunnelWire := ""
	if withParamTunnel {
		tunnelDev = `,
    {
      "id": "tunnel_p", "type": "StatementTunnel", "kind": "simple", "stage": "backend",
      "properties": { "label": "speed", "tunnelParent": "fn_setup", "tunnelSide": "left" },
      "position": { "x": 0, "y": 60 }, "size": { "width": 18, "height": 18 },
      "connectors": [
        { "port": "in", "dataType": "int", "isOutput": false, "connections": [] },
        { "port": "out", "dataType": "int", "isOutput": true,
          "connections": [{ "wireId": "wp", "targetDevice": "printInt_1", "targetPort": "value" }] }
      ],
      "containment": { "isContainer": false, "status": "free" }
    },
    {
      "id": "printInt_1", "type": "StatementPrintInt", "kind": "simple", "stage": "backend",
      "properties": {},
      "position": { "x": 60, "y": 60 }, "size": { "width": 120, "height": 74 },
      "connectors": [
        { "port": "value", "dataType": "int", "isOutput": false,
          "connections": [{ "wireId": "wp", "targetDevice": "tunnel_p", "targetPort": "out" }] }
      ],
      "containment": { "isContainer": false, "parent": "fn_setup", "status": "contained" }
    }`
		tunnelWire = `{ "id": "wp", "from": { "device": "tunnel_p", "port": "out" }, "to": { "device": "printInt_1", "port": "value" }, "dataType": "int" }`
	}
	setupChildren := `[]`
	if withParamTunnel {
		setupChildren = `["printInt_1"]`
	}
	return fmt.Sprintf(`{
  "version": "1.0",
  "metadata": %s,
  "devices": [
    {
      "id": "fn_setup", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "setup" },
      "position": { "x": 0, "y": 0 }, "size": { "width": 300, "height": 200 },
      "connectors": [],
      "containment": { "isContainer": true, "children": %s }
    },
    {
      "id": "fn_loop", "type": "StatementFunction", "kind": "complex", "stage": "backend",
      "properties": { "functionName": "loop" },
      "position": { "x": 0, "y": 260 }, "size": { "width": 300, "height": 200 },
      "connectors": [],
      "containment": { "isContainer": true, "children": [] }
    }%s
  ],
  "wires": [%s]
}`, metadata, setupChildren, tunnelDev, tunnelWire)
}

func TestArduinoReservedEntries(t *testing.T) {
	t.Run("uno preset: external linkage, no main, no uncalled noise", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:    json.RawMessage(arduinoEntryScene(`{ "language": "c" }`, false)),
			Language: "c",
		})
		code := generatedCode(resp)
		if !strings.Contains(code, "void setup(void) {") ||
			!strings.Contains(code, "void loop(void) {") {
			t.Fatalf("entry headers missing; got:\n%s", code)
		}
		if strings.Contains(code, "static void setup") ||
			strings.Contains(code, "static void loop") {
			t.Fatalf("entries must have external linkage (no static); got:\n%s", code)
		}
		if strings.Contains(code, "int main(void)") {
			t.Fatalf("the core owns main() — ours must be omitted; got:\n%s", code)
		}
		for _, d := range resp.Diagnostics {
			if d.Kind == diagnostics.KindFunctionUncalled {
				t.Fatalf("uncalled warning must be silenced for entries; got: %s", d.Message)
			}
		}
	})

	t.Run("portable profile: ordinary functions, main stays", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene: json.RawMessage(arduinoEntryScene(
				`{ "language": "c", "targetProfile": "portable" }`, false)),
			Language: "c",
		})
		code := generatedCode(resp)
		if !strings.Contains(code, "static void setup(void)") {
			t.Fatalf("portable keeps static linkage; got:\n%s", code)
		}
		if !strings.Contains(code, "int main(void)") {
			t.Fatalf("portable keeps main(); got:\n%s", code)
		}
		found := false
		for _, d := range resp.Diagnostics {
			if d.Kind == diagnostics.KindFunctionUncalled {
				found = true
			}
		}
		if !found {
			t.Fatalf("portable keeps the honest uncalled warning")
		}
	})

	t.Run("tunnel signature on an entry errors", func(t *testing.T) {
		resp := Generate(context.Background(), Request{
			Scene:    json.RawMessage(arduinoEntryScene(`{ "language": "c" }`, true)),
			Language: "c",
		})
		found := false
		for _, d := range resp.Diagnostics {
			if d.Kind == diagnostics.KindFunctionSignature &&
				d.Severity == diagnostics.SeverityError &&
				strings.Contains(d.Message, "takes no parameters") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected the void(void) guard error; diags: %+v", resp.Diagnostics)
		}
	})
}
