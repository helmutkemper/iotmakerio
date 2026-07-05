// /server/codegen/blackbox/parser_c_func_regression_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// Regression pins for two C99 function-device parser fixes:
//
//  1. A preprocessor line (`#include`) between the file header and a void
//     function must not be swept into the return-type span — doing so made the
//     return type not exactly "void", which spawned a phantom "return" output.
//  2. Prose that wraps across lines must survive directive stripping intact —
//     the per-line "." rejoin used to corrupt a sentence split over two lines,
//     and that corruption reached the generated main.c (authored source is
//     inlined verbatim for the reader).

import (
	"strings"
	"testing"
)

func TestParseC_PreprocessorBeforeFunc_NoPhantomReturn(t *testing.T) {
	src := "#include <stdio.h>\n" +
		"\n" +
		"// writes an int.\n" +
		"// label:print_int.\n" +
		"// icon:terminal.\n" +
		"void print_int(\n" +
		"    // label:value.  the int to write.  connection:mandatory.\n" +
		"    int value) {\n" +
		"    printf(\"%d\\n\", value);\n" +
		"}\n"
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	if len(def.Functions) != 1 {
		t.Fatalf("Functions: got %d, want 1", len(def.Functions))
	}
	f := def.Functions[0]
	if len(f.Outputs) != 0 {
		t.Fatalf("a void function must have NO output ports; got %d: %+v", len(f.Outputs), f.Outputs)
	}
	if len(f.Inputs) != 1 || f.Inputs[0].Name != "value" {
		t.Fatalf("want exactly one input 'value'; got %+v", f.Inputs)
	}
}

func TestParseC_WrappedDocNotMangled(t *testing.T) {
	src := "#include <stdio.h>\n" +
		"\n" +
		"// print_int writes a single integer to standard output, followed by a\n" +
		"// newline. Host targets only.\n" +
		"// label:print_int.\n" +
		"// icon:terminal.\n" +
		"void print_int(int value) {\n" +
		"    printf(\"%d\\n\", value);\n" +
		"}\n"
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if err != nil {
		t.Fatalf("ParseC: %v", err)
	}
	doc := def.Functions[0].Doc
	// The sentence "...followed by a newline." wraps across two lines. No
	// spurious period may be inserted after "a".
	if strings.Contains(doc, "followed by a.") {
		t.Fatalf("wrapped sentence corrupted by a per-line period:\n%q", doc)
	}
	if !strings.Contains(doc, "followed by a") {
		t.Fatalf("prose was lost during directive stripping:\n%q", doc)
	}
}
