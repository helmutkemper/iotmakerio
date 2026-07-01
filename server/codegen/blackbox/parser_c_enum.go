package blackbox

// parser_c_enum.go — C99 enum discovery and enumerator-value
// resolution (Slice C99-6 "enum type devices", design §12.2).
//
// Scope of this file:
//
//   - findAllCEnums: locate every `enum` declaration in the
//     stripped source, in all three C99 forms:
//         enum Tag { … };
//         typedef enum { … } Alias;
//         typedef enum Tag { … } Alias;
//   - parse each enumerator (name [= initialiser]) and resolve its
//     integer value per C99 §6.7.2.2: an enumerator with no
//     initialiser is (previous + 1), and the first such is 0.
//     Decimal and hexadecimal literals are honoured; other constant
//     expressions are kept verbatim (ValueIsRaw) rather than mis-
//     evaluated.
//
// What this file does NOT do: the trigger decision (is this enum
// referenced by a public function?) lives in parser_c.go, reusing
// referencedTypesInPublicAPI. Leading-comment label extraction also
// happens in parser_c.go where the comment map is in scope.

import (
	"strconv"
	"strings"
)

// rawCEnum is the parser's intermediate view of one enum, before
// trigger filtering and leading-comment processing.
type rawCEnum struct {
	Tag       string // "" when anonymous
	Alias     string // "" when there is no typedef alias
	Name      string // resolved: Tag if present, else Alias
	DeclStart int    // byte offset of the `enum` or `typedef` keyword
	DeclEnd   int    // byte offset just past the terminating `;`
	BodyStart int    // byte offset just after `{`
	BodyEnd   int    // byte offset of `}`
	Values    []rawCEnumValue
}

// rawCEnumValue is one enumerator with its resolved value.
type rawCEnumValue struct {
	Name        string
	Value       int
	HasExplicit bool   // had an `= …` initialiser
	ValueIsRaw  bool   // initialiser present but not int-evaluable
	RawValue    string // verbatim initialiser text when ValueIsRaw
	DeclStart   int    // byte offset of the enumerator name in source
}

// findAllCEnums walks `stripped` and returns every enum it finds.
// Offsets are valid against BOTH `stripped` and the original source
// because preprocessC preserves length (it blanks comment/string
// bytes in place rather than deleting them).
//
// Algorithm: scan for the `enum` keyword as a whole word. For each
// hit, walk forward over an optional tag identifier, require a `{`,
// matchBrace to the closing `}`, then read an optional alias
// identifier before the terminating `;`. A `typedef` keyword may
// precede `enum`; we detect it by scanning backward over whitespace.
func findAllCEnums(stripped string) []rawCEnum {
	var out []rawCEnum

	i := 0
	for i < len(stripped) {
		// Find the next `enum` token. We require word boundaries so
		// identifiers like `my_enum_t` don't false-trigger.
		idx := indexWord(stripped, "enum", i)
		if idx < 0 {
			break
		}
		// `enum` inside a `//` line comment is prose, not a
		// declaration. preprocessC keeps line comments verbatim for
		// directive reading, so skip them here.
		if isInsideLineComment(stripped, idx) {
			i = idx + len("enum")
			continue
		}

		// Determine the declaration start: if a `typedef` keyword
		// immediately precedes (only whitespace between), the decl
		// starts there; otherwise at `enum`.
		declStart := idx
		if tdAt := precedingTypedef(stripped, idx); tdAt >= 0 {
			declStart = tdAt
		}

		// Walk past `enum`.
		j := idx + len("enum")
		j = skipSpaces(stripped, j)

		// Optional tag.
		tag := ""
		if j < len(stripped) && isIdentByte(stripped[j]) {
			start := j
			for j < len(stripped) && isIdentByte(stripped[j]) {
				j++
			}
			tag = stripped[start:j]
			j = skipSpaces(stripped, j)
		}

		// Require `{`. If absent, this is a forward use (e.g.
		// `enum Color c;` as a variable type) — skip it.
		if j >= len(stripped) || stripped[j] != '{' {
			i = idx + len("enum")
			continue
		}
		bodyStart := j + 1
		bodyEnd, ok := matchBrace(stripped, j)
		if !ok {
			break
		}

		// Optional alias between `}` and `;`.
		k := skipSpaces(stripped, bodyEnd+1)
		alias := ""
		if k < len(stripped) && isIdentByte(stripped[k]) {
			start := k
			for k < len(stripped) && isIdentByte(stripped[k]) {
				k++
			}
			alias = stripped[start:k]
			k = skipSpaces(stripped, k)
		}

		// Terminating `;`.
		declEnd := k
		if declEnd < len(stripped) && stripped[declEnd] == ';' {
			declEnd++
		}

		name := tag
		if name == "" {
			name = alias
		}

		e := rawCEnum{
			Tag:       tag,
			Alias:     alias,
			Name:      name,
			DeclStart: declStart,
			DeclEnd:   declEnd,
			BodyStart: bodyStart,
			BodyEnd:   bodyEnd,
		}
		e.Values = parseEnumBody(stripped[bodyStart:bodyEnd], bodyStart)
		out = append(out, e)

		i = declEnd
		if i <= idx {
			i = idx + len("enum")
		}
	}

	return out
}

// parseEnumBody splits the text between `{` and `}` into
// enumerators and resolves each value. `baseOffset` is the byte
// offset of the body's first character in the original source so
// each enumerator's DeclStart points at the real source location
// (used later to attach/replace the `// label:` leading comment).
func parseEnumBody(body string, baseOffset int) []rawCEnumValue {
	var out []rawCEnumValue

	// Split on top-level commas. Enumerator initialisers cannot
	// contain commas at depth 0 in valid C99 (a comma there would
	// start a new enumerator), and we already stripped strings, so a
	// naive comma split is safe. We still track paren depth in case
	// of `= (1, 2)`-style oddities, which are not valid initialisers
	// but shouldn't crash us.
	prev := -1 // value of the previous enumerator
	seg := 0
	depth := 0
	start := 0
	flush := func(end int) {
		// Line comments (`//…`) are KEPT by preprocessC (IDS
		// directives live in them), so a segment may begin with one
		// or more comment lines before the enumerator name. Walk
		// forward over whitespace and `//…\n` runs to find the byte
		// where the real enumerator name starts.
		p := start
		for p < end {
			ch := body[p]
			if isSpace(ch) {
				p++
				continue
			}
			if ch == '/' && p+1 < end && body[p+1] == '/' {
				for p < end && body[p] != '\n' {
					p++
				}
				continue
			}
			break
		}
		if p >= end {
			return // segment was only whitespace/comments
		}
		nameStart := p
		off := baseOffset + nameStart

		// The declaration text runs to `end`, but may carry a
		// trailing `//` comment after the value (e.g.
		// `RED = 1, // note`). Cut it off before splitting.
		decl := body[nameStart:end]
		if ci := strings.Index(decl, "//"); ci >= 0 {
			decl = decl[:ci]
		}
		decl = strings.TrimSpace(decl)
		if decl == "" {
			return
		}

		name, initText := splitEnumerator(decl)
		if name == "" {
			return
		}

		v := rawCEnumValue{
			Name:      name,
			DeclStart: off,
		}
		if initText == "" {
			// No initialiser: previous + 1 (or 0 for the first).
			v.Value = prev + 1
		} else {
			v.HasExplicit = true
			if n, ok := parseCIntLiteral(initText); ok {
				v.Value = n
			} else {
				v.ValueIsRaw = true
				v.RawValue = initText
				// Best-effort so auto-increment after a raw value
				// still produces *some* number; we use prev+1.
				v.Value = prev + 1
			}
		}
		prev = v.Value
		out = append(out, v)
		seg++
	}

	for idx := 0; idx < len(body); idx++ {
		switch body[idx] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				flush(idx)
				start = idx + 1
			}
		}
	}
	// Trailing segment (no trailing comma).
	flush(len(body))

	return out
}

// splitEnumerator splits "NAME" or "NAME = INIT" into (name, init).
// The init part is returned trimmed; "" when there is no `=`.
func splitEnumerator(s string) (string, string) {
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return strings.TrimSpace(s), ""
	}
	name := strings.TrimSpace(s[:eq])
	init := strings.TrimSpace(s[eq+1:])
	return name, init
}

// parseCIntLiteral parses a decimal or hexadecimal C integer
// literal, tolerating a trailing `U`/`L` suffix and a leading sign.
// Returns (value, true) on success.
func parseCIntLiteral(s string) (int, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	// Strip integer suffixes (u, l, ul, ll, …) case-insensitively.
	for len(t) > 0 {
		last := t[len(t)-1]
		if last == 'u' || last == 'U' || last == 'l' || last == 'L' {
			t = t[:len(t)-1]
			continue
		}
		break
	}
	if t == "" {
		return 0, false
	}
	// strconv.ParseInt with base 0 understands 0x… and plain decimal
	// (and octal, which matches C semantics for a leading 0).
	n, err := strconv.ParseInt(t, 0, 64)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// indexWord finds the next whole-word occurrence of `word` in `s`
// at or after `from`. "Whole word" means the characters on either
// side are not identifier bytes, so `enum` does not match inside
// `my_enum`.
func indexWord(s, word string, from int) int {
	i := from
	for {
		rel := strings.Index(s[i:], word)
		if rel < 0 {
			return -1
		}
		at := i + rel
		leftOK := at == 0 || !isIdentByte(s[at-1])
		rightIdx := at + len(word)
		rightOK := rightIdx >= len(s) || !isIdentByte(s[rightIdx])
		if leftOK && rightOK {
			return at
		}
		i = at + len(word)
		if i >= len(s) {
			return -1
		}
	}
}

// precedingTypedef returns the byte offset of a `typedef` keyword
// that immediately precedes the position `at` (separated only by
// whitespace), or -1 when there is none.
func precedingTypedef(s string, at int) int {
	j := at - 1
	for j >= 0 && isSpace(s[j]) {
		j--
	}
	// j now points at the last byte before the whitespace run.
	const kw = "typedef"
	if j+1 < len(kw) {
		return -1
	}
	start := j + 1 - len(kw)
	if start < 0 {
		return -1
	}
	if s[start:j+1] != kw {
		return -1
	}
	// Word boundary on the left of `typedef`.
	if start > 0 && isIdentByte(s[start-1]) {
		return -1
	}
	return start
}
