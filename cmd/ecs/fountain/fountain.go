package main

import (
	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Fountain is the state of the fountain itself, shared by the Systems as a
// resource: the spray, the meshes baked at startup, the tallies, what the HUD
// shows and the frame snapshot it reads.
type Fountain struct {
	spray fountain.Spray

	cube, disc scene.MeshRef

	spawned, retired int

	counted fountain.HUD // this step's census, shown beside the next step's snapshot
	shown   fountain.HUD

	snapshots fountain.Snapshots
}

// throw decides one mote and dresses it as an Entity: the spray decides where
// it is, how it moves and how long it lasts, and the Components say how it is
// drawn.
func (f *Fountain) throw() mote {
	born := f.spray.Throw()
	return mote{
		Place:  born.Place,
		Draw:   ecsscene.Mesh{Ref: f.cube, Bounds: fountain.MoteBounds},
		Shade:  moteMaterial(),
		Tint:   ecsscene.Params{Values: m.NewList(gfx.ColorParam(fountain.MoteTintParam, fountain.Tint(born.Age)))},
		Motion: born.Motion,
		Age:    born.Age,
	}
}

// camera is the camera Component: a ground pass that clears colour and depth,
// then the forward pass that clears depth and keeps the ground's colour.
func camera() ecsscene.Camera {
	return ecsscene.Camera{
		ID:            CameraMain,
		FovY:          fountain.FieldOfViewY,
		Near:          fountain.Near,
		Far:           fountain.Far,
		SunDirection:  fountain.SunDirection,
		SunColor:      fountain.SunColor,
		SunIntensity:  fountain.SunIntensity,
		AmbientSky:    fountain.SkyColor,
		AmbientGround: fountain.EarthColor,
		Passes: m.NewList(
			ecsscene.Pass{Tag: tagGround, ClearColor: m.Some(fountain.BackdropColor), ClearDepth: m.Some[float32](1)},
			ecsscene.Pass{Tag: ecsscene.TagForward, ClearDepth: m.Some[float32](1), Order: 1},
		),
	}
}
