package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// impostorSet is the second camera D spawns.
type impostorSet struct {
	Place    m.Transform
	Camera   scene.Camera
	Impostor Impostor
}

type impostorQuery struct {
	_ ecs.With[Impostor]
}

// duplicate spawns a second Camera under CameraMain's id while D is held, and
// despawns it when D is let go.
//
// A Camera's id is its identity, not a free parameter, so two Entities holding
// one means two Systems each believe they own that camera: scene reports it and
// draws one of them. The impostor is deliberately absurd - looking at the
// ground from underneath - so that which one scene drew is a thing the picture
// says rather than a thing this comment says. Which one that is follows scene's
// walk of its Camera Store, which the ECS leaves unspecified; what is specified
// is that one is drawn and the other reported.
func duplicate(
	impostors *ecs.Query[impostorQuery],
	spawn *ecs.Spawn[impostorSet],
	entities *ecs.WriteableEntities,
	state *ecs.Read[*State],
) {
	held := state.Get().duplicate
	present := false
	for e := range impostors.All() {
		if held {
			present = true
			continue
		}
		entities.Despawn(e)
	}
	if held && !present {
		spawn.New(impostorSet{
			Place:  m.LookAt(m.Vec3{Y: -4}, m.Vec3{}, m.Vec3{Y: 1}),
			Camera: mainCamera(),
		})
	}
}
