package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

type orbitQuery struct {
	Place  *m.Transform
	Camera scene.Camera
}

// orbit places the camera where the keys have left it on its orbit.
func orbit(q *ecs.Query[orbitQuery], state *ecs.Read[*Procedural]) {
	place := state.Get().eye()
	for _, it := range q.All() {
		*it.Place = place
	}
}
