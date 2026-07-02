// ui/contextMenu/copy.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The context-menu "copy" action, kept in ONE place. Every device's body menu
// is opened through OpenForDevice, which prepends a shared "copy" item built
// from a tiny interface (CopyableDevice) plus a handler installed once by the
// workspace. This is a decorator over Open — the Go-idiomatic way to add one
// behaviour to many callers without repeating it in each device's menu builder,
// and without the menu having to know the concrete device types.

package contextMenu

import (
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/translate"
)

// CopyableDevice is the minimal behaviour the menu needs to duplicate a device:
// its stable type name and its current data. Every stage device already
// satisfies this (it is a strict subset of scene.SceneDevice), so a device opts
// in simply by being passed to OpenForDevice — no device gains a new method and
// this package never imports the device or scene packages. This follows the
// "accept interfaces" guideline: depend on a small local interface, not on
// concrete types.
//
// Português: Comportamento mínimo para duplicar um device: o nome do tipo e os
// dados atuais. Todo device de stage já satisfaz isto (é um subconjunto de
// scene.SceneDevice), então ele "opta" apenas por ser passado ao OpenForDevice
// — nenhum device ganha método novo e este pacote não importa scene/devices.
type CopyableDevice interface {
	// GetDeviceType returns the stable device-type string (e.g.
	// "StatementConstInt") the factory uses to recreate the device. This is the
	// ONLY hard requirement: it is part of scene.SceneDevice, so EVERY stage
	// device is copyable. The device's data is fetched separately and optionally
	// (see OpenForDevice), because a few devices — e.g. the loop containers —
	// have no configurable data and do not implement GetProperties.
	GetDeviceType() string
}

// SetCopyHandler installs the callback that performs a copy. The workspace wires
// this once, per stage, to the factory's placement-based duplicator
// (DeviceFactory.CreateCopy). Passing nil disables the "copy" item.
//
// Português: Instala o callback que executa a cópia. O workspace fixa isto uma
// vez, por stage, apontando para DeviceFactory.CreateCopy. nil desabilita o item.
func (c *Controller) SetCopyHandler(fn func(deviceType string, props map[string]interface{})) {
	c.copyHandler = fn
}

// OpenForDevice opens a device's body context menu, PREPENDING a shared "copy"
// item. This is the single definition of the "copy" action: a decorator over
// OpenAtWorld so every device gets it identically, without the item being
// repeated in each device's own menu builder.
//
//   - dev is the source device (supplies the copy's type + data).
//   - extra is the device's own menu (Inspect, Delete, …), already built.
//   - worldX/worldY are the device's world coordinates; OpenAtWorld converts
//     them to screen space.
//
// If no copy handler has been installed, this behaves exactly like OpenAtWorld
// (no "copy" item), so callers can adopt it unconditionally.
//
// Português: Abre o menu de corpo de um device, PREPENDANDO um item "copy"
// compartilhado — a única definição da ação "copy", um decorator sobre o
// OpenAtWorld, para que todo device a receba igual sem repetir o item. Sem
// handler instalado, comporta-se exatamente como OpenAtWorld.
func (c *Controller) OpenForDevice(dev CopyableDevice, extra []Item, worldX, worldY float64) {
	items := extra

	if c.copyHandler != nil && dev != nil {
		// Snapshot the source's type and data NOW, at menu-build time, so a
		// later edit to the source device cannot change what the pending copy
		// will place. The captured values are what CreateCopy replays on click.
		//
		// Português: Fotografa o tipo e os dados da origem AGORA, na montagem do
		// menu, para que uma edição posterior na origem não altere o que a
		// cópia pendente vai colocar.
		deviceType := dev.GetDeviceType()

		// GetProperties is OPTIONAL. Most devices expose their data (value,
		// varName, label, …) through it, but a few — e.g. the loop containers,
		// which have no configurable data of their own — do not implement it. A
		// device without GetProperties copies with no data, i.e. as a fresh
		// instance, which is exactly right for those. Fetching it through a
		// second, ad-hoc interface lets CopyableDevice stay minimal.
		var props map[string]interface{}
		if p, ok := dev.(interface {
			GetProperties() map[string]interface{}
		}); ok {
			props = p.GetProperties()
		}

		copyItem := Item{
			ID:    "copy",
			Label: translate.T("menuDeviceCopy", "Copy"),
			// FontAwesome "copy" (solid), same family as the other body-menu
			// icons (e.g. trash-can for Delete). The path lives in rulesIcon so
			// it is defined once and resolvable by name ("copy") like the rest.
			FontAwesomePath: rulesIcon.KFACopy,
			ViewBox:         "0 0 448 512",
			HelpFallback: "Place a duplicate of this device — same data, no wires — " +
				"at your next click on the stage.",
			OnClick: func() {
				c.copyHandler(deviceType, props)
			},
		}

		// Prepend so "copy" sits at the top, above the device's own items.
		items = append([]Item{copyItem}, extra...)
	}

	c.OpenAtWorld(items, worldX, worldY)
}
