package main

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// The HUD's geometry, in the logical viewport main.go declares.
//
// Every row is a named constant rather than an offset from the row above it,
// because arithmetic that says "two lines below the last thing" silently
// overlaps as soon as a line is added. The one rule the whole layout obeys is
// that a line of bodySz text is about 0.6 x bodySz wide per character, so
// maxLine characters is what fits between the margins - and every string below
// is written to that budget.
const (
	margin   = 28
	titleSz  = 17
	bodySz   = 13
	bodyLine = 19
	// maxLine is how many characters of bodySz text fit between the margins:
	// (960 - 56) / (0.6 x 13), rounded down and given a little room.
	maxLine = 110

	titleY    = 34
	deviceY   = 60
	countersY = 80

	// The music's own progress bar, wide enough that ten seconds of Seek is a
	// visible jump on a track of 176.
	trackY = 100
	trackH = 10

	// The two lines about the music Clip itself, below the bar.
	clipY1 = 132
	clipY2 = 151

	// The Voice table.
	headerY     = 180
	rowY        = 200
	rowH        = 22
	rowTextDrop = 15

	// The paused banner: a panel tall enough for a heading and two lines,
	// centred on the table it covers.
	bannerY = 248
	bannerH = 100

	// The two lines of key legend, measured up from the bottom edge.
	keysY1 = screenHeight - margin - bodyLine
	keysY2 = screenHeight - margin

	// The table's columns. The playhead column is the widest because its value
	// is two padded numbers and a unit.
	colClip   = margin
	colBus    = margin + 110
	colPitch  = margin + 200
	colGain   = margin + 275
	colHead   = margin + 385
	colPaused = margin + 540
	barLeft   = margin + 610
	barWidth  = float32(screenWidth) - (margin + 610) - margin
)

var (
	colorBack    = m.NewColorSrgb(0.05, 0.06, 0.08, 1)
	colorInk     = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	colorDim     = m.NewColorSrgb(0.46, 0.50, 0.58, 1)
	colorTrough  = m.NewColorSrgb(0.12, 0.13, 0.17, 1)
	colorMusic   = m.NewColorSrgb(0.42, 0.71, 0.94, 1)
	colorEffects = m.NewColorSrgb(0.95, 0.72, 0.35, 1)
	colorWarn    = m.NewColorSrgb(0.95, 0.55, 0.40, 1)
	colorPanel   = m.NewColorSrgb(0.10, 0.11, 0.15, 1)
)

// draw records the whole frame: the header, the music's progress bar, one row
// per live Voice, and the keys.
//
// Every number it prints comes off one of the four sound views it is handed.
// The demo's own state reaches this function in exactly three places - the
// three slider positions, the count of one-shots recorded, and which Clip a
// ClipRef names - and each of those is something the engine has no opinion
// about. Everything the engine does have an opinion about is asked rather than
// remembered, which is the rule every audio demo here follows.
func (p *mixer) draw(
	q *canvas.OpQueue,
	live *sound.Voices,
	volumes *sound.Buses,
	clips kernel.Read[*sound.Clips],
	dev *sound.Device,
	paused bool,
) {
	q.Clear(layerBackdrop, colorBack)
	window := m.Rect{Width: screenWidth, Height: screenHeight}
	q.SetLayerTransform(layerBackdrop, window, canvas.AspectInscribe)
	q.SetLayerTransform(layerHUD, window, canvas.AspectInscribe)

	info, state := sound.ClipInfoOf(clips, p.sounds[clipMusic].ref)

	p.drawHeader(q, live, volumes, dev, info, state)
	p.drawTrack(q, live, info)
	p.drawTable(q, live)
	p.drawKeys(q)
	if paused {
		drawPausedBanner(q)
	}
}

// drawHeader says what the demo is, what device it got, how full the table is
// and where the three sliders stand.
func (p *mixer) drawHeader(
	q *canvas.OpQueue,
	live *sound.Voices,
	volumes *sound.Buses,
	dev *sound.Device,
	info sound.ClipInfo,
	state sound.State,
) {
	text(q, margin, titleY, titleSz,
		"mixer — a streamed track under one-shots that overlap it, on two Buses", colorInk)

	// The device line is the honest one. Absent, not opened yet and lost all
	// look the same from here and a game cannot tell them apart, so the demo
	// says what it knows and adds the one thing that stays true either way.
	if dev.Ready {
		text(q, margin, deviceY, bodySz, fmt.Sprintf(
			"device  %s  %d Hz  %d ch  %.1f ms latency",
			dev.Name, dev.SampleRate, dev.Channels, float64(dev.Latency.Microseconds())/1000), colorDim)
	} else {
		text(q, margin, deviceY, bodySz,
			"device  not ready — nothing is audible; a browser needs one click first. "+
				"Playheads still advance.", colorWarn)
	}

	// Bus volumes are read back off sound.Buses rather than recomputed from the
	// sliders: what a Bus is set to as of the last flush is the engine's answer,
	// and a slider that disagreed with it would be the bug this line catches.
	text(q, margin, countersY, bodySz, fmt.Sprintf(
		"voices %2d / %2d      master %.2f      music bus %.2f      effects bus %.2f      one-shots fired %d",
		live.Len(), live.Cap(), p.master,
		volumes.Volume(BusMusic), volumes.Volume(BusEffects), p.fired), colorInk)

	text(q, margin, clipY1, bodySz, fmt.Sprintf(
		"%s — %s, %.2f s, %d ch, %d Hz",
		musicPath, clipStateName(state), info.Duration, info.Channels, info.SampleRate), colorDim)
	text(q, margin, clipY2, bodySz,
		"2.7 MiB encoded / 59.3 MiB decoded (21.8x): over the 512 KiB "+
			"DecodedClipLimit, so it streams, unasked.", colorDim)
}

// drawTrack draws the music's playhead across its whole duration, which is what
// makes a ten-second Seek something you can see land rather than a number that
// changed.
func (p *mixer) drawTrack(q *canvas.OpQueue, live *sound.Voices, info sound.ClipInfo) {
	trough := m.Rect{X: margin, Y: trackY, Width: screenWidth - 2*margin, Height: trackH}
	q.FillRect(layerBackdrop, trough, canvas.ShapeDraw{Color: colorTrough})
	music, ok := live.Info(p.voice)
	if !ok || info.Duration <= 0 {
		return
	}
	filled := trough
	filled.Width = trough.Width * max(0, min(1, music.Playhead/info.Duration))
	q.FillRect(layerBackdrop, filled, canvas.ShapeDraw{Color: colorMusic})
}

// drawTable draws one row per live Voice, each with a bar as long as the
// audibility sound derived for it.
//
// The bar is the point of the window. Audibility is volume x bus x falloff x
// cone, so moving one Bus moves every bar in that Bus's colour and leaves the
// others exactly where they were - which is the settings-screen case stated as
// something an eye can check in one frame.
func (p *mixer) drawTable(q *canvas.OpQueue, live *sound.Voices) {
	text(q, colClip, headerY, bodySz, "clip", colorDim)
	text(q, colBus, headerY, bodySz, "bus", colorDim)
	text(q, colPitch, headerY, bodySz, "pitch", colorDim)
	text(q, colGain, headerY, bodySz, "audibility", colorDim)
	text(q, colHead, headerY, bodySz, "playhead", colorDim)
	text(q, colPaused, headerY, bodySz, "paused", colorDim)
	text(q, barLeft, headerY, bodySz, "audibility, drawn", colorDim)

	if live.Len() == 0 {
		text(q, colClip, rowY+rowTextDrop, bodySz,
			"no Voices — press M for the music, SPACE for a one-shot", colorDim)
		return
	}

	row := 0
	for voice := range live.All() {
		y := float32(rowY + row*rowH)
		ink := colorEffects
		if voice.Bus == BusMusic {
			ink = colorMusic
		}

		q.FillRect(layerBackdrop,
			m.Rect{X: barLeft, Y: y + 4, Width: barWidth, Height: rowH - 9},
			canvas.ShapeDraw{Color: colorTrough})
		q.FillRect(layerBackdrop,
			m.Rect{X: barLeft, Y: y + 4, Width: barWidth * max(0, min(1, voice.Audibility)), Height: rowH - 9},
			canvas.ShapeDraw{Color: ink})

		ty := y + rowTextDrop
		text(q, colClip, ty, bodySz, p.labelFor(voice.Clip), ink)
		text(q, colBus, ty, bodySz, busName(voice.Bus), colorDim)
		text(q, colPitch, ty, bodySz, fmt.Sprintf("%.2f", voice.Params.Pitch.Or(1)), colorInk)
		text(q, colGain, ty, bodySz, fmt.Sprintf("%.3f", voice.Audibility), colorInk)
		text(q, colHead, ty, bodySz,
			fmt.Sprintf("%6.2f / %6.2f s", voice.Playhead, voice.Duration), colorInk)
		// Paused is sound's resolved answer rather than the game's: a Voice its
		// own Params suspended and a Voice an engine pause suspended both read
		// true here, which is what a reader asking whether this playhead is
		// moving actually wants to know.
		if voice.Paused {
			text(q, colPaused, ty, bodySz, "yes", colorWarn)
		} else {
			text(q, colPaused, ty, bodySz, "-", colorDim)
		}
		row++
	}

	if live.Len() >= live.Cap() {
		text(q, colClip, float32(rowY+row*rowH)+rowTextDrop, bodySz,
			"the table is full — a one-shot now steals the quietest thing in its own band, "+
				"and the music is a band above", colorWarn)
	}
}

// drawKeys is the legend, which a demo driven from the keyboard has to carry on
// screen or it is a demo nobody can drive.
func (p *mixer) drawKeys(q *canvas.OpQueue) {
	text(q, margin, keysY1, bodySz,
		"M music on/off    SPACE fire one    1 2 3 fire at pitch 0.70 / 1.00 / 1.45    "+
			"B fire sixteen at once", colorDim)
	text(q, margin, keysY2, bodySz,
		"S suspend music    LEFT RIGHT seek ±10 s    [ ] music bus    , . fx bus    "+
			"- = master    P pause    ESC quit", colorDim)
}

// drawPausedBanner is drawn by the tick that is about to stop the tick source,
// so it is what stays on screen for as long as the pause lasts.
//
// It records onto layerHUD rather than the backdrop, and last. Painter's order
// inside a layer is record order and a layer covers the one below it, so a
// filled panel recorded here hides both the audibility bars on the backdrop and
// the table text already recorded above them. The same panel on the backdrop
// would sit under every row it is meant to cover.
func drawPausedBanner(q *canvas.OpQueue) {
	panel := m.Rect{X: margin, Y: bannerY, Width: screenWidth - 2*margin, Height: bannerH}
	q.FillRect(layerHUD, panel, canvas.ShapeDraw{Color: colorPanel})
	q.StrokeRect(layerHUD, panel, canvas.ShapeDraw{Color: colorWarn, Thickness: 2})
	text(q, margin+20, bannerY+30, titleSz, "PAUSED — press P to resume", colorWarn)
	text(q, margin+20, bannerY+56, bodySz,
		"app.TimePause stops the update tick and nothing else. The window is live; this frame is "+
			"the last one composed.", colorDim)
	text(q, margin+20, bannerY+56+bodyLine, bodySz,
		"no tick means nothing redraws. sound suspended every Voice on the sample it will resume "+
			"them from.", colorDim)
}

// text is one line of HUD, positioned by its left edge and its baseline row.
func text(q *canvas.OpQueue, x, y, size float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{Position: m.Vec2{X: x, Y: y}, Size: size, Color: color})
}

// busName names one of the demo's Buses for the table. Master is named too,
// even though nothing here is on it: a Bus outside the declared range resolves
// to Master rather than erroring, so a row that said "master" would be the
// symptom of a mis-declared constant and is worth being able to read.
func busName(bus sound.Bus) string {
	switch bus {
	case BusMusic:
		return "music"
	case BusEffects:
		return "effects"
	case sound.Master:
		return "master"
	default:
		return fmt.Sprintf("bus %d", int(bus))
	}
}

// clipStateName says where a Clip is between being named and being playable. A
// duration of zero and a state of loading are different answers to different
// questions, which is why the state is printed beside the facts rather than
// inferred from them.
func clipStateName(state sound.State) string {
	switch state {
	case sound.ClipReady:
		return "ready"
	case sound.ClipFailed:
		return "failed"
	default:
		return "loading"
	}
}
