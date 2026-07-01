// /ide/rulesConnection/golangManagerTypeConversion.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesConnection

import (
	"errors"
	"fmt"
	"image/color"
	"strings"

	"github.com/helmutkemper/iotmakerio/platform/factoryColor"
)

// golangManagerTypeConversion implements RulesDataType for the Go language.
//
// English:
//
//	It handles two responsibilities:
//
//	  1. Verify() — validates that a type string is known. Recognises all Go
//	     primitive types, common composite types, pointer types (*T),
//	     package-qualified named types (pkg.Type), and IDE semantic types
//	     (time.Duration). Unknown types that do not fit any of these patterns
//	     are reported as errors.
//
//	  2. TypeToColor() — assigns a visual colour to each type category so the
//	     IDE can render connector dots in a consistent palette. The colour is used
//	     for the small square/circle at each port on every block.
//
//	The original version only accepted a fixed list of primitive type names.
//	This caused two problems for black-box components:
//
//	  a) Verify() raised an error for every complex type (*machine.I2C, etc.),
//	     flooding the log with false positives and potentially blocking the
//	     connection validation path.
//
//	  b) TypeToColor() returned red (the error colour) for these types, making
//	     every hardware-bus port look broken to the user.
//
//	The fix adds pattern-based recognition before the whitelist check:
//	  • Pointer types  (strings.HasPrefix("*"))   → "struct" category → violet
//	  • Package types  (strings.Contains("."))     → "struct" category → violet
//	  • Slice types    (strings.HasPrefix("[]"))   → "slice"  category → dark magenta
//
//	Additionally, "time.Duration" is treated as a first-class semantic type
//	with its own dedicated cyan colour, distinct from int64, to prevent
//	accidental cross-wiring between raw integers and temporal values.
//
//	If none of the patterns match and the type is not in the whitelist, the error
//	is still raised — preserving the original intent of catching genuine typos in
//	type annotations.
//
// Português:
//
//	Lida com duas responsabilidades:
//
//	  1. Verify() — valida que uma string de tipo é conhecida. Reconhece todos os
//	     tipos primitivos Go, tipos compostos comuns, tipos ponteiro (*T), tipos
//	     nomeados qualificados por pacote (pkg.Type), e tipos semânticos da IDE
//	     (time.Duration).
//
//	  2. TypeToColor() — atribui uma cor visual a cada categoria de tipo para que
//	     a IDE renderize pontos de conector em uma paleta consistente.
//
//	"time.Duration" é tratado como tipo semântico de primeira classe com cor
//	ciano dedicada, distinto de int64, para prevenir conexão cruzada acidental
//	entre inteiros crus e valores temporais.
type golangManagerTypeConversion struct {
	err error
}

// GetError returns the accumulated errors from type conversion operations.
//
// Português: Retorna os erros acumulados das operações de conversão de tipo.
func (e *golangManagerTypeConversion) GetError() (err error) {
	return e.err
}

// Verify checks that a type string is a known Go type.
//
// Recognition order:
//
//  1. Pointer types  (*machine.I2C, *spi.Device, *T) — always valid.
//  2. Slice types    ([]byte, []uint8, []string)      — always valid.
//  3. IDE semantic types (time.Duration)               — always valid.
//  4. Package types  (machine.I2C, spi.Config)        — always valid.
//  5. Primitive whitelist (bool, int, string, …)      — always valid.
//  6. Anything else → error (preserves the original typo-catching intent).
//
// Note: step 3 must come BEFORE step 4 because "time.Duration" contains a dot
// and would otherwise be silently accepted as a generic package type. By
// checking it explicitly first, the semantic type is recognised with the
// correct colour in TypeToColor().
//
// Português:
//
//	Ordem de reconhecimento:
//	  1. Tipos ponteiro (*T)              — sempre válidos.
//	  2. Tipos slice ([]T)               — sempre válidos.
//	  3. Tipos semânticos IDE (time.Duration) — sempre válidos.
//	  4. Tipos qualificados por pacote   — sempre válidos.
//	  5. Lista branca de primitivos      — sempre válidos.
//	  6. Qualquer outro → erro.
func (e *golangManagerTypeConversion) Verify(dataType string) (err error) {
	// ── Pattern 1: Pointer types (*machine.I2C, *spi.Device, *T) ────────
	// All pointer types are valid Go; black-box components pass them between
	// blocks for hardware bus sharing (I2C, SPI, UART, etc.).
	// Português: Todos os tipos ponteiro são válidos. Componentes black-box
	// os passam entre blocos para compartilhamento de barramentos de hardware.
	if strings.HasPrefix(dataType, "*") {
		return nil
	}

	// ── Pattern 2: Slice types ([]byte, []uint8, []machine.Pin) ─────────
	// Slices of any element type are valid. A future gate could check the
	// element type recursively, but that level of validation is not needed
	// for the current IDE scope.
	// Português: Slices de qualquer tipo de elemento são válidos.
	if strings.HasPrefix(dataType, "[]") {
		return nil
	}

	// ── Pattern 3: IDE semantic types ───────────────────────────────────
	// Types that are structurally valid Go AND carry special IDE semantics.
	// Must be checked BEFORE the generic dot-check (pattern 4) so they get
	// their dedicated colour in TypeToColor() instead of the generic violet.
	//
	// Português: Tipos com semântica especial na IDE. Devem ser verificados
	// ANTES do check genérico de ponto para receber cor dedicada.
	switch dataType {
	case "time.Duration":
		return nil
	}

	// ── Pattern 4: Package-qualified named types (machine.I2C) ──────────
	// A dot in the type name is the reliable Go signal for a package-qualified
	// type. These come from imported packages and are always structurally valid.
	// Português: Um ponto no nome do tipo é o sinal Go para tipo qualificado
	// por pacote. Sempre estruturalmente válido.
	if strings.Contains(dataType, ".") {
		return nil
	}

	// ── Primitive whitelist ──────────────────────────────────────────────
	// Only the types in this list are known primitive Go types or IDE-internal
	// meta-types. Anything else is a genuine unknown.
	// Português: Apenas os tipos desta lista são primitivos Go conhecidos ou
	// meta-tipos internos da IDE.
	switch dataType {
	case "bool",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64",
		"complex64", "complex128",
		"string", "byte", "rune",
		"error",
		"slice",  // IDE meta-type for untyped slice connectors
		"struct": // IDE meta-type for untyped struct connectors
		return nil
	}

	// ── Unknown type ─────────────────────────────────────────────────────
	// At this point the type matched none of the patterns above. This is most
	// likely a typo in a black-box annotation. Record the error so the IDE can
	// surface it in the Inspect panel.
	// Português: Nenhum padrão acima correspondeu. Provavelmente um erro de
	// digitação na anotação da black-box.
	err = errors.Join(e.err, fmt.Errorf("unknown data type `%s` for `%v`", dataType, TypeOfDataCurrentlyInEffect))
	return
}

// TypeToColor assigns a visual colour to a Go type for connector dot rendering.
//
// The colour palette follows these conventions:
//   - Error colour (red) is RESERVED for the "error" meta-type only.
//   - Pointer/struct types get violet — distinct from all primitive colours.
//   - Numeric families share a hue family (blue for int, purple for uint, etc.).
//   - time.Duration gets cyan — distinct from int64 (blue) to enforce semantic
//     type safety. A maker cannot accidentally wire an int64 into a Duration port.
//   - Unknown types that pass Verify() still get a deterministic colour (violet
//     for complex types, red only if genuinely unrecognised).
//
// Português:
//
//	A paleta segue estas convenções:
//	  - Vermelho (erro) é RESERVADO para o meta-tipo "error".
//	  - Tipos ponteiro/struct recebem violeta — distinto de todos os primitivos.
//	  - Famílias numéricas compartilham uma família de matiz.
//	  - time.Duration recebe ciano — distinto de int64 (azul) para segurança
//	    semântica de tipos.
//	  - Tipos desconhecidos que passaram em Verify() ainda recebem uma cor
//	    determinística (violeta para tipos complexos).
func (e *golangManagerTypeConversion) TypeToColor(dataType string) (c color.RGBA) {
	// ── IDE semantic types — must be checked BEFORE pattern checks ───────
	// time.Duration contains a dot but needs its own dedicated colour (cyan),
	// not the generic violet assigned to package-qualified types.
	//
	// Português: time.Duration contém um ponto mas precisa de cor dedicada
	// (ciano), não o violeta genérico de tipos qualificados por pacote.
	if dataType == "time.Duration" {
		return color.RGBA{R: 0, G: 204, B: 204, A: 255} // cyan #00CCCC
	}

	// ── Pointer types → violet ───────────────────────────────────────────
	// Communicates "structured hardware data" and matches the violet wire style
	// in wire/registry.go DefaultTypeStyles["struct"].
	// Português: Comunica "dados de hardware estruturados" e combina com o estilo
	// de fio violeta em wire/registry.go.
	if strings.HasPrefix(dataType, "*") {
		return factoryColor.NewViolet()
	}

	// ── Package-qualified types → violet ─────────────────────────────────
	if strings.Contains(dataType, ".") {
		return factoryColor.NewViolet()
	}

	// ── Slice types → dark magenta ────────────────────────────────────────
	if strings.HasPrefix(dataType, "[]") {
		return factoryColor.NewDarkMagenta()
	}

	switch dataType {
	case "bool":
		return factoryColor.NewGreen()
	case "int", "int8", "int16", "int32", "int64":
		return factoryColor.NewBlue()
	case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return factoryColor.NewBlueViolet()
	case "float32", "float64":
		return factoryColor.NewYellowGreen()
	case "byte", "rune":
		return factoryColor.NewBlueViolet()
	case "slice":
		return factoryColor.NewDarkMagenta()
	case "string":
		return factoryColor.NewMediumTurquoise()
	case "struct":
		return factoryColor.NewViolet()
	case "error":
		// Error is the ONLY type that legitimately uses red.
		// Português: Error é o ÚNICO tipo que usa vermelho legitimamente.
		return factoryColor.NewRed()
	default:
		// Any type that reached here passed Verify() through one of the
		// pattern rules but has no explicit colour. Use violet (struct family)
		// as the safest generic "complex type" colour. Red is intentionally
		// avoided here because it would alarm the user for a type that is
		// actually valid — just not individually colour-mapped.
		//
		// Português: Qualquer tipo que chegou aqui passou em Verify() por uma
		// das regras de padrão mas não tem cor explícita. Usa violeta como cor
		// genérica segura para "tipo complexo".
		return factoryColor.NewViolet()
	}
}
