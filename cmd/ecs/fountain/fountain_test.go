package main

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene/ecssceneplugin"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/slots/storage"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
)

// run composes the demo exactly as main does, waits for both models to be
// resident without stepping, and drives n steps. A load runs off the frame, so
// waiting before the first step is what makes step n the frame a real run
// reaches at step n once its loads have long finished.
func run(t *testing.T, n int) *headless.Engine {
	t.Helper()
	assetConfig, err := assets.Config(storage.Config{})
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	engine := headless.NewOver(t, assetConfig, ecsplugin.New(), ecssceneplugin.New(), New())

	deadline := time.Now().Add(60 * time.Second)
	for {
		resident := 0
		engine.Lookup(func(la scene.LookupAccess) {
			for _, path := range []string{nozzlePath, foxPath} {
				la.Preload(path)
				if la.State(path) == scene.ModelResident {
					resident++
				}
			}
		})
		if resident == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the models never became resident; engine reported %v", engine.Errors())
		}
		time.Sleep(5 * time.Millisecond)
	}

	engine.Steps(n)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine
}

func ask(t *testing.T, engine *headless.Engine) HUD {
	t.Helper()
	reply, err := engine.Executioner().ExecuteCommand[HUDCmd](HUDRequest{})
	if err != nil {
		t.Fatalf("hud: %v", err)
	}
	return reply
}

// The HUD's arithmetic: the census is the tally, every kind of Entity is
// counted, and every draw it counts is one of those Entities.
func TestTheHUDsArithmetic(t *testing.T) {
	for _, steps := range []int{2, 180, referenceStep} {
		h := ask(t, run(t, steps))
		if h.Step != steps-1 {
			t.Errorf("after %d steps the HUD shows step %d, want the step scene last flushed", steps, h.Step)
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
		// is its own call, so its own batch.
		if h.Passes != 2 || h.Culled != 0 {
			t.Errorf("step %d: %d passes culling %d; want 2 culling nothing", h.Step, h.Passes, h.Culled)
		}
		if want := h.Motes + 4; h.Drawn != want || h.Batches != want {
			t.Errorf("step %d: drawn %d in %d batches for %d motes; want %d of each",
				h.Step, h.Drawn, h.Batches, h.Motes, want)
		}
	}
}

// At the reference step every Component is doing its job in the frame scene
// recorded.
func TestTheReferenceStepShowsEveryComponent(t *testing.T) {
	engine := run(t, referenceStep)
	ops := engine.Ops()

	var (
		motes, basins, points, spots int
		cubes                        = map[scene.MeshRef]int{}
		models                       = map[string]scene.Op{}
		cameras                      []scene.Op
	)
	for _, op := range ops {
		switch op.Kind {
		case scene.OpMesh:
			switch len(op.Draw.Material) {
			case 1:
				motes++
				cubes[op.Mesh]++
				if len(op.Draw.Params) != 1 || op.Draw.Params[0].Name() != moteTintParam {
					t.Fatalf("a mote draws with params %v, want its one tint", op.Draw.Params)
				}
				if scale := op.Draw.Transform.Scale; scale.Y <= scale.X || scale.X != scale.Z {
					t.Fatalf("a mote is scaled %v, want it stretched along its local Y", scale)
				}
			case 2:
				basins++
				if op.Draw.Material[0].Tag != tagGround || op.Draw.Material[1].Tag != scene.TagForward {
					t.Errorf("the basin's material serves %q and %q, want %q and %q",
						op.Draw.Material[0].Tag, op.Draw.Material[1].Tag, tagGround, scene.TagForward)
				}
			default:
				t.Errorf("a mesh draws with %d material tags", len(op.Draw.Material))
			}
		case scene.OpModel:
			models[op.Path] = op
		case scene.OpPointLight:
			points++
		case scene.OpSpotLight:
			spots++
		case scene.OpCamera:
			cameras = append(cameras, op)
		}
	}

	h := ask(t, engine)
	if motes < 50 || basins != 1 || points != 1 || spots != 1 || len(models) != 2 || len(cameras) != 1 {
		t.Fatalf("recorded %d motes, %d basins, %d point and %d spot lights, models %v and %d cameras",
			motes, basins, points, spots, models, len(cameras))
	}
	if len(cubes) != 1 {
		t.Errorf("the motes draw %d distinct meshes, want the one cube baked at startup", len(cubes))
	}
	if _, ok := models[nozzlePath]; !ok {
		t.Errorf("no draw of the nozzle %q", nozzlePath)
	}
	plays := models[foxPath].Model.Plays
	if len(plays) != 2 || plays[0].Clip != foxWalk || plays[1].Clip != foxRun || plays[0].Time <= 0 {
		t.Errorf("the fox plays %+v, want Walk and Run with time advanced", plays)
	}
	passes := cameras[0].Descr.Passes
	if len(passes) != 2 {
		t.Fatalf("the camera declares %d passes, want 2", len(passes))
	}
	if _, clears := passes[0].ClearColor.Get(); !clears {
		t.Error("the camera's first pass does not clear colour")
	}

	views := engine.Passes()
	if len(views) != 2 || views[0].Tag != tagGround || views[1].Tag != scene.TagForward {
		t.Fatalf("published passes %+v, want the ground pass then forward", views)
	}
	if views[0].Instances != 1 || views[1].Instances != motes+3 {
		t.Errorf("the ground pass packed %d and forward %d, want 1 and %d",
			views[0].Instances, views[1].Instances, motes+3)
	}
	for _, view := range views {
		if view.Lights != 2 || view.Culled != 0 {
			t.Errorf("pass %q packed %d lights and culled %d, want 2 and none", view.Tag, view.Lights, view.Culled)
		}
	}
	if h.Step != referenceStep-1 {
		t.Errorf("the HUD shows step %d", h.Step)
	}
}

// referenceHUD is the HUD text in reference.png, read off the image.
//
// The headless backend rasterizes nothing, so the comparison is the HUD rather
// than the pixels: reference.png is a GPU run paused and stepped to
// referenceStep, and every number in its HUD is a count of what that step drew,
// so this step taken headless reading the same text is the same frame.
var referenceHUD = []string{
	"fountain  step 000600  time 10.00s",
	"step 000599  motes 112 = spawned 831 - retired 719",
	"fox 1  lights 2  cameras 1",
	"passes 2  drawn 116  batches 116  culled 0",
}

func TestTheHUDReadsAsInReferencePNG(t *testing.T) {
	got := ask(t, run(t, referenceStep)).lines(referenceStep)
	if len(got) != len(referenceHUD) {
		t.Fatalf("the HUD reads\n%q\nwant\n%q", got, referenceHUD)
	}
	for i := range got {
		if got[i] != referenceHUD[i] {
			t.Errorf("line %d reads %q, want %q", i, got[i], referenceHUD[i])
		}
	}
}
