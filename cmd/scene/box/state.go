package main

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock, so frame N is reproducible and a test drives N steps directly: the
// update event's Dt is deliberately ignored.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose, where the reference screenshot is taken.
const (
	orbitRadius    = 5.6
	startAzimuth   = 0.9
	startElevation = 0.38
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
)

// orbitTarget is the point the camera looks at and orbits, a little above the
// ground plane so the plane fills the lower half of the frame.
var orbitTarget = m.Vec3{Y: 0.5}

// State is the demo's own clock and the camera's place on its orbit, shared by
// the Systems as a resource. clockSystem is the only one that writes it; every
// other System reads it to place what it moves.
type State struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
}

// NewState is the documented starting pose at step zero.
func NewState() *State { return &State{azimuth: startAzimuth, elevation: startElevation} }

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine and a test can drive N steps directly.
func (s *State) time() float32 { return float32(s.step) * fixedStep }

// eye is the camera's position on its orbit.
func (s *State) eye() m.Vec3 {
	cosElevation := float32(math.Cos(float64(s.elevation)))
	return orbitTarget.Add(m.Vec3{
		X: orbitRadius * cosElevation * float32(math.Sin(float64(s.azimuth))),
		Y: orbitRadius * float32(math.Sin(float64(s.elevation))),
		Z: orbitRadius * cosElevation * float32(math.Cos(float64(s.azimuth))),
	})
}

// eyePlace is the camera's Transform: at eye, looking at orbitTarget.
func (s *State) eyePlace() m.Transform { return m.LookAt(s.eye(), orbitTarget, m.Vec3{Y: 1}) }

// spinCenter is where the orbiting sphere stands at the current step.
func (s *State) spinCenter() m.Vec3 {
	angle := float64(s.time())
	return m.Vec3{
		X: spinRadius * float32(math.Cos(angle)),
		Y: sphereRadius,
		Z: spinRadius * float32(math.Sin(angle)),
	}
}
