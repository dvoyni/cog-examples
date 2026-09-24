package main

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"

	"github.com/dvoyni/cog-examples/internal/headless"
)

// The tracer's frame reaches the backend as the narrowest path should: the
// camera's one pass, clearing colour and depth, and in it one draw per box,
// both through the bundled scene shader because neither box names a Material.
func TestTheFrameIsOnePassAndTwoBoxes(t *testing.T) {
	engine := headless.New(t, New())
	engine.Steps(3)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	backend := engine.Backend()
	passes, draws := len(backend.Passes), len(backend.Draws)
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}

	camera := -1
	for i, pass := range backend.Passes[passes:] {
		if pass.Label == "scene.camera-100.forward" {
			if camera >= 0 {
				t.Fatalf("the step began the camera's pass twice")
			}
			camera = passes + i
			if pass.Load != gfx.LoadClear || pass.DepthLoad != gfx.LoadClear {
				t.Errorf("the camera's pass loads %v and depth %v, want both cleared", pass.Load, pass.DepthLoad)
			}
		}
	}
	if camera < 0 {
		t.Fatalf("the step began no pass for the camera")
	}
	boxes := 0
	for _, draw := range backend.Draws[draws:] {
		if draw.Pass != camera {
			continue
		}
		if !backend.IsScenePipeline(draw.Pipeline) {
			t.Errorf("a draw in the camera's pass took a pipeline not built from the bundled scene shader")
		}
		boxes += draw.Instances
	}
	if boxes != 2 {
		t.Errorf("the camera's pass drew %d box instances, want 2", boxes)
	}
}
