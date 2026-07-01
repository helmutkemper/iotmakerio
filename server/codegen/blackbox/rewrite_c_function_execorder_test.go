// /server/codegen/blackbox/rewrite_c_function_execorder_test.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"strings"
	"testing"
)

// TestRewriteC_FunctionExecutionOrder verifies the C99 function planner writes
// and clears the executionOrder directive exactly like the Go method planner:
// a non-nil value emits executionOrder:N.; omitting it (the wizard's "clear")
// drops the directive, because the whole comment block is rebuilt from args.
func TestRewriteC_FunctionExecutionOrder(t *testing.T) {
	src := "" +
		"// label:Create.\n" +
		"// icon:plus.\n" +
		"sht3x_t *sht3x_create(i2c_port_t bus) {\n" +
		"    return 0;\n" +
		"}\n"

	// Set executionOrder = 20, carrying label/icon as the wizard's card does.
	out, err := RewriteC(src, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.sht3x_create", map[string]any{
			"label":          "Create sensor",
			"icon":           "plus",
			"executionOrder": 20,
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (set): %v", err)
	}
	if !strings.Contains(out, "executionOrder:20.") {
		t.Fatalf("want executionOrder:20. in output, got:\n%s", out)
	}
	if !strings.Contains(out, "label:Create sensor.") {
		t.Fatalf("label must be preserved alongside executionOrder, got:\n%s", out)
	}

	// Clear: omit executionOrder → directive removed (block rebuilt from args).
	out2, err := RewriteC(out, []WizardEdit{
		mkEdit(OpSetStructDirectives, "function.sht3x_create", map[string]any{
			"label": "Create sensor",
			"icon":  "plus",
		}),
	})
	if err != nil {
		t.Fatalf("RewriteC (clear): %v", err)
	}
	if strings.Contains(out2, "executionOrder") {
		t.Fatalf("executionOrder must be removed when omitted, got:\n%s", out2)
	}
	if !strings.Contains(out2, "label:Create sensor.") {
		t.Fatalf("label must survive the clear, got:\n%s", out2)
	}
}
