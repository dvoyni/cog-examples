package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
	"github.com/qmuntal/gltf"
)

// This file is where most of this demo lives. The grid is nearly all assertion
// and almost no picture on purpose: loading and the lookup facade are the two
// contracts whose failures are invisible in a frame, so the frame is the
// smaller half of the demonstration and this is the larger one.

// run starts the demo headless over the vendored asset set and steps until
// every station has reached the residency its table row expects - six resident,
// three failed - which is the frame the reference screenshot shows.
//
// The wait is wall clock rather than a frame count on purpose: a load does not
// run on the frame's thread, and decoding TextureSettingsTest's images takes
// longer than a few hundred headless frames of doing nothing else. That is
// exactly the hitch the asynchronous path exists to keep out of the frame, and
// a test that waited in frames would be asserting it does not exist.
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
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	demo := New()
	return headless.NewOver(t, config, demo), demo
}

// settle steps until every station has reached its expected residency, then
// twice more: the frame that first sees a model resident is also the first to
// record its draws, and Passes publishes the frame the last flush consumed.
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

// unsettled names the stations that have not reached their expected residency,
// so a timeout says which rather than only that one did.
func unsettled(demo *Loading) string {
	out := ""
	for i := range stations {
		if demo.State(i) != stations[i].expect {
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

// Every station reaches the residency its table row expects, and the three that
// fail are the three that were meant to. This is the assertion the whole demo
// rests on: a grid where the wrong pad is bare still looks like a grid.
func TestEveryStationReachesTheResidencyItsTableExpects(t *testing.T) {
	_, demo := run(t)
	for i := range stations {
		if got := demo.State(i); got != stations[i].expect {
			t.Errorf("station %q is %s, want %s",
				stations[i].name, stateName(got), stateName(stations[i].expect))
		}
	}
}

// Every pad is drawn and only a station whose selector resolved on a resident
// model draws a model. Five pads are bare, and a count is the only thing that
// separates "skipped" from "drawn somewhere off-screen".
func TestEveryPadIsDrawnAndOnlyResolvedStationsDrawAModel(t *testing.T) {
	engine, _ := run(t)
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
	if ops := len(engine.Ops()); ops != RecordedOps {
		t.Errorf("reported %d ops, want %d", ops, RecordedOps)
	}
}

// Nothing is substituted for a model that is not resident. On the first frame
// nothing has loaded, so the only draws are the sixteen pads - not a box, not a
// smaller model, not the last model that did load.
//
// This is the contract that cannot be seen any other way. A substitute would
// render a plausible frame, and every count in it would look reasonable.
func TestNothingIsSubstitutedForAModelThatIsNotResident(t *testing.T) {
	engine, demo := start(t)
	// Every frame from the first to settled. On each one the draw count has to
	// be at most the pads plus the primitives of the stations resident on that
	// frame. A substitute is the one thing that could push it over, and it
	// would push it over on precisely the frames this loop covers and no
	// others.
	//
	// Both numbers come from LastFlush, which reads them together. Taking them
	// separately - the count from the queue, the residency from the demo's own
	// table - compares two different frames, because a load installs at a frame
	// boundary that falls between the two reads; that skew failed this
	// assertion about one run in nine with nothing substituted anywhere.
	deadline := time.Now().Add(60 * time.Second)
	incomplete := 0
	for frames := 0; !demo.Settled(); frames++ {
		if time.Now().After(deadline) {
			t.Fatalf("stations never settled: %s", unsettled(demo))
		}
		engine.Steps(1)
		incomplete += checkNothingSubstituted(t, demo, frames)
		time.Sleep(time.Millisecond)
	}
	// The loop's last flush is described by the update after it, so one more
	// step is what brings the final unsettled frame - the one with the most
	// resident stations and so the tightest bound of all of them - into view.
	engine.Steps(1)
	incomplete += checkNothingSubstituted(t, demo, -1)
	if incomplete == 0 {
		t.Error("every station was resident on the very first frame, so this test saw " +
			"no frame in which anything could have been substituted")
	}
}

// checkNothingSubstituted holds the bound for the frame the last flush
// consumed, and returns 1 if that frame was one in which a substitution was
// possible at all - that is, one with a station still not resident.
func checkNothingSubstituted(t *testing.T, demo *Loading, frame int) int {
	t.Helper()
	recorded, residency, ok := demo.LastFlush()
	if !ok {
		return 0
	}
	want, resident := len(stations), 0
	for i := range stations {
		if residency[i] {
			want += stations[i].draws
			resident++
		}
	}
	if recorded > want {
		t.Fatalf("frame %d recorded %d draws with %d of %d stations resident; "+
			"at most %d can be real, so something was substituted",
			frame, recorded, resident, len(stations), want)
	}
	if resident < len(stations) {
		return 1
	}
	return 0
}

// Preload names every path the grid draws. A station whose path was missing
// here would load off its first draw instead, which is a different code path
// and a silent one: the picture would be identical and the hitch would have
// moved back into the frame.
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

// Preload makes a model resident with no draw of it anywhere. It is the same
// idempotent load a draw fires, fired without one, which is the whole of moving
// a decode into a loading screen the app controls.
func TestPreloadMakesAModelResidentWithNoDrawOfIt(t *testing.T) {
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	// No demo plugin: nothing in this engine records a single op.
	engine := headless.NewOver(t, config)
	engine.Lookup(func(la scene.LookupAccess) { la.Preload(pathQuantized) })
	waitFor(t, engine, pathQuantized, scene.ModelResident)
	if passes := engine.Passes(); len(passes) != 0 {
		t.Fatalf("published %d passes with nothing recording, want none", len(passes))
	}
}

// waitFor steps until one path reaches a state, or fails the test.
func waitFor(t *testing.T, engine *headless.Engine, path string, want scene.ModelState) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var got scene.ModelState
		engine.Lookup(func(la scene.LookupAccess) { got = la.State(path) })
		if got == want {
			// Three more frames. Residency flips inside the command that
			// installs the model, and the bakes it queued reach the backend on
			// the render after that - so a test that measured here would count
			// the uploads of the frame before.
			engine.Steps(3)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is %s after 60s, want %s", path, stateName(got), stateName(want))
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
}

// The two failure modes are told apart by when they happen, not by their type.
//
// An invalid path is failed synchronously, inside the very call that asked: it
// never reaches a load command at all, so the report-from-the-goroutine rule
// cannot see it and without this every query on it would return a silent false
// forever. A file that is truncated, and a file that is not there, both open a
// load command and fail inside it, a frame or more later.
//
// This corrects what this demo's ticket asked for. #97 expected the
// non-existent path to be the synchronous one; it is not, and cannot be. The
// mount answers fs.ErrNotExist for it - which is also what lets storage fall
// through to the next mount - and finding that out means looking at the
// filesystem, which is what the load command is for.
func TestAnInvalidPathFailsSynchronouslyAndABrokenFileAsynchronously(t *testing.T) {
	engine, demo := start(t)
	var invalid, truncated, absent scene.ModelState
	engine.Lookup(func(la scene.LookupAccess) {
		invalid = la.State(pathInvalid)
		truncated = la.State(pathTruncated)
		absent = la.State(pathMissing)
	})
	if invalid != scene.ModelFailed {
		t.Errorf("the first State of an invalid path is %s, want %s - it is refused "+
			"before a load is enqueued", stateName(invalid), stateName(scene.ModelFailed))
	}
	for name, state := range map[string]scene.ModelState{
		"truncated": truncated, "absent": absent,
	} {
		if state != scene.ModelLoading {
			t.Errorf("the first State of the %s file is %s, want %s - the load has to "+
				"look at the filesystem before it can say", name, stateName(state),
				stateName(scene.ModelLoading))
		}
	}
	settle(t, engine, demo)
	for _, path := range []string{pathTruncated, pathMissing} {
		var state scene.ModelState
		engine.Lookup(func(la scene.LookupAccess) { state = la.State(path) })
		if state != scene.ModelFailed {
			t.Errorf("%s settled at %s, want %s", path, stateName(state),
				stateName(scene.ModelFailed))
		}
	}
	// The two asynchronous failures are the same class through State and
	// distinguishable only by what they wrap, which is the honest report of
	// what the API can tell a caller.
	var notExist, unexpectedEOF bool
	for _, err := range engine.Errors() {
		var unavailable scene.ErrModelUnavailable
		if !errors.As(err, &unavailable) {
			continue
		}
		switch unavailable.Model {
		case pathMissing:
			notExist = errors.Is(err, fs.ErrNotExist)
		case pathTruncated:
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

// reportsFor counts how many times one path has been reported unloadable.
func reportsFor(engine *headless.Engine, path string) int {
	count := 0
	for _, err := range engine.Errors() {
		var unavailable scene.ErrModelUnavailable
		if errors.As(err, &unavailable) && unavailable.Model == path {
			count++
		}
	}
	return count
}

// settle without a demo waits on the three broken paths alone.
func settleBroken(t *testing.T, engine *headless.Engine) {
	t.Helper()
	for _, path := range []string{pathTruncated, pathMissing} {
		waitFor(t, engine, path, scene.ModelFailed)
	}
}

// A failed path never retries, and unload is the only lever that clears it.
// A typo'd path drawn every frame must not spawn a load command every frame
// forever, so failure is terminal - and a Retry that did not first free would
// be a second name for the idempotent load that already exists.
func TestAFailedPathNeverRetriesAndUnloadIsTheOnlyLever(t *testing.T) {
	engine, demo := run(t)
	// Thirty frames drawing three failed paths, and none goes back to loading.
	// A count of baked buffers cannot say this - canvas bakes the HUD's text
	// every frame - but the state machine can: a path that retried would have
	// to pass through ModelLoading to do it.
	for frame := range 30 {
		engine.Steps(1)
		for _, path := range []string{pathTruncated, pathMissing, pathInvalid} {
			var state scene.ModelState
			engine.Lookup(func(la scene.LookupAccess) { state = la.State(path) })
			if state != scene.ModelFailed {
				t.Fatalf("on frame %d %s is %s, want %s: failure is terminal and clears "+
					"only on unload", frame, path, stateName(state),
					stateName(scene.ModelFailed))
			}
		}
	}
	// The demo's own retry lever: unload, then preload. All three fail again,
	// because all three are still broken - but they went back through loading
	// to get there, which is what says the unload cleared them.
	// The observable is a second report, not a second ModelLoading: reports are
	// keyed and fired once, and the key is cleared on unload, so the truncated
	// file reporting twice is the proof the unload actually reset the slot. The
	// state cannot say it - this file fails so fast that a poll a frame later
	// has already missed the loading it passed through.
	before := reportsFor(engine, pathTruncated)
	demo.Retry()
	// The demo queues the unload and lets the next frame's draw ask again, and
	// it has to: an unload is applied at the frame boundary, so a Preload
	// issued in the same handler still sees the failed entry and does nothing
	// at all. A retry has to straddle the boundary.
	engine.Steps(4)
	settleBroken(t, engine)
	if after := reportsFor(engine, pathTruncated); after != before+1 {
		t.Errorf("the truncated file reported %d times before the retry and %d after, "+
			"want exactly one more: unload is what clears a failed path", before, after)
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
		ref  scene.ModelRef
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
		engine.Lookup(func(la scene.LookupAccess) { got, ok = la.Nodes(c.ref, nil) })
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
	for _, ref := range []scene.ModelRef{
		{Path: pathTruncated},
		{Path: pathMissing},
		{Path: pathInvalid},
		stations[stationNoSuchScene].ref(),
		stations[stationNoSuchNode].ref(),
	} {
		engine.Lookup(func(la scene.LookupAccess) {
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
		engine.Lookup(func(la scene.LookupAccess) { min, max, ok = la.AABB(station.ref()) })
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
		engine.Lookup(func(la scene.LookupAccess) {
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

// A Node draw discards the scene root's transform and an empty Node keeps it.
// On this asset that is a whole axis: the scene's only root is Yup2Zup, whose
// rotation is what stands the truck up, so the body drawn through a Node
// selector lies on its side and the same subtree drawn as the scene does not.
func TestAWholeSceneDrawKeepsTheRootTransformAndANodeDrawDiscardsIt(t *testing.T) {
	engine, _ := run(t)
	var whole, body m.Box3
	engine.Lookup(func(la scene.LookupAccess) {
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
// the same draw the default would - which on this asset set is the only shape a
// matching Scene can take. Six vendored assets name their scene "Scene", and in
// every one of them that scene is also the declared default; MultipleScenes,
// the repository's only file with more than one scenes entry, leaves both of
// its unnamed. So selecting a non-default scene has no asset behind it and is
// recorded here as a gap rather than designed around.
func TestAMatchedSceneSelectorResolvesToTheSameDrawAsTheDefault(t *testing.T) {
	engine, _ := run(t)
	var named, byDefault m.Box3
	var namedOK, defaultOK bool
	engine.Lookup(func(la scene.LookupAccess) {
		min, max, ok := la.AABB(scene.ModelRef{Path: pathTruck, Scene: "Scene"})
		named, namedOK = m.Box3{Min: min, Max: max}, ok
		min, max, ok = la.AABB(scene.ModelRef{Path: pathTruck})
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
		var missingScene scene.ErrModelSceneMissing
		if errors.As(err, &missingScene) && missingScene.Scene == stations[stationNoSuchScene].scene {
			sceneReports++
		}
		var missingNode scene.ErrModelNodeMissing
		if errors.As(err, &missingNode) && missingNode.Node == stations[stationNoSuchNode].node {
			nodeReports++
		}
	}
	if sceneReports != 1 {
		t.Errorf("the unmatched Scene reported %d times over ~%d frames, want once",
			sceneReports, demo.step)
	}
	if nodeReports != 1 {
		t.Errorf("the unmatched Node reported %d times over ~%d frames, want once",
			nodeReports, demo.step)
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
	// canvas's font atlas and scene's two 1x1 defaults, and a total that had to
	// subtract them would be an assertion about the subtraction.
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	engine := headless.NewOver(t, config)
	engine.Lookup(func(la scene.LookupAccess) { la.Preload(pathSamplers) })
	waitFor(t, engine, pathSamplers, scene.ModelResident)
	backend := engine.Backend()
	// Every model texture is baked with a mip chain and scene's own two 1x1
	// defaults are not, so the mipped count is the model's alone and needs no
	// subtraction to say so.
	if got := backend.MippedTextures; got != len(doc.Images) {
		t.Errorf("baked %d model textures for a file with %d images and %d glTF "+
			"textures over %d samplers, want %d: the cache is keyed by resolved "+
			"path and colour space, not by glTF texture",
			got, len(doc.Images), len(doc.Textures), len(doc.Samplers), len(doc.Images))
	}
	if got := backend.BakedTextures; got != len(doc.Images)+defaultTextures {
		t.Errorf("baked %d textures in all, want %d model textures plus scene's %d "+
			"1x1 defaults", got, len(doc.Images), defaultTextures)
	}
}

// Drawing one model six times bakes its texture once. It is the other of the
// two honest exercises of the path-keyed cache, and the one that says the key
// is the path rather than the draw.
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
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	// The same number of draws the grid makes, alone in an engine with no HUD,
	// so the mipped count is the truck's and needs no subtraction.
	engine := headless.NewOver(t, config, &soloDemo{path: pathTruck, copies: copies})
	waitFor(t, engine, pathTruck, scene.ModelResident)
	truck := parse(t, pathTruck)
	if got := engine.Backend().MippedTextures; got != len(truck.Images) {
		t.Errorf("%d draws of one model baked %d textures, want %d - one per image, "+
			"because a model's cache key is its path", copies, got, len(truck.Images))
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
	// Scene's own two 1x1 defaults are in the total and are deliberately not
	// mipped: for a constant texel every level is identical, so there is
	// nothing to generate. Canvas's font atlas is in it too. So the claim here
	// is a floor - every model texture - rather than an equality.
	images := len(parse(t, pathSamplers).Images) + len(parse(t, pathTruck).Images)
	if backend.MippedTextures < images {
		t.Errorf("%d of %d baked textures carry a mip chain, want at least the %d "+
			"images the grid's two textured files hold",
			backend.MippedTextures, backend.BakedTextures, images)
	}
}

// defaultTextures is how many 1x1 textures scene bakes for itself: the white
// texel every empty colour slot binds and the flat normal every empty normal
// slot binds.
const defaultTextures = 2

// Unloading a model does not cascade to its textures. With no refcount the
// lookup cannot know whether another resident model binds the same image by
// path, and freeing one that is still bound is a dead texture in a live bind
// group rather than a missing picture - so UnloadTexture is the separate,
// deliberate lever.
//
// The observable is that the reload the next frame's draws trigger bakes no new
// texture: the geometry came back and the image never left.
func TestUnloadingAModelDoesNotCascadeToItsTextures(t *testing.T) {
	engine, demo := run(t)
	backend := engine.Backend()
	before := backend.BakedTextures
	poseBefore := demo.TotalPoseBytes()
	if poseBefore == 0 {
		t.Fatal("no baked poses to lose; the truck's own clip should make some")
	}

	demo.UnloadModel()
	// The unload lands at the next frame boundary, so the frame that asked
	// still draws what it always drew. The frame after that finds the slot
	// reset and reloads it, because a later draw of an unloaded path reloads.
	engine.Steps(2)
	if demo.State(stationScene) == scene.ModelResident {
		t.Error("the truck was still resident two frames after UnloadModel")
	}
	settle(t, engine, demo)
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

// UnloadAll gives up every resident model and every cached texture at once. It
// is the level teardown, and it is a flag rather than a walk because the table
// it would walk can still change before the boundary arrives.
func TestUnloadAllReleasesEveryResidentModel(t *testing.T) {
	engine, demo := run(t)
	if demo.TotalPoseBytes() == 0 {
		t.Fatal("nothing resident to release")
	}
	demo.UnloadAll()
	engine.Steps(2)
	resident := 0
	for i := range stations {
		if demo.State(i) == scene.ModelResident {
			resident++
		}
	}
	if resident == len(stations) {
		t.Error("every station was still resident two frames after UnloadAll")
	}
	settle(t, engine, demo)
}

// A replacement Material binds neither the file's record nor its textures, and
// OverrideParams keeps both. The two stations draw the same subtree of the same
// file, so everything that differs between their batches is the material.
//
// The observable is the material id: scene interns a material by content, so
// the repainted station's batches carry an id no other station's do, and the
// tinted station's carry the file's own - a merge is a copy of the record, not
// a different material.
func TestAReplacementMaterialIsADifferentMaterialAndAMergeIsNot(t *testing.T) {
	engine, _ := run(t)
	batches := pass(t, engine).Batches
	pipelines := scenePipelines(engine)
	if len(pipelines) < 2 {
		t.Fatalf("the frame built %d scene-shaded pipelines, want at least two - one for "+
			"the bundled PBR and one for the replacement", len(pipelines))
	}
	// The replacement's own shader is inline text rather than a resource path,
	// which is how a test tells a demo's WGSL from a bundled shader.
	backend := engine.Backend()
	replacement := 0
	for _, desc := range backend.Pipelines {
		if backend.ShaderPath(desc.Shader) != headless.SceneShaderPath {
			replacement++
		}
	}
	if replacement == 0 {
		t.Error("no pipeline was built from a shader that is not the bundled scene one, " +
			"so the replacement Material never reached the GPU")
	}
	if len(batches) == 0 {
		t.Fatal("no batches")
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
		var primitive scene.ErrModelPrimitiveSkipped
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
	engine.Lookup(func(la scene.LookupAccess) { la.Preload(plain) })
	waitFor(t, engine, plain, scene.ModelResident)

	doc := parse(t, pathQuantized)
	if !required(doc, "KHR_mesh_quantization") {
		t.Fatalf("%s no longer requires KHR_mesh_quantization", pathQuantized)
	}
	var quantised, twin m.Box3
	engine.Lookup(func(la scene.LookupAccess) {
		min, max, _ := la.AABB(scene.ModelRef{Path: pathQuantized})
		quantised = m.Box3{Min: min, Max: max}
		min, max, _ = la.AABB(scene.ModelRef{Path: plain})
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
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	solo := &soloDemo{path: pathNarrowIndices}
	engine := headless.NewOver(t, config, solo)
	waitFor(t, engine, pathNarrowIndices, scene.ModelResident)
	engine.Steps(2)

	doc := parse(t, pathNarrowIndices)
	want, narrow := flattenedIndices(t, doc)
	if narrow == 0 {
		t.Fatalf("%s no longer carries any u8 index accessor, so it cannot exercise "+
			"the widening any more", pathNarrowIndices)
	}
	backend := engine.Backend()
	got := 0
	for _, draw := range backend.Draws {
		if !backend.IsScenePipeline(draw.Pipeline) {
			continue
		}
		if !draw.Indexed {
			t.Error("a draw of an indexed model is not indexed")
		}
		got += draw.Count * draw.Instances
	}
	// Every frame's draws accumulate in the backend since the engine started,
	// so the total is a whole number of frames' worth of the file's own count.
	if want == 0 || got == 0 || got%want != 0 {
		t.Errorf("the frames drew %d indices in total, want a multiple of the %d the "+
			"file's own accessors declare", got, want)
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

// soloDemo draws one model at the origin and nothing else, so a test can
// attribute what reached the GPU to it.
type soloDemo struct {
	path   string
	copies int
}

func (*soloDemo) Name() kernel.PluginName { return "solo" }

func (*soloDemo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name, storage.Name}
}

func (d *soloDemo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[updateEventHandler](d.draw)
	return nil
}

func (d *soloDemo) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			q := sceneQueue.Get()
			q.Camera(CameraMain, scene.CameraDescr{
				Transform: scene.LookAt(m.Vec3{Y: 4, Z: 20}, m.Vec3{}, m.Vec3{Y: 1}),
				FovY:      fieldOfViewY, Near: nearPlane, Far: farPlane,
			})
			for i := range max(d.copies, 1) {
				q.Model(0, d.path, scene.ModelDraw{
					Transform: scene.At(float32(i)*6, 0, 0),
				})
			}
			return nil
		}
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
// accessors' declared min and max - which is the same source scene's own bounds
// come from, and glTF requires them on POSITION.
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
