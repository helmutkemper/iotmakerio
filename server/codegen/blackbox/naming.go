// server/codegen/blackbox/naming.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// Multi-file C output: source naming derived from the black-box id.
//
// English:
//
//	When a project is generated as multiple C files, each black-box SOURCE gets
//	its own folder and its own symbol prefix. Both are derived from the
//	black-box's database ID — a short, opaque token minted when the specialist
//	creates the black-box (unique per black-box, never reused once the black-box
//	is deleted). The id is the SINGLE source of uniqueness: one concept, two uses
//	— symbol prefixes and folder names.
//
//	Why an id and not the repository name: C has no namespaces, so two
//	specialists who both write `init()` — or both adopt the community habit of
//	`sensorlib_init()` — would collide at link time. And two different owners can
//	have repositories with the SAME name, so the repo name is not unique either.
//	The database id is unique per black-box by construction, so it fixes both the
//	symbol collision and the file collision at once.
//
//	Why prefix UNCONDITIONALLY (see PrefixSymbol): the prefix is applied to every
//	exported symbol without checking whether it already looks prefixed. That is
//	the security property, not an oversight. A malicious specialist who writes a
//	function literally named `P<someone-else-id>_steal`, hoping to hijack another
//	source's symbols, simply gets THEIR OWN id stamped on top —
//	`P<their-id>_P<victim-id>_steal` — which links to nothing of the victim's. A
//	"smart" idempotent check (skip if already prefixed) would REOPEN that hole,
//	so we deliberately do not do it.
//
//	Why the leading "P": a database id may begin with a digit (e.g. "3f9a2b1c"),
//	which is not a valid start for a C identifier. The constant "P" guarantees a
//	letter-first, valid identifier. The id is expected to be a bare
//	[A-Za-z0-9_] token (the minting side guarantees this); it is used verbatim,
//	so an id with other characters would produce invalid C — sanitise at the
//	mint, not here, to keep uniqueness intact.
//
// Português:
//
//	Quando um projeto é gerado como vários arquivos C, cada FONTE de black-box
//	ganha sua própria pasta e seu próprio prefixo de símbolo, ambos derivados do
//	ID da black-box no banco — um token curto e opaco criado quando o especialista
//	cria a black-box (único por black-box, nunca reusado após deleção). O id é a
//	ÚNICA fonte de unicidade: um conceito, dois usos — prefixos de símbolo e nomes
//	de pasta.
//
//	Por quê o id e não o nome do repo: C não tem namespaces, então dois
//	especialistas que escrevem `init()` — ou ambos adotam o hábito da comunidade
//	`sensorlib_init()` — colidiriam no link. E dois donos diferentes podem ter
//	repos com o MESMO nome, então o nome do repo também não é único. O id do banco
//	é único por construção, resolvendo a colisão de símbolo e de arquivo de uma vez.
//
//	Por quê prefixar SEMPRE (ver PrefixSymbol): o prefixo é aplicado a todo
//	símbolo exportado sem checar se já parece prefixado. Isso é a propriedade de
//	segurança. Um especialista malicioso que escreve uma função chamada
//	`P<id-de-outro>_roubar`, tentando sequestrar símbolos, apenas recebe o PRÓPRIO
//	id por cima — `P<id-dele>_P<id-vítima>_roubar` — que não liga a nada da
//	vítima. Uma checagem "esperta" (pular se já prefixado) REABRIRIA esse buraco.
//
//	Por quê o "P" inicial: um id pode começar com dígito (ex. "3f9a2b1c"), que não
//	é início válido de identificador C. O "P" garante identificador válido. O id é
//	usado verbatim; espera-se um token [A-Za-z0-9_] (garantido na criação).

// SymbolPrefix returns the prefix every exported C symbol of the black-box with
// this id carries: "P" + id + "_". Example: id "3f9a2b1c" → "P3f9a2b1c_".
//
// Português: Retorna o prefixo que todo símbolo C exportado da black-box com
// este id carrega: "P" + id + "_".
func SymbolPrefix(id string) string {
	return "P" + id + "_"
}

// PrefixSymbol prefixes symbol with the black-box's SymbolPrefix,
// UNCONDITIONALLY — it never checks whether symbol already looks prefixed. See
// the file header for why this unconditional behaviour IS the security property.
// Examples (id "3f9a2b1c"):
//
//	"init"                    → "P3f9a2b1c_init"
//	"sensorlib_init"          → "P3f9a2b1c_sensorlib_init"   (own namespace kept, still prefixed)
//	"Pxxxxxxxx_hijack"        → "P3f9a2b1c_Pxxxxxxxx_hijack" (foreign prefix stamped over — no hijack)
//
// Português: Prefixa symbol com o SymbolPrefix da black-box, SEMPRE — nunca checa
// se symbol já parece prefixado. Ver o header do arquivo: esse comportamento
// incondicional É a propriedade de segurança.
func PrefixSymbol(id, symbol string) string {
	return SymbolPrefix(id) + symbol
}

// SourceDir returns the folder that holds this black-box's generated files:
// "bb_" + id. The folder is named by the id, not the repository name, so two
// different owners with same-named repos never write into the same folder.
// Example: id "3f9a2b1c" → "bb_3f9a2b1c".
//
// Português: Retorna a pasta que guarda os arquivos gerados desta black-box:
// "bb_" + id. A pasta é nomeada pelo id, não pelo nome do repo, então dois donos
// com repos de mesmo nome nunca escrevem na mesma pasta.
func SourceDir(id string) string {
	return "bb_" + id
}
