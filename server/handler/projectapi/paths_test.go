// server/handler/projectapi/paths_test.go — Tests for projectBasePath,
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// the helper that computes the on-disk root of a project's file tree.
//
// Why this file is codegen-relevant:
//
//	The codegen pipeline emits Go source that the IDE saves through
//	handleSaveCodeVersion. That handler writes the resulting file under
//	{projectBasePath}/code/{filename}.go. Pinning the path layout here
//	keeps the static-serving URL pattern, the disk layout, and the
//	"rebuild from saved code" path all in lockstep.
//
// Scope:
//
//	Pure path math only — no filesystem, no DB. Verifies that the
//	function joins its inputs in the expected order and that
//	store.ProjectTypeSlug is applied to the projectType segment.
package projectapi

import (
	"path/filepath"
	"testing"

	"server/config"
	"server/store"
)

func TestProjectBasePath_layoutAndSlug(t *testing.T) {
	cfg := &config.Config{UserFilesDir: "public/static"}

	// custom_device → "customDevice" via store.ProjectTypeSlug.
	got := projectBasePath(cfg, "user-1", store.ProjectTypeCustomDevice, "proj-42")
	want := filepath.Join("public/static", "user-1", "project", "customDevice", "proj-42")
	if got != want {
		t.Errorf("custom_device: got %q, want %q", got, want)
	}
}

func TestProjectBasePath_unknownTypePassesThrough(t *testing.T) {
	// Unknown types fall through ProjectTypeSlug's default branch and
	// are used as-is. This pins the contract that an unknown type
	// doesn't mutate into the empty string or a default.
	cfg := &config.Config{UserFilesDir: "/var/data"}
	got := projectBasePath(cfg, "u", "futureType", "p")
	want := filepath.Join("/var/data", "u", "project", "futureType", "p")
	if got != want {
		t.Errorf("unknown type: got %q, want %q", got, want)
	}
}

func TestProjectBasePath_emptyConfigDir(t *testing.T) {
	// An empty UserFilesDir should produce a relative path rooted at
	// the user-id segment. We don't validate config elsewhere, so this
	// pins the behaviour rather than the policy: filepath.Join collapses
	// the empty leading element.
	cfg := &config.Config{UserFilesDir: ""}
	got := projectBasePath(cfg, "user-1", store.ProjectTypeCustomDevice, "proj-42")
	want := filepath.Join("user-1", "project", "customDevice", "proj-42")
	if got != want {
		t.Errorf("empty config dir: got %q, want %q", got, want)
	}
}
