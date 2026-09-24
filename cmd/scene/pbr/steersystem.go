package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
)

// steer steps the demo's own clock and applies the input. Input is read before
// the step so a held arrow moves the camera on the very frame it is pressed.
func steer(keys *ecs.Read[*input.State], state *ecs.Write[*Pbr]) {
	p := state.Get()
	if keyboard := keys.Get(); keyboard != nil {
		if keyboard.JustPressed(input.KeySpace) {
			p.paused = !p.paused
		}
		step := orbitSpeed * fixedStep
		if keyboard.Pressed(input.KeyLeft) {
			p.azimuth -= step
		}
		if keyboard.Pressed(input.KeyRight) {
			p.azimuth += step
		}
		if keyboard.Pressed(input.KeyUp) {
			p.elevation = m.Clamp(p.elevation+step, -0.2, 1.4)
		}
		if keyboard.Pressed(input.KeyDown) {
			p.elevation = m.Clamp(p.elevation-step, -0.2, 1.4)
		}
		for i, key := range focusKeys {
			if keyboard.JustPressed(key) {
				p.setFocus(i + 1)
			}
		}
		if keyboard.JustPressed(input.Key0) {
			p.setFocus(0)
		}
		if keyboard.JustPressed(input.KeyR) {
			p.setFocus(0)
			p.step = 0
		}
	}
	if !p.paused {
		p.step++
	}
}

// focusKeys are the number keys that fly the camera to a station, in station
// order.
var focusKeys = [len(stations)]input.Key{
	input.Key1, input.Key2, input.Key3, input.Key4, input.Key5, input.Key6,
}
