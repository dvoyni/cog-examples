package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
)

// steer applies the keys and steps the demo's own clock. It runs after input's
// advance, so JustPressed is this tick's edge, and before everything that reads
// the clock.
func steer(state *ecs.Write[*Demo], keys *ecs.Read[*input.State]) {
	state.Get().advance(keys.Get())
}

// advance applies the input, then steps the clock unless it is paused. Input is
// read before the step so a held arrow moves the camera on the very frame it is
// pressed. A nil state is a tick with no input at all.
func (d *Demo) advance(state *input.State) {
	if state != nil {
		if state.JustPressed(input.KeySpace) {
			d.paused = !d.paused
		}
		if state.JustPressed(input.KeyO) {
			d.contrast = !d.contrast
		}
		step := orbitSpeed * fixedStep
		if state.Pressed(input.KeyLeft) {
			d.azimuth -= step
		}
		if state.Pressed(input.KeyRight) {
			d.azimuth += step
		}
		if state.Pressed(input.KeyUp) {
			d.elevation = m.Clamp(d.elevation+step, -0.2, 1.4)
		}
		if state.Pressed(input.KeyDown) {
			d.elevation = m.Clamp(d.elevation-step, -0.2, 1.4)
		}
		for i, key := range focusKeys {
			if state.JustPressed(key) {
				d.setFocus(i + 1)
			}
		}
		if state.JustPressed(input.Key0) {
			d.setFocus(0)
		}
		if state.JustPressed(input.KeyR) {
			d.setFocus(0)
			d.step = int(startTime * stepsPerSecond)
			d.paused = true
			d.contrast = true
		}
	}
	if !d.paused {
		d.step++
	}
}

// focusKeys are the number keys that fly the camera to a station, in station
// order.
var focusKeys = [len(stations)]input.Key{
	input.Key1, input.Key2, input.Key3, input.Key4,
}

// setFocus points the camera at a station, or back at the overview, returning
// the orbit to the documented azimuth and elevation so a focus key always lands
// on the same picture.
func (d *Demo) setFocus(focus int) {
	d.focus = focus
	d.azimuth, d.elevation = startAzimuth, startElevation
}
