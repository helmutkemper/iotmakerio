// /server/codegen/ir/types.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ir

// types.go — Intermediate Representation types for the codegen pipeline.
//
// The IR is a linear list of instructions produced by the emitter from
// the computation graph. Backends consume IR to produce target language code.
//
// Português: Tipos da Representação Intermediária para o pipeline de codegen.
// O IR é uma lista linear de instruções produzida pelo emitter a partir do
// grafo de computação. Backends consomem IR para produzir código na linguagem alvo.

import (
	"fmt"
	"strings"

	"server/codegen/blackbox"
)

// Op defines the instruction opcode.
type Op string

const (
	// Constants and variables
	OpConst Op = "CONST" // CONST %dest type value
	OpVar   Op = "VAR"   // VAR %dest type initialValue

	// OpFuncBegin / OpFuncEnd delimit a NAMED-FUNCTION region: everything
	// between them is the function's body, lifted OUT of main by each
	// backend (the linkage axis of the embedded family — ARDUINO_TARGET
	// §3). Dest carries the exact function name; doctrine 1 applies: the
	// name is emitted verbatim, EXEMPT from the iotm_ prefix (maker's own
	// code, not a third-party black-box). Português: Delimitam uma
	// região-função nomeada, içada para fora do main pelos backends;
	// Dest é o nome exato, isento do prefixo (doutrina 1).
	OpFuncBegin Op = "FUNC_BEGIN" // FUNC_BEGIN %name
	OpFuncEnd   Op = "FUNC_END"   // FUNC_END
	OpAssign    Op = "ASSIGN"     // ASSIGN %dest type %source

	// OpConstArray declares a fixed-size CONSTANT COLLECTION literal — the
	// IR form of the StatementConstArray{Int,Float,String} device (e.g. []int{1, 2, 3}).
	//
	// Shape: CONST_ARRAY %dest elemType v1 v2 v3 …
	//
	//   - Type holds the BARE ELEMENT type ("int", "float32", …), never the
	//     collection token ("[]int") — each backend rebuilds the collection
	//     form idiomatically, and the length is simply len(Args).
	//   - Args hold the element literals, already formatted by the IR
	//     emitter (numbers in plain decimal, strings pre-quoted), so the
	//     backends consume them exactly like OpConst values.
	//
	// Backends translate it as:
	//
	//	Go: <dest> := []<elem>{v1, v2, v3}
	//	C:  <elem> <dest>[] = {v1, v2, v3};
	//	    const size_t <dest>_len = 3;  // explicit length companion —
	//	                                  // survives pointer decay at call sites
	//
	// The collection is a compile-time literal (size fixed at design time),
	// so no heap / malloc is ever involved — safe for embedded targets.
	// See docs/claude_const_array_plan.md for the full feature plan.
	//
	// Português: Declara uma COLEÇÃO CONSTANTE de tamanho fixo (literal,
	// ex: []int{1, 2, 3}). Type carrega o tipo do ELEMENTO (nunca "[]T");
	// Args carregam os literais já formatados (comprimento = len(Args)).
	// O backend C emite também o símbolo de comprimento `<dest>_len`, que
	// sobrevive ao decaimento do array para ponteiro na chamada de função.
	// Literal de tempo de compilação: sem malloc, seguro para embarcados.
	// OpDataBlob declares one maker-data byte array (the Data · File /
	// Data · Text devices): Dest is the node id (symbol + `_len`
	// companion, slice `#` protocol compatible), Meta carries the
	// payload ("base64"), the logical length ("lenNoNul" — a Text NUL is
	// appended but never counted), the origin ("kind": file|text) and
	// the human name ("sourceName"). The C backend renders it at FILE
	// scope with IOTM_ASSET_ATTR (flash on AVR).
	// Português: Declara um array de bytes de dados do maker. Dest é o
	// id do nó (símbolo + companion `_len`, compatível com o protocolo
	// `#` do slice); Meta carrega o payload em base64, o tamanho lógico
	// (NUL de Text anexado mas nunca contado), a origem e o nome humano.
	// O backend C o renderiza em ESCOPO DE ARQUIVO com IOTM_ASSET_ATTR.
	OpDataBlob Op = "DATA_BLOB"

	// DataBlobMaxBytes is the flash-asset ceiling for one Data · File /
	// Data · Text device: 2 MB of payload (before the optional NUL). Chosen
	// for the small targets these arrays exist for — beyond it, embedding
	// stops being a kindness. Enforced at emit time (KindAssetTooLarge).
	// Português: Teto de 2 MB por asset de flash; acima disso, embutir
	// deixa de ser gentileza. Imposto no emit.
	DataBlobMaxBytes = 2 << 20

	OpConstArray Op = "CONST_ARRAY" // CONST_ARRAY %dest elemType v1 v2 v3 …

	// OpIndex reads ONE element from a constant collection by integer index,
	// with a safe, bounds-checked result. It is the StatementIndex{Int,Float,
	// String} device: two inputs (the array and the index) and two outputs — the
	// value at that position and an OPTIONAL bool telling whether the index was
	// in range.
	//
	//	IN  Args[0] = %array   a collection register (e.g. from OpConstArray)
	//	    Args[1] = %index    an int register or literal
	//	OUT Dest             = %value, the element — or the type's ZERO value when
	//	                       the index is out of range, so there is no panic in
	//	                       Go and no undefined read in C
	//	    Meta["okDest"]   = %ok, a bool register — or ABSENT when the ok output
	//	                       is not wired, in which case the backends inline the
	//	                       bounds check and emit no dead ok variable
	//
	// A NEGATIVE index counts as out of range. The bounds check reuses the
	// collection's length that OpConstArray already provides: len(arr) in Go, the
	// `<arr>_len` companion symbol in C99. The element is only read inside the
	// passing branch, so the raw subscript never runs out of bounds.
	//
	// Português: Lê UM elemento de uma coleção constante por índice inteiro, com
	// resultado seguro e checado. É o device StatementIndex{Int,Float,String}:
	// duas entradas (array e índice) e duas saídas — o valor na posição e um bool
	// OPCIONAL indicando se o índice estava no range. Índice NEGATIVO é fora do
	// range. Fora do range devolve o ZERO do tipo (sem panic no Go, sem leitura
	// indefinida no C). A checagem reusa o comprimento que o OpConstArray já dá:
	// len(arr) no Go, o companheiro `<arr>_len` no C99. O elemento só é lido
	// dentro do ramo que passou, então o subscrito nunca acessa fora do range.
	OpIndex Op = "INDEX" // INDEX %value type %array %index   (Meta["okDest"] optional)

	// Arithmetic
	OpAdd Op = "ADD" // ADD %dest type %a %b
	OpSub Op = "SUB" // SUB %dest type %a %b
	OpMul Op = "MUL" // MUL %dest type %a %b
	OpDiv Op = "DIV" // DIV %dest type %a %b

	// OpConvert emits a type conversion from one concrete numeric type
	// to another. The source operand is the single argument; the
	// target type lives in the Type field; the destination register is
	// what downstream instructions reference in place of the original
	// operand.
	//
	// Used by the IR emitter to bridge type mismatches detected at
	// arithmetic and comparison sites. Backends translate it literally:
	//
	//	Go:     <dest> := <targetType>(<source>)
	//	C:      <targetType> <dest> = (<targetType>)<source>;
	//	Python: <dest> = <targetType>(<source>)  # int(), float(), etc.
	//
	// Português: Converte um operando de um tipo numérico concreto
	// para outro. Inserido pelo emitter para casar tipos antes de
	// operações aritméticas ou de comparação. Backends traduzem
	// literalmente pra forma idiomática da linguagem alvo.
	OpConvert Op = "CONVERT" // CONVERT %dest targetType %source

	// Comparison
	OpCmpEQ Op = "CMP_EQ" // CMP_EQ %dest bool %a %b
	OpCmpNE Op = "CMP_NE" // CMP_NE %dest bool %a %b
	OpCmpLT Op = "CMP_LT" // CMP_LT %dest bool %a %b
	OpCmpGT Op = "CMP_GT" // CMP_GT %dest bool %a %b
	OpCmpLE Op = "CMP_LE" // CMP_LE %dest bool %a %b
	OpCmpGE Op = "CMP_GE" // CMP_GE %dest bool %a %b

	// Control flow
	OpLoopBegin Op = "LOOP_BEGIN" // LOOP_BEGIN %loopId
	OpBreakIf   Op = "BREAK_IF"   // BREAK_IF %condition
	OpLoopEnd   Op = "LOOP_END"   // LOOP_END %loopId

	// OpSleep emits a time.Sleep(duration) call at the end of a loop iteration.
	// Used by StatementLoopDuration — an infinite loop with a timed cadence.
	// The single argument is a register holding a time.Duration (int64 nanos).
	//
	// Português: Emite uma chamada time.Sleep(duration) no final de uma iteração
	// de loop. Usado por StatementLoopDuration — loop infinito com cadência temporal.
	OpSleep Op = "SLEEP" // SLEEP %source

	// Conditional branching (if/else).
	// StatementIfElse emits these to wrap two groups of nodes.
	// The backend uses the presence/absence of instructions between markers
	// to determine the output form: if, if-negated, or if-else.
	//
	// Português: Ramificação condicional (if/else).
	// StatementIfElse emite estas instruções para envolver dois grupos de nós.
	OpIfBegin Op = "IF_BEGIN" // IF_BEGIN %condition
	OpIfElse  Op = "IF_ELSE"  // IF_ELSE (separator between true and false branches)
	OpIfEnd   Op = "IF_END"   // IF_END

	// Switch/case opcodes — emitted for a StatementCase with a non-boolean
	// selector. A boolean StatementCase lowers to the if/else opcodes above
	// instead (handled by the builder + emitIfElse), so no switch is emitted
	// for the two-way boolean form.
	//
	// Português: Opcodes de switch/case — emitidos para StatementCase com
	// selector não-booleano. O StatementCase booleano vira if/else.
	OpSwitchBegin  Op = "SWITCH_BEGIN"  // SWITCH_BEGIN %selector
	OpCaseLabel    Op = "CASE_LABEL"    // CASE_LABEL  Args: [value1, value2, ...]
	OpDefaultLabel Op = "DEFAULT_LABEL" // DEFAULT_LABEL
	OpSwitchEnd    Op = "SWITCH_END"    // SWITCH_END

	// Conditional-chain opcodes — the if/else-if lowering used for a
	// StatementCase whose int selector has at least one range or comparison
	// case (between/gt/lt/gte/lte). A switch `case` only accepts discrete
	// constants in both Go and C, so any non-discrete case forces the whole
	// Case onto this chain; a Case with only is/isAnyOf cases keeps the
	// SWITCH_* ops above. Each COND_LABEL carries the resolved selector as
	// Args[0] and the match operands as Args[1:], with Meta["matchKind"]
	// telling the backend how to assemble the boolean expression (see
	// BuildCaseCondition). The backend renders the chain as a flat
	// if / } else if / } else / } sequence.
	//
	// Português: Opcodes da cadeia condicional — o lowering if/else-if usado
	// quando o selector int de um StatementCase tem ao menos um case de range
	// ou comparação. Um `case` de switch só aceita constantes discretas em Go
	// e C, então qualquer case não-discreto joga o Case inteiro para esta
	// cadeia. Cada COND_LABEL carrega o selector resolvido em Args[0] e os
	// operandos em Args[1:], com Meta["matchKind"] indicando como montar a
	// expressão booleana.
	OpCondBegin   Op = "COND_BEGIN"   // COND_BEGIN — opens an if/else-if chain
	OpCondLabel   Op = "COND_LABEL"   // COND_LABEL  Args: [%selector, op1, ...]  Meta: matchKind
	OpCondDefault Op = "COND_DEFAULT" // COND_DEFAULT — the trailing `else`
	OpCondEnd     Op = "COND_END"     // COND_END — closes the chain

	// Output
	OpOutput Op = "OUTPUT" // OUTPUT %source "channelName"
	// OpReturn — extended by Fatia C (2026-07-19): Args may carry 1..n
	// resolved values (the function's RIGHT-side tunnels in stage-Y
	// order) with Meta["names"]/Meta["types"] mirroring the signature.
	// Go renders `return a, b`; C99 assigns each out-param and returns.
	// The legacy single-source form stays valid. Português: Estendido
	// pela Fatia C — Args levam 1..n valores (túneis da direita em
	// ordem de Y), Meta espelha a assinatura; a forma legada de fonte
	// única segue válida.
	OpReturn Op = "RETURN" // RETURN %a [%b ...]

	// OpPrint emits a debug print of ONE value to standard output — the
	// StatementPrint{Int,Float,String,Bool,Byte,ByteArray} sink devices.
	// Args[0] is the source register; Type carries the value type so the
	// backend picks the right format verb; Meta carries the maker's choices:
	//
	//	Meta["prefix"] — free text printed before the value (may be empty)
	//	Meta["format"] — per-type variant:
	//	    int    → "decimal" | "hex"
	//	    float  → "float"   | "trunc"     (trunc = integer part, truncated,
	//	                                      never rounded — int64(v) in Go,
	//	                                      (long)v in C)
	//	    bool   → "truefalse" | "onezero"
	//	    byte   → "hex" | "decimal"
	//	    []byte → "hex" | "text"          (hex = space-separated pairs;
	//	                                      text = the bytes as UTF-8)
	//	    string → ""                       (no variant)
	//
	// Unlike OpOutput — an IDE display channel — OpPrint ALWAYS lands in the
	// generated program: fmt.Printf in Go, printf in C99 (which flips the
	// C emitter's usesStdio so <stdio.h> ships).
	//
	// Português: Emite um print de depuração de UM valor no stdout — os
	// devices sink StatementPrint{...}. Args[0] é o registrador fonte; Type
	// leva o tipo do valor para o backend escolher o verbo; Meta leva as
	// escolhas do maker (prefix = texto livre antes do valor; format = a
	// variante por tipo, tabela acima — trunc é parte inteira TRUNCADA,
	// nunca arredondada). Diferente do OpOutput (canal de display da IDE),
	// o OpPrint SEMPRE vira código: fmt.Printf no Go, printf no C99.
	OpPrint Op = "PRINT" // PRINT %source   Type: value type   Meta: prefix, format

	// Black-box devices
	//
	// BB_DECL declares a struct variable for a black-box instance.
	// BB_PROP sets a property field before Init is called.
	// BB_INIT calls the Init() method with wired inputs, captures outputs.
	// BB_RUN calls the Run() method with wired inputs, captures outputs.
	//
	// Português:
	// BB_DECL declara uma variável struct para uma instância black-box.
	// BB_PROP define um campo de propriedade antes de Init ser chamado.
	// BB_INIT chama o método Init() com entradas conectadas, captura saídas.
	// BB_RUN chama o método Run() com entradas conectadas, captura saídas.
	OpBBDecl   Op = "BB_DECL"   // BB_DECL %instanceId  Meta: struct=StructName
	OpBBProp   Op = "BB_PROP"   // BB_PROP %instanceId  Args: [fieldName, value]  Meta: goType=type
	OpBBInit   Op = "BB_INIT"   // BB_INIT %instanceId  Args: [%input1, %input2, ...]
	OpBBMethod Op = "BB_METHOD" // BB_METHOD %instanceId  Args: [%input1, %input2, ...]  Meta: method=MethodName

	// OpBBCall is a C99 standalone function-device call — a free function,
	// not a method on a struct instance. The scene type is "BlackBox<fn>:"
	// with an empty struct part. There is no instance variable (no BB_DECL),
	// no receiver, and no Init/Run pairing; the handle a C99 function returns
	// flows on an ordinary wire, so its composite output registers use the
	// node's own ID (%nodeId:portName). The ANSI C backend translates this
	// into `ret = fn(args)`; the Go backend ignores it (Go scenes never
	// produce function-devices). See docs/CODEGEN_C99_STAGE.md §4.
	OpBBCall Op = "BB_CALL" // BB_CALL %nodeId  Args: [%input1, ...]  Meta: fn=FunctionName
)

// Instruction is a single IR instruction.
// VariableDecl is a user-declared project variable injected into codegen by
// the server (from the project_variables table, keyed by project_id). Name is
// the identifier used both as the source-level name and the IR register; Type
// is the abstract type ("int", "float", "string"). v1 keeps every variable
// global — declared once at the top of the program, zero-initialised.
//
// Português: Variável de projeto declarada pelo usuário, injetada no codegen
// pelo servidor (da tabela project_variables, por project_id). Name é o
// identificador (nome no código e registrador no IR); Type é o tipo abstrato
// ("int", "float", "string"). v1 mantém toda variável global — declarada uma
// vez no topo do programa, zero-init.
type VariableDecl struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Instruction struct {
	Op   Op                // opcode
	Dest string            // destination register (%id), empty for control flow
	Type string            // data type (int, float, bool, string)
	Args []string          // operands: registers (%id) or literal values
	Meta map[string]string // extra metadata (label, channel name, struct name, etc.)
}

// String returns the text representation of an instruction.
func (i Instruction) String() string {
	var sb strings.Builder
	sb.WriteString(string(i.Op))
	if i.Dest != "" {
		sb.WriteString(" %")
		sb.WriteString(i.Dest)
	}
	if i.Type != "" {
		sb.WriteString(" ")
		sb.WriteString(i.Type)
	}
	for _, arg := range i.Args {
		sb.WriteString(" ")
		sb.WriteString(arg)
	}
	// Show Meta for black-box instructions
	if len(i.Meta) > 0 {
		parts := make([]string, 0, len(i.Meta))
		for k, v := range i.Meta {
			parts = append(parts, k+"="+v)
		}
		sb.WriteString(" {")
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("}")
	}
	return sb.String()
}

// Program is the complete IR output — a list of instructions plus metadata.
type Program struct {
	Instructions []Instruction
	Warnings     []string // non-fatal warnings (e.g. "unused output")

	// BlackBoxDefs holds all black-box definitions referenced by BB_ instructions.
	// Key is the struct name (e.g. "APDS9960").
	// The Go backend uses these to emit struct definitions, imports, and method code.
	//
	// Português: Contém todas as definições de black-box referenciadas por
	// instruções BB_. A chave é o nome do struct. O backend Go usa para emitir
	// definições de struct, imports e código de métodos.
	BlackBoxDefs map[string]*blackbox.BlackBoxDef

	// StringBufferSize is the size, in bytes, of the stack buffer the C backend
	// gives each string concatenation (see ansic.emitStringConcat). It is set
	// per generation from the selected target's RAM budget (target.Target). Zero
	// means "unset" — the C backend falls back to its own conservative default —
	// so the field is safe to leave blank in tests and on the Go path (which
	// ignores it entirely, concatenating strings natively).
	//
	// Português: Tamanho (bytes) do buffer de stack que o backend C dá a cada
	// concatenação de string (ver ansic.emitStringConcat). Definido por geração
	// a partir do orçamento de RAM do target escolhido. Zero significa "não
	// definido" — o backend C cai no seu próprio default conservador — então é
	// seguro deixar em branco em testes e no caminho Go (que o ignora).
	StringBufferSize int
}

// String returns the full IR as text (for debug or API response).
func (p *Program) String() string {
	var sb strings.Builder
	indent := 0
	for _, inst := range p.Instructions {
		if inst.Op == OpLoopEnd || inst.Op == OpIfEnd || inst.Op == OpIfElse || inst.Op == OpSwitchEnd || inst.Op == OpCondEnd {
			indent--
		}
		for j := 0; j < indent; j++ {
			sb.WriteString("  ")
		}
		sb.WriteString(inst.String())
		sb.WriteString("\n")
		if inst.Op == OpLoopBegin || inst.Op == OpIfBegin || inst.Op == OpIfElse || inst.Op == OpSwitchBegin || inst.Op == OpCondBegin {
			indent++
		}
	}
	return sb.String()
}

// Append adds an instruction to the program.
func (p *Program) Append(inst Instruction) {
	p.Instructions = append(p.Instructions, inst)
}

// Warn adds a warning message.
func (p *Program) Warn(format string, args ...interface{}) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}
