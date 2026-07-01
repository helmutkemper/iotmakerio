// /server/codegen/backend/ansic/emit.go

// Package ansic emits C99 source code from an IoTMaker IR program.
//
// This package is the C counterpart of backend/golang. It consumes the
// same ir.Program produced by ir.Emit and translates each instruction
// into C99 source text. The Go backend remains the reference
// implementation and the two backends produce semantically equivalent
// programs for every scene supported by the IR.
//
// Output shape:
//
// Emit returns a map keyed by relative file path within the exported
// project zip. Phase 1 always returns at least main.c; subsequent
// tasks add iotmaker_runtime.h and iotmaker_runtime_stub.c. The client
// (WASM IDE) zips this map into a single download.
//
// Why a map of files rather than a single string:
//
// Unlike Go, where every codegen output fits in one main.go, a C
// project needs at minimum a header declaring runtime hooks (sleep,
// future I/O abstractions) and an implementation file the maker can
// replace per target. Splitting these into separate files lets the
// maker regenerate main.c from the IoTMaker IDE without overwriting their
// target-specific runtime implementation.
//
// Why Emit takes a profile parameter:
//
// The IR carries abstract type names ("int", "float", "bool") that map
// to different concrete C types depending on the target architecture.
// The caller resolves a TargetProfile (via ResolveProfile) from the
// scene's metadata.targetProfile field and passes it to Emit so every
// declaration and literal emitted downstream is correct for the chosen
// alvo. See profile.go for the per-profile decisions and
// docs/CODEGEN_ANSI_C.md for the rationale.
//
// Phase status:
//
// This task implements the three foundational opcodes — OpConst,
// OpVar, OpAssign — plus the main(void) wrapping that turns an IR
// program into a compilable C file. Other opcodes (arithmetic,
// comparisons, control flow, sleep, black-box) are added by
// subsequent tasks and are silently ignored here. A scene that uses
// only unimplemented opcodes still produces a valid (empty) main.c.
//
// Português:
//
//	Pacote que emite C99 a partir do mesmo IR usado pelo backend Go.
//	Retorna um mapa de arquivos. Aceita um TargetProfile que decide
//	o mapeamento dos tipos abstratos do IR para tipos concretos.
//	Esta tarefa implementa CONST/VAR/ASSIGN mais o wrap do main; os
//	outros opcodes entram nas tarefas seguintes e ficam ignorados
//	por enquanto, sem quebrar.
package ansic

import (
	"fmt"
	"sort"
	"strings"

	"server/codegen/ir"
)

// =====================================================================
//  Public entry point
// =====================================================================

// Emit converts an IR program into a set of C99 source files using the
// type decisions captured in the provided profile.
//
// The returned map is keyed by relative path within the exported
// project. The exact set depends on whether the generated body
// touches the IoTMaker runtime:
//
//   - Always present:   main.c
//   - Conditional:      iotmaker_runtime.h, iotmaker_runtime_stub.c
//
// Scenes that never call a runtime symbol (no LoopDuration today)
// receive a single self-contained main.c the maker can compile with
// `gcc main.c -o test` on any host. Scenes that do call a runtime
// symbol receive all three files so the project builds end-to-end
// on a POSIX host out-of-the-box; the stub is swapped for a
// target-specific implementation when deploying to embedded.
//
// The signature is stable from Task 3 onward — every later task adds
// behaviour inside this function without changing what the caller in
// codeGen.go has to pass, protecting the Go path from collateral
// breakage during incremental development.
//
// Português:
//
//	Converte o IR em arquivos C. main.c sempre vai; header e stub
//	só viajam junto quando o corpo gerado realmente usa o runtime
//	(iotmaker_sleep_ns). Cenas simples ficam autocontidas em
//	main.c sozinho.
func Emit(prog *ir.Program, profile TargetProfile) map[string]string {
	e := &cEmitter{
		prog:     prog,
		profile:  profile,
		indent:   1, // body lives inside main(); one level of indent
		declared: make(map[string]bool),
	}

	e.emit()

	// Build main.c first — wrapMain reads e.usesRuntime to decide
	// whether to emit the `#include "iotmaker_runtime.h"` line, so
	// it has to run after emit() has walked every instruction.
	out := map[string]string{
		"main.c": e.wrapMain(),
	}

	// The runtime header and stub travel with main.c only when the
	// body actually depends on them. Cenas without LoopDuration
	// produce a single self-contained file the maker can compile
	// with `gcc main.c -o test` — no extra files, no dead
	// dependency. When the body calls iotmaker_sleep_ns, the two
	// extra files come along so the project builds end-to-end on
	// any POSIX host (the stub uses nanosleep), and the maker
	// swaps the stub for a target-specific implementation when
	// deploying to embedded.
	//
	// Português: Header e stub viajam com main.c apenas quando o
	// corpo realmente usa o runtime. Cenas simples saem com
	// main.c sozinho — sem dependências mortas. Quando precisa do
	// sleep, os dois arquivos extras entram pro projeto compilar
	// fora da caixa em qualquer host POSIX.
	if e.usesRuntime {
		out["iotmaker_runtime.h"] = RuntimeHeader
		out["iotmaker_runtime_stub.c"] = RuntimeStub
	}

	return out
}

// =====================================================================
//  Emitter state
// =====================================================================

// ifFrame tracks the state of one nested if/else block during code
// generation. Pushed onto cEmitter.ifStack by emitIfBegin, peeked by
// emitIfElseSep, popped by emitIfEnd. The three flags determine which
// of the four output forms the if/else takes — see emitIfBegin's
// doc comment for the form table.
//
// This struct intentionally mirrors backend/golang/emit.go's
// ifFrame field-for-field so the two backends evolve in step.
//
// Português:
//
//	Quadro de estado de um bloco if/else aninhado. Empilhado por
//	emitIfBegin, lido por emitIfElseSep, desempilhado por emitIfEnd.
//	Espelho exato do ifFrame do backend Go.
type ifFrame struct {
	hasTrue  bool // true branch has at least one instruction
	hasFalse bool // false branch has at least one instruction
	negated  bool // condition was negated (only false branch has content)
}

// cEmitter holds the per-call state of one Emit invocation: the IR
// being walked, the profile dictating type/literal choices, the
// running body buffer, and the bookkeeping that lets us distinguish
// a variable's first declaration from later assignments to the same
// name.
//
// Why declared is a map of dest names (not of full register refs):
//
// The IR uses a single namespace for destinations within a program.
// Every Dest field — whether it appears as the target of OpConst,
// OpVar, OpAssign, or any arithmetic — is a unique identifier in
// that namespace. The Go backend tracks this identical way (see
// goEmitter.declared) and uses it to decide between `:=` (first use)
// and `=` (reuse). In C the distinction is between a typed
// declaration ("int32_t x = ...;") and a plain assignment
// ("x = ...;"). The map answers that question with O(1) lookup.
//
// ifStack mechanics:
//
// The if/else family of opcodes uses a stack of ifFrame rather than
// a single field so nested if blocks are handled correctly: each
// OpIfBegin pushes a new frame, OpIfElse peeks the top, OpIfEnd
// pops. This is the same shape backend/golang/emit.go uses.
//
// Future state (added by later tasks):
//
//   - includes: a set of headers we know we need (<stdint.h>,
//     <stdbool.h>, "iotmaker_runtime.h"). Today we emit a fixed
//     three; Task 12 will make this dynamic.
//
//   - topLevel: a strings.Builder for declarations that must live
//     OUTSIDE main(), such as the typedef struct definitions for
//     black-box instances. Empty in Phase 1.
//
// Each of these is added when the task that needs it lands; the
// struct stays minimal until then to keep cognitive load low.
//
// Português:
//
//	Estado da emissão atual. declared mapeia nomes de destinos do IR
//	que já foram declarados — usado pra decidir entre "tipo nome =
//	valor;" (primeira vez) e "nome = valor;" (reuso). ifStack rastreia
//	if/else aninhados: cada IfBegin empilha um frame, IfElse consulta
//	o topo, IfEnd desempilha. Estado adicional (includes, topLevel)
//	será introduzido pelas tarefas que precisarem.
type cEmitter struct {
	prog     *ir.Program
	profile  TargetProfile
	body     strings.Builder
	indent   int
	declared map[string]bool
	ifStack  []ifFrame

	// switchStack tracks nested switch blocks. One bool per open switch:
	// true once a case/default body has been opened (so the next label, or
	// SWITCH_END, knows it must emit a `break;` and dedent first). Pushed on
	// SWITCH_BEGIN, popped on SWITCH_END.
	switchStack []bool

	// condStack tracks nested if/else-if chains (the StatementCase range/
	// comparison lowering). One bool per open chain: true once the first
	// branch is open, so the next COND_LABEL closes the previous branch with
	// `} else if` instead of opening a fresh `if`. Pushed on COND_BEGIN,
	// popped on COND_END. Mirrors switchStack but needs no `break;` — an
	// if/else-if chain is not a switch, so there is no fall-through to stop.
	condStack []bool

	// usesRuntime records whether the emitted body called any
	// function declared in iotmaker_runtime.h. Today the runtime
	// declares a single hook (iotmaker_sleep_ns) used by emitSleep,
	// so this flag is set only by that emitter; adding a new
	// runtime symbol means setting the flag wherever the symbol
	// is emitted.
	//
	// Read by wrapMain (to gate the `#include` line) and by Emit
	// (to gate the runtime header and stub files in the output
	// map). When false, the generated project consists of main.c
	// alone — a self-contained program that compiles with
	// `gcc main.c` without dragging in the runtime files.
	//
	// Português: Marca se o corpo gerado usa algo do runtime.
	// Setado em emitSleep (única chamada por enquanto). wrapMain e
	// Emit consultam pra decidir se geram o include e os arquivos
	// extras. False = main.c autocontido.
	usesRuntime bool

	// usesStddef records whether the emitted body referenced size_t,
	// which lives in <stddef.h> — neither <stdint.h> nor <stdbool.h>
	// (the two foundational includes) is guaranteed to provide it.
	// Today the only size_t producer is emitConstArray's `_len`
	// companion symbol, so this flag is set only there; any future
	// emitter that writes size_t must set it too.
	//
	// Read by wrapMain to gate the `#include <stddef.h>` line — the
	// same honest-artefact stance as usesRuntime: a header whose
	// symbols never appear in main.c is a dead dependency the maker
	// would have to chase down when extracting the file.
	//
	// Português: Marca se o corpo gerado usa size_t (que vem de
	// <stddef.h>). Hoje só o emitConstArray (símbolo `_len`) seta;
	// wrapMain consulta pra emitir o include apenas quando preciso —
	// mesma filosofia do usesRuntime: include só quando o símbolo
	// realmente aparece no arquivo.
	usesStddef bool
}

// =====================================================================
//  Dispatch loop
// =====================================================================

// emit walks the IR instructions and dispatches each opcode to its
// dedicated translator. Opcodes not yet implemented by the current
// phase fall through to the default branch and emit nothing, which
// is the deliberate "fail soft, fail visible" stance for incremental
// development: a scene using an unimplemented opcode produces a
// short main.c rather than a compile error or panic, and the
// missing translation is visible at code-review time.
//
// Why no error for unhandled opcodes:
//
// At this stage, the C backend is known to be incomplete — every
// later task adds opcode coverage. Emitting an error for unhandled
// opcodes would break the "always returns valid C99" contract that
// downstream tooling (zipping, the WASM client's preview) relies on.
// When all opcodes are handled (Task 14 closure), this dispatch may
// optionally add a panic-on-unknown default; for now it stays
// permissive.
//
// Phase 2 status:
//
// Black-box opcodes (OpBBDecl, OpBBProp, OpBBInit, OpBBMethod) appear
// in the IR only when the scene contains black-box instances.
// Validation in codeGen.go rejects scenes with missing BlackBoxDefs,
// so reaching this dispatch with a black-box instruction implies the
// definition was present. The Phase 1 behaviour is identical to
// other unimplemented opcodes: ignore. A future Phase 2 will replace
// these dispatch arms with real emitters.
//
// Português:
//
//	Loop que distribui cada instrução do IR pro tradutor específico.
//	Opcodes não implementados ainda caem no default e emitem nada —
//	contrato "sempre devolve C99 válido", quebra suave durante o
//	desenvolvimento incremental. Black-boxes (Fase 2) também caem
//	aqui por enquanto.
func (e *cEmitter) emit() {
	for _, inst := range e.prog.Instructions {
		switch inst.Op {
		case ir.OpConst:
			e.emitConst(inst)
		case ir.OpVar:
			e.emitVar(inst)
		case ir.OpAssign:
			e.emitAssign(inst)
		case ir.OpConstArray:
			e.emitConstArray(inst)
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv:
			e.emitBinOp(inst)
		case ir.OpConvert:
			e.emitConvert(inst)
		case ir.OpCmpEQ, ir.OpCmpNE, ir.OpCmpLT, ir.OpCmpGT, ir.OpCmpLE, ir.OpCmpGE:
			e.emitCompare(inst)
		case ir.OpLoopBegin:
			e.emitLoopBegin(inst)
		case ir.OpBreakIf:
			e.emitBreakIf(inst)
		case ir.OpLoopEnd:
			e.emitLoopEnd(inst)
		case ir.OpIfBegin:
			e.emitIfBegin(inst)
		case ir.OpIfElse:
			e.emitIfElseSep(inst)
		case ir.OpIfEnd:
			e.emitIfEnd(inst)
		case ir.OpSwitchBegin:
			e.emitSwitchBegin(inst)
		case ir.OpCaseLabel:
			e.emitCaseLabel(inst)
		case ir.OpDefaultLabel:
			e.emitDefaultLabel(inst)
		case ir.OpSwitchEnd:
			e.emitSwitchEnd(inst)
		case ir.OpCondBegin:
			e.emitCondBegin(inst)
		case ir.OpCondLabel:
			e.emitCondLabel(inst)
		case ir.OpCondDefault:
			e.emitCondDefault(inst)
		case ir.OpCondEnd:
			e.emitCondEnd(inst)
		case ir.OpSleep:
			e.emitSleep(inst)
		case ir.OpBBCall:
			// C99 standalone function-device call. The Go struct black-box
			// opcodes below are still Phase 2 (Go scenes don't reach the C
			// backend), but BB_CALL is C99-only and emitted here.
			e.emitBBCall(inst)
		// Opcodes intentionally never emitted in C:
		//   OpOutput  — display devices are an IDE concept
		//   OpReturn  — Go-only cosmetic no-op
		// Phase 2 (Go struct black-box):
		//   OpBBDecl/Prop/Init/Method                    — Phase 2
		default:
			// Permissive default; see function doc for rationale.
		}
	}
}

// =====================================================================
//  Native instruction emitters
// =====================================================================

// emitConst translates OpConst into a typed declaration with an
// initialiser. In C this is:
//
//	<type> <name> = <literal>;
//
// where <type> comes from the profile (via cTypeName), <name> is the
// cIdent-normalised Dest, and <literal> is the value with the
// per-profile suffix (via cLiteral) so the constant compiles
// correctly on the target architecture.
//
// After emission, declared[inst.Dest] is set so any later OpAssign
// targeting the same name emits a plain assignment instead of a
// duplicate declaration (which would be a C error).
//
// Why we never emit "static const":
//
// The Go backend produces local variables that the optimizer is free
// to fold into constants. The C compiler does the same with locals,
// so adding "static const" buys nothing while complicating the
// generated code and breaking simple assignments downstream. Locals
// without modifiers are the right baseline.
//
// Português:
//
//	Traduz OpConst para "tipo nome = literal;". Tipo e literal vêm
//	dos helpers, que consultam o perfil pra escolher int32_t/int64_t,
//	"L"/"LL", "f"/"" etc. Marca o destino como declarado pra
//	atribuições subsequentes virarem "nome = ..." sem redeclarar.
func (e *cEmitter) emitConst(inst ir.Instruction) {
	// OpConst always carries exactly one Arg: the literal value as
	// text. A malformed IR with zero args would index out of range;
	// guard explicitly so the emitter degrades gracefully instead
	// of panicking on bad input.
	if len(inst.Args) == 0 {
		return
	}

	name := cIdent(inst.Dest)
	cType := cTypeName(inst.Type, e.profile)
	val := cLiteral(inst.Type, inst.Args[0], e.profile)

	e.writef("%s %s = %s;\n", cType, name, val)
	e.declared[inst.Dest] = true
}

// emitVar translates OpVar into an uninitialised declaration:
//
//	<type> <name>;
//
// OpVar is produced by the IR emitter for values that need to live
// across a scope boundary — typically a value computed inside a loop
// body but read outside it, where the C language requires the
// variable to be declared in the enclosing scope and assigned to
// from inside.
//
// Compound Dest form ("instanceId:portName"):
//
// The Go backend handles this via goOperand. The C backend does the
// same via cOperand to keep the producer's identifier identical to
// the one consumers will reference. Until Phase 2 (black-box)
// arrives, the compound form does not appear in practice — the
// guard preserves the structural parity with the Go backend so the
// transition is a no-op when Phase 2 lands.
//
// Português:
//
//	Traduz OpVar para "tipo nome;" — declaração sem inicialização.
//	Surge quando o IR promove uma variável para o escopo externo
//	(cruzamento de escopo). Para Dest composto (forma de black-box),
//	usa cOperand pra produzir o mesmo identificador que os
//	consumidores referenciam.
func (e *cEmitter) emitVar(inst ir.Instruction) {
	name := cOperand("%" + inst.Dest)
	cType := cTypeName(inst.Type, e.profile)

	// A user variable (emitVariableDecls sets the "varInit" marker) is declared
	// WITH its zero initialiser — int32_t counter = 0; — because C does not
	// zero-initialise locals, so a variable that no SetVar ever writes would
	// otherwise hold garbage. A wire-promotion OpVar carries no marker: it is
	// assigned from inside the enclosing scope right after, so it stays a bare
	// declaration exactly as before. cLiteral formats the zero per type/profile
	// (int → 0, float → 0.0f, string → "").
	//
	// Português: Uma variável de usuário (marcador "varInit") é declarada COM
	// seu inicializador zero — int32_t counter = 0; — porque C não zera locais
	// e uma variável que nenhum SetVar escreve ficaria com lixo. Um OpVar de
	// promoção de fio não tem o marcador: é atribuído logo em seguida, então
	// continua declaração nua, como antes.
	if inst.Meta["varInit"] == "1" && len(inst.Args) > 0 {
		e.writef("%s %s = %s;\n", cType, name, cLiteral(inst.Type, inst.Args[0], e.profile))
	} else {
		e.writef("%s %s;\n", cType, name)
	}
	e.declared[inst.Dest] = true
}

// emitAssign translates OpAssign into a plain assignment:
//
//	<name> = <literal>;
//
// OpAssign is paired with a preceding OpVar (or OpConst): the
// variable has already been declared, and OpAssign updates its
// value. We do not re-emit the type. We do not consult declared[]
// because a malformed IR that issues OpAssign for an undeclared
// register would fail at the C compiler anyway, and surfacing it
// there is more useful than masking it with a synthetic declaration
// here.
//
// Note that inst.Args[0] is treated as a literal value, mirroring
// the Go backend's emitAssign — Args[0] for OpAssign is the value
// to store, not a register reference. If Phase 2 introduces an
// "ASSIGN from register" form, this function will need to handle
// the "%" prefix via cOperand instead.
//
// Português:
//
//	Traduz OpAssign para "nome = literal;". A variável já foi
//	declarada previamente por OpVar ou OpConst. Args[0] é tratado
//	como literal (espelho do backend Go).
func (e *cEmitter) emitAssign(inst ir.Instruction) {
	if len(inst.Args) == 0 {
		return
	}

	name := cIdent(inst.Dest)
	// Two source kinds, exactly as the Go backend's emitAssign: the scope-
	// crossing promotion scheme assigns a LITERAL (formatted by cLiteral),
	// while a SetVar device assigns a REGISTER — the value wired into it
	// (`%const_5`, `%counter`, or a black-box `%inst:port`) — which must
	// resolve to the producer's C identifier via cOperand. This is exactly the
	// "ASSIGN from register" form this function's note anticipated. Branching
	// on the "%" prefix keeps the literal path untouched.
	//
	// Português: Dois tipos de fonte, igual ao emitAssign do Go: a promoção que
	// cruza escopo atribui um LITERAL (via cLiteral); um device SetVar atribui
	// um REGISTRADOR — o valor ligado a ele (`%const_5`, `%counter` ou
	// `%inst:port`) — que precisa virar o identificador C do produtor via
	// cOperand. É exatamente a forma "ASSIGN from register" que a nota desta
	// função previu. Ramificar pelo "%" mantém o caminho do literal intacto.
	var val string
	if strings.HasPrefix(inst.Args[0], "%") {
		val = cOperand(inst.Args[0])
	} else {
		val = cLiteral(inst.Type, inst.Args[0], e.profile)
	}

	e.writef("%s = %s;\n", name, val)
}

// emitConstArray translates OpConstArray — a fixed-size constant collection
// literal (the StatementConstArray{Int,Float,String} device) — into a C fixed array plus its
// explicit length companion:
//
//	<elem> <dest>[] = {v1, v2, v3};
//	const size_t <dest>_len = 3;
//
// inst.Type carries the BARE element type and inst.Args the element
// literals, already formatted by the IR emitter (plain decimal numbers —
// never exponent form — and pre-quoted strings); see OpConstArray in
// ir/types.go. Element rendering reuses the same mappers as every scalar
// in this backend:
//
//   - cTypeName resolves the element type through the TargetProfile exactly
//     like emitConst/emitVar do (abstract "int" → profile.IntType, e.g.
//     int32_t on arduino_uno), so the collection's elements match every
//     other int the profile emits.
//   - cLiteral applies the per-type literal suffix to EACH element
//     (IntSuffix, float32's unconditional "f", float64's bare decimal).
//     Suffixes inside an initializer list are technically redundant in C99
//     (initializers convert implicitly), but keeping them makes the array
//     elements byte-identical to the same value emitted as a scalar const —
//     one literal style across the whole file.
//
// The size is fixed at design time — never malloc — and the `_len`
// companion is an explicit symbol (not sizeof at the use site) precisely
// so it SURVIVES POINTER DECAY when the array is passed to a function
// taking (const T*, size_t) — plan decision 3.
//
// ZERO-LENGTH STANCE: an empty initializer list `{}` is NOT valid C99
// (§6.7.8 requires at least one initializer), so an empty collection is
// emitted as a one-slot zeroed array with `_len = 0`:
//
//	<elem> <dest>[1] = {0};
//	const size_t <dest>_len = 0;
//
// This always compiles, and the (pointer, length) contract every consumer
// uses remains semantically exact — `_len` is the logical size, and a
// consumer iterating `_len` elements never touches the dummy slot. ({0}
// also zeroes any element type: 0 is a valid initializer for numerics,
// bool, and pointers — `const char*` gets NULL.) The IR has already
// attached an authoring warning for the empty list, so the maker is told;
// this stance just guarantees the artefact still compiles.
//
// The `_len` symbol references size_t, so the emitter flags usesStddef and
// wrapMain adds `#include <stddef.h>` — gated, same honest-artefact stance
// as usesRuntime.
//
// Português: Traduz OpConstArray para o array C fixo + o símbolo de
// comprimento `_len` (size_t — decisão 3 do plano: sobrevive ao decaimento
// do array para ponteiro na chamada). Elementos via cTypeName/cLiteral,
// como qualquer escalar do backend. Coleção vazia vira `[1] = {0}` com
// `_len = 0`, porque `{}` não é C99 válido — compila sempre e o contrato
// (ponteiro, comprimento) continua exato. O include <stddef.h> é emitido
// somente quando um array existe (flag usesStddef).
func (e *cEmitter) emitConstArray(inst ir.Instruction) {
	name := cIdent(inst.Dest)
	elemC := cTypeName(inst.Type, e.profile)

	n := len(inst.Args)
	if n == 0 {
		// Empty collection: `{}` is invalid C99 — emit a one-slot zeroed
		// array; `_len = 0` below carries the real (logical) size.
		e.writef("%s %s[1] = {0}; /* empty collection: see %s_len */\n",
			elemC, name, name)
	} else {
		elems := make([]string, n)
		for i, a := range inst.Args {
			elems[i] = cLiteral(inst.Type, a, e.profile)
		}
		e.writef("%s %s[] = {%s};\n", elemC, name, strings.Join(elems, ", "))
	}

	e.usesStddef = true
	e.writef("const size_t %s_len = %d;\n", name, n)
	e.declared[inst.Dest] = true
}

// emitBinOp translates OpAdd / OpSub / OpMul / OpDiv into a single C
// statement. Two output shapes share the same machinery, distinguished
// by the declared map:
//
//	First use of dest:
//	   <type> <name> = <a> <op> <b>;
//
//	Reuse of an already-declared dest (e.g. inside a loop body that
//	updates a value promoted to the outer scope by OpVar):
//	   <name> = <a> <op> <b>;
//
// Why this matters in C specifically:
//
// In Go, ":=" works as both declaration and assignment, so the Go
// backend can use the same syntax on the first occurrence and a plain
// "=" on subsequent ones. In C, redeclaring a variable is a compile
// error ("error: redefinition of 'add1'"), so the emitter MUST omit
// the type prefix on every occurrence after the first. The declared
// map answers "have I declared this register before?" with O(1)
// lookup.
//
// Operand resolution:
//
// Both Args carry the "%name" prefix that designates an IR register
// reference. cOperand strips the prefix and normalises the identifier
// the same way cIdent would, so the operands reference the exact same
// C names that the upstream emitters (Const, Var, BinOp) produced.
//
// Type provenance:
//
// inst.Type is set by the IR emitter to the result type of the
// operation, which is always the same as the operand types — the IR
// emitter inserts OpConvert before binary ops if the operands had
// different abstract types (Task 7 handles that), so by the time we
// reach this function the inputs are guaranteed homogeneous. We trust
// inst.Type and feed it straight to cTypeName.
//
// Português:
//
//	Traduz OpAdd/Sub/Mul/Div para uma linha C. Primeira ocorrência do
//	dest emite com tipo ("int32_t add1 = a + b;"). Reuso emite só
//	atribuição ("add1 = a + b;") — em C, redeclarar variável é erro
//	de compilação. Args vêm como "%name" (referências de registrador);
//	cOperand normaliza pra o mesmo identificador que os outros
//	emitters produzem.
func (e *cEmitter) emitBinOp(inst ir.Instruction) {
	// Binary ops always carry exactly two operands. A malformed IR
	// with fewer would index out of range; degrade gracefully like
	// emitConst does, so a bug upstream surfaces as missing output
	// rather than a panic.
	if len(inst.Args) < 2 {
		return
	}

	name := cIdent(inst.Dest)
	a := cOperand(inst.Args[0])
	b := cOperand(inst.Args[1])
	op := cBinOp(inst.Op)

	if e.declared[inst.Dest] {
		e.writef("%s = %s %s %s;\n", name, a, op, b)
	} else {
		cType := cTypeName(inst.Type, e.profile)
		e.writef("%s %s = %s %s %s;\n", cType, name, a, op, b)
		e.declared[inst.Dest] = true
	}
}

// emitConvert translates OpConvert into a C cast expression bound to
// a fresh local variable:
//
//	<targetType> <dest> = (<targetType>)<src>;
//
// The IR emitter inserts OpConvert automatically just before any
// arithmetic or comparison whose operands have heterogeneous types
// — see ir/emit.go's type-compat pass. By the time the C backend
// reaches that arithmetic the operand types are guaranteed
// homogeneous: the Convert produced a new register of the target
// type, and the arithmetic references that register instead of the
// original.
//
// Why prefix-cast syntax and not a function-style cast:
//
// Go writes this as "float64(x)" — call syntax, because Go does not
// have a separate cast grammar. C distinguishes:
//
//   - Prefix cast: "(float)x" — the C-style cast, valid since C89.
//   - Function-style cast: "float(x)" — C++ only; in C this is a
//     syntax error because float is not a callable function.
//
// We use the prefix form. It is the only one that compiles under
// every C compiler we target (gcc, clang, avr-gcc, arm-none-eabi-gcc,
// IAR, Keil, SDCC).
//
// Why we do not consult declared[]:
//
// OpConvert always produces a fresh register name (the IR emitter
// guarantees uniqueness via a counter or a "_conv" suffix on the
// source name). The dest never collides with a previously-declared
// variable in a well-formed IR program. We therefore emit the
// declaration unconditionally, matching the Go backend's
// emitConvert. If a malformed IR ever produced a duplicate dest the
// C compiler would surface "redefinition of 'name'" pointing at this
// line — the same failure mode the Go backend would have, just
// surfaced by the C toolchain instead of the Go one.
//
// Português:
//
//	Traduz OpConvert para um cast C em prefixo:
//	"<tipo_alvo> <dest> = (<tipo_alvo>)<src>;". A sintaxe de função
//	tipo "float(x)" é só C++; em C é erro. Não checa declared porque
//	o IR garante Dest único pra cada Convert.
func (e *cEmitter) emitConvert(inst ir.Instruction) {
	// OpConvert always carries exactly one operand: the source
	// register being cast. Defensive guard mirrors the other
	// emitters and degrades gracefully on malformed input.
	if len(inst.Args) != 1 {
		return
	}

	name := cIdent(inst.Dest)
	targetCType := cTypeName(inst.Type, e.profile)
	src := cOperand(inst.Args[0])

	e.writef("%s %s = (%s)%s;\n", targetCType, name, targetCType, src)
	e.declared[inst.Dest] = true
}

// emitCompare translates the six comparison opcodes
// (OpCmpEQ/NE/LT/GT/LE/GE) into a single C statement that binds the
// comparison result to a fresh bool. Two output shapes share the same
// machinery, distinguished by the declared map exactly like
// emitBinOp:
//
//	First use of dest:
//	   bool <name> = <a> <op> <b>;
//
//	Reuse of an already-declared dest:
//	   <name> = <a> <op> <b>;
//
// Why keep this separate from emitBinOp:
//
// The two functions are structurally identical today (same declared
// branch, same writef shape, just different op tables). Merging them
// behind a private emitInfixOp would save a few lines of duplication
// but break the one-to-one correspondence with backend/golang/emit.go,
// where emitBinOp and emitCompare are also separate. Keeping the
// parallel matters because future divergences will land separately:
// string equality, NaN-aware float comparisons, or shortcut evaluation
// for chained relational ops are all comparison-only concerns. The
// duplication today is the cheap price of independent evolution
// tomorrow.
//
// Type of the result:
//
// The IR emitter sets inst.Type to "bool" on every comparison
// instruction (see ir/emit.go's emitCompare). cTypeName resolves
// "bool" to profile.BoolType, which is "bool" in every C99 profile
// we ship. <stdbool.h> is included unconditionally by wrapMain so
// the type is always available without per-instruction setup.
//
// Português:
//
//	Traduz as seis comparações para uma linha C que cria um bool.
//	Mesma estrutura do emitBinOp (com declared check), mas mantida
//	em função separada pra espelhar o backend Go e permitir
//	divergência futura (comparação de strings, NaN-aware, etc.).
//	Resultado é sempre bool — vem do IR via inst.Type.
func (e *cEmitter) emitCompare(inst ir.Instruction) {
	// Comparison ops carry exactly two operands. A malformed IR
	// with fewer would index out of range; degrade gracefully.
	if len(inst.Args) < 2 {
		return
	}

	name := cIdent(inst.Dest)
	a := cOperand(inst.Args[0])
	b := cOperand(inst.Args[1])
	op := cCmpOp(inst.Op)

	if e.declared[inst.Dest] {
		e.writef("%s = %s %s %s;\n", name, a, op, b)
	} else {
		cType := cTypeName(inst.Type, e.profile)
		e.writef("%s %s = %s %s %s;\n", cType, name, a, op, b)
		e.declared[inst.Dest] = true
	}
}

// =====================================================================
//  Loop control flow emitters
// =====================================================================

// emitLoopBegin opens an infinite C loop:
//
//	while (1) {
//
// The IR emitter guarantees a matching OpLoopEnd, and the body in
// between will be emitted at one extra level of indentation thanks to
// the e.indent++ here. We use the "while (1)" idiom rather than
// "for (;;)" because while-form reads more naturally for the
// non-programmer audience the IoTMaker IDE targets, even though both
// are equally valid C99 and produce identical machine code under any
// modern compiler (gcc, clang, avr-gcc, arm-none-eabi-gcc).
//
// The IR's inst.Dest carries the loop's owning device ID (e.g.
// "stmLoop_1"). We do not currently use it for codegen — the loop has
// no name in C — but it remains available for future enhancements
// like inserting a labelled-break for nested-loop exit if/when the
// IoTMaker IDE grammar ever supports them.
//
// Português:
//
//	Abre um loop infinito C: "while (1) {". O IR garante o
//	LoopEnd correspondente. e.indent++ faz o corpo sair indentado um
//	nível extra. Idioma "while (1)" preferido a "for (;;)" pra
//	leitura natural; ambos compilam pra a mesma instrução em qualquer
//	compilador moderno.
func (e *cEmitter) emitLoopBegin(inst ir.Instruction) {
	e.writef("while (1) {\n")
	e.indent++
}

// emitBreakIf emits the conditional break that exits the enclosing
// loop when the condition is true:
//
//	if (<cond>) {
//	    break;
//	}
//
// This is the C-canonical way to translate the IR's BREAK_IF, which
// represents the wire connected to a StatementLoop's "stop" port. The
// condition is read from a previously-computed register (typically a
// comparison result), so cOperand resolves the "%name" reference
// without needing to know the underlying type — the C compiler will
// require it to be bool-coercible.
//
// Note the difference from Go:
//
//   - Go: "if cond { break }" — no parentheses around the condition.
//   - C:  "if (cond) { break; }" — parentheses required, semicolon
//     required after break. Both are language-level requirements,
//     not stylistic.
//
// The inner break sits one indent level deeper than the if, so we
// bump indent before writing it and pop afterwards. The closing brace
// returns to the loop body's indent level — NOT to the outer scope,
// because we are still inside the while.
//
// Português:
//
//	Emite "if (cond) { break; }" — break condicional que sai do loop
//	envolvente. Em C, parênteses ao redor da condição são obrigatórios
//	(diferente de Go). O break tem ponto e vírgula (idem). Indent é
//	bumped pra a linha do break e revertido pro brace de fechamento,
//	mantendo o nível do corpo do while.
func (e *cEmitter) emitBreakIf(inst ir.Instruction) {
	// BREAK_IF carries exactly one operand: the condition register.
	// Defensive guard mirrors the other emitters.
	if len(inst.Args) == 0 {
		return
	}

	cond := cOperand(inst.Args[0])
	e.writef("if (%s) {\n", cond)
	e.indent++
	e.writef("break;\n")
	e.indent--
	e.writef("}\n")
}

// emitLoopEnd closes the loop opened by emitLoopBegin:
//
//	}
//
// e.indent-- restores the outer scope's indent level before writing
// the closing brace, so the "}" lines up with the "while (1) {" that
// opened it. This is the standard way to write nested-block emitters
// in this style.
//
// Português:
//
//	Fecha o while: emite "}" com indent-- antes pra alinhar com o
//	"while (1) {" que o abriu.
func (e *cEmitter) emitLoopEnd(inst ir.Instruction) {
	e.indent--
	e.writef("}\n")
}

// =====================================================================
//  If/Else instruction emitters
//
//  The IR's emitter attaches metadata (hasTrue, hasFalse) to every
//  OpIfBegin so the backend can pick the optimal C form without
//  having to scan ahead through the instruction stream. The four
//  forms:
//
//    Both branches present:  if (cond) { ... } else { ... }
//    Only true branch:       if (cond) { ... }
//    Only false branch:      if (!cond) { ... }       ← negated
//    Neither branch:         /* empty if/else on c */ ← comment only
//
//  The mechanism is a stack of ifFrame values: each OpIfBegin pushes
//  a new frame, OpIfElse peeks the top to decide whether to emit a
//  "} else {" separator (or do nothing), and OpIfEnd pops the stack
//  before closing the block. The stack supports arbitrary nesting.
//
//  Português:
//
//	O IR fornece metadata (hasTrue, hasFalse) para que o backend
//	escolha a forma C ideal sem precisar escanear adiante. Quatro
//	formas: ambos branches; só true; só false (negado); ambos
//	vazios (só comentário). Uma pilha de ifFrame permite
//	aninhamento arbitrário.
// =====================================================================

// emitIfBegin opens an if/else block, picking the form based on the
// hasTrue/hasFalse metadata. Push the corresponding ifFrame so
// emitIfElseSep and emitIfEnd can mirror the choice.
//
// The "both empty" case is theoretical — in practice the IR's
// scope-resolution pass elides empty if/else blocks before emitting
// IF_BEGIN, so this branch never fires on real scenes. We keep it
// because the Go backend keeps it, and behaviour parity is the goal.
//
// Why parentheses are mandatory:
//
// C requires parentheses around the if condition (a grammar rule,
// not a stylistic choice). The Go backend writes "if cond {"; the C
// backend writes "if (cond) {". The same applies to the negated
// form "if (!cond) {".
//
// Português:
//
//	Abre um bloco if/else escolhendo entre 4 formas conforme o
//	metadata hasTrue/hasFalse. Empilha um ifFrame pra os outros
//	emitters mirror a escolha. Parênteses em volta da condição são
//	exigência sintática do C, não opção de estilo.
func (e *cEmitter) emitIfBegin(inst ir.Instruction) {
	cond := ""
	if len(inst.Args) > 0 {
		cond = cOperand(inst.Args[0])
	}

	hasTrue := inst.Meta["hasTrue"] == "true"
	hasFalse := inst.Meta["hasFalse"] == "true"

	// Negated form fires only when the false branch has content and
	// the true branch is empty — we invert the condition to avoid
	// emitting a useless "} else {" separator with empty content
	// before it.
	negated := !hasTrue && hasFalse

	e.ifStack = append(e.ifStack, ifFrame{
		hasTrue:  hasTrue,
		hasFalse: hasFalse,
		negated:  negated,
	})

	switch {
	case negated:
		// Only the false branch has content — negate the condition
		// and let that branch's content become the if body.
		// emitIfElseSep will be a no-op for this frame.
		e.writef("if (!%s) {\n", cond)
	case hasTrue || hasFalse:
		// At least one branch has content. The form (with or
		// without "else") is decided at emitIfElseSep time.
		e.writef("if (%s) {\n", cond)
	default:
		// Both branches empty — preserved for behaviour parity with
		// the Go backend even though the IR is not expected to emit
		// this in practice. The emitted form is comment-only.
		e.writef("/* empty if/else on %s */\n", cond)
	}
	e.indent++
}

// emitIfElseSep handles the boundary between the true branch (already
// emitted) and the false branch (about to be emitted). The Go backend
// calls this "emitIfElseSep" to emphasise that it is a separator
// rather than a standalone "else" keyword — depending on the frame's
// shape, it may emit "} else {", or nothing at all.
//
// Cases:
//
//   - frame.negated: the IF_ELSE separator is a no-op. The original
//     true branch was empty and the false branch became the body of
//     the "if (!cond)". There is no else to write.
//
//   - frame.hasTrue && frame.hasFalse: full if/else. Pop the indent
//     to align the "} else {" with the opening "if", write the
//     separator, push the indent back for the false branch body.
//
//   - frame.hasTrue && !frame.hasFalse: there is no false branch,
//     so no separator is needed.
//
//   - both flags false: emitIfBegin emitted a comment, not a block;
//     no separator applies. This path is reached only on the
//     theoretical "both empty" case carried for Go parity.
//
// Português:
//
//	Trata a fronteira entre o branch true (já emitido) e o branch
//	false (a emitir). Pode emitir "} else {" ou nada, conforme o
//	frame empilhado.
func (e *cEmitter) emitIfElseSep(inst ir.Instruction) {
	if len(e.ifStack) == 0 {
		return
	}
	frame := e.ifStack[len(e.ifStack)-1]

	switch {
	case frame.negated:
		// False-branch content was already routed into the "if
		// (!cond)" body — nothing to do here.
		return
	case frame.hasTrue && frame.hasFalse:
		// Full if/else. Close the true branch, open the false.
		e.indent--
		e.writef("} else {\n")
		e.indent++
	case frame.hasTrue && !frame.hasFalse:
		// True-only — no else to emit.
		return
	}
}

// emitIfEnd closes the if/else block and pops the stack. Matches
// emitLoopEnd's shape: indent-- before the closing brace so it
// aligns with the opening "if (...) {" (or "if (!...) {").
//
// Note on the "both empty" path: emitIfBegin in that case emitted a
// comment without an opening brace, but emitIfEnd unconditionally
// emits a closing brace. This is a known cosmetic mismatch
// preserved for behaviour parity with the Go backend. Because the
// IR never produces both-empty if blocks in practice, the mismatch
// has no observable effect on real scenes.
//
// Português:
//
//	Fecha o bloco e desempilha. indent-- antes do "}" pra alinhar
//	com o "if" que o abriu. Caso "both empty" tem um "}" sem "{"
//	correspondente — preservado pra paridade com o backend Go;
//	IR real não produz esse caso na prática.
func (e *cEmitter) emitIfEnd(inst ir.Instruction) {
	if len(e.ifStack) > 0 {
		e.ifStack = e.ifStack[:len(e.ifStack)-1]
	}
	e.indent--
	e.writef("}\n")
}

// =====================================================================
//  Switch/case instruction emitters
//
//  C aligns `case` labels with the `switch` keyword and indents each case
//  body one extra level. Unlike Go, C falls through, so every case body is
//  terminated with `break;` before the next label (and before the closing
//  brace). A case that matches several values emits one `case V:` label per
//  value, stacked. switchStack[top] records whether a case body is open so
//  closePrevCase knows when to emit the `break;` and dedent.
//
//  Português: Em C o `case` alinha com o `switch` e o corpo entra um nível.
//  Diferente do Go, C tem fallthrough, então todo corpo termina com `break;`
//  antes do próximo label (e antes da chave de fechamento). Um case com vários
//  valores emite um `case V:` por valor.
// =====================================================================

func (e *cEmitter) emitSwitchBegin(inst ir.Instruction) {
	sel := ""
	if len(inst.Args) > 0 {
		sel = cOperand(inst.Args[0])
	}
	e.writef("switch (%s) {\n", sel)
	e.switchStack = append(e.switchStack, false)
}

// closePrevCase terminates the currently-open case body (if any) with a
// `break;` and closes the brace block that scopes the case's declarations.
//
// The brace block is required: in C a label (including `case`) labels a
// statement, and a declaration is not a statement, so `case 0: int x = …;`
// is ill-formed. Wrapping each body in `{ … }` makes the declarations valid.
//
// Português: Fecha o corpo do case atual com `break;` e a chave do bloco. O
// bloco é obrigatório porque em C um `case` rotula uma instrução e uma
// declaração não é instrução — `case 0: int x = …;` é inválido sem `{ }`.
func (e *cEmitter) closePrevCase() {
	if n := len(e.switchStack); n > 0 && e.switchStack[n-1] {
		e.writef("break;\n")
		e.indent--
		e.writef("}\n")
	}
}

func (e *cEmitter) emitCaseLabel(inst ir.Instruction) {
	e.closePrevCase()
	for _, v := range inst.Args {
		e.writef("case %s:\n", v)
	}
	e.writef("{\n") // brace block so declarations in the body are valid C
	e.indent++
	if n := len(e.switchStack); n > 0 {
		e.switchStack[n-1] = true
	}
}

func (e *cEmitter) emitDefaultLabel(inst ir.Instruction) {
	e.closePrevCase()
	e.writef("default:\n")
	e.writef("{\n")
	e.indent++
	if n := len(e.switchStack); n > 0 {
		e.switchStack[n-1] = true
	}
}

func (e *cEmitter) emitSwitchEnd(inst ir.Instruction) {
	e.closePrevCase()
	if n := len(e.switchStack); n > 0 {
		e.switchStack = e.switchStack[:n-1]
	}
	e.writef("}\n")
}

// emitCondBegin opens an if/else-if chain (the StatementCase range/comparison
// lowering). It only pushes the stack — no code is emitted until the first
// COND_LABEL. Unlike a switch, the chain needs no `break;` and no per-branch
// brace block for declarations: each `if`/`else if`/`else` already introduces
// its own `{ … }`, so `if (sel == 0) { int x = …; }` is valid C as-is.
func (e *cEmitter) emitCondBegin(inst ir.Instruction) {
	e.condStack = append(e.condStack, false)
}

// emitCondLabel renders one branch. Args[0] is the resolved selector and
// Args[1:] the case operands; ir.BuildCaseCondition turns them plus
// Meta["matchKind"] into the boolean expression, which is then wrapped in C's
// parenthesised `if (...)` form. The first branch emits `if (<cond>) {`; later
// branches close the previous body and emit `} else if (<cond>) {`.
func (e *cEmitter) emitCondLabel(inst ir.Instruction) {
	sel := ""
	if len(inst.Args) > 0 {
		sel = cOperand(inst.Args[0])
	}
	var operands []string
	if len(inst.Args) > 1 {
		operands = inst.Args[1:]
	}
	cond := ir.BuildCaseCondition(sel, inst.Meta["matchKind"], operands)

	if n := len(e.condStack); n > 0 && e.condStack[n-1] {
		e.indent-- // close the previous branch body
		e.writef("} else if (%s) {\n", cond)
	} else {
		e.writef("if (%s) {\n", cond)
		if n := len(e.condStack); n > 0 {
			e.condStack[n-1] = true
		}
	}
	e.indent++
}

// emitCondDefault renders the chain's trailing `else`. A Case that reaches the
// chain lowering always has at least one preceding branch, so the previous
// body is open here.
func (e *cEmitter) emitCondDefault(inst ir.Instruction) {
	if n := len(e.condStack); n > 0 && e.condStack[n-1] {
		e.indent-- // close the previous branch body
	}
	e.writef("} else {\n")
	e.indent++
	if n := len(e.condStack); n > 0 {
		e.condStack[n-1] = true
	}
}

// emitCondEnd closes the chain.
func (e *cEmitter) emitCondEnd(inst ir.Instruction) {
	if n := len(e.condStack); n > 0 {
		if e.condStack[n-1] {
			e.indent-- // close the last branch body
		}
		e.condStack = e.condStack[:n-1]
	}
	e.writef("}\n")
}

// =====================================================================
//  Sleep emitter (OpSleep)
// =====================================================================

// emitSleep translates OpSleep into a call to the IoTMaker runtime's
// sleep hook:
//
//	iotmaker_sleep_ns(<source>);
//
// OpSleep is produced by the IR emitter at the end of every
// StatementLoopDuration body (see ir/emit.go's emitScope), one per
// iteration. The argument is a register holding a time.Duration
// value in nanoseconds.
//
// Why the runtime hook instead of a portable sleep:
//
// C has no portable sleep primitive. POSIX has nanosleep, Arduino
// has delay(), the RP2040 SDK has sleep_us, ESP-IDF has
// vTaskDelay, bare-metal hardware has nothing. Generating the
// portable name iotmaker_sleep_ns and letting the maker (or a
// future board-support package) provide the implementation keeps
// the generated program target-independent. See runtime.go for the
// contract and the default POSIX stub.
//
// The hook is declared in iotmaker_runtime.h, which wrapMain
// includes unconditionally — see wrapMain's doc for the rationale
// of always including the runtime header.
//
// Português:
//
//	Traduz OpSleep para uma chamada de iotmaker_sleep_ns(<src>). O
//	hook é declarado em iotmaker_runtime.h (incluído sempre) e
//	implementado em iotmaker_runtime_stub.c (POSIX nanosleep), que
//	o maker substitui pra embarcado. Mantém o programa gerado
//	independente do alvo.
func (e *cEmitter) emitSleep(inst ir.Instruction) {
	// Sleep carries exactly one operand: the duration register.
	// Defensive guard mirrors the other emitters.
	if len(inst.Args) == 0 {
		return
	}

	// iotmaker_sleep_ns lives in iotmaker_runtime.h — the moment we
	// emit a call to it we commit to including the runtime in the
	// output. Set the flag before writef so a future panic in the
	// formatter doesn't leave us in a "code uses sleep but header
	// is missing" state.
	e.usesRuntime = true

	src := cOperand(inst.Args[0])
	e.writef("iotmaker_sleep_ns(%s);\n", src)
}

// =====================================================================
//  Operator mapping helpers
// =====================================================================

// cBinOp maps an arithmetic IR opcode to its C operator string.
//
// The four arithmetic operators in C share the same notation as Go
// (and most C-family languages), so the mapping is one-to-one. The
// helper exists, rather than being inlined into emitBinOp, so the
// next tasks (comparisons, logical ops) can mirror the same pattern
// without duplicating the switch logic that lives here.
//
// The "?" fallback for unknown opcodes is intentionally invalid C —
// if a future opcode is accidentally routed through emitBinOp without
// adding a case here, the generated file will fail to compile with a
// clear "expected expression before '?'" error rather than silently
// emitting wrong code.
//
// Português:
//
//	Mapeia opcodes aritméticos do IR para operadores C. Convenção
//	idêntica ao Go (e à família C). Fallback "?" para opcodes
//	desconhecidos é deliberadamente inválido em C, pra surfacing
//	loud durante o desenvolvimento.
func cBinOp(op ir.Op) string {
	switch op {
	case ir.OpAdd:
		return "+"
	case ir.OpSub:
		return "-"
	case ir.OpMul:
		return "*"
	case ir.OpDiv:
		return "/"
	default:
		return "?"
	}
}

// cCmpOp maps a comparison IR opcode to its C operator string.
//
// All six C99 relational and equality operators are identical to
// Go's (and most C-family languages'), so the mapping is one-to-one.
// As with cBinOp, the fallback "?" is intentionally invalid C — a
// future opcode routed through emitCompare without a case here will
// fail to compile loudly rather than emit silently wrong code.
//
// Important: unlike some other languages, C99 has no separate
// equality-vs-identity distinction. "==" compares values for built-in
// types; for pointers it compares the addresses. Phase 1 never
// produces comparisons over pointer types — they would arrive from
// black-box outputs (Phase 2) and need a dedicated translation pass
// at that point.
//
// Português:
//
//	Mapeia opcodes de comparação do IR para operadores C. Convenção
//	idêntica ao Go. Fallback "?" é inválido em C deliberadamente.
//	C99 não tem "===" — "==" é o único operador de igualdade.
//	Comparações sobre ponteiros (Fase 2 via black-box) precisarão de
//	tratamento próprio quando chegarem.
func cCmpOp(op ir.Op) string {
	switch op {
	case ir.OpCmpEQ:
		return "=="
	case ir.OpCmpNE:
		return "!="
	case ir.OpCmpLT:
		return "<"
	case ir.OpCmpGT:
		return ">"
	case ir.OpCmpLE:
		return "<="
	case ir.OpCmpGE:
		return ">="
	default:
		return "?"
	}
}

// =====================================================================
//  Output wrapping
// =====================================================================

// writef appends one indented line to the body buffer. The indent is
// scaled by e.indent (one level == four spaces, the conventional
// indent for C). Use this for every line of generated C code so the
// output is uniformly indented and re-indented when nested scopes
// (loops, ifs) increment e.indent.
//
// Why four spaces, not tab:
//
// C codebases overwhelmingly use four-space indent. Tab indentation
// is Go-specific and would look out of place in a generated .c file
// that a maker may open in Arduino IDE, VS Code, or any other C
// editor where the default tab width is two or eight.
//
// Português:
//
//	Acrescenta uma linha ao body com a indentação atual. Convenção
//	C: 1 nível = 4 espaços (não tab, que é convenção Go).
func (e *cEmitter) writef(format string, args ...interface{}) {
	for i := 0; i < e.indent; i++ {
		e.body.WriteString("    ")
	}
	fmt.Fprintf(&e.body, format, args...)
}

// wrapMain assembles the final main.c from the per-call state: the
// fixed header comment documenting the active profile, the includes
// the program needs, and the main() function wrapping the emitted
// body. The result is a single string ready to be placed under the
// "main.c" key in the returned map.
//
// Includes emitted today:
//
//   - <stdint.h>              — required for int32_t / int64_t / etc.
//   - <stdbool.h>             — required for bool / true / false
//   - "iotmaker_runtime.h"    — runtime hooks (sleep, future I/O
//     abstractions). Declared in the header
//     file emitted alongside main.c; the
//     default implementation lives in
//     iotmaker_runtime_stub.c.
//
// All three are unconditional because:
//
//   - Their cost is zero: a few bytes of preprocessor time and no
//     runtime cost.
//
//   - Predicting which one is needed would require a pre-pass over
//     the IR; emitting them up-front lets us keep emit() a simple
//     dispatch loop.
//
//   - Every program likely needs them anyway: any int operation hits
//     stdint, any bool comparison hits stdbool, any LoopDuration hits
//     the runtime header.
//
// Task 12 generalises into a small include-management API and may
// then make individual includes conditional on whether the IR
// actually uses them — but the cost of "always include" is so low
// that it could equally stay this way.
//
// Why a return 0 even when the body is empty:
//
// C requires main to return int. A bare main without return is
// undefined behaviour pre-C99 and a warning afterwards. Always
// emitting "return 0;" keeps the output clean even for trivial
// scenes that produce no instructions at all.
//
// Português:
//
//	Monta o main.c final: header com perfil, includes, main(void)
//	envolvendo o body, return 0. Includes sempre presentes: stdint.h
//	e stdbool.h. Programas vazios (cena sem opcodes traduzíveis)
//	geram um main válido com só "return 0;".
//
// emitBBCall translates OpBBCall — a C99 standalone function-device call —
// into a C function call. The handle/values flow purely on wires, so there is
// no instance: just `<fn>(<args>)`, optionally capturing the return value.
//
//	return port wired:    <retType> <var> = <fn>(<args>);
//	return port unwired:  <fn>(<args>);
//
// Args are produced by the IR in parameter order: inputs as resolved producer
// registers, out-params (mutable-pointer outputs the function writes) as
// "&"-prefixed registers. The return type and out-param value types come from
// the function's output ports in the BlackBoxDef (authored types, so the
// declarations match the signature). PassThrough handle aliasing is resolved
// earlier in the IR, so the backend never sees a pass-through register here.
// See docs/CODEGEN_C99_STAGE.md §6.
//
// Português: Traduz OpBBCall (chamada de função-livre C99) para uma chamada C.
// Sem instância — o handle flui por fio. Captura o retorno quando a porta
// "return" está conectada; senão emite a chamada nua. Out-params e alias de
// PassThrough ainda não são emitidos (cobre primeiro o retorno único).
func (e *cEmitter) emitBBCall(inst ir.Instruction) {
	fn := inst.Meta["fn"]
	if fn == "" {
		return // malformed IR: BB_CALL with no function name; degrade soft
	}

	// Declare any out-param variables before the call: the function writes to
	// them through the &address we pass. The declared type is the out-param's
	// VALUE type — the output's authored GoType with one pointer level removed
	// — so `float *temperature` yields `float <var>;` passed as `&<var>`. The
	// same register is what downstream consumers resolve to, so a wired
	// temperature/humidity feeds the next block.
	for _, a := range inst.Args {
		if !strings.HasPrefix(a, "&") {
			continue
		}
		reg := a[1:] // "%nodeId:portName"
		key := strings.TrimPrefix(reg, "%")
		if e.declared[key] {
			continue
		}
		valType := cDerefType(e.cOutputType(fn, outParamPortName(reg)))
		e.writef("%s %s;\n", valType, cOperand(reg))
		e.declared[key] = true
	}

	// Build the call. Each arg carries how it must be rendered:
	//   "&reg"        out-param passed by address
	//   "=literal"    literal default for an unwired input (verbatim)
	//   "(type)reg"   scalar input cast to its parameter's authored type
	//   "#reg"        collection LENGTH companion: operand + "_len"
	//                 (the `slice:` pair's second argument — plan T7)
	//   "reg"         input register, no cast (pointers / handles /
	//                 collection arrays, which decay to pointers)
	args := make([]string, 0, len(inst.Args))
	for _, a := range inst.Args {
		switch {
		case strings.HasPrefix(a, "&"):
			args = append(args, "&"+cOperand(a[1:]))
		case strings.HasPrefix(a, "#"):
			// Collection length companion: the const array declared
			// `const size_t <name>_len = N;` exactly for this moment —
			// unlike sizeof, the symbol survives the array's decay to a
			// pointer in the argument right before it.
			args = append(args, cOperand(a[1:])+"_len")
		case strings.HasPrefix(a, "="):
			args = append(args, a[1:])
		case strings.HasPrefix(a, "("):
			// "(type)%register": keep the cast prefix verbatim, render the
			// operand. Scalar C types contain no ')', so the first ')' closes
			// the cast.
			if i := strings.IndexByte(a, ')'); i >= 0 {
				args = append(args, a[:i+1]+cOperand(a[i+1:]))
			} else {
				args = append(args, cOperand(a))
			}
		default:
			args = append(args, cOperand(a))
		}
	}
	call := fn + "(" + strings.Join(args, ", ") + ")"

	// Capture the return value only when the "return" output is wired.
	returnWired := false
	for _, name := range strings.Split(inst.Meta["connectedOutputs"], ",") {
		if name == "return" {
			returnWired = true
			break
		}
	}

	if !returnWired {
		e.writef("%s;\n", call)
		return
	}

	retType := e.cFunctionReturnType(fn)
	varName := cOperand("%" + inst.Dest + ":return")
	e.writef("%s %s = %s;\n", retType, varName, call)
	e.declared[inst.Dest+":return"] = true
}

// cFunctionReturnType resolves the C return type of a C99 function-device from
// its BlackBoxDef (keyed by function name; see store.LoadBlackBoxDefsForScene).
// The authored type is used verbatim so the declared variable matches the
// function's signature. Falls back to the profile int type when the def or the
// "return" port is unavailable, so a missing def degrades to compilable C
// rather than a blank type.
//
// Português: Resolve o tipo de retorno C do function-device a partir do
// BlackBoxDef. Usa o tipo autoral verbatim; cai no tipo int do profile se o
// def ou a porta "return" não estiverem disponíveis.
func (e *cEmitter) cFunctionReturnType(fn string) string {
	return e.cOutputType(fn, "return")
}

// cOutputType resolves the authored C type of a C99 function-device's output
// port (by name) from its BlackBoxDef — used for the return capture and, after
// dereferencing, for out-param variable declarations. Falls back to the profile
// int type when the def or the port is unavailable, so a missing def degrades
// to compilable C rather than a blank type.
//
// Português: Resolve o tipo C autoral de uma porta de saída (por nome) a partir
// do BlackBoxDef. Cai no tipo int do profile se o def ou a porta faltarem.
func (e *cEmitter) cOutputType(fn, portName string) string {
	if e.prog != nil && e.prog.BlackBoxDefs != nil {
		if def := e.prog.BlackBoxDefs[fn]; def != nil {
			for i := range def.Functions {
				if def.Functions[i].Name != fn {
					continue
				}
				for _, out := range def.Functions[i].Outputs {
					if out.Name == portName && out.GoType != "" {
						return out.GoType
					}
				}
			}
		}
	}
	return e.profile.IntType
}

// cDerefType removes one trailing pointer level from a C type so an out-param
// declared as `float *` becomes the value type `float`: the caller declares the
// value and passes its address. Returns the type unchanged when not a pointer.
//
// Português: Remove um nível de ponteiro do tipo C ("float *" → "float"); o
// chamador declara o valor e passa o endereço.
func cDerefType(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasSuffix(t, "*") {
		return strings.TrimSpace(t[:len(t)-1])
	}
	return t
}

// outParamPortName extracts the port name from a composite register of the form
// "%nodeId:portName".
func outParamPortName(reg string) string {
	if i := strings.LastIndex(reg, ":"); i >= 0 {
		return reg[i+1:]
	}
	return ""
}

// deviceSources returns the verbatim authored C source of every C99
// function-device the scene uses, de-duplicated, to inline ahead of main().
// A C99 device's implementation lives in the maker's own source (the parser
// keeps signatures, not bodies), so main.c must carry it to be self-contained
// and compilable. Only function-device defs (empty struct name) with a
// non-empty RawSource are inlined; Go-path defs are re-emitted from
// StructCode/MethodsCode elsewhere and skipped here. Output is sorted for
// deterministic builds; duplicate standard-library `#include`s in the inlined
// source are harmless (header-guarded).
//
// Português: Devolve o source autoral verbatim de cada function-device C99 da
// cena, deduplicado, pra inserir antes do main(). A implementação de um device
// C99 mora no source do maker (o parser guarda assinaturas, não corpos), então
// o main.c precisa carregá-lo pra compilar. Ordenado pra build reprodutível.
func (e *cEmitter) deviceSources() string {
	if e.prog == nil || len(e.prog.BlackBoxDefs) == 0 {
		return ""
	}
	seen := make(map[string]bool)
	var sources []string
	for _, def := range e.prog.BlackBoxDefs {
		if def == nil || def.Name != "" || def.RawSource == "" {
			continue // only C99 function-device sources
		}
		if seen[def.RawSource] {
			continue
		}
		seen[def.RawSource] = true
		sources = append(sources, def.RawSource)
	}
	if len(sources) == 0 {
		return ""
	}
	sort.Strings(sources)

	var sb strings.Builder
	sb.WriteString("/* ── authored device sources (inlined verbatim) ── */\n")
	for _, src := range sources {
		sb.WriteString(src)
		if !strings.HasSuffix(src, "\n") {
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (e *cEmitter) wrapMain() string {
	var sb strings.Builder

	// Header — documents the profile and the per-target type
	// decisions in effect for this build. Same information that
	// appeared in the earlier placeholder; kept here because the
	// tests in Task 3 (TestCBackend_ProfileResolution) assert
	// against these substrings.
	fmt.Fprintf(&sb,
		"/* main.c — generated by IoTMaker codegen.\n"+
			" * Do not edit by hand. Regenerate from the IoTMaker IDE to update.\n"+
			" *\n"+
			" * Target profile: %s\n"+
			" *   Integer type: %s\n"+
			" *   Float type:   %s\n"+
			" */\n\n",
		e.profile.Name,
		e.profile.IntType,
		e.profile.FloatType,
	)

	// Includes — <stdint.h> and <stdbool.h> are foundational and
	// always emitted. The IoTMaker runtime header is project-local
	// (quoted instead of angle-bracketed) and only emitted when the
	// body called something from it. Including a header whose
	// symbols never appear in the file would be a dead dependency
	// that the maker has to chase down if they extract main.c to
	// build elsewhere; gating on e.usesRuntime keeps the artefact
	// honest.
	sb.WriteString("#include <stdint.h>\n")
	sb.WriteString("#include <stdbool.h>\n")
	if e.usesStddef {
		// size_t (the `_len` companion of a constant collection) lives in
		// <stddef.h> — neither stdint nor stdbool guarantees it. Gated on
		// actual use, same stance as the runtime header below.
		sb.WriteString("#include <stddef.h>\n")
	}
	if e.usesRuntime {
		sb.WriteString("#include \"iotmaker_runtime.h\"\n")
	}
	sb.WriteString("\n")

	// Authored C99 device sources, inlined verbatim so main.c is
	// self-contained: the function-device implementations (and their
	// wire-type/`#include`s) live in the maker's source, not in the parsed
	// metadata. Empty for Go scenes and for C scenes with no function-devices.
	if ds := e.deviceSources(); ds != "" {
		sb.WriteString(ds)
	}

	// main(void) — the IR's body lives here, indented by writef.
	sb.WriteString("int main(void) {\n")
	sb.WriteString(e.body.String())

	// Always-present return — the body never emits this itself, so
	// wrapMain owns it. Indented like the body for consistency.
	sb.WriteString("    return 0;\n")
	sb.WriteString("}\n")

	return sb.String()
}
