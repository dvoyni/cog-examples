package main

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/scene"
)

// referenceStep is the step the assertions below are written against: two
// seconds of demo time, inside the first beacon generation and the first ridge
// resolution, so a run to here is a steady frame with no swap in it. Demo time
// is accumulated fixed steps, so this is the same frame on every machine.
const referenceStep = 120

// demoShaderLayout is what material.go's WGSL declares, stated here rather than
// reflected: the headless backend has no shader front end, and the point of the
// assertion below is that this list is shorter than scene's own - a caller
// material may declare fewer bindings than scene binds, and must never declare
// more.
var demoShaderLayout = gfx.ShaderLayout{Resources: []gfx.ShaderResource{
	{Name: "sceneFrame", StorageBuffer: true, Group: 0, Binding: 0},
	{Name: "sceneInstances", StorageBuffer: true, Group: 0, Binding: 1},
}}

// start brings up an engine with the demo in it, at step zero.
func start(t *testing.T) (*Procedural, *headless.Engine) {
	t.Helper()
	demo := New()
	engine := headless.New(t, demo)
	engine.Backend().TextShaderLayout = demoShaderLayout
	return demo, engine
}

// run drives the demo n steps and fails if the engine reported anything. Every
// test but the beacon swap's uses it: a demo that reports in a steady frame has
// a bug, and the swap's one report is the only one this demo means to provoke.
func run(t *testing.T, n int) (*Procedural, *headless.Engine) {
	t.Helper()
	demo, engine := start(t)
	engine.Steps(n)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return demo, engine
}

// A steady frame is five draws and one camera, and the stray beacon is the one
// the frustum throws away. Recorded counts what the cull mask selected, so the
// stray is in it; Instances counts what survived, so it is not.
func TestTheSteadyFrameRecordsFiveDrawsAndCullsTheStray(t *testing.T) {
	demo, engine := run(t, referenceStep)
	if ops := engine.Ops(); len(ops) != SteadyOps {
		t.Fatalf("recorded %d ops, want %d", len(ops), SteadyOps)
	}
	if demo.submitted != SteadyDraws {
		t.Errorf("the demo submitted %d draws, want %d", demo.submitted, SteadyDraws)
	}
	passes := engine.Passes()
	if len(passes) != 1 {
		t.Fatalf("published %d passes, want the one implicit forward pass", len(passes))
	}
	pass := passes[0]
	if pass.CameraID != CameraMain {
		t.Errorf("pass camera %d, want %d", pass.CameraID, CameraMain)
	}
	if pass.Recorded != SteadyDraws {
		t.Errorf("the pass recorded %d draws, want %d", pass.Recorded, SteadyDraws)
	}
	if pass.Culled != CulledDraws {
		t.Errorf("the pass culled %d draws, want %d - the stray beacon", pass.Culled, CulledDraws)
	}
	if want := pass.Recorded - pass.Culled; pass.Instances != want {
		t.Errorf("the pass packed %d instances from %d survivors", pass.Instances, want)
	}
}

// The cull is a claim about a named object, not a count: the published frustum
// rejects the stray beacon's sphere and accepts the beacon's own. Both are the
// same mesh with the same declared bounds, so the only thing separating them is
// where they stand.
func TestThePublishedFrustumSeparatesTheBeaconFromTheStray(t *testing.T) {
	_, engine := run(t, referenceStep)
	frustum := engine.Passes()[0].Frustum
	if !frustum.ContainsSphere(beaconPosition, beaconScale) {
		t.Error("the published frustum rejects the beacon, which is on screen")
	}
	if frustum.ContainsSphere(strayPosition, beaconScale) {
		t.Errorf("the published frustum contains the stray at %v, which sits behind the camera",
			strayPosition)
	}
}

// A custom vertex layout gets no baked bounding sphere - scene cannot find
// POSITION in bytes it has never seen a layout for - so each draw has to say
// how it is culled. The ridge and the beacon declare a sphere; the ribbon says
// never-cull outright. A draw that said neither would be silently uncullable,
// which is the failure this pins.
func TestEveryCustomLayoutDrawSaysHowItIsCulled(t *testing.T) {
	_, engine := run(t, referenceStep)
	meshes := 0
	for _, op := range engine.Ops() {
		if op.Kind != scene.OpMesh {
			continue
		}
		meshes++
		if op.Draw.Material == nil {
			t.Errorf("a mesh draw named no material; a custom layout with the bundled "+
				"PBR is reported and skipped (mesh %d)", op.Mesh.ID())
		}
		if !op.Draw.NeverCull && op.Draw.Bounds.W == 0 {
			t.Errorf("mesh %d declares neither Bounds nor NeverCull, so it is never culled "+
				"and nobody said so", op.Mesh.ID())
		}
	}
	if want := SteadyDraws - BundledDraws; meshes != want {
		t.Errorf("recorded %d mesh draws, want %d - every draw but the two bundled ones",
			meshes, want)
	}
}

// The ribbon is the never-cull case and the ridge the explicit-sphere one, and
// the demo means each to be the one it is: swapping them would still pass the
// test above.
func TestTheRibbonNeverCullsAndTheRidgeDeclaresItsSphere(t *testing.T) {
	demo, engine := run(t, referenceStep)
	found := 0
	for _, op := range engine.Ops() {
		switch {
		case op.Kind != scene.OpMesh:
		case op.Mesh == demo.ribbon:
			found++
			if !op.Draw.NeverCull {
				t.Error("the ribbon does not say NeverCull, and a temporary mesh has no sphere of its own")
			}
		case op.Mesh == demo.ridge:
			found++
			if op.Draw.NeverCull || op.Draw.Bounds.W != ridgeBounds {
				t.Errorf("the ridge declares NeverCull %v and radius %v, want a radius of %v",
					op.Draw.NeverCull, op.Draw.Bounds.W, ridgeBounds)
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d of the ridge and ribbon draws, want both", found)
	}
}

// A temporary mesh's id and a durable one's can never collide: the two sources
// allocate independent dense ranges, and the temporary range carries a bit the
// durable one does not. The sort key is one uint32 of mesh id, so a collision
// would batch two different meshes as though they were the same geometry.
func TestTheDurableAndTemporaryMeshIDsCannotCollide(t *testing.T) {
	demo, _ := run(t, referenceStep)
	const temporaryBit = uint32(1) << 31
	if id := demo.ribbon.ID(); id&temporaryBit == 0 {
		t.Errorf("the ribbon's id is %#x, which carries no temporary bit", id)
	}
	for name, ref := range map[string]scene.MeshRef{"ridge": demo.ridge, "beacon": demo.beacon} {
		if id := ref.ID(); id == 0 || id&temporaryBit != 0 {
			t.Errorf("the %s's id is %#x, which is not a durable id", name, id)
		}
	}
}

// UpdateMesh replaces a durable mesh's geometry wholesale at any size while
// keeping its ref and its id. The ridge changes resolution mid-run, and it is
// still the same mesh afterwards: there is no capacity concept, so growth is a
// re-bake rather than a new mesh.
func TestUpdateMeshResizesTheRidgeWithoutChangingItsRef(t *testing.T) {
	demo, engine := start(t)
	engine.Steps(ridgeResizePeriod - 10)
	before, beforeCells := demo.ridge, demo.ridgeCellCount
	engine.Steps(20)
	if demo.ridge != before {
		t.Errorf("the ridge's ref changed across a resize: %+v against %+v", demo.ridge, before)
	}
	if demo.ridgeCellCount == beforeCells {
		t.Fatalf("the ridge is still %d cells across after the resize step", beforeCells)
	}
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the resize reported %d errors, first: %v", len(errs), errs[0])
	}
}

// A release makes the old ref stale at once. The demo draws that ref once more
// on the frame it released it: the draw is reported unavailable and skipped, so
// the pass packs one fewer instance than it kept, and the mesh that now holds
// that slot is not drawn in its place.
func TestABeaconSwapSkipsADrawOfTheReleasedRef(t *testing.T) {
	demo, engine := start(t)
	engine.Steps(beaconPeriod - 1)
	before := demo.beacon
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the steady frames reported %d errors, first: %v", len(errs), errs[0])
	}

	engine.Steps(1)
	if demo.generation != 1 || demo.staleDraws != 1 {
		t.Fatalf("after the swap the demo is at generation %d with %d stale draws, want 1 and 1",
			demo.generation, demo.staleDraws)
	}
	if demo.beacon == before {
		t.Error("the beacon's ref survived a release and a re-bake")
	}
	errs := engine.Errors()
	if len(errs) != 1 {
		t.Fatalf("the swap reported %d errors, want exactly one: %v", len(errs), errs)
	}
	var unavailable scene.ErrMeshUnavailable
	if !errors.As(errs[0], &unavailable) {
		t.Fatalf("the swap reported %v, want a scene.ErrMeshUnavailable", errs[0])
	}
	if unavailable.Mesh != before.ID() {
		t.Errorf("the report names mesh %d, want the released %d", unavailable.Mesh, before.ID())
	}

	pass := engine.Passes()[0]
	if pass.Recorded != SteadyDraws+1 {
		t.Fatalf("the swap frame recorded %d draws, want %d", pass.Recorded, SteadyDraws+1)
	}
	if want := pass.Recorded - pass.Culled - 1; pass.Instances != want {
		t.Errorf("the swap frame packed %d instances, want %d - one survivor drew nothing",
			pass.Instances, want)
	}
}

// The demo's material declares two of the three parameters scene binds on every
// draw, and that is the constraint the whole demo exists to prove livable: gfx
// binds what reflection reports and ignores the rest, so a parameter a shader
// does not declare costs nothing, while a binding it declares and nobody binds
// takes the frame's whole command buffer with it.
//
// Every draw in the frame binds sceneFrame and sceneInstances. Only the ground
// plane, which takes the bundled PBR, binds scenePbrMaterial as well.
func TestTheCustomMaterialBindsOnlyWhatItDeclares(t *testing.T) {
	_, engine := run(t, referenceStep)
	backend := engine.Backend()
	counts := map[headless.BufferBinding]int{}
	for _, binding := range backend.Buffers {
		counts[headless.BufferBinding{Group: binding.Group, Binding: binding.Binding}]++
	}
	frames := counts[headless.BufferBinding{Group: 0, Binding: 0}]
	instances := counts[headless.BufferBinding{Group: 0, Binding: 1}]
	material := counts[headless.BufferBinding{Group: 1, Binding: 0}]
	// The counts are of scene's draws only. The backend's own Draws list is
	// longer, because canvas's HUD is in it too and binds nothing scene named.
	if frames == 0 {
		t.Fatal("no draw bound sceneFrame at all")
	}
	if frames != instances {
		t.Errorf("%d draws bound sceneFrame and %d bound sceneInstances; every scene draw "+
			"binds both", frames, instances)
	}
	if material == 0 {
		t.Fatal("no draw bound scenePbrMaterial; the two bundled draws should")
	}
	// Every frame packs BundledDraws bundled draws and CustomDraws custom ones,
	// so the two counts hold that ratio however many frames were driven.
	if custom := frames - material; custom*BundledDraws != material*CustomDraws {
		t.Errorf("%d draws bound scenePbrMaterial and %d did not; want %d custom-material "+
			"draws for every %d bundled ones", material, custom, CustomDraws, BundledDraws)
	}
}

// Scene binds a pass's whole instance slice as one range of one arena, and the
// per-instance record is 64 bytes - the record that carries the world rows, the
// animation offset and the flags every buffer-built draw sets SCENE_NOSKIN in.
// A range that stopped agreeing with the pass's instance count would be a draw
// reading somebody else's matrix.
func TestTheInstanceRangeIsOneRecordPerPackedInstance(t *testing.T) {
	_, engine := run(t, referenceStep)
	const instanceSize = 64
	want := engine.Passes()[0].Instances * instanceSize
	seen := 0
	for _, binding := range engine.Backend().Buffers {
		if binding.Group != 0 || binding.Binding != 1 {
			continue
		}
		seen++
		if binding.Size != want {
			t.Fatalf("an instance range is %d bytes, want %d - %d instances of %d",
				binding.Size, want, want/instanceSize, instanceSize)
		}
	}
	if seen == 0 {
		t.Error("no instance range was bound")
	}
}

// Demo time is accumulated fixed steps, never wall clock, so the same step
// number is the same frame: two engines driven the same number of steps decide
// identically, down to the batch list. Geometry rebuilt every frame does not
// change that - it is rebuilt from the step counter.
func TestTheSameStepIsTheSameFrame(t *testing.T) {
	_, firstEngine := run(t, referenceStep)
	_, secondEngine := run(t, referenceStep)
	first, second := firstEngine.Passes(), secondEngine.Passes()
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

// The frame reaches the GPU: every packed instance becomes a draw, and the
// frame is presented. This is the only assertion that looks past what scene
// decided, and it is here so a frame that decides correctly and then never
// renders is not a passing test.
func TestThePackedInstancesReachTheBackend(t *testing.T) {
	_, engine := run(t, referenceStep)
	backend := engine.Backend()
	if backend.Presents == 0 {
		t.Error("the backend was never asked to present")
	}
	if backend.Bakes == 0 {
		t.Error("the backend baked no buffers, so no geometry was ever uploaded")
	}
	drawn := 0
	for _, call := range backend.Draws {
		drawn += call.Instances
	}
	if pass := engine.Passes()[0]; drawn < pass.Instances {
		t.Errorf("the backend drew %d instances, the pass packed %d", drawn, pass.Instances)
	}
}
