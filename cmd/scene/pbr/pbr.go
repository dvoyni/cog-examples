package main

import (
	"time"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// Pbr is the demo's state, shared by its Systems as a resource: the step
// counter, the orbit, the focus and the numbers the HUD prints.
type Pbr struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
	// focus is 0 for the overview and 1..6 for a station close-up, which is the
	// key that selected it.
	focus int
	rate  rate
	// resident is which stations the HUD found resident on the last frame.
	resident [len(stations)]bool
	// declared counts the Light Entities spawned, which is the number the
	// pass's own lights are capped from. It is zero until the lights station is
	// resident, because the lamps are spawned all at once when it is.
	declared int
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is app's fixed timestep,
// nor from the step counter, which is the same number however long a frame took.
// It counts frames over a window rather than averaging 1/interval per frame, so
// a startup spike is one frame in the count instead of a reading that never
// happened decaying for a hundred frames afterwards.
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

// setFocus points the camera at a station, or back at the overview, returning
// the orbit to the documented azimuth and elevation so a focus key always lands
// on the same picture.
func (p *Pbr) setFocus(focus int) {
	p.focus = focus
	p.azimuth, p.elevation = startAzimuth, startElevation
}

// target and radius are the orbit the camera is on, which is the overview's or
// the focused station's.
func (p *Pbr) target() m.Vec3 {
	if p.focus == 0 {
		return orbitTarget
	}
	return placements[p.focus-1].center
}

func (p *Pbr) radius() float32 {
	if p.focus == 0 {
		return overviewRadius
	}
	return stations[p.focus-1].radius
}

// time is the demo's clock: accumulated fixed steps. Nothing in the drawn
// frame reads it - the still life is deliberately still - so it is the HUD's
// number and the orbit's rate, and every frame at a given pose is the same
// frame.
func (p *Pbr) time() float32 { return float32(p.step) * fixedStep }

// eye is the camera's position on its orbit.
func (p *Pbr) eye() m.Vec3 {
	return eyeAt(p.target(), p.radius(), p.azimuth, p.elevation)
}

// place is the camera's Transform: standing on its orbit, facing the target.
func (p *Pbr) place() m.Transform {
	return m.LookAt(p.eye(), p.target(), m.Vec3{Y: 1})
}

// ResidentCount is how many stations the HUD found resident on the last frame.
func (p *Pbr) ResidentCount() int {
	n := 0
	for _, ok := range p.resident {
		if ok {
			n++
		}
	}
	return n
}

// camera is the Camera Component. Everything but the lens and the lighting is
// left at its zero value, and every zero is the default: Projection is
// Perspective, CullMask is LayersAll, SunIntensity and AmbientIntensity are 1,
// and Passes is empty, which is one default forward pass at the camera's own
// id that clears depth and keeps the backdrop canvas cleared beneath it.
//
// SunColor carries sunStrength, and the ambient is cool: a bright sun would
// wash out twenty-one punctual lights and the demo would be a test of one
// directional light.
func camera() scene.Camera {
	return scene.Camera{
		ID:            CameraMain,
		FovY:          fieldOfViewY,
		Near:          nearPlane,
		Far:           farPlane,
		SunDirection:  sunDirection,
		SunColor:      sunColor.MulS(sunStrength),
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	}
}
