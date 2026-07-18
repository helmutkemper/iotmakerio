// /server/codegen/codeGen_c_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package codegen

// codeGen_c_test.go — Integration tests for the C99 backend.
//
// This file is the C counterpart of codeGen_test.go. It reuses the same
// scene fixtures (sceneLinear, sceneLoop) declared there because the
// goal is to assert that the C backend processes the same scene the
// Go backend already handles, not to invent new scenes.
//
// Each test follows the format established by codeGen_test.go: drive
// Generate end-to-end, then assert substrings in the resulting source.
// Substring assertions are deterministic and resilient to formatting
// tweaks that don't change semantics.
//
// Phase 1 / Task 1 scope:
//
//   - Prove that req.Language="c" routes to the new backend and that
//     the response carries a Files map (not a Code string).
//   - Prove that req.Language="go" is unaffected — Code is populated
//     and Files remains nil. This is the regression guard that lets
//     us extend the C backend without fear of contaminating the Go
//     path.
//
// Subsequent tasks add tests that assert actual translated C content
// (declarations, arithmetic, control flow). For now we only validate
// the routing and shape of the response.
//
// Português:
//
//	Testes de integração do backend C. Reusa as cenas de
//	codeGen_test.go. Tarefa 1 só valida roteamento e formato da
//	Response — testes de tradução real entram nas tarefas seguintes.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestCBackend_Scaffolding asserts the C backend is reachable through
// the Generate pipeline and returns a Files map with a non-empty
// main.c. The body is the Phase 1 placeholder, so the assertions stay
// at the level of "the file exists and looks like minimal valid C".
//
// Português: Garante que o backend C é alcançável e devolve main.c
// não vazio com forma de programa C mínimo.
func TestCBackend_Scaffolding(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLinear),
		Language: "c",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors from C backend: %v", resp.Errors)
	}

	// The C path populates Files, not Code. This is the contract that
	// distinguishes single-file backends (Go) from multi-file backends
	// (C). The next assertion is the inverse on the Go path.
	if resp.Files == nil {
		t.Fatal("expected resp.Files to be non-nil for language=\"c\"")
	}

	mainC, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected Files to contain key %q; got %d entries", "main.c", len(resp.Files))
	}

	// The scaffold is a minimal valid C program. These two substrings
	// confirm it: the entry point and a return. Later tasks replace
	// these with assertions on actual translated code.
	assertContains(t, mainC, "int main(void)")
	assertContains(t, mainC, "return 0;")
}

// TestCBackend_GoPathUnchanged is the regression guard.
//
// It calls Generate with the exact same scene as TestCBackend_Scaffolding
// but with Language="go" and asserts the legacy behaviour: resp.Files["main.go"] is
// populated, resp.Files is nil. If a future change accidentally routes
// the Go path through the multi-file branch, this test catches it.
//
// The existing TestGoCodegen in codeGen_test.go covers the actual
// content of the Go output; we only need the shape check here.
//
// Português: Garante que language="go" continua devolvendo Code (string)
// e nunca Files. Pega regressão se algo cruzar os caminhos.
func TestCBackend_GoPathUnchanged(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLinear),
		Language: "go",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors from Go backend: %v", resp.Errors)
	}

	if resp.Files["main.go"] == "" {
		t.Fatal("expected resp.Files[\"main.go\"] to be populated for language=\"go\"")
	}

	// §7.4 (2026-07-16): Go now ships as Files{"main.go": …} — this guard
	// inverted with the contract; Code is legacy and stays empty.
	// Português: O guarda inverteu com o contrato — Go viaja no mapa.
	if len(resp.Files) != 1 {
		t.Fatalf("expected exactly the main.go entry for language=\"go\"; got %d entries", len(resp.Files))
	}
	if resp.Code != "" {
		t.Fatal("resp.Code is a legacy field and must stay empty since §7.4")
	}
}

// TestCBackend_EmptyLanguageStillGoesGo guards the historical default:
// an empty Language field means "go", inherited from the earliest
// clients that did not send the field. The C backend must not steal
// this default away from Go.
//
// Português: Language vazio cai em Go por compat com clientes antigos.
// O backend C não pode tomar o default.
func TestCBackend_EmptyLanguageStillGoesGo(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLinear),
		Language: "",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors with empty language: %v", resp.Errors)
	}

	if resp.Files["main.go"] == "" {
		t.Fatal("expected resp.Files[\"main.go\"] to be populated when Language is empty (Go default)")
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected exactly the main.go entry when Language is empty; got %d entries", len(resp.Files))
	}
}

// =====================================================================
//  Target profile resolution
// =====================================================================

// TestCBackend_ProfileResolution drives the end-to-end pipeline with
// scenes whose metadata.targetProfile field varies, and asserts that
// the resolved profile reaches the emitter and observably shapes its
// output. The placeholder main.c carries the profile name and its
// IntType/FloatType in the file header, so each case here is checked
// against the substrings the test expects to find — or NOT find, in
// the case of fallback profiles.
//
// The cases collectively exercise:
//
//   - Every shipped profile by canonical name.
//   - The "field absent" path (older scenes that pre-date the
//     targetProfile metadata key) — must fall back to arduino_uno.
//   - The "garbage name" path — must also fall back to arduino_uno.
//
// Real translation of IR opcodes happens in later tasks; this test
// only proves the profile makes it through.
//
// Português:
//
//	Dirige o pipeline com cenas variando metadata.targetProfile e
//	checa se o perfil resolvido chega ao backend e aparece no header
//	do main.c placeholder. Cobre todos os perfis enviados, ausência
//	do campo (compat com cenas antigas) e nomes inválidos (fallback).
func TestCBackend_ProfileResolution(t *testing.T) {
	cases := []struct {
		// label is the t.Run subtest name.
		label string
		// sceneProfile is the literal value stored in
		// metadata.targetProfile, or the empty string to omit the
		// field entirely (simulating an older scene).
		sceneProfile string
		// wantHeader is a substring that must appear in main.c when
		// the resolved profile matches the expectation. We use the
		// "Target profile: <name>" line emitted by buildScaffoldMainC.
		wantHeader string
		// wantIntType is a substring asserted in the header to
		// guarantee the profile's type decisions reached the
		// emitter, not just the name.
		wantIntType string
	}{
		{
			label:        "arduino_uno_explicit",
			sceneProfile: "arduino_uno",
			wantHeader:   "Target profile: arduino_uno",
			wantIntType:  "int32_t",
		},
		{
			label:        "cortex_m_explicit",
			sceneProfile: "cortex_m",
			wantHeader:   "Target profile: cortex_m",
			wantIntType:  "int32_t",
		},
		{
			label:        "pi_linux_explicit",
			sceneProfile: "pi_linux",
			wantHeader:   "Target profile: pi_linux",
			wantIntType:  "int64_t",
		},
		{
			label:        "portable_explicit",
			sceneProfile: "portable",
			wantHeader:   "Target profile: portable",
			wantIntType:  "int32_t",
		},
		{
			label:        "field_absent_defaults_to_arduino",
			sceneProfile: "", // omits the metadata key entirely
			wantHeader:   "Target profile: arduino_uno",
			wantIntType:  "int32_t",
		},
		{
			label:        "garbage_name_defaults_to_arduino",
			sceneProfile: "this_does_not_exist",
			wantHeader:   "Target profile: arduino_uno",
			wantIntType:  "int32_t",
		},
	}

	for _, tc := range cases {
		tc := tc // capture loop variable for parallel-safe t.Run
		t.Run(tc.label, func(t *testing.T) {
			scene := buildSceneWithProfile(tc.sceneProfile)
			resp := Generate(context.Background(), Request{
				Scene:    json.RawMessage(scene),
				Language: "c",
			})

			if len(resp.Errors) > 0 {
				t.Fatalf("unexpected errors: %v", resp.Errors)
			}

			mainC, ok := resp.Files["main.c"]
			if !ok {
				t.Fatalf("expected main.c in Files; got %d entries", len(resp.Files))
			}

			assertContains(t, mainC, tc.wantHeader)
			assertContains(t, mainC, tc.wantIntType)
		})
	}
}

// TestCBackend_PiLinuxDifferentiationFromArduino is a focused
// integration assertion that the int64_t / int32_t split actually
// surfaces in the emitted file when only the profile name differs.
// This is the test the design plan explicitly called for: "three
// scenes with different targetProfile values where the output differs
// — one int32_t, another int64_t".
//
// Português:
//
//	Garante que mudar só o nome do perfil produz saída diferente —
//	especialmente int32_t vs int64_t entre arduino_uno e pi_linux.
func TestCBackend_PiLinuxDifferentiationFromArduino(t *testing.T) {
	arduinoResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(buildSceneWithProfile("arduino_uno")),
		Language: "c",
	})
	if len(arduinoResp.Errors) > 0 {
		t.Fatalf("arduino_uno: unexpected errors: %v", arduinoResp.Errors)
	}

	piResp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(buildSceneWithProfile("pi_linux")),
		Language: "c",
	})
	if len(piResp.Errors) > 0 {
		t.Fatalf("pi_linux: unexpected errors: %v", piResp.Errors)
	}

	arduinoMain := arduinoResp.Files["main.c"]
	piMain := piResp.Files["main.c"]

	if arduinoMain == piMain {
		t.Fatal("expected main.c to differ between arduino_uno and pi_linux profiles; got identical output")
	}

	assertContains(t, arduinoMain, "int32_t")
	assertNotContains(t, arduinoMain, "int64_t")

	assertContains(t, piMain, "int64_t")
	assertNotContains(t, piMain, "int32_t")
}

// =====================================================================
//  End-to-end arithmetic — sceneLinear through the C backend
// =====================================================================

// TestCBackend_SceneLinear_ArduinoUno is the pipeline-level
// counterpart of the emit_test.go arithmetic suite. It drives the
// real sceneLinear fixture (declared in codeGen_test.go) through
// Generate and asserts the full C output matches what we expect from
// the IR emitter chained into the C backend.
//
// sceneLinear is a deliberately minimal scene: two ConstInt devices
// feeding a single Add, whose output reaches a Gauge. The Gauge is
// IDE-only (display device) and therefore the C backend ignores its
// OpOutput — see docs/CODEGEN_ANSI_C.md §7. The expected C contains
// the three computed lines and NO printf, fmt, or display-related
// output.
//
// This is the test the design plan calls "Definition of done" for
// Task 6: "sceneLinear test passes. Gauge ignored. Verify absence of
// printf, fmt, or any reference to display output in the C."
//
// Português:
//
//	Versão pipeline-level dos testes de aritmética. Usa o sceneLinear
//	já existente (dois ConstInt → Add → Gauge), passa pelo Generate
//	inteiro, e checa que o C resultante tem as três linhas computadas
//	e nenhum vestígio do Gauge (display é IDE-only, ignorado).
func TestCBackend_SceneLinear_ArduinoUno(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLinear),
		Language: "c",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", resp.Errors)
	}

	main, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; got %d entries", len(resp.Files))
	}

	t.Log("Generated C:\n" + main)

	// The three lines computed from the scene.
	assertContains(t, main, "int32_t constInt1 = 10L;")
	assertContains(t, main, "int32_t constInt3 = 20L;")
	assertContains(t, main, "int32_t add1 = constInt1 + constInt3;")

	// Display absence — the Gauge in sceneLinear has label "total",
	// and the Go backend emits fmt.Println("total", add1). The C
	// backend must do none of that.
	assertNotContains(t, main, "printf")
	assertNotContains(t, main, "puts")
	assertNotContains(t, main, "fmt")
	assertNotContains(t, main, "Println")
	assertNotContains(t, main, "total") // Gauge label — must not appear
}

// TestCBackend_SceneLinear_PiLinux exercises the same scene under
// the 64-bit profile, confirming that the profile flows through
// every arithmetic emission and not just the Const ones.
//
// To switch profiles we cannot rely on sceneLinear's metadata
// (which has no targetProfile field — it falls back to arduino_uno),
// so we splice the field in via a tiny string replacement. This is
// safe because the metadata object is a single well-formed JSON
// object and we know exactly where its closing brace sits in the
// fixture.
//
// Português:
//
//	Mesma cena no perfil pi_linux pra confirmar que o profile flui
//	por toda emissão. sceneLinear não tem targetProfile no metadata
//	— injetamos via substituição pontual de string.
func TestCBackend_SceneLinear_PiLinux(t *testing.T) {
	// Splice "targetProfile": "pi_linux" into the metadata object.
	// The sceneLinear fixture's metadata ends with the camera
	// object; we insert just after the closing brace of camera.
	scene := strings.Replace(
		sceneLinear,
		`"camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 }`,
		`"camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 }, "targetProfile": "pi_linux"`,
		1,
	)

	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(scene),
		Language: "c",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", resp.Errors)
	}

	main := resp.Files["main.c"]

	// All declarations and arithmetic must use the 64-bit type and
	// LL suffix dictated by the pi_linux profile.
	assertContains(t, main, "int64_t constInt1 = 10LL;")
	assertContains(t, main, "int64_t constInt3 = 20LL;")
	assertContains(t, main, "int64_t add1 = constInt1 + constInt3;")

	// And no traces of the smaller-width arduino_uno equivalent.
	assertNotContains(t, main, "int32_t")
	assertNotContains(t, main, "10L,") // 10L without LL would be wrong
}

// =====================================================================
//  End-to-end loop — sceneLoop through the C backend
// =====================================================================

// TestCBackend_SceneLoop_ArduinoUno drives the existing sceneLoop
// fixture (declared in codeGen_test.go) through the C backend and
// asserts the full output: a promoted variable outside the loop, the
// while body with arithmetic and a comparison, a conditional break,
// and the Gauge — outside the loop — silently dropped because the C
// backend ignores OpOutput.
//
// This is the integration test for Task 9: it requires every emitter
// from Tasks 5-9 working together to produce a syntactically and
// semantically valid C program. If anything regresses in Const, Var,
// Assign, Arithmetic, Comparison, or Loop, this test catches it.
//
// Expected C structure (arduino_uno profile):
//
//	int32_t add1;                                      ← OpVar (outer scope)
//	while (1) {
//	    int32_t constInt1 = 10L;
//	    int32_t constInt3 = 20L;
//	    add1 = constInt1 + constInt3;                  ← reused, no type
//	    int32_t constInt5 = 100L;
//	    bool compare1 = add1 > constInt5;
//	    if (compare1) {
//	        break;
//	    }
//	}
//	(no Gauge output — IDE-only)
//
// Português:
//
//	Teste de integração end-to-end com sceneLoop. Verifica VAR
//	promovido pra escopo externo, corpo do while com aritmética e
//	comparação, break condicional, e ausência do Gauge (display
//	IDE-only, OpOutput ignorado).
func TestCBackend_SceneLoop_ArduinoUno(t *testing.T) {
	resp := Generate(context.Background(), Request{
		Scene:    json.RawMessage(sceneLoop),
		Language: "c",
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", resp.Errors)
	}

	main, ok := resp.Files["main.c"]
	if !ok {
		t.Fatalf("expected main.c in Files; got %d entries", len(resp.Files))
	}

	t.Log("Generated C:\n" + main)

	// Outer scope: add1 declared (uninitialised) BEFORE the while.
	assertContains(t, main, "int32_t add1;")
	assertContains(t, main, "while (1) {")

	// Inside the loop: the two consts, the reused assignment, the
	// compare, the break-if.
	assertContains(t, main, "int32_t constInt1 = 10L;")
	assertContains(t, main, "int32_t constInt3 = 20L;")
	assertContains(t, main, "add1 = constInt1 + constInt3;")
	assertContains(t, main, "int32_t constInt5 = 100L;")
	assertContains(t, main, "bool compare1 = add1 > constInt5;")
	assertContains(t, main, "if (compare1) {")
	assertContains(t, main, "break;")

	// Critical: add1 must NOT be redeclared inside the loop. This
	// is the C-specific protection — the declared map prevents
	// "int32_t add1 = constInt1 + constInt3;" inside the body.
	if strings.Count(main, "int32_t add1") != 1 {
		t.Errorf("expected exactly one 'int32_t add1' (the OpVar declaration); got:\n%s", main)
	}

	// Order: declaration BEFORE while, while BEFORE break.
	declIdx := strings.Index(main, "int32_t add1;")
	whileIdx := strings.Index(main, "while (1) {")
	breakIdx := strings.Index(main, "break;")
	if !(declIdx < whileIdx && whileIdx < breakIdx) {
		t.Errorf("instructions out of order in main.c:\n%s", main)
	}

	// Display absence — same guarantee as TestCBackend_SceneLinear.
	assertNotContains(t, main, "printf")
	assertNotContains(t, main, "fmt")
	assertNotContains(t, main, "Println")
	assertNotContains(t, main, "total") // Gauge label
}

// buildSceneWithProfile returns a minimal valid SceneJSON whose
// metadata carries the supplied target profile name. Passing the empty
// string omits the targetProfile key entirely so the test exercises
// the "field absent" code path (older scenes from before profiles
// existed). The device list is intentionally empty — the IR is not
// inspected by the C backend in this phase, so the pipeline only
// needs to reach the backend without errors.
//
// Português:
//
//	Gera uma cena mínima carregando o targetProfile informado. String
//	vazia omite o campo (testa compat com cenas antigas). Devices
//	vazios bastam porque o backend ainda não inspeciona o IR.
func buildSceneWithProfile(profile string) string {
	profileField := ""
	if profile != "" {
		profileField = `, "targetProfile": "` + profile + `"`
	}
	return `{
		"version": "1.0",
		"metadata": {
			"density": 1,
			"canvasWidth": 800,
			"canvasHeight": 600,
			"camera": { "offsetX": 0, "offsetY": 0, "zoom": 1 }` + profileField + `
		},
		"devices": [],
		"wires": []
	}`
}
