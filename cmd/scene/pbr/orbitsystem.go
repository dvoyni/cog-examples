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

// orbit places the camera on the orbit steer left it on. It writes the
// Transform every step, moved or not: scene.RecordOnUpdate reads every
// placement every frame anyway, so skipping an unmoved one would save nothing.
func orbit(cameras *ecs.Query[orbitQuery], state *ecs.Read[*Pbr]) {
	place := state.Get().place()
	for _, it := range cameras.All() {
		*it.Place = place
	}
}
