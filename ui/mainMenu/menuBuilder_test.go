//go:build js && wasm
// +build js,wasm

// /ide/ui/mainMenu/menuBuilder_test.go — Unit tests for the own-index derivation.
//
// BUILD TAG: this file is gated by `//go:build js && wasm` at the top.
// The ui/mainMenu package imports syscall/js at package level (through
// menuBuilder.go and siblings), so the whole package compiles only under
// GOARCH=wasm GOOS=js. Without this tag, `go test ./...` on a host
// toolchain fails with "build constraints exclude all Go files in
// .../syscall/js". With the tag, the host toolchain SKIPS this file
// silently — matching the behaviour of the other WASM-only files in
// the package — and these tests are only discovered by a WASM-targeted
// invocation of `go test`.
//
// Purpose:
//
//	Exercises buildOwnBlackBoxIndex and buildOwnTemplateIndex — the two pure
//	helpers introduced in Phase 2 of the "My Items" refactor. These functions
//	are the structural defence against the bug originally documented in
//	/ide/docs/CLAUDE_KNOWN_ISSUES.md §2.2 ("My Items contaminated with
//	curated devices and public templates").
//
//	Testing the full buildMyItems() is impractical in Go's host test runner
//	because ui/mainMenu imports syscall/js at package level (through
//	menuBuilder.go and siblings). The package compiles only under
//	GOARCH=wasm GOOS=js. The tests below follow the same convention:
//	they run under WASM too.
//
// How to run:
//
//	The project's `make test` target runs host-side tests only. To run
//	these tests locally, use a WASM runner such as wasmbrowsertest or
//	node.js with the Go WASM shim.
//
//	Go 1.24 and newer (GOROOT/lib/wasm/):
//
//	    GOARCH=wasm GOOS=js go test ./ui/mainMenu/... \
//	        -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"
//
//	Go 1.23 and older (GOROOT/misc/wasm/):
//
//	    GOARCH=wasm GOOS=js go test ./ui/mainMenu/... \
//	        -exec="$(go env GOROOT)/misc/wasm/go_js_wasm_exec"
//
//	(The Go toolchain moved these support files from misc/ to lib/ in
//	Go 1.24 — see https://go.dev/wiki/WebAssembly for the current path.)
//
//	Requires Node.js on PATH. On macOS: `brew install node`.
//
//	If no runner is available, read these tests as executable documentation
//	of the ownership-filter contract. The invariants asserted here are the
//	same ones that Phase 1's manual smoke test #2 validated in the browser.
//
// Why these tests matter:
//
//	In three months someone may try to "simplify" the two buildOwn*Index
//	helpers by inlining them, or by replacing the IsOwn-only filter with a
//	more complex one. These tests pin the current contract and fail loudly
//	when any such change drifts from the original intent. Do not remove
//	them without a replacement that covers the same invariants.
package mainMenu

import (
	"testing"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/templateclient"
)

// ─── Black-box index ─────────────────────────────────────────────────────────

// TestBuildOwnBlackBoxIndex_Empty exercises the edge cases where no index
// should be produced. The return value must be nil (not an empty slice) so
// that len() == 0 is the single visibility check in buildMyItems.
func TestBuildOwnBlackBoxIndex_Empty(t *testing.T) {
	tests := []struct {
		name string
		in   []*blackbox.BlackBoxDefClient
	}{
		{"nil slice", nil},
		{"empty slice", []*blackbox.BlackBoxDefClient{}},
		{"all curated", []*blackbox.BlackBoxDefClient{
			{Name: "APDS9960", IsOwn: false, Origin: blackbox.OriginCurated},
			{Name: "RP2040_I2C", IsOwn: false, Origin: blackbox.OriginCurated},
		}},
		{"all public", []*blackbox.BlackBoxDefClient{
			{Name: "shared", IsOwn: false, Origin: blackbox.OriginPublic},
		}},
		{"mixed non-own only", []*blackbox.BlackBoxDefClient{
			{Name: "a", IsOwn: false, Origin: blackbox.OriginCurated},
			{Name: "b", IsOwn: false, Origin: blackbox.OriginPublic},
			{Name: "c", IsOwn: false, Origin: ""}, // unknown provenance
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOwnBlackBoxIndex(tc.in)
			if got != nil {
				t.Errorf("expected nil, got slice of length %d: %v", len(got), got)
			}
		})
	}
}

// TestBuildOwnBlackBoxIndex_FiltersOwn exercises the main positive case:
// among a mixed set, only the IsOwn entries reach the index, in input order.
//
// The order assertion matters because the hex menu presents items to the
// user in the order they appear in the list; swapping input order would
// be a visible UI regression.
func TestBuildOwnBlackBoxIndex_FiltersOwn(t *testing.T) {
	defs := []*blackbox.BlackBoxDefClient{
		{Name: "myDevice", IsOwn: true, Origin: blackbox.OriginOwn},
		{Name: "curated1", IsOwn: false, Origin: blackbox.OriginCurated},
		{Name: "anotherMine", IsOwn: true, Origin: blackbox.OriginOwn},
		{Name: "curated2", IsOwn: false, Origin: blackbox.OriginCurated},
		{Name: "thirdMine", IsOwn: true, Origin: blackbox.OriginOwn},
	}

	got := buildOwnBlackBoxIndex(defs)

	if len(got) != 3 {
		t.Fatalf("expected 3 own entries, got %d", len(got))
	}

	wantNames := []string{"myDevice", "anotherMine", "thirdMine"}
	for i, want := range wantNames {
		if got[i] == nil {
			t.Errorf("index[%d]: unexpected nil pointer", i)
			continue
		}
		if got[i].Name != want {
			t.Errorf("index[%d]: got name %q, want %q", i, got[i].Name, want)
		}
	}
}

// TestBuildOwnBlackBoxIndex_SkipsNil verifies that defensive nil pointers
// inside the input slice are skipped silently rather than panicking. This
// matters because extractEmbeddedDefs skips malformed entries by just
// continuing the loop — it cannot guarantee the caller's slice has no gaps
// at every call site in perpetuity.
func TestBuildOwnBlackBoxIndex_SkipsNil(t *testing.T) {
	defs := []*blackbox.BlackBoxDefClient{
		{Name: "a", IsOwn: true, Origin: blackbox.OriginOwn},
		nil,
		{Name: "b", IsOwn: true, Origin: blackbox.OriginOwn},
	}

	got := buildOwnBlackBoxIndex(defs)

	if len(got) != 2 {
		t.Fatalf("expected 2 entries (nil skipped), got %d", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("unexpected order: %s, %s", got[0].Name, got[1].Name)
	}
}

// TestSetBlackBoxDefs_RebuildResets is the regression test that pins the
// behaviour Kemper originally reported in CLAUDE_KNOWN_ISSUES.md §2.2:
// a user who logs out and logs back in as a different account must NOT see
// the previous user's devices under "My Items".
//
// In the new architecture, the own-index is derived from the current defs
// list at every call to SetBlackBoxDefs. Calling it a second time with a
// different caller's defs must replace the index entirely — no ghost
// entries from the first call can remain.
func TestSetBlackBoxDefs_RebuildResets(t *testing.T) {
	b := &MenuBuilder{}

	// First user, owning one device.
	first := []*blackbox.BlackBoxDefClient{
		{Name: "alice_device", IsOwn: true, Origin: blackbox.OriginOwn},
	}
	b.SetBlackBoxDefs(first)
	if len(b.ownBlackBoxIndex) != 1 || b.ownBlackBoxIndex[0].Name != "alice_device" {
		t.Fatalf("first call: unexpected index state: %v", b.ownBlackBoxIndex)
	}

	// Second user (different account), owning nothing of their own,
	// but the catalog still has curated content visible to them.
	second := []*blackbox.BlackBoxDefClient{
		{Name: "curated_device", IsOwn: false, Origin: blackbox.OriginCurated},
	}
	b.SetBlackBoxDefs(second)
	if b.ownBlackBoxIndex != nil {
		t.Errorf("second call: expected nil index for user with zero own devices, got %v",
			b.ownBlackBoxIndex)
	}

	// Third user (third account), owning one new device. Must see only
	// their own, never anything from the first two calls.
	third := []*blackbox.BlackBoxDefClient{
		{Name: "carol_device", IsOwn: true, Origin: blackbox.OriginOwn},
		{Name: "curated_device", IsOwn: false, Origin: blackbox.OriginCurated},
	}
	b.SetBlackBoxDefs(third)
	if len(b.ownBlackBoxIndex) != 1 {
		t.Fatalf("third call: expected 1 own entry, got %d", len(b.ownBlackBoxIndex))
	}
	if b.ownBlackBoxIndex[0].Name != "carol_device" {
		t.Errorf("third call: wrong entry — leak from a previous call? got %q",
			b.ownBlackBoxIndex[0].Name)
	}
}

// ─── Template index ──────────────────────────────────────────────────────────

// TestBuildOwnTemplateIndex_Empty mirrors the black-box empty cases.
// Kept as a separate test because TemplateMetaClient is a distinct struct
// and a rename/refactor on one side must not silently pass thanks to the
// other side's coverage.
func TestBuildOwnTemplateIndex_Empty(t *testing.T) {
	tests := []struct {
		name string
		in   []*templateclient.TemplateFullClient
	}{
		{"nil slice", nil},
		{"empty slice", []*templateclient.TemplateFullClient{}},
		{"all public", []*templateclient.TemplateFullClient{
			{Meta: templateclient.TemplateMetaClient{ID: "a", IsOwn: false, Origin: blackbox.OriginPublic}},
			{Meta: templateclient.TemplateMetaClient{ID: "b", IsOwn: false, Origin: blackbox.OriginPublic}},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOwnTemplateIndex(tc.in)
			if got != nil {
				t.Errorf("expected nil, got slice of length %d", len(got))
			}
		})
	}
}

// TestBuildOwnTemplateIndex_FiltersOwn is the template counterpart of
// TestBuildOwnBlackBoxIndex_FiltersOwn. See that test for the rationale.
func TestBuildOwnTemplateIndex_FiltersOwn(t *testing.T) {
	defs := []*templateclient.TemplateFullClient{
		{Meta: templateclient.TemplateMetaClient{ID: "mine1", IsOwn: true, Origin: blackbox.OriginOwn}},
		{Meta: templateclient.TemplateMetaClient{ID: "theirs", IsOwn: false, Origin: blackbox.OriginPublic}},
		{Meta: templateclient.TemplateMetaClient{ID: "mine2", IsOwn: true, Origin: blackbox.OriginOwn}},
	}

	got := buildOwnTemplateIndex(defs)

	if len(got) != 2 {
		t.Fatalf("expected 2 own entries, got %d", len(got))
	}
	if got[0].Meta.ID != "mine1" || got[1].Meta.ID != "mine2" {
		t.Errorf("unexpected order: %s, %s", got[0].Meta.ID, got[1].Meta.ID)
	}
}

// TestSetTemplateDefs_RebuildResets mirrors TestSetBlackBoxDefs_RebuildResets
// for the template flow. See that test's comment for the rationale.
func TestSetTemplateDefs_RebuildResets(t *testing.T) {
	b := &MenuBuilder{}

	b.SetTemplateDefs([]*templateclient.TemplateFullClient{
		{Meta: templateclient.TemplateMetaClient{ID: "alice_tpl", IsOwn: true, Origin: blackbox.OriginOwn}},
	})
	if len(b.ownTemplateIndex) != 1 {
		t.Fatalf("first call: expected 1 own entry, got %d", len(b.ownTemplateIndex))
	}

	// Second user with only public templates visible.
	b.SetTemplateDefs([]*templateclient.TemplateFullClient{
		{Meta: templateclient.TemplateMetaClient{ID: "shared_tpl", IsOwn: false, Origin: blackbox.OriginPublic}},
	})
	if b.ownTemplateIndex != nil {
		t.Errorf("second call: expected nil own index, got %v", b.ownTemplateIndex)
	}
}

// ─── The original regression that justifies this whole refactor ──────────────

// TestOriginalRegression_NewUserSeesNoMyItems pins the exact invariant from
// CLAUDE_KNOWN_ISSUES.md §2.2: a brand-new user with zero own devices AND
// zero own templates, facing a populated catalog of curated devices and
// public templates, must end up with two nil own-indexes. Once those are
// nil, buildMyItems() returns nil, and the "My Items" entry vanishes from
// the hex menu.
//
// This is the case Kemper validated manually as smoke test #2 in Phase 1.
// Encoding it here guarantees no future refactor can regress it silently.
func TestOriginalRegression_NewUserSeesNoMyItems(t *testing.T) {
	b := &MenuBuilder{}

	// Curated devices that the admin promoted into a section. New user
	// has zero own devices — their /api/v1/blackbox response is empty.
	// After Phase 2, everything the user can place on the canvas is in
	// a single slice; the ownership marker travels with each entry.
	b.SetBlackBoxDefs([]*blackbox.BlackBoxDefClient{
		{Name: "SparkfunTempSensor", IsOwn: false, Origin: blackbox.OriginCurated},
		{Name: "AdafruitServo", IsOwn: false, Origin: blackbox.OriginCurated},
	})

	// Public templates from other specialists. New user has zero own
	// templates — the list from the server is all IsOwn=false.
	b.SetTemplateDefs([]*templateclient.TemplateFullClient{
		{Meta: templateclient.TemplateMetaClient{ID: "shared", IsOwn: false, Origin: blackbox.OriginPublic}},
	})

	if b.ownBlackBoxIndex != nil {
		t.Errorf("new user regression: own-blackbox-index should be nil, got %d entries",
			len(b.ownBlackBoxIndex))
	}
	if b.ownTemplateIndex != nil {
		t.Errorf("new user regression: own-template-index should be nil, got %d entries",
			len(b.ownTemplateIndex))
	}

	// Equivalent assertion phrased from the buildMyItems perspective:
	// both indexes are empty, so the guard
	//   if len(b.ownBlackBoxIndex) == 0 && len(b.ownTemplateIndex) == 0 { return nil }
	// triggers, and "My Items" is hidden from the menu. If anyone ever
	// rewrites buildMyItems and forgets this guard, fix it; this test
	// cannot enforce that directly because the full function drags the
	// whole WASM-gated package into its call tree.
	if len(b.ownBlackBoxIndex) != 0 || len(b.ownTemplateIndex) != 0 {
		t.Errorf("both indexes must be empty for new user; got %d blackbox, %d template",
			len(b.ownBlackBoxIndex), len(b.ownTemplateIndex))
	}
}
