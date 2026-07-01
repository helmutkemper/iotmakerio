// /ide/stageWorkspace/importBranchRestore.go
package stageWorkspace

import (
	"log"

	"github.com/helmutkemper/iotmakerio/scene"
)

// branchRestorable is implemented by container devices whose branch
// membership must be re-applied after a scene import, once the oldID→newID
// map is complete. Only StatementIfElse implements it today.
//
// Português: Implementada por containers cuja associação de branch precisa
// ser reaplicada após importar, quando o mapa oldID→newID está completo.
type branchRestorable interface {
	RestoreBranchState(selectedBranch string, trueIDs, falseIDs []string)
}

// restoreImportedBranches re-applies if/else branch membership after an
// import. Branch membership is saved per container (GetProperties) but never
// travels back through ApplyProperties: StatementIfElse is not
// scene.Inspectable (so ApplyProperties is not called on import), and a
// map[string]string carries neither the []string arrays nor the import's ID
// remapping. Without this, assignNewChildren dumps every contained child into
// the default branch and both branches' devices overlap. This runs after the
// device-creation loop (idMap complete) and BEFORE the post-import
// NotifyChange, so the assignNewChildren that NotifyChange triggers becomes a
// no-op.
//
// Português: Reaplica a associação de branch do if/else após importar. A
// associação é salva mas não volta pelo ApplyProperties (StatementIfElse não
// é scene.Inspectable; e map[string]string não leva arrays nem o
// remapeamento de IDs). Sem isto, assignNewChildren joga todos os filhos
// numa branch só. Roda após criar os devices (idMap completo) e ANTES do
// NotifyChange pós-import.
func (w *Workspace) restoreImportedBranches(devices []scene.DeviceJSON, idMap map[string]string) {
	for _, dev := range devices {
		newID, ok := idMap[dev.ID]
		if !ok {
			continue // device not created in this stage (foreign/skipped)
		}
		created := w.SceneMgr.FindDevice(newID)
		if created == nil {
			continue
		}
		br, ok := created.(branchRestorable)
		if !ok {
			continue // not a branch container — nothing to restore
		}

		selectedBranch, _ := dev.Properties["selectedBranch"].(string)
		trueIDs := remapBranchIDs(dev.Properties["trueBranchIDs"], idMap)
		falseIDs := remapBranchIDs(dev.Properties["falseBranchIDs"], idMap)
		br.RestoreBranchState(selectedBranch, trueIDs, falseIDs)

		log.Printf("[Workspace:%s] Import: restored branches for %s (selected=%q, true=%v, false=%v)",
			w.Name, newID, selectedBranch, trueIDs, falseIDs)
	}
}

// remapBranchIDs converts a saved branch-ID list — stored in the scene as a
// []interface{} of strings under the OLD device IDs — into the post-import
// IDs via idMap. IDs absent from the map (e.g. a child that did not import)
// are dropped.
//
// Português: Converte a lista de IDs de branch salva ([]interface{} de
// strings com os IDs antigos) para os IDs pós-import via idMap. IDs ausentes
// do mapa são descartados.
func remapBranchIDs(raw interface{}, idMap map[string]string) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		oldID, ok := v.(string)
		if !ok {
			continue
		}
		if newID, ok := idMap[oldID]; ok {
			out = append(out, newID)
		}
	}
	return out
}
