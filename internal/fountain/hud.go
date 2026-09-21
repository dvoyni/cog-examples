package fountain

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// HUD is one step's census beside the frame that drew that step.
//
// The census is each fountain's own count of what it simulated and recorded.
// Passes, Drawn and Batches are read off gfx's frame snapshot, the same way in
// both fountains, so the two HUDs read the same numbers.
type HUD struct {
	Step             int
	Spawned, Retired int
	Motes            int
	Foxes            int
	Lights           int
	Cameras          int
	Passes           int
	Drawn            int // instances, across the camera's passes
	Batches          int // draws, across the camera's passes
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

// Lines is the HUD's text, as a function so a test reads exactly what is drawn.
func (h HUD) Lines(step int) []string {
	return []string{
		fmt.Sprintf("fountain  step %06d  time %.2fs", step, Clock(step)),
		fmt.Sprintf("step %06d  motes %d = spawned %d - retired %d", h.Step, h.Motes, h.Spawned, h.Retired),
		fmt.Sprintf("fox %d  lights %d  cameras %d", h.Foxes, h.Lights, h.Cameras),
		fmt.Sprintf("passes %d  drawn %d  batches %d", h.Passes, h.Drawn, h.Batches),
	}
}

// Draw records the HUD on layer.
func (h HUD) Draw(q *canvas.OpQueue, layer canvas.Layer, step int) {
	q.SetLayerTransform(layer, m.Rect{Width: ScreenWidth, Height: ScreenHeight}, canvas.AspectInscribe)
	lines := h.Lines(step)
	for i, line := range lines {
		text(q, layer, hudTop+float32(i)*hudLine, line, hudColor)
	}
	if h.Motes != h.Spawned-h.Retired {
		text(q, layer, hudTop+float32(len(lines))*hudLine,
			fmt.Sprintf("census %d disagrees with the tally %d", h.Motes, h.Spawned-h.Retired), hudWarnColor)
	}
	text(q, layer, ScreenHeight-hudFoot, "no keys: step N is the same frame on every machine", hudDimColor)
}

func text(q *canvas.OpQueue, layer canvas.Layer, y float32, line string, color m.Color) {
	q.Text(layer, "", line, canvas.TextDraw{Position: m.Vec2{X: hudLeft, Y: y}, Size: hudSize, Color: color})
}

// WithFrame is the HUD with its frame figures read off view: the passes of the
// fountain's camera, the instances they drew and the draws they made. Every
// other pass in the frame - canvas's, which draws this HUD - is left out.
func (h HUD) WithFrame(view gfx.FrameView) HUD {
	h.Passes, h.Drawn, h.Batches = 0, 0, 0
	for _, pass := range CameraPasses(view) {
		h.Passes++
		h.Drawn += pass.Instances
		h.Batches += pass.Draws
	}
	return h
}

// CameraPasses is the passes of view that belong to the fountain's camera, in
// run order: those whose label ends in TagGround or TagForward.
func CameraPasses(view gfx.FrameView) []gfx.PassView {
	var passes []gfx.PassView
	for _, pass := range view.Passes {
		if tag := PassTag(pass.Label); tag == TagGround || tag == TagForward {
			passes = append(passes, pass)
		}
	}
	return passes
}

// PassTag is the tag a pass label ends in: what follows its last dot, or the
// whole label when it has none. A renderer labels a camera's pass with the
// camera and the tag, so the tag is the part both fountains agree on.
func PassTag(label string) string {
	return label[strings.LastIndexByte(label, '.')+1:]
}

// Snapshots keeps the one frame snapshot a fountain has armed, and the last one
// that came back.
//
// gfx holds one snapshot at a time, and a snapshot describes a tick that began
// after it was armed. So Rearm runs once a tick, after gfx's present has taken
// the tick's snapshot: it collects that snapshot and arms the next, which the
// following tick answers. The HUD recorded during a tick therefore reads the
// snapshot of the tick before it, one step late, which is the step it shows the
// census of.
type Snapshots struct {
	done   <-chan gfx.FrameSnapshot
	latest gfx.FrameSnapshot
}

// Rearm collects the snapshot armed a tick ago, if it has come back, and arms
// the next one. An arm refused because another is live - an agent's, say - is
// simply tried again next tick.
func (s *Snapshots) Rearm(k kernel.Kernel, arm func(kernel.Kernel, gfx.ArmFrameRequest) gfx.ArmFrameResponse) {
	if s.done != nil {
		select {
		case snapshot := <-s.done:
			s.latest, s.done = snapshot, nil
		default:
		}
	}
	if s.done == nil {
		if armed := arm(k, gfx.ArmFrameRequest{}); armed.Err == nil {
			s.done = armed.Done
		}
	}
}

// Latest is the last snapshot that came back.
func (s *Snapshots) Latest() gfx.FrameSnapshot { return s.latest }
