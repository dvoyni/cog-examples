package main

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// run starts the demo headless over the vendored asset set and steps until all
// three files are resident, which is when the frame it draws is the frame the
// reference screenshot was taken of.
//
// The loop settles on its first pass: the load System loads a file inside the
// step whose Hook first named it. What that step costs is the hitch this design
// accepts - decoding WaterBottle's 1024px textures is the expensive one here -
// and Preload is the lever a game pulls to move it.
func run(t *testing.T) (*headless.Engine, *Demo) {
	t.Helper()
	plugin := New()
	engine := headless.New(t, plugin)
	demo := plugin.demo
	deadline := time.Now().Add(60 * time.Second)
	for demo.ResidentCount() < len(modelPaths) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d files became resident; engine reported %v",
				demo.ResidentCount(), len(modelPaths), engine.Errors())
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
	// One more step: the HUD reads residency after the step that keyed the
	// files, so the step it first sees them in has already drawn them, and the
	// next is the first whose frame a test inspects.
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine, demo
}

// frame is what one step sent the backend through the camera's one pass: its
// draws in emission order, and the pass's instance range decoded.
type frame struct {
	draws     []headless.DrawCall
	instances []packedInstance
	backend   *headless.Backend
}

// cameraPassLabel is the label scene gives the camera's one default pass.
const cameraPassLabel = "scene.camera-100.forward"

// step takes one step and keeps what it sent the backend in the camera's pass.
//
// The backend's lists accumulate across every step since the engine started,
// so the step's own share is everything past their lengths before it. The
// camera's pass is found by its label, and its draws by the pass they were made
// in; canvas's HUD draws in passes of its own.
func step(t *testing.T, engine *headless.Engine) frame {
	t.Helper()
	backend := engine.Backend()
	passes, draws, buffers := len(backend.Passes), len(backend.Draws), len(backend.Buffers)
	presents := backend.Presents
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	if backend.Presents == presents {
		t.Fatal("the backend was not asked to present")
	}
	pass := -1
	for i := passes; i < len(backend.Passes); i++ {
		if backend.Passes[i].Label != cameraPassLabel {
			continue
		}
		if pass >= 0 {
			t.Fatalf("the step began two passes labelled %q", cameraPassLabel)
		}
		pass = i
	}
	if pass < 0 {
		t.Fatalf("the step began no pass labelled %q", cameraPassLabel)
	}
	f := frame{backend: backend}
	for _, call := range backend.Draws[draws:] {
		if call.Pass == pass {
			f.draws = append(f.draws, call)
		}
	}
	if len(f.draws) == 0 {
		t.Fatal("the camera's pass made no draws")
	}
	f.instances = decodeInstances(t, backend, buffers)
	return f
}

// total is how many instances the pass's draws read between them.
func (f frame) total() int {
	n := 0
	for _, call := range f.draws {
		n += call.Instances
	}
	return n
}

// blended reports whether a draw was made through a blending pipeline.
func (f frame) blended(call headless.DrawCall) bool {
	return f.backend.PipelineOf(call.Pipeline).State.Blend != gfx.BlendOpaque
}

// field is the field's draw, which is the largest in the frame by a wide
// margin: everything else in the courtyard is a handful of instances.
// Identifying it by size rather than by anything scene assigns is what keeps
// the test out of scene's internals, and the margin is asserted rather than
// assumed.
func (f frame) field(t *testing.T) headless.DrawCall {
	t.Helper()
	best, second := headless.DrawCall{}, 0
	for _, call := range f.draws {
		if call.Instances > best.Instances {
			best, second = call, best.Instances
			continue
		}
		second = max(second, call.Instances)
	}
	if best.Instances < 2*second+1 {
		t.Fatalf("the largest draw holds %d instances and the next holds %d; the field is "+
			"meant to be unmistakable", best.Instances, second)
	}
	return best
}

// read is the instances one draw reads, out of the pass's range.
func (f frame) read(t *testing.T, call headless.DrawCall) []packedInstance {
	t.Helper()
	if call.FirstInstance+call.Instances > len(f.instances) {
		t.Fatalf("a draw reads instances %d to %d of a range holding %d",
			call.FirstInstance, call.FirstInstance+call.Instances, len(f.instances))
	}
	return f.instances[call.FirstInstance : call.FirstInstance+call.Instances]
}

// The frustum the camera culls against at the demo's current pose, rebuilt
// from the same Camera Component and Transform the demo gave scene, at the
// headless framebuffer's aspect.
func frustum(t *testing.T, demo *Demo) m.Frustum {
	t.Helper()
	viewProjection, err := scene.ViewProjection(camera(), demo.place(),
		m.Vec2{X: headless.FramebufferWidth, Y: headless.FramebufferHeight})
	if err != nil {
		t.Fatalf("the camera has no view-projection: %v", err)
	}
	return m.FrustumFromMat4(viewProjection)
}

// survivors is the translation of every crate of the field the frustum keeps.
func survivors(t *testing.T, engine *headless.Engine, demo *Demo) []m.Vec3 {
	t.Helper()
	local := modelSphere(t, engine, cratePath)
	view := frustum(t, demo)
	var kept []m.Vec3
	for i := range crateTransforms {
		world := crateTransforms[i].Mat4()
		sphere := local.Transform(world)
		if view.ContainsSphere(sphere.Center, sphere.Radius) {
			kept = append(kept, world.Translation())
		}
	}
	return kept
}

// The lattice is what the demo says it is, and the colonnade is a rule rather
// than a number.
//
// Nothing else in the suite recomputes the layout, so this is what catches a
// buildField that dropped a row or stretched a crate it should not have. Every
// pillar carries a non-uniform Scale and every crate the uniform crate size.
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
	stretched := 0
	for i := range crateTransforms {
		if isStretched(crateTransforms[i]) {
			stretched++
			continue
		}
		if crateTransforms[i].Scale != m.NewVec3(crateSize) {
			t.Fatalf("crate %d carries scale %v, want the uniform %v",
				i, crateTransforms[i].Scale, float32(crateSize))
		}
	}
	if stretched != PillarCount {
		t.Errorf("%d transforms carry a non-uniform Scale and %d are pillars", stretched, PillarCount)
	}
}

// Every file becomes resident, every surviving instance of every primitive is
// drawn, and the frame makes the draws the layout predicts.
//
// This is the number the demo rests on. A model that silently loaded half of
// itself, or a Batch that drew one instance instead of N, would still render a
// picture; only a spelled-out count catches either.
func TestEverySurvivingInstanceIsDrawnInThePredictedDraws(t *testing.T) {
	engine, demo := run(t)
	if demo.ResidentCount() != len(modelPaths) {
		t.Fatalf("%d of %d files resident", demo.ResidentCount(), len(modelPaths))
	}
	f := step(t, engine)
	culled := CrateCount - len(survivors(t, engine, demo))
	if want := Instances - culled; f.total() != want {
		t.Errorf("the pass drew %d instances; %d exist and the frustum culls %d crates, "+
			"so want %d", f.total(), Instances, culled, want)
	}
	if len(f.draws) != InstancedDraws {
		t.Errorf("the pass made %d draws, want %d", len(f.draws), InstancedDraws)
	}
	if len(f.instances) != f.total() {
		t.Errorf("the pass bound %d instances and its draws read %d", len(f.instances), f.total())
	}
}

// The per-instance cull is the frustum's own answer, instance by instance.
//
// This is the demo's centrepiece and it is asserted against the frustum rather
// than against a count: a Batch that culled whole when any one instance left
// the frustum, or kept whole when any one stayed, would pass a count-shaped
// test at some pose. Every crate is tested here, and the field's draw is
// compared with the survivors instance by instance - which is also the proof
// that the survivors pack contiguously, since they arrive as one draw of N.
//
// They are compared as a set. The ECS leaves the order it walks Entities in
// unspecified, and scene packs a Batch's survivors in that order, so the order
// inside the draw is not the demo's to predict.
func TestThePerInstanceCullAgreesWithTheFrustum(t *testing.T) {
	engine, demo := run(t)
	f := step(t, engine)
	expected := survivors(t, engine, demo)
	culled := CrateCount - len(expected)
	if len(expected) == 0 || culled == 0 {
		t.Fatalf("the reference pose keeps %d crates and culls %d; it is meant to do both",
			len(expected), culled)
	}

	// The stack shares the field's key, so the draw is the surviving crates
	// and the stack's three.
	field := f.field(t)
	if field.Instances != len(expected)+stackCount {
		t.Fatalf("the field's draw holds %d instances and %d crates pass the frustum test, "+
			"with the stack's %d beside them", field.Instances, len(expected), stackCount)
	}
	want := append(slices.Clone(expected), stackTranslations()...)
	if missing := unmatched(want, translations(f.read(t, field))); len(missing) > 0 {
		t.Fatalf("%d of the field's survivors are not in its draw, first at %v",
			len(missing), missing[0])
	}
}

// stackTranslations is where each crate of the stack stands.
func stackTranslations() []m.Vec3 {
	out := make([]m.Vec3, len(stackTransforms))
	for i := range stackTransforms {
		out[i] = stackTransforms[i].Mat4().Translation()
	}
	return out
}

// The stack is spawned apart from the field and still draws in the field's one
// draw: a Batch is every Entity whose key is equal, and nothing in the key says
// which spawn placed an Entity, where it stands or how large it is.
func TestTheStackDrawsInTheFieldsDraw(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	field := f.field(t)
	if missing := unmatched(stackTranslations(), translations(f.read(t, field))); len(missing) > 0 {
		t.Fatalf("stacked crate at %v is not in the field's draw; it shares the field's "+
			"key and must share its draw", missing[0])
	}
	for _, call := range f.draws {
		if call == field {
			continue
		}
		for _, instance := range f.read(t, call) {
			for _, at := range stackTranslations() {
				if instance.translation().Distance(at) <= positionEpsilon {
					t.Fatalf("stacked crate at %v drew apart from the field", at)
				}
			}
		}
	}
}

// isStretched reports whether a transform scales its axes by different amounts,
// which in this field is what a pillar is.
func isStretched(transform m.Transform) bool {
	s := transform.Scale
	return s.X != s.Y || s.Y != s.Z
}

// The non-uniform flag follows the instance, not the Batch.
//
// SCENE_NONUNIFORM is set per instance as it is packed, and the pillars share
// the field's Batch with the cubes around them, as the squat bottles share the
// bottles' - so a flag set per draw would mark all of each draw or none of it.
// The ground and the lamp marker are debug shapes baked at their own size and
// drawn at an unscaled Transform, so neither is non-uniform.
func TestTheNonUniformFlagFollowsTheInstanceRatherThanTheBatch(t *testing.T) {
	engine, demo := run(t)
	f := step(t, engine)
	local := modelSphere(t, engine, cratePath)
	view := frustum(t, demo)

	pillars := 0
	for i := range crateTransforms {
		if !isStretched(crateTransforms[i]) {
			continue
		}
		sphere := local.Transform(crateTransforms[i].Mat4())
		if view.ContainsSphere(sphere.Center, sphere.Radius) {
			pillars++
		}
	}
	if pillars == 0 {
		t.Fatal("no pillar survives the reference pose; the flag has nothing to be set on")
	}
	squats := squatCount
	if squats == 0 || squats == bottleCount {
		t.Fatalf("%d of %d bottles are squat; the row is meant to alternate", squats, bottleCount)
	}

	nonUniform := 0
	for i := range f.instances {
		if f.instances[i].nonUniform() {
			nonUniform++
		}
	}
	if want := pillars + squats; nonUniform != want {
		t.Errorf("%d of %d packed instances are non-uniform, want %d: %d surviving pillars "+
			"and %d squat bottles", nonUniform, len(f.instances), want, pillars, squats)
	}

	// And positively: the flagged instances of the field's own draw are the
	// pillars, not merely as many as there are pillars.
	for i, instance := range f.read(t, f.field(t)) {
		tall := instance.world[1].Y > crateSize*1.5
		if instance.nonUniform() != tall {
			t.Fatalf("instance %d of the field's draw has y scale %v and nonUniform=%v",
				i, instance.world[1].Y, instance.nonUniform())
		}
	}
}

// A blended Batch splits into one draw per instance, while the same Model's
// opaque primitives stay one draw of two each.
//
// Both halves come out of the two screen Entities, which share a key per
// primitive, and that is what makes this the exception rather than a second
// demo: sorting the pair by its nearest instance would composite the far screen
// over the near one. The blended draws come after every opaque one, and walk
// back to front.
func TestABlendedBatchSplitsIntoOneDrawPerInstance(t *testing.T) {
	engine, demo := run(t)
	f := step(t, engine)

	blendAt := -1
	var blend []headless.DrawCall
	for i, call := range f.draws {
		if !f.blended(call) {
			if blendAt >= 0 {
				t.Fatalf("opaque draw %d follows blended draw %d; blending is drawn last", i, blendAt)
			}
			continue
		}
		if blendAt < 0 {
			blendAt = i
		}
		blend = append(blend, call)
	}
	if want := len(paneStands) * PaneBlendPrimitives; len(blend) != want {
		t.Fatalf("the pass made %d blended draws, want %d: one per blended primitive per screen",
			len(blend), want)
	}
	for i, call := range blend {
		if call.Instances != 1 {
			t.Fatalf("blended draw %d holds %d instances, want one each", i, call.Instances)
		}
	}

	// Each blended draw's instance is no nearer the eye than the one before
	// it, which is what back-to-front means.
	eye := demo.eye()
	previous := float32(math.Inf(1))
	for i, call := range blend {
		distance := f.read(t, call)[0].translation().Distance(eye)
		if distance > previous {
			t.Fatalf("blended draw %d is %.2f from the eye, behind the %.2f before it",
				i, distance, previous)
		}
		previous = distance
	}

	// The same Entities' opaque primitives did batch: one draw of two each.
	paired := 0
	for _, call := range f.draws {
		if !f.blended(call) && call.Instances == len(paneStands) {
			paired++
		}
	}
	if want := panePrimitives - PaneBlendPrimitives; paired != want {
		t.Errorf("%d opaque draws hold both screens, want %d", paired, want)
	}
}

// Every draw reads its own slice of the pass's instance range, and the slices
// tile it.
//
// firstInstance is load-bearing rather than decorative: WebGPU's instance_index
// starts at it, so a draw reads sceneInstances with no offset plumbing of its
// own, and a draw whose firstInstance is wrong would render some other Batch's
// transforms with no error anywhere. Sorted by firstInstance the draws run
// 0..n with no gap and no overlap; a draw reading a wrong range that happened
// to hold the right count would fail here.
func TestEveryDrawReadsItsOwnSliceOfThePassRange(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	covered := make([]bool, len(f.instances))
	for _, call := range f.draws {
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
}

// One instance range per pass, bound to every draw of the bundled shader.
//
// Every draw of a pass binds the same slice of the frame's instance arena - the
// pass's own - and finds its instances inside it through firstInstance, so the
// slice is exactly as long as the pass's instances.
func TestOneInstanceRangePerPass(t *testing.T) {
	engine, _ := run(t)
	backend := engine.Backend()
	buffers := len(backend.Buffers)
	f := step(t, engine)

	bound := instanceBindings(backend, buffers)
	if len(bound) == 0 {
		t.Fatal("no draw of the bundled shader bound its instances")
	}
	for i, binding := range bound {
		if binding.Buffer != bound[0].Buffer || binding.Offset != bound[0].Offset ||
			binding.Size != bound[0].Size {
			t.Fatalf("draw %d bound instances at buffer %d offset %d size %d, and the first "+
				"bound buffer %d offset %d size %d",
				i, binding.Buffer, binding.Offset, binding.Size,
				bound[0].Buffer, bound[0].Offset, bound[0].Size)
		}
	}
	if want := f.total() * instanceRecordSize; bound[0].Size != want {
		t.Errorf("the pass bound %d bytes of instances for %d drawn instances, want %d",
			bound[0].Size, f.total(), want)
	}
}

// Key 2 gives every crate a key of its own, and the field becomes one draw per
// surviving crate reading exactly the instances the one draw read; key 1 folds
// them back into one.
//
// This is what a Batch key costs, asserted from both sides: the same instances,
// byte for byte, in one draw or in five hundred. The parameter key 2 writes is
// one no shader declares, so it changes the key and nothing else.
func TestAKeyPerCrateIsTheSameInstancesInADrawPerCrate(t *testing.T) {
	engine, demo := run(t)
	shared := step(t, engine)
	kept := len(survivors(t, engine, demo)) + stackCount

	perCrate := tap(t, engine, input.Key2)
	if demo.FieldKeys() != CrateCount+stackCount {
		t.Fatalf("after key 2 the crates hold %d keys, want one each", demo.FieldKeys())
	}
	if want := len(shared.draws) - 1 + kept; len(perCrate.draws) != want {
		t.Errorf("a key per crate made %d draws, want %d: the %d other draws and one for "+
			"each of the %d crates in view", len(perCrate.draws), want,
			len(shared.draws)-1, kept)
	}
	if !sameRecords(shared.instances, perCrate.instances) {
		t.Error("a key per crate packed different instances than one shared key")
	}

	again := tap(t, engine, input.Key1)
	if len(again.draws) != len(shared.draws) {
		t.Errorf("key 1 made %d draws, want the %d one shared key makes",
			len(again.draws), len(shared.draws))
	}
	if again.field(t).Instances != kept {
		t.Errorf("key 1 folded %d crates back into the field's draw, want %d",
			again.field(t).Instances, kept)
	}
}

// tap presses one key through input, as a window would, takes the step that
// reads the press, and releases the key. The release comes after the step
// because input clears a JustPressed edge at its next apply, and the demo's own
// key handling is what the step is meant to see.
func tap(t *testing.T, engine *headless.Engine, key input.Key) frame {
	t.Helper()
	engine.Input(input.KeyChange(key, 0, true))
	f := step(t, engine)
	engine.Input(input.KeyChange(key, 0, false))
	return f
}

// Orbiting changes which instances survive without changing what the frame is
// made of. A Batch is its Entities, not its survivors, so a field that loses
// more of its crates to the frustum is still one draw.
func TestOrbitingChangesTheSurvivorsAndNotTheDraws(t *testing.T) {
	engine, demo := run(t)
	before := step(t, engine)

	demo.azimuth = startAzimuth + 0.9
	demo.elevation = 0.9
	after := step(t, engine)

	if after.total() == before.total() {
		t.Errorf("the pass drew %d instances from both poses; the cull is not per instance "+
			"against this camera's frustum", before.total())
	}
	if want := Instances - CrateCount + len(survivors(t, engine, demo)); after.total() != want {
		t.Errorf("from the second pose the pass drew %d instances and the frustum keeps %d",
			after.total(), want)
	}
	if len(after.draws) != len(before.draws) {
		t.Errorf("the frame made %d draws from one pose and %d from the other; a Batch "+
			"is its Entities, not its survivors", len(before.draws), len(after.draws))
	}
}

// Every frame at a given pose is the same frame. The demo moves nothing on its
// own, which is what lets the reference screenshot be retaken by launching the
// demo and capturing it rather than by hitting a step number.
func TestTheReferencePoseIsTheSameFrameAtEveryStep(t *testing.T) {
	engine, demo := run(t)
	before := step(t, engine)
	at := demo.step

	engine.Steps(36)
	after := step(t, engine)
	if demo.step == at {
		t.Fatal("the step counter did not advance")
	}
	if len(before.draws) != len(after.draws) {
		t.Fatalf("the frame made %d draws and then %d", len(before.draws), len(after.draws))
	}
	for i := range before.draws {
		b, a := before.draws[i], after.draws[i]
		if b.First != a.First || b.Count != a.Count ||
			b.Instances != a.Instances || b.FirstInstance != a.FirstInstance {
			t.Fatalf("draw %d was %+v and is now %+v", i, b, a)
		}
	}
	if !slices.Equal(before.instances, after.instances) {
		t.Error("the frame packed different instances at the same pose")
	}
}

// Reading back what the step packed.
//
// The instance record is decoded rather than asked for because there is nothing
// to ask: an instance's own matrix and flags are visible nowhere in the public
// surface. The layout is model's Instance, which builtin/model's shaders read,
// and the flag is model's own constant:
//
//	Instance 64 bytes: three rows of the 4x3 world matrix, an animation offset,
//	                   the flags, a joint and a mesh slot
const instanceRecordSize = 64

// packedInstance is one entry of the pass's instance range.
type packedInstance struct {
	world [3]m.Vec4
	flags uint32
}

// translation is the instance's world position, which the packed rows carry in
// their w.
func (p packedInstance) translation() m.Vec3 {
	return m.Vec3{X: p.world[0].W, Y: p.world[1].W, Z: p.world[2].W}
}

func (p packedInstance) nonUniform() bool { return p.flags&model.SceneNonUniform != 0 }

func translations(instances []packedInstance) []m.Vec3 {
	out := make([]m.Vec3, len(instances))
	for i := range instances {
		out[i] = instances[i].translation()
	}
	return out
}

// unmatched is every position in want with no position in got within
// positionEpsilon, each position in got matching at most one.
func unmatched(want, got []m.Vec3) []m.Vec3 {
	used := make([]bool, len(got))
	var missing []m.Vec3
	for _, w := range want {
		found := false
		for i, g := range got {
			if !used[i] && g.Distance(w) <= positionEpsilon {
				used[i], found = true, true
				break
			}
		}
		if !found {
			missing = append(missing, w)
		}
	}
	return missing
}

// sameRecords reports whether two instance ranges hold the same records,
// whatever order they hold them in.
func sameRecords(a, b []packedInstance) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, record := range a {
		found := false
		for i := range b {
			if !used[i] && b[i] == record {
				used[i], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// instanceBindings is every binding of sceneInstances the draws of the bundled
// shader made since the backend's Buffers held from entries. Filtering on the
// pipeline is what keeps canvas's bindings out, since gfx labels every pipeline
// alike and only the shader behind it differs.
func instanceBindings(backend *headless.Backend, from int) []headless.BufferBinding {
	var out []headless.BufferBinding
	for _, binding := range backend.Buffers[from:] {
		if binding.Group == 0 && binding.Binding == 1 && backend.IsScenePipeline(binding.Pipeline) {
			out = append(out, binding)
		}
	}
	return out
}

// decodeInstances decodes the pass's instance range, as the first draw of the
// bundled shader since from bound it. Every such draw binds the same range,
// which TestOneInstanceRangePerPass asserts.
func decodeInstances(t *testing.T, backend *headless.Backend, from int) []packedInstance {
	t.Helper()
	bound := instanceBindings(backend, from)
	if len(bound) == 0 {
		t.Fatal("no draw of the bundled shader bound its instances")
	}
	data := backend.BoundBytes(bound[0])
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

// modelSphere is a resident model's local-space bounding sphere, read from the
// lookup facade rather than transcribed. It is the sphere scene culls by, so a
// test that asks for it is asking the same question scene's cull answered.
func modelSphere(t *testing.T, engine *headless.Engine, path string) m.Sphere {
	t.Helper()
	var sphere m.Sphere
	ok := false
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		var bounds m.Vec4
		bounds, ok = la.Bounds(model.ModelRef{Path: path})
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
