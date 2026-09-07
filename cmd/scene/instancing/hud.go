package main

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// The HUD, drawn with canvas.Text and the font canvas embeds - a text op with
// an empty font path names none, and that is what every demo but the one
// demonstrating a named font writes.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/instancing` sees the numbers without a second command,
// and this is the demo where that rule does the most work: the batch table
// below is the whole of what an instanced draw buys, and it exists nowhere else
// in the running app.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

// keysPerLine is how many batch keys fit across the logical screen at hudSize.
// The table wraps rather than scrolls: a frame that packed five hundred batches
// is a frame whose first few keys already say what happened, and the count on
// the line above says how many there were.
const (
	keysPerLine = 8
	keyLines    = 3
)

// hud prints the demo's key numbers and its batch table on screen, and clears
// the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and the implicit pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
func (p *Instancing) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	state := "running"
	if p.paused {
		state = "paused"
	}
	mode := "1 one instanced call"
	if p.perCall {
		mode = "2 one call per crate"
	}
	lines := []string{
		fmt.Sprintf("instancing  step %06d  time %.2fs  fps %.0f  %s",
			p.step, p.time(), p.rate.perSecond, state),
		fmt.Sprintf("models %d/%d resident   field %d crates, %d of them pillars   mode %s",
			p.ResidentCount(), len(modelPaths), CrateCount, PillarCount, mode),
		fmt.Sprintf("draws %d  culled %d  packed %d  batches %d  largest batch %d",
			p.stats.recorded, p.stats.culled, p.stats.instances,
			p.stats.batches, p.stats.biggest),
		fmt.Sprintf("passes %d  ops %d", p.stats.passes, p.stats.ops),
		"batches as material/mesh x instances, opaque by sort key then blend by depth:",
	}
	lines = append(lines, batchTable(p.Batches())...)
	for i, line := range lines {
		p.text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	p.text(q, screenHeight-hudFoot,
		"space pause   arrows orbit   1 instanced   2 per crate   r reset", hudDimColor)
}

// batchTable renders the published batch list as wrapped lines of keys, which
// is the sort key made visible: read left to right, material then mesh never
// decreases across the opaque run, and the blend bucket that follows it is
// ordered by depth instead.
//
// It is capped at keyLines lines. The per-crate mode packs one batch per
// surviving crate and a five-hundred-line table would be a wall rather than a
// reading, so the tail is summarised by the count.
func batchTable(batches []scene.BatchView) []string {
	if len(batches) == 0 {
		return []string{"  (nothing packed yet)"}
	}
	shown := min(len(batches), keyLines*keysPerLine)
	out := make([]string, 0, keyLines)
	var line strings.Builder
	for i := range shown {
		if i > 0 && i%keysPerLine == 0 {
			out = append(out, line.String())
			line.Reset()
		}
		fmt.Fprintf(&line, "  %d/%dx%d",
			batches[i].MaterialID, batches[i].MeshID, batches[i].InstanceCount)
	}
	if rest := len(batches) - shown; rest > 0 {
		fmt.Fprintf(&line, "  ... and %d more", rest)
	}
	return append(out, line.String())
}

// text draws one line at the HUD's left margin. Position is the top-left of the
// line, so a caller places lines by stepping y and never has to know where the
// baseline sits.
func (p *Instancing) text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
