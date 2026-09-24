package main

import (
	"time"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// control steps the demo's own clock, reads the keys and the pointer, and turns
// a mouse press into a pick.
func control(inputs *ecs.Read[*input.State], viewport *ecs.Read[*gfx.Viewport], state *ecs.Write[*State]) {
	s := state.Get()
	s.rate.measure(time.Now())
	advance(s, inputs.Get())
	press(s, inputs.Get(), viewport.Get())
}

// advance steps the clock and reads the keys.
//
// Input is read before the step so a held arrow moves the camera on the very
// frame it is pressed. The arrows work only while paused, because they are for
// walking the track to a frame worth looking at rather than for driving.
func advance(s *State, keys *input.State) {
	if keys != nil {
		if keys.JustPressed(input.KeySpace) {
			s.paused = !s.paused
		}
		if keys.JustPressed(input.KeyR) {
			s.step, s.picked = startStep, -1
		}
		s.duplicate = keys.Pressed(input.KeyD)
		if s.paused {
			if keys.Pressed(input.KeyRight) {
				s.step++
			}
			if keys.Pressed(input.KeyLeft) {
				s.step--
			}
		}
	}
	if !s.paused {
		s.step++
	}
}

// press turns a mouse press into a pick, and it is the whole screen-to-world
// half of this demo.
//
// The order is what matters. A click is a point on the screen; a camera answers
// about points on its own target; so the click is mapped into a panel's texels
// first - which is where the panel's rect and its target size stop being
// interchangeable - and only then handed to that panel's camera, through the
// matrix scene draws it with. Hitting neither panel picks nothing rather than
// picking through the nearer camera.
//
// The two panels are two cameras and one world, so clicking the same cube in
// either picks the same entry. That is the criterion, and it is also the
// reason nothing here is written twice.
func press(s *State, keys *input.State, view *gfx.Viewport) {
	if keys == nil || view == nil || view.WindowWidth <= 0 || view.WindowHeight <= 0 {
		return
	}
	// The pointer arrives in window pixels; canvas draws in the logical screen
	// the window is fitted to, and ScreenToWorld undoes that fit.
	pointer := keys.Pointer()
	logical := m.Vec2{
		X: float32(pointer.X) * view.Width / view.WindowWidth,
		Y: float32(pointer.Y) * view.Height / view.WindowHeight,
	}
	s.pointer = canvas.ScreenToWorld(
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe,
		m.Vec2{X: view.Width, Y: view.Height}, logical)
	if !keys.JustPressed(input.KeyMouseLeft) {
		return
	}
	for _, panel := range panels(s.time()) {
		texel, ok := panel.view.texel(s.pointer)
		if !ok {
			continue
		}
		ray, ok := panel.screenToRay(texel)
		if !ok {
			continue
		}
		if hit, ok := pick(ray, s.targets); ok {
			s.picked = hit
		} else {
			s.picked = -1
		}
		return
	}
	s.picked = -1
}
