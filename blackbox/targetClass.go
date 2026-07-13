// blackbox/targetClass.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// Client-side mirror of the min-target ladder — hand-kept copies of
// server/codegen/blackbox/target_class.go (classes) and
// server/codegen/target/registry.go (board ids). The WASM menu gate reads
// these to disable devices whose declared minimum class exceeds the
// project's board; the SERVER validator remains the authority at export.
//
// Português: Espelho client da escada de min-target — cópias mantidas à
// mão de server/codegen/blackbox/target_class.go (classes) e
// server/codegen/target/registry.go (ids de placa). O portão do menu WASM
// as lê para desabilitar devices cuja classe mínima excede a placa do
// projeto; o validador do SERVER segue sendo a autoridade no export.

package blackbox

// MinTargetOrdinal maps a device's declared class to its ladder rung.
// Empty (undeclared) = lowest rung: runs anywhere. UNKNOWN values also map
// to the lowest rung ON PURPOSE: the menu must not gate on a specialist's
// typo — the export validator names the typo with the valid classes, and
// that is the right place for the error.
// Português: Mapeia a classe declarada para o degrau. Vazio = degrau mais
// baixo: roda em qualquer lugar. Valor DESCONHECIDO também vai para o
// degrau mais baixo DE PROPÓSITO: o menu não deve barrar por typo do
// especialista — o validador de export nomeia o typo com as classes
// válidas, e lá é o lugar certo do erro.
func MinTargetOrdinal(class string) int {
	switch class {
	case "mcu32":
		return 2
	case "posix":
		return 3
	}
	return 1
}

// TargetIDOrdinal maps a board REGISTRY id to its ladder rung. Empty (no
// board picked yet) and unknown ids map to the TOP rung — permissive, the
// same default the server's ClassOfProfile documents: a maker developing
// on a PC must never see devices gated before choosing real hardware.
// Português: Mapeia o id de placa do registro para o degrau. Vazio (sem
// placa) e ids desconhecidos vão para o degrau MAIS ALTO — permissivo, o
// mesmo default do ClassOfProfile do server: maker desenvolvendo no PC não
// pode ver devices barrados antes de escolher hardware real.
func TargetIDOrdinal(id string) int {
	switch id {
	case "arduino_uno":
		return 1
	case "esp32_c6":
		return 2
	}
	return 3
}
