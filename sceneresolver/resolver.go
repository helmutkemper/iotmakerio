// ide/sceneresolver/resolver.go — Resolves template variable values from the canvas.
//
// English:
//
//	The resolver reads the scene JSON exported by SceneMgr and determines the
//	effective value of each template variable by inspecting the canvas graph.
//
//	Resolution priority (highest first):
//	  1. Wire value  — a ConstInt / ConstBool / other source device connected
//	                   via a wire to the Init connector that matches the field.
//	  2. Prop value  — the value set in the Inspect panel (properties.props[Field]).
//	  3. Template default — the compiled default from the template manifest.
//
//	Wire matching is case-insensitive: the template var "Port" maps to field
//	"ServerConfig.Port" whose Init connector is named "port" (Go parameter name).
//	"port" == "Port" after lowercasing, so the wire is followed.
//
//	Supported source device types (wire side):
//	  StatementConstInt  → properties.value (number → string)
//	  StatementConstBool → properties.value (bool → "true"/"false")
//
//	Future source types (math nodes, string const, etc.) can be added to
//	extractOutputValue without touching the rest of the resolver.
//
// Português:
//
//	O resolver lê o JSON da cena e determina o valor efetivo de cada variável
//	de template inspecionando o grafo do canvas. Fio > Prop > Default do template.
package sceneresolver

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/helmutkemper/iotmakerio/templateclient"
)

// ── Scene JSON types ──────────────────────────────────────────────────────────
//
// These mirror the scene serializer JSON format exactly. Field names must
// match the JSON tags produced by scene.Serializer.Export().

type sceneDoc struct {
	Devices []sceneDevice `json:"devices"`
	Wires   []sceneWire   `json:"wires"`
}

type sceneDevice struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Properties json.RawMessage  `json:"properties"`
	Connectors []sceneConnector `json:"connectors"`
}

type sceneConnector struct {
	Port        string            `json:"port"`
	DataType    string            `json:"dataType"`
	IsOutput    bool              `json:"isOutput"`
	Connections []sceneConnection `json:"connections"`
}

type sceneConnection struct {
	WireID       string `json:"wireId"`
	TargetDevice string `json:"targetDevice"`
	TargetPort   string `json:"targetPort"`
}

type sceneWire struct {
	ID       string       `json:"id"`
	From     sceneWireEnd `json:"from"`
	To       sceneWireEnd `json:"to"`
	DataType string       `json:"dataType"`
}

type sceneWireEnd struct {
	Device string `json:"device"`
	Port   string `json:"port"`
}

// blackBoxInitProps is the properties block for BlackBoxInit:X devices.
// Props holds the Inspect panel values keyed by the Go field name (e.g. "Port").
type blackBoxInitProps struct {
	InstanceID string            `json:"instanceId"`
	Props      map[string]string `json:"props"`
}

// constIntProps is the properties block for StatementConstInt devices.
type constIntProps struct {
	Value json.Number `json:"value"`
}

// constBoolProps is the properties block for StatementConstBool devices.
type constBoolProps struct {
	Value bool `json:"value"`
}

// constStringProps is the properties block for StatementConstString devices.
type constStringProps struct {
	Value string `json:"value"`
}

// ── Public API ────────────────────────────────────────────────────────────────

// ResolvedTemplate holds the fully resolved configuration for one template
// instance found on the canvas.
type ResolvedTemplate struct {
	// TemplateID is the server-side ID of the template package.
	TemplateID string

	// TemplateName is the human-readable name used as the download filename.
	TemplateName string

	// Config maps template variable names to their resolved values.
	// Ready to POST to /api/v1/templates/:id/generate.
	Config map[string]string
}

// HasTemplateDevices returns true when the scene contains at least one device
// whose struct name belongs to a loaded template.
//
// Used by the workspace to decide between template ZIP generation and the
// regular Go codegen flow before doing the heavier Resolve() call.
func HasTemplateDevices(
	sceneJSON string,
	loadedTemplates map[string]*templateclient.TemplateFullClient,
) bool {
	if sceneJSON == "{}" || sceneJSON == "" || len(loadedTemplates) == 0 {
		return false
	}

	// Collect all struct names that belong to any loaded template.
	templateStructs := make(map[string]bool)
	for _, tmpl := range loadedTemplates {
		if tmpl.Def == nil {
			continue
		}
		for _, dev := range tmpl.Def.Devices {
			templateStructs[dev.Name] = true
		}
	}

	var doc sceneDoc
	if err := json.Unmarshal([]byte(sceneJSON), &doc); err != nil {
		return false
	}

	for _, d := range doc.Devices {
		if strings.HasPrefix(d.Type, "BlackBoxInit:") {
			structName := strings.TrimPrefix(d.Type, "BlackBoxInit:")
			if templateStructs[structName] {
				return true
			}
		}
	}
	return false
}

// Resolve scans the scene JSON for template devices and returns one
// ResolvedTemplate per template found on the canvas.
//
// Wire values take priority over Inspect panel props, which in turn take
// priority over the template's compiled defaults.
//
// Returns nil when no template devices are found.
func Resolve(
	sceneJSON string,
	loadedTemplates map[string]*templateclient.TemplateFullClient,
) []ResolvedTemplate {
	if sceneJSON == "{}" || sceneJSON == "" || len(loadedTemplates) == 0 {
		return nil
	}

	var doc sceneDoc
	if err := json.Unmarshal([]byte(sceneJSON), &doc); err != nil {
		return nil
	}

	// Build lookup indices for O(1) access during resolution.
	deviceByID := make(map[string]*sceneDevice, len(doc.Devices))
	for i := range doc.Devices {
		deviceByID[doc.Devices[i].ID] = &doc.Devices[i]
	}

	// Index Init devices by struct name so the resolver can find them quickly.
	// A struct may have multiple instances on the canvas (different instanceIds).
	// We collect all of them; the resolution uses the first one whose connector
	// matches. Future work: route by instanceId to support multiple instances.
	initByStruct := make(map[string][]*sceneDevice)
	for i := range doc.Devices {
		d := &doc.Devices[i]
		if !strings.HasPrefix(d.Type, "BlackBoxInit:") {
			continue
		}
		name := strings.TrimPrefix(d.Type, "BlackBoxInit:")
		initByStruct[name] = append(initByStruct[name], d)
	}

	var results []ResolvedTemplate
	for templateID, tmpl := range loadedTemplates {
		if tmpl.Def == nil {
			continue
		}

		// Check if any of this template's structs appear on the canvas.
		found := false
		for _, dev := range tmpl.Def.Devices {
			if _, ok := initByStruct[dev.Name]; ok {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		config := resolveConfig(tmpl, initByStruct, deviceByID)
		results = append(results, ResolvedTemplate{
			TemplateID:   templateID,
			TemplateName: tmpl.Meta.Name,
			Config:       config,
		})
	}

	return results
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveConfig builds the config map for one template by walking the canvas
// graph for each declared variable.
func resolveConfig(
	tmpl *templateclient.TemplateFullClient,
	initByStruct map[string][]*sceneDevice,
	deviceByID map[string]*sceneDevice,
) map[string]string {
	config := make(map[string]string, len(tmpl.Def.Manifest.Vars))

	for varName, path := range tmpl.Def.Manifest.Vars {
		// path = "DeviceName.FieldName", e.g. "ServerConfig.Port"
		dotIdx := strings.Index(path, ".")
		if dotIdx < 1 || dotIdx == len(path)-1 {
			config[varName] = tmpl.VarDefault(varName)
			continue
		}
		structName := path[:dotIdx]
		fieldName := path[dotIdx+1:]

		devices, ok := initByStruct[structName]
		if !ok || len(devices) == 0 {
			config[varName] = tmpl.VarDefault(varName)
			continue
		}

		// Use the first canvas instance.
		initDev := devices[0]

		// Priority 1: follow a wire connected to the matching Init connector.
		if val, ok := valueFromWire(initDev, fieldName, deviceByID); ok {
			config[varName] = val
			continue
		}

		// Priority 2: read from the Inspect panel props.
		if val, ok := valueFromProps(initDev, fieldName); ok {
			config[varName] = val
			continue
		}

		// Priority 3: use the template compiled default.
		config[varName] = tmpl.VarDefault(varName)
	}

	return config
}

// valueFromWire looks for an input connector on dev whose name matches
// fieldName (case-insensitive), then follows the wire to the source device
// and extracts its output value.
//
// "Port" field → "port" connector → wire → StatementConstInt{value:8082} → "8082"
func valueFromWire(
	dev *sceneDevice,
	fieldName string,
	deviceByID map[string]*sceneDevice,
) (string, bool) {
	lower := strings.ToLower(fieldName)

	for _, conn := range dev.Connectors {
		// Only input connectors receive wires.
		if conn.IsOutput {
			continue
		}
		// Case-insensitive: "port" connector matches "Port" field.
		if strings.ToLower(conn.Port) != lower {
			continue
		}
		if len(conn.Connections) == 0 {
			return "", false
		}
		// Follow the first wire to the source device.
		c := conn.Connections[0]
		src, ok := deviceByID[c.TargetDevice]
		if !ok {
			return "", false
		}
		return extractOutputValue(src)
	}

	return "", false
}

// valueFromProps reads a named field from the device's Inspect panel props.
// The field name must match exactly (prop keys are Go field names, e.g. "Port").
func valueFromProps(dev *sceneDevice, fieldName string) (string, bool) {
	var props blackBoxInitProps
	if err := json.Unmarshal(dev.Properties, &props); err != nil {
		return "", false
	}
	val, ok := props.Props[fieldName]
	return val, ok
}

// extractOutputValue reads the single output value of a source device.
// Each built-in device type has a fixed property schema.
func extractOutputValue(dev *sceneDevice) (string, bool) {
	switch dev.Type {

	// Integer constant: properties.value is a JSON number.
	case "StatementConstInt":
		var p constIntProps
		if err := json.Unmarshal(dev.Properties, &p); err != nil {
			return "", false
		}
		// Use the raw number string to avoid floating-point formatting.
		raw := p.Value.String()
		// If the number has a decimal part (e.g. "8082.0"), trim it.
		if idx := strings.Index(raw, "."); idx >= 0 {
			raw = raw[:idx]
		}
		return raw, true

	// Boolean constant: properties.value is a JSON bool.
	case "StatementConstBool":
		var p constBoolProps
		if err := json.Unmarshal(dev.Properties, &p); err != nil {
			return "", false
		}
		return strconv.FormatBool(p.Value), true

	// String constant: properties.value is a JSON string.
	case "StatementConstString":
		var p constStringProps
		if err := json.Unmarshal(dev.Properties, &p); err != nil {
			return "", false
		}
		return p.Value, true

	default:
		// Unknown source type — caller will fall back to prop or default.
		return "", false
	}
}
