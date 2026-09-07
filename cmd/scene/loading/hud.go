package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// The HUD, drawn with canvas.Text and the font canvas embeds - a text op with an
// empty font path names none, and that is what every demo but the one
// demonstrating a named font writes.
//
// This demo's HUD is not a decoration the way the others' are. A grid of pads
// with nothing on five of them is an ambiguous picture: it does not say whether
// a station is still loading, failed, or resolved a selector to nothing. The
// residency column is what tells those apart, and it is the demo's actual
// output. Printing it to stdout instead would break the rule that a human
// running `go run ./cmd/scene/loading` sees the numbers without a second
// command.

// The HUD's layout, in the logical screen's coordinates. The station block is
// two columns of eight rows, mirroring the grid's four-by-four so a reader can
// find a pad's line without counting.
const (
	// hudSize is a point smaller than the other demos' eleven, and that is the
	// whole reason the two columns fit: sixteen rows of name, state, count and
	// note is a much wider block than any other demo prints, and a column that
	// overruns hudColumn does not wrap - it draws straight through the column
	// beside it and both become unreadable.
	hudSize   = 10
	hudLeft   = 14
	hudTop    = 12
	hudLine   = hudSize * 7 / 5
	hudFoot   = hudTop + hudSize*2/3
	hudColumn = screenWidth / 2
	hudPerCol = len(stations) / 2
	// hudNamePad and hudFixed are the station line's fixed part: the name
	// column, and the whole prefix a note begins after. NoteBudget is the rest
	// of the column, and a test holds every note inside it.
	hudNamePad = 15
	hudFixed   = hudNamePad + 1 + 8 + 1 + 4 + 2
)

// hud prints the residency table and the frame's key numbers, and clears the
// frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and the implicit pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
func (p *Loading) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	settled := "loading"
	if p.Settled() {
		settled = "settled"
	}
	p.text(q, hudLeft, hudTop, fmt.Sprintf(
		"loading  step %06d  time %.2fs  fps %.0f  %s   pose %s  morph %s",
		p.step, p.time(), p.rate.perSecond, settled,
		bytes(p.poseBytes), bytes(p.morphBytes)), hudColor)
	p.text(q, hudLeft, hudTop+hudLine, fmt.Sprintf(
		"draws %d  culled %d  packed %d  batches %d  passes %d  ops %d",
		p.stats.recorded, p.stats.culled, p.stats.instances,
		p.stats.batches, p.stats.passes, p.stats.ops), hudDimColor)

	top := float32(hudTop + hudLine*3)
	for i := range stations {
		x := float32(hudLeft)
		row := i
		if i >= hudPerCol {
			x, row = hudColumn, i-hudPerCol
		}
		y := top + float32(row)*hudLine
		p.text(q, x, y, p.stationLine(i), stateColor(p.states[i], &stations[i]))
	}

	p.text(q, hudLeft, screenHeight-hudFoot-hudLine,
		"u unload truck   t unload its textures   x unload all   r retry the failures",
		hudDimColor)
	p.text(q, hudLeft, screenHeight-hudFoot,
		"a bare pad is a draw that was skipped, never substituted", hudDimColor)
}

// stationLine is one station's row: its name, its residency, how many nodes its
// selector covered, and what it is here for.
func (p *Loading) stationLine(i int) string {
	station := &stations[i]
	nodes := "  -"
	if p.nodeCount[i] >= 0 {
		nodes = fmt.Sprintf("%3d", p.nodeCount[i])
	}
	return fmt.Sprintf("%-*s %-8s n%s  %s",
		hudNamePad, station.name, stateName(p.states[i]), nodes, station.note)
}

// stateName is one residency in the width the table is laid out for.
func stateName(state scene.ModelState) string {
	switch state {
	case scene.ModelLoading:
		return "loading"
	case scene.ModelResident:
		return "resident"
	case scene.ModelFailed:
		return "failed"
	}
	// ModelMissing is unobservable through State on a valid path - the very act
	// of asking moves it to ModelLoading - so this line is what an unloaded
	// slot looks like for the one frame before the next query reloads it.
	return "missing"
}

// stateColor separates a station that reached what its table row expects from
// one that has not. A failed path is green here when failing is the point,
// which is the whole reason the expectation is in the table rather than assumed.
func stateColor(state scene.ModelState, station *station) m.Color {
	switch {
	case state == station.expect:
		return hudOkColor
	case state == scene.ModelLoading:
		return hudDimColor
	default:
		return hudWarnColor
	}
}

// bytes prints a byte count in the unit a human reads it in. The numbers here
// span four orders of magnitude - a static square has no poses at all and one
// animation asset has half a megabyte of them - so a raw count is unreadable
// and a fixed unit is wrong at one end or the other.
func bytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// text draws one line. Position is the top-left of the line, so a caller places
// lines by stepping y and never has to know where the baseline sits.
func (p *Loading) text(q *canvas.OpQueue, x, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: x, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
