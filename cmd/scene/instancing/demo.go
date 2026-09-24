package main

import (
	"math"
	"time"

	"github.com/dvoyni/cog/libs/m"
)

// Demo is the demo's own state, shared by its Systems as a resource: the step
// counter, the orbit, the draw mode and what the HUD prints. None of it is
// about an Entity, so none of it is a Component.
type Demo struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
	// perCrate is the mode the keys asked for: false is every crate sharing one
	// Batch key, true is every crate a key of its own. split is the mode the
	// crates' Params were last written in, so the split System writes them only
	// when the two differ rather than every step.
	perCrate bool
	split    bool
	rate     rate
	// resident is which of the three files reported residency on the last
	// step, for the HUD.
	resident [len(modelPaths)]bool
}

// newDemo is the demo at its documented starting pose.
func newDemo() *Demo {
	return &Demo{azimuth: startAzimuth, elevation: startElevation}
}

// time is the demo's clock: accumulated fixed steps. Nothing in the frame reads
// it - the courtyard is deliberately still - so it is the HUD's number and the
// orbit's rate, and every frame at a given pose is the same frame.
func (d *Demo) time() float32 { return float32(d.step) * fixedStep }

// eye is the camera's position on its orbit.
func (d *Demo) eye() m.Vec3 {
	cosElevation := float32(math.Cos(float64(d.elevation)))
	return orbitTarget.Add(m.Vec3{
		X: overviewRadius * cosElevation * float32(math.Sin(float64(d.azimuth))),
		Y: overviewRadius * float32(math.Sin(float64(d.elevation))),
		Z: overviewRadius * cosElevation * float32(math.Cos(float64(d.azimuth))),
	})
}

// place is the camera's Transform on its orbit, facing the target.
func (d *Demo) place() m.Transform {
	return m.LookAt(d.eye(), orbitTarget, m.Vec3{Y: 1})
}

// ResidentCount is how many files reported residency on the last step.
func (d *Demo) ResidentCount() int {
	n := 0
	for _, ok := range d.resident {
		if ok {
			n++
		}
	}
	return n
}

// FieldKeys is how many Batch keys the crates hold between them, which is a
// number the demo decides rather than one it reads back: one while they share
// their Params, and one per crate once each names its own serial.
func (d *Demo) FieldKeys() int {
	if d.split {
		return CrateCount + stackCount
	}
	return 1
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is app's fixed timestep,
// nor from the step counter, which is the same number however long a frame
// took. It counts frames over a window rather than averaging 1/interval per
// frame, so a startup spike is one frame in the count instead of a reading that
// never happened decaying for a hundred frames afterwards.
type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

// ratePeriod is how long the meter counts before republishing.
const ratePeriod = 250 * time.Millisecond

func (r *rate) measure(now time.Time) {
	if r.window.IsZero() {
		r.window = now
		return
	}
	r.frames++
	if elapsed := now.Sub(r.window); elapsed >= ratePeriod {
		r.perSecond = float32(float64(r.frames) / elapsed.Seconds())
		r.frames, r.window = 0, now
	}
}
