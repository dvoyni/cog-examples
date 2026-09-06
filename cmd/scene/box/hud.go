package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
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

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

// hud prints the demo's key numbers on screen, and clears the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and the implicit pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
func (p *Box) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	state := "running"
	if p.paused {
		state = "paused"
	}
	visible := "no"
	if p.stats.sphereVisible {
		visible = "yes"
	}
	lines := [...]string{
		fmt.Sprintf("box  step %06d  time %.2fs  fps %.0f  %s",
			p.step, p.time(), p.rate.perSecond, state),
		fmt.Sprintf("draws %d  culled %d  packed %d  lights %d",
			p.stats.recorded, p.stats.culled, p.stats.instances, p.stats.lights),
		fmt.Sprintf("passes %d  batches %d  ops %d",
			p.stats.passes, p.stats.batches, p.stats.ops),
		fmt.Sprintf("sphere in frustum %s", visible),
	}
	for i, line := range lines {
		p.text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	p.text(q, screenHeight-hudFoot, "space pause   arrows orbit   r reset", hudDimColor)
}

// text draws one line at the HUD's left margin. Position is the top-left of the
// line, so a caller places lines by stepping y and never has to know where the
// baseline sits.
func (p *Box) text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
