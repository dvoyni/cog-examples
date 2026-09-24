package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

type rigQuery struct {
	Gait *Gait
}

// rig builds the fox's gait machine from Fox.glb's own clips, once, the first
// tick the file answers Clips. The fox is drawn at its rest frame until then,
// and gait steps the new machine up to wherever the clock stands.
//
// It holds the device facade's three resources because Clips loads the file it
// names. A demo pays for that in its own System, which is where the cost of a
// synchronous load belongs; once the fox is rigged it does nothing.
func rig(
	k kernel.Kernel,
	foxes *ecs.Query[rigQuery],
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
) {
	queue := resources.Get()
	if queue == nil || !queue.Ready() {
		return
	}
	var la model.LookupDeviceAccess
	var clips []model.ClipInfo
	for _, it := range foxes.All() {
		if it.Gait.Rigged {
			continue
		}
		if clips == nil {
			la = model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), queue)
			var ok bool
			if clips, ok = la.Clips(foxPath, nil); !ok {
				return
			}
		}
		// Rigged even on a failure: a file whose clips do not make the machine
		// will not make it next tick either, and one report is the lesson.
		it.Gait.Rigged = true
		machine, err := newFoxGait(clips)
		if err != nil {
			k.ReportError(err)
			continue
		}
		it.Gait.Machine, it.Gait.Start, it.Gait.Step = machine, machine, 0
	}
}

// The fox's gait machine: Walk and Run, each fading to the other on a trigger.
// The fade is long against both clips - Walk runs 0.71s and Run 1.16s - so the
// gait cycles inside it and it reads as a change of gait rather than as a
// stutter.
const (
	gaitFade    = 1.2
	triggerRun  = "run"
	triggerWalk = "walk"
)

// newFoxGait builds the fox's gait machine over the clips Fox.glb declares.
func newFoxGait(clips []model.ClipInfo) (model.ClipMachine, error) {
	return model.NewClipMachine(clips,
		[]model.ClipState{
			{Name: foxWalk, Clip: foxWalk, Loop: true},
			{Name: foxRun, Clip: foxRun, Loop: true},
		},
		[]model.ClipTransition{
			{From: foxWalk, To: foxRun, On: triggerRun, Crossfade: gaitFade, Ease: model.EaseCubicInOut},
			{From: foxRun, To: foxWalk, On: triggerWalk, Crossfade: gaitFade, Ease: model.EaseCubicInOut},
		})
}
