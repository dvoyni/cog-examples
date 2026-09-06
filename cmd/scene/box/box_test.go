package main

import (
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// referenceStep is the step the assertions below are written against: two
// seconds of demo time, far enough in that the spin and the sphere's orbit have
// both moved off their starting values. Demo time is accumulated fixed steps,
// so this is the same frame on every machine.
const referenceStep = 120

// run drives the demo n steps and hands back the engine.
func run(t *testing.T, n int) *headless.Engine {
	t.Helper()
	engine := headless.New(t, New())
	engine.Steps(n)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine
}

// A camera with an empty Passes emits exactly one pass: the implicit forward
// pass, at the camera's own id, which is the contract box exists to pin.
func TestAnEmptyPassesEmitsOneForwardPassAtTheCameraID(t *testing.T) {
	passes := run(t, referenceStep).Passes()
	if len(passes) != 1 {
		t.Fatalf("published %d passes, want the one implicit pass", len(passes))
	}
	pass := passes[0]
	if pass.CameraID != CameraMain {
		t.Errorf("pass camera %d, want %d", pass.CameraID, CameraMain)
	}
	if pass.Order != gfx.Order(CameraMain) {
		t.Errorf("pass order %d, want the camera's own id %d", pass.Order, gfx.Order(CameraMain))
	}
	if pass.Tag != scene.TagForward {
		t.Errorf("pass tag %q, want %q", pass.Tag, scene.TagForward)
	}
}

// Ops reports calls and Passes reports draws. The wire box is one call and
// twelve edge draws, each culled on its own, so the two numbers differ by
// eleven and neither is a stand-in for the other.
func TestOpsReportsCallsAndThePassReportsDraws(t *testing.T) {
	engine := run(t, referenceStep)
	ops := engine.Ops()
	if len(ops) != RecordedOps {
		t.Fatalf("recorded %d ops, want %d", len(ops), RecordedOps)
	}
	if ops[0].Kind != scene.OpCamera || ops[0].Camera != CameraMain {
		t.Errorf("the first op is %v for camera %d, want the camera registration",
			ops[0].Kind, ops[0].Camera)
	}
	kinds := map[scene.OpKind]int{}
	for _, op := range ops {
		kinds[op.Kind]++
	}
	want := map[scene.OpKind]int{
		scene.OpCamera: 1, scene.OpPlane: 1, scene.OpBox: 2,
		scene.OpSphere: 1, scene.OpWireBox: 1, scene.OpLine3D: 3,
	}
	for kind, count := range want {
		if kinds[kind] != count {
			t.Errorf("%d ops of kind %v, want %d", kinds[kind], kind, count)
		}
	}
	if pass := engine.Passes()[0]; pass.Recorded != RecordedDraws {
		t.Errorf("the pass recorded %d draws, want %d - one call per shape but "+
			"twelve draws for the wire box", pass.Recorded, RecordedDraws)
	}
}

// At the documented starting pose the whole scene is in front of the camera:
// nothing is culled, and every recorded draw is packed into an instance.
func TestNothingIsCulledAtTheDocumentedPose(t *testing.T) {
	pass := run(t, referenceStep).Passes()[0]
	if pass.Culled != 0 {
		t.Errorf("culled %d draws at the documented pose, want none", pass.Culled)
	}
	if pass.Instances != pass.Recorded-pass.Culled {
		t.Errorf("packed %d instances from %d recorded and %d culled",
			pass.Instances, pass.Recorded, pass.Culled)
	}
	total := 0
	for _, batch := range pass.Batches {
		total += batch.InstanceCount
	}
	if total != pass.Instances {
		t.Errorf("the batches account for %d instances, the pass packed %d", total, pass.Instances)
	}
}

// The pass publishes the frustum that made the culling decision, so a test can
// ask about one shape rather than only count. The orbiting sphere is inside it
// at the documented pose; a point far behind the camera is not.
func TestThePublishedFrustumAnswersForOneShape(t *testing.T) {
	engine := run(t, referenceStep)
	pass := engine.Passes()[0]
	demo := New()
	demo.step = referenceStep
	if !pass.Frustum.ContainsSphere(demo.spinCenter(), sphereRadius) {
		t.Error("the published frustum rejects the orbiting sphere, which is on screen")
	}
	behind := demo.eye().Sub(orbitTarget).MulS(2).Add(orbitTarget)
	if pass.Frustum.ContainsSphere(behind, sphereRadius) {
		t.Errorf("the published frustum contains %v, which is behind the camera at %v",
			behind, demo.eye())
	}
}

// Demo time is accumulated fixed steps, never wall clock, so the same step
// number is the same frame: two engines driven the same number of steps decide
// identically, down to the batch list.
func TestTheSameStepIsTheSameFrame(t *testing.T) {
	first := run(t, referenceStep).Passes()
	second := run(t, referenceStep).Passes()
	if len(first) != len(second) {
		t.Fatalf("two runs of %d steps published %d and %d passes",
			referenceStep, len(first), len(second))
	}
	for i := range first {
		a, b := first[i], second[i]
		if a.Recorded != b.Recorded || a.Culled != b.Culled || a.Instances != b.Instances {
			t.Errorf("pass %d differs between runs: %d/%d/%d against %d/%d/%d",
				i, a.Recorded, a.Culled, a.Instances, b.Recorded, b.Culled, b.Instances)
		}
		if a.Frustum != b.Frustum {
			t.Errorf("pass %d published a different frustum between runs", i)
		}
		if len(a.Batches) != len(b.Batches) {
			t.Errorf("pass %d batched %d against %d", i, len(a.Batches), len(b.Batches))
			continue
		}
		for j := range a.Batches {
			if a.Batches[j] != b.Batches[j] {
				t.Errorf("pass %d batch %d differs: %+v against %+v", i, j, a.Batches[j], b.Batches[j])
			}
		}
	}
}

// A shape of no size records its call and draws nothing. The demo never records
// one, so this asserts the boundary the demo's own numbers rest on: the counts
// above are the shapes' counts, not a coincidence of the vocabulary.
func TestEveryRecordedShapeHasSize(t *testing.T) {
	for _, op := range run(t, referenceStep).Ops() {
		switch op.Kind {
		case scene.OpSphere:
			if op.Radius <= 0 {
				t.Errorf("the sphere has radius %v, so it draws nothing", op.Radius)
			}
		case scene.OpPlane:
			if op.Size.X <= 0 || op.Size.Z <= 0 {
				t.Errorf("the plane is %v across, so it draws nothing", op.Size)
			}
		case scene.OpLine3D:
			if op.Thickness <= 0 || op.Start == op.End {
				t.Errorf("the line from %v to %v is %v thick, so it draws nothing",
					op.Start, op.End, op.Thickness)
			}
		case scene.OpWireBox:
			if op.Thickness <= 0 || op.Size == (m.Vec3{}) {
				t.Errorf("the wire box is %v with thickness %v, so it draws nothing",
					op.Size, op.Thickness)
			}
		}
	}
}

// Every zero value is the default. The camera writes neither CullMask nor the
// two intensities nor Passes, and the resting box writes no Scale at all: what
// the recorder handed scene is exactly what the demo typed, and scene read the
// zeroes as the defaults rather than the demo filling them in.
func TestTheDemoLeavesTheDefaultsUnwritten(t *testing.T) {
	ops := run(t, referenceStep).Ops()
	camera := ops[0]
	if camera.Descr.CullMask != 0 {
		t.Errorf("the camera wrote CullMask %v; the demo means to leave it zero", camera.Descr.CullMask)
	}
	if camera.Descr.SunIntensity != 0 || camera.Descr.AmbientIntensity != 0 {
		t.Errorf("the camera wrote intensities %v and %v; the demo means to leave them zero",
			camera.Descr.SunIntensity, camera.Descr.AmbientIntensity)
	}
	if len(camera.Descr.Passes) != 0 {
		t.Errorf("the camera declared %d passes; the demo means to declare none",
			len(camera.Descr.Passes))
	}
	if camera.Descr.Projection != 0 {
		t.Errorf("the camera wrote Projection %v; the demo means to leave it zero",
			camera.Descr.Projection)
	}
	var resting *scene.Op
	for i := range ops {
		if ops[i].Kind == scene.OpBox && ops[i].Transform.Rotation == (m.Quat{}) {
			resting = &ops[i]
		}
	}
	if resting == nil {
		t.Fatal("no unrotated box was recorded")
	}
	if resting.Transform.Scale != 0 || resting.Transform.Matrix != nil {
		t.Errorf("the resting box wrote Scale %v and Matrix %v; a zero Scale means 1",
			resting.Transform.Scale, resting.Transform.Matrix)
	}
}

// The frame reaches the GPU: every packed instance becomes a draw, and the
// frame is presented once. This is the only assertion that looks past what
// scene decided, and it is here so a frame that decides correctly and then
// never renders is not a passing test.
func TestThePackedInstancesReachTheBackend(t *testing.T) {
	engine := run(t, referenceStep)
	backend := engine.Backend()
	if backend.Presents == 0 {
		t.Error("the backend was never asked to present")
	}
	drawn := 0
	for _, call := range backend.Draws {
		drawn += call.Instances
	}
	if pass := engine.Passes()[0]; drawn < pass.Instances {
		t.Errorf("the backend drew %d instances, the pass packed %d", drawn, pass.Instances)
	}
}
