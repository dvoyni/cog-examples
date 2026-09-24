package main

import (
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
)

type (
	moteCount   struct{ Age Life }
	foxCount    struct{ Gait scene.Animation }
	lightCount  struct{ Light scene.Light }
	cameraCount struct{ Camera scene.Camera }
)

// hud counts the world and prints it. A tick's frame snapshot comes back at
// that tick's very end, so what is shown is the previous step's census beside
// the snapshot of the frame that step drew, and this step's census waits a
// tick.
func hud(
	motes *ecs.Query[moteCount],
	foxes *ecs.Query[foxCount],
	lights *ecs.Query[lightCount],
	cameras *ecs.Query[cameraCount],
	state *ecs.Write[*Fountain],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	f := state.Get()
	f.shown = f.counted.WithFrame(f.snapshots.Latest().Frame)

	f.counted = HUD{Step: f.spray.Step(), Spawned: f.spawned, Retired: f.retired}
	for range motes.All() {
		f.counted.Motes++
	}
	for range foxes.All() {
		f.counted.Foxes++
	}
	for range lights.All() {
		f.counted.Lights++
	}
	for range cameras.All() {
		f.counted.Cameras++
	}

	f.shown.Draw(overlay.Get(), layerHUD, f.spray.Step())
}
