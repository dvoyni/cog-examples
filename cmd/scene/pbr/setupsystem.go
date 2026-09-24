package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// setup bakes the two meshes through model's lookup and spawns everything in
// the still life but the lamps: the ground, six plinths, seven Model Entities
// and the camera. It runs once, on the init event.
//
// The models draw nothing until scene's load System has loaded them, and that
// is all an absent model costs: the HUD's residency count is what says so.
// The lamps wait in lampSystem for the one file whose lights they include.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	slabs *ecs.Spawn[slab],
	exhibits *ecs.Spawn[exhibit],
	eyes *ecs.Spawn[eye],
	state *ecs.Read[*Pbr],
) {
	la := model.NewLookupAccess(k, lookup.Get())
	boxVertices, boxIndices := boxGeometry()
	box := la.BakeMesh(boxVertices, boxIndices, gfx.TopologyTriangleList)
	quadVertices, quadIndices := quadGeometry()
	quad := la.BakeMesh(quadVertices, quadIndices, gfx.TopologyTriangleList)

	// The colour rides in Params rather than in a Material, so the six plinths
	// share one key and are one Batch, and the ground's differs only there.
	slabs.New(slab{
		Place: m.Transform{Scale: m.Vec3{X: groundSide, Y: 1, Z: groundSide}},
		Draw:  scene.Mesh{Ref: quad},
		Tint:  tint(groundColor),
	})
	for i := range placements {
		slabs.New(slab{
			Place: placements[i].plinth,
			Draw:  scene.Mesh{Ref: box},
			Tint:  tint(plinthColor),
		})
		exhibits.New(exhibit{
			Place: placements[i].model,
			Model: scene.Model{Ref: model.ModelRef{Path: stations[i].path}},
		})
	}

	// The alpha station's second copy, at its own depth. Two copies is what
	// makes the blend sort's back-to-front order observable: one copy's two
	// blended primitives cannot separate a depth sort from a mesh-id sort.
	second := placements[stationAlpha].model
	second.Position = second.Position.Add(alphaSecondCopy)
	exhibits.New(exhibit{
		Place: second,
		Model: scene.Model{Ref: model.ModelRef{Path: stations[stationAlpha].path}},
	})

	eyes.New(eye{Place: state.Get().place(), Camera: camera()})
}

// tint is a slab's colour, as the bundled PBR's base colour factor.
func tint(color m.Color) scene.Params {
	return scene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", color))}
}
