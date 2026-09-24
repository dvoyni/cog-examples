package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The Component sets: one per kind of thing setup spawns.
type (
	groundSet struct {
		Place m.Transform
		Shape scene.DebugPlane
	}
	cubeSet struct {
		Place m.Transform
		Shape scene.DebugBox
		Cube  Cube
	}
	obeliskSet struct {
		Place m.Transform
		Draw  scene.Mesh
		Shade scene.Material
	}
	propSet struct {
		Place m.Transform
		Model scene.Model
	}
	mainCameraSet struct {
		Place  m.Transform
		Camera scene.Camera
		Rider  Rider
	}
	mapCameraSet struct {
		Place  m.Transform
		Camera scene.Camera
	}
	eyeSet struct {
		Place m.Transform
		Shape scene.DebugSphere
		Rider Rider
	}
	frustumSet struct {
		Place m.Transform
		Shape scene.DebugLine
		Rider Rider
	}
	outlineSet struct {
		Place   m.Transform
		Shape   scene.DebugWireBox
		Outline Outline
	}
)

// riders is how many Entities ride the main camera: the camera, its eye, and
// the frustum outline's four edges out of the eye and four across the far end.
const riders = 1 + 1 + 8

// The overlay's line widths and the eye's size, in world units: the minimap
// shows 26 units across 400 canvas units, so these are a couple of pixels.
const (
	overlayWidth = 0.05
	eyeRadius    = 0.22
)

// setup bakes the obelisk and spawns the whole world, once, on the init event.
//
// Everything after this edits Components in place. Nothing is spawned per
// frame except the impostor camera, and nothing drawn is rebuilt: the cubes
// change colour, the riders move, and the wire box changes size when the pick
// does.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	state *ecs.Write[*State],
	grounds *ecs.Spawn[groundSet],
	cubeSpawn *ecs.Spawn[cubeSet],
	obelisks *ecs.Spawn[obeliskSet],
	props *ecs.Spawn[propSet],
	mainCameras *ecs.Spawn[mainCameraSet],
	mapCameras *ecs.Spawn[mapCameraSet],
	eyes *ecs.Spawn[eyeSet],
	frusta *ecs.Spawn[frustumSet],
	outlines *ecs.Spawn[outlineSet],
) {
	s := state.Get()
	spawned := 0

	grounds.New(groundSet{Shape: scene.DebugPlane{
		Normal: m.Vec3{Y: 1},
		Size:   m.Vec2{X: groundSide, Y: groundSide},
		Color:  groundColor,
		Layers: LayerWorld,
	}})
	spawned++

	for i := range cubes {
		cubeSpawn.New(cubeSet{
			Place: cubeTransform(i),
			Shape: scene.DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: cubes[i].color, Layers: LayerWorld},
			Cube:  Cube{Index: i},
		})
		spawned++
	}

	vertices, indices := obeliskMesh()
	obelisks.New(obeliskSet{
		Place: obeliskTransform(),
		Draw: scene.Mesh{
			Ref:    model.NewLookupAccess(k, lookup.Get()).BakeMesh(vertices, indices, gfx.TopologyTriangleList),
			Layers: LayerWorld,
		},
		Shade: obeliskMaterial,
	})
	spawned++

	props.New(propSet{
		Place: modelTransform(),
		Model: scene.Model{Ref: model.ModelRef{Path: modelPath}, Layers: LayerWorld},
	})
	spawned++

	// The three cameras. Their Passes are left empty here and written by the
	// target System every frame before scene reads them, because a pass's
	// target is a frame-local texture that does not exist yet.
	place := mainPlace(s.time())
	mainCameras.New(mainCameraSet{Place: place, Camera: mainCamera()})
	mapCameras.New(mapCameraSet{Place: mapPlace(), Camera: mapCamera(CameraMap, LayerWorld)})
	mapCameras.New(mapCameraSet{Place: mapPlace(), Camera: mapCamera(CameraOverlay, LayerOverlay)})
	spawned += 3

	// The overlay: the eye and the frustum, in the main camera's own space and
	// standing where it stands, and the wire box, which draws nothing until
	// something is picked because a box of no size is a shape with nothing to
	// draw.
	eyes.New(eyeSet{Place: place, Shape: scene.DebugSphere{Radius: eyeRadius, Color: eyeColor, Layers: LayerOverlay}})
	spawned++
	if corners, ok := frustumCorners(); ok {
		for i, corner := range corners {
			next := corners[(i+1)%len(corners)]
			for _, edge := range [2][2]m.Vec3{{{}, corner}, {corner, next}} {
				frusta.New(frustumSet{Place: place, Shape: scene.DebugLine{
					From: edge[0], To: edge[1], Width: overlayWidth, Color: frustumColor, Layers: LayerOverlay,
				}})
				spawned++
			}
		}
	}
	outlines.New(outlineSet{Shape: scene.DebugWireBox{Width: overlayWidth, Color: highlightColor, Layers: LayerOverlay}})
	spawned++

	// The pick list starts with what the app knows the bounds of. The model
	// joins it once model can say where its file's bounds are.
	s.targets = s.targets[:0]
	for i := range cubes {
		s.targets = append(s.targets, pickable{name: cubes[i].name, sphere: cubeSphere(i)})
	}
	s.targets = append(s.targets, pickable{name: "obelisk", sphere: obeliskSphere()})
	s.spawned = spawned
}
