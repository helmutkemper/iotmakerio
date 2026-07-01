// /ide/devices/compFlow/statementCase_caseRestore.go
package compFlow

// CaseRestoreEntry carries one case's persisted data across the import
// boundary, with its child IDs already remapped to the POST-IMPORT device IDs
// by the caller. It is the N-way analogue of the (selectedBranch, trueIDs,
// falseIDs) triple the if/else restore passes; with N cases an explicit struct
// is clearer than parallel slices, which is why this package exports a type the
// workspace can build (the if/else restore needed only primitives).
//
// Português: Carrega os dados de um case através da fronteira de import, com os
// IDs dos filhos já remapeados para os IDs pós-import. É o análogo N-vias da
// tripla (selectedBranch, trueIDs, falseIDs) do if/else.
type CaseRestoreEntry struct {
	ID    string
	Label string
	// MatchKind mirrors caseEntry.matchKind ("is"/"isAnyOf"/"between"/"gt"/
	// "lt"/"gte"/"lte"). It may arrive empty from a scene saved before the
	// field existed; RestoreCaseState backfills it from the value count so the
	// in-memory model is always explicit.
	MatchKind string
	Values    []string
	IDs       []string
	IsDefault bool
}

// RestoreCaseState re-applies the persisted case membership (which child
// belongs to which case), the selector type, the default case and the selected
// case after a scene IMPORT. The child IDs inside each entry are the
// POST-IMPORT (remapped) device IDs: the workspace rebuilds devices with fresh
// IDs and an oldID→newID map, and the saved per-case ids (which reference the
// OLD, saved IDs) are translated through that map by the caller before reaching
// here.
//
// Why this exists: case membership is written by GetProperties but cannot be
// restored through ApplyProperties — StatementCase is not scene.Inspectable, so
// ApplyProperties is never invoked on import; and even if it were, its
// map[string]string signature carries neither the []string members nor the
// import's ID remapping. Without this restore, assignNewChildren finds every
// case empty and assigns ALL contained children to the selected case — so every
// case's devices pile into one and applyCaseVisibility has nothing to hide (the
// exact if/else "else devices show up in the if" load bug, generalised).
// Calling this before the post-import NotifyChange makes the assignNewChildren
// it triggers a no-op (every child already assigned) and lets
// applyCaseVisibility hide the inactive cases.
//
// Português: Reaplica a associação de cases, o tipo do selector, o default e o
// case selecionado após IMPORTAR uma cena. Os IDs já chegam remapeados.
// Necessário porque a associação é salva em GetProperties mas não trafega pelo
// ApplyProperties (StatementCase não é scene.Inspectable; map[string]string não
// leva arrays nem o remapeamento). Chamado antes do NotifyChange pós-import.
func (e *StatementCase) RestoreCaseState(selectorType, selectedCase, defaultCaseID string, cases []CaseRestoreEntry) {
	if selectorType != "" {
		e.selectorType = selectorType
	}

	if len(cases) > 0 {
		rebuilt := make([]caseEntry, 0, len(cases))
		for _, c := range cases {
			// Backfill an empty matchKind the same way the codegen's
			// extractCases does, so a scene saved before matchKind existed
			// restores into an explicit kind rather than "".
			mk := c.MatchKind
			if mk == "" {
				if len(c.Values) > 1 {
					mk = "isAnyOf"
				} else {
					mk = "is"
				}
			}
			rebuilt = append(rebuilt, caseEntry{
				id:        c.ID,
				label:     c.Label,
				matchKind: mk,
				values:    append([]string(nil), c.Values...),
				ids:       append([]string(nil), c.IDs...),
				isDefault: c.IsDefault,
			})
		}
		e.cases = rebuilt
	}

	e.defaultCaseID = defaultCaseID

	if selectedCase != "" {
		e.selectedCase = selectedCase
	}
	if e.selectedCase == "" && len(e.cases) > 0 {
		e.selectedCase = e.cases[0].id
	}

	if e.ornamentDraw != nil {
		e.ornamentDraw.SetSelectorType(e.selectorType)
		e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
	}

	// Hide the inactive cases right away so the import does not leave every
	// case overlapping. The NotifyChange that follows the import
	// (assignNewChildren + applyCaseVisibility) is then idempotent.
	e.applyCaseVisibility()
}
