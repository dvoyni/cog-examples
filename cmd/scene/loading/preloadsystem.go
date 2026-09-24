package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// preload asks for every distinct path once, on the first update whose backend
// is up, before scene's load System keys a single station.
//
// It is the loading screen in miniature: Preload is the same load the load
// System fires for a Model, fired without one, so an app moves the decode into
// a screen it controls instead of into whichever tick first keys the file.
// Nothing here waits for it - the stations were spawned at init whatever their
// state, because a Model that is not resident is skipped, never substituted,
// and that is what the bare pads show.
//
// It waits for the backend because a load before the backend is up is refused
// rather than cached, and a refused preload would move the decode straight
// back into the load System.
func preload(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	state *ecs.Write[*Residency],
) {
	r := state.Get()
	if r.preloaded || resources.Get() == nil || !resources.Get().Ready() {
		return
	}
	r.preloaded = true
	la := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())
	for _, path := range PreloadOrder {
		la.Preload(path)
	}
}
