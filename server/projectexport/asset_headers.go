// server/projectexport/asset_headers.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package projectexport

// asset_headers.go — Thin forwarders. The implementation moved to
// codegen/blackbox/asset_header.go (the pure leaf both this package
// and the maker's C emitter can import — see the note there for the
// layering rationale). The projectexport API and its tests stay put.
//
// Português: Encaminhadores finos — a implementação mudou para
// codegen/blackbox/asset_header.go (folha pura que os dois
// consumidores importam). API e testes deste pacote permanecem.

import blackbox "server/codegen/blackbox"

// AssetHeaderPath forwards to blackbox.AssetHeaderPath.
func AssetHeaderPath(assetPath string) string { return blackbox.AssetHeaderPath(assetPath) }

// RenderAssetHeader forwards to blackbox.RenderAssetHeader.
func RenderAssetHeader(assetPath string, data []byte) []byte {
	return blackbox.RenderAssetHeader(assetPath, data)
}
