// server/handler/blackboxapi/attach_help_files.go — Read a project's
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// markdown help files from the SQLite store and attach them to the
// in-memory BlackBoxDef before it is converted to the wire shape that
// the WASM IDE expects.
//
// Background:
//
// The publish-time worker at server/cmd/worker/main.go already builds
// device help by reading .md files out of a GitHub release ZIP. This
// file does the same thing for projects the user is *editing*:
//
//   - Files come from the project_help_files table (SQLite blobs).
//   - readme.<lang>.md, <method>.<lang>.md, <method>.<N>.<lang>.md
//     are recognised by the same grammar (bbparser.HelpFileRe and the
//     readme-prefix branch inside bbparser.BuildDeviceHelp).
//   - Image references in the markdown are rewritten to point at the
//     project's authenticated /files/help/<path> endpoint.
//
// Result: the moment the user creates "readme.en.md" in the wizard's
// File Manager and saves, the IDE sidebar's "My Items" panel shows
// it as the device's introduction — no publish step required.
//
// All names that match the `<method>` part are lowercased by
// BuildDeviceHelp; the toClientDef → menu pipeline already expects
// lowercase keys, so no extra normalisation is needed here.
package blackboxapi

import (
	"encoding/base64"
	"log"
	"path"
	"strings"

	bbparser "server/codegen/blackbox"
	"server/store"
)

// attachProjectHelpFiles loads markdown files from project_help_files
// for the given project UUID and writes the assembled DeviceHelp into
// def.Help (replacing whatever the parser emitted, which is always the
// zero value because BlackBoxDef parsing does not look at help files).
//
// Failure modes are deliberately non-fatal:
//
//   - Database error: log and return — the device still appears in the
//     sidebar with a blank readme, exactly as it did before this code
//     existed. Better than 500-ing the entire blackbox listing.
//   - File outside the recognised filename grammar: silently skipped
//     by BuildDeviceHelp. Lets the user store unrelated assets in
//     project_help_files (e.g. images-only projects) without forcing
//     them to filter on the client side.
//
// The function is intentionally not a method on def — it does I/O and
// the rest of the parser package is pure.
func attachProjectHelpFiles(def *bbparser.BlackBoxDef, projectID string) {
	if def == nil || projectID == "" {
		return
	}

	// List metadata for every help file in the project. We need the
	// full list to:
	//
	//   1. Build imageURLs (image MIME → public URL) so markdown image
	//      references can be rewritten before being sent to the WASM
	//      client. The IDE renders these URLs directly through
	//      <img src=...>, so they have to point somewhere the browser
	//      can fetch.
	//
	//   2. Decide which files are markdown — only those go into
	//      mdFiles to feed BuildDeviceHelp. Reading every blob to find
	//      out would be wasteful when the metadata already tells us
	//      the MIME type.
	metas, err := store.ListHelpFiles(projectID)
	if err != nil {
		log.Printf("[blackboxapi] attach help files: list project=%s: %v",
			projectID, err)
		return
	}

	// imageURLs maps the relative path inside the project (the same
	// shape RewriteImagePaths expects: "examples/foo.png" or "bar.svg")
	// to a value the WASM IDE's <img src="..."> can use directly.
	//
	// Why "data:" URLs and not "/api/v1/projects/<UUID>/files/help/..." ?
	//
	//   The help-files endpoint requires a Bearer token (Authorization
	//   header). When the markdown is rendered into the sidebar, marked.js
	//   produces <img src="..."> tags. The browser fetches images using
	//   only the cookie session; it does NOT replay the JavaScript-side
	//   Bearer header. The image request would arrive unauthenticated and
	//   the server would 401, leaving a broken image in the sidebar.
	//
	//   Inlining each image as a base64 "data:" URL sidesteps the auth
	//   problem entirely: the bytes travel inside the same JSON response
	//   that delivers the markdown, which is itself authenticated. The
	//   browser does not make a separate HTTP request for the image.
	//
	//   Cost: base64 inflates each image by ~33%, and every project's
	//   images travel inside every /api/v1/blackbox response. Today's
	//   per-project quota is 5 MB; in practice readmes use small PNGs
	//   (tens of KB). Acceptable. If the JSON ever grows uncomfortable,
	//   the next evolution is signed short-lived URLs or a client-side
	//   fetch-and-blob rewriter; both leave this function's surface
	//   unchanged because it still emits a string per image.
	//
	//   Note: SVGs are inlined as base64 too rather than as
	//   `data:image/svg+xml;utf8,...` because base64 is content-safe
	//   for any byte sequence (no XML escaping required).
	imageURLs := make(map[string]string, len(metas))
	for _, m := range metas {
		if !isImageMime(m.MimeType) {
			continue
		}
		hf, fetchErr := store.GetHelpFile(projectID, m.Path)
		if fetchErr != nil {
			log.Printf("[blackboxapi] attach help files: fetch image %s/%s: %v",
				projectID, m.Path, fetchErr)
			continue
		}
		imageURLs[m.Path] = "data:" + m.MimeType + ";base64," +
			base64.StdEncoding.EncodeToString(hf.Content)
	}

	// mdFiles maps filename → bytes for every markdown file in the
	// project. Keys are the basenames (e.g. "readme.en.md",
	// "Init.en.md") because BuildDeviceHelp parses the filename
	// grammar against the basename, not the full path. Files that
	// happen to live under examples/ (e.g. an example/instructions
	// markdown a future spec might allow) are ignored here because
	// today's grammar only recognises root-level help files.
	mdFiles := make(map[string][]byte, len(metas))
	for _, m := range metas {
		if !isMarkdownMime(m.MimeType) {
			continue
		}
		// The grammar only recognises root-level files
		// (`<method>.<lang>.md`, `readme.<lang>.md`); skip anything
		// that lives in a subdirectory.
		if path.Dir(m.Path) != "." {
			continue
		}
		hf, fetchErr := store.GetHelpFile(projectID, m.Path)
		if fetchErr != nil {
			log.Printf("[blackboxapi] attach help files: fetch %s/%s: %v",
				projectID, m.Path, fetchErr)
			continue
		}
		mdFiles[m.Path] = hf.Content
	}

	if len(mdFiles) == 0 {
		// No markdown files — leave def.Help at its zero value. This
		// is the common case for a freshly-created project that has
		// not yet been documented; the sidebar will simply show no
		// readme until the user adds one.
		//
		// Note: we still resolve def.Interactive below even when
		// there are no markdown files. The diagram is hosted on the
		// File Manager regardless of whether there is help text, and
		// the WASM Inspect panel can render the SVG by itself when
		// def.Interactive is non-empty even with empty help.
	} else {
		help, warns := bbparser.BuildDeviceHelp(mdFiles, imageURLs)
		for _, w := range warns {
			log.Printf("[blackboxapi] help warning project=%s: %s",
				projectID, w)
		}
		def.Help = help
	}

	// Resolve def.Interactive from bare stem to the project's stored URL.
	//
	// The parser sets def.Interactive to the raw value of the
	// `interactive:NAME.` directive — e.g. "rp2040". The WASM IDE expects
	// def.Interactive to be a *fetchable* URL: the renderer in
	// ui/overlay/overlay.go::fetchAndInjectSVG calls fetch(def.Interactive)
	// directly. A bare stem like "rp2040" cannot be fetched and produces
	// no <img src> match in activateInlineSVGs either, so the diagram
	// silently stays in readme mode.
	//
	// The publish-time worker does this resolution at
	// server/cmd/worker/main.go:514-535 against the GitHub release ZIP.
	// This function does the equivalent for *projects under live edit*:
	// look up "<stem>.svg" in the imageURLs map we just built from
	// project_help_files and replace the bare stem with that URL.
	//
	// Filename matching is case-insensitive because the Help files API
	// allows any casing (e.g. "RP2040.svg" stored, "interactive:rp2040."
	// declared, or vice-versa). Aligning with the spec's case-insensitive
	// role lookup keeps a single rule across the whole feature.
	//
	// If the SVG is not in the project's help files yet, the field is
	// cleared so the IDE's "DiagramURL" check (`if tab.DiagramURL != ""`)
	// short-circuits the whole highlight pipeline cleanly. Better than
	// leaving a stale stem that the renderer would silently fail on.
	//
	// Português: o parser deixa def.Interactive com o valor cru da
	// directive (ex: "rp2040"). O renderer WASM espera uma URL
	// buscável — então procuramos "<stem>.svg" no mapa de imagens do
	// projeto e substituímos. Se não houver SVG salvo, zeramos para
	// que o caminho do destaque seja desligado de forma limpa.
	if def.Interactive != "" {
		want := def.Interactive + ".svg"
		matched := ""
		for relPath, url := range imageURLs {
			if strings.EqualFold(relPath, want) {
				matched = url
				break
			}
		}
		if matched != "" {
			def.Interactive = matched
		} else {
			log.Printf(
				"[blackboxapi] project=%s: interactive:%q declared but %q "+
					"not found in project help files — diagram disabled",
				projectID, def.Interactive, want)
			def.Interactive = ""
		}
	}
}

// isMarkdownMime returns true for the canonical text/markdown content
// type stored by handlePutHelpFile (and only that). Tightening the
// check to a single canonical value is safer than substring matching:
// a future addition like "text/markdown-extra" must not be mistaken
// for a standard help file without an explicit decision here.
func isMarkdownMime(mime string) bool {
	return mime == "text/markdown; charset=utf-8" ||
		mime == "text/markdown"
}

// isImageMime returns true for any image type accepted by the help-
// files endpoint's MIME whitelist (helpFileExtMime in help_files.go).
// Used to populate imageURLs without enumerating every type — if a new
// image type is added to the whitelist later, this function still
// classifies it correctly without an edit.
func isImageMime(mime string) bool {
	return len(mime) >= 6 && mime[:6] == "image/"
}
