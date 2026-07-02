// scene/properties.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scene

import (
	"fmt"
	"strings"
)

// ReplayProperties converts a device's exported JSON property map (the shape
// produced by SceneDevice.GetProperties and stored in the scene's "devices"
// array) into the flat map[string]string that Inspectable.ApplyProperties
// expects, then applies it. A nil device or an empty map is a no-op.
//
// This is the SINGLE conversion point shared by the two call sites that MUST
// behave identically:
//
//   - scene import — rebuilding a saved stage (stageWorkspace.importScene);
//   - context-menu "copy" — duplicating a device (DeviceFactory.CreateCopy).
//
// Keeping one implementation is what guarantees a copied device is configured
// exactly like a reloaded one: same value, same varName, same BlackBox props.
// If the exporter/importer contract ever changes, it changes here once.
//
// Flattening rule (mirrors the exporter):
//
//   - a scalar value becomes fmt.Sprintf("%v", v) under its own key;
//   - a NESTED map is expanded to flat "<singular>_<subkey>" entries. BlackBox
//     devices store their BB_PROP values under a PLURAL key, e.g.
//     "props": {"sdaPin": "GP14", "sclPin": "GP15"}; ApplyProperties expects
//     "prop_sdaPin" → "GP14". The prefix is formed by stripping the trailing
//     "s" from the key ("props" → "prop_") and joining with "_".
//
// Português: Converte o mapa de propriedades JSON de um device (o formato do
// GetProperties, gravado no array "devices" da cena) no map[string]string plano
// que o Inspectable.ApplyProperties espera, e aplica. É o ÚNICO ponto de
// conversão, compartilhado por dois chamadores que precisam se comportar igual:
// o import da cena e a ação "copy" do menu de contexto — é isto que garante que
// um device copiado fica configurado exatamente como um recarregado. Valor
// escalar vira fmt.Sprintf("%v", v); mapa aninhado (BlackBox guarda seus
// BB_PROP sob a chave plural "props") é expandido para "prop_<subchave>",
// tirando o "s" final da chave para formar o prefixo.
func ReplayProperties(dev Inspectable, jsonProps map[string]interface{}) {
	if dev == nil || len(jsonProps) == 0 {
		return
	}

	props := make(map[string]string, len(jsonProps)*2)
	for k, v := range jsonProps {
		switch val := v.(type) {
		case map[string]interface{}:
			// Nested map (e.g. "props") → flat "prop_<subkey>" entries.
			prefix := strings.TrimSuffix(k, "s") + "_"
			for subKey, subVal := range val {
				props[prefix+subKey] = fmt.Sprintf("%v", subVal)
			}
		default:
			props[k] = fmt.Sprintf("%v", v)
		}
	}

	dev.ApplyProperties(props)
}
