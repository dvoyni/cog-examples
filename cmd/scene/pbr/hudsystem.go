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

// The HUD, drawn with canvas.Text and the font canvas embeds - a text op with an
// empty font path names none, and that is what every demo but the one
// demonstrating a named font writes.
//
// Printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/pbr` sees the numbers without a second command. The two
// numbers this demo owes that box does not are the residency count, because six
// models load after the first frame and an empty room is otherwise
// unexplained, and the lights declared over the cap, because the cap's drop is
// silent everywhere else.
//
// Every number here is one the demo already knows - what it spawned, what model
// holds resident, its own clock - and none is read back out of the frame.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize = 11                   // glyph size
	hudLeft = 16                   // where each line starts
	hudTop  = 14                   // the top of the first line
	hudLine = hudSize * 7 / 5      // baseline-to-baseline
	hudFoot = hudTop + hudSize*2/3 // the footer's inset from the bottom
)

type (
	modelCount  struct{ Model scene.Model }
	meshCount   struct{ Mesh scene.Mesh }
	lightCount  struct{ Light scene.Light }
	cameraCount struct{ Camera scene.Camera }
)

// hud counts the world, asks model which stations are resident, prints the
// demo's key numbers on screen, and clears the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and its default pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
//
// Residency is asked through model's read facade, which never loads: a path
// that is loading, failed or was never named is absent alike, which is exactly
// what "not drawable yet" means.
func hud(
	models *ecs.Query[modelCount],
	meshes *ecs.Query[meshCount],
	lights *ecs.Query[lightCount],
	cameras *ecs.Query[cameraCount],
	lookup *ecs.Read[*model.Lookup],
	state *ecs.Write[*Pbr],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	p, q := state.Get(), overlay.Get()
	p.rate.measure(time.Now())

	read := model.NewLookupReadAccess(lookup.Get())
	for i := range stations {
		_, p.resident[i] = read.Handle(model.ModelRef{Path: stations[i].path})
	}

	var census struct{ models, meshes, lights, cameras int }
	for range models.All() {
		census.models++
	}
	for range meshes.All() {
		census.meshes++
	}
	for range lights.All() {
		census.lights++
	}
	for range cameras.All() {
		census.cameras++
	}

	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	status := "running"
	if p.paused {
		status = "paused"
	}
	view := "overview"
	if p.focus > 0 {
		view = fmt.Sprintf("%d %s", p.focus, stations[p.focus-1].name)
	}
	// Which lights were packed is decided inside scene, per pass, and a light
	// past the cap is dropped there without a word: that is the rule this demo
	// shows. So the HUD says how many were declared and what the cap is, and
	// that anything past it is dropped silently - not how many were, which the
	// frame alone knows, and which at a station close-up is mostly culling
	// rather than the cap anyway.
	lines := [...]string{
		fmt.Sprintf("pbr  step %06d  time %.2fs  fps %.0f  %s",
			p.step, p.time(), p.rate.perSecond, status),
		fmt.Sprintf("models %d/%d resident   view %s",
			p.ResidentCount(), len(stations), view),
		fmt.Sprintf("entities  models %d  meshes %d  lights %d  cameras %d",
			census.models, census.meshes, census.lights, census.cameras),
		fmt.Sprintf("lights %d declared  cap %d  past it, dropped silently",
			p.declared, MaxLights),
	}
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudColor)
	}
	text(q, screenHeight-hudFoot,
		"space pause   arrows orbit   1-6 station   0 overview   r reset", hudDimColor)
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
