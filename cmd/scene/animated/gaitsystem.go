package main

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
)

type gaitQuery struct {
	Gait *Gait
	Pose *scene.Animation
}

// gait steps the fox's gait machine to the demo's step, firing the schedule's
// triggers on the way, and copies the machine's plays into the fox's Animation.
//
// The machine is stepped here, by the game, because scene is stateless about
// animation: nothing in it advances clip time, and the Animation it draws is
// whatever this System last wrote.
func gait(foxes *ecs.Query[gaitQuery], state *ecs.Read[*Demo]) {
	step := state.Get().step
	for _, it := range foxes.All() {
		if !it.Gait.Rigged {
			continue
		}
		syncGait(it.Gait, step)
		it.Gait.Machine.PlaysInto(&it.Pose.Plays)
	}
}

// The schedule firing the gait machine's triggers. The first fires at
// gaitPhaseSteps and one more every gaitDwellSteps after it, run and walk
// alternately.
//
// The schedule is in steps rather than seconds so that a trigger lands on
// exactly one step. Its phase exists because startTime is already spoken for:
// the reference time is pinned by the two stations that read from a file's own
// clip lengths, and the schedule is this demo's own function of the clock, so
// it is the one that can be moved to meet the others. Here it puts the trigger
// at 25.5 seconds, and the reference frame catches the walk-to-run fade that
// trigger started halfway through its 1.2 seconds.
//
// The dwell is long against the fade, so a run of the demo shows a pure walk
// and a pure run between them.
const (
	gaitPhaseSteps = 90
	gaitDwellSteps = 180
)

// gaitTrigger is what the schedule fires on step, or "" when it fires nothing.
func gaitTrigger(step int) string {
	if step < gaitPhaseSteps || (step-gaitPhaseSteps)%gaitDwellSteps != 0 {
		return ""
	}
	if (step-gaitPhaseSteps)/gaitDwellSteps%2 == 0 {
		return triggerRun
	}
	return triggerWalk
}

// syncGait steps one gait machine to step, and records the last state it
// entered for the HUD. A step behind the machine is a rewind, which starts
// again from the machine as it was built: nothing runs a machine backwards,
// and the steps to catch up are cheap.
func syncGait(g *Gait, step int) {
	if g.Step > step {
		g.Machine, g.Step = g.Start, 0
	}
	var events []model.ClipEvent
	for g.Step < step {
		g.Step++
		if trigger := gaitTrigger(g.Step); trigger != "" {
			g.Machine.Fire(trigger)
		}
		events = g.Machine.Step(fixedStep, events[:0])
		for _, event := range events {
			if event.Kind == model.ClipEntered {
				g.Last = fmt.Sprintf("%s entered at step %d", g.Machine.StateName(event.State), g.Step)
			}
		}
	}
}
