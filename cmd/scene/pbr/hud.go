package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

// The HUD, drawn with canvas.Text and the font canvas embeds - a text op with an
// empty font path names none, and that is what every demo but the one
// demonstrating a named font writes.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/pbr` sees the numbers without a second command. The two
// numbers this demo owes that box does not are the residency count, because six
// models arrive asynchronously and an empty frame is otherwise unexplained, and
// the light count over what was declared, because the cap's drop is silent
// everywhere else.

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
func (p *Pbr) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	state := "running"
	if p.paused {
		state = "paused"
	}
	view := "overview"
	if p.focus > 0 {
		view = fmt.Sprintf("%d %s", p.focus, stations[p.focus-1].name)
	}
	// What did not reach the buffer was culled by the pass's frustum or dropped
	// past the cap, and the two are not separable from out here: PassView
	// reports how many were packed and nothing counts the difference. Saying
	// "dropped by the cap" would be a guess, and at a station close-up it would
	// be the wrong one - most of the loss there is culling.
	lost := p.declared - p.stats.lights
	if lost < 0 {
		lost = 0
	}
	lines := [...]string{
		fmt.Sprintf("pbr  step %06d  time %.2fs  fps %.0f  %s",
			p.step, p.time(), p.rate.perSecond, state),
		fmt.Sprintf("models %d/%d resident   view %s",
			p.ResidentCount(), len(stations), view),
		fmt.Sprintf("draws %d  culled %d  packed %d  batches %d",
			p.stats.recorded, p.stats.culled, p.stats.instances, p.stats.batches),
		fmt.Sprintf("lights %d/%d packed  cap %d  %d culled or capped, silently",
			p.stats.lights, p.declared, MaxLights, lost),
		fmt.Sprintf("passes %d  ops %d", p.stats.passes, p.stats.ops),
	}
	for i, line := range lines {
		p.text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	p.text(q, screenHeight-hudFoot,
		"space pause   arrows orbit   1-6 station   0 overview   r reset", hudDimColor)
}

// text draws one line at the HUD's left margin. Position is the top-left of the
// line, so a caller places lines by stepping y and never has to know where the
// baseline sits.
func (p *Pbr) text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
