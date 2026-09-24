package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

type stationQuery struct {
	Station Station
}

// unload gives up whatever the last key press asked for, and then marks every
// Model naming a freed path changed.
//
// The mark is the other half of the lever, and it is not optional. scene's load
// System keys a Model once, on change, and keeps the handle it resolved; a
// handle names a slot, and after an unload that slot is free to be reissued to
// the next model loaded. So an unload that left the Models untouched would
// leave scene drawing whatever lands in the slot next. Marking them changed
// before the load System runs makes it key them afresh this tick, which loads
// the file again: a free followed by a touch is a reload, and nothing scene
// draws ever names a freed model.
//
// It holds the device facade's three resources because the texture unloads
// free a GPU texture at the call and the retry preloads.
func unload(
	k kernel.Kernel,
	placed *ecs.Query[stationQuery],
	models *ecs.Set[scene.Model],
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	state *ecs.Write[*Residency],
) {
	r := state.Get()
	pending := r.pending
	if pending == (pendingUnload{}) {
		return
	}
	r.pending = pendingUnload{}
	la := model.NewLookupAccess(k, lookup.Get())
	device := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())

	var freed [len(stations)]bool
	freePath := func(path string) {
		for i := range stations {
			if stations[i].path == path {
				freed[i] = true
			}
		}
	}
	if pending.all {
		device.UnloadAll()
		for i := range freed {
			freed[i] = true
		}
	}
	if pending.model || pending.texture {
		// The truck's geometry, baked poses and material records. Not its
		// textures: with no refcount the lookup cannot know whether another
		// resident model binds the same image by path, so freeing one that is
		// still bound would be a dead texture in a live bind group rather
		// than a missing picture.
		la.UnloadModel(pathTruck)
		freePath(pathTruck)
	}
	if pending.texture {
		// The separate, deliberate lever. For a glb the path names the
		// container, so this releases every image embedded in it. It is only
		// ever pulled with the model already gone, because a resident model
		// still binds the textures it frees.
		device.UnloadTexture(pathTruck)
	}
	if pending.retry {
		// The only retry there is. A failed path clears here and nowhere else,
		// and Preload is what asks again - there is no Retry, because a Retry
		// that did not first free would be a second name for the idempotent
		// load that already exists. The two calls sit in one System because
		// freeing is immediate: the preload behind the unload loads afresh.
		// All three fail again: they are still broken.
		for _, path := range []string{pathTruncated, pathMissing, pathInvalid} {
			la.UnloadModel(path)
			device.Preload(path)
			freePath(path)
		}
	}

	for e, it := range placed.All() {
		if freed[it.Station.Index] {
			models.MarkChanged(e)
			r.reloads++
		}
	}
}
