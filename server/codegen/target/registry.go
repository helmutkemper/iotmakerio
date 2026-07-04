// server/codegen/target/registry.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// Package target holds the registry of hardware TARGETS a maker can generate
// code for — the tangible boards ("Arduino UNO", "ESP32-C6", "PC / tablet")
// they pick, following the Arduino convention of choosing a board rather than
// an architecture.
//
// A Target is a thin PRESET: display data (name, description, RAM, icon) plus
// two things the code generator consumes — which type-family profile to use,
// and the string-concatenation buffer size that fits the board's RAM. The
// correctness-critical part (the int/float widths) lives in the profiles under
// backend/ansic; a Target only references a profile by name, never redefines
// the widths.
//
// Why targets live in CODE and not the database:
//
// Black-boxes are community-authored, DB-backed, and extensible — a wrong
// black-box is contained. A target's type mappings are the opposite: a wrong
// int width silently breaks the generated firmware, everywhere, with no error.
// That is not something an admin should be able to add through a form. So the
// target set is small, curated, and reviewed in a pull request. Adding a board
// is one entry in the `targets` slice below — reviewed like any other code.
//
// Português: O pacote target guarda o registro de PLACAS-alvo que o maker pode
// gerar código ("Arduino UNO", "ESP32-C6", "PC / tablet") — a coisa tangível
// que ele escolhe, seguindo a convenção Arduino de escolher a placa, não a
// arquitetura. Um Target é um PRESET fino: dados de exibição + duas coisas que
// o gerador consome (qual profile de família de tipos usar, e o tamanho do
// buffer de concatenação de string que cabe na RAM da placa). A parte crítica
// (larguras de int/float) fica nos profiles em backend/ansic; o Target só
// referencia um profile por nome. Targets vivem em CÓDIGO, não no banco:
// diferente de black-boxes (autoria da comunidade, contidas), um mapeamento de
// tipo errado quebra o firmware em silêncio — então o conjunto é pequeno,
// curado e revisado num PR. Adicionar uma placa é uma entrada no slice abaixo.
package target

import (
	"sort"

	"server/codegen/backend/ansic"
)

// Target is one selectable hardware preset. The maker reads the display fields
// (DisplayName, Description, RAMBytes, Icon); the code generator consumes
// ProfileName and StringBufferSize.
type Target struct {
	// ID is the stable, canonical identifier stored in the scene
	// (graph.MetadataInput.Target) and sent back by the target endpoint. It is
	// never shown to a human. e.g. "arduino_uno".
	ID string

	// DisplayName is the label shown in the maker's dropdown. e.g. "Arduino UNO".
	DisplayName string

	// Description is the explanatory text shown beneath the dropdown. It teaches
	// a non-technical maker what the choice means: the RAM implication, what the
	// generator does under the hood, and when to pick this board.
	Description string

	// RAMBytes is the board's usable RAM, for display ("2 KB") and as a sanity
	// anchor for StringBufferSize. Zero means "not applicable / ample" (e.g. a
	// desktop target) — the UI should render that as "ample", not "0 bytes".
	RAMBytes int

	// ProfileName references a type-family profile in backend/ansic (resolved by
	// ansic.ResolveProfile) — the correctness-critical int/float widths. Many
	// boards share one family, so this is a reference, not a copy. Ignored by
	// the Go backend, which uses native Go types.
	ProfileName string

	// StringBufferSize is the fixed size, in bytes, of the stack buffer each
	// string concatenation writes into on this board (see
	// ansic.emitStringConcat). A tight board gets a small buffer; a desktop gets
	// a generous one. Ignored by the Go backend, whose "+" concatenates natively.
	StringBufferSize int

	// Icon is the icon name for the dropdown row (Tabler outline set). Optional.
	Icon string

	// Order sorts the dropdown; lower values appear first, so the most common
	// boards sit at the top.
	Order int
}

// targets is the curated registry. To add a board: append an entry, pick an
// existing ProfileName whose int/float widths match the board's architecture,
// and size StringBufferSize to its RAM. Reviewed in a PR like any code.
//
// NOTE (naming): the profiles are still named by board/platform
// (arduino_uno, cortex_m, pi_linux). An ESP32-C6 (RISC-V) pointing at
// ProfileNameCortexM is functionally correct — the 32-bit int/float widths
// match — but the profile NAME reads oddly. Renaming the profiles to
// type-family names (avr8 / bits32 / bits64) is a deferred cleanup; targets are
// the user-facing layer now, so the profile names are purely internal.
var targets = []Target{
	{
		ID:               "arduino_uno",
		DisplayName:      "Arduino UNO",
		Description:      "8-bit AVR, 2 KB RAM. Tight memory: generated strings use small fixed buffers and numbers are 16-bit. Best for classic Arduino boards.",
		RAMBytes:         2048,
		ProfileName:      ansic.ProfileNameArduinoUno, // int32 (L-suffixed) / float
		StringBufferSize: 64,
		Icon:             "cpu",
		Order:            10,
	},
	{
		ID:               "esp32_c6",
		DisplayName:      "ESP32-C6",
		Description:      "RISC-V, 512 KB RAM. Plenty of room: string buffers can be generous. A good pick for Wi-Fi and networked projects.",
		RAMBytes:         524288,
		ProfileName:      ansic.ProfileNameCortexM, // 32-bit int/float — fits RISC-V32
		StringBufferSize: 256,
		Icon:             "wifi",
		Order:            20,
	},
	{
		ID:               "pc_tablet",
		DisplayName:      "PC / tablet",
		Description:      "Runs as a desktop program — effectively unlimited RAM, so buffers are large. Pick this to try a project on your own computer before flashing hardware.",
		RAMBytes:         0,                        // ample / not applicable
		ProfileName:      ansic.ProfileNamePiLinux, // 64-bit int / double
		StringBufferSize: 512,
		Icon:             "device-desktop",
		Order:            30,
	},
}

// byID indexes the registry for O(1) lookup, built once at package init.
var byID = func() map[string]Target {
	m := make(map[string]Target, len(targets))
	for _, t := range targets {
		m[t.ID] = t
	}
	return m
}()

// DefaultTarget is returned when a scene names no target, or an unknown one. It
// is the conservative choice — the tight Arduino UNO — so a scene authored
// before targets existed, or one with a stale id, still generates safe,
// small-footprint code rather than assuming a generous platform.
func DefaultTarget() Target { return byID["arduino_uno"] }

// ResolveTarget maps a stored target id to its Target, falling back to
// DefaultTarget for the empty or unknown case. It never returns a zero Target,
// so callers can use the result unconditionally.
//
// Português: Mapeia um id de target armazenado para o Target, caindo em
// DefaultTarget no caso vazio ou desconhecido. Nunca retorna um Target zero.
func ResolveTarget(id string) Target {
	if t, ok := byID[id]; ok {
		return t
	}
	return DefaultTarget()
}

// AllTargets returns every target, sorted by Order (then DisplayName for
// stability), for the maker-facing dropdown and its serving endpoint. It
// returns a fresh slice so callers cannot mutate the registry.
//
// Português: Retorna todos os targets ordenados por Order (e DisplayName para
// estabilidade), para o dropdown do maker e o endpoint que o serve. Retorna um
// slice novo para o chamador não conseguir mutar o registro.
func AllTargets() []Target {
	out := make([]Target, len(targets))
	copy(out, targets)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}
