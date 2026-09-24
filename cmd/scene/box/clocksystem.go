package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
)

// clockSystem steps the demo's own clock and applies the orbit keys. Input is
// read before the step so a held arrow moves the camera on the very frame it
// is pressed, and pausing stops the spin and the sphere without freezing the
// orbit: a paused frame is still one a human wants to look around.
func clockSystem(keys *ecs.Read[*input.State], state *ecs.Write[*State]) {
	s := state.Get()
	if pressed := keys.Get(); pressed != nil {
		if pressed.JustPressed(input.KeySpace) {
			s.paused = !s.paused
		}
		step := orbitSpeed * fixedStep
		if pressed.Pressed(input.KeyLeft) {
			s.azimuth -= step
		}
		if pressed.Pressed(input.KeyRight) {
			s.azimuth += step
		}
		if pressed.Pressed(input.KeyUp) {
			s.elevation = m.Clamp(s.elevation+step, -1.4, 1.4)
		}
		if pressed.Pressed(input.KeyDown) {
			s.elevation = m.Clamp(s.elevation-step, -1.4, 1.4)
		}
		if pressed.JustPressed(input.KeyR) {
			s.azimuth, s.elevation, s.step = startAzimuth, startElevation, 0
		}
	}
	if !s.paused {
		s.step++
	}
}
