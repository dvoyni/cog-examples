package main

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Fountain is the state of the fountain itself, shared by the Systems as a
// resource: the spray, the meshes baked at startup, the tallies, what the HUD
// shows and the frame snapshot it reads.
type Fountain struct {
	spray Spray

	// rigged is whether rigSystem has given the fox its gait machine.
	rigged bool

	cube, disc model.MeshRef

	spawned, retired int

	counted HUD // this step's census, shown beside the next step's snapshot
	shown   HUD

	snapshots Snapshots
}

// throw decides one mote and dresses it as an Entity: the spray decides where
// it is, how it moves and how long it lasts, and the Components say how it is
// drawn.
func (f *Fountain) throw() mote {
	born := f.spray.Throw()
	return mote{
		Place:  born.Place,
		Draw:   scene.Mesh{Ref: f.cube, Bounds: MoteBounds},
		Shade:  moteMaterial(),
		Tint:   scene.Params{Values: m.NewList(gfx.ColorParam(MoteTintParam, Tint(born.Age)))},
		Motion: born.Motion,
		Age:    born.Age,
	}
}
