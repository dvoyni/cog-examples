package main

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// referenceStep is the step the assertions below are written against: two
// seconds of demo time, inside the first beacon generation and the first ridge
// resolution, so a run to here is a steady frame with no swap in it. Demo time
// is accumulated fixed steps, so this is the same frame on every machine.
const referenceStep = 120

// demoShaderLayout is what material.go's WGSL declares, stated here rather than
// reflected: the headless backend has no shader front end, and the point of the
// assertions below is that this list is shorter than what scene offers - a
// caller material may declare fewer bindings than scene binds, and must never
// declare more. sceneFrame is the one binding model.PbrPath brings in, and
// sceneInstances the one the material declares itself.
var demoShaderLayout = gfx.ShaderLayout{Resources: []gfx.ShaderResource{
	{Name: "sceneFrame", Kind: gfx.ResourceStorageBuffer, Group: 0, Binding: 0},
	{Name: "sceneInstances", Kind: gfx.ResourceStorageBuffer, Group: 0, Binding: 1},
}}

// textShaderLabel is the label gfx gives a shader built from inline source,
// which is how a draw with the demo's material is told from a bundled one.
const textShaderLabel = "gfx.shader"

// start brings up an engine with the demo in it, at step zero. The engine
// composes ecs and scene itself, so the demo's plugin is all a test adds.
func start(t *testing.T, extra ...kernel.Plugin) (*Procedural, *headless.Engine) {
	t.Helper()
	demo := New()
	engine := headless.New(t, append([]kernel.Plugin{demo}, extra...)...)
	engine.Backend().TextShaderLayout = demoShaderLayout
	return demo.state, engine
}

// run drives the demo n steps and fails if the engine reported anything. Every
// test but the beacon swap's uses it: a demo that reports in a steady frame has
// a bug, and the swap's one report is the only one this demo means to provoke.
func run(t *testing.T, n int, extra ...kernel.Plugin) (*Procedural, *headless.Engine) {
	t.Helper()
	demo, engine := start(t, extra...)
	engine.Steps(n)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return demo, engine
}

// frame is what one step sent the backend: the passes it began, the draws it
// made and the buffers they bound, with each draw's Pass rebased onto this
// step's passes.
type frame struct {
	backend *headless.Backend
	passes  []gfx.PassDesc
	draws   []headless.DrawCall
	buffers []headless.BufferBinding
}

// step takes one more step and keeps what that step reached the backend with.
func step(engine *headless.Engine) frame {
	backend := engine.Backend()
	passes, draws, buffers := len(backend.Passes), len(backend.Draws), len(backend.Buffers)
	engine.Steps(1)
	f := frame{
		backend: backend,
		passes:  slices.Clone(backend.Passes[passes:]),
		draws:   slices.Clone(backend.Draws[draws:]),
		buffers: slices.Clone(backend.Buffers[buffers:]),
	}
	for i := range f.draws {
		f.draws[i].Pass -= passes
	}
	return f
}

// referenceFrame drives the demo to the step before referenceStep and keeps
// what referenceStep itself drew.
func referenceFrame(t *testing.T) (*Procedural, frame) {
	t.Helper()
	demo, engine := run(t, referenceStep-1)
	f := step(engine)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the reference step reported %d errors, first: %v", len(errs), errs[0])
	}
	return demo, f
}

// cameraPass is the index of the camera's one pass among the frame's, which
// scene labels by the camera id and the forward tag. canvas's HUD begins passes
// of its own, and a draw in one of those is not scene's.
func (f frame) cameraPass(t *testing.T) int {
	t.Helper()
	prefix := "scene.camera" + strconv.Itoa(int(CameraMain)) + "."
	found := -1
	for i, pass := range f.passes {
		if strings.HasPrefix(pass.Label, prefix) {
			if found >= 0 {
				t.Fatalf("the camera began a second pass, %q after %q", pass.Label, f.passes[found].Label)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("no pass is the camera's; the frame began %d passes", len(f.passes))
	}
	return found
}

// custom reports whether a draw took the demo's own material.
func (f frame) custom(draw headless.DrawCall) bool {
	return f.backend.ShaderPath(f.backend.PipelineOf(draw.Pipeline).Shader) == textShaderLabel
}

// A steady frame is one pass of five draws, each of one instance: the ground and
// the reference sphere through the bundled PBR, the ridge, the ribbon and the
// beacon through the demo's material. The stray is the fourth Entity carrying
// a live ref and that material, and the frustum throws it away; the ghost holds
// the zero ref and draws nothing without a word.
func TestTheSteadyFrameDrawsFiveAndCullsTheStray(t *testing.T) {
	_, f := referenceFrame(t)
	pass := f.cameraPass(t)
	bundled, custom := 0, 0
	type shape struct {
		count   int
		indexed bool
	}
	var customShapes []shape
	for _, draw := range f.draws {
		if draw.Pass != pass {
			continue
		}
		if draw.Instances != 1 {
			t.Errorf("a draw packed %d instances, want every Batch to draw one", draw.Instances)
		}
		switch {
		case f.backend.IsScenePipeline(draw.Pipeline):
			bundled += draw.Instances
		case f.custom(draw):
			custom += draw.Instances
			customShapes = append(customShapes, shape{draw.Count, draw.Indexed})
		default:
			t.Errorf("the camera's pass drew with a pipeline that is neither the bundled PBR nor the demo's")
		}
	}
	if bundled != BundledDraws || custom != CustomDraws {
		t.Errorf("the camera drew %d bundled and %d custom instances, want %d and %d - the stray culled",
			bundled, custom, BundledDraws, CustomDraws)
	}
	// The three custom draws are the three meshes, told apart by their size:
	// the ridge and the beacon indexed, the ribbon a strip with no indices at
	// all, which is the non-indexed half of the mesh contract reaching the GPU.
	want := []shape{
		{ridgeCells * ridgeCells * 6, true},
		{2 * (ribbonSegments + 1), false},
		{24, true},
	}
	for _, w := range want {
		if !slices.Contains(customShapes, w) {
			t.Errorf("no custom draw of %d vertices, indexed %v; the custom draws were %+v", w.count, w.indexed, customShapes)
		}
	}
}

// The cull is a claim about a named object, not a count: the camera's own
// frustum, rebuilt from the Camera Component through scene.ViewProjection,
// rejects the stray beacon's sphere and accepts the beacon's own. Both are the
// same mesh with the same declared bounds, so the only thing separating them is
// where they stand. The HUD prints the stray's half of this.
func TestTheCamerasFrustumSeparatesTheBeaconFromTheStray(t *testing.T) {
	demo, _ := run(t, referenceStep)
	view := &gfx.Viewport{WindowWidth: headless.WindowWidth, WindowHeight: headless.WindowHeight}
	if !onScreen(camera(), demo.eye(), view, beaconPosition, beaconScale) {
		t.Error("the camera's frustum rejects the beacon, which is on screen")
	}
	if onScreen(camera(), demo.eye(), view, strayPosition, beaconScale) {
		t.Errorf("the camera's frustum contains the stray at %v, which sits behind the camera", strayPosition)
	}
	if demo.strayVisible {
		t.Error("the HUD says the stray is on screen")
	}
}

// census answers every Mesh Entity in the world, with whether it carries a
// Material: the Components themselves, since the backend sees only what
// survived them.
type census struct {
	meshes    map[ecs.Entity]scene.Mesh
	materials map[ecs.Entity]bool
}

type (
	censusCmd     kernel.Command[censusRequest, census]
	censusRequest struct{}
	censusQuery   struct{ Draw scene.Mesh }
)

// censusPlugin is a test's window onto the world: one command, answered by a
// System reading the Mesh and Material Stores.
type censusPlugin struct{}

func (censusPlugin) Name() kernel.PluginName { return "procedural-census" }

func (censusPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name, Name}
}

func (censusPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[censusCmd](ecs.ToExecute[censusRequest, census](registrar, takeCensus))
	return nil
}

func takeCensus(q *ecs.Query[censusQuery], materials *ecs.Get[scene.Material], answer *ecs.Resp[census]) {
	c := census{meshes: map[ecs.Entity]scene.Mesh{}, materials: map[ecs.Entity]bool{}}
	for e, it := range q.All() {
		c.meshes[e] = it.Draw
		_, c.materials[e] = materials.Of(e)
	}
	answer.Set(c)
}

func takeCensusOf(t *testing.T, engine *headless.Engine) census {
	t.Helper()
	c := engine.Executioner().ExecuteCommand[censusCmd](censusRequest{})
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the census reported %d errors, first: %v", len(errs), errs[0])
	}
	return c
}

// A custom vertex layout gets no baked bounding sphere - scene cannot find
// POSITION in bytes it has never seen a layout for - so each such Mesh has to
// say how it is culled, and has to carry a Material, which the bundled PBR
// cannot stand in for. The ridge, the beacon, the stray and the ghost declare a
// sphere; the ribbon says never-cull outright. A Mesh that said neither would be
// silently uncullable, which is the failure this pins.
func TestEveryCustomLayoutMeshSaysHowItIsCulled(t *testing.T) {
	demo, engine := run(t, referenceStep, censusPlugin{})
	c := takeCensusOf(t, engine)
	pieces := []ecs.Entity{demo.ridgeEntity, demo.ribbonEntity, demo.beaconEntity, demo.strayEntity, demo.ghostEntity}
	if want := BundledDraws + len(pieces); len(c.meshes) != want {
		t.Errorf("the world holds %d Mesh Entities, want %d", len(c.meshes), want)
	}
	for _, e := range pieces {
		mesh, ok := c.meshes[e]
		if !ok {
			t.Errorf("%v carries no Mesh", e)
			continue
		}
		if !c.materials[e] {
			t.Errorf("%v draws a custom layout with no Material; scene reports and skips it", e)
		}
		if !mesh.NeverCull && mesh.Bounds.W == 0 {
			t.Errorf("%v declares neither Bounds nor NeverCull, so it is never culled and nobody said so", e)
		}
	}
	for e := range c.meshes {
		if !slices.Contains(pieces, e) && c.materials[e] {
			t.Errorf("%v is a bundled-PBR shape and carries a Material", e)
		}
	}
}

// The ribbon is the never-cull case and the ridge the explicit-sphere one, and
// the demo means each to be the one it is: swapping them would still pass the
// test above.
func TestTheRibbonNeverCullsAndTheRidgeDeclaresItsSphere(t *testing.T) {
	demo, engine := run(t, referenceStep, censusPlugin{})
	c := takeCensusOf(t, engine)
	if ribbon := c.meshes[demo.ribbonEntity]; !ribbon.NeverCull || ribbon.Ref != demo.ribbon {
		t.Errorf("the ribbon's Mesh is %+v, want NeverCull on this frame's ref", ribbon)
	}
	if ridge := c.meshes[demo.ridgeEntity]; ridge.NeverCull || ridge.Bounds.W != ridgeBounds {
		t.Errorf("the ridge declares NeverCull %v and radius %v, want a radius of %v",
			ridge.NeverCull, ridge.Bounds.W, ridgeBounds)
	}
}

// The ribbon lives one frame: each step bakes a fresh ref and releases the last
// one, where the ridge keeps its ref for the demo's life. The fresh bake takes
// back the slot the release freed, so the id holds still while the generation
// counts frames - and a ref from one frame ago names a mesh that is gone.
func TestTheRibbonIsBakedFreshEveryFrame(t *testing.T) {
	demo, engine := run(t, referenceStep)
	ribbon, ridge := demo.ribbon, demo.ridge
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the step reported %d errors, first: %v", len(errs), errs[0])
	}
	if demo.ribbon == ribbon {
		t.Error("the ribbon kept its ref across a step")
	}
	if demo.ribbon.ID() != ribbon.ID() || demo.ribbon.Generation() <= ribbon.Generation() {
		t.Errorf("the ribbon went from %d generation %d to %d generation %d, want its own slot back, a generation on",
			ribbon.ID(), ribbon.Generation(), demo.ribbon.ID(), demo.ribbon.Generation())
	}
	if demo.ridge != ridge {
		t.Error("the ridge's ref changed across a step")
	}
}

// UpdateMesh replaces a durable mesh's geometry wholesale at any size while
// keeping its ref and its id. The ridge changes resolution mid-run, and it is
// still the same mesh afterwards: there is no capacity concept, so growth is a
// re-bake rather than a new mesh. The backend draws the new size.
func TestUpdateMeshResizesTheRidgeWithoutChangingItsRef(t *testing.T) {
	demo, engine := start(t)
	engine.Steps(ridgeResizePeriod - 10)
	before, beforeCells := demo.ridge, demo.ridgeCellCount
	engine.Steps(19)
	f := step(engine)
	if demo.ridge != before {
		t.Errorf("the ridge's ref changed across a resize: %+v against %+v", demo.ridge, before)
	}
	if demo.ridgeCellCount == beforeCells {
		t.Fatalf("the ridge is still %d cells across after the resize step", beforeCells)
	}
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the resize reported %d errors, first: %v", len(errs), errs[0])
	}
	want := demo.ridgeCellCount * demo.ridgeCellCount * 6
	for _, draw := range f.draws {
		if f.custom(draw) && draw.Indexed && draw.Count == want {
			return
		}
	}
	t.Errorf("no custom draw of the resized ridge's %d indices reached the backend", want)
}

// A release makes the old ref stale at once. The demo hands that ref to the
// ghost on the frame it released it: scene reports it unavailable and skips
// its Batch, so the frame still draws three custom instances, and the mesh that
// now holds that slot is not drawn in its place. The frame after, the ghost is
// back on the zero ref and nothing is reported.
func TestABeaconSwapSkipsADrawOfTheReleasedRef(t *testing.T) {
	demo, engine := start(t)
	engine.Steps(beaconPeriod - 1)
	before := demo.beacon
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the steady frames reported %d errors, first: %v", len(errs), errs[0])
	}

	f := step(engine)
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
	var unavailable model.ErrMeshUnavailable
	if !errors.As(errs[0], &unavailable) {
		t.Fatalf("the swap reported %v, want a model.ErrMeshUnavailable", errs[0])
	}
	if unavailable.Mesh != before.ID() {
		t.Errorf("the report names mesh %d, want the released %d", unavailable.Mesh, before.ID())
	}
	pass := f.cameraPass(t)
	custom := 0
	for _, draw := range f.draws {
		if draw.Pass == pass && f.custom(draw) {
			custom += draw.Instances
		}
	}
	if custom != CustomDraws {
		t.Errorf("the swap frame drew %d custom instances, want %d - the ghost drew nothing", custom, CustomDraws)
	}

	engine.Steps(1)
	if errs := engine.Errors(); len(errs) != 1 {
		t.Errorf("the frame after the swap reported again: %v", errs[1:])
	}
	if demo.staleID.Load() != 0 {
		t.Error("the ghost still names the released ref a frame later")
	}
}

// The demo's material declares two of the bindings scene offers every draw, and
// that is the constraint the whole demo exists to prove livable: gfx binds what
// reflection reports and ignores the rest, so a binding a shader does not
// declare costs nothing, while a binding it declares and nobody binds costs the
// draw, reported as gfx.ErrStorageBufferUnsupplied.
//
// So the custom draws reached the backend, bound sceneFrame and sceneInstances
// - the include resolved through model's mount, which is how sceneFrame is
// declared at all - and bound nothing else. Every scene draw, bundled or
// custom alike, binds those two: scene binds what describes the scene, never a
// material's numbers, so the two kinds of draw differ only in their pipeline.
func TestTheCustomMaterialBindsOnlyWhatItDeclares(t *testing.T) {
	_, f := referenceFrame(t)
	custom := map[gfx.PipelineID]bool{}
	bundled := map[gfx.PipelineID]bool{}
	for _, draw := range f.draws {
		switch {
		case f.custom(draw):
			custom[draw.Pipeline] = true
		case f.backend.IsScenePipeline(draw.Pipeline):
			bundled[draw.Pipeline] = true
		}
	}
	if len(custom) == 0 {
		t.Fatal("nothing drew with the demo's material")
	}
	// Every Entity holds the one Material value, so the custom draws split
	// into pipelines by topology alone: the ridge and the beacon are triangle
	// lists, and the ribbon the one strip.
	if len(custom) != 2 {
		t.Errorf("the custom draws used %d pipelines, want the shared material's two topologies", len(custom))
	}

	declared := map[headless.BufferBinding]bool{}
	for _, want := range demoShaderLayout.Resources {
		declared[headless.BufferBinding{Group: want.Group, Binding: want.Binding}] = true
	}
	bound := map[headless.BufferBinding]bool{}
	for _, binding := range f.buffers {
		slot := headless.BufferBinding{Group: binding.Group, Binding: binding.Binding}
		switch {
		case custom[binding.Pipeline]:
			bound[slot] = true
			if !declared[slot] {
				t.Errorf("a custom-material draw bound %d/%d, which the material never declared",
					binding.Group, binding.Binding)
			}
		case bundled[binding.Pipeline] && !declared[slot]:
			t.Errorf("a bundled draw bound %d/%d beyond sceneFrame and sceneInstances",
				binding.Group, binding.Binding)
		}
	}
	for _, want := range demoShaderLayout.Resources {
		if !bound[headless.BufferBinding{Group: want.Group, Binding: want.Binding}] {
			t.Errorf("the custom-material draws never bound %s", want.Name)
		}
	}
}

// Scene binds a pass's whole slice of instances as one range, and each draw
// reads its own records from it by first instance. The record is 64 bytes: the
// one carrying the world rows the material's vertex stage dots against. A
// range that stopped agreeing with what the pass drew would be a draw reading
// somebody else's matrix.
func TestTheInstanceRangeIsOneRecordPerDrawnInstance(t *testing.T) {
	_, f := referenceFrame(t)
	const instanceSize = 64
	pass := f.cameraPass(t)
	drawn, highest := 0, 0
	for _, draw := range f.draws {
		if draw.Pass == pass && (f.custom(draw) || f.backend.IsScenePipeline(draw.Pipeline)) {
			drawn += draw.Instances
			highest = max(highest, draw.FirstInstance+draw.Instances)
		}
	}
	if highest != drawn {
		t.Errorf("the draws reach instance %d of the %d drawn, so two share a record or one is skipped",
			highest, drawn)
	}
	seen := 0
	for _, binding := range f.buffers {
		if binding.Group != 0 || binding.Binding != 1 {
			continue
		}
		seen++
		if binding.Size != drawn*instanceSize {
			t.Fatalf("an instance range is %d bytes, want %d - %d instances of %d",
				binding.Size, drawn*instanceSize, drawn, instanceSize)
		}
	}
	if seen == 0 {
		t.Error("no instance range was bound")
	}
}

// Demo time is accumulated fixed steps, never wall clock, so the same step
// number is the same frame: two engines driven the same number of steps send
// the backend the same passes and the same draws. Geometry rebuilt every frame
// does not change that - it is rebuilt from the step counter.
func TestTheSameStepIsTheSameFrame(t *testing.T) {
	_, first := referenceFrame(t)
	_, second := referenceFrame(t)
	labels := func(f frame) []string {
		out := make([]string, len(f.passes))
		for i := range f.passes {
			out[i] = f.passes[i].Label
		}
		return out
	}
	if a, b := labels(first), labels(second); !slices.Equal(a, b) {
		t.Fatalf("two runs of %d steps began passes %v and %v", referenceStep, a, b)
	}
	if len(first.draws) != len(second.draws) {
		t.Fatalf("two runs of %d steps made %d and %d draws", referenceStep, len(first.draws), len(second.draws))
	}
	for i := range first.draws {
		a, b := first.draws[i], second.draws[i]
		a.Pipeline, b.Pipeline = 0, 0
		if a != b || first.custom(first.draws[i]) != second.custom(second.draws[i]) {
			t.Errorf("draw %d differs between runs: %+v against %+v", i, first.draws[i], second.draws[i])
		}
	}
}

// The frame reaches the GPU: geometry was uploaded, the draws were made, and
// the frame was presented. It is here so a frame that decides correctly and
// then never renders is not a passing test.
func TestTheFrameReachesTheBackend(t *testing.T) {
	_, engine := run(t, referenceStep)
	backend := engine.Backend()
	if backend.Presents == 0 {
		t.Error("the backend was never asked to present")
	}
	if backend.Bakes == 0 {
		t.Error("the backend baked no buffers, so no geometry was ever uploaded")
	}
	if len(backend.Draws) == 0 {
		t.Error("the backend was asked to draw nothing")
	}
}
