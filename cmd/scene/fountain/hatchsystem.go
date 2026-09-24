package main

import "github.com/dvoyni/cog/bundles/ecs"

// hatch advances the spray and throws the motes this step is owed.
func hatch(motes *ecs.Spawn[mote], state *ecs.Write[*Fountain]) {
	f := state.Get()
	for range f.spray.Advance() {
		motes.New(f.throw())
		f.spawned++
	}
}
