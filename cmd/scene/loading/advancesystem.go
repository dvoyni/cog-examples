package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
)

// advance steps the demo's own clock and turns key presses into the unload the
// unload System applies. The keys are queued rather than applied here because
// this System holds no facade, and because an unload landing between two of
// the tick's own queries would make the HUD disagree with itself.
func advance(keys *ecs.Read[*input.State], state *ecs.Write[*Residency]) {
	r := state.Get()
	r.step++
	in := keys.Get()
	if in == nil {
		return
	}
	if in.JustPressed(input.KeyU) {
		r.pending.model = true
	}
	if in.JustPressed(input.KeyT) {
		r.pending.texture = true
	}
	if in.JustPressed(input.KeyX) {
		r.pending.all = true
	}
	if in.JustPressed(input.KeyR) {
		r.pending.retry = true
	}
}
