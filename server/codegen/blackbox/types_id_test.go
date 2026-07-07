// server/codegen/blackbox/types_id_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBlackBoxDefID_SurvivesJSONRoundTrip pins the transport property the
// multi-file C output depends on: BlackBoxDef.ID must survive the JSON hop
// between the codegen submit handler and the Asynq worker (the handler
// marshals map[string]*BlackBoxDef into CodegenPayload.BlackBoxDefs; the
// worker unmarshals it back — see server/cmd/worker/main.go). If the field
// ever loses its json tag, the worker receives defs with an empty ID, the
// emitter silently falls back to the single-file inline path, and the folder
// isolation quietly disappears. This test makes that failure loud.
//
// Português: Garante que o ID sobrevive ao trânsito JSON handler→worker
// (payload da task Asynq). Sem a tag, o worker recebe defs com ID vazio, o
// emitter cai no caminho inline de arquivo único e o isolamento por pasta
// some em silêncio. Este teste transforma essa falha em barulho.
func TestBlackBoxDefID_SurvivesJSONRoundTrip(t *testing.T) {
	defs := map[string]*BlackBoxDef{
		"print_int": {ID: "3f9a2b1c", CodeID: "47", RawSource: "void print_int(int v) {}"},
	}

	blob, err := json.Marshal(defs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back map[string]*BlackBoxDef
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := back["print_int"]; got == nil || got.ID != "3f9a2b1c" {
		t.Fatalf("ID lost in transit: got %+v", back["print_int"])
	}
	// CodeID rides the same hop for the same reason: the worker's emitter
	// spells every generated name from it (CodeIdent) — losing it would not
	// break the build, but every export would silently degrade to the long
	// full-id names.
	if got := back["print_int"]; got.CodeID != "47" {
		t.Fatalf("CodeID lost in transit: got %+v", got)
	}
}

// TestBlackBoxDefCodeIdent pins the spelling choice both consumers (CSurface
// and the C emitter's file assembly) delegate to: the short code number when
// stitched, the full id otherwise, empty only for a def with no identity.
func TestBlackBoxDefCodeIdent(t *testing.T) {
	if got := (&BlackBoxDef{ID: "3f9a2b1c", CodeID: "47"}).CodeIdent(); got != "47" {
		t.Fatalf("stitched: want %q, got %q", "47", got)
	}
	if got := (&BlackBoxDef{ID: "3f9a2b1c"}).CodeIdent(); got != "3f9a2b1c" {
		t.Fatalf("fallback: want the full id, got %q", got)
	}
	if got := (&BlackBoxDef{}).CodeIdent(); got != "" {
		t.Fatalf("no identity: want empty, got %q", got)
	}
}

// TestBlackBoxDefID_OmittedWhenEmpty pins the storage property: at parse time
// the def has no ID (the parser never sees the database), and omitempty must
// keep the serialised parsed_json blob free of an "id" key. The row's id
// column is the single source of identity — the loader stitches it in and
// overwrites whatever a cached blob carries (see store.LoadBlackBoxDefsForScene).
// A blob that serialised `"id":""` would be harmless today, but a blob that
// serialised a NON-empty id would look authoritative to a future reader; this
// test keeps the blob honest at the source.
//
// Português: No parse o def não tem ID, e o omitempty mantém o parsed_json sem
// a chave "id". A identidade mora na coluna do banco; o loader costura e
// sobrescreve o que o blob trouxer. Este teste mantém o blob honesto na origem.
func TestBlackBoxDefID_OmittedWhenEmpty(t *testing.T) {
	blob, err := json.Marshal(&BlackBoxDef{Name: "APDS9960"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), `"id"`) {
		t.Fatalf("empty ID must be omitted from parsed_json, got: %s", blob)
	}
}
