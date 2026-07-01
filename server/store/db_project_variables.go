// server/store/db_project_variables.go — Schema for user-declared project
// variables (the GetVar/SetVar device family).
//
// A project variable is a named, typed value a maker declares for a project
// (e.g. "counter" int, "label" string). GetVar/SetVar devices reference it by
// name on the stage; at code generation the name becomes the source-level
// identifier AND the IR register, declared once and zero-initialised. v1 keeps
// every variable global (program scope); arrays are deferred to v2.
//
// Storage rationale:
//   - project_id — FK to projects.id with ON DELETE CASCADE so deleting a
//     project sweeps its variables automatically. Mirrors
//     project_help_files (the canonical project-scoped table).
//   - name       — the identifier. UNIQUE(project_id, name) makes the name
//     unique within a project and is the lookup key. Validity (a
//     legal Go/C identifier, no reserved-word collision) is
//     enforced in the store layer (project_variables.go), NOT by a
//     schema CHECK, so the rule lives in one place and can evolve
//     without a migration.
//   - type       — abstract IR type: "int" | "float" | "string". The concrete
//     width is resolved per-target by the backend (the same way
//     ConstInt's "int" is), so the row stays target-agnostic.
//   - created_at — Unix seconds, set on insert. Used only for stable ordering;
//     variables are never mutated in place (delete + recreate).
//
// Português: Esquema das variáveis de projeto declaradas pelo usuário (família
// GetVar/SetVar). Uma variável é um valor nomeado e tipado que o maker declara
// para um projeto; os devices a referenciam por nome na stage e, no codegen, o
// nome vira o identificador no código E o registrador no IR, declarado uma vez
// e zero-init. v1 mantém toda variável global; arrays ficam para a v2.
//   - project_id — FK para projects.id com ON DELETE CASCADE (espelha
//     project_help_files).
//   - name       — o identificador. UNIQUE(project_id, name). A validade (ID
//     legal em Go/C, sem colisão com palavra reservada) é checada
//     na camada store, não por CHECK de schema.
//   - type       — tipo IR abstrato: "int" | "float" | "string". A largura
//     concreta é resolvida por alvo no backend.
//   - created_at — Unix segundos, só para ordenação estável.
package store

// projectVariablesMigrationStmts returns the DDL statements for the
// project-variables feature. Called by migrate() in db.go as part of the
// sequential migration chain. All statements are idempotent
// (CREATE TABLE / CREATE INDEX IF NOT EXISTS).
//
// Português: Retorna a DDL da feature de variáveis de projeto. Chamada por
// migrate() em db.go na cadeia sequencial de migração. Tudo idempotente.
func projectVariablesMigrationStmts() []string {
	return []string{

		// ── project_variables ─────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS project_variables (
			id          TEXT    NOT NULL,
			project_id  TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name        TEXT    NOT NULL,
			type        TEXT    NOT NULL,
			created_at  INTEGER NOT NULL,
			PRIMARY KEY (id),
			UNIQUE (project_id, name)
		);`,

		// Listing and the codegen-time load both filter by project_id, so this
		// index covers them with a single scan.
		`CREATE INDEX IF NOT EXISTS idx_project_variables_project
			ON project_variables (project_id);`,
	}
}
