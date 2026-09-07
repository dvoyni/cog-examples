package main

import (
	"errors"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// run starts the demo headless over the vendored asset set and steps until
// every file is resident, which is when the frame it records is the frame the
// reference screenshot was taken of.
//
// The wait is wall clock rather than a frame count on purpose: a load does not
// run on the frame's thread, and MorphStressTest is half a megabyte of deltas.
// That is exactly the hitch the asynchronous path exists to keep out of the
// frame, and a test that waited in frames would be asserting it does not exist.
//
// Unlike pbr's, this harness does not fail on a reported error, because this
// demo reports one on purpose: the interpolation station offers nine plays
// against a cap of four. TestTheCapReportsOnceAndDropsTheLightestPlays is where
// that report is asserted, and TestTheDemoReportsNothingButTheCap is where
// everything else is.
func run(t *testing.T) (*headless.Engine, *Animated) {
	t.Helper()
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	demo := New()
	engine := headless.NewOver(t, config, demo)
	deadline := time.Now().Add(60 * time.Second)
	for demo.ResidentCount() < len(ModelPaths) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d models became resident; engine reported %v",
				demo.ResidentCount(), len(ModelPaths), engine.Errors())
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
	// One more pair of steps: the frame that first sees every model resident is
	// also the first to record its draws, and Passes publishes the frame the
	// last flush consumed.
	engine.Steps(2)
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

// lookupOf runs one read against the demo's own facade.
func lookupOf[T any](t *testing.T, engine *headless.Engine, read func(scene.LookupAccess) T) T {
	t.Helper()
	var out T
	engine.Lookup(func(la scene.LookupAccess) { out = read(la) })
	return out
}

// Every file becomes resident and every primitive of every draw becomes a
// draw. This is the number the whole demo rests on: a model that silently
// loaded half of itself would still render a picture, and only a spelled-out
// count catches it.
func TestEveryModelBecomesResidentAndEveryPrimitiveBecomesADraw(t *testing.T) {
	engine, demo := run(t)
	if demo.ResidentCount() != len(ModelPaths) {
		t.Fatalf("%d of %d models resident", demo.ResidentCount(), len(ModelPaths))
	}
	view := pass(t, engine)
	if view.Recorded != RecordedDraws {
		t.Errorf("recorded %d draws, want %d", view.Recorded, RecordedDraws)
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

// # The canary assertion
//
// Every draw binds all seven of the bundled shader's storage buffers, group 2's
// three included. This is the failure the whole demo exists to catch: a
// declared binding that is never bound kills the entire frame in a browser with
// nothing logged, and a desktop adapter's hardware limits sit far above the
// floor where it would show.
//
// It is asserted per draw rather than per frame because the failure is
// per draw: a skinned model binds its poses and a morph-only one binds its
// deltas, and the flush fills whichever half a model does not have from the
// null skin. A frame-wide "were all seven seen" would pass while a single
// morph-only draw left scenePoses unbound.
func TestEveryDrawBindsAllSevenStorageBuffers(t *testing.T) {
	engine, _ := run(t)
	backend := engine.Backend()

	// The frame's own bindings, not every frame's since the engine started:
	// Buffers accumulates, and the frames before residency bound less.
	want := [][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {2, 0}, {2, 1}, {2, 2}}
	seen := map[[2]int]int{}
	scenePipelineBindings := 0
	for _, binding := range backend.Buffers {
		if !backend.IsScenePipeline(binding.Pipeline) {
			continue
		}
		seen[[2]int{binding.Group, binding.Binding}]++
		scenePipelineBindings++
	}
	if len(seen) != len(want) {
		t.Fatalf("the frame bound %d distinct slots, want the shader's %d: %v",
			len(seen), len(want), seen)
	}
	for _, slot := range want {
		if seen[slot] == 0 {
			t.Errorf("group %d binding %d was never bound; on the web that is a blank canvas",
				slot[0], slot[1])
		}
	}
	// Every slot bound the same number of times is what "every draw bound all
	// seven" means without counting draws: the counts can only agree if no
	// draw skipped one.
	for _, slot := range want {
		if seen[slot] != seen[want[0]] {
			t.Errorf("group %d binding %d bound %d times against %d for group 0 binding 0; some draw skipped it",
				slot[0], slot[1], seen[slot], seen[want[0]])
		}
	}
	if scenePipelineBindings != len(want)*seen[want[0]] {
		t.Errorf("scene pipelines made %d bindings, want %d slots x %d draws",
			scenePipelineBindings, len(want), seen[want[0]])
	}
}

// The seven storage buffers are also the whole of scene's budget against the
// browser floor of eight, and gfx measures every shader it reflects against
// DefaultLimits rather than against the device. That check runs on the desktop
// too, so a web limit violation fails loudly here first - which is the only
// reason it is worth having a desktop test of a browser property at all.
func TestNoShaderExceedsTheWebLimits(t *testing.T) {
	engine, _ := run(t)
	var exceeded gfx.ErrShaderExceedsWebLimits
	for _, err := range engine.Errors() {
		if errors.As(err, &exceeded) {
			t.Fatalf("%s declares %d %s against the web floor of %d (this device allows %d)",
				exceeded.Shader, exceeded.Declared, exceeded.Limit, exceeded.Floor, exceeded.Device)
		}
	}
}

// Nothing is reported but the cap. The demo provokes exactly one report on
// purpose, and a test that tolerated any error would stop noticing the ones it
// did not mean to provoke - a missing clip name, a typo'd node, a texture that
// failed to decode.
func TestTheDemoReportsNothingButTheCap(t *testing.T) {
	engine, _ := run(t)
	for _, err := range engine.Errors() {
		var over scene.ErrModelPlaysOverLimit
		if errors.As(err, &over) {
			continue
		}
		t.Errorf("unexpected report: %v", err)
	}
}

// # The fox station: baked poses, the crossfade and the rest frame
//
// The rig is the thing no document built in memory reproduces: 24 joints, an
// inverse bind each, every vertex across four influences, and three clips baked
// onto one 60 Hz grid.
func TestTheFoxBakesItsRigAndItsThreeClips(t *testing.T) {
	engine, _ := run(t)
	joints := lookupOf(t, engine, func(la scene.LookupAccess) []string {
		names, _ := la.Joints(foxPath, nil)
		return names
	})
	if len(joints) != FoxJoints {
		t.Errorf("the fox has %d joints, want its %d-bone rig", len(joints), FoxJoints)
	}
	clips := lookupOf(t, engine, func(la scene.LookupAccess) []scene.ClipInfo {
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
	bytes := lookupOf(t, engine, func(la scene.LookupAccess) int {
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

// The crossfade hands scene two plays with un-normalised weights that both
// matter at the reference time. Pre-normalising here would hide the one
// property worth demonstrating - that scene normalises across a draw's plays
// because the blend is a weighted mean of TRS.
func TestTheCrossfadeOffersTwoRealWeightsAtTheReferenceTime(t *testing.T) {
	demo := New()
	walk, run := demo.FoxBlend()
	// Within one step, not exactly: the clock is a step count times 1/60, so
	// the reference time is only representable to the step it lands on.
	if drift := demo.Time() - startTime; drift < -fixedStep || drift > fixedStep {
		t.Fatalf("the demo opens at %v, want the documented %v", demo.Time(), startTime)
	}
	if walk <= 0.05 || run <= 0.05 {
		t.Errorf("crossfade at the reference time is walk %.3f run %.3f; "+
			"the reference frame should catch the blend mid-slide, not at an end", walk, run)
	}
	// The two are a partition of one clip's worth of animation, which is what
	// makes the pair a crossfade rather than two independent layers.
	if total := walk + run; total < 0.999 || total > 1.001 {
		t.Errorf("the crossfade weights total %v, want 1", total)
	}
	// Both ends of the slide are reachable, so a run of the demo shows a pure
	// walk and a pure run rather than a permanent mixture.
	pure := 0
	for step := range int(crossfadePeriod * stepsPerSecond) {
		demo.step = step
		if w, r := demo.FoxBlend(); w > 0.999 || r > 0.999 {
			pure++
		}
	}
	if pure == 0 {
		t.Error("the crossfade never reaches either clip on its own over one period")
	}
}

// The demo opens paused, which is what makes the reference screenshot
// reproducible: motion is the subject here, so a capture of a running demo
// would land on whatever step the screenshotter reached.
func TestTheDemoOpensPausedAtTheReferenceTime(t *testing.T) {
	demo := New()
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
	joints := lookupOf(t, engine, func(la scene.LookupAccess) []string {
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
	clips := lookupOf(t, engine, func(la scene.LookupAccess) []scene.ClipInfo {
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
	drawn := 0
	for _, draw := range engine.Backend().Draws {
		if draw.Indexed {
			drawn++
		}
	}
	if drawn == 0 {
		t.Fatal("the frame made no indexed draw at all")
	}
	// The cap copy is the whole file in one draw, so its ten primitives are ten
	// batches of the frame's total.
	if view := pass(t, engine); view.Recorded != RecordedDraws {
		t.Errorf("recorded %d draws, want %d; the u8-indexed file is %d of them",
			view.Recorded, RecordedDraws, 9+interpPrimitives)
	}
}

// The cap copy offers every clip the file has and keeps the four heaviest. The
// report is the contract: a fifth play degrades the draw rather than failing
// it, so the copy still renders and the drop is a report rather than a hole.
func TestTheCapReportsOnceAndDropsTheLightestPlays(t *testing.T) {
	engine, _ := run(t)
	var over scene.ErrModelPlaysOverLimit
	found := 0
	for _, err := range engine.Errors() {
		if errors.As(err, &over) {
			found++
		}
	}
	if found == 0 {
		t.Fatalf("the cap never reported; errors were %v", engine.Errors())
	}
	// Once per model, not once per frame and not once per dropped play: the
	// demo runs for many frames before this test reads the list.
	if found != 1 {
		t.Errorf("the cap reported %d times, want once per model", found)
	}
	if over.Plays != InterpClips || over.Limit != CapPlays {
		t.Errorf("report says %d plays against a limit of %d, want %d and %d",
			over.Plays, over.Limit, InterpClips, CapPlays)
	}
	if over.Model != interpPath {
		t.Errorf("report names %q, want the interpolation file", over.Model)
	}
}

// Which four survive is not rigged, it falls out of the rule: the cap keeps the
// heaviest, so the translation row takes three places and the first rotation
// clip offered takes the last. The weights are the demo's own, so this asserts
// the picture the station makes rather than scene's arithmetic.
func TestTheCapKeepsTheTranslationRowAndOneRotation(t *testing.T) {
	type offered struct {
		clip   string
		weight float32
	}
	var plays []offered
	for row := range interpGrid {
		for column := range interpGrid[row] {
			plays = append(plays, offered{interpGrid[row][column].clip, interpRowWeights[row]})
		}
	}
	// The same rule scene applies: fill four, then displace the lightest only
	// when the newcomer beats it, so ties go to whoever was offered first.
	var kept [CapPlays]offered
	count := 0
	for _, play := range plays {
		if count < CapPlays {
			kept[count] = play
			count++
			continue
		}
		lightest := 0
		for i := 1; i < count; i++ {
			if kept[i].weight < kept[lightest].weight {
				lightest = i
			}
		}
		if play.weight > kept[lightest].weight {
			kept[lightest] = play
		}
	}
	translations, rotations := 0, 0
	for _, play := range kept {
		switch {
		case play.weight == interpRowWeights[2]:
			translations++
		case play.weight == interpRowWeights[1]:
			rotations++
		default:
			t.Errorf("the cap kept %q at weight %v, which is the scale row", play.clip, play.weight)
		}
	}
	if translations != 3 || rotations != 1 {
		t.Errorf("the cap kept %d translation and %d rotation clips, want 3 and 1",
			translations, rotations)
	}
}

// # The morph stations: the attribute mask, sparse weights and the override
//
// The two cubes are the same cube. The mask a delta record is packed against is
// intersected with what the base primitive authored, so the file that authors a
// TANGENT spends three slots a vertex and the one that does not spends two -
// and a mask read off the targets alone would give both the same stride.
func TestTheTwoMorphCubesDifferByTheirAuthoredTangent(t *testing.T) {
	engine, demo := run(t)
	plain, quantized := demo.memory.morph[2], demo.memory.morph[3]
	const (
		cubeVertices = 24
		cubeTargets  = 2
		slot         = 16
	)
	if want := cubeVertices * 3 * slot * cubeTargets; plain != want {
		t.Errorf("the plain cube's deltas are %d bytes, want %d for position, normal and tangent",
			plain, want)
	}
	if want := cubeVertices * 2 * slot * cubeTargets; quantized != want {
		t.Errorf("the quantized cube's deltas are %d bytes, want %d for position and normal",
			quantized, want)
	}
	// The pair is the point: identical geometry, identical motion, different
	// stride. Equal byte counts would mean the mask never consulted the base.
	if plain == quantized {
		t.Error("both cubes report the same delta bytes; the mask was taken from the targets")
	}
	total := lookupOf(t, engine, func(la scene.LookupAccess) int { return la.TotalMorphBytes() })
	if want := plain + quantized + demo.memory.morph[4]; total != want {
		t.Errorf("TotalMorphBytes = %d, want the three morphed models' %d", total, want)
	}
}

// The demo's tabulated shape names are the file's own. They are tabulated so
// the HUD can name the held shape without a per-frame lookup, and a table that
// drifted would print a confident wrong name.
func TestTheTabulatedShapeNamesAreTheFilesOwn(t *testing.T) {
	engine, _ := run(t)
	names := lookupOf(t, engine, func(la scene.LookupAccess) []string {
		out, _ := la.MorphTargets(stressPath, nil)
		return out
	})
	if len(names) != StressTargets {
		t.Fatalf("MorphTargets = %v, want the file's %d shapes", names, StressTargets)
	}
	for i, name := range names {
		if StressShapeNames[i] != name {
			t.Errorf("shape %d is %q in the file and %q in the demo", i, name, StressShapeNames[i])
		}
	}
}

// The override is sparse by construction: one shape of eight, so only one
// weight is nonzero and only one target reaches the frame's anim block. That is
// the whole reason the morph blend is CPU-side - a dense array of eight would
// upload eight records to say seven zeroes.
func TestTheOverrideIsOneShapeOfEight(t *testing.T) {
	demo := New()
	seen := map[int]bool{}
	for step := range int(shapeDwell*StressTargets*stepsPerSecond) + 1 {
		demo.step = step
		weights := demo.OverrideWeights()
		if len(weights) != StressTargets {
			t.Fatalf("the override is %d weights, want the file's %d", len(weights), StressTargets)
		}
		nonzero := 0
		for _, weight := range weights {
			if weight != 0 {
				nonzero++
			}
		}
		if nonzero != 1 {
			t.Fatalf("at step %d the override has %d nonzero weights, want one", step, nonzero)
		}
		seen[demo.OverrideShape()] = true
	}
	// Every shape is reached, so the station shows all eight rather than
	// cycling through a subset.
	if len(seen) != StressTargets {
		t.Errorf("the override reaches %d of the file's %d shapes", len(seen), StressTargets)
	}
}

// Turning the override off is the direct A/B the station exists for: with it on
// the two copies disagree, with it off they move together. The draw count does
// not change either way, because a weight array is not a draw.
func TestTheOverrideDoesNotChangeTheDrawCount(t *testing.T) {
	engine, demo := run(t)
	before := pass(t, engine).Recorded
	demo.override = false
	engine.Steps(2)
	after := pass(t, engine).Recorded
	if before != after {
		t.Errorf("toggling the override changed the draw count from %d to %d", before, after)
	}
	if before != RecordedDraws {
		t.Errorf("recorded %d draws, want %d", before, RecordedDraws)
	}
}
