// ide/stageWorkspace/zipdownload.go — utility to package the codegen
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// multi-file output and offer it to the maker as a download.
//
// The C99 backend (and any future multi-file backend) returns a
// map[string]string keyed by filename. The maker reading the
// generated source in the overlay needs a way to take the whole
// pack home — copying each tab manually defeats the point of
// emitting more than one file.
//
// We package the map into a ZIP on the WASM side rather than ask
// the server to do it: the client already has every byte, so a
// round-trip would only add latency and a new endpoint. archive/
// zip is in the Go standard library and compiles to WASM with
// zero CGO, so the only cost is the in-memory buffer.
//
// The download itself uses the standard browser pattern:
//
//	bytes → Uint8Array → Blob → ObjectURL → invisible <a download>
//	→ programmatic click → revokeObjectURL.
//
// js.CopyBytesToJS is the bridge that copies the []byte into the
// JS-side typed array without a per-byte ferry crossing.
//
// Português: Empacota a saída multi-arquivo do codegen em um ZIP
// e dispara o download via Blob URL. archive/zip da stdlib do Go
// roda em WASM sem CGO; o cliente já tem os bytes, então não
// precisamos pedir nada ao servidor.

package stageWorkspace

import (
	"archive/zip"
	"bytes"
	"log"
	"syscall/js"
)

// downloadFilesAsZip packages files into an in-memory ZIP and
// triggers a browser download with the given filename. Returns
// an error when the ZIP cannot be built; the download itself is
// fire-and-forget after the click — the browser owns the rest.
//
// The filename should include the .zip extension and any prefix
// the caller wants (e.g. "iotmaker-c99.zip"). Filenames inside
// the archive are taken verbatim from the map keys.
func downloadFilesAsZip(files map[string]string, filename string) error {
	if len(files) == 0 {
		// Nothing to package — treat as a no-op rather than emit
		// an empty zip. A caller that hit this branch probably
		// had upstream filtering that pruned everything; bubbling
		// "empty input" as an error helps surface that bug.
		return nil
	}

	// Build the ZIP into a memory buffer. archive/zip handles the
	// central directory and per-entry metadata for us; we only
	// need to write each file's contents.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}

	data := buf.Bytes()
	log.Printf("[Codegen:zip] packaged %d files, %d bytes total → %s",
		len(files), len(data), filename)

	// Ferry the bytes into a JS Uint8Array. CopyBytesToJS is the
	// single-call bridge that avoids the per-byte cost of a Go
	// loop crossing the WASM/JS boundary.
	jsArr := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(jsArr, data)

	// Wrap the array in a Blob with the correct MIME type so the
	// browser knows to offer the standard "save file" UI rather
	// than try to render the bytes inline.
	blobOpts := js.Global().Get("Object").New()
	blobOpts.Set("type", "application/zip")
	blob := js.Global().Get("Blob").New(
		[]any{jsArr},
		blobOpts,
	)

	// Create an ObjectURL pointing at the blob. The URL is valid
	// for the lifetime of the document or until we revoke it
	// (which we do right after the click — the browser keeps the
	// downloaded copy regardless).
	url := js.Global().Get("URL").Call("createObjectURL", blob)

	// Create an invisible <a download> and click it programmatically.
	// This is the canonical "save bytes to disk from JS" pattern;
	// some Safari versions need the element to be in the DOM, so
	// we attach to body before clicking and remove right after.
	doc := js.Global().Get("document")
	a := doc.Call("createElement", "a")
	a.Set("href", url)
	a.Set("download", filename)
	a.Get("style").Set("display", "none")
	doc.Get("body").Call("appendChild", a)
	a.Call("click")
	doc.Get("body").Call("removeChild", a)

	// Revoke the URL so the blob can be garbage-collected once
	// the download finishes. The browser has already grabbed
	// the bytes for the file save by this point.
	js.Global().Get("URL").Call("revokeObjectURL", url)
	return nil
}
