// server/codegen/blackbox/parser_c_enum_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// parser_c_enum_test.go — Tests for C99 enum type devices
// (Slice C99-6, §12.2).

import (
	"strings"
	"testing"
)

// ─── trigger gate ──────────────────────────────────────────────────────────────

func TestParseC_Enum_SurfacesWhenUsedInPublicSignature(t *testing.T) {
	src := `typedef enum {
    DISPLAY_COLOR_WHITE = 0,
    DISPLAY_COLOR_RED   = 1,
} display_color_t;

void display_write(display_color_t color, const char *text);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Enums) != 1 {
		t.Fatalf("want 1 enum surfaced; got %d", len(def.Enums))
	}
	if def.Enums[0].Name != "display_color_t" {
		t.Errorf("enum name = %q, want 'display_color_t'", def.Enums[0].Name)
	}
	if len(def.Enums[0].Values) != 2 {
		t.Errorf("want 2 values; got %d", len(def.Enums[0].Values))
	}
}

func TestParseC_Enum_HiddenWhenOnlyInternal(t *testing.T) {
	// The enum is used only by a static helper → not surfaced.
	src := `typedef enum {
    MODE_A = 0,
    MODE_B = 1,
} mode_t;

static void apply(mode_t m) { (void)m; }

void public_api(int x);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Enums) != 0 {
		t.Errorf("internal enum should be hidden; got %d enums", len(def.Enums))
	}
}

func TestParseC_Enum_HiddenWhenNoPublicFuncs(t *testing.T) {
	// No public functions at all → the trigger can't fire, enum stays hidden.
	src := `typedef enum { A, B } e_t;`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Enums) != 0 {
		t.Errorf("no public funcs → no enum surfaced; got %d", len(def.Enums))
	}
}

// ─── value resolution ───────────────────────────────────────────────────────────

func TestParseC_Enum_ImplicitAutoIncrement(t *testing.T) {
	src := `typedef enum {
    A, B, C
} abc_t;
void use(abc_t v);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Enums) != 1 {
		t.Fatalf("want 1 enum; got %d", len(def.Enums))
	}
	vals := def.Enums[0].Values
	if len(vals) != 3 {
		t.Fatalf("want 3 values; got %d", len(vals))
	}
	want := []int{0, 1, 2}
	for i, w := range want {
		if vals[i].Value != w {
			t.Errorf("value[%d] (%s) = %d, want %d", i, vals[i].Name, vals[i].Value, w)
		}
	}
}

func TestParseC_Enum_ExplicitThenImplicit(t *testing.T) {
	src := `typedef enum {
    A = 5,
    B,
    C = 10,
    D
} abc_t;
void use(abc_t v);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	vals := def.Enums[0].Values
	want := map[string]int{"A": 5, "B": 6, "C": 10, "D": 11}
	for _, v := range vals {
		if want[v.Name] != v.Value {
			t.Errorf("%s = %d, want %d", v.Name, v.Value, want[v.Name])
		}
	}
}

func TestParseC_Enum_HexLiteral(t *testing.T) {
	src := `typedef enum {
    LOW  = 0x00,
    HIGH = 0xFF
} level_t;
void use(level_t v);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	vals := def.Enums[0].Values
	if vals[0].Value != 0 || vals[1].Value != 255 {
		t.Errorf("hex parse wrong: %d, %d (want 0, 255)", vals[0].Value, vals[1].Value)
	}
}

func TestParseC_Enum_RawExpressionKeptVerbatim(t *testing.T) {
	src := `typedef enum {
    FLAG_A = 1,
    FLAG_B = (1 << 2)
} flag_t;
void use(flag_t v);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	vals := def.Enums[0].Values
	if !vals[1].ValueIsRaw {
		t.Errorf("FLAG_B should be ValueIsRaw; got value=%d raw=%v", vals[1].Value, vals[1].ValueIsRaw)
	}
	if !strings.Contains(vals[1].RawValue, "1 << 2") {
		t.Errorf("RawValue should keep the expression; got %q", vals[1].RawValue)
	}
}

// ─── naming forms ────────────────────────────────────────────────────────────────

func TestParseC_Enum_TagWinsOverAlias(t *testing.T) {
	src := `typedef enum Color {
    RED, GREEN
} color_t;
void use(color_t c);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Enums) != 1 {
		t.Fatalf("want 1 enum; got %d", len(def.Enums))
	}
	// Tag wins for Name; trigger matched via the alias used in the
	// signature.
	if def.Enums[0].Name != "Color" {
		t.Errorf("Name = %q, want 'Color' (tag wins)", def.Enums[0].Name)
	}
}

func TestParseC_Enum_TagOnlyForm(t *testing.T) {
	src := `enum Color { RED, GREEN };
void use(enum Color c);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	if len(def.Enums) != 1 || def.Enums[0].Name != "Color" {
		t.Errorf("tag-only enum should surface as 'Color'; got %+v", def.Enums)
	}
}

// ─── labels from leading comments ────────────────────────────────────────────────

func TestParseC_Enum_PerValueLabelsFromLeadingComments(t *testing.T) {
	src := `typedef enum {
    // label:White.
    DISPLAY_COLOR_WHITE = 0,
    // label:Red.
    DISPLAY_COLOR_RED   = 1,
    DISPLAY_COLOR_BLUE  = 2,
} display_color_t;
void display_write(display_color_t color);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	vals := def.Enums[0].Values
	byName := map[string]string{}
	for _, v := range vals {
		byName[v.Name] = v.Label
	}
	if byName["DISPLAY_COLOR_WHITE"] != "White" {
		t.Errorf("WHITE label = %q, want 'White'", byName["DISPLAY_COLOR_WHITE"])
	}
	if byName["DISPLAY_COLOR_RED"] != "Red" {
		t.Errorf("RED label = %q, want 'Red'", byName["DISPLAY_COLOR_RED"])
	}
	if byName["DISPLAY_COLOR_BLUE"] != "" {
		t.Errorf("BLUE label should be empty (incomplete); got %q", byName["DISPLAY_COLOR_BLUE"])
	}
}

func TestParseC_Enum_LevelIconLabelFromLeadingComment(t *testing.T) {
	src := `// icon:palette. label:Color.
typedef enum {
    A, B
} color_t;
void use(color_t c);`
	def, _ := ParseC([]byte(src), DefaultParserLimits())
	e := def.Enums[0]
	if e.Icon != "palette" {
		t.Errorf("enum icon = %q, want 'palette'", e.Icon)
	}
	if e.Label != "Color" {
		t.Errorf("enum label = %q, want 'Color'", e.Label)
	}
}
