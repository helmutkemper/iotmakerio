// /server/codegen/backend/ansic/profile.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ansic

// profile.go — Target profile registry for the ANSI C backend.
//
// Why profiles exist:
//
// IoTMaker is cross-target by design. The same scene drawn in the
// IoTMaker IDE must produce C code that compiles and behaves correctly
// on targets as different as an Arduino Uno (AVR 8-bit, 16-bit int,
// software-emulated double-precision floats, 2 KB of RAM) and a
// Raspberry Pi running Linux (64-bit ARM, native double, gigabytes
// of RAM). Hardcoding a single mapping of "IR int" to "C int" is
// wrong for at least one of those targets.
//
// A TargetProfile captures the small set of decisions that change per
// target: which concrete C type stands in for the abstract IR types
// "int", "float", and "bool", and which suffix to append to numeric
// literals so they are interpreted correctly by the target's compiler.
// The maker picks a profile per project (eventually via a wizard
// dropdown; today via the metadata.targetProfile field in SceneJSON)
// and the C backend honours it throughout the emission.
//
// Where the profile name lives:
//
// The canonical name (one of the four constants below) travels inside
// SceneJSON under metadata.targetProfile. graph.MetadataInput parses
// it as a plain string. codeGen.go passes it to ResolveProfile right
// before invoking the backend. The Go backend ignores the field
// entirely, so adding a profile to a scene never affects Go output.
//
// Why this file imports nothing from the rest of codegen:
//
// Keeping profile.go self-contained (only standard library, no IR or
// graph dependencies) means tests for the profile registry can run in
// isolation, the file can be reused by any future tooling that needs
// to enumerate available targets without pulling the entire pipeline
// into the import graph, and the data here cannot accidentally
// develop coupling to evolving IR shapes.
//
// Adding a new profile:
//
// 1. Add a name constant in the const block below.
// 2. Add a package-level var with the TargetProfile literal.
// 3. Add the new var to the profilesByName map.
// 4. Add the new var to the slice returned by ListProfiles, in the
//    position you want it to appear in UI lists.
// 5. The TestProfile_RegistryConsistency test will catch any of the
//    above steps that are forgotten.
//
// Português:
//
//	Registro de perfis-alvo do backend C. O perfil define o mapeamento
//	dos tipos abstratos do IR (int/float/bool) para tipos concretos do
//	C, mais os sufixos de literal corretos para o alvo. O nome do
//	perfil viaja na cena via metadata.targetProfile e é resolvido por
//	ResolveProfile imediatamente antes da emissão. Para adicionar um
//	perfil novo, siga os cinco passos listados acima — o teste de
//	consistência pega esquecimentos.

// =====================================================================
//  Canonical profile names
// =====================================================================

// ProfileName* are the canonical string IDs stored in SceneJSON under
// metadata.targetProfile. Always reference these constants instead of
// re-typing the literal — the compiler will catch a typo immediately,
// while a stray "arduinouno" string would silently fall through to the
// default in ResolveProfile.
const (
	// ProfileNameArduinoUno targets AVR 8-bit MCUs (Uno, Nano, Mega328P,
	// Leonardo, Mega2560). It is the conservative default: 16-bit int,
	// 32-bit float ("double" is software-emulated and equal to float
	// on avr-gcc by default), 32 KB of flash. Picking arduino_uno when
	// the actual target is more capable wastes a bit of precision but
	// never produces broken code.
	ProfileNameArduinoUno = "arduino_uno"

	// ProfileNameCortexM targets 32-bit ARM Cortex-M MCUs (RP2040,
	// STM32, ESP32-C3, nRF52). int and long are 32-bit, int32_t is
	// native, double is typically software-emulated unless an FPU is
	// present, so we still emit float by default.
	ProfileNameCortexM = "cortex_m"

	// ProfileNamePiLinux targets full operating systems running on
	// 64-bit hardware (Raspberry Pi 3/4/Zero 2W, x86_64 Linux/macOS).
	// Native 64-bit integers and double-precision floats are cheap.
	ProfileNamePiLinux = "pi_linux"

	// ProfileNamePortable targets "unknown" — when the maker has not
	// decided yet or wants the most compatible C output across all
	// alvos. Currently identical to arduino_uno (same conservative
	// types and suffixes). May diverge in the future to use macros
	// from <inttypes.h> (INT32_C, etc.) for maximum portability.
	ProfileNamePortable = "portable"
)

// =====================================================================
//  The TargetProfile struct
// =====================================================================

// TargetProfile is the per-target translation contract used by the C
// backend. Every field here is consumed by emit.go (in tasks 4 and
// onward) at well-defined points:
//
//   - IntType / FloatType / BoolType pick the C type emitted for a
//     declaration when the IR carries the abstract names "int",
//     "float", and "bool".
//
//   - IntSuffix / FloatSuffix are appended to numeric literals so the
//     compiler interprets them with the intended width and precision.
//     They matter the most on AVR: a bare "10" is a 16-bit int that
//     can overflow, and a bare "3.14" is a double that triggers
//     expensive software emulation.
//
// All TargetProfile values are intended to be read-only after the
// package initialises. They are exposed as plain variables (not
// constants, because Go does not allow struct constants) but the
// codegen pipeline never writes to them and the test suite asserts
// that the registered set matches the exported variables.
//
// Português:
//
//	Contrato por alvo. IntType/FloatType/BoolType decidem o tipo C
//	emitido para os abstratos do IR. IntSuffix/FloatSuffix são
//	cruciais em AVR — sem o "f" um literal float vira double e usa
//	rotinas de software caras; sem o "L" um inteiro pode ficar em
//	16 bits e estourar.
type TargetProfile struct {
	// Name is the canonical ID (one of ProfileName* constants).
	// Stored in SceneJSON; never displayed to humans.
	Name string

	// Label is the human-readable name shown in future UI dropdowns.
	// May be translated; ResolveProfile does not consume it.
	Label string

	// IntType is the C type for the IR's abstract "int". Typically
	// "int32_t" or "int64_t".
	IntType string

	// FloatType is the C type for the IR's abstract "float". Typically
	// "float" or "double".
	FloatType string

	// BoolType is the C type for the IR's abstract "bool". In C99 this
	// is always "bool" (provided by <stdbool.h>), but the field exists
	// for future flexibility — for example a C89 profile would map it
	// to "int" with 0/1 conventions.
	BoolType string

	// IntSuffix is appended to integer literals. "L" forces a 32-bit
	// "long" interpretation on AVR where bare ints are 16-bit; "LL"
	// forces 64-bit on every C99 platform.
	IntSuffix string

	// FloatSuffix is appended to floating-point literals. "f" keeps a
	// constant as 32-bit float (avoiding accidental promotion to
	// double, which is a major performance trap on AVR); empty means
	// "use the platform's default — double in standard C".
	FloatSuffix string

	// ProvidesEntryPoints marks profiles whose RUNTIME owns main() and
	// calls reserved-name functions (setup/loop/serialEvent) by
	// external linkage — the Arduino model. On such profiles those
	// functions drop `static`, the uncalled warning is silenced, a
	// tunnel signature on them is an error, and main() is omitted when
	// the trunk is empty. Português: Marca profiles cujo RUNTIME é dono
	// do main() e chama funções de nome reservado por linkage externo —
	// o modelo Arduino. Nesses profiles elas perdem o `static`, o aviso
	// de uncalled silencia, assinatura nelas é erro, e o main() some
	// quando o tronco está vazio.
	ProvidesEntryPoints bool
}

// =====================================================================
//  Pre-defined profiles
// =====================================================================

// ProfileArduinoUno is the conservative default profile. It is also
// returned by ResolveProfile whenever the requested name is empty or
// unknown — making "do nothing" produce code that compiles for the
// smallest supported target instead of for a phantom "general" target
// that does not exist.
var ProfileArduinoUno = TargetProfile{
	Name:        ProfileNameArduinoUno,
	Label:       "Arduino Uno (AVR 8-bit)",
	IntType:     "int32_t",
	FloatType:   "float",
	BoolType:    "bool",
	IntSuffix:   "L",
	FloatSuffix: "f",
	// The Arduino core owns main() and calls setup()/loop()/serialEvent()
	// by external linkage — reserved-name Functions become the sketch's
	// entry points on this profile. Português: O core Arduino é dono do
	// main() e chama setup()/loop()/serialEvent() por linkage externo.
	ProvidesEntryPoints: true,
}

// ProfileCortexM matches arduino_uno's type choices today, but exists
// as a distinct profile so it can diverge later (for example by adding
// FPU-aware double support when targeting Cortex-M4F).
var ProfileCortexM = TargetProfile{
	Name:        ProfileNameCortexM,
	Label:       "ARM Cortex-M (RP2040, STM32, ESP32)",
	IntType:     "int32_t",
	FloatType:   "float",
	BoolType:    "bool",
	IntSuffix:   "L",
	FloatSuffix: "f",
}

// ProfilePiLinux uses the full width of native 64-bit arithmetic that
// these alvos support cheaply. The empty FloatSuffix yields C
// literals like 3.14 (parsed as double), matching the default
// behaviour of every desktop compiler.
var ProfilePiLinux = TargetProfile{
	Name:        ProfileNamePiLinux,
	Label:       "Raspberry Pi / Linux / macOS (64-bit)",
	IntType:     "int64_t",
	FloatType:   "double",
	BoolType:    "bool",
	IntSuffix:   "LL",
	FloatSuffix: "",
}

// ProfilePortable is identical to ProfileArduinoUno today; preserved
// as a separate identity so it can evolve into a "use <inttypes.h>
// macros everywhere" variant for maximum cross-compiler portability.
var ProfilePortable = TargetProfile{
	Name:        ProfileNamePortable,
	Label:       "Portable C99",
	IntType:     "int32_t",
	FloatType:   "float",
	BoolType:    "bool",
	IntSuffix:   "L",
	FloatSuffix: "f",
}

// =====================================================================
//  Registry
// =====================================================================

// profilesByName is the lookup table used by ResolveProfile. It is the
// single source of truth for "which names are recognised". When adding
// a new profile, this map must be updated — and the
// TestProfile_RegistryConsistency test will fail loudly if the var,
// the map, and ListProfiles drift out of sync.
//
// The map is intentionally not exported: external callers go through
// ResolveProfile (which handles unknown names) and ListProfiles (which
// returns a stable, ordered snapshot).
//
// Português:
//
//	Tabela de lookup usada por ResolveProfile. Quem adicionar um
//	perfil precisa atualizar este mapa também — o teste de
//	consistência avisa se algum estiver fora de sincronia.
var profilesByName = map[string]TargetProfile{
	ProfileNameArduinoUno: ProfileArduinoUno,
	ProfileNameCortexM:    ProfileCortexM,
	ProfileNamePiLinux:    ProfilePiLinux,
	ProfileNamePortable:   ProfilePortable,
}

// =====================================================================
//  Public API
// =====================================================================

// ResolveProfile returns the TargetProfile for the given canonical
// name. Empty strings and unknown names both resolve to
// ProfileArduinoUno — the conservative default. This is deliberate:
// every scene must produce *some* C code without further user
// intervention, even when the scene was authored before targetProfile
// existed as a field, or when the value was misspelled, or when a
// future profile name reaches an older server. The contract is
// "produce code that compiles for the smallest supported target",
// not "fail".
//
// The returned value is a copy of the registered profile, so callers
// may modify it locally without affecting subsequent calls — though
// no current call site does so.
//
// Português:
//
//	Resolve um nome canônico para o TargetProfile correspondente.
//	Nome vazio ou desconhecido cai em ProfileArduinoUno por design
//	— "produzir código pro menor alvo suportado", nunca falhar.
func ResolveProfile(name string) TargetProfile {
	if p, ok := profilesByName[name]; ok {
		return p
	}
	return ProfileArduinoUno
}

// ListProfiles returns every registered profile in display order
// (most restrictive first, fallback last). Intended for future UI
// dropdowns that let the maker pick a target from a list — the order
// here is the order the user will see.
//
// The slice is freshly allocated on each call, so callers may sort
// or filter it without side effects on subsequent calls.
//
// Português:
//
//	Lista todos os perfis registrados em ordem de exibição. Para uso
//	em dropdowns de seleção de alvo no painel do projeto (UI futura).
func ListProfiles() []TargetProfile {
	// Order: most restrictive first (arduino_uno = default), then
	// embedded 32-bit, then desktop/Pi, then the generic fallback.
	return []TargetProfile{
		ProfileArduinoUno,
		ProfileCortexM,
		ProfilePiLinux,
		ProfilePortable,
	}
}
