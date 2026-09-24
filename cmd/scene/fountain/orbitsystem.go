package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

type cameraQuery struct {
	Place  *m.Transform
	Camera scene.Camera
}

// orbit carries the camera round the basin on the demo clock.
func orbit(q *ecs.Query[cameraQuery], state *ecs.Read[*Fountain]) {
	t := state.Get().spray.Clock()
	for _, it := range q.All() {
		*it.Place = CameraPlace(t)
	}
}
