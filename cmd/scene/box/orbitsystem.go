package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

type orbitQuery struct {
	Place *m.Transform
	Orbit Orbit
}

// orbitSystem carries the sphere, and the lamp riding over it, round the
// origin on the demo clock. Both are Orbit Entities, so the lamp stays over
// the sphere by construction rather than by a second copy of the path.
func orbitSystem(q *ecs.Query[orbitQuery], state *ecs.Read[*State]) {
	center := state.Get().spinCenter()
	for _, it := range q.All() {
		it.Place.Position = center.Add(m.Vec3{Y: it.Orbit.Lift})
	}
}
