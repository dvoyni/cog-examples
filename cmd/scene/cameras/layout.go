package main

import "github.com/dvoyni/cog/m"

// The logical screen the whole frame is laid out in. The window is fitted to
// it, so the split stays put at any window size.
//
// It is the window's own default size rather than the 960x540 the sibling demos
// use, because this demo's numbers are pixel mappings and a reader checking
// them against a screenshot should not have to divide by 1.333 first.
const (
	screenWidth  = 1280
	screenHeight = 720
)

// The two viewports, and the one thing worth reading twice: rect is canvas
// units and size is texels, and they are equal for one panel and not the other.
//
// mainPanel is the perspective view. Its target matches its rectangle, so a
// texel is a canvas unit there and the mapping is a translation.
//
// mapPanel is the orthographic minimap. It renders 512 texels into 400 canvas
// units - a 1.28x supersample - which is what scene means by having no
// RenderScale field: a temporary target smaller or larger than where it lands
// already is one. It is also what keeps the demo honest, because the main
// view's mapping cannot tell a missing scale factor from a correct one.
var (
	mainPanel = panel{
		rect: m.Rect{X: 16, Y: 88, Width: 832, Height: 592},
		size: m.Vec2{X: 832, Y: 592},
	}
	mapPanel = panel{
		rect: m.Rect{X: 864, Y: 88, Width: 400, Height: 400},
		size: m.Vec2{X: 512, Y: 512},
	}
)
