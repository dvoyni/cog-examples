package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

// The HUD, drawn with canvas rectangles rather than canvas.Text.
//
// box is the demo that runs with zero assets, and canvas.Text needs a font file
// on disk: a text op with an empty font path draws nothing. So the key numbers
// are printed through a 3x5 bitmap font baked into this file, one FillRect per
// lit pixel. Every other demo has assets and uses canvas.Text; this one cannot,
// and printing to stdout instead would break the rule that a human running
// `go run ./cmd/scene/box` sees the numbers without a second command.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudPixel   = 3  // the side of one glyph pixel
	hudLeft    = 16 // where the first glyph starts
	hudTop     = 14
	hudLine    = hudPixel*5 + 8 // baseline-to-baseline
	hudAdvance = hudPixel * 4   // one glyph plus its gap
)

// hud prints the demo's key numbers on screen, and clears the frame.
//
// The clear is canvas's rather than the camera's because the camera declares no
// passes at all and the implicit pass preserves colour; layerBackdrop sorts
// below the camera, so the order is clear, then the scene, then this text.
func (p *Box) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	state := "RUNNING"
	if p.paused {
		state = "PAUSED"
	}
	visible := "NO"
	if p.stats.sphereVisible {
		visible = "YES"
	}
	lines := [...]string{
		fmt.Sprintf("BOX  STEP %06d  TIME %.2fS  FPS %.0f  %s",
			p.step, p.time(), p.rate.perSecond, state),
		fmt.Sprintf("DRAWS %d  CULLED %d  PACKED %d",
			p.stats.recorded, p.stats.culled, p.stats.instances),
		fmt.Sprintf("PASSES %d  BATCHES %d  OPS %d",
			p.stats.passes, p.stats.batches, p.stats.ops),
		fmt.Sprintf("SPHERE IN FRUSTUM %s", visible),
	}
	for i, line := range lines {
		p.text(q, hudLeft, hudTop+float32(i*hudLine), line, hudColor)
	}
	p.text(q, hudLeft, screenHeight-hudTop-hudPixel*5,
		"SPACE PAUSE   ARROWS ORBIT   R RESET", hudDimColor)
}

// text draws one line of the bitmap font, one FillRect per lit pixel. Runs of
// lit pixels are merged along each row, which turns a solid glyph row from
// three quads into one and keeps a full HUD to a couple of hundred sprites.
func (p *Box) text(q *canvas.OpQueue, x, y float32, line string, color m.Color) {
	for index, char := range line {
		bits, ok := glyphs[char]
		if !ok || bits == 0 {
			continue
		}
		originX := x + float32(index*hudAdvance)
		for row := 0; row < glyphRows; row++ {
			runStart, runLength := 0, 0
			for column := 0; column <= glyphColumns; column++ {
				lit := column < glyphColumns && bits>>uint((glyphRows-1-row)*glyphColumns+(glyphColumns-1-column))&1 == 1
				switch {
				case lit && runLength == 0:
					runStart, runLength = column, 1
				case lit:
					runLength++
				case runLength > 0:
					q.FillRect(layerHUD, m.Rect{
						X:      originX + float32(runStart*hudPixel),
						Y:      y + float32(row*hudPixel),
						Width:  float32(runLength * hudPixel),
						Height: hudPixel,
					}, color)
					runLength = 0
				}
			}
		}
	}
}

// The bitmap font is 3 wide and 5 tall, packed into a uint16 row-major with the
// top row in the high bits, so a glyph literal reads as the shape it draws.
const (
	glyphColumns = 3
	glyphRows    = 5
)

// glyph packs five rows of three bits, top row first.
func glyph(r0, r1, r2, r3, r4 uint16) uint16 {
	return r0<<12 | r1<<9 | r2<<6 | r3<<3 | r4
}

// glyphs is the font: the digits, the uppercase letters and the handful of
// punctuation marks the HUD's lines use. An unmapped rune draws nothing, so a
// line that grows a character this table does not have loses that character
// rather than the frame.
var glyphs = map[rune]uint16{
	' ': 0,
	'.': glyph(0b000, 0b000, 0b000, 0b000, 0b010),
	':': glyph(0b000, 0b010, 0b000, 0b010, 0b000),
	'-': glyph(0b000, 0b000, 0b111, 0b000, 0b000),
	'/': glyph(0b001, 0b001, 0b010, 0b100, 0b100),
	'0': glyph(0b111, 0b101, 0b101, 0b101, 0b111),
	'1': glyph(0b010, 0b110, 0b010, 0b010, 0b111),
	'2': glyph(0b111, 0b001, 0b111, 0b100, 0b111),
	'3': glyph(0b111, 0b001, 0b111, 0b001, 0b111),
	'4': glyph(0b101, 0b101, 0b111, 0b001, 0b001),
	'5': glyph(0b111, 0b100, 0b111, 0b001, 0b111),
	'6': glyph(0b111, 0b100, 0b111, 0b101, 0b111),
	'7': glyph(0b111, 0b001, 0b001, 0b001, 0b001),
	'8': glyph(0b111, 0b101, 0b111, 0b101, 0b111),
	'9': glyph(0b111, 0b101, 0b111, 0b001, 0b111),
	'A': glyph(0b111, 0b101, 0b111, 0b101, 0b101),
	'B': glyph(0b110, 0b101, 0b110, 0b101, 0b110),
	'C': glyph(0b111, 0b100, 0b100, 0b100, 0b111),
	'D': glyph(0b110, 0b101, 0b101, 0b101, 0b110),
	'E': glyph(0b111, 0b100, 0b111, 0b100, 0b111),
	'F': glyph(0b111, 0b100, 0b111, 0b100, 0b100),
	'G': glyph(0b111, 0b100, 0b101, 0b101, 0b111),
	'H': glyph(0b101, 0b101, 0b111, 0b101, 0b101),
	'I': glyph(0b111, 0b010, 0b010, 0b010, 0b111),
	'J': glyph(0b001, 0b001, 0b001, 0b101, 0b111),
	'K': glyph(0b101, 0b101, 0b110, 0b101, 0b101),
	'L': glyph(0b100, 0b100, 0b100, 0b100, 0b111),
	'M': glyph(0b101, 0b111, 0b111, 0b101, 0b101),
	'N': glyph(0b110, 0b101, 0b101, 0b101, 0b101),
	'O': glyph(0b111, 0b101, 0b101, 0b101, 0b111),
	'P': glyph(0b111, 0b101, 0b111, 0b100, 0b100),
	'Q': glyph(0b111, 0b101, 0b101, 0b111, 0b001),
	'R': glyph(0b111, 0b101, 0b110, 0b101, 0b101),
	'S': glyph(0b111, 0b100, 0b111, 0b001, 0b111),
	'T': glyph(0b111, 0b010, 0b010, 0b010, 0b010),
	'U': glyph(0b101, 0b101, 0b101, 0b101, 0b111),
	'V': glyph(0b101, 0b101, 0b101, 0b101, 0b010),
	'W': glyph(0b101, 0b101, 0b111, 0b111, 0b101),
	'X': glyph(0b101, 0b101, 0b010, 0b101, 0b101),
	'Y': glyph(0b101, 0b101, 0b010, 0b010, 0b010),
	'Z': glyph(0b111, 0b001, 0b010, 0b100, 0b111),
}
