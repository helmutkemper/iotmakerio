// /ide/devices/compFlow/statementIfElse_branchRestore.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// RestoreBranchState re-applies the persisted branch membership (which child
// belongs to the true vs the false branch) and the selected branch after a
// scene IMPORT. The IDs passed in are the POST-IMPORT (remapped) device IDs:
// the workspace rebuilds devices with fresh IDs and an oldID→newID map, and
// the saved trueBranchIDs/falseBranchIDs (which reference the OLD, saved IDs)
// are translated through that map by the caller before reaching here.
//
// Why this exists: branch membership is written by GetProperties but cannot
// be restored through ApplyProperties — StatementIfElse is not
// scene.Inspectable (it has no inspect panel), so ApplyProperties is never
// invoked on import; and even if it were, its map[string]string signature
// carries neither the []string arrays nor the import's ID remapping. Without
// this restore, assignNewChildren finds both branch lists empty and assigns
// EVERY contained child to the currently-selected branch — so both branches'
// devices pile into one branch and applyBranchVisibility has nothing to hide
// (the "else devices show up in the if" load bug). Calling this before the
// post-import NotifyChange makes the assignNewChildren it triggers a no-op
// (every child is already assigned) and lets applyBranchVisibility hide the
// inactive branch.
//
// Português: Reaplica a associação de branch (true/false) e a branch
// selecionada após IMPORTAR uma cena. Os IDs recebidos já são os IDs
// remapeados (pós-import). É necessário porque a associação é salva em
// GetProperties mas não trafega pelo ApplyProperties: o StatementIfElse não
// é scene.Inspectable (sem painel de inspeção), então ApplyProperties nem é
// chamado no import; e map[string]string não levaria os arrays nem o
// remapeamento de IDs. Sem isso, assignNewChildren joga todos os filhos
// contidos na branch selecionada e applyBranchVisibility não tem o que
// esconder. Chamado antes do NotifyChange pós-import.
func (e *StatementIfElse) RestoreBranchState(selectedBranch string, trueIDs, falseIDs []string) {
	if selectedBranch != "" {
		e.selectedBranch = selectedBranch
		if e.ornamentDraw != nil {
			e.ornamentDraw.SetBranch(selectedBranch)
		}
	}

	// Copy so we never alias the caller's slices.
	e.trueBranchIDs = append([]string(nil), trueIDs...)
	e.falseBranchIDs = append([]string(nil), falseIDs...)

	// Hide the inactive branch right away so the import does not leave both
	// branches overlapping. The NotifyChange that follows the import
	// (assignNewChildren + applyBranchVisibility) is then idempotent.
	e.applyBranchVisibility()
}
