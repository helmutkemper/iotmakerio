package compBlackBox

// godocMarkdown.go — Auto-generates a Markdown help page from Go doc comments.
//
// English:
//
//	When a specialist writes a black-box component without /* */ manual pages,
//	the IDE still displays a Help tab using the doc comments already present
//	in the source file. This is "godoc as documentation" — the same information
//	that `go doc` would show, formatted as Markdown for the Inspect panel.
//
//	The generated page has three sections:
//	  1. Component description (package doc comment, if any)
//	  2. Method description (Init or Run doc comment, if any)
//	  3. No content → empty string returned, Help tab is suppressed
//
//	This is a fallback only. Explicit /* */ manual pages (manualName: tag)
//	always take precedence and are richer — they can include tables, code
//	examples, and wiring diagrams. The auto-generated page exists so the
//	Help tab is useful from day one, without requiring the specialist to
//	write separate documentation.
//
// Português:
//
//	Gera uma página Markdown a partir dos comentários Go já existentes no
//	componente. É um fallback — páginas /* */ explícitas têm prioridade.

import "strings"

// buildGodocMarkdown assembles a Markdown string from the component doc and
// the method doc comment. Returns "" when both are empty (Help tab suppressed).
//
// Parameters:
//   - componentName: the struct name, used as the page heading
//   - componentDoc:  package-level doc comment (may be empty)
//   - methodName:    "Init" or "Run"
//   - methodDoc:     method doc comment with IDS Params/Returns sections (may be empty)
func buildGodocMarkdown(componentName, componentDoc, methodName, methodDoc string) string {
	componentDoc = strings.TrimSpace(componentDoc)
	methodDoc = strings.TrimSpace(methodDoc)

	// Nothing to show — suppress the Help tab entirely.
	if componentDoc == "" && methodDoc == "" {
		return ""
	}

	var sb strings.Builder

	// ── Component heading and description ────────────────────────────────
	sb.WriteString("# ")
	sb.WriteString(componentName)
	sb.WriteString("\n\n")

	if componentDoc != "" {
		sb.WriteString(componentDoc)
		sb.WriteString("\n\n")
	}

	// ── Method section ────────────────────────────────────────────────────
	if methodDoc != "" {
		sb.WriteString("---\n\n")
		sb.WriteString("## ")
		sb.WriteString(methodName)
		sb.WriteString("()\n\n")
		// The method doc is already plain text with IDS tags (connection:,
		// range:, unit: etc.). Render it verbatim inside a code block so the
		// IDS tags are visible and readable, matching what `go doc` shows.
		sb.WriteString("```\n")
		sb.WriteString(methodDoc)
		sb.WriteString("\n```\n")
	}

	return strings.TrimSpace(sb.String())
}
