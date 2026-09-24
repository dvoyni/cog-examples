package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The Component sets setup spawns: one per shape of Entity. A station carrying
// a tint or a replacement material is a set of its own, because a spawn set is
// a struct and an optional Component is one it either has or does not.
type (
	eye struct {
		Place  m.Transform
		Camera scene.Camera
	}
	pad struct {
		Place m.Transform
		Draw  scene.Mesh
		Paint scene.Params
	}
	plainStation struct {
		Place   m.Transform
		Model   scene.Model
		Station Station
	}
	tintedStation struct {
		Place   m.Transform
		Model   scene.Model
		Tint    scene.Params
		Station Station
	}
	repaintedStation struct {
		Place   m.Transform
		Model   scene.Model
		Shade   scene.Material
		Station Station
	}
)

// setup bakes the one pad mesh and spawns the whole grid: the camera, sixteen
// pads and sixteen stations. It runs once, on the init event.
//
// Nothing here loads a model. A station's Model names its path and nothing
// more; scene's load System loads it the first tick the backend is up, and the
// preload System asks for every path before that System does.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	state *ecs.Write[*Residency],
	eyes *ecs.Spawn[eye],
	pads *ecs.Spawn[pad],
	plain *ecs.Spawn[plainStation],
	tinted *ecs.Spawn[tintedStation],
	repainted *ecs.Spawn[repaintedStation],
) {
	r := state.Get()
	la := model.NewLookupAccess(k, lookup.Get())
	vertices, indices := padGeometry()
	quad := la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)

	eyes.New(eye{
		Place: m.LookAt(cameraEye, cameraTarget, m.Vec3{Y: 1}),
		// Everything else is left at its zero value: Projection is
		// Perspective, CullMask is every layer, both intensities are 1, and
		// Passes is empty, which is one default pass at the camera's own id.
		Camera: scene.Camera{
			ID:            CameraMain,
			FovY:          fieldOfViewY,
			Near:          nearPlane,
			Far:           farPlane,
			SunDirection:  m.Vec3{X: -0.35, Y: -1, Z: -0.45},
			SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
			AmbientSky:    m.NewColorSrgb(0.30, 0.34, 0.42, 1),
			AmbientGround: m.NewColorSrgb(0.10, 0.09, 0.08, 1),
		},
	})
	r.entities++

	for i := range stations {
		s := &stations[i]
		// The pad is lit paint on the bundled PBR, not a debug shape: debug
		// shapes are self-lit, and a pad that ignored the sun would read as a
		// hole in the ground beside the lit models standing on it.
		colour := padColor
		if !s.loads {
			colour = padFailColor
		}
		pads.New(pad{
			Place: m.Transform{Position: stationPad(s)},
			Draw:  scene.Mesh{Ref: quad},
			Paint: scene.Params{Values: m.NewList(model.PaintParams(nil, colour, false)...)},
		})

		place, drawn, tag := stationPlacement(s), scene.Model{Ref: s.ref()}, Station{Index: i}
		switch {
		case s.repaint:
			// A Material is laid over the file's own, and gfx binds by name, so
			// a shader declaring none of the file's bindings never reads its
			// record: this station takes the repaint shader's constant paint
			// and nothing of the livery.
			repainted.New(repaintedStation{Place: place, Model: drawn, Shade: repaintMaterial(), Station: tag})
		case s.tint != (m.Color{}):
			// Params lay over the file's own material by name. glTF's parameter
			// names are the user-facing contract, so this is the whole of a
			// team colour.
			tinted.New(tintedStation{
				Place: place, Model: drawn, Station: tag,
				Tint: scene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", s.tint))},
			})
		default:
			plain.New(plainStation{Place: place, Model: drawn, Station: tag})
		}
		r.entities += 2
	}
}

// padGeometry is one pad: a padSize square on the ground plane, facing up.
func padGeometry() ([]model.Vertex, []uint32) {
	const h = padSize / 2
	up, tangent, white := m.Vec3{Y: 1}, m.Vec4{X: 1, W: 1}, m.Color{R: 1, G: 1, B: 1, A: 1}
	corner := func(x, z float32) model.Vertex {
		return model.Vertex{Position: m.Vec3{X: x, Z: z}, Normal: up, Tangent: tangent, Color: white}
	}
	vertices := []model.Vertex{corner(-h, -h), corner(-h, h), corner(h, h), corner(h, -h)}
	// Counter-clockwise seen from above, so the face the camera looks down on
	// is the front one.
	return vertices, []uint32{0, 1, 2, 0, 2, 3}
}

// stationPad is the centre of one station's pad, on the ground plane.
func stationPad(s *station) m.Vec3 {
	return m.Vec3{
		X: (float32(s.column) - float32(columns-1)/2) * colSpacing,
		Z: (float32(rows-1)/2 - float32(s.row)) * rowSpacing,
	}
}

// stationPlacement is the Transform one station's model stands at: its own
// scale, lifted so the model's lowest point rests on the pad.
//
// The lift is the file's own minY through the scale, which is why the table
// carries minY: a Node selector re-roots, so the number is the subtree's, not
// the scene's, and the two differ by a whole axis on this asset.
func stationPlacement(s *station) m.Transform {
	pad := stationPad(s)
	return m.At(pad.X, pad.Y-s.minY*s.scale, pad.Z).WithScale(s.scale)
}
