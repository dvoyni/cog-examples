package main

import (
	"time"

	"github.com/dvoyni/cog/bundles/model"
)

// Demo is the state the demo's Systems share: the clock, the camera's orbit,
// the keys' toggles, and what the HUD prints. It is a resource rather than a
// Component because it describes the demo, not any one Entity in it.
type Demo struct {
	step   int
	paused bool
	// contrast is whether the stress station's second copy plays its own clip.
	// Toggling it off is the direct A/B: the copy falls back to its
	// neighbour's clip and starts moving with it.
	contrast  bool
	azimuth   float32
	elevation float32
	// focus is 0 for the overview and 1..4 for a station close-up, which is
	// the key that selected it.
	focus int
	rate  rate
	// resident is which paths reported residency on the last tick, for the
	// HUD. Clips' ok is the residency predicate, and it is false for a
	// missing, loading and failed path alike, which is exactly what "not
	// drawable yet" means.
	resident [len(ModelPaths)]bool
	// memory is the pose and delta bytes each model reported on the last
	// tick, and the two totals. They are the numbers a rig's storage is read
	// off, and the reason the query exists.
	memory memory
	// clips is the scratch ClipInfo slice the residency read fills, so a tick
	// that asks five files for their clips allocates nothing.
	clips []model.ClipInfo
	// spawned is how many Entities setup made, and census what the HUD
	// counted of them on the last tick.
	spawned int
	census  census
	// fox and lastGait are the moving fox's plays and its machine's last
	// entered state, as the HUD last read them off its Animation and Gait.
	fox      [CapPlays]model.ClipPlay
	lastGait string
}

// memory is what the lookup facade reported about GPU-resident animation data
// on the last tick.
type memory struct {
	pose       [len(ModelPaths)]int
	morph      [len(ModelPaths)]int
	totalPose  int
	totalMorph int
}

// census is what the HUD counted of the world on the last tick: every Entity
// carrying each of the Components the demo draws with.
type census struct {
	models, animations, meshes, cameras int
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is app's fixed timestep,
// nor from the step counter, which is the same number however long a frame took.
type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

// newDemo is the documented starting pose, paused, with the contrast on.
func newDemo() *Demo {
	return &Demo{
		paused:    true,
		contrast:  true,
		azimuth:   startAzimuth,
		elevation: startElevation,
		step:      int(startTime * stepsPerSecond),
	}
}

// Time is the demo's clock: accumulated fixed steps. Every clip play's Time is
// this number, so the whole row is on one timeline and a step is a step
// everywhere.
func (d *Demo) Time() float32 { return float32(d.step) * fixedStep }

// ResidentCount is how many files reported residency on the last tick.
func (d *Demo) ResidentCount() int {
	n := 0
	for _, ok := range d.resident {
		if ok {
			n++
		}
	}
	return n
}
