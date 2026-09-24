package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// residency asks the facade what it knows about each file: whether it is
// drawable yet, and what its baked animation costs. The HUD prints both.
//
// Clips' ok is the drawable predicate: it is false for a file that never loaded
// and for one that failed alike, and it is the apt one here, because a demo
// that cannot name a clip has nothing to play.
//
// Two facades over the one Lookup, because the two totals need neither the
// filesystem nor the queue and therefore sit on the half that costs a caller
// one resource. This System holds all three only because every per-model query
// loads the file it names.
func residency(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	state *ecs.Write[*Demo],
) {
	queue := resources.Get()
	if queue == nil || !queue.Ready() {
		return
	}
	d := state.Get()
	la := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), queue)
	for i, path := range ModelPaths {
		d.clips, d.resident[i] = la.Clips(path, d.clips[:0])
		d.memory.pose[i], _ = la.PoseBytes(path)
		d.memory.morph[i], _ = la.MorphBytes(path)
	}
	totals := model.NewLookupAccess(k, lookup.Get())
	d.memory.totalPose = totals.TotalPoseBytes()
	d.memory.totalMorph = totals.TotalMorphBytes()
}
