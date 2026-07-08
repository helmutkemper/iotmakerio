// server/codegen/blackbox/rewrite_c_callbacktype_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"encoding/json"
	"strings"
	"testing"
)

// The §12.3 round trip, pinned end to end: a consumed function-pointer
// typedef surfaces with UsedAsParameter, starts incomplete, gains
// icon/label through the rewrite, re-parses complete, and a blank
// payload clears it back to incomplete.
//
// Português: O round trip do §12.3 fixado: typedef consumido aparece
// (UsedAsParameter), nasce incompleto, ganha icon/label pelo rewrite,
// re-parseia completo, e payload vazio o devolve a incompleto.

const cbTypeSrc = `#include <stdint.h>

typedef void (*probe_alert_cb)(int severity);

typedef void (*probe_unused_cb)(int x);

// label:Set alert.
// icon:bell.
void probe_set_alert(
    // doc:Handler to call.
    // connection:mandatory.
    probe_alert_cb cb
);
`

func cbParse(t *testing.T, src string) *BlackBoxDef {
	t.Helper()
	def, err := ParseC([]byte(src), DefaultParserLimits())
	if def == nil {
		t.Fatalf("ParseC: def=nil err=%v", err)
	}
	return def
}

func TestParseC_CallbackType_TriggerAndIdentity(t *testing.T) {
	def := cbParse(t, cbTypeSrc)
	byName := map[string]CallbackTypeDef{}
	for _, ct := range def.CallbackTypes {
		byName[ct.Name] = ct
	}
	used, ok := byName["probe_alert_cb"]
	if !ok || !used.UsedAsParameter {
		t.Fatalf("consumed typedef must trigger the card: %+v", byName)
	}
	if orphan, ok := byName["probe_unused_cb"]; !ok || orphan.UsedAsParameter {
		t.Errorf("orphan contract must stay listed but NOT surface: %+v", orphan)
	}

	inc := ComputeIncomplete(def)
	if !containsStr(inc, "callbacktype.probe_alert_cb") {
		t.Errorf("unlabeled surfaced type must be incomplete; got %v", inc)
	}
	if containsStr(inc, "callbacktype.probe_unused_cb") {
		t.Errorf("orphan must not be incomplete (no card to complete); got %v", inc)
	}
}

func TestRewriteC_CallbackType_RoundTrip(t *testing.T) {
	args, _ := json.Marshal(map[string]string{
		"label": "Alert handler", "icon": "bell", "comment": "Fired on threshold.",
	})
	out, err := RewriteC(cbTypeSrc, []WizardEdit{{
		Path: "callbacktype.probe_alert_cb",
		Op:   OpSetStructDirectives,
		Args: args,
	}})
	if err != nil {
		t.Fatalf("RewriteC: %v", err)
	}
	if !strings.Contains(out, "label:Alert handler.") || !strings.Contains(out, "icon:bell.") {
		t.Fatalf("directives must land above the typedef:\n%s", out)
	}

	def := cbParse(t, out)
	for _, ct := range def.CallbackTypes {
		if ct.Name == "probe_alert_cb" {
			if ct.Label != "Alert handler" || ct.Icon != "bell" {
				t.Fatalf("round trip lost identity: %+v", ct)
			}
		}
	}
	if containsStr(ComputeIncomplete(def), "callbacktype.probe_alert_cb") {
		t.Errorf("labeled card must be complete")
	}

	// Blank payload clears the block → incomplete again.
	blank, _ := json.Marshal(map[string]string{})
	cleared, err := RewriteC(out, []WizardEdit{{
		Path: "callbacktype.probe_alert_cb",
		Op:   OpSetStructDirectives,
		Args: blank,
	}})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if containsStr(ComputeIncomplete(cbParse(t, cleared)), "callbacktype.probe_alert_cb") == false {
		t.Errorf("cleared card must be incomplete again")
	}
}

func TestRewriteC_CallbackType_UnknownName(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"label": "x"})
	if _, err := RewriteC(cbTypeSrc, []WizardEdit{{
		Path: "callbacktype.nope",
		Op:   OpSetStructDirectives,
		Args: args,
	}}); err == nil || !strings.Contains(err.Error(), `"nope" not found`) {
		t.Fatalf("unknown name must be a clean error; got %v", err)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
