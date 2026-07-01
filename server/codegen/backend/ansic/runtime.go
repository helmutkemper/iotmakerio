// /server/codegen/backend/ansic/runtime.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

// runtime.go — Verbatim contents of the C runtime files emitted
// alongside main.c.
//
// The C backend produces three files in its output map:
//
//   - main.c                       — the per-scene program
//   - iotmaker_runtime.h           — the runtime contract (this file)
//   - iotmaker_runtime_stub.c      — a POSIX implementation that lets
//                                    the program compile and run on
//                                    Linux/macOS without further work
//
// Why split into a separate Go file:
//
// The header and stub are static strings that never depend on the
// scene or the profile. Keeping them out of emit.go means edits to
// either file land here without touching the emitter, and someone
// reviewing changes to the contract can see them in isolation.
//
// Why the contract is a hook rather than embedded code:
//
// The "sleep" primitive does not exist portably in standard C. POSIX
// gives nanosleep, Arduino gives delay(), the RP2040 SDK gives
// sleep_us, ESP-IDF gives vTaskDelay, bare-metal gives nothing at
// all. By generating a call to iotmaker_sleep_ns and letting the
// maker (or a board-specific support package) provide the body, the
// generated program is portable in shape and trivial to retarget.
// The stub here uses POSIX nanosleep so "gcc main.c
// iotmaker_runtime_stub.c -o test" works on any Unix host without
// extra setup.
//
// What happens when the maker targets a microcontroller:
//
// The maker replaces iotmaker_runtime_stub.c with a board-specific
// implementation (e.g. an Arduino .ino with delay(), or an RP2040
// file using sleep_us from pico-stdio). main.c never needs to
// change — the contract in iotmaker_runtime.h is stable. This is
// the IoTMaker equivalent of what Arduino did with its HAL layer.
//
// Português:
//
//	Conteúdo estático dos dois arquivos auxiliares C que o backend
//	emite ao lado de main.c. iotmaker_runtime.h declara hooks
//	portáveis; iotmaker_runtime_stub.c implementa via POSIX
//	nanosleep para que o projeto compile no desktop sem trabalho
//	adicional. O maker substitui o stub por implementação específica
//	do alvo (Arduino, RP2040, ESP32, ...) sem tocar em main.c — o
//	contrato no header é estável.

// RuntimeHeader is the verbatim contents of the iotmaker_runtime.h
// file emitted by Emit. The text intentionally documents the
// contract in the file itself, because the maker is meant to read
// it before writing their per-target stub replacement.
//
// The header is C99: it uses <stdint.h> for int64_t and
// <stdbool.h> for bool. Both are part of the standard library on
// every C99-conformant compiler (gcc, clang, avr-gcc, arm-none-eabi-
// gcc, IAR, Keil, SDCC with --std-c99).
//
// Português:
//
//	Conteúdo literal de iotmaker_runtime.h. C99 padrão. Documenta o
//	contrato no próprio arquivo pra que o maker possa ler antes de
//	escrever a versão específica do alvo.
const RuntimeHeader = `/* iotmaker_runtime.h — IoTMaker generated runtime contract.
 *
 * This header declares the runtime hooks the generated program
 * depends on. For every hook here, a corresponding implementation
 * must be linked. iotmaker_runtime_stub.c provides a portable POSIX
 * implementation suitable for desktop testing. Replace it with a
 * target-specific implementation for embedded deployment.
 *
 * Per-target replacements:
 *
 *   Arduino   — delay() from <Arduino.h>
 *   RP2040    — sleep_us() from pico-stdio
 *   ESP-IDF   — vTaskDelay() from FreeRTOS
 *   Linux/Pi  — nanosleep() from <time.h>  (this stub)
 */
#ifndef IOTMAKER_RUNTIME_H
#define IOTMAKER_RUNTIME_H

#include <stdint.h>
#include <stdbool.h>

/* Suspend the current execution for ` + "`ns`" + ` nanoseconds. Used by
 * StatementLoopDuration to enforce a timed cadence between
 * iterations. Implementations may round to the nearest tick
 * supported by the platform — for example, Arduino delay() works in
 * milliseconds, so the AVR stub rounds ns down to ms. Sub-millisecond
 * cadences will not be honoured on AVR; use a platform with finer
 * timing for those. */
void iotmaker_sleep_ns(int64_t ns);

#endif /* IOTMAKER_RUNTIME_H */
`

// RuntimeStub is the verbatim contents of the iotmaker_runtime_stub.c
// file emitted by Emit. It implements every hook declared in
// iotmaker_runtime.h using POSIX primitives only — so the project
// compiles and runs on any Unix host with no extra setup.
//
// Why nanosleep and not usleep:
//
// usleep is deprecated in POSIX.1-2008. nanosleep is the
// recommended replacement and exists on every POSIX system we
// target: Linux (all distros), macOS, BSD, Cygwin/MinGW. The
// integer arithmetic uses LL suffixes so the constants are 64-bit
// regardless of the host's default int width — same reason the
// generated code uses LL for duration literals.
//
// Português:
//
//	Implementação POSIX dos hooks declarados em iotmaker_runtime.h.
//	Usa nanosleep (POSIX.1) e não usleep (deprecated). Aritmética
//	em long long para portabilidade — mesma razão que o código
//	gerado usa "LL" em literais de duração.
const RuntimeStub = `/* iotmaker_runtime_stub.c — POSIX implementation of the IoTMaker
 * runtime hooks. Suitable for desktop testing on Linux, macOS,
 * BSD, or any other POSIX-compliant host. For embedded deployment,
 * replace this file with a target-specific implementation.
 *
 * Example replacement for Arduino:
 *
 *   #include "iotmaker_runtime.h"
 *   #include <Arduino.h>
 *   void iotmaker_sleep_ns(int64_t ns) {
 *       delay((unsigned long)(ns / 1000000LL));  // ms granularity
 *   }
 */
#include "iotmaker_runtime.h"
#include <time.h>

void iotmaker_sleep_ns(int64_t ns) {
    struct timespec ts;
    ts.tv_sec  = (time_t)(ns / 1000000000LL);
    ts.tv_nsec = (long)  (ns % 1000000000LL);
    nanosleep(&ts, NULL);
}
`
