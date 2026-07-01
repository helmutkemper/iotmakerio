# /ide/scenegraph/readme.md

# scenegraph — live spatial relationships for the IoTMaker IDE

This package is the single source of truth for spatial relationships
between devices on the stage: who contains whom, who overlaps whom,
which devices travel together when a container is dragged, what the
floor and ceiling are when a container is resized.

## Why this package exists

Before `scenegraph`, the same questions were answered in three
different places (the serializer, each container device, and the
workspace) using slightly different criteria. Drag, resize and
serialization operated on inconsistent views of the same facts. When
devices were nested, the IDE dropped into wrong states: warning marks
on the wrong device, children jumping to the parent's size, children
not following their container.

The fix was to extract the rule engine and the live state into a
single small package with no upward dependencies. Every other part
of the IDE — the serializer, the workspace, the container devices —
now asks `scenegraph` rather than computing on its own.

For the full design discussion, see:

- `/ide/docs/ARCHITECTURE_SCENEGRAPH.md` — five-minute design summary
- `/ide/docs/SCENEGRAPH_SPEC.md` — implementer reference

## The rule, in one sentence

Border 1 of A must be entirely outside border 1 of B, or — if B is
Complex — border 1 of A must be entirely inside border 3 of B. Any
other configuration is an error.

Three kinds of device exist: `Simple` and `Fitting` (borders 1 and 2,
cannot hold children, cannot overlap anyone), and `Complex` (borders
1, 2 and 3, can hold children).

Three kinds of error exist, detected by `classifyPair`:
`ConflictOverlap`, `ConflictStraddle`, `ConflictPiercedOuter`.

## What's in this package

```
kind.go        Kind enum (Simple | Fitting | Complex), classification helpers.
rect.go        Rect type, ContainsRect / IntersectsRect / Area / Union.
node.go        Node, NodeView, InteractionState, ConflictKind, Conflict.
deviceref.go   DeviceRef — the minimal interface the graph needs to
               talk to a device.
observer.go    Observer interface + NoopObserver.
rules.go       Pure geometric functions: classifyPair, findParent,
               findConflicts. Testable in isolation.
snap.go        Fitting snap placeholder. Extension point for later.
graph.go       The Graph type: public lifecycle + query surface.
rules_test.go  Unit tests for the pure functions (29 cases).
graph_test.go  Integration tests that drive Graph end-to-end.
```

## Quick tour of the API

A workspace owns one `*scenegraph.Graph` and one observer:

```go
g := scenegraph.NewGraph()
g.SetObserver(myObserver)  // paints red borders, triggers JSON regen
```

Each device implements `scenegraph.DeviceRef` and registers once:

```go
g.Register(myDeviceRef)    // initial parent + conflicts computed
// ... later
g.Unregister("device_id")  // clean removal; children re-parented
```

During a drag gesture the device calls:

```go
g.BeginDrag(id)   // mousedown — snapshots descendants if Complex
g.UpdateDrag(id)  // mousemove — refreshes geometry, fires conflicts
g.EndDrag(id, dx, dy)  // mouseup — moves descendants, reassigns parent
```

During a resize gesture:

```go
g.BeginResize(id)  // caches floor (children) and ceiling (parent)
// UpdateResize is not exposed yet; the device uses ChildrenBounds and
// ParentInnerRect queries directly to compute clamps. See the device
// rewrite of compLoop/statementLoop.go for the pattern.
g.EndResize(id)
```

Queries the consumer will use frequently:

```go
g.ChildrenOf(id)        // direct children of a Complex
g.Descendants(id)       // direct and indirect descendants (depth-first)
g.ParentOf(id)          // parent ID or ""
g.ChildrenBounds(id)    // union of children's border 1 (nil if none)
g.ParentInnerRect(id)   // parent's border 3 (nil if root)
g.FindParent(outer, excludeID)  // smallest Complex that contains outer
g.FindConflicts(id)     // current conflicts involving id
g.Snapshot()            // full read-only view for the serializer
```

## What's intentionally not here

- **JSON serialisation** — that is `scene/serializer.go`'s job; it
  consumes `Graph.Snapshot()` and produces `DeviceJSON`.
- **Padding arithmetic** — that lives in `rulesContainer`; the
  scenegraph asks devices for their outer/inner rectangles and doesn't
  care how they were derived.
- **Sprite rendering, warning marks, wire routes** — the
  `DeviceRef.MoveBy` method delegates those to the device itself. The
  scenegraph only moves logical rectangles.
- **Goroutine safety** — the IDE runs single-threaded (WASM main
  loop). Do not call `scenegraph` methods from multiple goroutines
  without external synchronisation.

## How to test changes

```bash
cd /ide
go test ./scenegraph/...
```

The tests do not depend on the sprite layer, the WASM runtime, or the
browser; they run in plain CI. `graph_test.go` uses an in-memory
`fakeDevice` that implements `DeviceRef` and mirrors geometry
changes, so the full lifecycle can be exercised deterministically.

## Extending

**New conflict kinds** — add the constant in `node.go`, emit it from
`classifyPair` in `rules.go`, add test cases in `rules_test.go`.
Nothing else in the codebase needs to change.

**New device kinds** — add the constant in `kind.go`, update the
`CanContain` / `CanBeChild` methods, update `classifyPair` to cover
the new pair combinations, add tests. No touch to `graph.go`.

**Fitting snap** — the entry point is `applyFittingSnap` in `snap.go`.
It is called unconditionally from `EndDrag` but is currently a no-op.
When the Fitting feature ships, the implementation goes entirely in
`snap.go`.
