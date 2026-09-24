package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// setup bakes the first generation of every mesh and spawns every Entity the
// demo draws. It runs once, on the init event.
//
// BakeMesh mints its ref at once and queues the upload onto model's Lookup for
// scene's load System to drain, so a mesh baked here, before any backend is
// up, is drawable on the first frame that has one; LookupAccess itself never
// touches the GPU.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	state *ecs.Write[*Procedural],
	shapes *ecs.Spawn[shape],
	pieces *ecs.Spawn[piece],
	eyes *ecs.Spawn[eye],
) {
	p, la := state.Get(), model.NewLookupAccess(k, lookup.Get())

	// The two bundled-PBR draws, both of them here to be compared against: two
	// materials reading the same sun out of the same sceneFrame should agree
	// about where it is.
	//
	// The plane alone would not settle that. It has one normal, so it is one
	// brightness however the sun is read, and a shader reading sunDirection out
	// of the wrong offset would still light it. The sphere carries a
	// terminator, which is a direction rather than a level, and it stands at
	// the beacon's own height and radius on the opposite side. Neither declares
	// Bounds: a standard-layout mesh gets its sphere baked from its vertices.
	groundVertices, groundIndices := groundGeometry(groundSide)
	sphereVertices, sphereIndices := sphereGeometry()
	p.bakes += 2
	shapes.New(shape{
		Draw: scene.Mesh{Ref: la.BakeMesh(groundVertices, groundIndices, gfx.TopologyTriangleList)},
		Tint: tint(groundColor),
	})
	shapes.New(shape{
		Place: m.At(referencePosition.X, referencePosition.Y, referencePosition.Z).WithScale(referenceRadius),
		Draw:  scene.Mesh{Ref: la.BakeMesh(sphereVertices, sphereIndices, gfx.TopologyTriangleList)},
		Tint:  tint(referenceColor),
	})

	// The ridge. Bounds is given explicitly because a custom vertex layout gets
	// no baked sphere - scene cannot locate POSITION in bytes it has never seen
	// a layout for - and a Mesh with no bounds at all is never culled.
	vertices, indices := ridgeGeometry(p.ridgeCellCount, 0)
	p.ridge = la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)
	p.bakes++
	p.ridgeEntity = pieces.New(piece{
		Place: m.At(ridgePosition.X, ridgePosition.Y, ridgePosition.Z).WithScale(ridgeScale),
		Draw:  scene.Mesh{Ref: p.ridge, Bounds: m.Vec4{W: ridgeBounds}},
		Shade: sharedMaterial,
	})

	// The ribbon. NeverCull rather than Bounds: it is rebuilt every frame at a
	// size the demo already knows is on screen, so the honest answer is to say
	// so rather than to maintain a number that would only ever be right by
	// accident.
	p.ribbon = p.bakeRibbon(la)
	p.ribbonEntity = pieces.New(piece{
		Place: m.At(ribbonPosition.X, ribbonPosition.Y, ribbonPosition.Z),
		Draw:  scene.Mesh{Ref: p.ribbon, NeverCull: true},
		Shade: sharedMaterial,
	})

	// The beacon, and the stray copy of it behind the camera. One mesh, two
	// Entities, one declared sphere each: the beacon survives the frustum and
	// the stray does not, so the two are one Batch that draws one instance.
	p.beacon = p.bakeBeacon(la)
	p.beaconEntity = pieces.New(piece{
		Place: beaconPlace(0),
		Draw:  scene.Mesh{Ref: p.beacon, Bounds: m.Vec4{W: 1}},
		Shade: sharedMaterial,
	})
	p.strayEntity = pieces.New(piece{
		Place: m.At(strayPosition.X, strayPosition.Y, strayPosition.Z).WithScale(beaconScale),
		Draw:  scene.Mesh{Ref: p.beacon, Bounds: m.Vec4{W: 1}},
		Shade: sharedMaterial,
	})

	// The ghost stands where the beacon does and draws nothing: its ref is the
	// zero one, which scene skips in silence, on every frame but the one a
	// release staled a ref for it to draw.
	p.ghostEntity = pieces.New(piece{
		Place: m.At(beaconPosition.X, beaconPosition.Y, beaconPosition.Z).WithScale(beaconScale),
		Draw:  scene.Mesh{Bounds: m.Vec4{W: 1}},
		Shade: sharedMaterial,
	})

	eyes.New(eye{Place: p.eye(), Camera: camera()})
}

// tint is a bundled-PBR draw's colour, as the parameter the bundled material
// reads it from.
func tint(color m.Color) scene.Params {
	return scene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", color))}
}
