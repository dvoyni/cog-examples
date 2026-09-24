package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// bounds adds the model to the pick list the first step model answers Bounds
// for its file, and does nothing after that.
//
// The model is the one pickable whose bounds are the file's, which the app does
// not know and cannot hard-code without going stale the first time the asset
// changes. So it asks model's device facade, whose Bounds loads the file it
// names, and puts the local sphere through m.Sphere.Transform - exact under the
// uniform scale a m.Transform carries. A file that could not be loaded is simply
// not in the list, which is the right answer: a click cannot pick what is not
// drawn.
func bounds(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	state *ecs.Write[*State],
) {
	s := state.Get()
	if s.resident || resources.Get() == nil || !resources.Get().Ready() {
		return
	}
	la := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())
	sphere, ok := la.Bounds(model.ModelRef{Path: modelPath})
	if !ok {
		return
	}
	local := m.Sphere{Center: m.Vec3{X: sphere.X, Y: sphere.Y, Z: sphere.Z}, Radius: sphere.W}
	s.targets = append(s.targets, pickable{name: "model", sphere: local.Transform(modelTransform().Mat4())})
	s.resident = true
}
