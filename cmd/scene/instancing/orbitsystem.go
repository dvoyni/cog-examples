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

// orbit places the camera on its orbit. It is the one System that moves an
// Entity, so it runs before scene.RecordOnUpdate and a step draws the pose
// that step decided.
func orbit(cameras *ecs.Query[orbitQuery], state *ecs.Read[*Demo]) {
	place := state.Get().place()
	for _, it := range cameras.All() {
		*it.Place = place
	}
}
