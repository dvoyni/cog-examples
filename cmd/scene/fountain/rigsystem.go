package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

type rigQuery struct {
	Gait *model.ClipMachine
}

// rig gives the fox its gait machine the first step Fox.glb answers Clips,
// caught up to the steps before this one, which prowl is about to take. It
// holds the device facade's three resources because Clips loads the file it
// names; once the fox is rigged it does nothing.
func rig(
	k kernel.Kernel,
	foxes *ecs.Query[rigQuery],
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	state *ecs.Write[*Fountain],
) {
	f := state.Get()
	if f.rigged || resources.Get() == nil || !resources.Get().Ready() {
		return
	}
	la := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())
	clips, ok := la.Clips(FoxPath, nil)
	if !ok {
		return
	}
	f.rigged = true
	// hatch has already advanced the spray onto this step, and prowl takes it.
	gait, err := FoxGaitAt(clips, f.spray.Step()-1)
	if err != nil {
		k.ReportError(err)
		return
	}
	for _, it := range foxes.All() {
		*it.Gait = gait
	}
}
