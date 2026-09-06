package main

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// run starts the demo headless over the vendored asset set and steps until
// every station is resident, which is when the frame it records is the frame
// the reference screenshot was taken of.
//
// The wait is wall clock rather than a frame count on purpose: a load does not
// run on the frame's thread, and decoding WaterBottle's 1024px textures takes
// longer than a few hundred headless frames of doing nothing else. That is
// exactly the hitch the asynchronous path exists to keep out of the frame, and
// a test that waited in frames would be asserting it does not exist.
func run(t *testing.T) (*headless.Engine, *Pbr) {
	t.Helper()
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	demo := New()
	engine := headless.NewOver(t, config, demo)
	deadline := time.Now().Add(60 * time.Second)
	for demo.ResidentCount() < len(stations) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d stations became resident; engine reported %v",
				demo.ResidentCount(), len(stations), engine.Errors())
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
	// One more pair of steps: the frame that first sees every model resident is
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

// Every station's model becomes resident and every one of its primitives
// becomes a draw. This is the number the whole demo rests on: a model that
// silently loaded half of itself would still render a picture, and only a
// spelled-out count catches it.
func TestEveryStationBecomesResidentAndEveryPrimitiveBecomesADraw(t *testing.T) {
	engine, demo := run(t)
	if demo.ResidentCount() != len(stations) {
		t.Fatalf("%d of %d stations resident", demo.ResidentCount(), len(stations))
	}
	view := pass(t, engine)
	if view.Recorded != RecordedDraws {
		t.Errorf("recorded %d draws, want %d", view.Recorded, RecordedDraws)
	}
	if view.Culled != 0 {
		t.Errorf("culled %d draws at the reference pose, want none", view.Culled)
	}
	if view.Instances != RecordedDraws {
		t.Errorf("packed %d instances, want %d", view.Instances, RecordedDraws)
	}
	// Nothing here is an instanced draw, so a batch is a draw.
	if len(view.Batches) != RecordedDraws {
		t.Errorf("emitted %d batches for %d draws, want one each",
			len(view.Batches), RecordedDraws)
	}
}

// The opaque and alpha-masked draws come first, sorted by material then mesh
// with no depth term, and the blended ones come last. MASK is fixed-function
// identical to OPAQUE plus a shader discard, so the three cutoff plates sort
// with the opaque geometry rather than into a bucket of their own.
func TestTheOpaqueBucketSortsByMaterialThenMeshAndComesFirst(t *testing.T) {
	engine, _ := run(t)
	batches := pass(t, engine).Batches
	opaque := batches[:len(batches)-blendBatches]
	for i := 1; i < len(opaque); i++ {
		if opaqueKey(opaque[i-1]) > opaqueKey(opaque[i]) {
			t.Fatalf("batch %d (material %d mesh %d) sorts after batch %d (material %d mesh %d)",
				i-1, opaque[i-1].MaterialID, opaque[i-1].MeshID,
				i, opaque[i].MaterialID, opaque[i].MeshID)
		}
	}
}

// blendBatches is how many batches the blend bucket holds: the alpha model's
// two BLEND primitives, once per copy.
const blendBatches = 2 * BlendPrimitives

// opaqueKey is the key the opaque class is sorted by, material first and mesh
// second.
func opaqueKey(batch scene.BatchView) uint64 {
	return uint64(batch.MaterialID)<<32 | uint64(batch.MeshID)
}

// The blend bucket is sorted back to front, and the two copies of the alpha
// model interleave rather than group.
//
// This is why the station is drawn twice. Both copies contribute the same two
// primitives - TestBlendMesh and DecalBlendMesh, both MatBlend - so all four
// batches share a material id and hold exactly two mesh ids between them. Sorted
// by the opaque key those four come out grouped, aa bb; sorted back to front by
// depth they come out interleaved, abab, because the whole of the far copy is
// behind the whole of the near one. One copy could not tell the two orders
// apart.
func TestTheBlendBucketIsBackToFrontRatherThanByMaterialKey(t *testing.T) {
	engine, demo := run(t)
	view := pass(t, engine)
	blend := view.Batches[len(view.Batches)-blendBatches:]

	material := blend[0].MaterialID
	meshes := map[uint32]int{}
	for _, batch := range blend {
		if batch.MaterialID != material {
			t.Fatalf("the blend bucket holds materials %d and %d, want only MatBlend",
				material, batch.MaterialID)
		}
		meshes[batch.MeshID]++
	}
	if len(meshes) != BlendPrimitives {
		t.Fatalf("the blend bucket holds %d distinct meshes, want %d",
			len(meshes), BlendPrimitives)
	}
	if blend[0].MeshID != blend[2].MeshID || blend[1].MeshID != blend[3].MeshID ||
		blend[0].MeshID == blend[1].MeshID {
		t.Fatalf("the blend bucket is ordered %d %d %d %d; a depth sort interleaves the "+
			"two copies and a material-keyed sort groups them",
			blend[0].MeshID, blend[1].MeshID, blend[2].MeshID, blend[3].MeshID)
	}

	// And positively: each batch's instance is no nearer the eye than the one
	// before it. The sort key is view-space distance to the bounding sphere's
	// centre, and each of these primitives is centred on the node it hangs off,
	// so its world translation stands for that centre.
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
}

// glTF's alphaMode and doubleSided reach the pipeline as fixed-function state.
// The demo's six files carry all three modes and both facings, and the state
// they land on is what a headless run can read back only from the pipeline
// descriptions the frame created.
//
// FrontFace stays CCW for every one of them. It flips to CW only for a
// primitive under a mirrored node transform, and no file in the vendored set has
// one; a demo cannot provoke it either, because the flip is decided at load from
// the file's own nodes and not from the transform a draw is given.
func TestAlphaModeAndDoubleSidedReachThePipelineState(t *testing.T) {
	engine, _ := run(t)
	backend := engine.Backend()

	var opaqueCulled, opaqueTwoSided, blended int
	for i := range backend.Pipelines {
		desc := &backend.Pipelines[i]
		if backend.ShaderPath(desc.Shader) != headless.SceneShaderPath {
			continue
		}
		if desc.State.FrontFace != gfx.FrontCCW {
			t.Errorf("a scene pipeline declares FrontFace %v, want CCW: no vendored file "+
				"has a mirrored node", desc.State.FrontFace)
		}
		switch {
		case desc.State.Blend == gfx.BlendAlpha:
			blended++
			if desc.State.DepthWrite {
				t.Error("a blended pipeline writes depth; transparency would occlude itself")
			}
			if desc.State.Cull != gfx.CullNone {
				t.Errorf("MatBlend is doubleSided, so its pipeline culls %v, want none",
					desc.State.Cull)
			}
		case desc.State.Cull == gfx.CullNone:
			opaqueTwoSided++
		default:
			opaqueCulled++
		}
		if desc.State.Blend != gfx.BlendAlpha && !desc.State.DepthWrite {
			t.Error("an opaque or masked pipeline does not write depth")
		}
	}
	if blended == 0 {
		t.Error("no blended pipeline: AlphaBlendModeTest's MatBlend is alphaMode BLEND")
	}
	if opaqueTwoSided == 0 {
		t.Error("no two-sided opaque pipeline: MatOpaque and the cutoff materials are doubleSided")
	}
	if opaqueCulled == 0 {
		t.Error("no back-culled pipeline: BottleMat and MatBed are single-sided")
	}
}

// Twenty-one lights are recorded, sixteen are packed, and nothing is reported.
// The silence is the contract: a seventeenth light is dynamic and camera-shaped,
// so there is no natural moment to report at.
func TestTheCapPacksSixteenOfTwentyOneAndSaysNothing(t *testing.T) {
	engine, demo := run(t)
	if demo.declared != RecordedLights {
		t.Fatalf("the demo declared %d lights, want %d", demo.declared, RecordedLights)
	}
	view := pass(t, engine)
	if view.Lights != MaxLights {
		t.Fatalf("the pass packed %d lights, want the cap of %d", view.Lights, MaxLights)
	}
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("dropping %d lights reported %d errors, first: %v",
			RecordedLights-MaxLights, len(errs), errs[0])
	}
}

// The sixteen kept at the reference pose are the fill, the two spots, the
// station's own eight and the five rim lamps; the five deep lamps are the five
// dropped. Which is which is only visible in the packed light array itself:
// PassView reports how many were packed, and the drop is silent everywhere else.
func TestTheReferencePoseKeepsTheRimLampsAndDropsTheDeepOnes(t *testing.T) {
	engine, _ := run(t)
	packed := packedLights(t, engine)
	if len(packed) != MaxLights {
		t.Fatalf("the frame block holds %d lights, want %d", len(packed), MaxLights)
	}

	// The fill light leaves Range at zero, so its packed 1/range^4 is zero and
	// it has no sphere to be culled by. It is here at every pose.
	if !containsPosition(packed, m.Vec3{X: 0, Y: fillHeight, Z: fillDepth}) {
		t.Error("the infinite-range fill light was dropped; a Range of zero has no sphere")
	}
	for i := range rimColors {
		position := m.Vec3{X: spread(i, rimCount, rimSpacing), Y: rimY, Z: rimZ}
		if !containsPosition(packed, position) {
			t.Errorf("rim lamp %d at %v was dropped; its range reaches the eye", i, position)
		}
	}
	for i := range deepColors {
		position := m.Vec3{X: spread(i, deepCount, deepSpacing), Y: deepY, Z: deepZ}
		if containsPosition(packed, position) {
			t.Errorf("deep lamp %d at %v was kept; its range window is closed at the eye",
				i, position)
		}
	}
}

// Turn the camera round and the selection follows it: the deep lamps that scored
// nothing from the front score from behind, and take the places of lights that
// score nothing from there. The count never moves off the cap and nothing is
// reported, which is the whole of "dynamic and camera-shaped".
func TestTheCapSelectionFollowsTheCamera(t *testing.T) {
	engine, demo := run(t)
	front := packedLights(t, engine)

	demo.azimuth = startAzimuth + math.Pi
	engine.Steps(2)
	behind := packedLights(t, engine)

	if view := pass(t, engine); view.Lights != MaxLights {
		t.Fatalf("from behind the pass packed %d lights, want the cap of %d",
			view.Lights, MaxLights)
	}
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("orbiting reported %d errors, first: %v", len(errs), errs[0])
	}

	kept := 0
	for i := range deepColors {
		position := m.Vec3{X: spread(i, deepCount, deepSpacing), Y: deepY, Z: deepZ}
		if containsPosition(behind, position) {
			kept++
		}
	}
	if kept == 0 {
		t.Error("no deep lamp was packed from behind; the cap is not ranking by contribution at the eye")
	}
	if samePositions(front, behind) {
		t.Error("the same sixteen lights were packed from both poses; the selection is not camera-shaped")
	}
	if !containsPosition(behind, m.Vec3{X: 0, Y: fillHeight, Z: fillDepth}) {
		t.Error("the infinite-range fill light was dropped from behind")
	}
}

// The plinths and the ground are the frame's only non-uniform bases, and they
// are all the demo has: Transform.Scale is scalar by design, so a slab has to go
// through Transform.Matrix. Every model draw's own basis is a uniform scale and
// a rotation, which is what makes the plinths the first rotated non-uniform
// bases the bundled PBR shades in this tree - Line3D and WireBox build such a
// matrix in box but are self-lit and never read a normal.
func TestOnlyThePlinthsAndTheGroundArePackedNonUniform(t *testing.T) {
	engine, _ := run(t)
	instances := sceneInstances(t, engine)
	nonUniform := 0
	for i := range instances {
		if instances[i].nonUniform() {
			nonUniform++
		}
	}
	if want := len(stations) + 1; nonUniform != want {
		t.Errorf("%d of %d instances are packed non-uniform, want %d: the six plinths and the ground",
			nonUniform, len(instances), want)
	}
}

// Every frame at a given pose is the same frame. The demo records nothing that
// moves on its own, which is what lets the reference screenshot be retaken by
// launching the demo and capturing it rather than by hitting a step number.
func TestTheReferencePoseIsTheSameFrameAtEveryStep(t *testing.T) {
	engine, demo := run(t)
	before := append([]scene.BatchView(nil), pass(t, engine).Batches...)
	beforeLights := packedLights(t, engine)
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
	if !samePositions(beforeLights, packedLights(t, engine)) {
		t.Error("the packed light selection changed while the camera stood still")
	}
}

// The stations are in the order the keys, the HUD and the package comment's
// table all name them: the front row left to right, then the back row. Nothing
// derives that order, so nothing but this catches the table drifting out of step
// with the array - which it did once, and the browser run is what found it,
// because pressing 4 named a different station than the doc did.
func TestTheStationOrderIsTheDocumentedKeyOrder(t *testing.T) {
	want := [len(stations)]string{"bottle", "compare", "alpha", "vertex", "emissive", "lights"}
	for i := range stations {
		if stations[i].name != want[i] {
			t.Errorf("key %d selects %q, and the table says %q", i+1, stations[i].name, want[i])
		}
	}
	if len(focusKeys) != len(stations) {
		t.Errorf("%d focus keys for %d stations", len(focusKeys), len(stations))
	}
	// The front row stands nearer the camera than the back row, which is what
	// makes "front row then back row" a description of the still life rather
	// than of the array.
	for i := range stations[:3] {
		if stations[i].z <= stations[i+3].z {
			t.Errorf("station %q is not in front of %q", stations[i].name, stations[i+3].name)
		}
	}
}

// The frame reaches the GPU: every packed instance becomes a draw, and the frame
// is presented. This is the only assertion that looks past what scene decided,
// and it is here so a frame that decides correctly and then never renders is not
// a passing test.
func TestThePackedInstancesReachTheBackend(t *testing.T) {
	engine, _ := run(t)
	backend := engine.Backend()
	if backend.Presents == 0 {
		t.Error("the backend was never asked to present")
	}
	drawn := 0
	for _, call := range backend.Draws {
		drawn += call.Instances
	}
	if view := pass(t, engine); drawn < view.Instances {
		t.Errorf("the backend drew %d instances, the pass packed %d", drawn, view.Instances)
	}
}

// The demo leaves every default unwritten. Its camera declares no cull mask, no
// intensities, no projection and no passes, and the plinths are the only draws
// that write a Matrix.
func TestTheDemoLeavesTheDefaultsUnwritten(t *testing.T) {
	engine, _ := run(t)
	ops := engine.Ops()
	if len(ops) == 0 || ops[0].Kind != scene.OpCamera {
		t.Fatal("the first op is not the camera registration")
	}
	camera := ops[0].Descr
	if camera.CullMask != 0 || camera.Projection != 0 || len(camera.Passes) != 0 {
		t.Errorf("the camera wrote CullMask %v, Projection %v and %d passes; the demo means to leave them zero",
			camera.CullMask, camera.Projection, len(camera.Passes))
	}
	if camera.SunIntensity != 0 || camera.AmbientIntensity != 0 {
		t.Errorf("the camera wrote intensities %v and %v; the demo means to leave them zero",
			camera.SunIntensity, camera.AmbientIntensity)
	}
	fill := false
	for i := range ops {
		if ops[i].Kind == scene.OpPointLight && ops[i].Light.Range == 0 {
			fill = true
		}
	}
	if !fill {
		t.Error("no light left Range at zero; the fill light means to take glTF's own default")
	}
}

// Reading back what the flush packed.
//
// Two of scene's storage records are decoded here, and they are decoded rather
// than asked for because there is nothing to ask: PassView reports how many
// lights a pass packed and how many instances it packed, and the cap's own
// selection and an instance's flags are visible nowhere else in the public
// surface. Both layouts are the ones the specification fixes for
// builtin/scene/scene.wgsl, and both are anchored at the end of their record so
// that a field added ahead of them does not silently shift what is read:
//
//	SceneLight   48 bytes: position vec3, 1/range^4, direction vec3, spotScale,
//	                       colour vec3, spotOffset
//	sceneFrame            ends with lightCount, three words of padding and
//	                       lights: array<SceneLight, 16>
//	SceneInstance 64 bytes: three rows of the 4x3 world matrix, an animation
//	                       offset and the flags
const (
	lightRecordSize    = 48
	instanceRecordSize = 64
	// sceneNonUniform marks an instance whose world matrix does not scale
	// uniformly, and is the shader's signal to take an inverse-transpose for
	// its normals.
	sceneNonUniform = 1
)

// packedLight is one entry of the frame block's light array.
type packedLight struct {
	position  m.Vec3
	invRange4 float32
}

// packedLights decodes the light array out of the pass's own sceneFrame block.
func packedLights(t *testing.T, engine *headless.Engine) []packedLight {
	t.Helper()
	block := sceneBytes(t, engine, 0, 0)
	array := block[len(block)-MaxLights*lightRecordSize:]
	// lightCount sits sixteen bytes before the array: it is a u32 followed by
	// three words of padding, which is what takes the array to its own
	// alignment.
	count := int(binary.LittleEndian.Uint32(block[len(block)-MaxLights*lightRecordSize-16:]))
	if count > MaxLights {
		t.Fatalf("the frame block says %d lights, past the cap of %d", count, MaxLights)
	}
	out := make([]packedLight, count)
	for i := range out {
		record := array[i*lightRecordSize:]
		out[i] = packedLight{
			position:  vec3At(record, 0),
			invRange4: floatAt(record, 12),
		}
	}
	return out
}

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
	data := sceneBytes(t, engine, 0, 1)
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

// sceneBytes is the bytes most recently bound at one group and binding by a
// pipeline built from the bundled scene shader.
//
// It scans backwards because the backend records every binding since the engine
// started, and the frames before the models were resident bound a much shorter
// instance range - the ground and six plinths and nothing else. Every scene draw
// within one frame binds the same range of both of these, so the last is the
// last frame's; filtering on the pipeline is what keeps canvas's own bindings
// out, since gfx labels every pipeline alike and only the shader behind it
// differs.
func sceneBytes(t *testing.T, engine *headless.Engine, group, binding int) []byte {
	t.Helper()
	backend := engine.Backend()
	for i := len(backend.Buffers) - 1; i >= 0; i-- {
		bound := backend.Buffers[i]
		if bound.Group != group || bound.Binding != binding {
			continue
		}
		if !backend.IsScenePipeline(bound.Pipeline) {
			continue
		}
		if data := backend.BoundBytes(bound); data != nil {
			return data
		}
		t.Fatalf("the binding at group %d binding %d named an unbaked buffer", group, binding)
	}
	t.Fatalf("no scene draw bound group %d binding %d", group, binding)
	return nil
}

func vec3At(data []byte, offset int) m.Vec3 {
	return m.Vec3{X: floatAt(data, offset), Y: floatAt(data, offset+4), Z: floatAt(data, offset+8)}
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

// positionEpsilon is how close a packed position has to be to count as the light
// the demo declared. The record is float32 of a float32, so the only slack
// needed is for nothing at all; a real tolerance would let a light half a unit
// away pass for its neighbour.
const positionEpsilon = 1e-4

func containsPosition(lights []packedLight, position m.Vec3) bool {
	for i := range lights {
		if lights[i].position.Distance(position) < positionEpsilon {
			return true
		}
	}
	return false
}

// samePositions reports whether two selections hold the same lights, in any
// order: the buffer's order is an insertion order rather than a ranking, and the
// shader only sums it.
func samePositions(a, b []packedLight) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !containsPosition(b, a[i].position) {
			return false
		}
	}
	return true
}
