package main

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The HUD, drawn with canvas.Text and no font of its own.
//
// box is the demo that runs with zero assets, and text costs it none: a text op
// with an empty font path draws with the font canvas embeds, so the numbers
// reach the screen without a file, a mount, or any configuration. Naming
// canvas.DefaultFontPath here would draw exactly the same frame; the empty path
// is what the other demos will write, so it is what this one demonstrates.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/box` sees the numbers without a second command.
//
// Every number on it is one the demo already knows - its own clock, and a
// count of the Entities it spawned - and none is read back out of the
// renderer. Whether the sphere is on screen is asked of the camera the same
// way a pick would be, through scene.ViewProjection.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

// Meter is the HUD's frames-per-second meter, and the demo's only wall clock.
// It is a resource of its own because hudSystem is the only System that
// touches it.
//
// It cannot come from the update event's Dt: that is app's *fixed*
// timestep, so reading it back would report the rate the demo was asked to run
// at rather than the rate it managed. Nor can it come from the step counter,
// which is the whole point of accumulated fixed steps - step 120 is step 120
// whether it took two seconds or twenty. So this is measured against
// time.Now(), and it is deliberately the only thing in the demo that is: it
// feeds the HUD and nothing else, and no assertion reads it.
//
// It counts frames over a window rather than averaging 1/interval per frame.
// A per-frame average is dominated by its own worst sample: two ticks a
// microsecond apart during startup are a reading of a million, and an
// exponential average carries a thousandth of that for a hundred frames
// afterwards, so the HUD spends its first seconds reporting a number that never
// happened. A count over a window cannot do that - a spike is one frame in the
// count, whatever its interval was.
type Meter struct {
	window    time.Time
	frames    int
	perSecond float32
}

// ratePeriod is how long the meter counts before republishing. Long enough that
// the number holds still to be read, short enough that a stall shows up while
// the human is still looking at what caused it.
const ratePeriod = 250 * time.Millisecond

// measure counts one update tick, which is one drawn frame here, and
// republishes the rate once the window is full.
func (r *Meter) measure(now time.Time) {
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

// The HUD's census Queries, one per kind of thing the demo spawns.
type (
	boxCount     struct{ Shape scene.DebugBox }
	sphereCount  struct{ Shape scene.DebugSphere }
	planeCount   struct{ Shape scene.DebugPlane }
	lineCount    struct{ Shape scene.DebugLine }
	wireBoxCount struct{ Shape scene.DebugWireBox }
	lightCount   struct{ Light scene.Light }
	cameraView   struct {
		Place  m.Transform
		Camera scene.Camera
	}
	sphereView struct {
		Place m.Transform
		Shape scene.DebugSphere
		_     ecs.With[Orbit]
	}
)

// hudSystem prints the demo's key numbers on screen, and clears the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and the implicit pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
func hudSystem(
	boxes *ecs.Query[boxCount],
	spheres *ecs.Query[sphereCount],
	planes *ecs.Query[planeCount],
	lines *ecs.Query[lineCount],
	wires *ecs.Query[wireBoxCount],
	lights *ecs.Query[lightCount],
	cameras *ecs.Query[cameraView],
	orbiting *ecs.Query[sphereView],
	state *ecs.Read[*State],
	viewport *ecs.Read[*gfx.Viewport],
	meter *ecs.Write[*Meter],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	s, r, q := state.Get(), meter.Get(), overlay.Get()
	r.measure(time.Now())

	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	run := "running"
	if s.paused {
		run = "paused"
	}
	// The sphere is on screen when it is inside the frustum of some camera.
	// A camera that cannot be projected yet - no window, say - sees nothing.
	visible := "no"
	if view := viewport.Get(); view != nil {
		size := m.Vec2{X: view.WindowWidth, Y: view.WindowHeight}
		for _, sphere := range orbiting.All() {
			for _, eye := range cameras.All() {
				if in, err := sphereInFrustum(eye.Camera, eye.Place, size,
					sphere.Place.Position, sphere.Shape.Radius); err == nil && in {
					visible = "yes"
				}
			}
		}
	}
	text := [...]string{
		fmt.Sprintf("box  step %06d  time %.2fs  fps %.0f  %s",
			s.step, s.time(), r.perSecond, run),
		fmt.Sprintf("boxes %d  spheres %d  planes %d  lines %d  wire boxes %d",
			count(boxes), count(spheres), count(planes), count(lines), count(wires)),
		fmt.Sprintf("lights %d  cameras %d", count(lights), count(cameras)),
		fmt.Sprintf("sphere in frustum %s", visible),
	}
	for i, line := range text {
		hudText(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	hudText(q, screenHeight-hudFoot, "space pause   arrows orbit   r reset", hudDimColor)
}

// count is how many Entities a census Query matches.
func count[Q any](q *ecs.Query[Q]) int {
	n := 0
	for range q.All() {
		n++
	}
	return n
}

// sphereInFrustum asks whether a sphere is inside the frustum camera, placed
// at at, draws through at a viewport of the given size: the same matrix its
// pass uploads, so the answer is the renderer's culling decision rather than
// an approximation of it. A camera the renderer would skip is its error.
func sphereInFrustum(camera scene.Camera, at m.Transform, viewport m.Vec2, center m.Vec3, radius float32) (bool, error) {
	viewProjection, err := scene.ViewProjection(camera, at, viewport)
	if err != nil {
		return false, err
	}
	return m.FrustumFromMat4(viewProjection).ContainsSphere(center, radius), nil
}

// hudText draws one line at the HUD's left margin. Position is the top-left of
// the line, so a caller places lines by stepping y and never has to know where
// the baseline sits.
func hudText(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
