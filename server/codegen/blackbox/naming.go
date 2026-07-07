// server/codegen/blackbox/naming.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "strings"

// Multi-file C output: the iotm_<n> naming family.
//
// English:
//
//	When a project is generated as multiple C files, each black-box SOURCE
//	gets its own folder, its own file pair and its own symbol prefix — one
//	FAMILY, one identity readable everywhere:
//
//	    folder:  iotm_47/
//	    files:   iotm_47.c, iotm_47.h
//	    symbol:  iotm_47_print_float
//	    guard:   IOTM_47_H
//
//	The "47" is the black-box's CODE NUMBER: a small sequential integer
//	allocated centrally at creation time (see the store's
//	CodeNumberAllocator contract). Sequential-with-a-central-counter is the
//	strongest form of the project's "the database is the single source of
//	uniqueness" principle: collisions are impossible BY CONSTRUCTION, not
//	improbable. The long random primary key exists for a world without
//	coordination; we have coordination, so names get to be short. Numbers
//	are NEVER reused, even after their owner is deleted — a recycled number
//	would make old exported code and new code share an identity.
//
//	Why the "iotm_" radical (and not "P<n>_" or "bb_<n>_"): the prefix must
//	not collide with the HUMAN C namespace either. "P" + small digits is
//	dense territory in embedded headers — on Keil 8051 targets, P1_0, P2_3
//	are literally the hardware port bits — and generic radicals invite
//	accidents. "iotm_" is project-branded and unclaimed. The radical is a
//	PARAMETER of this family (Naming), defaulting to DefaultRadical: a maker
//	whose larger codebase already uses the radical (for example, two
//	IoTMaker exports linked into one firmware, sharing a box) will be able
//	to change it per scene. The scene metadata field (ExportPrefix) exists;
//	the UI to set it is a documented future pendency — see
//	docs/C99_EXPORT_NAMING.md.
//
//	Why prefix UNCONDITIONALLY (see Naming.PrefixSymbol): the prefix is
//	applied to every exported symbol without checking whether it already
//	looks prefixed. That is the security property, not an oversight, and it
//	is independent of the radical VALUE: a malicious specialist who writes a
//	function literally named `iotm_47_steal`, hoping to hijack another
//	box's symbols, simply gets THEIR OWN code stamped on top —
//	`iotm_103_iotm_47_steal` — which links to nothing of the victim's. A
//	"smart" idempotent check (skip if already prefixed) would REOPEN that
//	hole, so we deliberately do not do it. The property survives ANY radical
//	a maker configures, because it never depended on the radical.
//
//	Fallback: a def whose CodeID was never stitched (legacy blob, test
//	fixture, registry miss) uses its full database id in the same family —
//	`iotm_3f9a…_print_float` — long but correct. The emitter NEVER invents
//	an identity (BlackBoxDef.ID contract).
//
// Português:
//
//	Cada FONTE de black-box ganha pasta, par de arquivos e prefixo de
//	símbolo — uma FAMÍLIA só, a identidade se lê igual em qualquer lugar
//	(aba, main.c, Makefile, erro de linker). O "47" é o NÚMERO DE CÓDIGO:
//	inteiro sequencial alocado centralmente na criação (contrato
//	CodeNumberAllocator no store). Sequencial com contador central é a forma
//	mais forte do princípio "o banco é a fonte única de unicidade": colisão
//	é impossível POR CONSTRUÇÃO. Número nunca é reusado, nem após deleção.
//
//	Por quê o radical "iotm_": o prefixo também não pode colidir com o
//	namespace C humano — "P" + dígito é território denso no embarcado (no
//	Keil 8051, P1_0 e P2_3 são os bits das portas de hardware). "iotm_" tem
//	marca do projeto e não é reivindicado por ninguém. O radical é PARÂMETRO
//	da família (Naming), com default DefaultRadical; a configuração por cena
//	(ExportPrefix) existe no metadata — a UI é pendência futura documentada.
//
//	Por quê prefixar SEMPRE: propriedade de segurança, independente do VALOR
//	do radical — `iotm_47_roubar` malicioso vira `iotm_103_iotm_47_roubar`.
//	Checagem "esperta" reabriria o buraco. Fallback: def sem CodeID usa o id
//	longo do banco na mesma família — longo, porém correto; o emitter nunca
//	inventa identidade.

// DefaultRadical is the naming family's default stem. Chosen for namespace
// hygiene against both the marketplace (the code number handles that) and the
// HUMAN C world: no vendor header, RTOS or popular library claims "iotm_".
//
// Português: Radical default da família. Escolhido por higiene de namespace
// também contra o mundo C humano: nenhum header de fabricante, RTOS ou
// biblioteca popular reivindica "iotm_".
const DefaultRadical = "iotm_"

// maxRadicalLen caps a configured radical so composed symbols stay friendly
// to C99's minimum guarantee of 31 significant characters in external
// identifiers (§5.2.4.1): radical + number + "_" + authored name must fit
// real toolchains, including conservative embedded ones. 16 leaves room for
// a 3-digit number and a ~10-char function name inside the guarantee.
//
// Português: Teto do radical configurado, protegendo o orçamento de 31
// caracteres significativos que o C99 garante em identificadores externos.
const maxRadicalLen = 16

// Naming composes every generated-code name of the multi-file C output from
// one radical: folder, file pair, symbol prefix and include guard. The zero
// value is valid and uses DefaultRadical — construct with NewNaming only to
// apply a maker-configured radical (scene Metadata.ExportPrefix).
//
// It is a value type on purpose: it carries one string and is threaded from
// the codegen entry point (codeGen.go) through the emitter into CSurface, so
// every name in one export derives from the SAME radical — one knob moves
// the whole family.
//
// Português: Compõe todos os nomes do código gerado a partir de um radical:
// pasta, arquivos, prefixo de símbolo e guard. O zero value é válido (usa
// DefaultRadical); NewNaming existe para aplicar o radical configurado pelo
// maker. Tipo-valor de propósito: um botão move a família inteira.
type Naming struct {
	radical string
}

// NewNaming returns a Naming for the given radical, falling back to
// DefaultRadical when the value is empty or invalid (see ValidRadical). The
// tolerant fallback mirrors the parser's stance: a bad configuration degrades
// to the safe default instead of failing the export — the export validator
// may warn about it separately.
//
// Português: Naming para o radical dado, caindo no DefaultRadical quando
// vazio ou inválido (postura tolerante: configuração ruim degrada para o
// default seguro em vez de quebrar o export).
func NewNaming(radical string) Naming {
	if !ValidRadical(radical) {
		return Naming{}
	}
	return Naming{radical: radical}
}

// ValidRadical reports whether s can prefix a C identifier: letter or
// underscore first, then letters, digits or underscores, non-empty, and
// within maxRadicalLen. The trailing "_" convention (as in "iotm_") is
// recommended for readability but not required — the radical is used
// verbatim.
//
// Português: Diz se s pode prefixar um identificador C (letra/underscore
// primeiro, depois [A-Za-z0-9_], não vazio, dentro do teto). O "_" final é
// convenção recomendada, não exigida — o radical é usado verbatim.
func ValidRadical(s string) bool {
	if s == "" || len(s) > maxRadicalLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_',
			c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Radical returns the family stem in effect (the configured one, or
// DefaultRadical for the zero value).
func (n Naming) Radical() string {
	if n.radical == "" {
		return DefaultRadical
	}
	return n.radical
}

// SymbolPrefix returns the prefix every exported C symbol of the black-box
// with this code carries: radical + code + "_".
// Example: code "47" → "iotm_47_".
//
// Português: Prefixo de todo símbolo C exportado da black-box com este
// código: radical + código + "_".
func (n Naming) SymbolPrefix(code string) string {
	return n.Radical() + code + "_"
}

// PrefixSymbol applies the black-box's symbol prefix to one exported symbol —
// UNCONDITIONALLY, by design (see the package doc's security note: stamping
// over an already-prefixed-looking name is the anti-hijack property).
// Example: code "47", symbol "print_float" → "iotm_47_print_float".
//
// Português: Aplica o prefixo a um símbolo exportado — SEMPRE, de propósito
// (carimbar por cima é a propriedade anti-sequestro).
func (n Naming) PrefixSymbol(code, symbol string) string {
	return n.SymbolPrefix(code) + symbol
}

// SourceDir returns the folder that holds this black-box's generated files:
// radical + code. Folder, files and symbols share the stem on purpose — a
// maker looking at an unzipped project, a tab label, or a linker error naming
// iotm_47/iotm_47.o reads the identity from any of them.
// Example: code "47" → "iotm_47".
//
// Português: Pasta dos arquivos gerados desta black-box: radical + código.
// Pasta, arquivos e símbolos compartilham a raiz de propósito — a identidade
// se lê em qualquer superfície.
func (n Naming) SourceDir(code string) string {
	return n.Radical() + code
}

// SourceName returns the C source filename inside SourceDir(code):
// radical + code + ".c". Example: code "47" → "iotm_47.c".
func (n Naming) SourceName(code string) string {
	return n.Radical() + code + ".c"
}

// HeaderName returns the generated header filename inside SourceDir(code):
// radical + code + ".h". main.c includes it (as "iotm_47/iotm_47.h") — it is
// main.c's ONLY view of the black-box. The bb unit itself never includes it:
// its renamed source re-defines the surface types, and typedef redefinition
// is a C99 error; the definition↔signature cross-check lives in the unit's
// generated postamble instead (see csurface.go Postamble).
//
// Português: Nome do header gerado: radical + código + ".h" — única visão
// que o main.c tem da caixa. A própria unidade nunca o inclui (redefinição
// de typedef é erro em C99); o cross-check vive no posâmbulo gerado.
func (n Naming) HeaderName(code string) string {
	return n.Radical() + code + ".h"
}

// Guard returns the include-guard macro of the generated header:
// upper(radical + code) + "_H" — conventional all-caps macro style.
// Uppercasing a valid radical/code keeps it a valid macro name, and no two
// codes differ only by case (codes are decimal digits; fallback ids are
// lowercase hex), so uniqueness is preserved.
// Example: code "47", radical "iotm_" → "IOTM_47_H".
//
// Português: Macro de guard do header gerado: radical + código em
// maiúsculas + "_H" (convenção de macro). Nenhum par de códigos difere só
// por caixa, então a unicidade se preserva.
func (n Naming) Guard(code string) string {
	return strings.ToUpper(n.Radical()+code) + "_H"
}
