package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/qmuntal/gltf"
)

// This file is where most of this demo lives. The grid is nearly all assertion
// and almost no picture on purpose: loading and the lookup facade are the two
// contracts whose failures are invisible in a frame, so the frame is the
// smaller half of the demonstration and this is the larger one.
//
// What a test knows of the frame is what reached the fake Backend: the passes
// gfx began, the draws it made in each and the pipelines they drew through.
// scene publishes no account of its own frame, so a count here is a count of
// instances the GPU was asked to draw.

// run starts the demo headless over the vendored asset set and steps until
// every station has reached the outcome its table row expects - thirteen
// loaded, three failed - which is the frame the reference screenshot shows.
//
// The loop settles on its first pass: a load runs inside the tick whose load
// System keyed the file, so the tick that draws a station is the tick that
// loaded it. What that tick costs is the hitch this design accepts - decoding
// TextureSettingsTest's images is the expensive one - and Preload is the lever
// a game pulls to move it.
func run(t *testing.T) (*headless.Engine, *Loading) {
	t.Helper()
	engine, demo := start(t)
	settle(t, engine, demo)
	return engine, demo
}

// start brings the engine up without stepping it, for the tests that are about
// what the first few frames do.
func start(t *testing.T) (*headless.Engine, *Loading) {
	t.Helper()
	demo := New()
	return headless.New(t, demo), demo
}

// settle steps until every station has reached its expected residency, then
// twice more, so what the backend holds last is a whole settled frame.
func settle(t *testing.T, engine *headless.Engine, demo *Loading) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !demo.Settled() {
		if time.Now().After(deadline) {
			t.Fatalf("stations never settled: %s; engine reported %v",
				unsettled(demo), engine.Errors())
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
	engine.Steps(2)
	if unexpected := unexpectedErrors(engine, demo); len(unexpected) > 0 {
		t.Fatalf("the engine reported %d errors this demo does not provoke, first: %v",
			len(unexpected), unexpected[0])
	}
}

// unsettled names the stations that did not reach the outcome their table row
// expects, so a timeout says which rather than only that one did.
func unsettled(demo *Loading) string {
	out := ""
	for i := range stations {
		if (demo.State(i) == nil) != stations[i].loads {
			out += " " + stations[i].name + "=" + stateName(demo.State(i))
		}
	}
	return out
}

// unexpectedErrors is every report the demo's own allow-list does not claim.
// The allow-list is the demo's, not a copy of it: a test with its own list
// would let the two drift, and the shipped one is the one that has to be right.
func unexpectedErrors(engine *headless.Engine, demo *Loading) []error {
	var out []error
	for _, err := range engine.Errors() {
		if expected, _ := demo.expectedReport(err); !expected {
			out = append(out, err)
		}
	}
	return out
}

// frame is what one step sent the backend: the passes scene began for its
// cameras, and the draws made in them. Draws index frame.passes.
type frame struct {
	passes []gfx.PassDesc
	draws  []headless.DrawCall
}

// cameraPassPrefix begins every pass scene labels for a camera,
// "scene.camera<ID>.<tag>". Anything else in the backend is canvas's.
const cameraPassPrefix = "scene.camera"

// step takes one step and keeps what it sent the backend in scene's passes.
func step(engine *headless.Engine) frame {
	backend := engine.Backend()
	passes, draws := len(backend.Passes), len(backend.Draws)
	engine.Steps(1)
	var f frame
	rebase := map[int]int{}
	for i, pass := range backend.Passes[passes:] {
		if strings.HasPrefix(pass.Label, cameraPassPrefix) {
			rebase[passes+i] = len(f.passes)
			f.passes = append(f.passes, pass)
		}
	}
	for _, draw := range backend.Draws[draws:] {
		if at, ok := rebase[draw.Pass]; ok {
			draw.Pass = at
			f.draws = append(f.draws, draw)
		}
	}
	return f
}

// instances is how many instances a frame's scene draws asked for in all.
func (f frame) instances() int {
	total := 0
	for _, draw := range f.draws {
		total += draw.Instances
	}
	return total
}

// Every station reaches the residency its table row expects, and the three that
// fail are the three that were meant to. This is the assertion the whole demo
// rests on: a grid where the wrong pad is bare still looks like a grid.
func TestEveryStationReachesTheResidencyItsTableExpects(t *testing.T) {
	_, demo := run(t)
	for i := range stations {
		if got := demo.State(i); (got == nil) != stations[i].loads {
			t.Errorf("station %q is %s (%v), want it to have loaded: %v",
				stations[i].name, stateName(got), got, stations[i].loads)
		}
	}
	// The camera, then a pad and a station each.
	if got, want := demo.Entities(), 1+2*len(stations); got != want {
		t.Errorf("setup spawned %d Entities, want %d", got, want)
	}
}

// Every pad is drawn and only a station whose selector resolved on a resident
// model draws a model. Five pads are bare, and a count is the only thing that
// separates "skipped" from "drawn somewhere off-screen".
//
// Nothing is culled at the reference pose, so the instances the backend was
// asked for are the table's count exactly; and scene draws each Batch of equal
// instances as one draw, so there are fewer draws than instances.
func TestEveryPadIsDrawnAndOnlyResolvedStationsDrawAModel(t *testing.T) {
	engine, _ := run(t)
	f := step(engine)
	if len(f.passes) != 1 {
		t.Fatalf("scene began %d camera passes, want the camera's one default pass", len(f.passes))
	}
	// The default pass keeps the colour canvas's backdrop cleared below it.
	if f.passes[0].Load == gfx.LoadClear {
		t.Error("the camera's default pass clears colour, which would wipe the backdrop")
	}
	if got := f.instances(); got != SettledInstances {
		t.Errorf("the pass drew %d instances, want %d: sixteen pads and each resident "+
			"station's primitives", got, SettledInstances)
	}
	if len(f.draws) >= SettledInstances {
		t.Errorf("the pass made %d draws for %d instances; equal instances batch, so "+
			"there should be fewer", len(f.draws), SettledInstances)
	}
}

// Nothing is substituted for a model that is not resident. The only draws for a
// station that failed are none at all - not a box, not a smaller model, not the
// last model that did load.
//
// This is the contract that cannot be seen any other way. A substitute would
// render a plausible frame, and every count in it would look reasonable.
func TestNothingIsSubstitutedForAModelThatIsNotResident(t *testing.T) {
	engine, demo := start(t)
	// Every frame from the first to a few past settled. On each one the
	// instance count has to be at most the pads plus the primitives of the
	// stations resident on that tick. A substitute is the one thing that could
	// push it over.
	//
	// Both numbers describe the same tick: the survey reads the residency
	// after scene's load System keyed the Models, and scene draws what that
	// System keyed, in the same tick.
	deadline := time.Now().Add(60 * time.Second)
	incomplete := 0
	for frames, extra := 0, 0; extra < 3; frames++ {
		if time.Now().After(deadline) {
			t.Fatalf("stations never settled: %s", unsettled(demo))
		}
		f := step(engine)
		incomplete += checkNothingSubstituted(t, demo, f, frames)
		if demo.Settled() {
			extra++
		}
		time.Sleep(time.Millisecond)
	}
	if incomplete == 0 {
		t.Error("every station was resident on the very first frame, so this test saw " +
			"no frame in which anything could have been substituted")
	}
}

// checkNothingSubstituted holds the bound for one frame, and returns 1 if that
// frame was one in which a substitution was possible at all - that is, one
// with a station still not resident.
func checkNothingSubstituted(t *testing.T, demo *Loading, f frame, at int) int {
	t.Helper()
	want, resident := len(stations), 0
	for i := range stations {
		if demo.State(i) == nil {
			want += stations[i].draws
			resident++
		}
	}
	if got := f.instances(); got > want {
		t.Fatalf("frame %d drew %d instances with %d of %d stations resident; "+
			"at most %d can be real, so something was substituted",
			at, got, resident, len(stations), want)
	}
	if resident < len(stations) {
		return 1
	}
	return 0
}

// Preload names every path the grid draws. A station whose path was missing
// here would load off scene's load System instead, which is a different code
// path and a silent one: the picture would be identical and the hitch would
// have moved back into the frame.
func TestPreloadNamesEveryPathTheGridDraws(t *testing.T) {
	named := map[string]bool{}
	for _, path := range PreloadOrder {
		if named[path] {
			t.Errorf("PreloadOrder names %q twice", path)
		}
		named[path] = true
	}
	for i := range stations {
		if !named[stations[i].path] {
			t.Errorf("station %q draws %q, which Preload never asks for",
				stations[i].name, stations[i].path)
		}
	}
	drawn := map[string]bool{}
	for i := range stations {
		drawn[stations[i].path] = true
	}
	for _, path := range PreloadOrder {
		if !drawn[path] {
			t.Errorf("PreloadOrder asks for %q, which no station draws", path)
		}
	}
}

// Preload makes a model resident with no Entity naming it anywhere. It is the
// same idempotent load scene's load System fires, fired without one, which is
// the whole of moving a decode into a loading screen the app controls.
func TestPreloadMakesAModelResidentWithNoDrawOfIt(t *testing.T) {
	mount, err := assets.Mount()
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	// No demo plugin: nothing in this engine spawns a single Entity, so the
	// asset set is mounted on its own.
	engine := headless.New(t, headless.Mounting(mount))
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		la.Preload(pathQuantized)
		if err := la.State(pathQuantized); err != nil {
			t.Fatalf("Preload left %q unloaded: %v", pathQuantized, err)
		}
	})
	// The bakes Preload queued reach the backend on the render after the update
	// that made them, so a test measuring uploads still steps the frames.
	for range 3 {
		if f := step(engine); len(f.passes) != 0 {
			t.Fatalf("scene began %d camera passes with no camera, want none", len(f.passes))
		}
	}
}

// loadNow loads one path and fails the test if it did not load. There is
// nothing to wait for - the read, the parse and the uploads all finish inside
// Preload - so what used to be a polling loop is one call.
//
// It still steps three frames: the bakes the load queued reach the backend on
// the render after the update that queued them, so a test that measured
// uploads here would count the frame before.
func loadNow(t *testing.T, engine *headless.Engine, path string) {
	t.Helper()
	var err error
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		la.Preload(path)
		err = la.State(path)
	})
	if err != nil {
		t.Fatalf("%s did not load: %v", path, err)
	}
	engine.Steps(3)
}

// Every failure happens where the caller is standing. There is no slow half
// and fast half: the read, the parse and the uploads all run inside the call
// that asked, so an invalid path, a truncated file and a file that is not
// there all answer on the first query.
//
// This replaces what this demo's ticket asked for twice over. #97 expected the
// non-existent path to be the synchronous one and it was not, because finding
// out meant looking at the filesystem; now looking at the filesystem is
// something the asking call does, so all three are synchronous and the
// distinction the old test drew has nothing left to draw.
func TestEveryFailureHappensInTheCallThatAsked(t *testing.T) {
	engine, demo := start(t)
	var invalid, truncated, absent error
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		invalid = la.State(pathInvalid)
		truncated = la.State(pathTruncated)
		absent = la.State(pathMissing)
	})
	if _, ok := invalid.(model.ErrModelPathInvalid); !ok {
		t.Errorf("the first State of an invalid path is %v, want it refused before the cache", invalid)
	}
	for name, err := range map[string]error{"truncated": truncated, "absent": absent} {
		if err == nil {
			t.Errorf("the first State of the %s file is nil, want the failure it just hit", name)
		}
	}
	settle(t, engine, demo)
	// The two file failures are distinguishable by what they wrap, which is the
	// honest report of what the API can tell a caller. The absent one is the
	// library's own read failure, reported under the descriptor; the truncated
	// one is the loader's, reported as model's own error.
	var notExist, unexpectedEOF bool
	for _, err := range engine.Errors() {
		if strings.Contains(err.Error(), pathMissing) && errors.Is(err, fs.ErrNotExist) {
			notExist = true
		}
		var unavailable model.ErrModelUnavailable
		if errors.As(err, &unavailable) && unavailable.Model == pathTruncated {
			unexpectedEOF = !errors.Is(err, fs.ErrNotExist)
		}
	}
	if !notExist {
		t.Error("the absent file did not report a wrapped fs.ErrNotExist")
	}
	if !unexpectedEOF {
		t.Error("the truncated file reported fs.ErrNotExist, want the read failure")
	}
}

// reportsFor counts how many times one path has been reported unloadable,
// whichever half of the load reported it: the loader names the model in its own
// error, and the library names the path in the message of its read failure.
func reportsFor(engine *headless.Engine, path string) int {
	count := 0
	for _, err := range engine.Errors() {
		var unavailable model.ErrModelUnavailable
		if errors.As(err, &unavailable) && unavailable.Model == path {
			count++
			continue
		}
		if errors.Is(err, fs.ErrNotExist) && strings.Contains(err.Error(), path) {
			count++
		}
	}
	return count
}

// A failed path never retries, and unload is the only lever that clears it.
// A typo'd path named by a Model every tick must not re-read the file every
// tick forever, so failure is terminal - and a Retry that did not first free
// would be a second name for the idempotent load that already exists.
func TestAFailedPathNeverRetriesAndUnloadIsTheOnlyLever(t *testing.T) {
	engine, demo := run(t)
	// Thirty frames with three failed paths on the grid, and none of them loads
	// again. The observable is the report count: a path that retried would
	// report afresh, because the entry that silences it is the same entry that
	// stops the load.
	before := map[string]int{}
	for _, path := range []string{pathTruncated, pathMissing} {
		before[path] = reportsFor(engine, path)
	}
	for frame := range 30 {
		engine.Steps(1)
		for _, path := range []string{pathTruncated, pathMissing} {
			if got := reportsFor(engine, path); got != before[path] {
				t.Fatalf("on frame %d %s has reported %d times, want the %d it had: "+
					"failure is terminal and clears only on unload",
					frame, path, got, before[path])
			}
		}
		for _, path := range []string{pathTruncated, pathMissing, pathInvalid} {
			var err error
			engine.LookupDevice(func(la model.LookupDeviceAccess) { err = la.State(path) })
			if err == nil {
				t.Fatalf("on frame %d %s loaded, want it still failed", frame, path)
			}
		}
	}
	// The demo's own retry lever: unload, then preload. All three fail again,
	// because all three are still broken - and the observable is a second
	// report, because reports are keyed and fired once and the key is cleared
	// on unload. The truncated file reporting twice is the proof the unload
	// actually retired the entry, and reporting exactly twice is the proof
	// that marking its Model changed did not load it a third time.
	reported := reportsFor(engine, pathTruncated)
	demo.Retry()
	engine.Steps(2)
	if after := reportsFor(engine, pathTruncated); after != reported+1 {
		t.Errorf("the truncated file reported %d times before the retry and %d after, "+
			"want exactly one more: unload is what clears a failed path", reported, after)
	}
	if unexpected := unexpectedErrors(engine, demo); len(unexpected) > 0 {
		t.Errorf("the retry reported %d errors the demo does not provoke, first: %v",
			len(unexpected), unexpected[0])
	}
}

// Nodes lists a ref's addressable nodes depth-first, and a Node ref lists that
// subtree with the node itself first. Unnamed nodes are absent, because a
// selector is a name and a node without one cannot be addressed - which is why
// MultipleScenes, whose nodes are all unnamed, lists none at all.
func TestNodesListsTheSubtreeDepthFirst(t *testing.T) {
	engine, _ := run(t)
	for _, c := range []struct {
		ref  model.ModelRef
		want []string
	}{
		{stations[stationScene].ref(),
			[]string{"Yup2Zup", "Cesium_Milk_Truck", "Node", "Wheels", "Node.001", "Wheels.001"}},
		{stations[stationBody].ref(),
			[]string{"Cesium_Milk_Truck", "Node", "Wheels", "Node.001", "Wheels.001"}},
		{stations[stationWheel].ref(), []string{"Wheels"}},
		{stations[stationWheelOther].ref(), []string{"Wheels.001"}},
		{stations[stationDefaultScene].ref(), nil},
	} {
		var got []string
		var ok bool
		engine.LookupDevice(func(la model.LookupDeviceAccess) { got, ok = la.Nodes(c.ref, nil) })
		if !ok {
			t.Errorf("Nodes(%+v) answered false", c.ref)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("Nodes(%+v) = %v, want %v", c.ref, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Nodes(%+v) = %v, want %v", c.ref, got, c.want)
				break
			}
		}
	}
}

// Every query on the facade answers (value, false) rather than a zero value a
// caller might mistake for an answer - for a failed path, for a scene name the
// file does not carry, and for a node name it does not carry. State is the only
// one that separates those from a path that is still loading.
func TestEveryQueryAnswersFalseForAnUnresolvedRef(t *testing.T) {
	engine, _ := run(t)
	for _, ref := range []model.ModelRef{
		{Path: pathTruncated},
		{Path: pathMissing},
		{Path: pathInvalid},
		stations[stationNoSuchScene].ref(),
		stations[stationNoSuchNode].ref(),
	} {
		engine.LookupDevice(func(la model.LookupDeviceAccess) {
			if _, ok := la.Nodes(ref, nil); ok {
				t.Errorf("Nodes(%+v) answered true", ref)
			}
			if _, ok := la.Bounds(ref); ok {
				t.Errorf("Bounds(%+v) answered true", ref)
			}
			if _, _, ok := la.AABB(ref); ok {
				t.Errorf("AABB(%+v) answered true", ref)
			}
			if _, ok := la.Clips(ref.Path, nil); ok && ref.Path == pathTruncated {
				t.Errorf("Clips(%q) answered true for a failed path", ref.Path)
			}
		})
	}
}

// The station table's measurements are the vendored bytes'. The layout is typed
// rather than read from AABB at runtime so the picture does not change on the
// frame a model becomes resident, and this is what keeps the two honest: a
// re-vendored asset fails here instead of silently drifting the grid.
func TestTheStationTableMeasuresTheVendoredBytes(t *testing.T) {
	engine, _ := run(t)
	for i := range stations {
		station := &stations[i]
		if station.draws == 0 {
			continue // no geometry to measure
		}
		var min, max m.Vec3
		var ok bool
		engine.LookupDevice(func(la model.LookupDeviceAccess) { min, max, ok = la.AABB(station.ref()) })
		if !ok {
			t.Errorf("station %q: AABB answered false", station.name)
			continue
		}
		size := max.Sub(min)
		if !nearVec(size, station.size, 1e-3) {
			t.Errorf("station %q: measures %+v, table says %+v", station.name, size, station.size)
		}
		if !near(min.Y, station.minY, 1e-3) {
			t.Errorf("station %q: minY is %v, table says %v", station.name, min.Y, station.minY)
		}
		// The scale is what keeps sixteen files of wildly different authored
		// sizes comparable on one grid: nothing overruns the spacing between
		// two pads, and nothing shrinks to a speck on its own.
		reach := size.X
		for _, extent := range [2]float32{size.Y, size.Z} {
			if extent > reach {
				reach = extent
			}
		}
		reach *= station.scale
		if reach > colSpacing || reach < padReach/2 {
			t.Errorf("station %q: scales to %v at its longest, want between %v and %v",
				station.name, reach, float32(padReach)/2, float32(colSpacing))
		}
	}
}

// Re-rooting discards every ancestor transform, not merely the immediate
// parent's. The two wheels are the assertion: they are the same mesh under the
// same local rotation, hanging off two parents with different offsets, so a
// re-root that left any ancestor's transform in would put them in two different
// places. Re-rooted they are identical, and they are the file's own mesh box.
//
// The expected box is built here from the chain in the file rather than typed,
// because a hand-transcribed number is exactly what this asset has caught
// before: this demo's ticket quotes the spec's claim that the wheels "sit at
// ±1.43 on X", and they do not - the offsets are on their parents and they are
// asymmetric.
func TestReRootingDiscardsEveryAncestorTransform(t *testing.T) {
	engine, _ := run(t)
	doc := parse(t, pathTruck)
	want := meshBox(t, doc, nodeNamed(t, doc, "Wheels").Mesh)

	var boxes [2]m.Box3
	for at, index := range [2]int{stationWheel, stationWheelOther} {
		var min, max m.Vec3
		var ok bool
		engine.LookupDevice(func(la model.LookupDeviceAccess) {
			min, max, ok = la.AABB(stations[index].ref())
		})
		if !ok {
			t.Fatalf("station %q: AABB answered false", stations[index].name)
		}
		boxes[at] = m.Box3{Min: min, Max: max}
	}
	if !nearVec(boxes[0].Min, boxes[1].Min, 1e-4) || !nearVec(boxes[0].Max, boxes[1].Max, 1e-4) {
		t.Errorf("the two wheels re-root to different boxes:\n  Wheels     %+v\n  Wheels.001 %+v\n"+
			"they are the same mesh under the same local rotation, so a re-root that "+
			"discarded every ancestor transform would land them in the same place",
			boxes[0], boxes[1])
	}
	if !nearVec(boxes[0].Min, want.Min, 1e-3) || !nearVec(boxes[0].Max, want.Max, 1e-3) {
		t.Errorf("the re-rooted wheel is %+v, want the file's own mesh box %+v", boxes[0], want)
	}
}

// A Node selector discards the scene root's transform and an empty Node keeps
// it. On this asset that is a whole axis: the scene's only root is Yup2Zup,
// whose rotation is what stands the truck up, so the body drawn through a Node
// selector lies on its side and the same subtree drawn as the scene does not.
func TestAWholeSceneDrawKeepsTheRootTransformAndANodeDrawDiscardsIt(t *testing.T) {
	engine, _ := run(t)
	var whole, body m.Box3
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		min, max, _ := la.AABB(stations[stationScene].ref())
		whole = m.Box3{Min: min, Max: max}
		min, max, _ = la.AABB(stations[stationBody].ref())
		body = m.Box3{Min: min, Max: max}
	})
	// Yup2Zup is a quarter turn, so the two boxes carry the same extents with
	// two axes exchanged. Comparing the extents rather than the corners is what
	// makes this an assertion about the rotation and not about the asset's
	// centre of mass.
	wholeSize, bodySize := whole.Max.Sub(whole.Min), body.Max.Sub(body.Min)
	if near(wholeSize.Y, bodySize.Y, 1e-3) {
		t.Errorf("the scene draw and the node draw have the same height (%v): "+
			"the root's rotation was not discarded", wholeSize.Y)
	}
	// Yup2Zup is a quarter turn about an axis that cycles all three extents
	// rather than swapping two: the file is authored Z-up and the root stands
	// it up, so X, Y, Z comes back as Y, Z, X.
	if !near(wholeSize.X, bodySize.Y, 1e-3) ||
		!near(wholeSize.Y, bodySize.Z, 1e-3) ||
		!near(wholeSize.Z, bodySize.X, 1e-3) {
		t.Errorf("the node draw's extents %+v are not the scene draw's %+v cycled, "+
			"which is what discarding Yup2Zup does", bodySize, wholeSize)
	}
}

// A Scene selector that names the file's own scene resolves, and resolves to
// the same view the default would - which on this asset set is the only shape a
// matching Scene can take. Six vendored assets name their scene "Scene", and in
// every one of them that scene is also the declared default; MultipleScenes,
// the repository's only file with more than one scenes entry, leaves both of
// its unnamed. So selecting a non-default scene has no asset behind it and is
// kept here as a gap rather than designed around.
func TestAMatchedSceneSelectorResolvesToTheSameDrawAsTheDefault(t *testing.T) {
	engine, _ := run(t)
	var named, byDefault m.Box3
	var namedOK, defaultOK bool
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		min, max, ok := la.AABB(model.ModelRef{Path: pathTruck, Scene: "Scene"})
		named, namedOK = m.Box3{Min: min, Max: max}, ok
		min, max, ok = la.AABB(model.ModelRef{Path: pathTruck})
		byDefault, defaultOK = m.Box3{Min: min, Max: max}, ok
	})
	if !namedOK {
		t.Fatal(`Scene: "Scene" answered false on a file whose only scene is named Scene`)
	}
	if !defaultOK {
		t.Fatal("the default scene answered false")
	}
	if named != byDefault {
		t.Errorf("the named scene is %+v and the default is %+v", named, byDefault)
	}
}

// An unmatched selector reports exactly once however many frames draw it, and
// never falls back. One typo'd node name rendering an entire building at the
// origin is the worse failure of the two, and it is the one a report cannot
// make visible.
func TestAnUnmatchedSelectorReportsOnceHoweverManyFramesDrawIt(t *testing.T) {
	engine, demo := run(t)
	engine.Steps(60)
	var sceneReports, nodeReports int
	for _, err := range engine.Errors() {
		var missingScene model.ErrModelSceneMissing
		if errors.As(err, &missingScene) && missingScene.Scene == stations[stationNoSuchScene].scene {
			sceneReports++
		}
		var missingNode model.ErrModelNodeMissing
		if errors.As(err, &missingNode) && missingNode.Node == stations[stationNoSuchNode].node {
			nodeReports++
		}
	}
	if sceneReports != 1 {
		t.Errorf("the unmatched Scene reported %d times over ~%d frames, want once",
			sceneReports, demo.Step())
	}
	if nodeReports != 1 {
		t.Errorf("the unmatched Node reported %d times over ~%d frames, want once",
			nodeReports, demo.Step())
	}
}

// Nine glTF textures over three images reach the GPU as three uploads, and
// drawing one model six times decodes its one image once.
//
// These are the only two honest exercises of the path-keyed texture cache there
// are. It cannot be exercised across two models: every model owns a private
// directory, so no two files ever resolve to the same path.
func TestTheTextureCacheBakesOneTexturePerImageNotPerGlTFTexture(t *testing.T) {
	doc := parse(t, pathSamplers)
	if len(doc.Images) == 0 || len(doc.Textures) <= len(doc.Images) {
		t.Fatalf("TextureSettingsTest has %d images and %d textures; this assertion needs "+
			"more textures than images", len(doc.Images), len(doc.Textures))
	}
	// Alone in its own engine, and preloaded rather than drawn, so the count is
	// this file's and nothing else's: the grid's own engine also carries
	// canvas's font atlas, and a total that had to subtract it would be an
	// assertion about the subtraction.
	mount, err := assets.Mount()
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	engine := headless.New(t, headless.Mounting(mount))
	loadNow(t, engine, pathSamplers)
	backend := engine.Backend()
	// Every model texture is baked with a mip chain and the bundled PBR's two
	// 1x1 defaults are not, so the mipped count is the model's alone and needs
	// no subtraction to say so.
	if got := backend.MippedTextures; got != len(doc.Images) {
		t.Errorf("baked %d model textures for a file with %d images and %d glTF "+
			"textures over %d samplers, want %d: the cache is keyed by resolved "+
			"path and colour space, not by glTF texture",
			got, len(doc.Images), len(doc.Textures), len(doc.Samplers), len(doc.Images))
	}
	if got := backend.BakedTextures; got != len(doc.Images)+defaultTextures {
		t.Errorf("baked %d textures in all, want %d model textures plus the bundled "+
			"PBR's %d 1x1 defaults", got, len(doc.Images), defaultTextures)
	}
}

// Drawing one model six times bakes its texture once. It is the other of the
// two honest exercises of the path-keyed cache, and the one that says the key
// is the path rather than the Entity.
func TestDrawingOneModelManyTimesBakesItsTextureOnce(t *testing.T) {
	copies := 0
	for i := range stations {
		if stations[i].path == pathTruck && stations[i].draws > 0 {
			copies++
		}
	}
	if copies < 2 {
		t.Fatalf("only %d station draws the truck; this assertion needs several", copies)
	}
	mount, err := assets.Mount()
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	// The same number of truck Entities the grid spawns, alone in an engine
	// with no HUD, so the mipped count is the truck's and needs no subtraction.
	engine := headless.New(t, headless.Mounting(mount), &soloDemo{path: pathTruck, copies: copies})
	loadNow(t, engine, pathTruck)
	truck := parse(t, pathTruck)
	if got := engine.Backend().MippedTextures; got != len(truck.Images) {
		t.Errorf("%d Entities of one model baked %d textures, want %d - one per image, "+
			"because a model's cache key is its path", copies, got, len(truck.Images))
	}
	if f := step(engine); f.instances() != copies*TruckPrimitives {
		t.Errorf("%d truck Entities drew %d instances, want %d",
			copies, f.instances(), copies*TruckPrimitives)
	}
}

// Every texture the loader bakes carries a mip chain, which is the fourth of
// the WebGPU gaps the loader papers over: there is no mipmap generation API, so
// the chain is a CPU box filter built at load and handed over with the base
// level. A texture uploaded without one is a model that shimmers at distance
// and nothing in the frame to say why.
func TestEveryModelTextureCarriesAMipChain(t *testing.T) {
	engine, _ := run(t)
	backend := engine.Backend()
	if backend.MippedTextures == 0 {
		t.Fatal("not one texture in the whole grid was baked with a mip chain")
	}
	// The bundled PBR's two 1x1 defaults are in the total and are deliberately
	// not mipped: for a constant texel every level is identical, so there is
	// nothing to generate. Canvas's font atlas is in it too. So the claim here
	// is a floor - every model texture - rather than an equality.
	images := len(parse(t, pathSamplers).Images) + len(parse(t, pathTruck).Images)
	if backend.MippedTextures < images {
		t.Errorf("%d of %d baked textures carry a mip chain, want at least the %d "+
			"images the grid's two textured files hold",
			backend.MippedTextures, backend.BakedTextures, images)
	}
}

// defaultTextures is how many 1x1 textures the bundled PBR bakes for itself:
// the white texel every empty colour slot binds and the flat normal every empty
// normal slot binds.
const defaultTextures = 2

// Unloading a model does not cascade to its textures. With no refcount the
// lookup cannot know whether another resident model binds the same image by
// path, and freeing one that is still bound is a dead texture in a live bind
// group rather than a missing picture - so UnloadTexture is the separate,
// deliberate lever.
//
// The observable is that the reload bakes no new texture: the geometry came
// back and the image never left.
func TestUnloadingAModelDoesNotCascadeToItsTextures(t *testing.T) {
	engine, demo := run(t)
	backend := engine.Backend()
	before := backend.BakedTextures
	poseBefore := demo.TotalPoseBytes()
	if poseBefore == 0 {
		t.Fatal("no baked poses to lose; the truck's own clip should make some")
	}

	demo.UnloadModel()
	// The unload frees where the System stands, and marks the truck's Models
	// changed, so scene's load System loads the truck again in the same tick:
	// a free followed by a touch is a reload, not an error. So what is being
	// measured after this is the reload, which is exactly what makes the
	// texture count the assertion.
	f := step(engine)
	if got := f.instances(); got != SettledInstances {
		t.Errorf("the tick that unloaded the truck drew %d instances, want the %d of a "+
			"settled grid: the reload lands before scene draws", got, SettledInstances)
	}
	settle(t, engine, demo)
	if demo.State(stationScene) != nil {
		t.Errorf("the truck did not come back after UnloadModel: %v", demo.State(stationScene))
	}
	if got := backend.BakedTextures; got != before {
		t.Errorf("reloading the truck baked %d more textures, want none: UnloadModel "+
			"cascaded to the texture cache", got-before)
	}
	if got := demo.TotalPoseBytes(); got != poseBefore {
		t.Errorf("the reloaded truck has %d pose bytes, want the %d it had", got, poseBefore)
	}
}

// UnloadTexture is the lever that does free them, and after it the reload bakes
// the image again. The pair with the test above is the whole point: two levers,
// two effects, and the model one deliberately does less.
func TestUnloadTextureIsTheLeverThatFreesThem(t *testing.T) {
	engine, demo := run(t)
	backend := engine.Backend()
	before := backend.BakedTextures
	demo.UnloadModel()
	demo.UnloadTexture()
	engine.Steps(2)
	settle(t, engine, demo)
	truck := parse(t, pathTruck)
	if got := backend.BakedTextures - before; got != len(truck.Images) {
		t.Errorf("reloading the truck after UnloadTexture baked %d textures, want %d",
			got, len(truck.Images))
	}
}

// UnloadAll gives up every loaded model and every cached texture at once. It is
// the level teardown, and it walks what is loaded at the call.
//
// The observable is the same one UnloadTexture's is: the unload System marks
// every station's Model changed, so everything loads again straight
// afterwards, and because the textures went with the models, that reload
// bakes every image afresh.
func TestUnloadAllReleasesEveryLoadedModel(t *testing.T) {
	engine, demo := run(t)
	backend := engine.Backend()
	if demo.TotalPoseBytes() == 0 {
		t.Fatal("nothing loaded to release")
	}
	before := backend.BakedTextures
	demo.UnloadAll()
	engine.Steps(2)
	settle(t, engine, demo)
	if got := backend.BakedTextures; got <= before {
		t.Errorf("reloading after UnloadAll baked %d textures, want every image again",
			got-before)
	}
	if f := step(engine); f.instances() != SettledInstances {
		t.Errorf("after UnloadAll the grid drew %d instances, want the %d of a settled grid",
			f.instances(), SettledInstances)
	}
}

// textShaderLabel is the label gfx gives a shader built from inline source,
// which is how a draw with the repaint material is told from a bundled one.
const textShaderLabel = "gfx.shader"

// A replacement is the repaint shader drawing the repainted station and
// nothing else, and a tint is the bundled shader. The two stations draw the
// same subtree of the same file, so everything that differs between their
// draws is what the Entity carries.
//
// The observable is the pipeline: the repainted station's primitives, and only
// they, reach the backend through a shader built from inline source, and the
// tinted station's draw through the bundled one like every other model's.
func TestAReplacementMaterialIsADifferentShaderAndATintIsNot(t *testing.T) {
	engine, _ := run(t)
	f := step(engine)
	backend := engine.Backend()
	repainted, bundled := 0, 0
	for _, draw := range f.draws {
		switch {
		case backend.ShaderPath(backend.PipelineOf(draw.Pipeline).Shader) == textShaderLabel:
			repainted += draw.Instances
		case backend.IsScenePipeline(draw.Pipeline):
			bundled += draw.Instances
		}
	}
	if repainted != stations[stationRepainted].draws {
		t.Errorf("%d instances drew with the repaint shader, want the repainted "+
			"station's %d primitives", repainted, stations[stationRepainted].draws)
	}
	// Everything else in the pass - the pads and every other resident
	// station, the tinted one among them - is the bundled shader's.
	if want := SettledInstances - stations[stationRepainted].draws; bundled != want {
		t.Errorf("%d instances drew with the bundled scene shader, want %d", bundled, want)
	}
}

// scenePipelines is every pipeline the frame built from the bundled scene
// shader, which is how a test picks scene's own out of a frame that also drew a
// HUD.
func scenePipelines(engine *headless.Engine) []gfx.PipelineDesc {
	backend := engine.Backend()
	var out []gfx.PipelineDesc
	for _, desc := range backend.Pipelines {
		if backend.ShaderPath(desc.Shader) == headless.SceneShaderPath {
			out = append(out, desc)
		}
	}
	return out
}

// Every primitive mode in the file becomes a list, and POINTS is skipped and
// reported. This is the third WebGPU gap, and MeshPrimitiveModes is the only
// asset that can reach it: no .glb in the Khronos repository contains a fan,
// strip, loop, line or point primitive, so a packed copy of this one is the
// whole of the evidence.
//
// The invariant the batching, index and skinning paths rely on is that a model
// mesh never assembles as a strip or a fan. Two topologies survive, not one: a
// triangle strip or fan becomes a triangle list, and a line strip or loop
// becomes a line list, because that is what gfx carries and there is nothing to
// turn a line into that is still a line.
func TestEveryPrimitiveModeBecomesAListAndPointsIsSkipped(t *testing.T) {
	engine, demo := run(t)
	doc := parse(t, pathPrimitiveModes)
	modes := map[gltf.PrimitiveMode]int{}
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			modes[primitive.Mode]++
		}
	}
	if len(modes) != 7 {
		t.Fatalf("%s carries %d distinct primitive modes, want all seven",
			pathPrimitiveModes, len(modes))
	}
	if got := demo.NodeCount(stationTopologies); got != 0 {
		// Every node in this file is unnamed, so none is addressable. That is
		// itself worth pinning: it is why this station has no Node selector.
		t.Errorf("%s lists %d addressable nodes, want none - they are all unnamed",
			pathPrimitiveModes, got)
	}
	for _, desc := range scenePipelines(engine) {
		switch desc.Topology {
		case gfx.TopologyTriangleList, gfx.TopologyLineList:
		default:
			t.Errorf("a scene pipeline assembles as %v; a model mesh is always a list",
				desc.Topology)
		}
	}
	// The POINTS primitive is skipped and the rest of the model still draws.
	var skipped int
	for _, err := range engine.Errors() {
		var primitive model.ErrModelPrimitiveSkipped
		if errors.As(err, &primitive) && primitive.Model == pathPrimitiveModes {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("%s reported %d skipped primitives, want the one POINTS mesh",
			pathPrimitiveModes, skipped)
	}
}

// The line topologies actually reach a pipeline. Six of the seven modes survive
// and two of them are lines, so a frame that built only triangle pipelines
// would mean the loop and the strip were quietly dropped rather than converted.
func TestTheConvertedLinePrimitivesReachALinePipeline(t *testing.T) {
	engine, _ := run(t)
	lines := 0
	for _, desc := range scenePipelines(engine) {
		if desc.Topology == gfx.TopologyLineList {
			lines++
		}
	}
	if lines == 0 {
		t.Errorf("no scene pipeline assembles as a line list, but %s carries LINES, "+
			"LINE_LOOP and LINE_STRIP", pathPrimitiveModes)
	}
}

// The quantised cube dequantises to its plain twin's bounds. This is the second
// WebGPU gap: there are no 3-component 8- or 16-bit vertex formats, so the
// loader widens them at load, and this file's positions are u16 and its normals
// i8.
//
// Bounds is the assertion because it is derived from the POSITION accessor's
// declared min and max rather than from the vertices - so it agrees with the
// plain twin only if the dequantisation used the same scale the file declares.
func TestTheQuantisedCubeDequantisesToItsPlainTwinsBounds(t *testing.T) {
	engine, _ := run(t)
	const plain = "assets/AnimatedMorphCube/AnimatedMorphCube.glb"
	loadNow(t, engine, plain)

	doc := parse(t, pathQuantized)
	if !required(doc, "KHR_mesh_quantization") {
		t.Fatalf("%s no longer requires KHR_mesh_quantization", pathQuantized)
	}
	var quantised, twin m.Box3
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		min, max, _ := la.AABB(model.ModelRef{Path: pathQuantized})
		quantised = m.Box3{Min: min, Max: max}
		min, max, _ = la.AABB(model.ModelRef{Path: plain})
		twin = m.Box3{Min: min, Max: max}
	})
	// A tolerance, not equality: the quantised file stores the same cube in
	// fewer bits, so its declared bounds are the plain one's rounded.
	const tolerance = 1e-2
	if !nearVec(quantised.Min, twin.Min, tolerance) || !nearVec(quantised.Max, twin.Max, tolerance) {
		t.Errorf("the quantised cube is %+v and its plain twin is %+v; a dequantisation "+
			"that used the wrong scale would be off by orders of magnitude, not %v",
			quantised, twin, tolerance)
	}
}

// required reports whether a document lists an extension in extensionsRequired.
func required(doc *gltf.Document, name string) bool {
	for _, ext := range doc.ExtensionsRequired {
		if ext == name {
			return true
		}
	}
	return false
}

// A model whose indices are eight bits wide draws every one of them. This is
// the first WebGPU gap: there is no uint8 index format, so the loader widens
// what the file used, and the count that reaches the GPU is the file's own.
//
// The model is drawn alone in its own engine so that every scene draw in the
// frame belongs to it, which is what lets an index count be attributed at all.
// The expected number is summed out of the file rather than typed: a
// transcribed table has been the wrong thing here before.
func TestAnEightBitIndexedModelDrawsTheFilesOwnIndexCount(t *testing.T) {
	mount, err := assets.Mount()
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	engine := headless.New(t, headless.Mounting(mount), &soloDemo{path: pathNarrowIndices})
	loadNow(t, engine, pathNarrowIndices)

	doc := parse(t, pathNarrowIndices)
	want, narrow := flattenedIndices(t, doc)
	if narrow == 0 {
		t.Fatalf("%s no longer carries any u8 index accessor, so it cannot exercise "+
			"the widening any more", pathNarrowIndices)
	}
	f := step(engine)
	backend := engine.Backend()
	got := 0
	for _, draw := range f.draws {
		if !backend.IsScenePipeline(draw.Pipeline) {
			continue
		}
		if !draw.Indexed {
			t.Error("a draw of an indexed model is not indexed")
		}
		// scene draws the nodes that share a mesh as one instanced draw, so a
		// draw's indices are its count once per instance.
		got += draw.Count * draw.Instances
	}
	if got != want {
		t.Errorf("the frame drew %d indices, want the %d the file's own accessors "+
			"declare", got, want)
	}
}

// flattenedIndices is how many indices a document's default scene draws, walking
// its nodes the way the loader flattens them, and how many of its index
// accessors are eight bits wide.
//
// Per node rather than per mesh, because that is what reaches the GPU: this
// file's ten nodes share two meshes, so a sum over doc.Meshes is short by a
// factor of eight and would make the assertion pass on the wrong number.
func flattenedIndices(t *testing.T, doc *gltf.Document) (total, narrow int) {
	t.Helper()
	var walk func(index int)
	walk = func(index int) {
		node := doc.Nodes[index]
		if node.Mesh != nil {
			for _, primitive := range doc.Meshes[*node.Mesh].Primitives {
				if primitive.Indices == nil {
					continue
				}
				accessor := doc.Accessors[*primitive.Indices]
				total += int(accessor.Count)
				if accessor.ComponentType == gltf.ComponentUbyte {
					narrow++
				}
			}
		}
		for _, child := range node.Children {
			walk(int(child))
		}
	}
	for _, root := range doc.Scenes[*doc.Scene].Nodes {
		walk(int(root))
	}
	return total, narrow
}

// soloDemo spawns one model at the origin, or several side by side, and a
// camera, and nothing else, so a test can attribute what reached the GPU to it.
type soloDemo struct {
	path   string
	copies int
}

type (
	soloEye struct {
		Place  m.Transform
		Camera scene.Camera
	}
	soloModel struct {
		Place m.Transform
		Model scene.Model
	}
	soloSetup kernel.Subscription[app.InitEvent]
)

func (*soloDemo) Name() kernel.PluginName { return "solo" }

func (*soloDemo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name, storage.Name}
}

func (d *soloDemo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[soloSetup](ecs.ToHandler[app.InitEvent](registrar,
		func(eyes *ecs.Spawn[soloEye], models *ecs.Spawn[soloModel]) {
			eyes.New(soloEye{
				Place: m.LookAt(m.Vec3{Y: 4, Z: 20}, m.Vec3{}, m.Vec3{Y: 1}),
				Camera: scene.Camera{
					ID: CameraMain, FovY: fieldOfViewY, Near: nearPlane, Far: farPlane,
				},
			})
			// Side by side about the origin, so every copy is in view and a
			// count of instances is a count of copies.
			copies := max(d.copies, 1)
			for i := range copies {
				models.New(soloModel{
					Place: m.At((float32(i)-float32(copies-1)/2)*6, 0, 0),
					Model: scene.Model{Ref: model.ModelRef{Path: d.path}},
				})
			}
		}))
	return nil
}

// parse reads one vendored file's glTF document, so an expectation can be
// summed out of the asset rather than transcribed beside it.
func parse(t *testing.T, path string) *gltf.Document {
	t.Helper()
	dir, err := assets.Locate()
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	doc, err := gltf.Open(filepath.Join(filepath.Dir(dir), path))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return doc
}

// nodeNamed finds one node of a document by name, first match, which is the
// same rule the Node selector uses.
func nodeNamed(t *testing.T, doc *gltf.Document, name string) *gltf.Node {
	t.Helper()
	for _, node := range doc.Nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("no node named %q", name)
	return nil
}

// meshBox is one mesh's own local-space box, from its primitives' POSITION
// accessors' declared min and max - which is the same source model's own
// bounds come from, and glTF requires them on POSITION.
func meshBox(t *testing.T, doc *gltf.Document, mesh *int) m.Box3 {
	t.Helper()
	if mesh == nil {
		t.Fatal("the node carries no mesh")
	}
	var box m.Box3
	first := true
	for _, primitive := range doc.Meshes[*mesh].Primitives {
		accessor := doc.Accessors[primitive.Attributes[gltf.POSITION]]
		if len(accessor.Min) < 3 || len(accessor.Max) < 3 {
			t.Fatal("the POSITION accessor declares no min/max")
		}
		one := m.Box3{Min: vec3(accessor.Min), Max: vec3(accessor.Max)}
		if first {
			box, first = one, false
			continue
		}
		box = box.Union(one)
	}
	return box
}

// vec3 is a glTF accessor bound, which the decoder reports as float64.
func vec3(v []float64) m.Vec3 {
	return m.Vec3{X: float32(v[0]), Y: float32(v[1]), Z: float32(v[2])}
}

func near(a, b, tolerance float32) bool {
	d := a - b
	return d < tolerance && d > -tolerance
}

func nearVec(a, b m.Vec3, tolerance float32) bool {
	return near(a.X, b.X, tolerance) && near(a.Y, b.Y, tolerance) && near(a.Z, b.Z, tolerance)
}

// Every station's note fits the column the HUD gives it, and every name fits
// its own.
//
// This is a test rather than a formatting rule because the failure is invisible
// in source and unmistakable on screen: canvas's text does not wrap, so a note
// one word too long draws straight through the column beside it and takes two
// rows of the residency table with it. This demo's HUD is its actual output - a
// grid of pads with five bare ones says nothing on its own - so an unreadable
// HUD is an unreadable demo. It went wrong once already, and only a capture at
// full resolution showed it.
func TestEveryStationNoteFitsItsHudColumn(t *testing.T) {
	for i := range stations {
		if got := len(stations[i].note); got > NoteBudget {
			t.Errorf("station %q: note is %d characters, budget is %d:\n  %s",
				stations[i].name, got, NoteBudget, stations[i].note)
		}
		if got := len(stations[i].name); got > hudNamePad {
			t.Errorf("station %q: name is %d characters, the column is %d wide",
				stations[i].name, got, hudNamePad)
		}
	}
	// Six logical pixels per character is canvas's embedded font at this size,
	// near enough. The point is that the budget is derived from the column
	// rather than guessed beside it, so moving either moves the other.
	const pixelsPerCharacter = 6
	if width := (hudFixed + NoteBudget) * pixelsPerCharacter; width > hudColumn {
		t.Errorf("a full-width line is about %d logical pixels, and the second "+
			"column starts at %d", width, hudColumn)
	}
	if width := hudColumn + (hudFixed+NoteBudget)*pixelsPerCharacter; width > screenWidth {
		t.Errorf("the second column ends at about %d, past the %d-wide screen",
			width, screenWidth)
	}
}
