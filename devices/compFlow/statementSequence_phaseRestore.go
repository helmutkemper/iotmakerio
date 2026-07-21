// /ide/devices/compFlow/statementSequence_phaseRestore.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// CaseRestoreEntry is DECLARED in statementCase_caseRestore.go (same
// package) — the Sequence twin REUSES it; redeclaring here broke the
// 2026-07-16 build (the type name carries no "StatementCase", so the
// rename pass left a duplicate). Português: Tipo declarado no restore do
// Case — o gêmeo REUSA; redeclarar quebrou o build.

// RestoreCaseState re-applies the persisted case membership (which child
// belongs to which case), the selector type, the default case and the selected
// case after a scene IMPORT. The child IDs inside each entry are the
// POST-IMPORT (remapped) device IDs: the workspace rebuilds devices with fresh
// IDs and an oldID→newID map, and the saved per-case ids (which reference the
// OLD, saved IDs) are translated through that map by the caller before reaching
// here.
//
// Why this exists: case membership is written by GetProperties but cannot be
// restored through ApplyProperties — StatementSequence is not scene.Inspectable, so
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
// ApplyProperties (StatementSequence não é scene.Inspectable; map[string]string não
// leva arrays nem o remapeamento). Chamado antes do NotifyChange pós-import.
func (e *StatementSequence) RestoreCaseState(selectorType, selectedCase, defaultCaseID string, cases []CaseRestoreEntry) {
	// Sequence: selectorType has no home here — phases have no selector.
	// The arg stays in the signature to satisfy caseRestorable; ignored.
	// Português: Sem seletor em fases; argumento ignorado (interface).
	_ = selectorType

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
			})
		}
		e.phases.entries = rebuilt
	}

	_ = defaultCaseID // no default-phase concept — ignored (interface arg)

	if selectedCase != "" {
		e.phases.selected = selectedCase
	}
	if e.phases.selected == "" && len(e.phases.entries) > 0 {
		e.phases.selected = e.phases.entries[0].id
	}

	if e.ornamentDraw != nil {
		e.ornamentDraw.SetCaseLabel(e.activeCaseLabel())
	}

	// Hide the inactive cases right away so the import does not leave every
	// case overlapping. The NotifyChange that follows the import
	// (assignNewChildren + applyCaseVisibility) is then idempotent.
	e.applyCaseVisibility()

	// Re-bake the ornament bitmap. SetCaseLabel above only updates the
	// MODEL; the pill's PIXELS come from recacheOrnament, and the import
	// loop's own `go RefreshVisual()` usually runs BEFORE this restore —
	// baking the Init-time label into the sprite. Without this re-bake
	// the pill showed "phase 0" while the (live-computed) dropdown menu
	// checkmarked the restored phase (field report 2026-07-18, "estava
	// na fase 0 e o menu mostrava fase 1"). Same idiom as selectCase and
	// removeLastPhase: label push + async re-bake.
	// Português: Re-assa o bitmap do ornamento. SetCaseLabel só muda o
	// MODELO; o pixel vem do recacheOrnament, e o RefreshVisual do
	// import costuma rodar ANTES deste restore — assando o label do
	// Init no sprite. Sem isto a pílula mostrava "phase 0" com o menu
	// (vivo) marcando a fase restaurada (campo 2026-07-18). Mesmo
	// idioma do selectCase/removeLastPhase: label + re-assar assíncrono.
	go e.recacheOrnament()
}
