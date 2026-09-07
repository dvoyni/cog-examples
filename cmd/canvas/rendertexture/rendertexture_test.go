package main

import (
	"testing"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog-examples/internal/headless"
)

// The recording assertions run against a bare canvas.OpQueue rather than an
// engine, because what they check is what the demo said and not what a GPU did
// with it. The queue is an ordinary value, so this needs no plugins at all.
func recorded(t *testing.T) (*canvas.OpQueue, gfx.TextureDescr) {
	t.Helper()
	// Stand-in handles for the pair TemporaryTarget hands back. They carry no
	// backend, which is all this level needs: the demo passes them through.
	texture := gfx.TextureWithBytes(panelSize, panelSize, gfx.FormatRGBA8Srgb, nil, false, false)
	demo := New()
	q := &canvas.OpQueue{}
	demo.recordPanel(q, gfx.TextureTarget(texture, 0, 0))
	demo.recordScreen(q, texture)
	return q, texture
}

// The panel layer is the one with a target, and the screen layer is the one
// without. Both clear, which is the whole reason a clear belongs to a layer
// rather than to the frame.
func TestOnlyThePanelLayerHasATarget(t *testing.T) {
	q, _ := recorded(t)

	if _, ok := q.LayerTarget(layerPanel); !ok {
		t.Error("the panel layer has no target, so it drew on the screen")
	}
	if _, ok := q.LayerTarget(layerScreen); ok {
		t.Error("the screen layer has a target, so nothing reached the screen")
	}
	if _, ok := q.LayerClear(layerPanel); !ok {
		t.Error("the panel layer does not clear, so it draws over a pooled target's leftovers")
	}
	if _, ok := q.LayerClear(layerScreen); !ok {
		t.Error("the screen layer does not clear")
	}
	if layerPanel >= layerScreen {
		t.Fatalf("layers = panel %d, screen %d: the pass that writes the texture must order before the one that reads it",
			layerPanel, layerScreen)
	}
}

// Part 3 of what render-to-texture is for, in one assertion: the unit of
// exchange is a plain gfx texture, so both consumers name the very same handle
// the panel rendered into - and a scene material would take it the same way.
func TestBothScreenDrawsSourceTheSameTexture(t *testing.T) {
	q, _ := recorded(t)

	sourced := map[canvas.OpKind]int{}
	for _, op := range q.Ops(nil) {
		width, height := op.Texture.Size()
		if op.Layer != layerScreen || width == 0 {
			continue
		}
		if width != panelSize || height != panelSize {
			t.Errorf("a %v sampled a %dx%d texture, want the panel's %d square", op.Kind, width, height, panelSize)
		}
		sourced[op.Kind]++
	}
	if sourced[canvas.OpSprite] != 1 || sourced[canvas.OpTriangles] != 1 || len(sourced) != 2 {
		t.Fatalf("screen draws sourcing the panel = %v, want one sprite and one triangle list", sourced)
	}
}

// Neither consumer names a material, because the default is the one that must
// be used: the sprite and triangle materials run the key-colour ramp, which
// turns every dark, low-green texel of a rendered image grey at its own red
// intensity with no key colour that switches it off.
func TestNoScreenDrawOverridesTheTextureMaterial(t *testing.T) {
	q, _ := recorded(t)

	for _, op := range q.Ops(nil) {
		if op.Layer == layerScreen && op.HasMaterial {
			t.Errorf("a screen draw carries a material of its own: %v", op.Kind)
		}
	}
}

// The frame the GPU actually sees: the panel's pass, then the screen's, with a
// barrier between them. The barrier is the only observable for the hazard - a
// sample unordered against the writes that produced it looks like a random
// flicker no screenshot catches, and gfx places the transition, not this demo.
func TestTheFrameRendersThePanelThenSamplesIt(t *testing.T) {
	engine := headless.New(t, New())
	engine.Steps(1)
	backend := engine.Backend()

	if len(backend.Passes) != 2 {
		t.Fatalf("GPU passes = %d, want the panel's then the screen's", len(backend.Passes))
	}
	panel, screen := backend.Passes[0], backend.Passes[1]
	if panel.Screen || panel.NoColor || panel.Target == 0 {
		t.Errorf("first pass = %+v, want a texture attachment", panel)
	}
	if !screen.Screen {
		t.Errorf("second pass = %+v, want the screen", screen)
	}
	if len(backend.Transitions) != 1 {
		t.Fatalf("transitions = %v, want one ordering the sample against the write", backend.Transitions)
	}
	got := backend.Transitions[0]
	if got.From != gfx.TextureUsageRenderAttachment || got.To != gfx.TextureUsageTextureBinding {
		t.Errorf("transition = %v -> %v, want render attachment to texture binding", got.From, got.To)
	}
	if got.BeforePass != 1 {
		t.Errorf("transition placed before pass %d, want before pass 1, the sampling one", got.BeforePass)
	}
	if errs := engine.Errors(); len(errs) != 0 {
		t.Fatalf("engine reported %v", errs)
	}
}

// The panel measures against its target, not the viewport, so the squares are
// laid out in texels of a 256 square while the window is 960x540. Nothing else
// in the demo would notice if that rule broke - the picture would simply be the
// wrong size inside the texture - so it is asserted rather than looked at.
func TestThePanelIsLaidOutInTexelsOfItsTarget(t *testing.T) {
	q, _ := recorded(t)

	window, _, ok := q.LayerWindow(layerPanel)
	if !ok || window != (m.Rect{Width: panelSize, Height: panelSize}) {
		t.Fatalf("panel window = (%+v, %v), want the target's own %d square", window, ok, panelSize)
	}
	var squares int
	for _, op := range q.Ops(nil) {
		if op.Layer != layerPanel || op.Kind != canvas.OpSprite {
			continue
		}
		if op.Transform.Size.X > panelSize || op.Transform.Size.Y > panelSize {
			t.Errorf("square %d is %v, which does not fit the %d target", squares, op.Transform.Size, panelSize)
		}
		squares++
	}
	if squares != len(panelColors) {
		t.Fatalf("squares recorded = %d, want %d", squares, len(panelColors))
	}
}
