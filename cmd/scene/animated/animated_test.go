package main

import (
	"errors"
	"slices"
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
)

// run starts the demo headless over the vendored asset set with every file
// preloaded, and steps it twice: the first step spawns the row and keys it,
// and the second draws it with every file resident, which is the frame the
// reference screenshot was taken of.
//
// Preload is the lever a game pulls to move a load's hitch - MorphStressTest
// reads half a megabyte of float deltas to pack its 18 KiB of them - and here
// it is what makes the second step the steady frame rather than a frame that
// happens to land after the loads.
//
// The demo provokes no report, so the harness fails on any: a test that
// tolerated errors would stop noticing the ones nobody meant to provoke - a
// missing clip name, a typo'd node, a texture that failed to decode.
func run(t *testing.T) (*headless.Engine, *Demo) {
	t.Helper()
	demo := New()
	engine := headless.New(t, demo)
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		for _, path := range ModelPaths {
			la.Preload(path)
			if err := la.State(path); err != nil {
				t.Fatalf("Preload left %q unloaded: %v", path, err)
			}
		}
	})
	engine.Steps(2)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	if demo.demo.ResidentCount() != len(ModelPaths) {
		t.Fatalf("%d of %d models resident", demo.demo.ResidentCount(), len(ModelPaths))
	}
	return engine, demo.demo
}

// frame is what one step sent the backend: the bundled scene shader's draws,
// and the storage buffers the frame bound through it.
type frame struct {
	draws    []headless.DrawCall
	bindings []headless.BufferBinding
}

// instances is how many instances the frame's scene draws drew in total.
func (f frame) instances() int {
	n := 0
	for _, draw := range f.draws {
		n += draw.Instances
	}
	return n
}

// step takes one more step and keeps what it sent the backend through the
// bundled scene shader. The backend accumulates every frame since the engine
// started, so the step's own are what is past the lengths before it; canvas's
// HUD draws are filtered out by pipeline.
func step(t *testing.T, engine *headless.Engine) frame {
	t.Helper()
	backend := engine.Backend()
	draws, bindings := len(backend.Draws), len(backend.Buffers)
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	var f frame
	for _, draw := range backend.Draws[draws:] {
		if backend.IsScenePipeline(draw.Pipeline) {
			f.draws = append(f.draws, draw)
		}
	}
	for _, binding := range backend.Buffers[bindings:] {
		if backend.IsScenePipeline(binding.Pipeline) {
			f.bindings = append(f.bindings, binding)
		}
	}
	return f
}

// deviceOf runs one read against the loading half of the facade, which is
// where every per-model query lives because every one of them loads the file
// it names.
func deviceOf[T any](t *testing.T, engine *headless.Engine, read func(model.LookupDeviceAccess) T) T {
	t.Helper()
	var out T
	engine.LookupDevice(func(la model.LookupDeviceAccess) { out = read(la) })
	return out
}

// press holds a key down for one step and lets it go, which is one JustPressed
// edge.
func press(engine *headless.Engine, key input.Key) {
	engine.Input(input.KeyChange(key, 0, true))
	engine.Steps(1)
	engine.Input(input.KeyChange(key, 0, false))
}

// Every file becomes resident and every primitive of every Entity becomes an
// instance. This is the number the whole demo rests on: a model that silently
// loaded half of itself would still render a picture, and only a spelled-out
// count catches it.
//
// Instances rather than draws, because scene batches Entities that share a
// mesh and a material into one instanced draw, so the instance count is the
// one that does not depend on how they batch. The draw count is asserted
// beside it, spelled out the same way: the two foxes are one draw; the nine
// grid cubes and the cap copy's nine are one mesh, so eighteen instances in
// one draw, and the label board another; each morph cube is its own file; the
// two stress copies batch per primitive, which is two draws; and the ground is
// one.
func TestEveryModelBecomesResidentAndEveryPrimitiveIsDrawn(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	if got := f.instances(); got != DrawnInstances {
		t.Errorf("drew %d instances of the scene shader, want %d", got, DrawnInstances)
	}
	const draws = 1 + 2 + 2 + stressPrimitives + 1
	if len(f.draws) != draws {
		t.Errorf("%d draws for %d instances, want %d batched draws", len(f.draws), DrawnInstances, draws)
	}
}

// # The canary assertion
//
// Every draw binds exactly the bindings its variant declares, and nothing is
// left over. This is the failure the whole demo exists to catch: a declared
// binding that is never bound kills the entire frame in a browser with nothing
// logged, and a desktop adapter's hardware limits sit far above the floor where
// it would show.
//
// It is asserted per variant rather than per frame because the failure is per
// draw, and a variant is what a draw declares: a skinned model declares its
// poses and a morph-only one its deltas, and a static prop declares neither. A
// frame-wide "were all seven seen" would pass while a single skinned draw
// left scenePoses unbound.
func TestEveryDrawBindsExactlyWhatItsVariantDeclares(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	backend := engine.Backend()

	seen := map[string]map[[2]int]int{}
	for _, binding := range f.bindings {
		supply := backend.PipelineSupply(binding.Pipeline)
		if seen[supply] == nil {
			seen[supply] = map[[2]int]int{}
		}
		seen[supply][[2]int{binding.Group, binding.Binding}]++
	}
	// The demo draws a skinned walk cycle, morphing cubes and a static ground,
	// so it is also where the variants themselves are exercised rather than
	// only described: three distinct modules, each declaring its own group 2.
	if len(seen) < 3 {
		t.Fatalf("the frame drew %d variants of the bundled shader, want the skinned, the morphed and the static: %v",
			len(seen), seen)
	}
	for supply, bound := range seen {
		declared := headless.SceneVariantResources(supply)
		want := make([][2]int, 0, len(declared))
		for _, resource := range declared {
			if resource.Kind.Base() == gfx.ResourceStorageBuffer {
				want = append(want, [2]int{resource.Group, resource.Binding})
			}
		}
		if len(bound) != len(want) {
			t.Errorf("variant %q bound %d distinct slots, want the %d it declares: %v",
				supply, len(bound), len(want), bound)
			continue
		}
		// Every slot bound the same number of times is what "every draw bound all
		// of them" means without counting draws: the counts can only agree if no
		// draw skipped one.
		for _, slot := range want {
			if bound[slot] == 0 {
				t.Errorf("variant %q never bound group %d binding %d; on the web that is a blank canvas",
					supply, slot[0], slot[1])
				continue
			}
			if bound[slot] != bound[want[0]] {
				t.Errorf("variant %q bound group %d binding %d %d times against %d for %v; some draw skipped it",
					supply, slot[0], slot[1], bound[slot], bound[want[0]], want[0])
			}
		}
	}
}

// The seven storage buffers are also the whole of scene's budget against the
// browser floor of eight, and gfx measures every shader it reflects against
// DefaultLimits rather than against the device. That check runs on the desktop
// too, so a web limit violation fails loudly here first - which is the only
// reason it is worth having a desktop test of a browser property at all.
func TestNoShaderExceedsTheWebLimits(t *testing.T) {
	engine, _ := run(t)
	step(t, engine)
	var exceeded gfx.ErrShaderExceedsWebLimits
	for _, err := range engine.Errors() {
		if errors.As(err, &exceeded) {
			t.Fatalf("%s declares %d %s against the web floor of %d (this device allows %d)",
				exceeded.Shader, exceeded.Declared, exceeded.Limit, exceeded.Floor, exceeded.Device)
		}
	}
}

// The HUD's census is what setup spawned: every Entity it made is counted by
// the Component it draws with, so a spawn that silently failed shows on screen.
func TestTheHUDCountsWhatSetupSpawned(t *testing.T) {
	_, demo := run(t)
	// Two foxes, nine grid cubes, the cap copy, two cubes and two stress
	// copies are Models; all but the still fox animate.
	const models = 2 + InterpClips + 1 + 2 + 2
	want := census{models: models, animations: models - 1, meshes: 1, cameras: 1}
	if demo.census != want {
		t.Errorf("the HUD counts %+v, want %+v", demo.census, want)
	}
	if demo.spawned != models+2 {
		t.Errorf("the HUD says %d Entities were spawned, want %d", demo.spawned, models+2)
	}
}

// # The fox station: baked poses, the crossfade and the rest frame
//
// The rig is the thing no document built in memory reproduces: 24 joints, an
// inverse bind each, every vertex across four influences, and three clips baked
// onto one 60 Hz grid.
func TestTheFoxBakesItsRigAndItsThreeClips(t *testing.T) {
	engine, _ := run(t)
	joints := deviceOf(t, engine, func(la model.LookupDeviceAccess) []string {
		names, _ := la.Joints(foxPath, nil)
		return names
	})
	if len(joints) != FoxJoints {
		t.Errorf("the fox has %d joints, want its %d-bone rig", len(joints), FoxJoints)
	}
	clips := deviceOf(t, engine, func(la model.LookupDeviceAccess) []model.ClipInfo {
		infos, _ := la.Clips(foxPath, nil)
		return infos
	})
	names := map[string]bool{}
	for _, clip := range clips {
		names[clip.Name] = true
		if clip.Duration <= 0 {
			t.Errorf("clip %q lasts %v seconds, want a real length", clip.Name, clip.Duration)
		}
	}
	for _, want := range []string{foxWalk, foxRun, FoxSurvey} {
		if !names[want] {
			t.Errorf("the fox declares no clip %q; it has %v", want, clips)
		}
	}
	// Survey is baked whether or not it is played: pose memory scales with the
	// clips a file carries, not with what is playing, which is the whole point
	// of the query below.
	if len(clips) != 3 {
		t.Errorf("the fox declares %d clips, want its three", len(clips))
	}
}

// FoxJoints is the rig's bone count, spelled out so the demo's claim about the
// asset is an assertion rather than a comment.
const FoxJoints = 24

// Pose memory is joints x rows x 48 bytes, where the rows are the rest frame
// plus every clip's frames at the bake rate. It is the trade the whole design
// makes, so the number it produces is worth pinning to the real rig.
func TestPoseMemoryIsTheRigTimesTheGrid(t *testing.T) {
	engine, demo := run(t)
	bytes := deviceOf(t, engine, func(la model.LookupDeviceAccess) int {
		out, _ := la.PoseBytes(foxPath)
		return out
	})
	const poseRecord = 48
	if bytes%(FoxJoints*poseRecord) != 0 {
		t.Errorf("PoseBytes = %d, which is not a whole number of %d-joint rows",
			bytes, FoxJoints)
	}
	// The rest frame plus Survey, Walk and Run, each sampled at the bake rate
	// with a row at the end for the frame pair to land on.
	rows := 1
	for _, duration := range []float32{3.41667, 0.70833, 1.15833} {
		rows += int(duration*stepsPerSecond) + 2
	}
	if want := FoxJoints * rows * poseRecord; bytes < want-FoxJoints*poseRecord*4 || bytes > want {
		t.Errorf("PoseBytes = %d, want about %d: %d joints x ~%d rows x %d bytes",
			bytes, want, FoxJoints, rows, poseRecord)
	}
	if demo.memory.pose[0] != bytes {
		t.Errorf("the HUD prints %d pose bytes, the facade reports %d",
			demo.memory.pose[0], bytes)
	}
	// A weights-only file bakes no poses at all, which is what makes the total
	// worth printing beside the per-model number.
	if stress := demo.memory.pose[4]; stress != 0 {
		t.Errorf("MorphStressTest baked %d pose bytes; a weights channel makes no joint", stress)
	}
}

// The fox's gait machine, rigged from Fox.glb's own clips, is caught
// mid-crossfade at the reference time: two plays whose weights both matter.
// They reach the fox's Animation as the machine made them, and scene
// normalises across an Entity's plays because the blend is a weighted mean of
// TRS.
func TestTheCrossfadeOffersTwoRealWeightsAtTheReferenceTime(t *testing.T) {
	engine, demo := run(t)
	// Within one step, not exactly: the clock is a step count times 1/60, so
	// the reference time is only representable to the step it lands on.
	if drift := demo.Time() - startTime; drift < -fixedStep || drift > fixedStep {
		t.Fatalf("the demo opens at %v, want the documented %v", demo.Time(), startTime)
	}
	plays := demo.FoxPlays()
	if len(plays) != 2 {
		t.Fatalf("at the reference time the fox plays %+v, want a crossfade of two", plays)
	}
	// Run is the state the last trigger entered, so it is the incoming play
	// and comes first.
	if plays[0].Clip != foxRun || plays[1].Clip != foxWalk {
		t.Errorf("at the reference time the fox plays %s then %s, want Run fading in over Walk",
			plays[0].Clip, plays[1].Clip)
	}
	for _, play := range plays {
		if play.Weight <= 0.05 {
			t.Errorf("at the reference time %s weighs %.3f; "+
				"the reference frame should catch the fade mid-slide, not at an end", play.Clip, play.Weight)
		}
	}
	// The two are a partition of one clip's worth of animation, which is what
	// makes the pair a crossfade rather than two independent layers.
	if total := plays[0].Weight + plays[1].Weight; total < 0.999 || total > 1.001 {
		t.Errorf("the crossfade weights total %v, want 1", total)
	}
	// Both gaits are reached on their own between triggers, so a run of the
	// demo shows a pure walk and a pure run rather than a permanent mixture.
	press(engine, input.KeySpace)
	pure := map[string]bool{}
	for range 2 * gaitDwellSteps {
		engine.Steps(1)
		if plays := demo.FoxPlays(); len(plays) == 1 {
			pure[plays[0].Clip] = true
		}
	}
	if !pure[foxWalk] || !pure[foxRun] {
		t.Errorf("over two dwells the fox is alone in %v, want both Walk and Run", pure)
	}
	// A rewind starts the machine again and lands on the same plays.
	press(engine, input.KeyR)
	if again := demo.FoxPlays(); !slices.Equal(again, plays) {
		t.Errorf("after a rewind the fox plays %+v, want %+v", again, plays)
	}
}

// The demo opens paused, which is what makes the reference screenshot
// reproducible: motion is the subject here, so a capture of a running demo
// would land on whatever step the screenshotter reached.
func TestTheDemoOpensPausedAtTheReferenceTime(t *testing.T) {
	demo := newDemo()
	if !demo.paused {
		t.Error("the demo opens running; reference.png would not be reproducible")
	}
	before := demo.Time()
	demo.advance(nil)
	if demo.Time() != before {
		t.Errorf("a paused step advanced the clock from %v to %v", before, demo.Time())
	}
	demo.paused = false
	demo.advance(nil)
	if want := before + fixedStep; demo.Time() != want {
		t.Errorf("a running step advanced the clock to %v, want %v", demo.Time(), want)
	}
}

// # The interpolation station: degenerate skins, u8 indices and the cap
//
// InterpolationTest has no skins key at all, and nine of its ten nodes are
// steered by a clip. Every one of them becomes a joint, which is the rule that
// makes rigid node animation - a wheel, a lid, a lift - work without an artist
// authoring a skin for it. It is the only asset in the vendored set that shows
// the rule on a file with no skin to fall back on.
func TestTheInterpolationCubesAreDegenerateSingleJointSkins(t *testing.T) {
	engine, _ := run(t)
	joints := deviceOf(t, engine, func(la model.LookupDeviceAccess) []string {
		names, _ := la.Joints(interpPath, nil)
		return names
	})
	if len(joints) != InterpClips {
		t.Fatalf("the file baked %d joints, want one per animated node (%d): %v",
			len(joints), InterpClips, joints)
	}
	// Each joint is the node its own clip steers, and the demo addresses them
	// by those names - so a table that drifted from the file would draw the
	// wrong cube rather than fail.
	named := map[string]bool{}
	for _, joint := range joints {
		named[joint] = true
	}
	for _, row := range interpGrid {
		for _, cell := range row {
			if !named[cell.node] {
				t.Errorf("the grid draws node %q for clip %q, but the file baked no such joint: %v",
					cell.node, cell.clip, joints)
			}
		}
	}
	// The plane is the tenth node and is steered by nothing, so it is not a
	// joint - which is what keeps "animated node" from meaning "every node".
	if len(joints) >= interpPrimitives {
		t.Errorf("%d joints for %d primitives; the unanimated plane should not be a joint",
			len(joints), interpPrimitives)
	}
}

// Every clip the demo names is one the file declares, and every clip the file
// declares is one the demo lays out. A grid short of a clip would render a
// still cube that reads as a bug in the interpolation rather than a gap in a
// table.
func TestTheGridCoversEveryClipTheFileDeclares(t *testing.T) {
	engine, _ := run(t)
	clips := deviceOf(t, engine, func(la model.LookupDeviceAccess) []model.ClipInfo {
		infos, _ := la.Clips(interpPath, nil)
		return infos
	})
	if len(clips) != InterpClips {
		t.Fatalf("the file declares %d clips, want %d", len(clips), InterpClips)
	}
	declared := map[string]bool{}
	for _, clip := range clips {
		declared[clip.Name] = true
	}
	drawn := map[string]bool{}
	for _, row := range interpGrid {
		for _, cell := range row {
			if !declared[cell.clip] {
				t.Errorf("the grid plays %q, which the file does not declare", cell.clip)
			}
			if drawn[cell.clip] {
				t.Errorf("the grid plays %q twice", cell.clip)
			}
			drawn[cell.clip] = true
		}
	}
	if len(drawn) != InterpClips {
		t.Errorf("the grid plays %d of the file's %d clips", len(drawn), InterpClips)
	}
}

// The file's index buffers are UNSIGNED_BYTE, which glTF allows and WebGPU has
// no format for, so the loader has to widen them. It is asserted as "the file
// draws every primitive it has": a widening that dropped the high nibble would
// still produce triangles, but a widening that did not happen at all produces
// no draw, and the browser is where that would otherwise be found.
func TestTheU8IndexedFileDrawsEveryPrimitive(t *testing.T) {
	engine, _ := run(t)
	f := step(t, engine)
	indexed := 0
	for _, draw := range f.draws {
		if draw.Indexed {
			indexed++
		}
	}
	if indexed == 0 {
		t.Fatal("the frame made no indexed draw at all")
	}
	// The cap copy is the whole file in one Entity, so its ten primitives are
	// ten instances of the frame's total.
	if got := f.instances(); got != DrawnInstances {
		t.Errorf("drew %d instances, want %d; the u8-indexed file is %d of them",
			got, DrawnInstances, 9+interpPrimitives)
	}
}

// The cap copy is offered every clip the file has and keeps the four heaviest.
// Which four survive is not rigged, it falls out of the rule: the translation
// row takes three places and the first rotation clip offered takes the last.
// The weights are the demo's own, so this asserts the picture the station
// makes rather than model's arithmetic.
//
// It is the demo that cuts nine to four, because an Animation holds four plays
// by type; run fails on any report, so the cap copy reaching model with no
// ErrModelPlaysOverLimit is asserted by every test here.
func TestTheCapKeepsTheTranslationRowAndOneRotation(t *testing.T) {
	kept := capPlaylist()
	translations, rotations := 0, 0
	for i, clip := range kept.Clips {
		switch weight := kept.Weights[i]; {
		case clip == "":
			t.Errorf("the cap left slot %d empty; nine offers fill all four", i)
		case weight == interpRowWeights[2]:
			translations++
		case weight == interpRowWeights[1]:
			rotations++
		default:
			t.Errorf("the cap kept %q at weight %v, which is the scale row", clip, weight)
		}
	}
	if translations != 3 || rotations != 1 {
		t.Errorf("the cap kept %d translation and %d rotation clips, want 3 and 1",
			translations, rotations)
	}
	if kept.Clips[slices.Index(kept.Weights[:], interpRowWeights[1])] != interpGrid[1][0].clip {
		t.Errorf("the cap kept %v; the rotation that survives is the first offered, %q",
			kept.Clips, interpGrid[1][0].clip)
	}
}

// # The morph stations: the attribute mask, sparse weights and the contrast
//
// The two cubes are the same cube. The mask a delta record is packed against is
// intersected with what the base primitive authored, so the file that authors a
// TANGENT spends three slots a vertex and the one that does not spends two -
// and a mask read off the targets alone would give both the same stride.
func TestTheTwoMorphCubesDifferByTheirAuthoredTangent(t *testing.T) {
	engine, demo := run(t)
	plain, quantized := demo.memory.morph[2], demo.memory.morph[3]
	// A block is its per-slot ranges, a base/first/count per target, and the
	// records each target's live span covers - so the two files differ in both
	// the range words and the record width, and the span counts are the cube's
	// own: 33 records over two targets in the plain file, 36 in the quantized
	// one, whose second target moves a wider run.
	const (
		word        = 4
		rangeWords  = 3
		headerWords = 3
		cubeTargets = 2
	)
	if want := word * (3*rangeWords + cubeTargets*headerWords + 33*4); plain != want {
		t.Errorf("the plain cube's deltas are %d bytes, want %d: sixteen-byte records for position, normal and tangent",
			plain, want)
	}
	if want := word * (2*rangeWords + cubeTargets*headerWords + 36*3); quantized != want {
		t.Errorf("the quantized cube's deltas are %d bytes, want %d: twelve-byte records for position and normal",
			quantized, want)
	}
	// The pair is the point: identical geometry, identical motion, different
	// stride. Equal byte counts would mean the mask never consulted the base.
	if plain == quantized {
		t.Error("both cubes report the same delta bytes; the mask was taken from the targets")
	}
	var total int
	engine.Lookup(func(la model.LookupAccess) { total = la.TotalMorphBytes() })
	if want := plain + quantized + demo.memory.morph[4]; total != want {
		t.Errorf("TotalMorphBytes = %d, want the three morphed models' %d", total, want)
	}
}

// The stress file carries the eight shapes the station and the HUD say it does,
// and the two clips the station plays. A file swapped underneath would
// otherwise draw a still model that reads as the contrast failing.
func TestTheStressFileHasEightShapesAndBothClips(t *testing.T) {
	engine, _ := run(t)
	names := deviceOf(t, engine, func(la model.LookupDeviceAccess) []string {
		out, _ := la.MorphTargets(stressPath, nil)
		return out
	})
	if len(names) != StressTargets {
		t.Errorf("MorphTargets = %v, want the file's %d shapes", names, StressTargets)
	}
	clips := deviceOf(t, engine, func(la model.LookupDeviceAccess) []model.ClipInfo {
		infos, _ := la.Clips(stressPath, nil)
		return infos
	})
	declared := map[string]bool{}
	for _, clip := range clips {
		declared[clip.Name] = true
	}
	for _, want := range []string{stressClip, stressContrastClip} {
		if !declared[want] {
			t.Errorf("the stress file declares no clip %q; it has %v", want, clips)
		}
	}
}

// Turning the contrast off is the direct A/B the station exists for: with it on
// the two copies disagree, with it off they move together. The instance count
// does not change either way, because which clip an Entity plays is not a draw.
func TestTheContrastDoesNotChangeTheInstanceCount(t *testing.T) {
	engine, demo := run(t)
	before := step(t, engine).instances()
	press(engine, input.KeyO)
	if demo.contrast {
		t.Fatal("key o left the contrast on")
	}
	after := step(t, engine).instances()
	if before != after {
		t.Errorf("toggling the contrast changed the instance count from %d to %d", before, after)
	}
	if before != DrawnInstances {
		t.Errorf("drew %d instances, want %d", before, DrawnInstances)
	}
}
