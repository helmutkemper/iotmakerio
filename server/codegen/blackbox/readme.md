# codegen/blackbox — Black-box source parser

## What this package does

Reads the Go source of a black-box component and extracts everything the
code-generation pipeline needs to render visual blocks and generate Go code:

- Struct name and configurable fields (`prop` tags)
- Complete signature of `Init()` and all named methods (inputs, outputs, types)
- Raw source code of the struct and methods (included verbatim in generated output)
- Embedded documentation pages from `/* */` blocks
- Required import paths
- Visual directives: `icon:`, `label:`, `executionOrder:`, `menu:`

---

## Why use AST instead of regexes?

Go source has recursive structure and ambiguities that make regex parsing
unreliable. Examples the AST handles correctly but regex does not:

```go
// This string contains "func" and parentheses
type Sensor struct {
	msg string  `prop:"Label with (parens) and \"quotes\""`
}

func (s *Sensor) Init(
	i2c  *machine.I2C,   // parameter on a separate line
	addr uint8,
) (err error) { }
```

The standard `go/ast` package parses correctly in all these cases because it
understands the full Go grammar.

---

## Exported types

### BlackBoxDef — complete component definition

```go
type BlackBoxDef struct {
	Name        string          // struct name (e.g. "APDS9960")
	Doc         string          // package-level doc comment
	StructIcon  string          // icon: from struct doc comment
	StructLabel string          // label: from struct doc comment
	Imports     []string        // import paths (e.g. ["machine", "time"])
	Init        *FuncDef        // nil when absent
	Methods     []NamedFuncDef  // non-Init methods in source-file order
	Props       []PropDef       // fields with `prop:` tag
	StructCode  string          // raw source of the struct declaration
	MethodsCode string          // raw source of all methods WITH doc comments
	ManualPages []ManualPage    // pages from /* */ blocks
}
```

### FuncDef — signature of Init() or any named method

```go
type FuncDef struct {
	Doc            string    // human-readable doc (machine directives stripped)
	ExecutionOrder int       // from executionOrder:N (0 = unordered)
	Icon           string    // from icon: directive (kebab-case name or "" )
	Label          string    // from label: directive ("" = use method name)
	MenuCol        int       // from menu:col,row — column offset from Back center
	MenuRow        int       // from menu:col,row — row offset from Back center
	MenuPosSet     bool      // true when menu: was explicitly declared
	Inputs         []PortDef // parameters
	Outputs        []PortDef // returns (including error)
}
```

`MenuCol` and `MenuRow` are **signed offsets** from the Back button center.
The absolute grid position is `BackCenterCol + MenuCol`, `BackCenterRow + MenuRow`.
`MenuPosSet = false` means the IDE should auto-place the item using the radial
layout engine (`rulesMainMenu.ApplyRadialLayout`).

### PortDef — one input or output

```go
type PortDef struct {
	Name    string  // parameter/return name
	GoType  string  // full type string ("int", "*machine.I2C", "uint16")
	IsError bool    // true if this is an error return
	Doc     string  // IDS comment line for this port
}
```

### PropDef — configurable field

```go
type PropDef struct {
	FieldName string   // Go field name (e.g. "gain")
	GoType    string   // Go type (e.g. "byte")
	Label     string   // Inspect panel label (prop tag)
	Default   string   // default value (default tag)
	Options   []string // dropdown options (options tag)
}
```

### ManualPage — documentation page

```go
type ManualPage struct {
	Name     string       // identifier (e.g. "wiring-guide")
	Language string       // BCP-47 lowercase (e.g. "en", "pt-br")
	ShowIn   ManualShowIn // "init", "both", or method name
	Content  string       // Markdown ready for rendering
}
```

---

## Machine directives extracted by extractDocDirectives()

`extractDocDirectives(doc string)` scans a doc comment, removes all machine
directives, and returns the clean human-readable text plus the extracted values.

| Directive         | Syntax                | Return value                                              |
|-------------------|-----------------------|-----------------------------------------------------------|
| `executionOrder:` | `executionOrder:N.`   | `order int` (0 = not set)                                 |
| `icon:`           | `icon:name.`          | `icon string` (kebab-case name or unicode codepoint)      |
| `label:`          | `label:text.`         | `label string` (preserves original casing)                |
| `menu:`           | `menu:col,row.`       | `menuCol, menuRow int` + `menuPosSet bool`                |

All four follow the IDS dot-terminated format. They may appear on the same line
or on separate lines. Lines containing only machine directives are dropped from
the clean doc output.

Struct-level parsing only extracts `icon:` and `label:` — `executionOrder:` and
`menu:` are not meaningful at the struct level and are discarded.

---

## Parse algorithm — 4 steps

### Step 1 — Go AST parse

```go
fset := token.NewFileSet()
file, err := parser.ParseFile(fset, "blackbox.go", src, parser.ParseComments)
```

`parser.ParseComments` is required so doc comments are accessible in the AST.

### Step 2 — Locate the exported struct

`findExportedStruct` scans `Decls` for a `GenDecl` with token `TYPE` whose
`TypeSpec` is an exported `StructType`. Returns the `GenDecl` (not `TypeSpec`)
because it has the correct offset for source extraction including the doc comment.

### Step 3 — Extract Init() and named methods

`findMethod` finds `Init`. All other exported methods on the struct are appended
to `Methods []NamedFuncDef` in source-file order.

`extractFuncDef` converts each `ast.FuncDecl` to a `FuncDef`:
- Parameters → `Inputs []PortDef`
- Returns → `Outputs []PortDef` (unnamed returns get auto-names: `err`, `out1`, …)
- Doc comment → `extractDocDirectives` for `Doc`, `ExecutionOrder`, `Icon`, `Label`,
	`MenuCol`, `MenuRow`, `MenuPosSet`

### Step 4 — Extract MethodsCode with doc comments

`nodeSourceWithDoc` uses `funcDecl.Doc.Pos()` (not `funcDecl.Pos()`) as the
start offset so the raw source includes the full doc comment — IDS tags and all.

---

## Manual pages — `/* */` blocks

`extractBlockComments` scans the raw source byte-by-byte, correctly skipping
string literals (`"..."` and `` `...` ``), and collects all `/* */` blocks.

`parseOneManualBlock` processes each block:
1. Reads directives line by line before ` ```markdown`
2. `manualName:` is required — absent blocks are silently ignored
3. Content is everything between ` ```markdown` and the closing ` ```*/`

Malformed blocks produce soft warnings (not errors). One bad block never prevents
the rest of the component from loading.

---

## Adding a new machine directive

1. Add the field to `FuncDef` in `types.go`.
2. Update `extractDocDirectives` signature and `tryExtractDirective` in `parser.go`.
3. Update the caller that discards struct-level values if the directive is
	 method-only (see the `_, _, def.StructIcon, def.StructLabel, _, _, _` pattern).
4. Add the field to `clientFuncDef` / `clientMethodDef` in
	 `server/handler/blackboxapi/handler.go`.
5. Propagate in `toClientFuncDef` / `toClientDef`.
6. Mirror the field in `blackbox/clientTypes.go` (WASM client types).
7. Update `server/blackbox/readme.md` (IDS standard documentation).
8. Update this file.

---

## Rewrite engine — `Rewrite(source, edits)`

`rewrite.go` and `tagcodec.go` together implement the slice-1 wizard
backend. The public entry point is:

```go
func Rewrite(source string, edits []WizardEdit) (string, error)
```

It applies a list of typed edits to a Go source file and returns the
rewritten, gofmt-formatted source. On any error the **original source
is returned unchanged** alongside the error so the caller can surface a
message to the user without losing in-progress work.

### Operations supported (slice 1)

| `op` | Path target | Effect |
|------|-------------|--------|
| `setStructDirectives` | `struct.<n>` | Replace struct doc with user comment + `label:`/`icon:` directives. |
| `setFieldProp` | `struct.<S>.field.<F>` | Build IDS prop tag (prop, default, options/range/regex, unit) and merge into the existing struct tag, preserving non-IDS keys. Optional `comment` arg also rewrites the field's godoc. |
| `disableFieldProp` | `struct.<S>.field.<F>` | Remove every IDS-owned tag key, keep all others. Drops the tag entirely if no user keys remain. |
| `setMethodDirectives` | `method.<S>.<M>` | Replace method doc with user comment + `label:`/`icon:`/`executionOrder:` directives. |
| `setPortConnection` | `method.<S>.<M>.{in\|out}.<n>` | Set or replace the port's IDS comment block above the parameter, with `connection:` and optional `label:`. Auto-expands single-line param/result lists to multi-line. |

### Two-phase pipeline

The engine separates tag mutations from comment mutations because they
fight for control of the same printer state. Tag edits go through
`go/ast` directly — `go/printer` round-trips tags verbatim, so this is
safe. Comment edits go through byte-level splices on the post-tag
intermediate source — `go/printer` is brittle when given synthetic
`*ast.CommentGroup` values with hand-built `token.Pos`, and the splice
approach sidesteps that fight entirely.

```
parse → apply tag edits on AST → format.Node →
        re-parse → apply doc splices RTL → format.Source
```

The right-to-left splice order keeps every recorded byte offset valid
even as earlier splices grow or shrink the buffer.

### Invariants (CLAUDE_WIZARD_DESIGN.md §5.1)

- **User-owned tag keys** (`json:`, `yaml:`, `xml:`, anything not in
  `idsOwnedKeys`) round-trip exactly — even their original whitespace is
  preserved when the field's tag is not the target of an edit.
- **User godoc prose** is preserved. The wizard always sends the full
  prose back as the `comment` arg; the engine reconstructs the comment
  block as `prose` + blank godoc line + IDS directives.
- **Function bodies** are never read or written.
- **Imports** are never inserted, reordered, or removed.
- **gofmt-valid output** — if `format.Source` rejects the result, the
  rewrite fails and the original source comes back unchanged.

### Single-line param-list expansion

IDS port directives live in line comments above each parameter. A
parameter on the same line as the opening `(` has nowhere to attach
that block. When a port edit targets such a parameter, the engine
expands that specific FieldList to multi-line, preserving each
parameter's type expression verbatim via `go/format` on the type AST
node. The expansion is gofmt-stable — gofmt does not collapse
multi-line param lists with trailing commas.

A method that has only struct-level edits keeps its inline param list
untouched.

### Tag codec contract (`tagcodec.go`)

`parseStructTag` and `emitStructTag` form a round-trip codec for the
contents of a Go struct tag (what lives between the backticks).

- Values are decoded with `strconv.Unquote` and re-encoded with
  `strconv.Quote`; embedded escapes survive the round trip.
- Pair order is preserved.
- Empty input parses to `(nil, nil)`; emit of an empty list returns
  `""`.
- Malformed input (missing colon, unbalanced quote, bad escape)
  returns an error rather than silently dropping pairs.

`idsOwnedKeys` enumerates the wizard-managed keys: `prop`, `default`,
`options`, `range`, `range_min`, `range_max`, `regex`, `unit`,
`encoding`, `bits`, `inputRegex`, `connection`. Every other key is
user-owned. To add a new IDS-managed key, extend that map and update
`buildPropPairs` in `rewrite.go`.

### HTTP surface

The engine is exposed via `POST /api/v1/blackbox/wizard/rewrite` in
`server/handler/blackboxapi/wizard.go`. The handler is a thin shim:
bind the request, delegate to `Rewrite`, return the new source in the
canonical envelope. See that file's header for the request and
response shapes.

### Tests

`rewrite_test.go` covers each operation end-to-end plus the two
preservation invariants (`TestRewrite_setFieldProp_preservesNonIDSTags`
and `TestRewrite_setStructDirectives_preservesUserProse`), the
single-line param-list expansion, and the no-op + error paths. Run with:

```
go test ./server/codegen/blackbox/...
```

---

## Completion engine — `ComputeIncomplete(def)`

`completion.go` is the single source of truth for the wizard's ⚠
rendering and the publish-gate check (slice 8). Every consumer that
needs to know "is X configured?" calls this function — the client
never recomputes.

```go
func ComputeIncomplete(def *BlackBoxDef) []string
```

The result is a **sorted slice of dotted paths**. Sorted because the
draft validator (slice 3) compares the server's recomputed set
against the client-posted one with deep equality; an unsorted result
would generate spurious tampering alarms. The slice is also never
nil — an empty file returns `[]string{}`, which JSON-marshals to `[]`
rather than `null`, so the JS can `forEach` without a null guard.

### Rules (from CLAUDE_WIZARD_DESIGN.md §6.2)

| Path                              | Mandatory                       |
|-----------------------------------|---------------------------------|
| `struct.<n>`                   | `label` and `icon`              |
| `struct.<S>.field.<F>` (native)   | `label` and `default`           |
| `method.<S>.<M>` (Init or named)  | `label` and `icon`              |
| `method.<S>.<M>.{in\|out}.<n>`    | `connection:` tag (any value)   |

### What is automatically excluded

- **Non-native fields** (`*machine.I2C`, `time.Duration`, etc.) are
  inert — they have no UI in the Field modal and never appear in the
  set. The `IsNativePropType` helper enforces this and is exported so
  slice 4's modal can mirror the same check.
- **Disabled fields** (no `prop:` tag at all) never reach `def.Props`
  in the first place — the parser only surfaces fields with `prop:`.
- **Error returns** (`(err error)`) and **anonymous parameters** are
  exempt from the port rule — the former because the IDS spec does
  not ask for a connection on errors, the latter because they cannot
  be addressed by any wizard path.

### Wiring

The function is invoked twice in `server/handler/blackboxapi/wizard.go`:

- `/wizard/parse` returns `{ parsed, incomplete }` so the wizard tab
  can render ⚠ on initial load.
- `/wizard/rewrite` re-parses the rewritten source and returns
  `{ code, parsed, incomplete, applied }` so a save round-trip refreshes
  the entire wizard view in one HTTP call.

### Tests

`completion_test.go` is table-driven, with one block per rule plus
edge cases: error returns, anonymous ports, non-native types, nil def,
sort order, and an end-to-end test that parses a real Go fragment and
confirms the set matches what the rules promise. Run with:

```
go test ./server/codegen/blackbox/...
```
