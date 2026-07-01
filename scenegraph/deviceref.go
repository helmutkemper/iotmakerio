// /ide/scenegraph/deviceref.go

package scenegraph

// DeviceRef is the minimal interface the Graph needs to interact with a
// device on the stage.
//
// English:
//
//	The scenegraph does not know what a "device" is in the rest of the
//	codebase — it only knows DeviceRef. This keeps the package free of
//	upward imports (no scene, no sprite, no devices/*). Bridging to the
//	real device types is the Serializer's job; it wraps each scene.SceneDevice
//	in a small adapter that satisfies DeviceRef.
//
//	Geometry accessors are pull-based: the Graph asks the ref for the
//	latest rectangles whenever it needs them (during UpdateDrag, for
//	example). This avoids keeping the Node.Outer/Inner in sync via a
//	separate push channel.
//
//	MoveBy is expected to update the underlying device's own position,
//	including any visual side-effects the device has (warning mark, wire
//	routes, etc.). After MoveBy, calling OuterRect again must return the
//	new rectangle.
//
// Português:
//
//	Interface mínima que o Graph precisa para interagir com um device.
//	O scenegraph não conhece os tipos reais do resto do projeto; só
//	conhece DeviceRef. Quem faz a ponte é o Serializer.
type DeviceRef interface {
	// ID returns the stable identifier of the device. Must be unique
	// across all devices registered with the same Graph.
	ID() string

	// Kind returns the device's Kind. Must be constant for the lifetime
	// of the device.
	Kind() Kind

	// OuterRect returns the current border 1 of the device (world
	// coordinates).
	OuterRect() Rect

	// InnerRect returns the current border 3 of the device, or nil if
	// the device is not Complex. For Complex devices, returning nil is a
	// programming error.
	InnerRect() *Rect

	// MoveBy translates the device by (dx, dy) in world coordinates.
	// After this call, OuterRect (and InnerRect, if Complex) must
	// reflect the new position. Implementations should also take care of
	// visual side-effects (warning mark, wire routing, ornament redraw).
	//
	// If the device cannot be moved, MoveBy should be a no-op.
	MoveBy(dx, dy float64)
}
