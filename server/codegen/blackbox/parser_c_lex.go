package blackbox

// parser_c_lex.go — Low-level lexical helpers for the C99 parser.
//
// English:
//
//	This file isolates the byte-level scanning utilities that the
//	C99 parser uses to navigate raw source. None of these functions
//	produce a BlackBoxDef — they are pure text helpers — but they
//	encode every quirk of C99 lexical syntax that downstream phases
//	rely on. Keeping them in their own file lets parser_c.go focus
//	on the structural work (find structs, find functions, classify
//	ports) without drowning in boundary-byte arithmetic.
//
//	The big idea is preprocessC: it runs once at the start of a
//	ParseC call and returns a "stripped" copy of the source where
//	every byte that lives inside a string literal, char literal, or
//	block comment is replaced by a space. The original byte offsets
//	are preserved. Newlines are also preserved so error messages
//	can report correct line numbers. Line comments (`//`) survive
//	unchanged because they carry IDS directives the parser needs.
//
//	Once preprocessed, the parser can treat the source as a flat
//	stream of code tokens — string contents cannot accidentally
//	contain `}` or `;` that throw off brace matching or statement
//	splitting. The block-comment map is returned so the parser can
//	look up the original text of any /* */ block by its start
//	offset (used when the package-doc collector wants the actual
//	comment contents, not the stripped spaces).
//
// Português:
//
//	Helpers léxicos do parser C99. Funções puras de manipulação de
//	texto — não produzem BlackBoxDef. preprocessC roda uma vez no
//	início, devolvendo uma cópia da fonte com strings, chars e
//	comentários de bloco substituídos por espaços (preservando
//	offsets e quebras de linha). Comentários de linha (`//`)
//	sobrevivem porque carregam as diretivas IDS. O mapa
//	blockComments permite buscar o texto original de um /* */
//	pelo seu offset inicial.

import "strings"

// ─── preprocessing ─────────────────────────────────────────────────────────────

// preprocessC strips string/char literals and block comments from the
// source. Returns:
//
//   - stripped: a copy of `src` with every byte that lived inside a
//     string literal, char literal, or /* */ block replaced by a
//     space. Newlines are preserved verbatim so line counting works.
//     Line comments (// ...) are NOT touched.
//
//   - blockComments: a map from the start offset of each /* */ block
//     in the original source to its original text (including the /*
//     and */ markers). The package-doc collector and the leading-
//     comment collector use this to read original /* */ content when
//     the stripped version would just be spaces.
//
// Performance notes:
//
//   - Single linear pass over the source bytes. No allocations beyond
//     the output buffer and the blockComments map.
//   - Unterminated string/char/block-comment literals are treated as
//     reaching end-of-source — the parser will surface its own error
//     when it cannot find a closing `}` or `;` downstream.
func preprocessC(src string) (stripped string, blockComments map[int]string) {
	out := []byte(src)
	blockComments = make(map[int]string)

	i := 0
	for i < len(src) {
		c := src[i]

		switch {
		case c == '"':
			// String literal. Walk past escape sequences (\" inside
			// the string does not close it). Stop at the matching ".
			j := i + 1
			for j < len(src) {
				if src[j] == '\\' && j+1 < len(src) {
					// Replace both the backslash and the escaped
					// byte with spaces unless the escaped byte is a
					// newline (which preserves line counts).
					out[j] = ' '
					if src[j+1] != '\n' {
						out[j+1] = ' '
					}
					j += 2
					continue
				}
				if src[j] == '"' {
					break
				}
				if src[j] != '\n' {
					out[j] = ' '
				}
				j++
			}
			i = j + 1

		case c == '\'':
			// Char literal. Same shape as string literal — character
			// constants can contain escape sequences too.
			j := i + 1
			for j < len(src) {
				if src[j] == '\\' && j+1 < len(src) {
					out[j] = ' '
					if src[j+1] != '\n' {
						out[j+1] = ' '
					}
					j += 2
					continue
				}
				if src[j] == '\'' {
					break
				}
				if src[j] != '\n' {
					out[j] = ' '
				}
				j++
			}
			i = j + 1

		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			// Block comment /* ... */. Record the original text,
			// then space-fill (preserving newlines).
			start := i
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				// Unterminated — treat rest of source as comment.
				blockComments[start] = src[start:]
				for k := start; k < len(src); k++ {
					if src[k] != '\n' {
						out[k] = ' '
					}
				}
				i = len(src)
				continue
			}
			absEnd := i + 2 + end + 2 // include the closing */
			blockComments[start] = src[start:absEnd]
			for k := start; k < absEnd; k++ {
				if src[k] != '\n' {
					out[k] = ' '
				}
			}
			i = absEnd

		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			// Line comment // ... — KEEP intact. IDS directives live
			// here. Walk to end-of-line so we don't re-enter as a
			// string or other lexeme; the bytes themselves stay.
			for i < len(src) && src[i] != '\n' {
				i++
			}

		default:
			i++
		}
	}
	return string(out), blockComments
}

// ─── tiny character helpers ────────────────────────────────────────────────────

// isIdentByte reports whether b is allowed inside a C identifier
// (letter, digit, or underscore). The first character of an
// identifier must additionally be non-digit; callers that need that
// stricter check use isIdentStartByte.
func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// isIdentStartByte reports whether b is a valid first character of a
// C identifier (letter or underscore). Used when we need to be sure
// we're at the start of an identifier rather than inside one.
func isIdentStartByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		b == '_'
}

// isSpace reports whether b is one of the C99 whitespace bytes the
// parser cares about (excluding form-feed and vertical-tab, which
// are technically whitespace per the standard but never appear in
// hand-written embedded code).
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// hasWordAt reports whether `word` appears in `s` starting at offset
// `i` AND is followed by a non-identifier byte (or end of string).
// This avoids matching `structurally` when looking for the keyword
// `struct`, for example.
func hasWordAt(s string, i int, word string) bool {
	if i+len(word) > len(s) {
		return false
	}
	if s[i:i+len(word)] != word {
		return false
	}
	next := i + len(word)
	if next == len(s) {
		return true
	}
	return !isIdentByte(s[next])
}

// ─── scanning utilities ────────────────────────────────────────────────────────

// skipSpaces advances past runs of whitespace and returns the new
// offset. Used in dozens of phases.
func skipSpaces(s string, i int) int {
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	return i
}

// readIdent reads the identifier starting at offset i in s. Returns
// the identifier and the offset past it. Returns "" and i unchanged
// when the byte at i is not a valid identifier start.
func readIdent(s string, i int) (string, int) {
	if i >= len(s) || !isIdentStartByte(s[i]) {
		return "", i
	}
	j := i + 1
	for j < len(s) && isIdentByte(s[j]) {
		j++
	}
	return s[i:j], j
}

// matchBrace returns the offset of the `}` that pairs with the `{`
// at offset open. Operates on the stripped source so braces inside
// strings/comments cannot confuse the count. Returns (0, false) when
// no match is found (malformed source).
func matchBrace(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// matchParen returns the offset of the `)` that pairs with the `(`
// at offset open. Same shape as matchBrace, used when scanning
// function parameter lists and nested function-pointer types.
func matchParen(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// findStatementEnd returns the offset of the next top-level `;` at or
// after `start`, ignoring semicolons nested inside braces or
// parentheses. Operates on the stripped source. Returns (-1, false)
// when none is found before EOF.
func findStatementEnd(s string, start int) (int, bool) {
	braceDepth, parenDepth := 0, 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case ';':
			if braceDepth == 0 && parenDepth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}

// ─── leading-comment collection ────────────────────────────────────────────────

// collectLeadingComments walks backwards from byteOffset, gathering
// the contiguous block of `//` lines and `/* */` blocks immediately
// above. Returns the joined comment text with `//`/`/*`/`*/` markers
// stripped — ready to feed into extractDocDirectives.
//
// The walk stops at the first blank line, the first non-comment
// non-blank line, or the start of the source. Blank lines BREAK the
// streak: a comment block that is two newlines above the
// declaration is not considered "leading".
//
// `blockComments` carries the original text of /* */ blocks (the
// stripped source has them as spaces) so the function can recover
// directives inside block comments.
func collectLeadingComments(src string, byteOffset int, blockComments map[int]string) string {
	// Find the start of the line containing byteOffset.
	lineStart := byteOffset
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}

	var lines []string
	cur := lineStart
	for cur > 0 {
		// Move to the previous line.
		prevLineEnd := cur - 1 // points at '\n'
		prevLineStart := prevLineEnd
		for prevLineStart > 0 && src[prevLineStart-1] != '\n' {
			prevLineStart--
		}
		lineText := src[prevLineStart:prevLineEnd]
		trimmed := strings.TrimSpace(lineText)

		if trimmed == "" {
			// Blank line — comments must be glued; stop here.
			break
		}

		if strings.HasPrefix(trimmed, "//") {
			lines = append([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))}, lines...)
			cur = prevLineStart
			continue
		}

		// Could be the end of a block comment. Recognise `*/` at end
		// of line and recover the full block from blockComments.
		if strings.HasSuffix(trimmed, "*/") {
			block := findBlockEndingNear(blockComments, prevLineStart, prevLineEnd)
			if block == "" {
				break
			}
			content := stripBlockBody(block)
			lines = append([]string{content}, lines...)
			// Continue scanning from the opener line.
			openerStart := findBlockStartForText(src, blockComments, block)
			if openerStart < 0 || openerStart >= cur {
				break
			}
			// Step back to the line containing the opener.
			openerLineStart := openerStart
			for openerLineStart > 0 && src[openerLineStart-1] != '\n' {
				openerLineStart--
			}
			cur = openerLineStart
			continue
		}

		// Non-comment non-blank line — the streak ends here.
		break
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// findBlockEndingNear returns the block-comment text whose closing
// `*/` falls within (or just past) the byte range [lineStart,
// lineEnd]. Returns "" when no such block is registered.
func findBlockEndingNear(blocks map[int]string, lineStart, lineEnd int) string {
	for start, text := range blocks {
		end := start + len(text)
		if end >= lineStart && end <= lineEnd+1 {
			return text
		}
	}
	return ""
}

// findBlockStartForText returns the byte offset where the given
// block-comment text starts in the source, or -1 if not found.
func findBlockStartForText(src string, blocks map[int]string, text string) int {
	for start, t := range blocks {
		if t == text {
			return start
		}
	}
	// Fall back to a direct search (cheap for our sizes).
	return strings.Index(src, text)
}

// stripBlockBody removes the /* and */ markers and any per-line "* "
// continuation prefixes from a block comment, returning the inner
// content joined with newlines.
func stripBlockBody(block string) string {
	body := strings.TrimPrefix(block, "/*")
	body = strings.TrimSuffix(body, "*/")
	var out []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		t = strings.TrimPrefix(t, "*")
		t = strings.TrimSpace(t)
		out = append(out, t)
	}
	// Trim leading/trailing empty lines.
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// ─── package doc + includes ────────────────────────────────────────────────────

// extractCPackageDoc returns the first contiguous comment block at
// the top of the source, before any #include or declaration. Both
// `//` and `/* */` forms are supported and may mix. IDS directives
// are stripped — they belong to a specific struct, not the file.
func extractCPackageDoc(src string) string {
	lines := strings.SplitAfter(src, "\n")
	var docLines []string
	inBlock := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)

		if inBlock {
			docLines = append(docLines, stripBlockCommentMarkers(trimmed))
			if strings.Contains(trimmed, "*/") {
				inBlock = false
			}
			continue
		}

		if trimmed == "" {
			if len(docLines) == 0 {
				continue
			}
			docLines = append(docLines, "")
			continue
		}

		if strings.HasPrefix(trimmed, "//") {
			docLines = append(docLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			inBlock = !strings.Contains(trimmed[2:], "*/")
			docLines = append(docLines, stripBlockCommentMarkers(trimmed))
			continue
		}

		// First non-comment, non-blank line — stop.
		break
	}

	// Drop trailing blank sentinels.
	for len(docLines) > 0 && strings.TrimSpace(docLines[len(docLines)-1]) == "" {
		docLines = docLines[:len(docLines)-1]
	}

	doc := strings.Join(docLines, "\n")
	doc, _, _, _, _, _, _ = extractDocDirectives(doc)
	return strings.TrimSpace(doc)
}

// stripBlockCommentMarkers removes /* */ borders and leading "* "
// continuation marks from a single line. Used by the package-doc
// collector for /* */ blocks read line-by-line.
func stripBlockCommentMarkers(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "/*")
	line = strings.TrimSuffix(line, "*/")
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "*")
	return strings.TrimSpace(line)
}

// extractCIncludes returns the verbatim `#include <foo.h>` /
// `#include "foo.h"` targets from the source. The brackets/quotes
// are kept inline so a downstream consumer can tell system headers
// from project headers without a separate flag (mirroring the Go
// parser's Imports slice that preserves the import path verbatim).
func extractCIncludes(stripped string) []string {
	var out []string
	for _, line := range strings.Split(stripped, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if !strings.HasPrefix(t, "include") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(t, "include"))
		if len(rest) > 0 && (rest[0] == '<' || rest[0] == '"') {
			out = append(out, rest)
		}
	}
	return out
}
