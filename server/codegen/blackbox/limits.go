// server/codegen/blackbox/limits.go — Parser complexity limits type.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// ParserLimits is defined in the codegen/blackbox package rather than in store
// to keep the parser free of any database dependency. The store package imports
// codegen/blackbox and returns a ParserLimits via store.GetParserLimits().
//
// Dependency graph (no cycles):
//
//	codegen/blackbox  →  (stdlib only)
//	store             →  codegen/blackbox
//	handler/*         →  store, codegen/blackbox
//	cmd/worker        →  store, codegen/blackbox
package blackbox

// ParserLimits controls the structural complexity caps for a single Parse call.
// All fields must be positive integers — zero or negative values cause the
// compiled-in defaults to be used for that field (via clamp).
//
// Obtain a populated value via:
//
//	limits := store.GetParserLimits(userID)   // live DB lookup — server code
//	limits := blackbox.DefaultParserLimits()  // compile-time defaults — tests
type ParserLimits struct {
	// MaxMethods is the maximum number of exported non-Init methods per device.
	// Exceeding this limit is a hard parse error — the component is rejected.
	MaxMethods int

	// MaxInputs is the maximum number of input ports (parameters) per method.
	// Excess inputs are truncated; a soft warning is added to ParseWarnings.
	MaxInputs int

	// MaxOutputs is the maximum number of output ports (return values) per
	// method. Excess outputs are truncated; a soft warning is added.
	MaxOutputs int

	// MaxProps is the maximum number of prop-tagged struct fields per device.
	// Excess props are truncated; a soft warning is added.
	MaxProps int
}

// Compiled-in fallback values — must match the seed values in
// store/db_parser_limits.go so behaviour is identical before and after the
// first server start.
const (
	compiledDefaultMaxMethods = 32
	compiledDefaultMaxInputs  = 16
	compiledDefaultMaxOutputs = 16
	compiledDefaultMaxProps   = 32
)

// DefaultParserLimits returns the compile-time default limits.
//
// Use in:
//   - Unit tests without a live database.
//   - Benchmarks and fuzz targets.
//   - Any context where a DB connection is unavailable.
//
// In production, use store.GetParserLimits(userID) so that admin-configured
// global limits and per-user overrides are respected.
func DefaultParserLimits() ParserLimits {
	return ParserLimits{
		MaxMethods: compiledDefaultMaxMethods,
		MaxInputs:  compiledDefaultMaxInputs,
		MaxOutputs: compiledDefaultMaxOutputs,
		MaxProps:   compiledDefaultMaxProps,
	}
}

// clamp returns v when v > 0, otherwise fallback.
// Prevents a misconfigured limit of 0 from rejecting every parse input.
func clamp(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
