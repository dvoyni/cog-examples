package main

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
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
// against the plays an Animation holds, because the cap's cut is not a
// picture; the pose and delta bytes, because that is the memory the whole
// design trades for a per-vertex loop that does no interpolation; and which
// clip the contrast copy is playing, because two copies of one model moving
// differently is otherwise a puzzle.
//
// Every other number is one the demo already knows - the files resident and
// the Entities it spawned - rather than one read back out of the renderer.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

// ratePeriod is how long the frames-per-second meter counts before
// republishing.
const ratePeriod = 250 * time.Millisecond

type (
	hudFoxQuery struct {
		Gait Gait
		Pose scene.Animation
	}
	modelCount     struct{ Model scene.Model }
	animationCount struct{ Pose scene.Animation }
	meshCount      struct{ Draw scene.Mesh }
	cameraCount    struct{ Camera scene.Camera }
)

// hud counts the world, reads the fox's blend, and prints the demo's key
// numbers on screen. It also clears the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and its default pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
func hud(
	foxes *ecs.Query[hudFoxQuery],
	models *ecs.Query[modelCount],
	animations *ecs.Query[animationCount],
	meshes *ecs.Query[meshCount],
	cameras *ecs.Query[cameraCount],
	state *ecs.Write[*Demo],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	d, q := state.Get(), overlay.Get()
	d.rate.measure(time.Now())

	d.census = census{}
	for range models.All() {
		d.census.models++
	}
	for range animations.All() {
		d.census.animations++
	}
	for range meshes.All() {
		d.census.meshes++
	}
	for range cameras.All() {
		d.census.cameras++
	}
	for _, it := range foxes.All() {
		d.fox, d.lastGait = it.Pose.Plays, it.Gait.Last
	}

	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	clock := "running"
	if d.paused {
		clock = "paused"
	}
	view := "overview"
	if d.focus > 0 {
		view = fmt.Sprintf("%d %s", d.focus, stations[d.focus-1].name)
	}
	walk, run := d.GaitWeights()
	contrast := fmt.Sprintf("off, both copies on %s", stressClip)
	if d.contrast {
		contrast = fmt.Sprintf("%s beside %s", stressContrastClip, stressClip)
	}

	lines := [...]string{
		fmt.Sprintf("animated  step %06d  time %.2fs  fps %.0f  %s",
			d.step, d.Time(), d.rate.perSecond, clock),
		fmt.Sprintf("models %d/%d resident   view %s",
			d.ResidentCount(), len(ModelPaths), view),
		fmt.Sprintf("fox gait machine  walk %.2f  run %.2f  last: %s",
			walk, run, d.lastGait),
		fmt.Sprintf("interp cap  %d clips offered  %d fit an Animation  %d cut",
			InterpClips, CapPlays, InterpClips-CapPlays),
		fmt.Sprintf("morph contrast  %s", contrast),
		// The two byte counts are per model and in total. The cubes are the
		// pair worth reading: the same cube at two strides, because the mask
		// is intersected with what each file authored.
		fmt.Sprintf("poses %s KiB   total %s KiB",
			kib(d.memory.pose[0]), kib(d.memory.totalPose)),
		fmt.Sprintf("deltas cube %s / quantized %s / stress %s KiB   total %s KiB",
			kib(d.memory.morph[2]), kib(d.memory.morph[3]),
			kib(d.memory.morph[4]), kib(d.memory.totalMorph)),
		fmt.Sprintf("entities %d  Model %d  Animation %d  Mesh %d  Camera %d",
			d.spawned, d.census.models, d.census.animations,
			d.census.meshes, d.census.cameras),
	}
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	text(q, screenHeight-hudFoot,
		"space play/pause   arrows orbit   1-4 station   0 overview   o morph contrast   r reset",
		hudDimColor)
}

// GaitWeights is the weight the fox's gait machine gave each clip on the last
// tick, Walk first. They are the machine's own and not normalised here: scene
// normalises an Entity's plays before anything is packed, because the blend is
// a weighted mean of TRS.
func (d *Demo) GaitWeights() (walk, run float32) {
	for _, play := range d.fox {
		switch play.Clip {
		case foxWalk:
			walk += play.Weight
		case foxRun:
			run += play.Weight
		}
	}
	return walk, run
}

// FoxPlays is the fox's live plays on the last tick, the empty slots left out.
func (d *Demo) FoxPlays() []model.ClipPlay {
	var plays []model.ClipPlay
	for _, play := range d.fox {
		if play.Clip != "" {
			plays = append(plays, play)
		}
	}
	return plays
}

func (r *rate) measure(now time.Time) {
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

// kib renders a byte count in kibibytes, which is the unit pose and delta
// memory is worth reading in: the numbers here run from a couple of kilobytes
// to a few hundred, and bytes would be six digits of noise.
func kib(bytes int) string { return fmt.Sprintf("%.0f", float64(bytes)/1024) }

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
