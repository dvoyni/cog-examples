package assets_test

import (
	"math"
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/scene"
)

// The vendored files the animation bake is judged against.
//
// Fox is the skinned asset: a real 24-joint rig with three clips, an inverse
// bind per joint and every vertex weighted across four influences, which is the
// shape no document built in memory in cog/scene reproduces. CesiumMilkTruck is
// the degenerate-joint asset: its wheels are rigid node animation, which is
// ordinary glTF and the case the "any node a clip steers becomes a joint" rule
// exists for.
const foxAsset = "assets/Fox/Fox.glb"

// clipsOf reads one resident model's clip table.
func clipsOf(t *testing.T, e *headless.Engine, path string) []scene.ClipInfo {
	t.Helper()
	var clips []scene.ClipInfo
	var ok bool
	e.Lookup(func(la scene.LookupAccess) { clips, ok = la.Clips(path, nil) })
	if !ok {
		t.Fatalf("%s is not resident, so it has no clips to report", path)
	}
	return clips
}

// jointsOf reads one resident model's joint names.
func jointsOf(t *testing.T, e *headless.Engine, path string) []string {
	t.Helper()
	var joints []string
	var ok bool
	e.Lookup(func(la scene.LookupAccess) { joints, ok = la.Joints(path, nil) })
	if !ok {
		t.Fatalf("%s is not resident, so it has no joints to report", path)
	}
	return joints
}

// Fox is the asset the whole skinning path is really judged by: 24 joints, one
// inverse bind each, three clips, and every vertex weighted across four
// influences. The clip names are the file's own, which is what a caller has to
// address a play with.
func TestFoxDeclaresItsThreeClipsAndItsRig(t *testing.T) {
	e := drawing(t, foxAsset, scene.ModelDraw{})
	clips := clipsOf(t, e, foxAsset)
	names := map[string]float32{}
	for _, clip := range clips {
		names[clip.Name] = clip.Duration
	}
	for _, want := range []string{"Survey", "Walk", "Run"} {
		duration, ok := names[want]
		if !ok {
			t.Errorf("Fox declares no clip %q; it has %v", want, clips)
			continue
		}
		// Every Fox clip is a real animation with a real length. A duration of
		// zero would mean the sampler inputs never reached the bake, which is
		// the failure that still produces a model and never moves it.
		if duration <= 0 {
			t.Errorf("clip %q lasts %v seconds, want a real length", want, duration)
		}
	}
	if len(clips) != 3 {
		t.Errorf("Fox declares %d clips, want its three", len(clips))
	}
	joints := jointsOf(t, e, foxAsset)
	if len(joints) != 24 {
		t.Errorf("Fox has %d joints, want its 24-bone rig", len(joints))
	}
	// The names are the file's node names, which is what makes the query worth
	// having: an index would say nothing a caller could act on.
	named := 0
	for _, joint := range joints {
		if joint != "" {
			named++
		}
	}
	if named != len(joints) {
		t.Errorf("%d of %d joints are unnamed, want the file's own names", len(joints)-named, named)
	}
}

// Pose memory is joints x frames x 48 bytes, and it is the number that should
// surprise nobody after the fact: the 60 Hz grid is the trade the design makes,
// so the size it produces is worth pinning to the real rig rather than to a
// document built to be small.
func TestFoxBakesTheExpectedPoseMemory(t *testing.T) {
	e := drawing(t, foxAsset, scene.ModelDraw{})
	var bytes int
	e.Lookup(func(la scene.LookupAccess) { bytes, _ = la.PoseBytes(foxAsset) })
	// 24 joints x 48 bytes is a row; the rest frame plus every clip's frames
	// are the rows. A three-clip rig lands in the hundreds of kilobytes, which
	// is the figure the spec quotes.
	if bytes%(24*48) != 0 {
		t.Errorf("PoseBytes = %d, which is not a whole number of 24-joint rows", bytes)
	}
	if bytes < 100*1024 || bytes > 2*1024*1024 {
		t.Errorf("PoseBytes = %d; a 24-joint three-clip rig at 60 Hz should be a few hundred KiB", bytes)
	}
	var total int
	e.Lookup(func(la scene.LookupAccess) { total = la.TotalPoseBytes() })
	if total != bytes {
		t.Errorf("TotalPoseBytes = %d with one model resident, want its %d", total, bytes)
	}
}

// A skinned draw is never culled, so a playing Fox reaches the pass whatever
// the frustum thinks of its bind-pose sphere.
func TestFoxDrawsWithAClipPlaying(t *testing.T) {
	e := drawing(t, foxAsset, scene.ModelDraw{
		Plays: []scene.ClipPlay{{Clip: "Walk", Time: 0.4, Loop: true, Weight: 1}},
	})
	passes := e.Passes()
	if len(passes) != 1 {
		t.Fatalf("passes = %d, want the camera's one", len(passes))
	}
	if passes[0].Instances == 0 {
		t.Fatal("a playing Fox drew nothing")
	}
	if passes[0].Culled != 0 {
		t.Errorf("culled %d draws, want none: a skinned draw is exempt", passes[0].Culled)
	}
	if errs := e.Errors(); len(errs) != 0 {
		t.Errorf("a clean file with a real clip reported %v", errs)
	}
}

// Blending two of the file's own clips is the case the four-play cap and the
// weight normalisation exist for, and it must reach the pass as one draw per
// primitive rather than one per play.
func TestFoxBlendsTwoClipsAsOneDraw(t *testing.T) {
	single := drawing(t, foxAsset, scene.ModelDraw{
		Plays: []scene.ClipPlay{{Clip: "Walk", Time: 0.4, Loop: true, Weight: 1}},
	})
	blended := drawing(t, foxAsset, scene.ModelDraw{
		Plays: []scene.ClipPlay{
			{Clip: "Walk", Time: 0.4, Loop: true, Weight: 0.5},
			{Clip: "Run", Time: 0.2, Loop: true, Weight: 0.5},
		},
	})
	if got, want := batches(t, blended), batches(t, single); got != want {
		t.Errorf("a two-clip blend drew %d batches and one clip drew %d; a blend is one draw", got, want)
	}
	if errs := blended.Errors(); len(errs) != 0 {
		t.Errorf("blending two of the file's own clips reported %v", errs)
	}
}

// A clip name the file does not carry is reported and the play dropped, and the
// model still draws - at its rest pose, which is a real pose because row 0 is
// the authored hierarchy resolved once.
func TestFoxReportsAClipNameItDoesNotCarry(t *testing.T) {
	e := drawing(t, foxAsset, scene.ModelDraw{
		Plays: []scene.ClipPlay{{Clip: "Gallop", Weight: 1}},
	})
	missing := 0
	for _, err := range e.Errors() {
		if _, ok := err.(scene.ErrModelClipMissing); ok {
			missing++
		}
	}
	if missing != 1 {
		t.Errorf("reported %d missing clips over the run, want one: %v", missing, e.Errors())
	}
	if batches(t, e) == 0 {
		t.Error("the Fox vanished; a typo'd clip costs the play, not the model")
	}
}

// The truck's wheels are rigid node animation, which is ordinary glTF - so the
// rule that a node a clip steers becomes a degenerate single-joint skin is what
// keeps them turning without a second animation mechanism.
func TestTheMilkTruckWheelsAreDegenerateJoints(t *testing.T) {
	e := drawing(t, truckAsset, scene.ModelDraw{})
	joints := jointsOf(t, e, truckAsset)
	if len(joints) == 0 {
		t.Fatal("the truck has no joints; its wheel animation would be frozen")
	}
	clips := clipsOf(t, e, truckAsset)
	if len(clips) == 0 {
		t.Fatal("the truck declares no clips")
	}
	// Its whole file still flattens to the same five primitives: a degenerate
	// joint changes where a primitive's transform lives, not how many there
	// are.
	if got := batches(t, e); got != truckPrimitives {
		t.Errorf("the truck drew %d batches, want its %d primitives", got, truckPrimitives)
	}
	// One wheel out of the file still selects one primitive, and re-rooting it
	// against a node whose transform now lives in the pose buffer still works.
	wheel := drawing(t, truckAsset, scene.ModelDraw{
		Node:  "Wheels",
		Plays: []scene.ClipPlay{{Clip: clips[0].Name, Time: 0.5, Loop: true, Weight: 1}},
	})
	if got := batches(t, wheel); got != 1 {
		t.Errorf("the Wheels node drew %d batches while playing, want the one wheel", got)
	}
	if errs := wheel.Errors(); len(errs) != 0 {
		t.Errorf("re-rooting an animated node reported %v", errs)
	}
}

// A model with no animation at all bakes no poses, which is what keeps a static
// prop off the per-vertex pose path entirely.
func TestAStaticVendoredModelBakesNoPoses(t *testing.T) {
	const bottle = "assets/WaterBottle/WaterBottle.glb"
	e := drawing(t, bottle, scene.ModelDraw{})
	var bytes int
	var clips []scene.ClipInfo
	e.Lookup(func(la scene.LookupAccess) {
		bytes, _ = la.PoseBytes(bottle)
		clips, _ = la.Clips(bottle, nil)
	})
	if bytes != 0 {
		t.Errorf("PoseBytes = %d for a file with no animation, want none", bytes)
	}
	if len(clips) != 0 {
		t.Errorf("clips = %v, want none", clips)
	}
}

// Loop is on the play rather than on the caller's time, so a time past the
// clip's end wraps rather than sticking - which is what makes gameplay's
// monotonically increasing clock the only clock in the system.
func TestALoopedPlayPastTheEndStillDraws(t *testing.T) {
	clips := clipsOf(t, drawing(t, foxAsset, scene.ModelDraw{}), foxAsset)
	var walk float32
	for _, clip := range clips {
		if clip.Name == "Walk" {
			walk = clip.Duration
		}
	}
	if walk <= 0 {
		t.Fatal("Fox's Walk clip has no duration to loop over")
	}
	for _, time := range []float32{walk * 37, -walk * 4.5, float32(math.Nextafter(float64(walk), 0))} {
		e := drawing(t, foxAsset, scene.ModelDraw{
			Plays: []scene.ClipPlay{{Clip: "Walk", Time: time, Loop: true, Weight: 1}},
		})
		if batches(t, e) == 0 {
			t.Errorf("a looped play at t=%v drew nothing", time)
		}
		if errs := e.Errors(); len(errs) != 0 {
			t.Errorf("a looped play at t=%v reported %v", time, errs)
		}
	}
}
