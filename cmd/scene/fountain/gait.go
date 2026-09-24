package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// The fox circles the basin at a speed that swells and ebbs, so its gait
// changes between Walk and Run.
const (
	foxScale  = 0.014
	foxRadius = 4.2
	foxRate   = 0.34 // radians a second, on average
	foxSurge  = 0.62 // how far the angle leads and lags the average
	FoxSwell  = 0.45 // radians a second of the surge's own cycle
	FoxWalk   = "Walk"
	FoxRun    = "Run"
	// The ground speeds, in world units a second, at which each clip's
	// authored cycle looks right: its stride over its own duration.
	WalkPace = 0.9
	RunPace  = 2.3
	// The fox breaks into a run above RunAbove and drops back to a walk below
	// WalkBelow. The gap between them is what keeps a speed hovering at one
	// threshold from flicking the gait back and forth.
	RunAbove  = 1.9
	WalkBelow = 1.3
	// GaitFade is how far, in world units of ground covered, a change of gait
	// crossfades over. It is a distance because the gait machine's clock is.
	GaitFade = 0.8
)

// The triggers the fox's gait machine takes, fired on the speed thresholds.
const (
	TriggerRun  = "run"
	TriggerWalk = "walk"
)

// foxAngle is where on its circle the fox is at time t.
func foxAngle(t float32) float32 {
	return foxRate*t + foxSurge*float32(math.Sin(float64(FoxSwell*t)))
}

// FoxSpeed is the fox's ground speed at time t.
func FoxSpeed(t float32) float32 {
	return foxRadius * (foxRate + foxSurge*FoxSwell*float32(math.Cos(float64(FoxSwell*t))))
}

// FoxPlace stands the fox on its circle at time t, facing along it.
func FoxPlace(t float32) m.Transform {
	angle := foxAngle(t)
	sin, cos := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return m.Transform{
		Position: m.Vec3{X: foxRadius * cos, Z: foxRadius * sin},
		Rotation: m.QuatRotationY(-angle),
		Scale:    m.NewVec3(foxScale),
	}
}

// NewFoxGait builds the fox's gait machine over the clips Fox.glb declares, as
// the lookup's Clips reports them: Walk and Run, each fading to the other on
// its trigger.
//
// The machine's clock is distance, not time. FoxGait steps it by the ground
// the fox covered, and each state's Rate is one over its gait's pace, so a clip
// advances by exactly as much of its cycle as the fox's stride covered and the
// feet stay planted whatever the speed. That is a rate only the game knows,
// which is why it is the game that owns the clock.
func NewFoxGait(clips []model.ClipInfo) (model.ClipMachine, error) {
	return model.NewClipMachine(clips,
		[]model.ClipState{
			{Name: FoxWalk, Clip: FoxWalk, Loop: true, Rate: 1 / WalkPace},
			{Name: FoxRun, Clip: FoxRun, Loop: true, Rate: 1 / RunPace},
		},
		[]model.ClipTransition{
			{From: FoxWalk, To: FoxRun, On: TriggerRun, Crossfade: GaitFade, Ease: model.EaseCubicInOut},
			{From: FoxRun, To: FoxWalk, On: TriggerWalk, Crossfade: GaitFade, Ease: model.EaseCubicInOut},
		})
}

// FoxGaitAt builds the fox's gait machine as it stands after steps 1 to step.
// rigSystem rigs the fox once Fox.glb is resident, which may be some steps in,
// and picking the gait up there rather than from a fresh machine is what keeps
// step N the same frame however late the file loaded.
func FoxGaitAt(clips []model.ClipInfo, step int) (model.ClipMachine, error) {
	gait, err := NewFoxGait(clips)
	if err != nil {
		return gait, err
	}
	for s := 1; s <= step; s++ {
		FoxGait(&gait, Clock(s))
	}
	return gait, nil
}

// FoxGait is one step of the fox's gait at time t: the trigger its speed
// calls for, then a Step by the ground covered. A trigger for the gait the fox
// is already in finds no transition and does nothing, so it is fired every
// step the speed calls for it.
func FoxGait(gait *model.ClipMachine, t float32) {
	speed := FoxSpeed(t)
	switch {
	case speed > RunAbove:
		gait.Fire(TriggerRun)
	case speed < WalkBelow:
		gait.Fire(TriggerWalk)
	}
	gait.Step(FixedStep*speed, nil)
}
