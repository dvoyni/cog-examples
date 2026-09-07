package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

// The HUD, drawn with canvas.Text and the font canvas embeds - a text op with
// an empty font path names none, and that is what every demo but the one
// demonstrating a named font writes.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/animated` sees the numbers without a second command, and
// under GOOS=js there is no terminal to print to at all: this demo is the one
// that has to run in a browser, so its numbers have to be in the frame.
//
// The four numbers this demo owes that pbr does not are the crossfade weights,
// because a blend is invisible as a number anywhere else; the plays offered
// against the plays blended, because the cap's drop is a report rather than a
// picture; the pose and delta bytes, because that is the memory the whole
// design trades for a per-vertex loop that does no interpolation; and which
// shape the override is holding, because a sparse array of eight floats is not
// legible from the model.

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
func (a *Animated) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	state := "running"
	if a.paused {
		state = "paused"
	}
	view := "overview"
	if a.focus > 0 {
		view = fmt.Sprintf("%d %s", a.focus, stations[a.focus-1].name)
	}
	walk, run := a.FoxBlend()
	override := "off, animated weights"
	if a.override {
		override = fmt.Sprintf("%s, 1 of %d nonzero",
			StressShapeNames[a.OverrideShape()], StressTargets)
	}

	lines := [...]string{
		fmt.Sprintf("animated  step %06d  time %.2fs  fps %.0f  %s",
			a.step, a.Time(), a.rate.perSecond, state),
		fmt.Sprintf("models %d/%d resident   view %s",
			a.ResidentCount(), len(ModelPaths), view),
		fmt.Sprintf("fox crossfade  walk %.2f  run %.2f  (normalised by scene, not here)",
			walk, run),
		fmt.Sprintf("interp cap  %d plays offered  %d blended  %d clips dropped, reported once",
			InterpClips, CapPlays, InterpClips-CapPlays),
		fmt.Sprintf("morph override  %s", override),
		// The two byte counts are per model and in total. The cubes are the
		// pair worth reading: the same cube at two strides, because the mask
		// is intersected with what each file authored.
		fmt.Sprintf("poses %s KiB   total %s KiB",
			kib(a.memory.pose[0]), kib(a.memory.totalPose)),
		fmt.Sprintf("deltas cube %s / quantized %s / stress %s KiB   total %s KiB",
			kib(a.memory.morph[2]), kib(a.memory.morph[3]),
			kib(a.memory.morph[4]), kib(a.memory.totalMorph)),
		fmt.Sprintf("draws %d  culled %d  packed %d  batches %d  passes %d  ops %d",
			a.stats.recorded, a.stats.culled, a.stats.instances,
			a.stats.batches, a.stats.passes, a.stats.ops),
	}
	for i, line := range lines {
		a.text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	a.text(q, screenHeight-hudFoot,
		"space play/pause   arrows orbit   1-4 station   0 overview   o morph override   r reset",
		hudDimColor)
}

// kib renders a byte count in kibibytes, which is the unit pose and delta
// memory is worth reading in: the numbers here run from a couple of kilobytes
// to a few hundred, and bytes would be six digits of noise.
func kib(bytes int) string { return fmt.Sprintf("%.0f", float64(bytes)/1024) }

// text draws one line at the HUD's left margin. Position is the top-left of the
// line, so a caller places lines by stepping y and never has to know where the
// baseline sits.
func (a *Animated) text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
