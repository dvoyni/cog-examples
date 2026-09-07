package main

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// run starts the demo headless over the vendored asset set and steps until all
// three files are resident, which is when the frame it records is the frame the
// reference screenshot was taken of.
//
// The wait is wall clock rather than a frame count on purpose: a load does not
// run on the frame's thread, and decoding WaterBottle's 1024px textures takes
// longer than a few hundred headless frames of doing nothing else. That is
// exactly the hitch the asynchronous path exists to keep out of the frame, and
// a test that waited in frames would be asserting it does not exist.
func run(t *testing.T) (*headless.Engine, *Instancing) {
	t.Helper()
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	demo := New()
	engine := headless.NewOver(t, config, demo)
	deadline := time.Now().Add(60 * time.Second)
	for demo.ResidentCount() < len(modelPaths) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d files became resident; engine reported %v",
				demo.ResidentCount(), len(modelPaths), engine.Errors())
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
	// One more pair of steps: the frame that first sees every file resident is
	// also the first to record its draws, and Passes publishes the frame the
	// last flush consumed.
	engine.Steps(2)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine, demo
}

// pass is the frame's one pass. The camera declares no passes at all, so this
// is the implicit forward pass at the camera's own id.
func pass(t *testing.T, engine *headless.Engine) scene.PassView {
	t.Helper()
	passes := engine.Passes()
	if len(passes) != 1 {
		t.Fatalf("published %d passes, want the one implicit pass", len(passes))
	}
	if passes[0].CameraID != CameraMain {
		t.Fatalf("pass camera %d, want %d", passes[0].CameraID, CameraMain)
	}
	return passes[0]
}

// blendBatches is how many batches the blend bucket holds: one per blended
// primitive per screen, because a blend-class instanced call does not batch.
const blendBatches = len(paneStands) * PaneBlendPrimitives

// The lattice is what the demo says it is, and the colonnade is a rule rather
// than a number.
//
// Nothing else in the suite recomputes the layout, so this is what catches a
// buildField that dropped a row or stretched a crate it should not have. Every
// pillar carries a Matrix and no crate does, which is the whole of "non-uniform
// scale goes through the escape hatch": Transform.Scale is a single float and
// cannot say 0.5 by 3.4 by 0.5.
func TestTheLatticeIsTheDocumentedRule(t *testing.T) {
	center := gridSide / 2
	crates, pillars := 0, 0
	for i := range gridSide {
		for j := range gridSide {
			switch {
			case inCourtyard(i, j, center):
			case isPillar(i, j):
				crates, pillars = crates+1, pillars+1
			default:
				crates++
			}
		}
	}
	if CrateCount != crates || len(crateTransforms) != crates {
		t.Errorf("the field holds %d transforms and the rule says %d", CrateCount, crates)
	}
	if PillarCount != pillars {
		t.Errorf("the colonnade holds %d pillars and the rule says %d", PillarCount, pillars)
	}
	if pillars == 0 || pillars >= crates {
		t.Fatalf("%d pillars among %d crates is not a colonnade", pillars, crates)
	}
	matrices := 0
	for i := range crateTransforms {
		if crateTransforms[i].Matrix != nil {
			matrices++
			continue
		}
		if crateTransforms[i].Scale != crateSize {
			t.Fatalf("crate %d carries scale %v, want the scalar %v",
				i, crateTransforms[i].Scale, float32(crateSize))
		}
	}
	if matrices != PillarCount {
		t.Errorf("%d transforms carry a Matrix and %d are pillars", matrices, PillarCount)
	}
}

// Every file becomes resident, every instance of every primitive becomes a
// draw, and the frame packs the batches the layout predicts.
//
// This is the number the demo rests on. A model that silently loaded half of
// itself, or an instanced call that expanded to one draw instead of N, would
// still render a picture; only a spelled-out count catches either.
func TestEveryInstanceOfEveryPrimitiveBecomesADraw(t *testing.T) {
	engine, demo := run(t)
	if demo.ResidentCount() != len(modelPaths) {
		t.Fatalf("%d of %d files resident", demo.ResidentCount(), len(modelPaths))
	}
	view := pass(t, engine)
	if view.Recorded != RecordedDraws {
		t.Errorf("recorded %d draws, want %d", view.Recorded, RecordedDraws)
	}
	if view.Culled+view.Instances != view.Recorded {
		t.Errorf("recorded %d draws, culled %d and packed %d; the three do not add up",
			view.Recorded, view.Culled, view.Instances)
	}
	if len(view.Batches) != InstancedBatches {
		t.Errorf("packed %d batches, want %d", len(view.Batches), InstancedBatches)
	}
	packed := 0
	for _, batch := range view.Batches {
		packed += batch.InstanceCount
	}
	if packed != view.Instances {
		t.Errorf("the batches hold %d instances and the pass reports %d", packed, view.Instances)
	}
}

// The per-instance cull is the frustum's own answer, instance by instance.
//
// This is the demo's centrepiece and it is asserted against the published
// frustum rather than against a count: a call that culled the whole set when
// any one instance left the frustum, or none of it when any one stayed, would
// pass a count-shaped test at some pose. Every crate is tested here, the
// survivors are compared to the packed instance slice position by position, and
// the comparison is in order - which is also the proof that the survivors pack
// contiguously and in recording order, since they arrive as one batch of N.
func TestThePerInstanceCullAgreesWithThePublishedFrustum(t *testing.T) {
	engine, _ := run(t)
	view := pass(t, engine)
	local := modelSphere(t, engine, cratePath)

	var expected []m.Vec3
	for i := range crateTransforms {
		world := crateTransforms[i].Mat4()
		sphere := local.Transform(world)
		if view.Frustum.ContainsSphere(sphere.Center, sphere.Radius) {
			expected = append(expected, world.Translation())
		}
	}
	culled := CrateCount - len(expected)
	if len(expected) == 0 || culled == 0 {
		t.Fatalf("the reference pose keeps %d crates and culls %d; it is meant to do both",
			len(expected), culled)
	}
	// Nothing but crates is culled at the reference pose - the courtyard is in
	// front of the camera - so the pass's own count is the field's.
	if view.Culled != culled {
		t.Errorf("the pass culled %d draws and %d crates fail the frustum test",
			view.Culled, culled)
	}

	batch := largestBatch(t, view.Batches)
	if batch.InstanceCount != len(expected) {
		t.Fatalf("the field packed %d instances and %d crates pass the frustum test",
			batch.InstanceCount, len(expected))
	}
	instances := sceneInstances(t, engine)
	for i, want := range expected {
		got := instances[batch.FirstInstance+i].translation()
		if got.Distance(want) > positionEpsilon {
			t.Fatalf("packed instance %d of the field stands at %v, want the crate at %v",
				i, got, want)
		}
	}
}

// The non-uniform flag follows the instance, not the call.
//
// SCENE_NONUNIFORM is set per instance at pack time, and the pillars are
// entries in the very same Transforms slice as the cubes around them, as the
// squat bottles are in the bottles' - so a flag set per draw would mark all of
// each call or none of it. The ground is the frame's remaining non-uniform
// basis, a Plane being a stretched unit box, and it is counted here rather than
// excused.
func TestTheNonUniformFlagFollowsTheInstanceRatherThanTheCall(t *testing.T) {
	engine, _ := run(t)
	view := pass(t, engine)
	local := modelSphere(t, engine, cratePath)

	pillars := 0
	for i := range crateTransforms {
		if crateTransforms[i].Matrix == nil {
			continue
		}
		sphere := local.Transform(crateTransforms[i].Mat4())
		if view.Frustum.ContainsSphere(sphere.Center, sphere.Radius) {
			pillars++
		}
	}
	if pillars == 0 {
		t.Fatal("no pillar survives the reference pose; the flag has nothing to be set on")
	}
	squats := len(squatMatrices)
	if squats == 0 || squats == bottleCount {
		t.Fatalf("%d of %d bottles are squat; the row is meant to alternate", squats, bottleCount)
	}

	instances := sceneInstances(t, engine)
	nonUniform := 0
	for i := range instances {
		if instances[i].nonUniform() {
			nonUniform++
		}
	}
	if want := pillars + squats + 1; nonUniform != want {
		t.Errorf("%d of %d packed instances are non-uniform, want %d: %d surviving pillars, "+
			"%d squat bottles and the ground",
			nonUniform, len(instances), want, pillars, squats)
	}

	// And positively: the flagged instances of the field's own batch are the
	// pillars, not merely as many as there are pillars.
	batch := largestBatch(t, view.Batches)
	for i := range batch.InstanceCount {
		instance := instances[batch.FirstInstance+i]
		tall := instance.world[1].Y > crateSize*1.5
		if instance.nonUniform() != tall {
			t.Fatalf("packed instance %d of the field has y scale %v and nonUniform=%v",
				i, instance.world[1].Y, instance.nonUniform())
		}
	}
}

// The opaque and alpha-masked batches come first, sorted by material then mesh
// with no depth term, and the blended ones come last.
//
// The stack is what makes this more than a restatement of recording order. It
// is recorded after the bottles and the screens and carries a material interned
// before either of theirs, so the sort has to lift it back beside the field;
// everything else in the frame is already in key order as it is recorded, and a
// monotonic scan alone would pass with the sort deleted.
//
// It lands immediately after the field rather than merely before the bottles:
// the two share a mesh and a material and therefore a sort key, and the sort's
// final tiebreak is the recording ordinal, which the field wins. That they are
// two batches at all is the other half - a batch is the call, not the key.
func TestTheOpaqueBatchesComeOutInSortKeyOrder(t *testing.T) {
	engine, _ := run(t)
	view := pass(t, engine)
	batches := view.Batches
	opaque := batches[:len(batches)-blendBatches]
	if len(opaque) < 3 {
		t.Fatalf("%d opaque batches is not enough to sort", len(opaque))
	}
	for i := 1; i < len(opaque); i++ {
		if opaqueKey(opaque[i-1]) > opaqueKey(opaque[i]) {
			t.Fatalf("batch %d (material %d mesh %d) sorts after batch %d (material %d mesh %d)",
				i-1, opaque[i-1].MaterialID, opaque[i-1].MeshID,
				i, opaque[i].MaterialID, opaque[i].MeshID)
		}
	}

	field := largestBatch(t, batches)
	at := -1
	for i, batch := range batches {
		if batch == field {
			at = i
		}
	}
	if at < 0 || at+1 >= len(opaque) {
		t.Fatalf("the field is batch %d of %d opaque batches", at, len(opaque))
	}
	stack := batches[at+1]
	if stack.MaterialID != field.MaterialID || stack.MeshID != field.MeshID {
		t.Fatalf("the batch after the field is material %d mesh %d and the field is %d/%d; "+
			"the stack is a second call of the same model and must sort straight after it",
			stack.MaterialID, stack.MeshID, field.MaterialID, field.MeshID)
	}
	if stack.InstanceCount != stackCount {
		t.Errorf("the stack packed %d instances, want %d", stack.InstanceCount, stackCount)
	}
	// It was recorded after two models that sort after it, which is the whole
	// of what the sort had to do here.
	for _, batch := range batches[at+2:] {
		if batch.MaterialID <= stack.MaterialID {
			t.Errorf("batch material %d follows the stack's %d; the stack was recorded last "+
				"and nothing after it should sort below it", batch.MaterialID, stack.MaterialID)
		}
	}
	if len(batches[at+2:]) < 2 {
		t.Error("nothing sorts after the stack; it is meant to be recorded past two models")
	}
}

// opaqueKey is the key the opaque class is sorted by, material first and mesh
// second.
func opaqueKey(batch scene.BatchView) uint64 {
	return uint64(batch.MaterialID)<<32 | uint64(batch.MeshID)
}

// A blend-class instanced call splits back into one batch per instance, while
// the same call's opaque primitives stay one batch of two.
//
// Both halves come out of one Model call with two Transforms, which is what
// makes this the exception rather than a second demo: sorting the pair by its
// nearest instance would composite the far screen over the near one. The two
// screens stand at two depths, so the four blended batches interleave - abab -
// where a material-keyed sort would group them aabb.
func TestABlendedInstancedCallSplitsIntoOneBatchPerInstance(t *testing.T) {
	engine, demo := run(t)
	view := pass(t, engine)
	blend := view.Batches[len(view.Batches)-blendBatches:]

	for i, batch := range blend {
		if batch.InstanceCount != 1 {
			t.Fatalf("blend batch %d holds %d instances, want one each",
				i, batch.InstanceCount)
		}
	}
	meshes := map[uint32]int{}
	for _, batch := range blend {
		meshes[batch.MeshID]++
	}
	if len(meshes) != PaneBlendPrimitives {
		t.Fatalf("the blend bucket holds %d distinct meshes, want %d",
			len(meshes), PaneBlendPrimitives)
	}
	if blend[0].MeshID != blend[2].MeshID || blend[1].MeshID != blend[3].MeshID ||
		blend[0].MeshID == blend[1].MeshID {
		t.Fatalf("the blend bucket is ordered %d %d %d %d; a depth sort interleaves the "+
			"two screens and a material-keyed sort groups them",
			blend[0].MeshID, blend[1].MeshID, blend[2].MeshID, blend[3].MeshID)
	}

	// Each blended batch's instance is no nearer the eye than the one before
	// it, which is what back-to-front means.
	eye := demo.eye()
	instances := sceneInstances(t, engine)
	previous := float32(math.Inf(1))
	for i, batch := range blend {
		distance := instances[batch.FirstInstance].translation().Distance(eye)
		if distance > previous {
			t.Fatalf("blend batch %d is %.2f from the eye, behind the %.2f before it",
				i, distance, previous)
		}
		previous = distance
	}

	// The same call's opaque primitives did batch: one batch of two each.
	opaque := view.Batches[:len(view.Batches)-blendBatches]
	paired := 0
	for _, batch := range opaque {
		if batch.InstanceCount == len(paneStands) {
			paired++
		}
	}
	if want := panePrimitives - PaneBlendPrimitives; paired != want {
		t.Errorf("%d opaque batches hold both screens, want %d", paired, want)
	}
}

// Every batch is one instanced draw at the backend, reading its own slice of
// the pass's instance range.
//
// firstInstance is load-bearing rather than decorative: WebGPU's instance_index
// starts at it, so a batch reads sceneInstances with no offset plumbing of its
// own, and a draw whose firstInstance does not match its batch would render
// some other batch's transforms with no error anywhere.
func TestEveryBatchReachesTheBackendAsOneInstancedDraw(t *testing.T) {
	engine, _ := run(t)
	batches := pass(t, engine).Batches
	draws := sceneDraws(t, engine)
	if len(draws) != len(batches) {
		t.Fatalf("the frame packed %d batches and made %d scene draws",
			len(batches), len(draws))
	}
	for i, batch := range batches {
		if draws[i].FirstInstance != batch.FirstInstance || draws[i].Instances != batch.InstanceCount {
			t.Errorf("batch %d is %d instances from %d and the draw is %d from %d",
				i, batch.InstanceCount, batch.FirstInstance,
				draws[i].Instances, draws[i].FirstInstance)
		}
	}

	// And the draws tile the pass's instance range exactly: sorted by
	// firstInstance they run 0..Instances with no gap and no overlap. A batch
	// and its draw agreeing on a wrong number would pass the loop above; only
	// the tiling says the numbers are the arena's.
	covered := make([]bool, pass(t, engine).Instances)
	for _, call := range draws {
		for i := range call.Instances {
			at := call.FirstInstance + i
			if at >= len(covered) {
				t.Fatalf("a draw reads instance %d of a range holding %d", at, len(covered))
			}
			if covered[at] {
				t.Fatalf("two draws both read instance %d", at)
			}
			covered[at] = true
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Fatalf("instance %d was packed and no draw reads it", i)
		}
	}

	if engine.Backend().Presents == 0 {
		t.Error("the backend was never asked to present")
	}
}

// One instance arena for the whole frame, bound to every draw as a range, and
// one material record per batch inside a second one.
//
// The two are the same discipline and differ in what a range means. Every draw
// of a pass binds the same slice of the instance arena - the pass's own - and
// finds its instances inside it through firstInstance; every draw binds its own
// 160-byte material record at its own 256-aligned offset, because a storage
// binding's offset must be aligned and there is deliberately no dedupe.
func TestOneInstanceArenaAndOneMaterialRecordPerBatch(t *testing.T) {
	engine, _ := run(t)
	view := pass(t, engine)
	backend := engine.Backend()

	instances := lastBindings(t, engine, 0, 1, len(view.Batches))
	for i, bound := range instances {
		if bound.Buffer != instances[0].Buffer || bound.Offset != instances[0].Offset ||
			bound.Size != instances[0].Size {
			t.Fatalf("draw %d bound instances at buffer %d offset %d size %d, and the first "+
				"bound buffer %d offset %d size %d",
				i, bound.Buffer, bound.Offset, bound.Size,
				instances[0].Buffer, instances[0].Offset, instances[0].Size)
		}
	}
	if want := view.Instances * instanceRecordSize; instances[0].Size != want {
		t.Errorf("the pass bound %d bytes of instances for %d packed instances, want %d",
			instances[0].Size, view.Instances, want)
	}

	materials := lastBindings(t, engine, 1, 0, len(view.Batches))
	seen := map[int]bool{}
	for i, bound := range materials {
		if bound.Buffer != materials[0].Buffer {
			t.Errorf("batch %d took its material record from buffer %d, not the frame's %d",
				i, bound.Buffer, materials[0].Buffer)
		}
		if bound.Size != materialRecordSize {
			t.Errorf("batch %d bound %d bytes of material record, want %d",
				i, bound.Size, materialRecordSize)
		}
		if bound.Offset%storageAlignment != 0 {
			t.Errorf("batch %d bound its material record at offset %d, which is not %d-aligned",
				i, bound.Offset, storageAlignment)
		}
		if seen[bound.Offset] {
			t.Errorf("two batches share the material record at offset %d; there is no dedupe",
				bound.Offset)
		}
		seen[bound.Offset] = true
	}
	if len(seen) != len(view.Batches) {
		t.Errorf("%d distinct material records for %d batches", len(seen), len(view.Batches))
	}
	if backend.Presents == 0 {
		t.Error("the backend was never asked to present")
	}
}

// One call per crate packs exactly the frame one instanced call packs, in one
// batch per surviving crate instead of one batch.
//
// This is the deferred automatic collapse of consecutive equal draws, asserted
// from the other side: when it lands it has to be output-identical to the
// instanced form, and identical means these bytes. The two orders coincide
// because the crates share a mesh and a material and therefore a sort key, and
// the sort's final tiebreak is the recording ordinal.
func TestOneCallPerCrateIsTheSameFrameInMoreBatches(t *testing.T) {
	engine, demo := run(t)
	view := pass(t, engine)
	survivors := largestBatch(t, view.Batches).InstanceCount
	before := sceneInstances(t, engine)

	demo.perCall = true
	engine.Steps(2)
	after := pass(t, engine)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("drawing the field per crate reported %d errors, first: %v", len(errs), errs[0])
	}

	if after.Instances != view.Instances {
		t.Errorf("per crate the pass packed %d instances, and instanced it packed %d",
			after.Instances, view.Instances)
	}
	if want := InstancedBatches - 1 + survivors; len(after.Batches) != want {
		t.Errorf("per crate the pass packed %d batches, want %d: one per surviving crate "+
			"in place of the field's one", len(after.Batches), want)
	}
	packed := sceneInstances(t, engine)
	if len(packed) != len(before) {
		t.Fatalf("per crate the arena holds %d instances and instanced it held %d",
			len(packed), len(before))
	}
	for i := range before {
		if packed[i] != before[i] {
			t.Fatalf("packed instance %d is %+v per crate and was %+v instanced",
				i, packed[i], before[i])
		}
	}
}

// Orbiting changes which instances survive without changing what the frame is
// made of. The batch is the call, so a field that loses half its crates to the
// frustum is still one batch.
func TestOrbitingChangesTheSurvivorsAndNotTheBatches(t *testing.T) {
	engine, demo := run(t)
	before := pass(t, engine)
	beforeCulled, beforeBatches := before.Culled, len(before.Batches)

	demo.azimuth = startAzimuth + 0.9
	demo.elevation = 0.9
	engine.Steps(2)
	after := pass(t, engine)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("orbiting reported %d errors, first: %v", len(errs), errs[0])
	}

	if after.Culled == beforeCulled {
		t.Errorf("the same %d draws were culled from both poses; the cull is not per instance "+
			"against this camera's frustum", beforeCulled)
	}
	if len(after.Batches) != beforeBatches {
		t.Errorf("the frame packed %d batches from one pose and %d from the other; the batch "+
			"is the call, not its survivors", beforeBatches, len(after.Batches))
	}
	if after.Recorded != before.Recorded {
		t.Errorf("the frame recorded %d draws from one pose and %d from the other",
			before.Recorded, after.Recorded)
	}
}

// Every frame at a given pose is the same frame. The demo records nothing that
// moves on its own, which is what lets the reference screenshot be retaken by
// launching the demo and capturing it rather than by hitting a step number.
func TestTheReferencePoseIsTheSameFrameAtEveryStep(t *testing.T) {
	engine, demo := run(t)
	before := append([]scene.BatchView(nil), pass(t, engine).Batches...)
	step := demo.step

	engine.Steps(37)
	if demo.step == step {
		t.Fatal("the step counter did not advance")
	}
	after := pass(t, engine).Batches
	if len(before) != len(after) {
		t.Fatalf("the frame emitted %d batches and then %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("batch %d was %+v and is now %+v", i, before[i], after[i])
		}
	}
}

// Reading back what the flush packed.
//
// The instance record is decoded rather than asked for because there is nothing
// to ask: PassView reports how many instances a pass packed, and an instance's
// own matrix and flags are visible nowhere in the public surface. The layout is
// the one the specification fixes for builtin/scene/scene.wgsl, anchored at the
// end of the record so a field added ahead of it does not silently shift what
// is read:
//
//	SceneInstance 64 bytes: three rows of the 4x3 world matrix, an animation
//	                        offset and the flags
const (
	instanceRecordSize = 64
	// materialRecordSize is scenePbrRecord's size, and storageAlignment is the
	// offset alignment a storage binding must take, which is why 160-byte
	// records land 256 bytes apart.
	materialRecordSize = 160
	storageAlignment   = 256
	// sceneNonUniform marks an instance whose world matrix does not scale
	// uniformly, and is the shader's signal to take an inverse-transpose for
	// its normals.
	sceneNonUniform = 1
)

// packedInstance is one entry of the pass's instance slice.
type packedInstance struct {
	world [3]m.Vec4
	flags uint32
}

// translation is the instance's world position, which the packed rows carry in
// their w.
func (p packedInstance) translation() m.Vec3 {
	return m.Vec3{X: p.world[0].W, Y: p.world[1].W, Z: p.world[2].W}
}

func (p packedInstance) nonUniform() bool { return p.flags&sceneNonUniform != 0 }

// sceneInstances decodes the pass's instance slice. The binding is the pass's
// own range of the frame's instance arena, which is what makes a batch's
// FirstInstance an index into it.
func sceneInstances(t *testing.T, engine *headless.Engine) []packedInstance {
	t.Helper()
	bound := lastBindings(t, engine, 0, 1, 1)[0]
	data := engine.Backend().BoundBytes(bound)
	if data == nil {
		t.Fatal("the instance binding named an unbaked buffer")
	}
	out := make([]packedInstance, len(data)/instanceRecordSize)
	for i := range out {
		record := data[i*instanceRecordSize:]
		out[i] = packedInstance{
			world: [3]m.Vec4{vec4At(record, 0), vec4At(record, 16), vec4At(record, 32)},
			flags: binary.LittleEndian.Uint32(record[52:]),
		}
	}
	return out
}

// lastBindings is the last n bindings the frame made at one group and binding
// through a pipeline built from the bundled scene shader, in the order the
// frame made them.
//
// It scans backwards because the backend records every binding since the engine
// started, and the frames before the models were resident bound a much shorter
// instance range. Filtering on the pipeline is what keeps canvas's own bindings
// out, since gfx labels every pipeline alike and only the shader behind it
// differs; asking for exactly as many as the frame has draws is what keeps the
// previous frame's out.
func lastBindings(t *testing.T, engine *headless.Engine, group, binding, n int) []headless.BufferBinding {
	t.Helper()
	backend := engine.Backend()
	out := make([]headless.BufferBinding, 0, n)
	for i := len(backend.Buffers) - 1; i >= 0 && len(out) < n; i-- {
		bound := backend.Buffers[i]
		if bound.Group != group || bound.Binding != binding {
			continue
		}
		if !backend.IsScenePipeline(bound.Pipeline) {
			continue
		}
		out = append(out, bound)
	}
	if len(out) < n {
		t.Fatalf("the frame bound group %d binding %d %d times, want %d",
			group, binding, len(out), n)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// sceneDraws is the last frame's scene draws, in emission order.
//
// The backend's draw list accumulates across every frame since the engine
// started, and one frame's draws are the pass's own followed by canvas's HUD -
// so the last frame's scene draws are the run of scene-pipeline draws found by
// skipping the canvas draws at the end and stopping at the first canvas draw
// before them.
func sceneDraws(t *testing.T, engine *headless.Engine) []headless.DrawCall {
	t.Helper()
	backend := engine.Backend()
	end := len(backend.Draws)
	for end > 0 && !backend.IsScenePipeline(backend.Draws[end-1].Pipeline) {
		end--
	}
	start := end
	for start > 0 && backend.IsScenePipeline(backend.Draws[start-1].Pipeline) {
		start--
	}
	if start == end {
		t.Fatal("the frame made no scene draws")
	}
	return backend.Draws[start:end]
}

// largestBatch is the field's batch, which is the largest one in the frame by a
// wide margin: everything else in the courtyard is a handful of instances.
// Identifying it by size rather than by id is what keeps the test out of
// scene's internal id assignment, and the margin is asserted rather than
// assumed.
func largestBatch(t *testing.T, batches []scene.BatchView) scene.BatchView {
	t.Helper()
	best, second := scene.BatchView{}, 0
	for _, batch := range batches {
		if batch.InstanceCount > best.InstanceCount {
			best, second = batch, best.InstanceCount
			continue
		}
		second = max(second, batch.InstanceCount)
	}
	if best.InstanceCount < 2*second+1 {
		t.Fatalf("the largest batch holds %d instances and the next holds %d; the field is "+
			"meant to be unmistakable", best.InstanceCount, second)
	}
	return best
}

// modelSphere is a resident model's local-space bounding sphere, read from the
// lookup facade rather than transcribed. It is the sphere the culler tests, so
// a test that asks for it is asking the same question the flush answered.
func modelSphere(t *testing.T, engine *headless.Engine, path string) m.Sphere {
	t.Helper()
	var sphere m.Sphere
	ok := false
	engine.Lookup(func(la scene.LookupAccess) {
		var bounds m.Vec4
		bounds, ok = la.Bounds(scene.ModelRef{Path: path})
		sphere = m.Sphere{
			Center: m.Vec3{X: bounds.X, Y: bounds.Y, Z: bounds.Z}, Radius: bounds.W,
		}
	})
	if !ok {
		t.Fatalf("%s has no bounds", path)
	}
	return sphere
}

func vec4At(data []byte, offset int) m.Vec4 {
	return m.Vec4{
		X: floatAt(data, offset), Y: floatAt(data, offset+4),
		Z: floatAt(data, offset+8), W: floatAt(data, offset+12),
	}
}

func floatAt(data []byte, offset int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
}

// positionEpsilon is how close a packed translation has to be to count as the
// transform the demo asked for. The record is float32 of a float32, so the only
// slack needed is for nothing at all; a real tolerance would let a crate pass
// for its neighbour on a 1.8-unit lattice.
const positionEpsilon = 1e-4
