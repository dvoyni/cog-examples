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

// The HUD, drawn with canvas.Text and no font of its own: a text op with an
// empty font path draws with the font canvas embeds, so the numbers reach the
// screen without a file, a mount, or any configuration. This demo has no assets
// at all, and its text is not about to be the exception.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/procedural` sees the numbers without a second command.
//
// Every number on it is one the demo knows because it made it: the step, the
// meshes and the calls that made them. scene publishes nothing about the frame
// it drew, and whether the stray is culled is answered the way the culler
// answers it, from the Camera Component and the window's aspect.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

type hudQuery struct {
	Place  m.Transform
	Camera scene.Camera
}

// hud prints the demo's key numbers on screen, and clears the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and its default pass keeps colour; layerBackdrop sorts below
// the camera, so the order is clear, then the scene, then this text.
func hud(
	cameras *ecs.Query[hudQuery],
	viewport *ecs.Read[*gfx.Viewport],
	state *ecs.Write[*Procedural],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	p, q := state.Get(), overlay.Get()
	p.rate.measure(time.Now())
	p.strayVisible = false
	if view := viewport.Get(); view != nil {
		for _, it := range cameras.All() {
			if onScreen(it.Camera, it.Place, view, strayPosition, beaconScale) {
				p.strayVisible = true
			}
		}
	}

	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	mode := "running"
	if p.paused {
		mode = "paused"
	}
	stray := "culled"
	if p.strayVisible {
		stray = "on screen"
	}
	lines := [...]string{
		fmt.Sprintf("procedural  step %06d  time %.2fs  fps %.0f  %s",
			p.step, p.time(), p.rate.perSecond, mode),
		fmt.Sprintf("mesh calls: baked %d  updated %d  released %d",
			p.bakes, p.updates, p.releases),
		fmt.Sprintf("ridge mesh %d  %d cells  (durable, updated every frame)",
			p.ridge.ID(), p.ridgeCellCount),
		fmt.Sprintf("ribbon mesh %d  generation %d  %d vertices  (baked and released every frame)",
			p.ribbon.ID(), p.ribbon.Generation(), p.ribbonVertices),
		fmt.Sprintf("beacon mesh %d  generation %d  stale draws skipped %d",
			p.beacon.ID(), p.generation, p.staleDraws),
		fmt.Sprintf("stray beacon %s", stray),
	}
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	text(q, screenHeight-hudFoot, "space pause   arrows orbit   r reset", hudDimColor)
}

// onScreen reports whether a sphere at center of radius is inside the frustum
// camera, placed at place, draws through into the window: the same matrix
// scene's default pass culls with, so the answer is the culler's.
func onScreen(camera scene.Camera, place m.Transform, view *gfx.Viewport, center m.Vec3, radius float32) bool {
	viewProjection, err := scene.ViewProjection(camera, place,
		m.Vec2{X: view.WindowWidth, Y: view.WindowHeight})
	if err != nil {
		return false
	}
	return m.FrustumFromMat4(viewProjection).ContainsSphere(center, radius)
}

// text draws one line at the HUD's left margin. Position is the top-left of the
// line, so a caller places lines by stepping y and never has to know where the
// baseline sits.
func text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
