// wire/registry.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import "strings"

// registry.go — Compatibility matrix, type styles and compatibility resolution.
//
// English:
//
//	This file owns two responsibilities:
//
//	  1. Visual style per data type (DefaultTypeStyles, DefaultUnknownStyle).
//	     Each entry maps a Go type name to the WireStyle used when drawing that
//	     wire on the canvas. The manager calls getTypeStyle() which first checks
//	     per-instance overrides, then this table, then falls back to
//	     DefaultUnknownStyle (grey dashed).
//
//	  2. Type compatibility matrix (DefaultCompatibility) and the resolution
//	     functions that use it (resolveCompatibleType, findCompatibleTypes).
//	     The matrix defines type promotion rules between primitives:
//	       int → float, bool → int, etc.
//
//	IMPORTANT — Exact-match fallback rule:
//
//	     Any Go type is always compatible with itself, even when it is not in
//	     the primitive matrix. This is the rule that allows black-box components
//	     to wire complex types such as *machine.I2C, *spi.Device, or any
//	     user-defined pointer/struct.
//
//	     The fallback is implemented inside resolveCompatibleType: when the
//	     output type has no entry in the matrix, it checks whether
//	     outputType == inputType and, if so, returns ok=true.
//
//	     This means the matrix is only needed for cross-type promotions. Identical
//	     types always connect — no matrix entry required.
//
//	Style for complex types:
//
//	     Pointer types (*T) and named struct types that are not in the primitive
//	     table fall back through getTypeStyle() in manager.go to the "struct"
//	     entry defined here. This gives them a consistent visual identity
//	     (violet wire) rather than the error-indicating grey dashed line.
//
// Português:
//
//	Este arquivo gerencia duas responsabilidades:
//
//	  1. Estilo visual por tipo de dado.
//	  2. Matriz de compatibilidade de tipos e funções de resolução.
//
//	IMPORTANTE — Regra de exact-match:
//
//	     Qualquer tipo Go é sempre compatível consigo mesmo, mesmo que não
//	     esteja na matriz primitiva. Isso permite que componentes black-box
//	     conectem tipos complexos como *machine.I2C sem nenhuma entrada manual
//	     na matriz.
//
//	Estilo para tipos complexos:
//
//	     Tipos ponteiro (*T) que não estão na tabela primitiva usam o estilo
//	     "struct" definido aqui — fio violeta — em vez da linha tracejada
//	     cinza de erro.

// =====================================================================
//  Default Type Styles | Estilos Padrão por Tipo
// =====================================================================

// DefaultTypeStyles maps data type names to their default visual styles.
// Users can override these by calling Manager.SetTypeStyle().
//
// The "struct" entry acts as a catch-all for complex Go types (pointer types,
// named struct types). The manager's getTypeStyle() falls back to this entry
// when it encounters any type whose name begins with "*" or contains a dot
// (package-qualified type name) and has no specific entry here.
//
// Português:
//
//	O entry "struct" é o catch-all para tipos complexos (ponteiros, structs
//	nomeados). getTypeStyle() cai para este entry ao encontrar tipos com
//	prefixo "*" ou com ponto (tipo qualificado por pacote).
var DefaultTypeStyles = map[string]WireStyle{
	// ── Primitive scalar types ──────────────────────────────────────────
	"int": {
		StrokeColor:   "#2196F3", // blue | azul
		StrokeWidth:   2.0,
		SelectedColor: "#90CAF9",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"int64": {
		StrokeColor:   "#1565C0", // dark blue | azul escuro
		StrokeWidth:   2.0,
		SelectedColor: "#90CAF9",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"uint": {
		StrokeColor:   "#5E35B1", // deep purple | roxo profundo
		StrokeWidth:   2.0,
		SelectedColor: "#CE93D8",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"uint8": {
		StrokeColor:   "#5E35B1",
		StrokeWidth:   2.0,
		SelectedColor: "#CE93D8",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"uint16": {
		StrokeColor:   "#5E35B1",
		StrokeWidth:   2.0,
		SelectedColor: "#CE93D8",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"uint32": {
		StrokeColor:   "#5E35B1",
		StrokeWidth:   2.0,
		SelectedColor: "#CE93D8",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"uint64": {
		StrokeColor:   "#4527A0", // darkest purple | roxo mais escuro
		StrokeWidth:   2.0,
		SelectedColor: "#CE93D8",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"float": {
		// Abstract float wire — teal, matching the device badge/border accent
		// (rulesDevice KColorTypeFloat64). It was previously "#F44336", the exact
		// red of the "error" style, so a valid float wire looked like an error;
		// float is now the maker-facing default type, so it gets its own clearly
		// non-red identity. Português: fio de float abstrato — teal, igual ao
		// badge; antes era o vermelho do erro e confundia.
		StrokeColor:   "#55DDAA",
		StrokeWidth:   2.0,
		SelectedColor: "#A7EFD8",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"float64": {
		StrokeColor:   "#9E9D24", // yellow-green | amarelo-verde
		StrokeWidth:   2.0,
		SelectedColor: "#F9A825",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"string": {
		StrokeColor:   "#4CAF50", // green | verde
		StrokeWidth:   2.0,
		SelectedColor: "#A5D6A7",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"bool": {
		StrokeColor:   "#FF9800", // orange | laranja
		StrokeWidth:   2.0,
		SelectedColor: "#FFCC80",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},
	"error": {
		StrokeColor:   "#F44336", // red dashed — handle me | vermelho tracejado — me trate
		StrokeWidth:   2.0,
		DashPattern:   []float64{6, 3},
		SelectedColor: "#EF9A9A",
		SelectedWidth: 4.0,
		CornerRadius:  6.0,
	},

	// ── Slice types ─────────────────────────────────────────────────────
	"[]int": {
		StrokeColor:   "#2196F3", // blue thick | azul grosso
		StrokeWidth:   4.0,
		SelectedColor: "#90CAF9",
		SelectedWidth: 6.0,
		CornerRadius:  6.0,
	},
	"[]float": {
		// Teal thick — the collection variant of the abstract float wire, same
		// accent as the scalar "float" above (was the error red "#F44336").
		StrokeColor:   "#55DDAA",
		StrokeWidth:   4.0,
		SelectedColor: "#A7EFD8",
		SelectedWidth: 6.0,
		CornerRadius:  6.0,
	},
	"[]string": {
		StrokeColor:   "#4CAF50", // green thick | verde grosso
		StrokeWidth:   4.0,
		SelectedColor: "#A5D6A7",
		SelectedWidth: 6.0,
		CornerRadius:  6.0,
	},
	"[]bool": {
		StrokeColor:   "#FF9800", // orange thick | laranja grosso
		StrokeWidth:   4.0,
		SelectedColor: "#FFCC80",
		SelectedWidth: 6.0,
		CornerRadius:  6.0,
	},

	// ── Complex / struct / pointer types ────────────────────────────────
	//
	// "struct" is the catch-all style for complex Go types:
	//   • Pointer types: *machine.I2C, *spi.Device, *machine.Pin, etc.
	//   • Package-qualified named types: machine.I2C (without pointer).
	//   • Any user-defined struct passed between black-box components.
	//
	// The manager's getTypeStyle() redirects to this entry via styleKeyForType()
	// for any type whose name starts with "*" or contains ".".
	//
	// Violet was chosen because:
	//   • It is visually distinct from all primitive type colours.
	//   • It communicates "hardware bus / structured data" naturally.
	//   • It remains distinguishable under most colourblindness filters.
	//
	// Português:
	//   "struct" é o catch-all para tipos complexos. getTypeStyle() redireciona
	//   para este entry via styleKeyForType() para tipos com prefixo "*" ou com
	//   ponto. Violeta foi escolhido por ser visualmente distinto de todos os
	//   tipos primitivos e comunicar "barramento de hardware".
	"struct": {
		StrokeColor:   "#9C27B0", // violet | violeta — hardware bus / structured data
		StrokeWidth:   2.5,
		SelectedColor: "#CE93D8",
		SelectedWidth: 4.5,
		CornerRadius:  6.0,
	},
}

// DefaultUnknownStyle is used when a data type has no registered style and
// does not qualify for the struct/pointer fallback. Rendered as a grey dashed
// line — visually signals "something is off" without breaking the canvas.
//
// Português: Usado quando um tipo de dado não tem estilo registrado e não
// qualifica para o fallback struct/pointer. Linha tracejada cinza sinaliza
// visualmente que algo está errado sem quebrar o canvas.
var DefaultUnknownStyle = WireStyle{
	StrokeColor:   "#9E9E9E", // grey | cinza
	StrokeWidth:   2.0,
	DashPattern:   []float64{4, 4},
	SelectedColor: "#E0E0E0",
	SelectedWidth: 4.0,
	CornerRadius:  6.0,
}

// =====================================================================
//  Compatibility Matrix | Matriz de Compatibilidade
// =====================================================================

// DefaultCompatibility defines which output types can connect to which input
// types through type promotion (coercion).
//
// IMPORTANT: This matrix is for cross-type promotion rules only.
// Identical types (outputType == inputType) are ALWAYS compatible, regardless
// of whether they appear here. The exact-match fallback in resolveCompatibleType
// handles all identical-type connections — including complex Go types like
// *machine.I2C — without any entry in this table.
//
// Key = output type, Value = list of accepted input types.
// Users can add custom promotions by calling Manager.SetCompatibility().
//
// Português:
//
//	DefaultCompatibility define regras de promoção entre tipos diferentes.
//
//	IMPORTANTE: Esta matriz é apenas para promoção entre tipos DIFERENTES.
//	Tipos idênticos (outputType == inputType) são SEMPRE compatíveis, independente
//	de aparecerem aqui. O fallback de exact-match em resolveCompatibleType trata
//	todas as conexões de tipo idêntico — incluindo *machine.I2C — sem entrada.
var DefaultCompatibility = map[string][]string{
	// T6 — IDE constants feeding AUTHORED black-box parameters: the
	// abstract "int" (ConstInt) may wire into any concrete numeric
	// parameter because the Go backend casts every argument to the
	// authored type at the call site ("cast escalar" —
	// castArgsToAuthoredParams). Lossy pairs (e.g. int → uint8) are
	// allowed here and surfaced by the codegen's KindTypeLossy warning,
	// which is the layer that owns numeric-range education.
	//
	// Português: O "int" abstrato pode ligar em qualquer parâmetro
	// numérico concreto — o backend Go faz o cast no call site; pares
	// com perda geram o aviso KindTypeLossy do codegen.
	"int": {"int", "float", "float64", "float32",
		"int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte"},
	"int64":   {"int64", "int", "float", "float64"},
	"uint":    {"uint", "int", "float64"},
	"uint8":   {"uint8", "uint16", "uint32", "uint64", "int", "float64"},
	"uint16":  {"uint16", "uint32", "uint64", "int", "float64"},
	"uint32":  {"uint32", "uint64", "int64", "float64"},
	"uint64":  {"uint64"},
	"float":   {"float", "float64", "float32"},
	"float64": {"float64", "float"},
	// A "string" source (e.g. ConstString) also feeds a C99 `const char*`
	// input — that C type is the GoType the parser stores for the port, and
	// it is the same "string" concept. Covers the parser's normalised form
	// ("const char*") plus the spaced variants, const and non-const.
	"string": {"string", "const char*", "char*", "const char *", "char *"},
	"bool":   {"bool", "int"},
	// T6 decision B — the abstract COLLECTIONS (ConstArrayInt, ConstArrayFloat)
	// may wire into any concrete same-family collection parameter ([]uint16 of
	// an authored Go method, []float32 of a C sensor, etc.): the IR infers the
	// declaration's element type FROM the consumer port (inferredCollectionElem),
	// so the generated slice literal always matches the parameter exactly.
	// Slices have no call-site conversion, hence inference at the DECLARATION
	// instead of a cast at the call. Only the string collection stays concrete
	// on the exact-match fallback.
	//
	// Português: Decisão B — as coleções abstratas (int e float) podem ligar em
	// qualquer parâmetro concreto da mesma família; o IR infere o tipo do
	// elemento a partir do consumidor, então o literal gerado sempre casa. Só a
	// coleção de string fica concreta no exact-match.
	"[]int": {"[]int", "[]int8", "[]int16", "[]int32", "[]int64",
		"[]uint", "[]uint8", "[]uint16", "[]uint32", "[]uint64", "[]byte"},
	"[]float":  {"[]float", "[]float32", "[]float64"},
	"[]string": {"[]string"},
	"[]bool":   {"[]bool"},
	// NOTE: Struct and pointer types (*T) are intentionally absent.
	// The exact-match rule in resolveCompatibleType handles them.
	// Nota: Tipos struct e ponteiro (*T) estão ausentes intencionalmente.
}

// =====================================================================
//  Compatibility Functions | Funções de Compatibilidade
// =====================================================================

// resolveCompatibleType checks whether an output type can connect to an input
// type.
//
// Resolution order (first match wins):
//
//  1. Exact match: outputType == inputType. Any Go type is compatible with
//     itself. This handles all identical-type connections, including complex
//     types (*machine.I2C, user structs) that have no matrix entry.
//
//  2. Matrix promotion: the output type has a matrix entry and the input type
//     is listed as an accepted target. This handles int→float, bool→int, etc.
//
// Returns the resolved type (always the input type) and true when compatible.
//
// Português:
//
//	Ordem de resolução (primeiro match vence):
//	  1. Exact match: tipos idênticos são sempre compatíveis.
//	  2. Promoção via matriz: int→float, bool→int, etc.
func resolveCompatibleType(outputType string, inputType string, compat map[string][]string) (resolvedType string, ok bool) {
	// ── Rule 1: Exact match ──────────────────────────────────────────────
	//
	// Evaluated before the matrix so that complex types (*machine.I2C, etc.)
	// connect immediately without needing explicit matrix entries. The matrix
	// only matters for cross-type promotions (int→float, bool→int, etc.).
	//
	// Português: Avaliado antes da matriz para que tipos complexos conectem
	// imediatamente sem entradas explícitas. A matriz importa apenas para
	// promoções cruzadas.
	if outputType == inputType {
		resolvedType = inputType
		ok = true
		return
	}

	// ── Rule 2: Matrix promotion ─────────────────────────────────────────
	allowed, exists := compat[outputType]
	if !exists {
		return // no promotion rule and not an exact match — incompatible
	}

	for _, a := range allowed {
		if a == inputType {
			resolvedType = inputType
			ok = true
			return
		}
	}
	return
}

// findCompatibleTypes checks whether any output type from the source can connect
// to any input type from the target. Returns the first matching pair.
//
// Português: Verifica se qualquer tipo de saída da origem pode conectar a
// qualquer tipo de entrada do destino. Retorna o primeiro par compatível.
func findCompatibleTypes(outputTypes []string, inputTypes []string, compat map[string][]string) (outputType string, inputType string, resolvedType string) {
	for _, ot := range outputTypes {
		for _, it := range inputTypes {
			rt, ok := resolveCompatibleType(ot, it, compat)
			if ok {
				return ot, it, rt
			}
		}
	}
	return "", "", ""
}

// =====================================================================
//  Style Helpers | Helpers de Estilo
// =====================================================================

// styleKeyForType returns the lookup key to use in DefaultTypeStyles for a given
// Go type string.
//
// For primitive types it returns the type unchanged. For pointer types ("*...")
// and package-qualified named types ("pkg.Type") it returns "struct", directing
// them to the violet catch-all style. This avoids an ever-growing table of
// per-peripheral style entries.
//
// Português:
//
//	Retorna a chave de busca em DefaultTypeStyles para um tipo Go.
//	Para primitivos retorna o tipo sem alteração. Para tipos ponteiro e
//	tipos qualificados por pacote retorna "struct".
func styleKeyForType(goType string) string {
	// Pointer types: *machine.I2C, *spi.Device, *machine.Pin, etc.
	// Português: Tipos ponteiro.
	if strings.HasPrefix(goType, "*") {
		return "struct"
	}

	// Package-qualified named types without pointer: machine.I2C, etc.
	// The dot is the reliable signal for a non-primitive Go type.
	// Português: Tipos nomeados qualificados por pacote sem ponteiro.
	if strings.Contains(goType, ".") {
		return "struct"
	}

	// Return the type as-is; the caller falls back to DefaultUnknownStyle
	// if not found in the table.
	return goType
}
