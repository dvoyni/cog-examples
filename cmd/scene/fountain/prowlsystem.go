package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

type (
	foxQuery struct {
		Place *m.Transform
		Gait  *model.ClipMachine
		Pose  *scene.Animation
	}
	spotQuery struct {
		Place *m.Transform
		Light scene.Light
	}
)

// prowl walks the fox round its circle, steps its gait machine, and keeps the
// spot on it.
//
// The machine is stepped here, by the game, because scene is stateless
// about animation: the gait's clock is the ground the fox covered, which only
// the game knows, and the machine's plays are copied into the Animation
// scene draws.
func prowl(foxes *ecs.Query[foxQuery], lights *ecs.Query[spotQuery], state *ecs.Read[*Fountain]) {
	t := state.Get().spray.Clock()
	for _, it := range foxes.All() {
		*it.Place = FoxPlace(t)
		FoxGait(it.Gait, t)
		it.Gait.PlaysInto(&it.Pose.Plays)
	}
	for _, it := range lights.All() {
		if it.Light.Descr.Kind == model.LightSpot {
			*it.Place = SpotPlace(t)
		}
	}
}
