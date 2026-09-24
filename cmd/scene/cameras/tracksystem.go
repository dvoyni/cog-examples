package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

type riderQuery struct {
	Place *m.Transform
	_     ecs.With[Rider]
}

// track stands the main camera and everything riding it where the track puts
// the camera at this step.
//
// The eye and the frustum lines are shapes in the camera's own space, so moving
// them is writing one m.Transform each: their geometry never changes, and scene
// never rebakes it.
func track(riders *ecs.Query[riderQuery], state *ecs.Read[*State]) {
	place := mainPlace(state.Get().time())
	for _, it := range riders.All() {
		*it.Place = place
	}
}
