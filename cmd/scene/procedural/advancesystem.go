package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
)

// advance reads the keys and steps the demo's own clock. Pausing stops the
// geometry without freezing the orbit: a paused frame is still one a human
// wants to look around.
func advance(keys *ecs.Read[*input.State], state *ecs.Write[*Procedural]) {
	p := state.Get()
	if keys := keys.Get(); keys != nil {
		if keys.JustPressed(input.KeySpace) {
			p.paused = !p.paused
		}
		step := orbitSpeed * fixedStep
		if keys.Pressed(input.KeyLeft) {
			p.azimuth -= step
		}
		if keys.Pressed(input.KeyRight) {
			p.azimuth += step
		}
		if keys.Pressed(input.KeyUp) {
			p.elevation = m.Clamp(p.elevation+step, -1.4, 1.4)
		}
		if keys.Pressed(input.KeyDown) {
			p.elevation = m.Clamp(p.elevation-step, -1.4, 1.4)
		}
		if keys.JustPressed(input.KeyR) {
			p.azimuth, p.elevation, p.step = startAzimuth, startElevation, 0
		}
	}
	p.advanced = !p.paused
	if p.advanced {
		p.step++
	}
}
