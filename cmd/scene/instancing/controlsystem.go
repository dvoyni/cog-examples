package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
)

// control steps the demo's own clock and applies the input. It runs before
// every other System of the demo, and input is read before the clock advances,
// so a held arrow moves the camera on the very step it is pressed.
func control(keys *ecs.Read[*input.State], state *ecs.Write[*Demo]) {
	d := state.Get()
	if pressed := keys.Get(); pressed != nil {
		if pressed.JustPressed(input.KeySpace) {
			d.paused = !d.paused
		}
		step := orbitSpeed * fixedStep
		if pressed.Pressed(input.KeyLeft) {
			d.azimuth -= step
		}
		if pressed.Pressed(input.KeyRight) {
			d.azimuth += step
		}
		if pressed.Pressed(input.KeyUp) {
			d.elevation = m.Clamp(d.elevation+step, 0.05, 1.4)
		}
		if pressed.Pressed(input.KeyDown) {
			d.elevation = m.Clamp(d.elevation-step, 0.05, 1.4)
		}
		if pressed.JustPressed(input.Key1) {
			d.perCrate = false
		}
		if pressed.JustPressed(input.Key2) {
			d.perCrate = true
		}
		if pressed.JustPressed(input.KeyR) {
			d.azimuth, d.elevation, d.step, d.perCrate = startAzimuth, startElevation, 0, false
		}
	}
	if !d.paused {
		d.step++
	}
}
