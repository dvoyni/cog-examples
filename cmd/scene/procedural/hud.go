package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

// The HUD, drawn with canvas.Text and no font of its own: a text op with an
// empty font path draws with the font canvas embeds, so the numbers reach the
// screen without a file, a mount, or any configuration. This demo has no assets
// at all, and its text is not about to be the exception.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/procedural` sees the numbers without a second command.

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
func (p *Procedural) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	state := "running"
	if p.paused {
		state = "paused"
	}
	stray := "culled"
	if p.stats.strayVisible {
		stray = "on screen"
	}
	lines := [...]string{
		fmt.Sprintf("procedural  step %06d  time %.2fs  fps %.0f  %s",
			p.step, p.time(), p.rate.perSecond, state),
		fmt.Sprintf("submitted %d  recorded %d  culled %d  packed %d  batches %d",
			p.submitted, p.stats.recorded, p.stats.culled, p.stats.instances, p.stats.batches),
		// The two mesh ids side by side: a temporary mesh's carries a bit a
		// durable one's never does, so the two ranges cannot collide in the
		// sort key however many of each the frame mints.
		fmt.Sprintf("ridge mesh %d  %d cells  (durable, re-baked every frame)",
			p.ridge.ID(), p.ridgeCellCount),
		fmt.Sprintf("ribbon mesh %d  %d vertices  (temporary, gone at the frame boundary)",
			p.ribbon.ID(), p.ribbonVertices),
		fmt.Sprintf("beacon mesh %d  generation %d  stale draws skipped %d",
			p.beacon.ID(), p.generation, p.staleDraws),
		fmt.Sprintf("stray beacon %s", stray),
	}
	for i, line := range lines {
		p.text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	p.text(q, screenHeight-hudFoot, "space pause   arrows orbit   r reset", hudDimColor)
}

// text draws one line at the HUD's left margin. Position is the top-left of the
// line, so a caller places lines by stepping y and never has to know where the
// baseline sits.
func (p *Procedural) text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
