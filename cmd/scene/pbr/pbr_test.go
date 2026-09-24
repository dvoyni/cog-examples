package main

import (
	"encoding/binary"
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// run starts the demo headless over the vendored asset set, preloads every
// station's file, and steps until the frame it draws is the frame the reference
// screenshot was taken of.
//
// Preload loads synchronously: by the time it returns, the file has been read,
// parsed and uploaded, so there is nothing to wait for. The first step then
// keys every Model Entity and spawns the lamps, and the two after it are the
// steady still life.
func run(t *testing.T) (*headless.Engine, *Pbr) {
	t.Helper()
	demo := New()
	engine := headless.New(t, demo)
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		for i := range stations {
			la.Preload(stations[i].path)
			if err := la.State(stations[i].path); err != nil {
				t.Fatalf("Preload left %q unloaded: %v", stations[i].path, err)
			}
		}
	})
	engine.Steps(3)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine, demo.pbr
}

// frame is what one step sent the backend: its passes, its draws rebased onto
// those passes, and its storage-buffer bindings.
type frame struct {
	backend  *headless.Backend
	passes   []gfx.PassDesc
	draws    []headless.DrawCall
	bindings []headless.BufferBinding
}

// step takes one more step and keeps what it sent the backend.
func step(t *testing.T, engine *headless.Engine) frame {
	t.Helper()
	backend := engine.Backend()
	passes, draws, bindings := len(backend.Passes), len(backend.Draws), len(backend.Buffers)
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	f := frame{
		backend:  backend,
		passes:   backend.Passes[passes:],
		draws:    slices.Clone(backend.Draws[draws:]),
		bindings: backend.Buffers[bindings:],
	}
	// Draws index Passes across the whole run; rebase them onto this step's.
	for i := range f.draws {
		f.draws[i].Pass -= passes
	}
	return f
}

// forwardLabel is the one pass the camera emits. It declares no passes at all,
// so this is its default forward pass, labelled by its id and its tag.
var forwardLabel = "scene.camera" + strconv.Itoa(int(CameraMain)) + ".forward"

// sceneDraws is the frame's one scene pass and the draws scene made in it,
// in the order they reached the backend. canvas's clear and HUD passes are
// either side of it.
func (f frame) sceneDraws(t *testing.T) (gfx.PassDesc, []headless.DrawCall) {
	t.Helper()
	pass := -1
	for i := range f.passes {
		if f.passes[i].Label != forwardLabel {
			continue
		}
		if pass >= 0 {
			t.Fatalf("the frame began %q twice, want the camera's one default pass", forwardLabel)
		}
		pass = i
	}
	if pass < 0 {
		t.Fatalf("the frame began no pass %q", forwardLabel)
	}
	var draws []headless.DrawCall
	for _, draw := range f.draws {
		if draw.Pass == pass && f.backend.IsScenePipeline(draw.Pipeline) {
			draws = append(draws, draw)
		}
	}
	return f.passes[pass], draws
}

// blended reports whether a draw's pipeline blends, which is how glTF's
// alphaMode BLEND reaches the backend.
func (f frame) blended(draw headless.DrawCall) bool {
	return f.backend.PipelineOf(draw.Pipeline).State.Blend == gfx.BlendAlpha
}

// Every station's model becomes resident and every one of its primitives
// becomes an instance. This is the number the whole demo rests on: a model that
// silently loaded half of itself would still render a picture, and only a
// spelled-out count catches it. Nothing is culled at the reference pose, so the
// pass packs exactly that many instances and draws each of them once.
func TestEveryStationBecomesResidentAndEveryPrimitiveIsDrawn(t *testing.T) {
	engine, demo := run(t)
	if demo.ResidentCount() != len(stations) {
		t.Fatalf("%d of %d stations resident", demo.ResidentCount(), len(stations))
	}
	f := step(t, engine)
	_, draws := f.sceneDraws(t)
	if packed := len(f.sceneInstances(t)); packed != Instances {
		t.Errorf("packed %d instances, want %d", packed, Instances)
	}
	drawn := 0
	for _, draw := range draws {
		drawn += draw.Instances
	}
	if drawn != Instances {
		t.Errorf("drew %d instances, want %d", drawn, Instances)
	}
	// Nothing here is spawned as an instanced crowd, but equal keys share a
	// Batch, and a Batch is one draw.
	if len(draws) != SceneDraws {
		t.Errorf("made %d draws for %d instances, want %d", len(draws), Instances, SceneDraws)
	}
}

// The opaque and alpha-masked draws come first and the blended ones last. MASK
// is fixed-function identical to OPAQUE plus a shader discard, so the three
// cutoff plates batch with the opaque geometry rather than drawing alone.
// Opaque Batches are instanced; a blended instance is a draw of its own.
func TestTheOpaqueDrawsComeFirstAndTheBlendedOnesDrawAlone(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	_, draws := f.sceneDraws(t)
	first := len(draws) - blendDraws
	for i, draw := range draws {
		if f.blended(draw) != (i >= first) {
			t.Fatalf("draw %d of %d blends %v; want the last %d, and only those, blended",
				i, len(draws), f.blended(draw), blendDraws)
		}
		if i >= first && draw.Instances != 1 {
			t.Errorf("blended draw %d packs %d instances, want one each", i, draw.Instances)
		}
	}
}

// copyEpsilon is how close two translations' difference has to be to
// alphaSecondCopy for them to be one primitive in both copies. It is float32
// rounding of a sum of a few units, and the two blended primitives stand far
// further apart than this.
const copyEpsilon = 1e-3

// blendDraws is how many draws blend: the alpha model's two BLEND primitives,
// once per copy.
const blendDraws = 2 * BlendPrimitives

// The blended draws are sorted back to front, and the two copies of the alpha
// model interleave rather than group.
//
// This is why the station is drawn twice. Both copies contribute the same two
// primitives - TestBlendMesh and DecalBlendMesh, both MatBlend - so the four
// draws hold exactly two meshes between them. Sorted by a material or mesh key
// those four come out grouped, aa bb; sorted back to front by depth they come
// out interleaved, abab, because the whole of the far copy is behind the whole
// of the near one. One copy could not tell the two orders apart.
//
// A draw names no mesh the backend can compare, so a primitive is told by where
// it stands: the far copy is the near one moved by alphaSecondCopy and nothing
// else, so the same primitive in the two copies stands exactly that far apart,
// and two different primitives do not.
func TestTheBlendedDrawsAreBackToFront(t *testing.T) {
	engine, demo := run(t)
	f := step(t, engine)
	_, draws := f.sceneDraws(t)
	blend := draws[len(draws)-blendDraws:]
	instances := f.sceneInstances(t)

	var at [blendDraws]m.Vec3
	for i, draw := range blend {
		at[i] = instances[draw.FirstInstance].translation()
	}
	samePrimitive := func(far, near int) bool {
		return at[far].Sub(at[near]).Distance(alphaSecondCopy) < copyEpsilon
	}
	if !samePrimitive(0, 2) || !samePrimitive(1, 3) || samePrimitive(0, 1) {
		t.Fatalf("the blended draws stand at %v; a depth sort interleaves the two copies, "+
			"the far copy's two primitives and then the near copy's in the same order, "+
			"and a key sort groups each primitive's two copies", at)
	}

	// And positively: each draw's instance is no nearer the eye than the one
	// before it. The sort key is view-space distance to the bounding sphere's
	// centre, and each of these primitives is centred on the node it hangs off,
	// so its world translation stands for that centre.
	eye := demo.eye()
	previous := float32(math.Inf(1))
	for i, draw := range blend {
		distance := instances[draw.FirstInstance].translation().Distance(eye)
		if distance > previous {
			t.Fatalf("blended draw %d is %.2f from the eye, behind the %.2f before it",
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
// the file's own nodes and not from the Transform an Entity is given.
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

// Twenty-one lights are declared, sixteen are packed, and nothing is reported.
// The silence is the contract: a seventeenth light is dynamic and camera-shaped,
// so there is no natural moment to report at.
func TestTheCapPacksSixteenOfTwentyOneAndSaysNothing(t *testing.T) {
	engine, demo := run(t)
	if demo.declared != DeclaredLights {
		t.Fatalf("the demo declared %d lights, want %d", demo.declared, DeclaredLights)
	}
	if packed := step(t, engine).packedLights(t); len(packed) != MaxLights {
		t.Fatalf("the pass packed %d lights, want the cap of %d", len(packed), MaxLights)
	}
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("dropping %d lights reported %d errors, first: %v",
			DeclaredLights-MaxLights, len(errs), errs[0])
	}
}

// The sixteen kept at the reference pose are the fill, the two spots, the
// station's own eight and the five rim lamps; the five deep lamps are the five
// dropped. Which is which is only visible in the packed light array itself:
// the drop is silent everywhere else.
//
// It is also the test that pins lampSystem's spawn order to the order scene's
// Query offers lights in: fifteen of the twenty-one score zero here, so which
// ten of them are kept is decided by offering order alone.
func TestTheReferencePoseKeepsTheRimLampsAndDropsTheDeepOnes(t *testing.T) {
	engine, _ := run(t)
	packed := step(t, engine).packedLights(t)
	if len(packed) != MaxLights {
		t.Fatalf("the frame block holds %d lights, want %d", len(packed), MaxLights)
	}

	// The fill light leaves Range at zero, so its packed 1/range^4 is zero and
	// it has no sphere to be culled by. It is here at every pose.
	if !containsPosition(packed, fillPosition) {
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

// fillPosition is where the fill light stands.
var fillPosition = m.Vec3{X: 0, Y: fillHeight, Z: fillDepth}

// Turn the camera round and the selection follows it: the deep lamps that scored
// nothing from the front score from behind, and take the places of lights that
// score nothing from there. The count never moves off the cap and nothing is
// reported, which is the whole of "dynamic and camera-shaped".
//
// The camera is turned the way a player turns it, by holding the left arrow
// for half a turn.
func TestTheCapSelectionFollowsTheCamera(t *testing.T) {
	engine, demo := run(t)
	front := step(t, engine).packedLights(t)

	halfTurn := int(math.Round(math.Pi / float64(orbitSpeed*fixedStep)))
	engine.Input(input.KeyChange(input.KeyLeft, 0, true))
	engine.Steps(halfTurn)
	engine.Input(input.KeyChange(input.KeyLeft, 0, false))
	engine.Steps(1)
	if turned := float64(demo.azimuth - startAzimuth); math.Abs(math.Abs(turned)-math.Pi) > 0.1 {
		t.Fatalf("holding left for %d steps turned the camera %.2f radians, want half a turn",
			halfTurn, turned)
	}
	behind := step(t, engine).packedLights(t)

	if len(behind) != MaxLights {
		t.Fatalf("from behind the pass packed %d lights, want the cap of %d", len(behind), MaxLights)
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
	if !containsPosition(behind, fillPosition) {
		t.Error("the infinite-range fill light was dropped from behind")
	}
}

// The plinths and the ground are the frame's only non-uniform bases, and they
// are all the demo has: a slab is the unit box under a flattened per-axis
// Scale, and the ground the unit quad under a wide one. Every Model Entity's
// own basis is a uniform scale and a rotation, which is what makes the plinths
// the rotated non-uniform bases the bundled PBR shades here.
func TestOnlyThePlinthsAndTheGroundArePackedNonUniform(t *testing.T) {
	engine, _ := run(t)
	instances := step(t, engine).sceneInstances(t)
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

// Every frame at a given pose is the same frame. The demo moves nothing on its
// own, which is what lets the reference screenshot be retaken by launching the
// demo and capturing it rather than by hitting a step number.
func TestTheReferencePoseIsTheSameFrameAtEveryStep(t *testing.T) {
	engine, demo := run(t)
	before := step(t, engine)
	_, beforeDraws := before.sceneDraws(t)
	beforeInstances := slices.Clone(before.sceneInstances(t))
	beforeLights := before.packedLights(t)
	at := demo.step

	engine.Steps(36)
	after := step(t, engine)
	if demo.step == at {
		t.Fatal("the step counter did not advance")
	}
	_, afterDraws := after.sceneDraws(t)
	if len(beforeDraws) != len(afterDraws) {
		t.Fatalf("the frame made %d scene draws and then %d", len(beforeDraws), len(afterDraws))
	}
	for i := range beforeDraws {
		// Pass is an index into each step's own passes, and is the same
		// pass by construction; everything else is the draw.
		b, a := beforeDraws[i], afterDraws[i]
		b.Pass, a.Pass = 0, 0
		if b != a {
			t.Fatalf("draw %d was %+v and is now %+v", i, b, a)
		}
	}
	if !slices.Equal(beforeInstances, after.sceneInstances(t)) {
		t.Error("the packed instances changed while nothing moved")
	}
	if !samePositions(beforeLights, after.packedLights(t)) {
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

// The frame is presented, not only drawn: a frame whose draws all reach the
// backend and which is never shown is not a passing run.
func TestTheFrameIsPresented(t *testing.T) {
	engine, _ := run(t)
	presents := engine.Backend().Presents
	step(t, engine)
	if engine.Backend().Presents <= presents {
		t.Error("the backend was not asked to present the step")
	}
}

// The demo leaves every default unwritten. Its camera declares no passes, so
// the one pass it emits is the default forward pass, which keeps the colour
// canvas cleared beneath it and clears depth; and the fill light leaves Range
// at zero, which packs as an invRange4 of zero.
func TestTheDemoLeavesTheDefaultsUnwritten(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	pass, _ := f.sceneDraws(t)
	if pass.Load == gfx.LoadClear {
		t.Error("the camera's pass clears colour; the default pass keeps the backdrop canvas cleared")
	}
	if pass.DepthLoad != gfx.LoadClear {
		t.Errorf("the camera's pass loads depth %v; the default pass clears it", pass.DepthLoad)
	}
	fill := false
	for _, light := range f.packedLights(t) {
		if light.position.Distance(fillPosition) < positionEpsilon {
			fill = light.invRange4 == 0
		}
	}
	if !fill {
		t.Error("the fill light did not pack an infinite range; it means to take glTF's own default Range of zero")
	}
}

// Reading back what the pass packed.
//
// Two of model's storage records are decoded here, and they are decoded rather
// than asked for because there is nothing to ask: the cap's own selection and
// an instance's flags are visible nowhere else in the public surface. Both
// layouts are the ones builtin/model/frame.wgsl and instance.wgsl fix, and
// the light array is anchored at the end of its record so that a field added
// ahead of it does not silently shift what is read:
//
//	SceneLight    48 bytes: position vec3, 1/range^4, direction vec3, spotScale,
//	                        colour vec3, spotOffset
//	SceneFrame             ends with lightCount, three words of padding and
//	                        lights: array<SceneLight, 16>
//	SceneInstance 64 bytes: three rows of the 4x3 world matrix, an animation
//	                        offset, the flags, a joint and the mesh
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
func (f frame) packedLights(t *testing.T) []packedLight {
	t.Helper()
	block := f.sceneBytes(t, 0, 0)
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
// own range of the frame's instance arena, which is what makes a draw's
// FirstInstance an index into it.
func (f frame) sceneInstances(t *testing.T) []packedInstance {
	t.Helper()
	data := f.sceneBytes(t, 0, 1)
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

// sceneBytes is the bytes the step bound at one group and binding for a
// pipeline built from the bundled scene shader. Every scene draw in the one
// pass binds the same range of both of these, so any of the step's bindings
// will do; filtering on the pipeline is what keeps canvas's own bindings out,
// since gfx labels every pipeline alike and only the shader behind it differs.
func (f frame) sceneBytes(t *testing.T, group, binding int) []byte {
	t.Helper()
	for i := len(f.bindings) - 1; i >= 0; i-- {
		bound := f.bindings[i]
		if bound.Group != group || bound.Binding != binding || !f.backend.IsScenePipeline(bound.Pipeline) {
			continue
		}
		if data := f.backend.BoundBytes(bound); data != nil {
			return data
		}
		t.Fatalf("the binding at group %d binding %d named an unbaked buffer", group, binding)
	}
	t.Fatalf("no scene draw bound group %d binding %d this step", group, binding)
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
