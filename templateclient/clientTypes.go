// /ide/templateclient/clientTypes.go

// Package templateclient handles all template-related communication between
// the WASM IDE and the server.
//
// English:
//
//	Three responsibilities:
//	  1. clientTypes.go  — Lightweight mirrors of the server-side template types.
//	  2. loader.go       — Fetch available templates from the server at startup.
//	  3. generator.go    — Send a generate request and trigger a browser download.
//
//	Template devices are placed on the canvas exactly like black-box devices —
//	they use the same StatementBlackBoxInit / StatementBlackBoxMethod blocks.
//	The only difference is the "Generate ZIP" action that collects prop values
//	and calls the server's generate endpoint.
//
//	Ownership (Origin, IsOwn on TemplateMetaClient): the server stamps every
//	list item with its provenance so the IDE can decide what appears under
//	"My Items" without decoding the JWT. The string values are shared with
//	the black-box package — see blackbox.OriginOwn / OriginPublic. "curated"
//	does not apply to templates today.
//
// Português:
//
//	Três responsabilidades:
//	  1. clientTypes.go  — Espelhos leves dos tipos do servidor.
//	  2. loader.go       — Busca templates do servidor na inicialização.
//	  3. generator.go    — Envia config e aciona download no browser.
//
//	Devices de template são colocados no canvas igual a black-boxes normais.
//	A diferença é a ação "Generate ZIP" que envia os valores das props.
//
//	Ownership: o servidor carimba cada item da lista com a origem (own ou
//	public) para a IDE decidir o que aparece em "My Items" sem precisar
//	decodificar o JWT.
package templateclient

import (
	"github.com/helmutkemper/iotmakerio/blackbox"
)

// TemplateStatus constants mirror the server-side store constants.
// Used to filter out non-ready templates before building the menu.
const (
	TemplateStatusReady = "ready"
)

// TemplateManifestClient is the client-side manifest of a template package.
// It mirrors the server-side TemplateManifest but only includes the fields
// the WASM IDE needs.
type TemplateManifestClient struct {
	// Name is the human-readable template name shown in the IDE Templates menu.
	Name string `json:"name"`

	// Version is the semantic version string (e.g. "1.0.0").
	Version string `json:"version"`

	// Description is a short explanation of what the template generates.
	Description string `json:"description,omitempty"`

	// Vars maps template variable names to device prop paths.
	// Key: variable name used in output/ files as {{.VarName}}.
	// Value: dot path "DeviceName.FieldName" pointing to a prop-tagged field.
	Vars map[string]string `json:"vars"`
}

// TemplateMetaClient is the lightweight template record returned by the list
// endpoint. Does not include device definitions or output file manifest.
type TemplateMetaClient struct {
	// ID is the primary key of the template_packages row.
	ID string `json:"id"`

	// UserID is the ID of the specialist who uploaded this template.
	UserID string `json:"userId"`

	// Name is the template name (denormalized from manifest).
	Name string `json:"name"`

	// Version is the template version (denormalized from manifest).
	Version string `json:"version"`

	// Description is a short human-readable explanation.
	Description string `json:"description,omitempty"`

	// Visibility is "private" or "public".
	Visibility string `json:"visibility"`

	// Status is "pending", "ready", or "error".
	Status string `json:"status"`

	// MenuCategory is the name of the top-level IDE menu category chosen by
	// the specialist (e.g. "Web Servers"). Empty means "Other".
	MenuCategory string `json:"menuCategory,omitempty"`

	// MenuSubcategory is the optional submenu within MenuCategory.
	// Empty means the template appears directly under MenuCategory.
	MenuSubcategory string `json:"menuSubcategory,omitempty"`

	// Origin identifies how this template reached the client:
	//   - blackbox.OriginOwn    → caller is the author.
	//   - blackbox.OriginPublic → authored by another specialist, visible
	//                             in the main Templates menu but never in
	//                             "My Items".
	//
	// Populated by the server in /api/v1/templates responses. Omitempty is
	// intentional: zero value means "unknown provenance", the safe default
	// for any future list endpoint that forgets to populate it.
	//
	// blackbox.OriginCurated does NOT apply to templates — templates are
	// never embedded in admin-curated menu sections today.
	Origin string `json:"origin,omitempty"`

	// IsOwn is the boolean shortcut for Origin == blackbox.OriginOwn. See
	// BlackBoxDefClient.IsOwn for the rationale behind having both fields.
	IsOwn bool `json:"isOwn,omitempty"`
}

// OutputFileMetaClient describes one file in the template's output/ folder.
// Used to understand which vars each file uses.
type OutputFileMetaClient struct {
	// RelPath is the path relative to output/ (e.g. "public/index.html").
	RelPath string `json:"relPath"`

	// IsText indicates whether this file is processed as a Go text/template.
	IsText bool `json:"isText"`

	// VarsUsed lists the variable names referenced in this file.
	VarsUsed []string `json:"varsUsed,omitempty"`
}

// TemplateDefClient is the full parsed definition of a template package.
// Mirrors the server-side templatepack.TemplatePackageDef.
//
// Devices are reused from the blackbox package because the JSON format is
// identical — the server serializes *codegen/blackbox.BlackBoxDef with the
// same field names that BlackBoxDefClient expects.
type TemplateDefClient struct {
	// Manifest is the parsed template.json content.
	Manifest TemplateManifestClient `json:"manifest"`

	// Devices are the parsed device definitions.
	// Each device appears as visual blocks in the IDE Hardware menu (via
	// the Templates submenu) with full wire support.
	Devices []*blackbox.BlackBoxDefClient `json:"devices"`

	// OutputFiles is the manifest of all files found in output/.
	OutputFiles []OutputFileMetaClient `json:"outputFiles"`

	// Help is the template-level help payload built from markdown files at
	// the root of the GitHub repository (readme.md, init.en.md, etc.).
	// It follows the same convention as standalone device help:
	//   readme.md        → overview tab shown in the IDE menu
	//   init.en.md       → help tabs for the Init block
	//   run.en.md        → help tabs for the Run block
	//   method.N.lang.md → numbered tabs per method per language
	Help blackbox.DeviceHelpClient `json:"help,omitempty"`
}

// TemplateFullClient combines the template metadata with its full definition.
// This is the type returned by LoadAllTemplates() and used by the IDE at
// menu-build time and at generate time.
type TemplateFullClient struct {
	// Meta is the lightweight template record, including ownership markers.
	Meta TemplateMetaClient

	// Def is the full parsed definition (devices + manifest + output files).
	// Nil if the template is not ready or the def failed to load.
	Def *TemplateDefClient

	// VarDefaults is a pre-computed map of variable name → default value.
	// Built from Def.Devices by matching Manifest.Vars paths to prop defaults.
	// Used by GenerateWithDefaults() when the canvas has no configured devices.
	VarDefaults map[string]string
}

// VarDefault returns the default value for the given variable name, or "".
// Safe to call on a nil receiver.
func (t *TemplateFullClient) VarDefault(varName string) string {
	if t == nil || t.VarDefaults == nil {
		return ""
	}
	return t.VarDefaults[varName]
}
