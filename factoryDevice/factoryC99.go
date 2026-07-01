// factoryDevice/factoryC99.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryDevice

// factoryC99.go — C99 device-function block creation for DeviceFactory.
//
// English:
//
//	C99 follows the device-per-function model (decision b): every public
//	C99 function is a standalone visual block, and the struct handle
//	(e.g. sht3x_t*) is a WIRE-TYPE that flows between blocks on the maker's
//	wires. There is no shared struct instance and no implicit receiver.
//
//	This is the opposite of the Go model in factoryBlackBox.go, where Init
//	creates one struct instance and every method shares its instanceId so
//	the codegen emits a single variable. For C99 there is nothing to share,
//	so each function block gets its OWN unique instanceId — the blocks are
//	independent sprites. The instanceId is therefore not meaningful for C99
//	codegen the way it is for Go; the handle dependency is expressed by the
//	wire the maker draws (the LabVIEW resource chain), not by a shared id.
//
//	The visual block is REUSED from compBlackBox.StatementBlackBoxMethod
//	(docs/c99_ide_integration.md §5.4). FunctionDefClient is a type alias of
//	MethodDefClient, so the same block renders a function's ports — inputs
//	from fn.Inputs, outputs from fn.Outputs — the latter already carrying the
//	synthesized handle pass-through (dev_out) the server added in toClientDef.
//
//	NOTE for codegen (Fatia 4): the reused block's GetDeviceType() yields
//	"BlackBox<fnName>:" with an empty struct part (def.Name == "" for C99).
//	That empty struct segment is the natural discriminator for a C99 function
//	device when the stage→C99 codegen lands; revisit there.
//
// Português:
//
//	C99 segue o modelo um-device-por-função (decisão b): cada função pública
//	é um bloco independente, e o handle (sht3x_t*) é um wire-type que viaja
//	pelos fios. Não há instância de struct compartilhada nem receiver
//	implícito — ao contrário do Go. Por isso cada bloco de função recebe seu
//	próprio instanceId único (blocos independentes). O bloco visual é
//	reaproveitado de StatementBlackBoxMethod via o alias FunctionDefClient.

import (
	"log"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/rulesSequentialId"
)

// CreateBlackBoxFunction creates a visual block for one C99 device-function
// (decision b) of the given black-box definition.
//
// fnName must match the Name field of one of the entries in def.Functions.
// If the function is not found, the call is logged and ignored.
//
// Unlike CreateBlackBoxMethod, this does NOT use the shared bbInstanceId
// cache: each C99 function block is independent, so it receives a fresh,
// unique instanceId. The block component (StatementBlackBoxMethod) is reused
// because FunctionDefClient aliases MethodDefClient.
//
// Português: Cria um bloco visual para uma função-device C99. fnName deve
// corresponder ao Name de uma entrada em def.Functions. Cada bloco é
// independente — recebe um instanceId único, sem o cache compartilhado.
func (f *DeviceFactory) CreateBlackBoxFunction(def *blackbox.BlackBoxDefClient, fnName string) {
	fn := def.GetFunction(fnName)
	if fn == nil {
		log.Printf("[Factory] %q has no C99 function %q — cannot create function device", def.Name, fnName)
		return
	}

	// A fresh, NON-cached id per block. The base is the constant "c99fn"
	// rather than def.Name because a C99 def has no struct name (def.Name == "")
	// and every function block is independent — sharing a base must NOT
	// collapse them onto a single id. GetIdFromBase increments per base, so
	// each call is unique ("c99fn_1", "c99fn_2", …).
	instanceId := rulesSequentialId.GetIdFromBase("c99fn")

	// Reuse the Go method block. fn is a *FunctionDefClient, which is a
	// *MethodDefClient via the alias, so it fits createBlackBoxMethod as-is.
	// Empty callbackRefFn → the normal CALLABLE block (parameters as inputs).
	f.createBlackBoxMethod(def, fn, instanceId, "")
}

// CreateBlackBoxCallbackRef creates the C99 callback REFERENCE block (the "ƒ"
// device) for the callback-handler function fnName of def — the dedicated
// counterpart of the normal callable block produced by CreateBlackBoxFunction.
// It is offered only for a function whose def carries a HandlerType (a
// `// callback:<type>.` marked function); the reference exposes NO inputs and a
// SINGLE output `callback` of the handler type, which the maker wires by address
// into a matching callback parameter (the LabVIEW static-VI-reference idiom —
// e.g. setDisplay(displayWrite)).
//
// Implementation note: the block is the SAME StatementBlackBoxMethod, fed a
// SYNTHETIC method (no inputs, one callback output) and flagged via the last
// createBlackBoxMethod argument so its GetDeviceType reports "CallbackRef:<fn>".
// The parsed function def is never mutated and the normal callable block is
// untouched. See the duality section of docs/CODEGEN_C99_CALLBACKS.md.
//
// Português: Cria o bloco de REFERÊNCIA de callback (device "ƒ") da função
// handler fnName — o par dedicado do bloco chamável normal. Sem entradas, uma
// única saída `callback` do tipo do handler, ligada por endereço a um parâmetro
// de callback compatível. Reusa o StatementBlackBoxMethod com um método
// sintético + flag de tipo; o def parseado não é alterado.
func (f *DeviceFactory) CreateBlackBoxCallbackRef(def *blackbox.BlackBoxDefClient, fnName string) {
	fn := def.GetFunction(fnName)
	if fn == nil {
		log.Printf("[Factory] %q has no C99 function %q — cannot create callback reference", def.Name, fnName)
		return
	}
	// A reference only exists for a callback handler; the synthetic output's
	// type is the handler's function-pointer typedef.
	if fn.HandlerType == "" {
		log.Printf("[Factory] C99 function %q is not a callback handler — no reference device", fnName)
		return
	}

	// The visible label distinguishes the reference from the callable with a
	// trailing "ƒ" (the function-reference glyph).
	labelText := fn.Label
	if labelText == "" {
		labelText = fnName
	}

	// Synthetic method: no inputs, one `callback` output of the handler type.
	// CallbackType is set so the strict ƒ-wire rule (fatia 5.I) can match it
	// against a consumer's callback parameter of the same type.
	synthetic := &blackbox.MethodDefClient{
		Name:  fnName,
		Icon:  fn.Icon,
		Label: labelText + " ƒ",
		Doc:   fn.Doc,
		Outputs: []blackbox.PortDefClient{
			{
				Name:         "callback",
				GoType:       fn.HandlerType,
				CallbackType: fn.HandlerType,
				Doc:          "Function reference, passed by address into a matching callback parameter.",
			},
		},
	}

	// Independent block, like every C99 device. A distinct id base keeps the
	// reference blocks from colliding with the callable function blocks.
	instanceId := rulesSequentialId.GetIdFromBase("c99cbref")

	// The last argument flags the callback-reference variant, so GetDeviceType
	// reports "CallbackRef:<fn>".
	f.createBlackBoxMethod(def, synthetic, instanceId, fnName)
}
