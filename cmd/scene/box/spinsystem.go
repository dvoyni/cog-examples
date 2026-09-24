package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

type spinQuery struct {
	Place *m.Transform
	Spin  Spin
}

// spinSystem turns every Spin Entity about its axis through the demo's time.
// Only the rotation is written, so the position and the scalar Scale the box
// was spawned with stay exactly as they were: the TRS case is all three at
// once, and the rotation is the one that moves.
func spinSystem(q *ecs.Query[spinQuery], state *ecs.Read[*State]) {
	angle := state.Get().time()
	for _, it := range q.All() {
		it.Place.Rotation = m.QuatAxisAngle(it.Spin.Axis, angle)
	}
}
