package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The Component sets setupSystem spawns: the camera, the spinning box, and the
// box standing still behind it.
type (
	eye struct {
		Place  m.Transform
		Camera scene.Camera
	}
	spinningBox struct {
		Place m.Transform
		Draw  scene.Mesh
		Tint  scene.Params
		Spin  Spin
	}
	restingBox struct {
		Place m.Transform
		Draw  scene.Mesh
		Tint  scene.Params
	}
)

var (
	clearColor = m.NewColorSrgb(0.06, 0.07, 0.09, 1)
	spinColor  = m.NewColorSrgb(0.42, 0.71, 0.94, 1)
	restColor  = m.NewColorSrgb(0.94, 0.55, 0.35, 1)
)

// clearDepth is the far plane. Clearing to zero would clear to the near plane
// and hide the whole scene.
const clearDepth float32 = 1

// setupSystem bakes the unit box through model's lookup and spawns the camera
// and the two boxes, once, on the init event. From here on nothing is spawned
// or despawned.
func setupSystem(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	eyes *ecs.Spawn[eye],
	spinning *ecs.Spawn[spinningBox],
	resting *ecs.Spawn[restingBox],
) {
	vertices, indices := boxGeometry()
	box := model.NewLookupAccess(k, lookup.Get()).BakeMesh(vertices, indices, gfx.TopologyTriangleList)

	eyes.New(eye{
		Place: m.LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
		Camera: scene.Camera{
			ID:   CameraMain,
			FovY: 1.0472,
			Near: 0.1,
			Far:  100,
			// A box drawn with the bundled PBR is lit, so a camera with no
			// sun and no ambient renders it black. These fields are the floor
			// of lighting.
			SunDirection:  m.Vec3{X: -0.3, Y: -1, Z: -0.2},
			SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
			AmbientSky:    m.NewColorSrgb(0.18, 0.22, 0.3, 1),
			AmbientGround: m.NewColorSrgb(0.1, 0.09, 0.08, 1),
			// One pass, the forward pass a zero Tag reads as, clearing both
			// colour and depth: there is nothing else in the frame to clear it.
			Passes: m.NewList(scene.Pass{ClearColor: m.Some(clearColor), ClearDepth: m.Some(clearDepth)}),
		},
	})
	spinning.New(spinningBox{
		Place: m.At(0, 0, 0),
		Draw:  scene.Mesh{Ref: box},
		Tint:  tint(spinColor),
	})
	// A second, smaller box behind the first: depth testing is only visible
	// when something can be behind something else.
	resting.New(restingBox{
		Place: m.At(-1.2, 0, -1.2).WithScale(0.6),
		Draw:  scene.Mesh{Ref: box},
		Tint:  tint(restColor),
	})
}

// tint paints a box through the bundled PBR's base colour, the one number of
// the material a colour needs.
func tint(color m.Color) scene.Params {
	return scene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", color))}
}

// boxGeometry is the unit cube about the origin in the standard vertex, four
// vertices a face so every face keeps its own flat normal, wound
// counter-clockwise seen from outside, which is the bundled PBR's front face.
func boxGeometry() ([]model.Vertex, []uint32) {
	faces := [...]struct{ normal, right, up m.Vec3 }{
		{m.Vec3{X: 1}, m.Vec3{Z: -1}, m.Vec3{Y: 1}},
		{m.Vec3{X: -1}, m.Vec3{Z: 1}, m.Vec3{Y: 1}},
		{m.Vec3{Y: 1}, m.Vec3{X: 1}, m.Vec3{Z: -1}},
		{m.Vec3{Y: -1}, m.Vec3{X: 1}, m.Vec3{Z: 1}},
		{m.Vec3{Z: 1}, m.Vec3{X: 1}, m.Vec3{Y: 1}},
		{m.Vec3{Z: -1}, m.Vec3{X: -1}, m.Vec3{Y: 1}},
	}
	vertices := make([]model.Vertex, 0, 24)
	indices := make([]uint32, 0, 36)
	for _, face := range faces {
		base := uint32(len(vertices))
		centre := face.normal.MulS(0.5)
		for _, corner := range [...][2]float32{{-0.5, -0.5}, {0.5, -0.5}, {0.5, 0.5}, {-0.5, 0.5}} {
			vertices = append(vertices, model.Vertex{
				Position: centre.Add(face.right.MulS(corner[0])).Add(face.up.MulS(corner[1])),
				Normal:   face.normal,
				Tangent:  m.Vec4{X: face.right.X, Y: face.right.Y, Z: face.right.Z, W: 1},
				UV0:      m.Vec2{X: corner[0] + 0.5, Y: 0.5 - corner[1]},
				Color:    m.White,
			})
		}
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}
	return vertices, indices
}
