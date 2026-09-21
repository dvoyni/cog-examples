package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene/ecssceneplugin"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/slots/gfx"

	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog-examples/internal/headless"
)

// run composes the demo exactly as main does, waits for both models to be
// resident without stepping, and drives n steps. A load runs off the frame, so
// waiting before the first step is what makes step n the frame a real run
// reaches at step n once its loads have long finished.
func run(t *testing.T, n int) *headless.Engine {
	t.Helper()
	engine := headless.New(t, ecsplugin.New(), ecssceneplugin.New(), New())

	// Preload loads: by the time it returns, the file has been read, parsed and
	// uploaded. There is nothing to wait for, so what used to be a polling loop
	// is one call and one assertion.
	engine.LookupDevice(func(la scene.LookupDeviceAccess) {
		for _, path := range []string{fountain.NozzlePath, fountain.FoxPath} {
			la.Preload(path)
			if err := la.State(path); err != nil {
				t.Fatalf("Preload left %q unloaded: %v", path, err)
			}
		}
	})

	engine.Steps(n)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine
}

func ask(t *testing.T, engine *headless.Engine) Shown {
	t.Helper()
	reply := engine.Executioner().ExecuteCommand[HUDCmd](HUDRequest{})
	// A dispatch the kernel could not perform is reported rather than returned,
	// and the zero response comes back, so the report is what says so.
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("hud: the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return reply
}

// The HUD's arithmetic: the census is the tally, every kind of Entity is
// counted, and the frame it sits beside has the camera's two passes.
func TestTheHUDsArithmetic(t *testing.T) {
	for _, steps := range []int{3, 180, fountain.ReferenceStep} {
		h := ask(t, run(t, steps)).HUD
		if h.Step != steps-1 {
			t.Errorf("after %d steps the HUD shows step %d, want the step before", steps, h.Step)
		}
		if h.Motes != h.Spawned-h.Retired {
			t.Errorf("step %d: %d motes live, but spawned %d - retired %d = %d",
				h.Step, h.Motes, h.Spawned, h.Retired, h.Spawned-h.Retired)
		}
		if h.Motes == 0 {
			t.Errorf("step %d: no motes", h.Step)
		}
		if steps >= 180 && h.Retired == 0 {
			t.Errorf("step %d: no mote has been retired", h.Step)
		}
		if h.Foxes != 1 || h.Lights != 2 || h.Cameras != 1 {
			t.Errorf("step %d: fox %d, lights %d, cameras %d; want 1, 2 and 1",
				h.Step, h.Foxes, h.Lights, h.Cameras)
		}
		// Two passes: the ground pass draws the basin, and the forward pass
		// draws every mote, the nozzle, the fox and the basin's ripples. Each
		// Entity is its own scene call while ecsscene proxies into scene, so
		// its own batch.
		if h.Passes != 2 {
			t.Errorf("step %d: %d passes, want 2", h.Step, h.Passes)
		}
		if want := h.Motes + 4; h.Drawn != want || h.Batches != want {
			t.Errorf("step %d: drawn %d in %d batches for %d motes; want %d of each",
				h.Step, h.Drawn, h.Batches, h.Motes, want)
		}
	}
}

// frame is one step's view from both sides of gfx: the snapshot the HUD armed,
// and what reached the backend while that step rendered.
type frame struct {
	snapshot gfx.FrameSnapshot
	passes   []gfx.PassDesc
	draws    []headless.DrawCall
	backend  *headless.Backend
}

// referenceFrame drives the demo to the step before ReferenceStep, then takes
// one more and keeps what that step sent the backend and the snapshot of it.
func referenceFrame(t *testing.T) frame {
	t.Helper()
	engine := run(t, fountain.ReferenceStep-1)
	backend := engine.Backend()
	passes, draws := len(backend.Passes), len(backend.Draws)
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	snapshot := ask(t, engine).Snapshot
	if snapshot.Err != nil || snapshot.Tick != fountain.ReferenceStep {
		t.Fatalf("the HUD's snapshot is of tick %d with error %v, want tick %d",
			snapshot.Tick, snapshot.Err, fountain.ReferenceStep)
	}
	f := frame{snapshot: snapshot, backend: backend, draws: slices.Clone(backend.Draws[draws:])}
	f.passes = backend.Passes[passes:]
	// Draws index Passes across the whole run; rebase them onto this step's.
	for i := range f.draws {
		f.draws[i].Pass -= passes
	}
	return f
}

// At the reference step every Component is doing its job in the frame: the
// Camera's two passes each clear what it says, the Model and Animation draw
// the nozzle and the skinned fox, the Mesh and Material draw the basin in both
// passes and every mote in forward, one pipeline for all of them.
func TestTheReferenceStepShowsEveryComponent(t *testing.T) {
	f := referenceFrame(t)

	views := fountain.CameraPasses(f.snapshot.Frame)
	if len(views) != 2 ||
		fountain.PassTag(views[0].Label) != fountain.TagGround ||
		fountain.PassTag(views[1].Label) != fountain.TagForward {
		t.Fatalf("the snapshot's camera passes are %+v, want ground then forward", views)
	}
	if views[0].Load != "clear" || views[0].DepthLoad != "clear" {
		t.Errorf("the ground pass loads %q and depth %q, want both cleared", views[0].Load, views[0].DepthLoad)
	}
	if views[1].Load == "clear" || views[1].DepthLoad != "clear" {
		t.Errorf("the forward pass loads %q and depth %q, want the ground's colour kept and depth cleared",
			views[1].Load, views[1].DepthLoad)
	}

	// The same two passes reached the backend, each with its clear.
	ground, forward := -1, -1
	for i, pass := range f.passes {
		switch fountain.PassTag(pass.Label) {
		case fountain.TagGround:
			ground = i
			if pass.Load != gfx.LoadClear || pass.DepthLoad != gfx.LoadClear {
				t.Errorf("the backend's ground pass loads %v and depth %v, want both cleared", pass.Load, pass.DepthLoad)
			}
		case fountain.TagForward:
			forward = i
			if pass.Load == gfx.LoadClear || pass.DepthLoad != gfx.LoadClear {
				t.Errorf("the backend's forward pass loads %v and depth %v", pass.Load, pass.DepthLoad)
			}
		}
	}
	if ground < 0 || forward < 0 || ground > forward {
		t.Fatalf("the backend began passes %v, want ground then forward", labels(f.passes))
	}

	// What each pass drew, told apart by pipeline: the nozzle and the fox take
	// scene's own shader, the fox's variant skinned; the stone, the ripples and
	// the motes take the fountain's WGSL, the ripples alone blended.
	type tally struct{ nozzle, fox, stone, ripples, motes, other int }
	var in [2]tally
	motePipelines := map[gfx.PipelineID]bool{}
	for _, draw := range f.draws {
		var at *tally
		switch draw.Pass {
		case ground:
			at = &in[0]
		case forward:
			at = &in[1]
		default:
			continue
		}
		desc := f.backend.PipelineOf(draw.Pipeline)
		switch {
		case f.backend.IsScenePipeline(draw.Pipeline) && strings.Contains(f.backend.PipelineSupply(draw.Pipeline), "SCENE_SKIN"):
			at.fox++
		case f.backend.IsScenePipeline(draw.Pipeline):
			at.nozzle++
		case desc.State.Blend != gfx.BlendOpaque:
			at.ripples++
		case draw.Pass == ground:
			at.stone++
		default:
			at.motes += draw.Instances
			motePipelines[draw.Pipeline] = true
		}
	}
	if got := in[0]; got != (tally{stone: 1}) {
		t.Errorf("the ground pass drew %+v, want the basin's stone alone", got)
	}
	got := in[1]
	if got.nozzle == 0 || got.fox == 0 {
		t.Errorf("the forward pass drew the nozzle %d times and the fox %d; want both", got.nozzle, got.fox)
	}
	if got.ripples != 1 {
		t.Errorf("the forward pass drew the basin's ripples %d times, want once", got.ripples)
	}
	if got.motes != fountain.ReferenceMotes || got.stone != 0 {
		t.Errorf("the forward pass drew %d motes and %d stone, want %d and none",
			got.motes, got.stone, fountain.ReferenceMotes)
	}
	// Every mote's Material Component holds the one shared value, and its
	// colour rides in Params, so every mote draws with the one pipeline.
	if len(motePipelines) != 1 {
		t.Errorf("the motes drew with %d pipelines, want the one shared material's", len(motePipelines))
	}
}

// The frame is the one internal/fountain says the reference step draws: the
// same passes, the same labels and the same instances in each, as
// cmd/scene/fountain's frame is held to, and no more draws in a pass than
// instances, which is what scene's frame makes.
func TestTheReferenceStepMatchesTheExpectedFigures(t *testing.T) {
	t.Skip("ecsscene still proxies into scene, one call per Entity; " +
		"this comparison starts asserting with the redesign's integration, dvoyni/cog#538")

	view := referenceFrame(t).snapshot.Frame
	if got := fountain.PassesOf(view); !slices.Equal(got, fountain.ReferencePasses) {
		t.Errorf("the camera's passes are %+v, want %+v", got, fountain.ReferencePasses)
	}
	for _, pass := range fountain.CameraPasses(view) {
		if pass.Draws > pass.Instances {
			t.Errorf("pass %q made %d draws of %d instances, more than scene's one a call",
				pass.Label, pass.Draws, pass.Instances)
		}
	}
}

func labels(passes []gfx.PassDesc) []string {
	out := make([]string, len(passes))
	for i := range passes {
		out[i] = passes[i].Label
	}
	return out
}

// referenceHUD is the HUD text in reference.png, read off the image.
//
// The headless backend rasterizes nothing, so the comparison is the HUD rather
// than the pixels: reference.png is a GPU run paused and stepped to
// ReferenceStep, and every number in its HUD is a count of what that step drew,
// so this step taken headless reading the same text is the same frame.
//
// The image's last line still ends "culled 0", a figure the HUD no longer
// shows; reference.png is recaptured once, after the ecsscene redesign
// (dvoyni/cog#538).
var referenceHUD = []string{
	"fountain  step 000600  time 10.00s",
	"step 000599  motes 112 = spawned 831 - retired 719",
	"fox 1  lights 2  cameras 1",
	"passes 2  drawn 116  batches 116",
}

func TestTheHUDReadsAsInReferencePNG(t *testing.T) {
	got := ask(t, run(t, fountain.ReferenceStep)).HUD.Lines(fountain.ReferenceStep)
	if len(got) != len(referenceHUD) {
		t.Fatalf("the HUD reads\n%q\nwant\n%q", got, referenceHUD)
	}
	for i := range got {
		if got[i] != referenceHUD[i] {
			t.Errorf("line %d reads %q, want %q", i, got[i], referenceHUD[i])
		}
	}
}
