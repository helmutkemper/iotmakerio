// /ide/stageWorkspace/importCaseRestore.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package stageWorkspace

import (
	"encoding/json"
	"log"

	"github.com/helmutkemper/iotmakerio/devices/compFlow"
	"github.com/helmutkemper/iotmakerio/scene"
)

// caseRestorable is implemented by container devices whose N-way case
// membership must be re-applied after a scene import, once the oldID→newID map
// is complete. Only StatementCase implements it today.
//
// Português: Implementada por containers cuja associação de cases (N vias)
// precisa ser reaplicada após importar. Só o StatementCase implementa.
type caseRestorable interface {
	RestoreCaseState(selectorType, selectedCase, defaultCaseID string, cases []compFlow.CaseRestoreEntry)
}

// restoreImportedCases re-applies StatementCase case membership after an
// import. Case membership is saved per container (GetProperties) but never
// travels back through ApplyProperties: StatementCase is not scene.Inspectable
// (so ApplyProperties is not called on import), and a map[string]string carries
// neither the []string members nor the import's ID remapping. Without this,
// assignNewChildren dumps every contained child into the selected case and all
// cases overlap. This must run after the device-creation loop (idMap complete)
// and BEFORE the post-import NotifyChange, so the assignNewChildren that
// NotifyChange triggers becomes a no-op.
//
// WIRING: call this in the import flow once idMap is complete and BEFORE the
// post-import NotifyChange, e.g.:
//
//	w.restoreImportedCases(devices, idMap)
//
// Português: Reaplica a associação de cases após importar. Rode no fluxo de
// import (idMap completo) e ANTES do NotifyChange pós-import.
// phaseRestorable is the Function's phase-restore contract — the
// import feeds it remapped entries. Português: Contrato de restore de
// fases da Function; o import entrega entradas remapeadas.
type phaseRestorable interface {
	RestorePhaseState(selected string, phases []compFlow.CaseRestoreEntry)
}

func (w *Workspace) restoreImportedCases(devices []scene.DeviceJSON, idMap map[string]string) {
	for _, dev := range devices {
		newID, ok := idMap[dev.ID]
		if !ok {
			continue // device not created in this stage (foreign/skipped)
		}
		created := w.SceneMgr.FindDevice(newID)
		if created == nil {
			continue
		}
		// PHASE-hosting Function (2026-07-21, "o backup restaurou o
		// componente totalmente errado"): its phases travel as a JSON
		// STRING with the OLD member ids — parse, remap through idMap
		// (the twins' own remapper) and hand to RestorePhaseState,
		// which re-applies membership+visibility synchronously. The
		// ApplyProperties path no longer parses phases (it would
		// clobber with stale ids at +200ms). Português: Function com
		// fases — a string JSON carrega ids ANTIGOS; parse + remap
		// pelo idMap e entrega ao RestorePhaseState; o ApplyProperties
		// não lê mais phases (clobberia com ids velhos).
		if pr, isPhase := created.(phaseRestorable); isPhase {
			if s, _ := dev.Properties["phases"].(string); s != "" {
				var raw interface{}
				if err := json.Unmarshal([]byte(s), &raw); err == nil {
					entries := remapCases(raw, idMap)
					selected, _ := dev.Properties["selectedPhase"].(string)
					pr.RestorePhaseState(selected, entries)
					log.Printf("[Workspace:%s] Import: restored phases for %s (selected=%q, phases=%d)",
						w.Name, newID, selected, len(entries))
				}
			}
			continue
		}

		cr, ok := created.(caseRestorable)
		if !ok {
			continue // not a case container — nothing to restore
		}

		selectorType, _ := dev.Properties["selectorType"].(string)
		selectedCase, _ := dev.Properties["selectedCase"].(string)
		defaultCaseID, _ := dev.Properties["defaultCaseId"].(string)
		cases := remapCases(dev.Properties["cases"], idMap)
		cr.RestoreCaseState(selectorType, selectedCase, defaultCaseID, cases)

		log.Printf("[Workspace:%s] Import: restored cases for %s (selector=%q, selected=%q, cases=%d)",
			w.Name, newID, selectorType, selectedCase, len(cases))
	}
}

// remapCases converts the saved "cases" array — stored in the scene as a
// []interface{} of objects, each with string "id"/"label", a "values" array and
// an "ids" array of the OLD device IDs — into []compFlow.CaseRestoreEntry with
// the child IDs translated to the post-import IDs via idMap. Child IDs absent
// from the map (e.g. a child that did not import) are dropped; literal values
// are carried through unchanged.
//
// Português: Converte o array "cases" salvo (com os IDs antigos dos filhos) em
// []compFlow.CaseRestoreEntry, traduzindo os IDs dos filhos via idMap. IDs
// ausentes são descartados; os valores literais passam inalterados.
func remapCases(raw interface{}, idMap map[string]string) []compFlow.CaseRestoreEntry {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]compFlow.CaseRestoreEntry, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		entry := compFlow.CaseRestoreEntry{}
		entry.ID, _ = m["id"].(string)
		entry.Label, _ = m["label"].(string)
		entry.MatchKind, _ = m["matchKind"].(string)
		entry.Values = caseStringSlice(m["values"])
		entry.IDs = remapIDSlice(m["ids"], idMap)
		if b, ok := m["isDefault"].(bool); ok {
			entry.IsDefault = b
		}
		out = append(out, entry)
	}
	return out
}

// caseStringSlice converts a []interface{} of strings into []string, skipping
// non-string elements.
//
// Português: Converte []interface{} de strings em []string.
func caseStringSlice(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// remapIDSlice translates a []interface{} of OLD device IDs to the post-import
// IDs via idMap, dropping IDs absent from the map.
//
// Português: Traduz []interface{} de IDs antigos para os pós-import via idMap.
func remapIDSlice(raw interface{}, idMap map[string]string) []string {
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
