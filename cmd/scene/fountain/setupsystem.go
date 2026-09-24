package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// setup bakes the two meshes through model's lookup and spawns everything that
// is not a mote. It runs once, on the init event.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	state *ecs.Write[*Fountain],
	nozzles *ecs.Spawn[nozzle],
	foxes *ecs.Spawn[fox],
	basins *ecs.Spawn[basin],
	lamps *ecs.Spawn[lamp],
	eyes *ecs.Spawn[eye],
) {
	f, la := state.Get(), model.NewLookupAccess(k, lookup.Get())
	cubeVertices, cubeIndices := CubeGeometry()
	f.cube = la.BakeMesh(cubeVertices, cubeIndices, gfx.TopologyTriangleList)
	discVertices, discIndices := DiscGeometry()
	f.disc = la.BakeMesh(discVertices, discIndices, gfx.TopologyTriangleList)

	nozzles.New(nozzle{
		Place: NozzlePlace(),
		Model: scene.Model{Ref: model.ModelRef{Path: NozzlePath}},
	})
	foxes.New(fox{
		Place: FoxPlace(0),
		Model: scene.Model{Ref: model.ModelRef{Path: FoxPath}},
	})
	basins.New(basin{
		Draw:  scene.Mesh{Ref: f.disc, Bounds: BasinBounds},
		Shade: basinMaterial(),
	})
	lamps.New(lamp{
		Place: m.Transform{Position: m.Vec3{Y: LampHeight}},
		Light: scene.Light{Descr: model.LightDescr{
			Kind: model.LightPoint, Color: LampColor,
			Intensity: LampIntensity, Range: LampRange,
		}},
	})
	lamps.New(lamp{
		Place: SpotPlace(0),
		Light: scene.Light{Descr: model.LightDescr{
			Kind: model.LightSpot, Color: SpotColor, Intensity: SpotIntensity,
			InnerCone: SpotInner, OuterCone: SpotOuter,
		}},
	})
	eyes.New(eye{Place: CameraPlace(0), Camera: camera()})
}
