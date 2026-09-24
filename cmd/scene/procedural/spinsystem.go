package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// spin turns the beacon on the demo clock. The stray does not turn: it is never
// on screen at the reference pose, and a still copy keeps its one job, being
// culled, plain.
func spin(places *ecs.Set[m.Transform], state *ecs.Read[*Procedural]) {
	p := state.Get()
	if place, ok := places.Ref(p.beaconEntity); ok {
		*place = beaconPlace(p.time())
	}
}
