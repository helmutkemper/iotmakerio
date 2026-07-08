// server/codegen/blackbox/parser_c_external.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "strings"

// External (non-static, file-scope) VARIABLE detection.
//
// English:
//
//	Why this exists: the multi-file rename model is "rename ALL external
//	symbols, expose SOME" (see BlackBoxDef.ExternalNames and csurface.go).
//	Functions are already covered — the function scanner ignores `static`
//	and surfaces everything else — but a specialist's files also share
//	STATE: `int g_bus_speed;` in util.c, referenced from core.c. That name
//	is an external link symbol; two boxes both owning a `g_state` would
//	collide in the maker's link unless both are renamed. This scanner
//	collects those variable names.
//
//	It is TOLERANT by doctrine (same stance as every other C scanner in
//	this package): it reads the common shapes of embedded C and silently
//	skips what it cannot parse. Known misses, accepted and documented:
//
//	  - exotic multi-dimensional declarators with parenthesised grouping
//	    (`int (*table[4])[8];`) beyond the simple function-pointer form;
//	  - K&R-era oddities;
//	  - variables declared via project-local macros.
//
//	A missed name means that ONE symbol keeps its authored spelling — a
//	potential cross-box collision, exactly today's risk surface, never a
//	new one. The specialist's fix is `static` (the right tool anyway) or
//	an IDS surface entry.
//
// Português:
//
//	O modelo de renomeação multiarquivo é "renomeia TUDO que é externo,
//	expõe SÓ a superfície". Funções já estão cobertas (o scanner ignora
//	`static`); faltavam as VARIÁVEIS de estado compartilhado entre os
//	arquivos do especialista (`int g_bus_speed;` no util.c, usada no
//	core.c) — símbolos externos que colidiriam entre caixas. Este scanner
//	as coleta, com a mesma postura tolerante dos demais: lê as formas
//	comuns do C embarcado e pula em silêncio o que não entende (misses
//	documentados acima). Nome perdido = um símbolo com a grafia original —
//	o risco de hoje, nunca um risco novo; o conserto do especialista é
//	`static`.

// cKeywordsNotDeclarators are tokens that can END a file-scope statement
// span without being a variable name. Guarding against them keeps a
// malformed span from donating a fake "variable".
var cKeywordsNotDeclarators = map[string]bool{
	"void": true, "int": true, "char": true, "short": true, "long": true,
	"float": true, "double": true, "signed": true, "unsigned": true,
	"const": true, "volatile": true, "struct": true, "union": true,
	"enum": true, "static": true, "extern": true, "typedef": true,
	"register": true, "inline": true, "restrict": true,
}

// extractCExternalVars walks the STRIPPED source (strings/comments blanked
// by preprocessC, so brace counting is safe) and returns the names of
// non-static file-scope variable definitions, in declaration order,
// deduplicated.
//
// The walk: at brace depth 0, a statement span runs until ';'. A '{' at
// depth 0 (a struct/enum/union body or a function body) is skipped to its
// matching '}' — the span then CONTINUES, because `struct { ... } g_cfg;`
// declares a variable after the body, while a function body's '}' is
// followed by no declarator and yields nothing.
//
// Per span, the filters mirror C's storage rules: `static`/`extern`/
// `typedef` spans are not external definitions here; `#` lines are
// preprocessor; spans with '(' are functions or prototypes — already the
// function scanner's job — EXCEPT the one variable shape that needs '(',
// the function-pointer variable `int (*handler)(int);`, which is captured
// by its distinctive `(*name)` declarator.
//
// Português: Caminha o fonte "stripped" no depth 0; um span vai até ';',
// pulando corpos entre chaves (o span CONTINUA depois do '}': `struct
// {...} g_cfg;` declara variável após o corpo; corpo de função não deixa
// declarador). Filtros espelham as regras do C: static/extern/typedef não
// são definição externa aqui; '(' é função/protótipo (trabalho do scanner
// de funções) — exceto a variável ponteiro-de-função `(*nome)`, capturada
// pela sua forma distinta.
func extractCExternalVars(stripped string) []string {
	var names []string
	seen := make(map[string]bool)

	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || cKeywordsNotDeclarators[name] || !isCIdentifier(name) {
			return
		}
		seen[name] = true
		names = append(names, name)
	}

	i, n := 0, len(stripped)
	for i < n {
		// Accumulate one file-scope statement span.
		var span strings.Builder
		endedAtFunctionBody := false
		for i < n {
			c := stripped[i]
			// Line comments survive preprocessC on purpose (the directive
			// scanners read label:/icon: from the stripped text), so THIS
			// walker skips them itself: comment prose inside a span turned
			// a '(' in a sentence into "this is a function" (swallowing a
			// real global) and a comment line before `typedef` defeated
			// the prefix filters. Caught live by the fixture harness.
			//
			// Português: Comentários de linha sobrevivem ao preprocessC de
			// propósito (os scanners de diretiva leem label:/icon: dali),
			// então ESTE caminhante pula-os por conta própria: prosa no
			// span fazia '(' de frase virar "é função" (engolindo um
			// global real) e comentário antes de `typedef` furava os
			// filtros de prefixo. Pego ao vivo pelo harness do fixture.
			if c == '/' && i+1 < n && stripped[i+1] == '/' {
				for i < n && stripped[i] != '\n' {
					i++
				}
				continue
			}
			if c == '{' {
				// Skip the body to its matching brace. What happens NEXT
				// depends on what the body belongs to, and the span itself
				// tells us: a '(' before the brace means a FUNCTION
				// definition — it ENDS at its closing brace (no ';'), so
				// the span terminates here and donates nothing. No '('
				// means a struct/enum/union body — the span CONTINUES to
				// ';' because `struct { ... } g_cfg;` declares a variable
				// after the body.
				//
				// Português: Pula o corpo. '(' antes da chave = definição
				// de FUNÇÃO — termina no '}' e o span acaba ali. Sem '(' =
				// corpo de struct/enum/union — o span CONTINUA até ';'
				// (`struct {...} g_cfg;` declara variável após o corpo).
				depth := 1
				i++
				for i < n && depth > 0 {
					switch stripped[i] {
					case '{':
						depth++
					case '}':
						depth--
					}
					i++
				}
				if strings.Contains(span.String(), "(") {
					endedAtFunctionBody = true
					break
				}
				continue
			}
			if c == ';' {
				i++
				break
			}
			span.WriteByte(c)
			i++
		}
		if endedAtFunctionBody {
			continue
		}
		stmt := strings.TrimSpace(span.String())
		if stmt == "" {
			continue
		}

		// Preprocessor lines never reach ';' logic cleanly and are not
		// declarations; a span whose visible text is only directives is
		// skipped wholesale. (preprocessC keeps directive text, so a span
		// may legitimately BEGIN with an #include swept before a real
		// declaration — trim leading directive lines instead of bailing.)
		for strings.HasPrefix(stmt, "#") {
			if nl := strings.IndexByte(stmt, '\n'); nl >= 0 {
				stmt = strings.TrimSpace(stmt[nl+1:])
			} else {
				stmt = ""
			}
		}
		if stmt == "" {
			continue
		}

		lower := stmt
		if strings.HasPrefix(lower, "typedef") ||
			strings.HasPrefix(lower, "static") ||
			strings.HasPrefix(lower, "extern") {
			continue // not an external definition owned by this box
		}

		if idx := strings.IndexByte(stmt, '('); idx >= 0 {
			// Function definition or prototype — the function scanner's
			// territory — EXCEPT the function-pointer variable, whose
			// declarator is the `(*name)` group.
			star := strings.Index(stmt, "(*")
			if star < 0 {
				continue
			}
			close := strings.IndexByte(stmt[star:], ')')
			if close < 0 {
				continue
			}
			decl := stmt[star+2 : star+close]
			// `(*name)` or `(*name[4])` — the identifier before any '['.
			if br := strings.IndexByte(decl, '['); br >= 0 {
				decl = decl[:br]
			}
			add(decl)
			continue
		}

		// Plain declaration span: split top-level commas (multi-declarator
		// `int a, b = 2;`), strip initialisers and array suffixes, and the
		// declarator is the LAST identifier of each piece.
		for _, piece := range strings.Split(stmt, ",") {
			if eq := strings.IndexByte(piece, '='); eq >= 0 {
				piece = piece[:eq]
			}
			if br := strings.IndexByte(piece, '['); br >= 0 {
				piece = piece[:br]
			}
			piece = strings.TrimSpace(piece)
			// Last identifier token: scan back over identifier chars.
			end := len(piece)
			for end > 0 && !isCIdentChar(piece[end-1]) {
				end--
			}
			start := end
			for start > 0 && isCIdentChar(piece[start-1]) {
				start--
			}
			if start < end {
				token := piece[start:end]
				// Two shapes donate a TYPE word instead of a variable and
				// must be filtered: a piece that is a single bare token
				// (`;`-terminated stray identifier), and the forward tag
				// declaration `struct fwd;` / `union u;` / `enum e;` —
				// two tokens where the first is the aggregate keyword and
				// the "declarator" is really the tag being introduced.
				//
				// Português: Duas formas doam TIPO em vez de variável:
				// token solto, e a declaração forward de tag (`struct
				// fwd;`) — o "declarador" é o próprio tag.
				fields := strings.Fields(stmt)
				if token == piece && len(fields) == 1 {
					continue
				}
				if len(fields) == 2 && token == fields[1] &&
					(fields[0] == "struct" || fields[0] == "union" || fields[0] == "enum") {
					continue
				}
				add(token)
			}
		}
	}
	return names
}

// isCIdentifier reports whether s is a well-formed C identifier.
func isCIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isCIdentChar(c) {
			return false
		}
		if i == 0 && c >= '0' && c <= '9' {
			return false
		}
	}
	return true
}

// isCIdentChar reports whether c can appear in a C identifier.
func isCIdentChar(c byte) bool {
	return c == '_' ||
		c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}
