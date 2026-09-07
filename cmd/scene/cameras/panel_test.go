package main

import (
	"testing"

	"github.com/dvoyni/cog/m"
)

// The two panels are deliberately unlike each other, and every test here leans
// on that. They differ in aspect (1.351 against 1.0) and the minimap differs in
// scale as well - 512 texels composited into 400 canvas units - so a mapping
// that used one panel's numbers for the other, or dropped the texel-to-canvas
// scale entirely, moves a point somewhere these assertions can see.

func TestAPanelMapsItsOwnCornersOntoItsOwnTexture(t *testing.T) {
	for _, p := range []struct {
		name  string
		panel panel
	}{{"main", mainPanel}, {"map", mapPanel}} {
		texel, ok := p.panel.texel(m.Vec2{X: p.panel.rect.X, Y: p.panel.rect.Y})
		if !ok || texel != (m.Vec2{}) {
			t.Errorf("%s: top-left canvas corner mapped to %v (ok %v), want the texture origin",
				p.name, texel, ok)
		}
		far := m.Vec2{
			X: p.panel.rect.X + p.panel.rect.Width,
			Y: p.panel.rect.Y + p.panel.rect.Height,
		}
		texel, ok = p.panel.texel(far)
		if !ok || texel != p.panel.size {
			t.Errorf("%s: bottom-right canvas corner mapped to %v (ok %v), want %v",
				p.name, texel, ok, p.panel.size)
		}
	}
}

func TestAPointOutsideAPanelIsNotInIt(t *testing.T) {
	// The click has to know which viewport it landed in before it can build a
	// ray, and the two panels do not overlap, so "outside" is the only answer
	// that keeps a click in the gap between them from picking through the
	// nearer camera.
	outside := []m.Vec2{
		{X: mainPanel.rect.X - 1, Y: mainPanel.rect.Y + 10},
		{X: mainPanel.rect.X + 10, Y: mainPanel.rect.Y - 1},
		{X: mainPanel.rect.X + mainPanel.rect.Width + 1, Y: mainPanel.rect.Y + 10},
		{X: mainPanel.rect.X + 10, Y: mainPanel.rect.Y + mainPanel.rect.Height + 1},
	}
	for _, point := range outside {
		if _, ok := mainPanel.texel(point); ok {
			t.Errorf("%v reported inside the main panel", point)
		}
	}
	for _, point := range outside {
		if _, ok := mapPanel.texel(point); ok {
			t.Errorf("%v reported inside the map panel, which is nowhere near it", point)
		}
	}
}

func TestTexelAndCanvasRoundTripThroughEachOther(t *testing.T) {
	// The nameplate goes one way and the click goes the other, so a scale
	// applied in one direction and forgotten in the other is exactly the bug
	// that makes a label land right and a click land wrong.
	for _, p := range []struct {
		name  string
		panel panel
	}{{"main", mainPanel}, {"map", mapPanel}} {
		for _, texel := range []m.Vec2{{}, {X: 17, Y: 31}, {X: p.panel.size.X, Y: p.panel.size.Y}} {
			back, ok := p.panel.texel(p.panel.canvas(texel))
			if !ok {
				t.Errorf("%s: texel %v left the panel on the way back", p.name, texel)
				continue
			}
			if !nearVec2(back, texel, 1e-3) {
				t.Errorf("%s: texel %v round-tripped to %v", p.name, texel, back)
			}
		}
	}
}

// The minimap composites 512 texels into 400 canvas units, so a texel is 400/512
// of a canvas unit there and exactly one in the main view. Spelling the number
// out is what makes this a test of the scale rather than of itself.
func TestTheMinimapCompositesAtItsOwnScale(t *testing.T) {
	if mapPanel.size.X == mapPanel.rect.Width {
		t.Fatal("the minimap renders at its composited size, so nothing here tests a scale at all")
	}
	moved := mapPanel.canvas(m.Vec2{X: 256, Y: 0}).Sub(mapPanel.canvas(m.Vec2{}))
	want := 256 * mapPanel.rect.Width / mapPanel.size.X
	if !nearVec2(moved, m.Vec2{X: want}, 1e-3) {
		t.Errorf("256 texels across is %v canvas units, want %v", moved.X, want)
	}
}

func nearVec2(a, b m.Vec2, tolerance float32) bool {
	return abs(a.X-b.X) <= tolerance && abs(a.Y-b.Y) <= tolerance
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
