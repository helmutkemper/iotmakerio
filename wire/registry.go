// wire/registry.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import (
	"strings"

	"github.com/helmutkemper/iotmakerio/rulesDevice"
)

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
//
// scalarStyle builds the WireStyle for a scalar type from its canonical
// palette color. The selected highlight is DERIVED (blend toward white) so
// the palette never needs a hand-maintained parallel table of light colors.
//
// Português: Monta o WireStyle de um tipo escalar a partir da cor canônica da
// paleta. O destaque "selected" é DERIVADO (mistura com branco), então a
// paleta nunca precisa de uma tabela paralela de cores claras mantida à mão.
// pointerStyle derives the pointer-wire look from the family color: the
// scalar style plus a dash pattern. See the [PTR] note in the style table.
// Português: Deriva o visual do fio ponteiro da cor da família: o estilo
// escalar mais o tracejado. Ver a nota [PTR] na tabela.
func pointerStyle(colorHex string) WireStyle {
	s := scalarStyle(colorHex)
	s.DashPattern = []float64{6, 4}
	return s
}

func scalarStyle(colorHex string) WireStyle {
	return WireStyle{
		StrokeColor:   colorHex,
		StrokeWidth:   2.0,
		SelectedColor: rulesDevice.KColorWireSelected,
		SelectedWidth: 5.0,
		CornerRadius:  6.0,
	}
}

// DefaultTypeStyles maps data type names to their default visual styles.
// Users can override these by calling Manager.SetTypeStyle().
//
// PALETTE UNIFICATION: every stroke color here is a rulesDevice constant —
// the single type→color source of truth shared by device accents, connector
// pins and wires. A wire meeting a pin of the same type is now guaranteed to
// be the same hue. Do NOT write literal hex colors in this table; add the
// constant to rulesDevice/palette.go instead.
//
// Collections ([]int, []float, …) are NOT listed here anymore: the manager
// derives them generically (see deriveTypeStyle) as "element color, thicker
// stroke", which also covers previously unmapped slices such as []byte and
// []float64 that used to fall through to the grey dashed unknown style.
//
// The "struct" entry acts as a catch-all for complex Go types (pointer types,
// named struct types). The manager's getTypeStyle() falls back to this entry
// when it encounters any type whose name begins with "*" or contains a dot
// (package-qualified type name) and has no specific entry here.
//
// Português:
//
//	UNIFICAÇÃO DE PALETA: toda cor de traço aqui é uma constante de
//	rulesDevice — a fonte única de tipo→cor compartilhada por devices, pinos
//	e fios. Um fio encostando num pino do mesmo tipo agora é garantidamente
//	do mesmo matiz. NÃO escreva hex literal nesta tabela; adicione a
//	constante em rulesDevice/palette.go.
//
//	Coleções não são mais listadas aqui: o manager as deriva genericamente
//	(cor do elemento, traço mais grosso) — ver deriveTypeStyle — o que também
//	cobre slices antes não mapeadas ([]byte, []float64), que caíam no estilo
//	cinza tracejado de desconhecido.
//
//	O entry "struct" é o catch-all para tipos complexos (ponteiros, structs
//	nomeados). getTypeStyle() cai para este entry ao encontrar tipos com
//	prefixo "*" ou com ponto (tipo qualificado por pacote).
var DefaultTypeStyles = map[string]WireStyle{
	// ── Signed integer family — blues ───────────────────────────────────
	"int":   scalarStyle(rulesDevice.KColorTypeInt),
	"int64": scalarStyle(rulesDevice.KColorTypeInt),
	"int32": scalarStyle(rulesDevice.KColorTypeInt32),
	"int16": scalarStyle(rulesDevice.KColorTypeInt32),
	"int8":  scalarStyle(rulesDevice.KColorTypeInt32),

	// ── Unsigned integer family — purples ───────────────────────────────
	// uint8 is intentionally ABSENT: in Go, byte IS uint8 (type alias), so
	// uint8 shares the byte entry below. See rulesDevice.KColorTypeUint.
	// Português: uint8 está AUSENTE de propósito: em Go, byte É uint8
	// (alias), então uint8 compartilha o entry de byte abaixo.
	"uint":   scalarStyle(rulesDevice.KColorTypeUint),
	"uint16": scalarStyle(rulesDevice.KColorTypeUint),
	"uint32": scalarStyle(rulesDevice.KColorTypeUint),
	"uint64": scalarStyle(rulesDevice.KColorTypeUint64),

	// ── Byte — purple, shared by the uint8 alias ────────────────────────
	"byte":  scalarStyle(rulesDevice.KColorTypeByte),
	"uint8": scalarStyle(rulesDevice.KColorTypeByte),

	// ── Float family — greens/teals ─────────────────────────────────────
	// "float" is the maker-facing abstract type (bit-width is decided by the
	// target profile); it shares the full-precision float64 accent, exactly
	// as abstract "int" shares the int64 accent above.
	// Português: "float" é o tipo abstrato que o maker vê; compartilha a cor
	// do float64, como o "int" abstrato compartilha a do int64.
	"float":   scalarStyle(rulesDevice.KColorTypeFloat64),
	"float64": scalarStyle(rulesDevice.KColorTypeFloat64),
	"float32": scalarStyle(rulesDevice.KColorTypeFloat32),

	// ── Text, boolean, temporal ─────────────────────────────────────────
	"string": scalarStyle(rulesDevice.KColorTypeString),
	"bool":   scalarStyle(rulesDevice.KColorTypeBool),

	// [PTR] Pointer-wire family tokens: same color as the base family,
	// DASHED — the stage-visible "this is a reference" cue. Only the debug
	// devices list these in AllowedTypes, so the dash also reads as "probe
	// wire". Português: Tokens de fio ponteiro: mesma cor da família base,
	// TRACEJADO — a marca visível de "isto é uma referência". Só os devices
	// de debug os listam em AllowedTypes, então o traço também lê como "fio
	// de sonda".
	"int*":          pointerStyle(rulesDevice.KColorTypeInt),
	"float*":        pointerStyle(rulesDevice.KColorTypeFloat64),
	"bool*":         pointerStyle(rulesDevice.KColorTypeBool),
	"byte*":         pointerStyle(rulesDevice.KColorTypeByte),
	"time.Duration": scalarStyle(rulesDevice.KColorTypeDuration),

	// ── Error — red dashed: "handle me" ─────────────────────────────────
	// The dash pattern is unique to error so an unhandled error path is
	// impossible to miss even for colorblind users.
	// Português: O tracejado é exclusivo do error para um caminho de erro
	// não tratado ser impossível de ignorar, mesmo para daltônicos.
	"error": func() WireStyle {
		s := scalarStyle(rulesDevice.KColorTypeError)
		s.DashPattern = []float64{6, 3}
		return s
	}(),

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
		StrokeColor:   rulesDevice.KColorTypeStruct,
		StrokeWidth:   2.5,
		SelectedColor: rulesDevice.KColorWireSelected,
		SelectedWidth: 5.5,
		CornerRadius:  6.0,
	},
}

// deriveTypeStyle resolves the style for types that are not listed verbatim
// in DefaultTypeStyles, in priority order:
//
//  1. Collections ("[]T"): the ELEMENT type's style, drawn thicker
//     (stroke 4.0, selected 6.0). Hue stays the element's — the wire system's
//     long-standing rule "a slice is the base color, just heavier" — so any
//     collection of a colorable element gets a correct wire, including
//     []byte, []float64 and slices of complex types.
//
//  2. Pointer / package-qualified types: the "struct" violet entry (via
//     styleKeyForType).
//
// Returns ok=false when nothing applies, letting the caller fall back to
// DefaultUnknownStyle (grey dashed).
//
// Português: Resolve o estilo de tipos que não estão literais na tabela:
// (1) coleções "[]T" usam o estilo do ELEMENTO com traço mais grosso — a
// regra antiga do sistema de fios — cobrindo []byte, []float64 e slices de
// tipos complexos; (2) ponteiros e tipos qualificados por pacote caem no
// entry violeta "struct". Retorna ok=false quando nada se aplica.
func deriveTypeStyle(dataType string) (style WireStyle, ok bool) {
	// ── Collections: element style, thicker ─────────────────────────────
	if strings.HasPrefix(dataType, "[]") {
		elem := dataType[2:]

		base, found := DefaultTypeStyles[elem]
		if !found {
			// Slice of a complex element ([]machine.Pin, []*spi.Device):
			// derive from the struct catch-all.
			// Português: Slice de elemento complexo: deriva do catch-all.
			if key := styleKeyForType(elem); key != elem {
				base, found = DefaultTypeStyles[key]
			}
		}
		if found {
			base.StrokeWidth = 4.0
			base.SelectedWidth = 6.0
			return base, true
		}
		return WireStyle{}, false
	}

	// ── Pointer / package-qualified → struct violet ─────────────────────
	if key := styleKeyForType(dataType); key != dataType {
		if s, found := DefaultTypeStyles[key]; found {
			return s, true
		}
	}

	return WireStyle{}, false
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
	SelectedColor: rulesDevice.KColorWireSelected,
	SelectedWidth: 5.0,
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
