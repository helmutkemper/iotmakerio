// server/store/project_variables.go — CRUD for user-declared project variables
// (the GetVar/SetVar device family). See db_project_variables.go for the schema
// and storage rationale.
//
// The name is special: it becomes the source-level identifier AND the IR
// register at code generation (the codegen emitter uses it verbatim — see
// emitVariableDecls / resolveInput2 in server/codegen/ir/emit.go). So a name
// that is not a legal identifier, or that collides with a target-language
// keyword, would produce uncompilable output. validateVariableName rejects
// both here, at the only write path, so codegen can trust every stored name.
//
// Português: CRUD das variáveis de projeto declaradas pelo usuário (família
// GetVar/SetVar). O nome vira o identificador no código E o registrador no IR
// no codegen (usado verbatim pelo emitter), então um nome que não seja um
// identificador legal — ou que colida com palavra-chave do alvo — geraria
// código que não compila. validateVariableName rejeita os dois aqui, no único
// caminho de escrita, para o codegen confiar em todo nome armazenado.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	cryptoauth "server/auth"
)

// ─── Errors ───────────────────────────────────────────────────────────────────

var (
	// ErrProjectVariableNotFound is returned when no matching variable exists.
	ErrProjectVariableNotFound = errors.New("project variable not found")

	// ErrProjectVariableExists is returned when a variable with the same name
	// already exists in the project (UNIQUE(project_id, name)).
	ErrProjectVariableExists = errors.New("project variable name already exists")

	// ErrInvalidVariableName is returned when the name is not a legal Go/C
	// identifier or collides with a reserved word.
	ErrInvalidVariableName = errors.New("invalid project variable name")

	// ErrInvalidVariableType is returned when the type is not one of the
	// supported abstract IR types.
	ErrInvalidVariableType = errors.New("invalid project variable type")
)

// ─── Model ────────────────────────────────────────────────────────────────────

// ProjectVariable is a named, typed value declared for a project. Name is the
// codegen identifier/register; Type is the abstract IR type.
type ProjectVariable struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`       // "int" | "float" | "string"
	CreatedAt int64  `json:"created_at"` // Unix seconds
}

// ─── Validation ───────────────────────────────────────────────────────────────

// variableNameRe matches a legal identifier shared by Go and C99: a letter or
// underscore followed by letters, digits or underscores. ASCII only — the IDE
// authors identifiers, not arbitrary Unicode, and ASCII keeps the generated C
// portable.
var variableNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedVariableNames are identifiers that are valid by shape but would
// collide with a keyword (or a predeclared Go type) in a target language and
// produce uncompilable code. The set is the union of Go keywords, the Go
// predeclared type/value names the generated identifier must not shadow, and
// the C99 keywords not already covered. Kept here (not a schema CHECK) so it
// travels with the codegen contract and can grow without a migration.
//
// Português: Identificadores válidos na forma, mas que colidiriam com uma
// palavra-chave (ou tipo predeclarado do Go) no alvo e gerariam código que não
// compila. União de palavras-chave do Go, nomes predeclarados do Go e
// palavras-chave do C99 ainda não cobertas.
var reservedVariableNames = map[string]bool{
	// Go keywords
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	// Go predeclared types/values the generated identifier must not shadow
	"bool": true, "byte": true, "int": true, "int8": true, "int16": true,
	"int32": true, "int64": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "uintptr": true, "float32": true,
	"float64": true, "complex64": true, "complex128": true, "string": true,
	"rune": true, "error": true, "true": true, "false": true, "nil": true,
	"iota": true, "len": true, "cap": true, "make": true, "new": true,
	// C99 keywords not already covered above
	"auto": true, "char": true, "do": true, "double": true, "enum": true,
	"extern": true, "float": true, "long": true, "register": true, "short": true,
	"signed": true, "sizeof": true, "static": true, "typedef": true, "union": true,
	"unsigned": true, "void": true, "volatile": true, "while": true,
}

// validateVariableName enforces a legal, non-reserved identifier.
func validateVariableName(name string) error {
	if !variableNameRe.MatchString(name) || reservedVariableNames[name] {
		return ErrInvalidVariableName
	}
	return nil
}

// validateVariableType enforces one of the supported abstract IR types.
func validateVariableType(t string) error {
	switch t {
	case "int", "float", "string":
		return nil
	}
	return ErrInvalidVariableType
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

// CreateProjectVariable declares a new variable for a project. The name is
// validated as a legal, non-reserved identifier and the type as a supported IR
// type. A pre-check returns ErrProjectVariableExists for a friendly duplicate
// message; the UNIQUE(project_id, name) constraint is the backstop against a
// concurrent insert in the (single-user-per-project) race window.
//
// Português: Declara uma nova variável para um projeto. Nome validado como
// identificador legal e não-reservado; tipo como um tipo IR suportado. Um
// pre-check devolve ErrProjectVariableExists (mensagem amigável); a constraint
// UNIQUE é o backstop contra inserção concorrente na janela de corrida.
func CreateProjectVariable(projectID, name, varType string) (*ProjectVariable, error) {
	if projectID == "" {
		return nil, fmt.Errorf("store: create project variable: empty projectID")
	}
	if err := validateVariableName(name); err != nil {
		return nil, err
	}
	if err := validateVariableType(varType); err != nil {
		return nil, err
	}

	// Friendly duplicate check (the UNIQUE constraint is the backstop).
	var dummy int
	switch err := DB.QueryRow(
		`SELECT 1 FROM project_variables WHERE project_id = ? AND name = ?`,
		projectID, name,
	).Scan(&dummy); {
	case err == nil:
		return nil, ErrProjectVariableExists
	case errors.Is(err, sql.ErrNoRows):
		// not a duplicate — proceed
	default:
		return nil, fmt.Errorf("store: check project variable: %w", err)
	}

	v := &ProjectVariable{
		ID:        cryptoauth.MustNewID(),
		ProjectID: projectID,
		Name:      name,
		Type:      varType,
		CreatedAt: time.Now().Unix(),
	}
	if _, err := DB.Exec(`
		INSERT INTO project_variables (id, project_id, name, type, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		v.ID, v.ProjectID, v.Name, v.Type, v.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("store: create project variable: %w", err)
	}
	return v, nil
}

// ListProjectVariables returns every variable declared for a project, ordered
// by name for a stable UI listing and a deterministic codegen declaration
// order.
//
// Português: Retorna todas as variáveis de um projeto, ordenadas por nome para
// listagem estável na UI e ordem de declaração determinística no codegen.
func ListProjectVariables(projectID string) ([]ProjectVariable, error) {
	rows, err := DB.Query(`
		SELECT id, project_id, name, type, created_at
		FROM project_variables
		WHERE project_id = ?
		ORDER BY name ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list project variables: %w", err)
	}
	defer rows.Close()

	var vars []ProjectVariable
	for rows.Next() {
		var v ProjectVariable
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Type, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan project variable: %w", err)
		}
		vars = append(vars, v)
	}
	return vars, rows.Err()
}

// DeleteProjectVariable removes a variable by id, scoped to its project so a
// caller cannot delete another project's variable by guessing an id. Returns
// ErrProjectVariableNotFound when nothing matched.
//
// NOTE (v1 — enforced upstream): a variable still referenced by a GetVar/SetVar
// device must not be deletable. That "in use" check needs the scene (which
// lives in the IDE, not the store), so it is enforced at the device/handler
// layer in a later slice; this function is the unconditional storage primitive.
//
// Português: Remove uma variável por id, restrita ao projeto (não dá para
// apagar variável de outro projeto adivinhando id). Devolve
// ErrProjectVariableNotFound se nada casou. NOTA (v1): variável ainda
// referenciada por um device GetVar/SetVar não pode ser apagada; essa checagem
// de "em uso" precisa da cena (que vive na IDE) e é feita na camada de
// device/handler numa fatia posterior — aqui é o primitivo de storage puro.
func DeleteProjectVariable(projectID, id string) error {
	result, err := DB.Exec(
		`DELETE FROM project_variables WHERE id = ? AND project_id = ?`,
		id, projectID,
	)
	if err != nil {
		return fmt.Errorf("store: delete project variable: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrProjectVariableNotFound
	}
	return nil
}
