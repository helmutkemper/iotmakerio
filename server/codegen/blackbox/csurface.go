// server/codegen/blackbox/csurface.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"sort"
	"strconv"
	"strings"
)

// Multi-file C output: the PUBLIC SURFACE of a black-box.
//
// English:
//
//	When a C project is generated as multiple files, each black-box source is
//	shipped VERBATIM in its own folder (e.g. iotm_47/iotm_47.c) and every
//	externally-visible name it declares must come out prefixed with the
//	box's symbol prefix (Naming.SymbolPrefix, e.g. "iotm_47_") — functions, wire-type struct tags and typedef aliases,
//	enum tags and ENUM CONSTANTS (they live in C's ordinary identifier
//	namespace, so two black-boxes both declaring `OK` would collide in
//	main.c), and callback typedef names. That set of names is the black-box's
//	"public surface", and this file owns everything derived from it.
//
//	HOW THE RENAME WORKS — the preprocessor, on purpose. We do not rewrite
//	the specialist's source text. A textual renamer that is right 99% of the
//	time is WORSE than none: the 1% it misses (one branch of an #ifdef, a
//	token built by ##-pasting) still compiles and fails silently in the
//	field, corrupting code the specialist tested. Instead the generated
//	generated source places one object-like macro per surface name ABOVE
//	the verbatim source:
//
//	    #define print_int iotm_47_print_int
//
//	The preprocessor then renames every occurrence of the identifier in the
//	WHOLE translation unit — the definition, the black-box's own internal
//	cross-calls, forward declarations — because it sees exactly the tokens
//	the compiler sees. Strings and comments are untouched (the preprocessor
//	never expands inside them), macros compose instead of desynchronising,
//	and a name the macro cannot reach fails LOUDLY at link time (undefined
//	reference) instead of silently. This is the industry-standard technique
//	for exactly this problem: zlib's Z_PREFIX and SQLite's amalgamation API
//	rename work the same way.
//
//	SECURITY — the unconditional-prefix property survives the mechanism. The
//	defines are generated from the PARSED surface, one per public name, with
//	the prefix applied unconditionally (see naming.go). A malicious source
//	declaring a function literally named `iotm_47_steal` simply produces
//	`#define iotm_47_steal iotm_103_iotm_47_steal` — its own code stamped on
//	top, linking to nothing of the victim's. An `#undef` in the malicious
//	source can strip its OWN rename, but then its definition keeps the bare
//	name while main.c calls the prefixed one — an undefined reference at
//	link, loud, never a hijack.
//
//	KNOWN LIMIT, accepted: the defines are active while the specialist's own
//	`#include <...>` lines are preprocessed (they sit inside the verbatim
//	source, below our defines). A black-box that names a public function
//	after a libc symbol (`printf`) will therefore rename the system header's
//	declaration too and fail to compile — noisily, inside the bb unit only.
//	That source was already broken under the single-file model (its
//	definition collided with libc at link), so this is not a regression; the
//	submission-time lint is the planned place to flag it early.
//
// Português:
//
//	A "superfície pública" é o conjunto de nomes externamente visíveis de uma
//	black-box: funções, tags e aliases de wire-types, tags e CONSTANTES de
//	enum (constantes vivem no namespace ordinário do C — dois `OK` colidem no
//	main.c) e typedefs de callback. A renomeação é feita pelo PREPROCESSADOR
//	(um #define por nome acima do fonte verbatim — ex.: `#define print_int
//	iotm_47_print_int`), nunca por reescrita de
//	texto: um renomeador 99% correto é pior que nenhum, porque o 1% que erra
//	compila e falha em silêncio; o preprocessador vê exatamente os tokens que
//	o compilador vê, renomeia a unidade INTEIRA (definição + chamadas
//	internas), não toca strings/comentários e falha alto (link) quando não
//	alcança. É a técnica padrão da indústria (Z_PREFIX do zlib, amálgama do
//	SQLite). A propriedade de segurança do prefixo incondicional sobrevive ao
//	mecanismo: fonte malicioso com `iotm_47_roubar` só ganha o próprio código
//	por cima (`iotm_103_iotm_47_roubar`). Limite aceito: renomear símbolo de
//	libc quebra a unidade da
//	própria black-box, com barulho — já era quebrado no modelo inline.

// CSurface is the computed public surface of ONE black-box def, ready to
// derive the rename defines, the generated header, and identifier prefixing
// for main.c-side text (casts, declared types, enum-constant defaults).
// Construct with NewCSurface; a nil *CSurface means "this def has no isolated
// identity" (no database id) and the caller must fall back to the single-file
// inline path.
//
// Português: Superfície pública computada de UM def, pronta para derivar os
// #define de renomeação, o header gerado e o prefixamento de identificadores
// no lado do main.c. Nil = def sem identidade (sem id) → caminho inline.
type CSurface struct {
	def *BlackBoxDef

	// naming is the family (radical) every derived name uses — threaded in
	// from the export so a maker-configured radical moves the WHOLE family:
	// defines, header, guard, and the main.c-side prefixes alike.
	naming Naming

	// code is def.CodeIdent() computed once: the short code number when the
	// loader stitched one, else the full database id (long-but-correct
	// fallback). Every name below derives from this single token.
	code string

	// names is the full renameable set (functions ∪ type names ∪ enum
	// constants ∪ callback typedefs), used by PrefixIdentifiers to decide
	// whether an identifier token belongs to this black-box.
	names map[string]bool
}

// NewCSurface computes the public surface of def under the given naming
// family. Returns nil when def is nil or has no database ID — per the
// BlackBoxDef.ID contract the emitter must NEVER invent an identity, so "no
// id" routes to the inline fallback. Note the two-layer contract: def.ID is
// WHETHER the box has an identity (this gate, and the emitter's unit/dedupe
// gates — they must always agree); def.CodeIdent() is merely how that
// identity is SPELLED in names (short code number, or the full id fallback).
//
// Português: Computa a superfície pública sob a família de nomes dada. Nil
// quando o def não tem ID — o emitter nunca inventa identidade; sem id,
// caminho inline. Contrato em duas camadas: def.ID diz SE a caixa tem
// identidade (este gate e os do emitter concordam sempre); CodeIdent diz só
// como ela se ESCREVE nos nomes.
func NewCSurface(def *BlackBoxDef, naming Naming) *CSurface {
	if def == nil || def.ID == "" {
		return nil
	}
	s := &CSurface{
		def:    def,
		naming: naming,
		code:   def.CodeIdent(),
		names:  make(map[string]bool),
	}
	for i := range def.Functions {
		s.add(def.Functions[i].Name)
	}
	for i := range def.WireTypes {
		// Name is the struct tag (or the alias when the struct is anonymous —
		// see StructDef.Name); Alias is the typedef the specialist writes in
		// public signatures. Both are visible names, both are renamed.
		s.add(def.WireTypes[i].Name)
		s.add(def.WireTypes[i].Alias)
	}
	for i := range def.Enums {
		s.add(def.Enums[i].Name)
		for _, v := range def.Enums[i].Values {
			s.add(v.Name)
		}
	}
	for i := range def.CallbackTypes {
		s.add(def.CallbackTypes[i].Name)
	}
	// External state variables join the RENAME set but never the header
	// surface: rename-all, expose-some. Internal linkage across the
	// specialist's files requires non-static symbols; renaming them keeps
	// two boxes' internals from colliding in the maker's link, while the
	// header stays a clean IDS contract. The Header() renderer reads the
	// def's typed fields directly, so nothing here leaks into it.
	//
	// Português: Variáveis externas entram no conjunto de RENOMEAÇÃO, nunca
	// na superfície do header (o Header lê os campos tipados do def):
	// renomeia-se tudo que é externo, expõe-se só o contrato IDS.
	for _, name := range def.ExternalNames {
		s.add(name)
	}
	return s
}

func (s *CSurface) add(name string) {
	if name != "" {
		s.names[name] = true
	}
}

// ID returns the black-box database id this surface was built for.
func (s *CSurface) ID() string { return s.def.ID }

// Code returns the identity token names are composed from (short code number
// or the full-id fallback — see BlackBoxDef.CodeIdent). Distinct from ID():
// ID is WHO the box is (dedupe, provenance); Code is how its names are
// SPELLED in this export.
//
// Português: Token de identidade dos nomes (número curto ou fallback do id
// completo). ID() é QUEM a caixa é; Code() é como os nomes se ESCREVEM.
func (s *CSurface) Code() string { return s.code }

// Files returns the authored snapshot this surface was parsed from, in tab
// order — the emitter ships each entry into the box's folder (.c wrapped by
// Preamble/Postamble, .h verbatim). A method, so the emitter never reaches
// into the def directly.
//
// Português: O snapshot autoral de origem, na ordem das abas — o emitter
// embarca cada entrada na pasta da caixa. Método para o emitter não tocar o
// def direto.
func (s *CSurface) Files() []FileEntry { return s.def.Files }

// Assets exposes the def's cargo lane — see BlackBoxDef.Assets.
func (s *CSurface) Assets() []AssetEntry { return s.def.Assets }

// sortedNames returns the surface names in deterministic (sorted) order, so
// the generated defines block is byte-stable across builds — same discipline
// as deviceSources()' sort in the C backend.
func (s *CSurface) sortedNames() []string {
	out := make([]string, 0, len(s.names))
	for n := range s.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// PrefixIdentifiers rewrites text so that every identifier token belonging to
// this surface carries the black-box's symbol prefix. It is a lexical scan
// over IDENTIFIER TOKENS — never a substring replace — and it skips string
// and character literals, so a `default:"MODE_FAST"` string default is left
// alone while a bare `MODE_FAST` enum-constant default is renamed. Used for:
//
//   - the generated header (parameter lists and return types name wire types,
//     enums and callback typedefs — those must appear prefixed to main.c);
//   - main.c-side text the C backend emits from authored type strings (cast
//     prefixes, return/out-param declaration types);
//   - "=<literal>" default arguments (an enumerator default must follow its
//     renamed enum).
//
// Identifiers NOT in the surface (profile types, libc names, the maker's own
// registers) pass through untouched.
//
// Português: Reescreve text prefixando apenas TOKENS de identificador que
// pertencem à superfície, pulando literais de string/char. Serve para o
// header gerado, para os textos de tipo emitidos no main.c (casts,
// declarações) e para defaults "=enumerador".
func (s *CSurface) PrefixIdentifiers(text string) string {
	if s == nil || text == "" {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); {
		c := text[i]

		// Skip string/char literals verbatim, honouring backslash escapes.
		if c == '"' || c == '\'' {
			quote := c
			b.WriteByte(c)
			i++
			for i < len(text) {
				b.WriteByte(text[i])
				if text[i] == '\\' && i+1 < len(text) {
					i++
					b.WriteByte(text[i])
					i++
					continue
				}
				if text[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}

		// Identifier token: [A-Za-z_][A-Za-z0-9_]*
		if isCIdentStart(c) {
			j := i + 1
			for j < len(text) && isCIdentPart(text[j]) {
				j++
			}
			ident := text[i:j]
			if s.names[ident] {
				b.WriteString(s.naming.PrefixSymbol(s.code, ident))
			} else {
				b.WriteString(ident)
			}
			i = j
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

func isCIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isCIdentPart(c byte) bool {
	return isCIdentStart(c) || (c >= '0' && c <= '9')
}

// RenameDefines returns the whole-unit rename block placed above the verbatim
// source in the unit: one `#define <name> <radical><code>_<name>` per name,
// sorted. See the file header for why the preprocessor (and not a source
// rewriter) is the rename mechanism, and why the unconditional prefix is the
// security property.
//
// Português: Bloco de #define (um por nome da superfície, ordenado) que
// renomeia a unidade inteira. Ver header do arquivo para o porquê.
func (s *CSurface) RenameDefines() string {
	var b strings.Builder
	b.WriteString("/* ── whole-unit rename (generated) ─────────────────────────────────\n")
	b.WriteString(" * One #define per public name: the preprocessor renames every\n")
	b.WriteString(" * occurrence in this translation unit — definitions and internal\n")
	b.WriteString(" * cross-calls alike — so the symbols this unit exports are exactly\n")
	b.WriteString(" * the ones declared in the generated header. Applied\n")
	b.WriteString(" * unconditionally: a name that already looks prefixed is stamped\n")
	b.WriteString(" * again on purpose (anti-hijack; see IoTMaker docs).\n")
	b.WriteString(" */\n")
	for _, name := range s.sortedNames() {
		b.WriteString("#define ")
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(s.naming.PrefixSymbol(s.code, name))
		b.WriteByte('\n')
	}
	return b.String()
}

// Header returns the generated header (e.g. iotm_47.h): include guard, foundation includes,
// and the black-box's public surface declared under its FINAL (prefixed)
// names — opaque forward typedefs for wire types, full enums (constants and
// explicit values preserved), callback typedefs, and one prototype per public
// function composed from the authored verbatim signature (CReturnType /
// CParams). This header is main.c's ONLY view of the black-box; the bb unit
// itself never includes it (its renamed source re-defines the surface types,
// and typedef redefinition is a C99 error — see Preamble), so the
// definition↔signature cross-check happens inside the unit via Postamble.
//
// Design notes, in declaration order:
//
//   - Wire types are declared as OPAQUE forward typedefs
//     (`typedef struct iotm_47_tag iotm_47_alias;`) — never full definitions.
//     Handles flow on wires as pointers (the LabVIEW resource-chain idiom),
//     and a pointer only needs an incomplete type; shipping the members would
//     both invite main.c to poke inside the specialist's state and collide
//     with the full definition inside the bb unit.
//   - Enums must be COMPLETE here (main.c materialises constants), and every
//     enumerator gets an explicit value: parser-computed values are emitted
//     as `= N` so the header's constants can never drift from the unit's own
//     enum; authored raw expressions are kept verbatim (prefixed), preserving
//     intent like `= 1 << 2`.
//   - The enum's single prefixed name is declared as BOTH tag and typedef
//     (`typedef enum P_x { … } P_x;` — tag and typedef live in different C
//     namespaces), so authored signatures work whether they said `enum x` or
//     the bare typedef.
//   - Prototypes reuse the authored signature verbatim, with surface
//     identifiers prefixed, so the header matches the (renamed) definition
//     exactly — including parameter names, which document the call.
//
// Português: Gera o header (ex.: iotm_47.h) — guard, includes de base e a superfície
// pública sob os nomes FINAIS: wire types como typedefs opacos (handle é
// ponteiro; membro não viaja), enums completos com valores explícitos
// (constantes não podem derivar entre unidades), typedefs de callback e um
// protótipo por função composto da assinatura autoral verbatim. Este header
// é a ÚNICA visão que o main.c tem da black-box; a própria unidade bb nunca
// o inclui (o fonte renomeado redefine os tipos — redefinição de typedef é
// erro em C99; ver Preamble) — o cross-check acontece via Postamble.
func (s *CSurface) Header() string {
	guard := s.naming.Guard(s.code)

	var b strings.Builder
	b.WriteString("#ifndef " + guard + "\n")
	b.WriteString("#define " + guard + "\n\n")

	// Foundation includes: authored signatures may use fixed-width ints,
	// bool, or size_t (the `slice:` directive's length companion). All three
	// headers are header-guarded and tiny — including them unconditionally
	// keeps the generated header self-sufficient no matter which subset the
	// signatures use.
	b.WriteString("#include <stdint.h>\n")
	b.WriteString("#include <stdbool.h>\n")
	b.WriteString("#include <stddef.h>\n")

	// Slice 0 of the embedded ladder (2026-07-16): the Arduino sketch
	// wrapper is C++ — without extern "C" the C symbols this header
	// declares would be name-mangled at the include site and never
	// link. Inert for gcc/C consumers. Português: O invólucro .ino é
	// C++ — sem extern "C" os símbolos C não linkam; inócuo para gcc.
	b.WriteString("\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n")

	// Wire types — opaque forward typedefs under the final names.
	if len(s.def.WireTypes) > 0 {
		b.WriteString("\n/* wire types (opaque handles) */\n")
		for i := range s.def.WireTypes {
			w := &s.def.WireTypes[i]
			tag := w.Name
			alias := w.Alias
			switch {
			case tag != "" && alias != "" && tag != alias:
				b.WriteString("typedef struct " + s.naming.PrefixSymbol(s.code, tag) +
					" " + s.naming.PrefixSymbol(s.code, alias) + ";\n")
			case tag != "":
				// No distinct alias: declare the tag; authored signatures
				// spell it `struct <tag> *`, which PrefixIdentifiers turns
				// into `struct iotm_47_<tag> *` — an incomplete type, exactly
				// what a handle needs.
				b.WriteString("struct " + s.naming.PrefixSymbol(s.code, tag) + ";\n")
			}
		}
	}

	// Enums — complete, prefixed, values pinned.
	for i := range s.def.Enums {
		e := &s.def.Enums[i]
		if e.Name == "" {
			continue
		}
		name := s.naming.PrefixSymbol(s.code, e.Name)
		b.WriteString("\ntypedef enum " + name + " {\n")
		for vi := range e.Values {
			v := &e.Values[vi]
			b.WriteString("    " + s.naming.PrefixSymbol(s.code, v.Name) + " = ")
			if v.ValueIsRaw {
				b.WriteString(s.PrefixIdentifiers(v.RawValue))
			} else {
				b.WriteString(strconv.Itoa(v.Value))
			}
			b.WriteString(",\n")
		}
		b.WriteString("} " + name + ";\n")
	}

	// Callback typedefs — function-pointer types under the final names, with
	// parameter/return types (which may name wire types) prefixed too.
	if len(s.def.CallbackTypes) > 0 {
		b.WriteString("\n/* callback types */\n")
		for i := range s.def.CallbackTypes {
			c := &s.def.CallbackTypes[i]
			ret := strings.TrimSpace(c.ReturnType)
			if ret == "" {
				ret = "void"
			}
			b.WriteString("typedef " + s.PrefixIdentifiers(ret) +
				" (*" + s.naming.PrefixSymbol(s.code, c.Name) + ")(" +
				s.PrefixIdentifiers(c.Params) + ");\n")
		}
	}

	// Function prototypes — authored signature verbatim, surface names
	// prefixed. A def loaded from a stale parsed_json predating CReturnType
	// would compose a blank return type; "int" is the compilable degrade,
	// same stance as cOutputType's profile fallback in the C backend.
	b.WriteString("\n/* public functions */\n")
	for i := range s.def.Functions {
		fn := &s.def.Functions[i]
		ret := strings.TrimSpace(fn.CReturnType)
		if ret == "" {
			ret = "int"
		}
		b.WriteString(s.PrefixIdentifiers(ret) + " " +
			s.naming.PrefixSymbol(s.code, fn.Name) + "(" +
			s.PrefixIdentifiers(fn.CParams) + ");\n")
	}

	b.WriteString("\n#ifdef __cplusplus\n} /* extern \"C\" */\n#endif\n")
	b.WriteString("\n#endif /* " + guard + " */\n")
	return b.String()
}

// Preamble returns everything that sits ABOVE the verbatim authored source in
// the generated unit (e.g. iotm_47.c): the specialist's attribution, the licensing note (this file is
// the AUTHOR's code — it is deliberately NOT stamped with the Generated Code
// Exception the other emitted files carry), the whole-unit rename defines,
// and the marker line that separates generated preamble from authored source.
//
// The unit deliberately does NOT include its own generated header: the header
// re-DEFINES the surface types under their final names (enum with constants,
// typedef aliases) for main.c's benefit, and the renamed authored source
// defines those same types again — typedef redefinition and enumerator
// redeclaration, hard errors in C99. Functions tolerate declaration +
// definition; types do not. The definition↔parsed-signature cross-check the
// self-include would have provided lives in Postamble instead, inside the
// unit, where the types are the source's own.
//
// Português: Tudo que fica ACIMA do fonte autoral verbatim na unidade —
// atribuição do especialista, nota de licença (este arquivo é do AUTOR; de
// propósito NÃO recebe a exceção de código gerado), os #define de renomeação
// e o marcador. A unidade NÃO inclui o próprio header de propósito: o header
// redefine os tipos da superfície (enum com constantes, typedefs) para o
// main.c, e o fonte renomeado os define de novo — redefinição de typedef e
// redeclaração de enumerador são erros duros em C99. Função tolera
// declaração + definição; tipo não. O cross-check vive no Postamble.
func (s *CSurface) Preamble() string {
	var b strings.Builder
	if line := AuthorLine(s.def); line != "" {
		b.WriteString(line)
	}
	b.WriteString("// This file carries a black-box author's source VERBATIM below the marker.\n")
	b.WriteString("// The authored source remains under its author's own license and is NOT\n")
	b.WriteString("// covered by the IoTMaker Generated Code Exception. Only the preamble\n")
	b.WriteString("// above the marker is generated.\n\n")
	b.WriteString(s.RenameDefines())
	b.WriteString("\n/* ── authored source below (verbatim) ─────────────────────────────── */\n")
	return b.String()
}

// Postamble returns the generated block appended AFTER the verbatim authored
// source in the generated unit: one re-declaration per public function, written with
// the ORIGINAL names — the rename defines are still active at this point in
// the unit, so the preprocessor turns each line into the prefixed prototype,
// and the compiler checks it against the (equally renamed) definition above.
//
// This is the unit-local replacement for including the generated header: it
// verifies, at bb compile time, that the signature the parser extracted
// (CReturnType/CParams — the exact text the header hands to main.c) matches
// the authored definition. If the parser ever mis-parses a signature, the
// divergence surfaces HERE as a loud "conflicting types" error, instead of
// compiling on both sides, linking (C checks no signatures at link), and
// corrupting the call at runtime — the silent failure class this whole
// design exists to prevent. Redundant re-declarations of an identical
// signature are legal C, so a source that already forward-declares its
// functions is unaffected.
//
// A legacy def whose stored parse predates CReturnType is SKIPPED here: with
// no parsed signature there is nothing to check, and a guessed placeholder
// would fabricate a false conflicting-types error on a perfectly good
// function. (The header still degrades that case to "int" for main.c — best
// effort, same as the old inline model's zero checks.)
//
// Português: Bloco gerado APÓS o fonte verbatim — uma redeclaração por
// função pública, escrita com os nomes ORIGINAIS: os #define ainda ativos a
// renomeiam, e o compilador confere contra a definição (igualmente
// renomeada) acima. É o cross-check do header, só que dentro da unidade,
// onde os tipos são os do próprio fonte: divergência de parse vira erro
// "conflicting types" alto na compilação da bb, nunca corrupção silenciosa
// em runtime. Redeclarar assinatura idêntica é C legal. Def legado sem
// CReturnType é pulado — sem assinatura parseada não há o que conferir.
func (s *CSurface) Postamble() string {
	var lines []string
	for i := range s.def.Functions {
		fn := &s.def.Functions[i]
		if strings.TrimSpace(fn.CReturnType) == "" {
			continue // legacy parse: nothing reliable to check against
		}
		lines = append(lines,
			strings.TrimSpace(fn.CReturnType)+" "+fn.Name+"("+fn.CParams+");")
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n/* ── generated declaration check ───────────────────────────────────\n")
	b.WriteString(" * Re-declares each public function with its parsed signature (the\n")
	b.WriteString(" * rename defines above turn these into the prefixed prototypes the\n")
	b.WriteString(" * generated header hands to main.c). If a definition and its parsed\n")
	b.WriteString(" * signature ever diverge, the compiler stops HERE — loudly — instead\n")
	b.WriteString(" * of letting main.c call through a wrong prototype at runtime.\n")
	b.WriteString(" */\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}
