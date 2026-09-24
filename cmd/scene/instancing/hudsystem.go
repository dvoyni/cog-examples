package main

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// The HUD, drawn with canvas.Text and the font canvas embeds - a text op with
// an empty font path names none, and that is what every demo but the one
// demonstrating a named font writes.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/instancing` sees the numbers without a second command.
// Every number on it is one the demo decided or asked for itself - what it
// spawned, which files are resident, how many Batch keys its crates hold -
// rather than one read back out of the frame: what those keys cost in draws is
// the backend's to count, and instancing_test.go counts it there.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

// hud reads residency, prints the demo's key numbers on screen, and clears the
// frame.
//
// The clear is canvas's rather than the camera's because the camera's one
// default pass keeps colour; layerBackdrop sorts below the camera, so the
// order is clear, then the scene, then this text.
//
// Residency is asked of model's read facade, which never loads: the load
// System is what loads the files the crates, bottles and screens name, and the
// HUD only watches it happen.
func hud(
	lookup *ecs.Read[*model.Lookup],
	state *ecs.Write[*Demo],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	d := state.Get()
	d.rate.measure(time.Now())
	read := model.NewLookupReadAccess(lookup.Get())
	for i, path := range modelPaths {
		handle, ok := read.Handle(model.ModelRef{Path: path})
		d.resident[i] = ok && read.Resident(handle)
	}

	q := overlay.Get()
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	status := "running"
	if d.paused {
		status = "paused"
	}
	mode := "1 one shared key"
	draws := "the field and the stack are one instanced draw"
	if d.split {
		mode = "2 one key per crate"
		draws = "every crate in view is a draw of its own"
	}
	lines := []string{
		fmt.Sprintf("instancing  step %06d  time %.2fs  fps %.0f  %s",
			d.step, d.time(), d.rate.perSecond, status),
		fmt.Sprintf("models %d/%d resident   field %d crates, %d of them pillars   mode %s",
			d.ResidentCount(), len(modelPaths), CrateCount, PillarCount, mode),
		fmt.Sprintf("entities: %d crates, %d stacked, %d bottles (%d squat), %d screens, %d debug shapes",
			CrateCount, stackCount, bottleCount, squatCount, len(paneTransforms), debugShapes),
		fmt.Sprintf("crate Batch keys %d: %s", d.FieldKeys(), draws),
		"equal keys batch into one draw; a blended primitive draws once per instance",
	}
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	text(q, screenHeight-hudFoot,
		"space pause   arrows orbit   1 shared key   2 key per crate   r reset", hudDimColor)
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
