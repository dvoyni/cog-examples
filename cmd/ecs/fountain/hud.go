package main

import (
	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

type (
	moteCount   struct{ Age fountain.Life }
	foxCount    struct{ Gait ecsscene.Animation }
	lightCount  struct{ Light ecsscene.Light }
	cameraCount struct{ Camera ecsscene.Camera }
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

	f.counted = fountain.HUD{Step: f.spray.Step(), Spawned: f.spawned, Retired: f.retired}
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

// rearm collects the snapshot of the tick that has just ended and arms the
// next one. It is a plain handler rather than a System, because a System
// dispatches no command.
func rearm() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var state kernel.Write[*Fountain]
	var arm func(kernel.Kernel, gfx.ArmFrameRequest) gfx.ArmFrameResponse
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*Fountain]()
			arm = access.Uses[gfx.ArmFrameCmd]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) {
			state.Get().snapshots.Rearm(k, arm)
		}
}

// HUDCmd answers what the HUD is showing and the frame snapshot it read, so a
// test can read both.
type HUDCmd kernel.Command[HUDRequest, Shown]

type HUDRequest struct{}

// Shown is the HUD on screen, and the last frame snapshot that came back.
type Shown struct {
	HUD      fountain.HUD
	Snapshot gfx.FrameSnapshot
}

func readHUD(state *ecs.Read[*Fountain], answer *ecs.Resp[Shown]) {
	f := state.Get()
	answer.Set(Shown{HUD: f.shown, Snapshot: f.snapshots.Latest()})
}
