package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

// HUD is one step's census beside the flush that drew that step.
type HUD struct {
	Step             int
	Spawned, Retired int
	Motes            int
	Foxes            int
	Lights           int
	Cameras          int
	Passes           int
	Drawn            int // instances packed, across every pass
	Batches          int
	Culled           int
}

const (
	hudSize = 11
	hudLeft = 16
	hudTop  = 14
	hudLine = hudSize * 7 / 5
	hudFoot = hudTop + hudSize*2/3
)

var (
	hudColor     = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor  = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
	hudWarnColor = m.NewColorSrgb(0.95, 0.55, 0.40, 1)
)

// lines is the HUD's text, as a function so a test reads exactly what is drawn.
func (h HUD) lines(step int) []string {
	return []string{
		fmt.Sprintf("fountain  step %06d  time %.2fs", step, float32(step)*fixedStep),
		fmt.Sprintf("step %06d  motes %d = spawned %d - retired %d", h.Step, h.Motes, h.Spawned, h.Retired),
		fmt.Sprintf("fox %d  lights %d  cameras %d", h.Foxes, h.Lights, h.Cameras),
		fmt.Sprintf("passes %d  drawn %d  batches %d  culled %d", h.Passes, h.Drawn, h.Batches, h.Culled),
	}
}

func (h HUD) draw(q *canvas.OpQueue, step int) {
	q.SetLayerTransform(layerHUD, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)
	lines := h.lines(step)
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	if h.Motes != h.Spawned-h.Retired {
		text(q, hudTop+float32(len(lines))*hudLine,
			fmt.Sprintf("census %d disagrees with the tally %d", h.Motes, h.Spawned-h.Retired), hudWarnColor)
	}
	text(q, screenHeight-hudFoot, "no keys: step N is the same frame on every machine", hudDimColor)
}

func text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{Position: m.Vec2{X: hudLeft, Y: y}, Size: hudSize, Color: color})
}
