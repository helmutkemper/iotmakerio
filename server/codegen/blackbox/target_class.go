// server/codegen/blackbox/target_class.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// The MINIMUM-TARGET ladder: a device declares (via `// min-target:<class>.`
// in its leading comment) the smallest class of hardware its code can run
// on, and the IDE compares it against the project's chosen target with ONE
// ordinal comparison. Classes — not raw kilobytes — because compatibility
// is multi-axis (RAM, flash, POSIX sockets, filesystem, FPU) and the class
// carries the capabilities implicitly; raw numbers invite false precision.
// New boards slot into existing rungs without schema changes.
//
//	avr    — up to ~2 KB RAM   (Arduino Uno, Nano)
//	mcu32  — 64 KB+ RAM        (ESP32, STM32/Cortex-M, Pico)
//	posix  — Linux/PC          (Raspberry Pi, desktop)
//
// An ABSENT tag means "runs anywhere" (ordinal of avr): total backward
// compatibility, zero friction for existing devices.
//
// Português: A escada de TARGET MÍNIMO: o device declara (via
// `// min-target:<classe>.` no comentário de cabeçalho) a menor classe de
// hardware onde seu código roda, e a IDE compara com o target do projeto
// numa ÚNICA comparação ordinal. Classes — não kilobytes crus — porque
// compatibilidade é multi-eixo (RAM, flash, sockets POSIX, filesystem,
// FPU) e a classe carrega as capacidades implicitamente. Tag AUSENTE
// significa "roda em qualquer lugar" (ordinal do avr): retrocompatível.

package blackbox

// Valid min-target classes, in ladder order.
// Português: Classes válidas, na ordem da escada.
const (
	MinTargetAvr   = "avr"
	MinTargetMcu32 = "mcu32"
	MinTargetPosix = "posix"
)

// MinTargetOrdinal maps a declared class to its rung. The empty string —
// the undeclared default — sits on the lowest rung ("runs anywhere"). The
// second return reports whether the value is a KNOWN class; validators
// turn false into a clear diagnostic instead of guessing.
// Português: Mapeia a classe declarada para o degrau. Vazio — o default
// não-declarado — fica no degrau mais baixo ("roda em qualquer lugar"). O
// segundo retorno diz se o valor é CONHECIDO; validadores transformam
// false em diagnóstico claro em vez de chutar.
func MinTargetOrdinal(class string) (int, bool) {
	switch class {
	case "", MinTargetAvr:
		return 1, true
	case MinTargetMcu32:
		return 2, true
	case MinTargetPosix:
		return 3, true
	}
	return 0, false
}

// ClassOfProfile maps a codegen target-profile name to its ladder class.
// An EMPTY profile — a scene that never chose a target — maps to posix on
// purpose: the maker is developing on a PC, and blocking posix-class
// devices there would break every existing scene; the moment they pick a
// real board the gate tightens. Unknown names also fall to posix for the
// same permissive-by-default reason (the profile resolver already warns).
// Português: Mapeia o nome do profile de target para a classe da escada.
// Profile VAZIO — cena que nunca escolheu target — vira posix de
// propósito: o maker está desenvolvendo num PC, e bloquear devices posix
// ali quebraria toda cena existente; no momento em que escolher uma placa
// real, o portão aperta. Nomes desconhecidos também caem em posix pela
// mesma permissividade-por-default (o resolvedor de profile já avisa).
func ClassOfProfile(profileName string) string {
	switch profileName {
	case "arduino_uno":
		return MinTargetAvr
	case "cortex_m":
		return MinTargetMcu32
	case "pi_linux", "portable", "":
		return MinTargetPosix
	}
	return MinTargetPosix
}
