package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// survey asks the facade what it knows, once per tick, for the HUD.
//
// It runs after scene's load System, so a station's state as printed is the
// state its own Model was keyed against this tick. State is the only one of
// these queries that says why a file is not there - every other query answers
// false for a typo and for a broken file alike, and a loading screen watching
// only ok can never print a reason.
//
// Every query here would load the file it names, which is why it holds the
// device facade's three resources; by the time it runs, the preload and the
// load System have already asked for every path, so none of them does.
func survey(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	state *ecs.Write[*Residency],
) {
	if resources.Get() == nil || !resources.Get().Ready() {
		return
	}
	r := state.Get()
	la := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())
	for i := range stations {
		s := la.State(stations[i].path)
		if !r.known[i] || (r.states[i] == nil) != (s == nil) {
			r.settled[i] = r.step
		}
		r.states[i], r.known[i] = s, true
		r.nodes = r.nodes[:0]
		if names, ok := la.Nodes(stations[i].ref(), r.nodes); ok {
			r.nodes = names
			r.nodeCount[i] = len(names)
		} else {
			r.nodeCount[i] = -1
		}
	}
	// The two totals need neither the filesystem nor the queue, so they sit
	// on the half of the facade a caller builds from the Lookup alone.
	totals := model.NewLookupAccess(k, lookup.Get())
	r.poseBytes, r.morphBytes = totals.TotalPoseBytes(), totals.TotalMorphBytes()
}
