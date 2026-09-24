package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

type eyeQuery struct {
	Place  *m.Transform
	Camera scene.Camera
}

// eyeSystem places the camera on its orbit, looking at orbitTarget. A Camera
// is placed by its Entity's Transform like anything else in the frame.
func eyeSystem(q *ecs.Query[eyeQuery], state *ecs.Read[*State]) {
	place := state.Get().eyePlace()
	for _, it := range q.All() {
		*it.Place = place
	}
}
