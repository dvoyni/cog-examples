// Command loading is the scene plugin's demo of everything that happens
// between naming a file and drawing it: model addressing, synchronous
// residency and its cost, and the lookup facade.
//
//	go run ./cmd/scene/loading
//
// It is nearly all assertion and almost no picture, and it is kept in the set
// for exactly that reason. Loading and the lookup facade are the two contracts
// whose failures are invisible - a model that silently substitutes, a node that
// silently falls back to the whole scene, a texture decoded twice - and nothing
// else in the demo set would ever see them. The picture here is a grid of
// sixteen slots, and the interesting slots are the empty ones.
//
// # The grid
//
// Sixteen pads in four rows, and one Model Entity standing on each, whether or
// not there is anything to draw. Every pad is drawn unconditionally: a station
// whose model failed, or whose selector matched nothing, shows a bare pad,
// because scene's load System leaves such a Model unkeyed and scene draws
// nothing for it rather than substituting anything. That is the whole of
// "skip, never substitute", and the pad is what makes it visible rather than
// merely absent.
//
//   - Row 1, addressing: the whole truck through a Scene selector that matches,
//     then its body and each of its two wheel pairs through a Node selector
//     that re-roots.
//   - Row 2, materials and selectors: the body tinted through a Params
//     Component and the body repainted through a Material whose shader reads
//     none of the file's bindings, then MultipleScenes at its declared default
//     and MultipleScenes under a Scene name no file in the repository carries.
//   - Row 3, the four WebGPU gaps the loader papers over: nine textures over
//     three images through five samplers, a file with all seven primitive modes
//     in it, a quantised mesh, and a model whose indices are eight bits wide.
//   - Row 4, the failures: a node name that matches nothing, a file that is
//     truncated, a file that is not there, and a path that is not a path.
//
// # What only eyes can judge
//
// The four trucks in row 1 differ, and each difference is a contract. The first
// stands upright because its scene's Yup2Zup root is kept; the second lies on
// its side because a Node selector discards that root's rotation and replaces
// it with the Entity's own Transform. The last two are wheel *pairs* - the file
// has one mesh for an axle and hangs it off two nodes - and they are identical
// to each other and centred on their pads, standing on end because the axle
// runs along the model's own Y once Yup2Zup is gone. That they are identical is
// the assertion: they are the same mesh under the same local rotation hanging
// off two parents with different offsets, so a re-root that left any ancestor's
// transform in would put them in two different places.
//
// In row 2 the tinted body keeps its livery under a colour wash, and the
// repainted one is flat grey with no livery at all. One sentence, two
// contracts: Params lay a colour over the file's own material by name and keep
// its textures, and a Material whose shader declares none of the file's
// bindings never reads them, so its base colours, factors and texture
// transforms do not survive.
//
// The four pads in row 4 are bare, and one pad in row 2 is bare. Five empty
// slots is the correct picture.
//
// # Keys
//
// The frame holds still, so the reference capture is taken by launching and
// waiting for residency. The residency itself is what moves, and the keys are
// how a human moves it:
//
//	u  unload the truck's geometry - its textures stay cached, so the reload
//	   that follows bakes no new texture
//	t  unload the truck's textures too, which is the separate, deliberate lever
//	x  unload everything
//	r  unload the three failed paths and preload them again, which is the only
//	   retry there is - and all three fail again, because they are still broken
//
// An unload is followed by a reload in the same tick: the unload System marks
// every Model naming the path changed, and scene's load System, which runs on
// what changed, loads the file again. A free followed by a touch is a reload.
//
// # The reference pose
//
// A documented fixed camera pose, no orbit and no clock in the frame. Demo time
// is accumulated fixed steps all the same, because the HUD prints it and
// because a demo whose numbers came off the wall clock could not be captured
// twice. reference.png beside this file is the frame once every station has
// settled.
//
// # Systems
//
// Each System is in the file named for it, and the one Component the demo
// declares, Station, is in components.go:
//
//	setup     bakes the pad, spawns the camera, the pads and the stations  (init)
//	advance   steps the demo clock and turns key presses into levers
//	unload    pulls the levers, and marks the Models they freed changed
//	preload   asks for every path once, before scene's load System does
//	survey    asks the facade what it knows, for the HUD and the tests
//	hud       prints the residency table
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/mcp/mcpplugin"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/sceneplugin"
	"github.com/dvoyni/cog/extensions/gogpu"
	"github.com/dvoyni/cog/extensions/gogpu/gogpuplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// The logical screen the HUD is laid out in, and the window size the reference
// screenshot is taken at. The HUD here is taller than any other demo's - it
// prints a residency line per station - so the window is taller too.
const (
	screenWidth  = 1100
	screenHeight = 720
	windowWidth  = screenWidth
	windowHeight = screenHeight
)

// CameraMain is the demo's only camera, at a negative id so it sorts below the
// HUD and above the backdrop that clears the frame.
const CameraMain scene.CameraID = -100

// The canvas layers, which are gfx orders directly. The camera declares no
// passes, and its one default pass preserves colour rather than clearing it,
// so the frame's one colour clear is canvas's on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.
			WithTitle("cog examples: scene loading").
			// Launched at the size the reference screenshot was taken at rather
			// than resized into it: a runtime resize leaves the viewport
			// un-refitted.
			WithSize(windowWidth, windowHeight),
	}
	permanentfs.Configure(config)

	// The demo plugin is last because its Systems order themselves against
	// scene's, which have to be registered first.
	demo := New()
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		modelplugin.New(),
		gogpuplugin.New(),
		ecsplugin.New(), sceneplugin.New(),
		mcpplugin.New(),
		demo,
	}

	engine := kernel.New(config).Handler(demo.report).WithPlugins(plugins...)
	// Ctrl+C asks the host to leave its loop, the same way closing the window
	// does.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		engine.Quit()
	}()
	if err := engine.Run(); err != nil {
		// A composition that failed, or a report the error handler terminated
		// on, ends Run with its cause. Say why, and fail the process.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// report is the demo's error handler, and this is the demo that needs one most.
//
// The kernel's default handler logs and returns true, which terminates the
// engine: a cog app that reports anything at all stops. That is the right
// default, because most reports are bugs. Five of this demo's sixteen stations
// exist to provoke one, so under the default it would shut down on the first
// frame a load completed.
//
// The allow-list is named rather than classed. Every entry is matched against
// the station table's own paths and selectors, so a report about a file this
// demo did not deliberately break still terminates - which is the difference
// between a demo that survives its own failures and one that cannot tell you
// the loader stopped working.
func (p *Loading) report(err error) error {
	if expected, why := p.expectedReport(err); expected {
		log.Printf("loading: %v (expected: %s)", err, why)
		return nil
	}
	log.Printf("loading: %v", err)
	return err
}

// expectedReport reports whether err is one of the five this demo provokes on
// purpose, and what provoked it.
//
// It matches on the error's own fields rather than on its text wherever it can:
// model's own errors carry the path and the selector that produced them, which
// is exactly what an allow-list needs to stay narrow.
//
// The one exception is the failed read. The asset library performs the read
// itself and reports its own failure, so what reaches here is a wrapped
// fs.ErrNotExist naming the path in its message and nothing else - which is why
// this one entry matches on the text, and matches it against the station
// table's own paths so it stays as narrow as the rest.
func (p *Loading) expectedReport(err error) (bool, string) {
	if errors.Is(err, fs.ErrNotExist) {
		for i := range stations {
			if !stations[i].loads && strings.Contains(err.Error(), stations[i].path) {
				return true, stations[i].name + " names a file that is not there, on purpose"
			}
		}
	}
	var unavailable model.ErrModelUnavailable
	if errors.As(err, &unavailable) {
		if station, ok := stationForPath(unavailable.Model); ok && !station.loads {
			return true, station.name + " names a file that cannot be loaded, on purpose"
		}
	}
	var invalid model.ErrModelPathInvalid
	if errors.As(err, &invalid) {
		if station, ok := stationForPath(invalid.Model); ok && !station.loads {
			return true, station.name + " names a path that is not a resource path at all"
		}
	}
	var sceneMissing model.ErrModelSceneMissing
	if errors.As(err, &sceneMissing) {
		if stationForSelector(sceneMissing.Model, sceneMissing.Scene, "") {
			return true, "the unmatched Scene selector reports once and draws nothing"
		}
	}
	var nodeMissing model.ErrModelNodeMissing
	if errors.As(err, &nodeMissing) {
		if stationForSelector(nodeMissing.Model, nodeMissing.Scene, nodeMissing.Node) {
			return true, "the unmatched Node selector reports once and draws nothing"
		}
	}
	var skipped model.ErrModelPrimitiveSkipped
	if errors.As(err, &skipped) && skipped.Model == pathPrimitiveModes {
		return true, "MeshPrimitiveModes carries a POINTS primitive, which has no gfx topology"
	}
	return false, ""
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "loading"

// The demo's Systems and its one plain handler. Each System is in the file
// named for it.
type (
	setupSystem   kernel.Subscription[app.InitEvent]
	advanceSystem kernel.Subscription[app.UpdateEvent]
	unloadSystem  kernel.Subscription[app.UpdateEvent]
	preloadSystem kernel.Subscription[app.UpdateEvent]
	surveySystem  kernel.Subscription[app.UpdateEvent]
	hudSystem     kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock: the update event's Dt is deliberately ignored. Nothing in the frame
// moves with it - the clock is here so the HUD can say how long a load took in
// a unit that is the same on every machine.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The vendored files this demo names, and the two that are not files at all.
//
// They are constants rather than string literals in the station table because
// six stations share the truck: the texture cache cannot be exercised across
// two models - every model owns a private directory, so no two files ever
// resolve to the same path - and drawing one model six times is one of the only
// two honest exercises of it there are.
const (
	pathTruck          = "assets/CesiumMilkTruck/CesiumMilkTruck.glb"
	pathScenes         = "assets/MultipleScenes/MultipleScenes.glb"
	pathSamplers       = "assets/TextureSettingsTest/TextureSettingsTest.glb"
	pathPrimitiveModes = "assets/MeshPrimitiveModes/MeshPrimitiveModes.glb"
	pathQuantized      = "assets/AnimatedMorphCube/AnimatedMorphCube-Quantized.glb"
	pathNarrowIndices  = "assets/InterpolationTest/InterpolationTest.glb"
	pathTruncated      = "assets/broken/truncated.glb"
	// pathMissing is a file that is not there. The mount answers fs.ErrNotExist
	// for it, which is also what lets storage fall through to the next mount, so
	// there is no error type here that says "absent" rather than "not mine".
	pathMissing = "assets/broken/does-not-exist.glb"
	// pathInvalid is not a resource path at all. An absolute path is refused by
	// the same rule canvas uses, before it reaches the cache at all - so it
	// leaves no entry behind and a typo stays a typo.
	pathInvalid = "/assets/broken/absolute.glb"
)

// station is one slot of the grid: what it draws, where it stands, and what it
// is there to show.
type station struct {
	name string
	path string
	// scene and node are the Model's selectors, and ref() pairs them with path
	// for the Model Component and for a query alike.
	scene string
	node  string
	// column and row place the pad, from the left and from the front.
	column, row int
	// scale and lift place the model on its pad: scale brings every file to
	// about padReach across, and lift raises its lowest point to the pad.
	//
	// They are typed rather than read from AABB at runtime so the picture is
	// the same on the frame a model becomes resident as on the frame after, and
	// the numbers beside them are what the vendored bytes actually measure.
	// TestTheStationTableMeasuresTheVendoredBytes checks the pair against the
	// facade, so a re-vendored asset fails a test rather than drifting the
	// layout.
	scale float32
	size  m.Vec3
	minY  float32
	// loads says whether this station's file is expected to load at all. It is
	// a bool rather than a state because a load has either finished or failed
	// by the time the call that asked for it returned: there is no third answer
	// and no in-flight state left to name.
	loads bool
	// draws is how many instances this station's Model contributes to a frame
	// once it has settled: one per primitive the selector's subtree covers, and
	// zero for a station that never draws anything.
	draws int
	// tint, when non-zero, is the base colour this station's Params lay over
	// the file's own materials. repaint gives it a Material instead.
	tint    m.Color
	repaint bool
	// note is what this station is here for, printed beside its residency.
	// The HUD lays the sixteen out as two columns, so a note has a width
	// budget rather than a style: NoteBudget characters, checked by a test,
	// because the failure of overrunning it is one column overwriting the
	// other and both becoming unreadable.
	note string
}

// ref is the station's selectors, as the Model Component holds them and the
// lookup facade takes them.
func (s *station) ref() model.ModelRef {
	return model.ModelRef{Path: s.path, Scene: s.scene, Node: s.node}
}

// The station table. Each row's size and minY are the file's own AABB through
// the selector above it, in the model's own space, and are what scale and lift
// are computed from.
var stations = [...]station{
	// Row 1: addressing. The whole scene, then three re-rooted subtrees of it.
	{
		name: "scene", path: pathTruck, scene: "Scene",
		column: 0, row: 0, scale: 0.62,
		size: m.Vec3{X: 2.792, Y: 2.6532, Z: 4.8689}, minY: -0.0688,
		loads: true, draws: TruckPrimitives,
		note: "Scene names the file's one scene, and it matches",
	},
	{
		name: "body", path: pathTruck, node: "Cesium_Milk_Truck",
		column: 1, row: 0, scale: 0.62,
		size: m.Vec3{X: 4.8689, Y: 2.792, Z: 2.6532}, minY: -1.396,
		loads: true, draws: TruckPrimitives,
		note: "Node re-roots: Yup2Zup's rotation is discarded",
	},
	{
		name: "wheel", path: pathTruck, node: "Wheels",
		column: 2, row: 0, scale: 1.05,
		size: m.Vec3{X: 0.8556, Y: 2.116, Z: 0.8556}, minY: -1.058,
		loads: true, draws: 1,
		note: "an axle pair at depth 4, re-rooted to the pad",
	},
	{
		name: "wheel.001", path: pathTruck, node: "Wheels.001",
		column: 3, row: 0, scale: 1.05,
		size: m.Vec3{X: 0.8556, Y: 2.116, Z: 0.8556}, minY: -1.058,
		loads: true, draws: 1,
		note: "same mesh, different ancestor offset, same box",
	},

	// Row 2: what an Entity may say about a resident model's materials, and
	// what a selector that matches nothing does.
	{
		name: "tinted", path: pathTruck, node: "Cesium_Milk_Truck",
		column: 0, row: 1, scale: 0.62,
		size: m.Vec3{X: 4.8689, Y: 2.792, Z: 2.6532}, minY: -1.396,
		loads: true, draws: TruckPrimitives,
		tint: m.NewColorSrgb(1.0, 0.45, 0.30, 1),
		note: "Params lay over it by name: the livery survives",
	},
	{
		name: "repainted", path: pathTruck, node: "Cesium_Milk_Truck",
		column: 1, row: 1, scale: 0.62,
		size: m.Vec3{X: 4.8689, Y: 2.792, Z: 2.6532}, minY: -1.396,
		loads: true, draws: TruckPrimitives,
		repaint: true,
		note:    "a Material reading none of the file's replaces it",
	},
	{
		name: "default scene", path: pathScenes,
		column: 2, row: 1, scale: 2.4,
		size: m.Vec3{X: 1, Y: 1, Z: 0}, minY: 0,
		loads: true, draws: 1,
		note: "two scenes, both unnamed; this is the default",
	},
	{
		name: "no such scene", path: pathScenes, scene: "Triangle",
		column: 3, row: 1, scale: 2.4,
		size: m.Vec3{X: 1, Y: 1, Z: 0}, minY: 0,
		loads: true, draws: 0,
		note: "an unmatched Scene reports once, never falls back",
	},

	// Row 3: the four gaps between glTF and WebGPU that the loader papers over.
	{
		name: "samplers", path: pathSamplers,
		column: 0, row: 2, scale: 0.24,
		size: m.Vec3{X: 10.3233, Y: 10.0721, Z: 0.25}, minY: -5.6186,
		loads: true, draws: SamplerPrimitives,
		note: "9 textures, 3 images; mips are a CPU box filter",
	},
	{
		name: "topologies", path: pathPrimitiveModes,
		column: 1, row: 2, scale: 0.5,
		size: m.Vec3{X: 5.732, Y: 5, Z: 0}, minY: -4,
		loads: true, draws: PrimitiveModeDraws,
		note: "strips, fans, a loop become lists; POINTS skipped",
	},
	{
		name: "quantised", path: pathQuantized,
		column: 2, row: 2, scale: 0.26,
		size: m.Vec3{X: 9.7644, Y: 9.7644, Z: 9.7644}, minY: -4.8822,
		loads: true, draws: 1,
		note: "u16 positions and i8 normals, dequantised at load",
	},
	{
		name: "narrow indices", path: pathNarrowIndices,
		column: 3, row: 2, scale: 0.3,
		size: m.Vec3{X: 8.8, Y: 9.9595, Z: 2.0037}, minY: -2.1595,
		loads: true, draws: NarrowIndexPrimitives,
		note: "u8 indices widened to u32; WebGPU has no u8",
	},

	// Row 4: the failures. Every pad here stays bare.
	{
		name: "no such node", path: pathTruck, node: "Wheels.002",
		column: 0, row: 3, scale: 0.62,
		size: m.Vec3{X: 0.8556, Y: 2.116, Z: 0.8556}, minY: -1.058,
		loads: true, draws: 0,
		note: "an unmatched Node reports once and draws nothing",
	},
	{
		name: "truncated", path: pathTruncated,
		column: 1, row: 3, scale: 2.4,
		size: m.Vec3{X: 1, Y: 1, Z: 1}, minY: 0,
		loads: false, draws: 0,
		note: "opens, then fails on EOF: the decode refuses it",
	},
	{
		name: "absent", path: pathMissing,
		column: 2, row: 3, scale: 2.4,
		size: m.Vec3{X: 1, Y: 1, Z: 1}, minY: 0,
		loads: false, draws: 0,
		note: "fs.ErrNotExist: the library's own read failure",
	},
	{
		name: "not a path", path: pathInvalid,
		column: 3, row: 3, scale: 2.4,
		size: m.Vec3{X: 1, Y: 1, Z: 1}, minY: 0,
		loads: false, draws: 0,
		note: "absolute: refused before it reaches the cache",
	},
}

// NoteBudget is the longest a station's note may be. It is the width the HUD's
// left column has before it reaches the right one, in characters, less the
// fixed name, state and node-count fields ahead of it.
const NoteBudget = 50

// The station indices the frame and the tests refer to by name.
const (
	stationScene = iota
	stationBody
	stationWheel
	stationWheelOther
	stationTinted
	stationRepainted
	stationDefaultScene
	stationNoSuchScene
	stationSamplers
	stationTopologies
	stationQuantised
	stationNarrowIndices
	stationNoSuchNode
	stationTruncated
	stationAbsent
	stationNotAPath
)

// What the vendored files flatten to, spelled out because a count is the only
// thing that catches a model which silently loaded half of itself.
const (
	// TruckPrimitives is CesiumMilkTruck's whole scene: three body primitives
	// and one wheel mesh drawn by two nodes. The body subtree carries all four,
	// because the two wheel nodes hang off it.
	TruckPrimitives = 5
	// SamplerPrimitives is TextureSettingsTest's ten meshes, one primitive each.
	SamplerPrimitives = 10
	// PrimitiveModeDraws is MeshPrimitiveModes' seven meshes less the POINTS
	// one, which has no gfx topology and is skipped and reported.
	PrimitiveModeDraws = 6
	// NarrowIndexPrimitives is InterpolationTest's ten nodes, one primitive each.
	NarrowIndexPrimitives = 10
)

// The grid. padReach is how wide a station's model is scaled to be, which is
// what every station's scale in the table above was chosen for.
const (
	columns      = 4
	rows         = 4
	colSpacing   = 5.0
	rowSpacing   = 6.0
	padSize      = 4.0
	padReach     = 2.5
	fieldOfViewY = 1.0472
	nearPlane    = 0.1
	farPlane     = 200
)

// The documented camera pose. It is fixed: nothing in this frame moves, so
// there is no orbit and the reference capture is taken by launching, waiting
// for residency and pressing nothing.
var (
	cameraEye    = m.Vec3{Y: 13.0, Z: 17.5}
	cameraTarget = m.Vec3{Y: 0.9, Z: -1.2}
)

// The frame's colours, every one written in sRGB and converted on the way in:
// Color holds linear components, and a demo that typed linear literals would be
// picking its palette in a space no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.08, 1)
	padColor      = m.NewColorSrgb(0.30, 0.32, 0.36, 1)
	padFailColor  = m.NewColorSrgb(0.34, 0.20, 0.20, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
	hudWarnColor  = m.NewColorSrgb(0.95, 0.62, 0.42, 1)
	hudOkColor    = m.NewColorSrgb(0.55, 0.85, 0.60, 1)
)

// Loading is the demo plugin. What its Systems share - the step counter, the
// residency table the HUD prints and the levers a key press pulls - is the
// Residency resource, and the plugin keeps the same pointer so a test reads
// and pulls exactly what a human does.
type Loading struct {
	residency *Residency
}

// Residency is the state the demo's Systems share, as one resource.
type Residency struct {
	step int
	// preloaded is whether the one Preload pass has run. It runs on the first
	// update whose backend is up rather than at init, because a load before
	// the backend is up is refused.
	preloaded bool
	// pending is the unload a key press asked for, applied by the unload
	// System at the top of the next update.
	pending pendingUnload
	// states is each station's outcome as of this frame - nil where the file
	// loaded - known says a frame has asked at all, and settled is the step
	// each station last changed between loaded and not.
	states  [len(stations)]error
	known   [len(stations)]bool
	settled [len(stations)]int
	// poseBytes and morphBytes are the lookup-wide totals, which are what an
	// unload visibly moves.
	poseBytes, morphBytes int
	// nodes is the scratch Nodes reads into, kept so the survey's per-frame
	// query allocates nothing.
	nodes []string
	// nodeCount is how many addressable nodes each station's selector covers.
	nodeCount [len(stations)]int
	// entities is how many Entities setup spawned: the camera, the pads and
	// the stations. It is a count the demo knows because it did the spawning.
	entities int
	// reloads is how many Models the unload System has marked changed since
	// the start, which is how many reloads the levers have asked for.
	reloads int
	rate    rate
}

// pendingUnload is what the next update should give up before scene's load
// System runs.
type pendingUnload struct {
	model, texture, all, retry bool
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is app's fixed timestep,
// nor from the step counter, which is the same number however long a frame took.
// It counts frames over a window rather than averaging 1/interval per frame, so
// a startup spike is one frame in the count instead of a reading that never
// happened decaying for a hundred frames afterwards.
type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

// ratePeriod is how long the meter counts before republishing.
const ratePeriod = 250 * time.Millisecond

func (r *rate) measure(now time.Time) {
	if r.window.IsZero() {
		r.window = now
		return
	}
	r.frames++
	if elapsed := now.Sub(r.window); elapsed >= ratePeriod {
		r.perSecond = float32(float64(r.frames) / elapsed.Seconds())
		r.frames, r.window = 0, now
	}
}

// New builds the demo plugin.
func New() *Loading { return &Loading{residency: &Residency{}} }

func (p *Loading) Name() kernel.PluginName { return Name }

func (p *Loading) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{
		canvas.Name, ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name,
	}
}

func (p *Loading) Register(registrar *kernel.Registrar, _ any) error {
	// storage mounts nothing by default, and the vendored asset set lives in
	// the repository rather than beside the executable, which `go run` builds
	// into a temporary directory - so the demo contributes it explicitly and
	// refuses to start without it.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	ecs.RegisterComponent[Station](registrar, uint32(len(stations)))
	registrar.InitResource(p.residency)

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	registrar.Subscribe[advanceSystem](ecs.ToHandler[app.UpdateEvent](registrar, advance)).First()
	// The levers and the preload run before scene's load System, so a Model
	// the unload marked changed is loaded again in the same tick and nothing
	// scene draws ever names a model the tick freed.
	registrar.Subscribe[unloadSystem](ecs.ToHandler[app.UpdateEvent](registrar, unload)).
		After[advanceSystem]().Before[scene.LoadOnUpdate]()
	registrar.Subscribe[preloadSystem](ecs.ToHandler[app.UpdateEvent](registrar, preload)).
		After[unloadSystem]().Before[scene.LoadOnUpdate]()
	// The survey runs after the load System, so a station's state as printed
	// is the state its own Model was keyed against this tick.
	registrar.Subscribe[surveySystem](ecs.ToHandler[app.UpdateEvent](registrar, survey)).
		After[scene.LoadOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[surveySystem]()

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	return nil
}

// setViewport fits the logical screen inside the window, swapping the axes when
// the window is taller than it is wide.
func setViewport() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
	var setDesiredViewport func(kernel.Kernel, gfx.SetDesiredViewportRequest) gfx.SetDesiredViewportResponse
	return func(access kernel.ResourceAccess) {
			setDesiredViewport = access.Uses[gfx.SetDesiredViewportCmd]()
		}, func(k kernel.Kernel, event app.WindowSizeChangeEvent) {
			if event.Width <= 0 || event.Height <= 0 {
				return
			}
			width, height := float32(screenWidth), float32(screenHeight)
			if event.Height > event.Width {
				width, height = height, width
			}
			setDesiredViewport(k,
				gfx.SetDesiredViewportRequest{Mode: gfx.ViewportFit, Width: width, Height: height})
		}
}

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine.
func (r *Residency) time() float32 { return float32(r.step) * fixedStep }

// UnloadModel, UnloadTexture, UnloadAll and Retry queue what the four keys
// queue. They are exported so loading_test.go drives the same levers a human
// does rather than a second path built for it.
func (p *Loading) UnloadModel()   { p.residency.pending.model = true }
func (p *Loading) UnloadTexture() { p.residency.pending.texture = true }
func (p *Loading) UnloadAll()     { p.residency.pending.all = true }
func (p *Loading) Retry()         { p.residency.pending.retry = true }

// PreloadOrder is every distinct path the grid names, in the order the one
// Preload pass asks for them. Exported because it is the list a test checks the
// station table against: a station whose path is missing here would load off
// scene's load System instead, which is a different code path and a silent one.
var PreloadOrder = []string{
	pathTruck,
	pathScenes,
	pathSamplers,
	pathPrimitiveModes,
	pathQuantized,
	pathNarrowIndices,
	pathTruncated,
	pathMissing,
	pathInvalid,
}

// SettledInstances is how many instances the camera's pass draws once every
// station has settled: one per pad, plus each station's own primitive count.
// It is a count the table knows, which is why a test can hold the frame to it.
var SettledInstances = func() int {
	total := len(stations)
	for i := range stations {
		total += stations[i].draws
	}
	return total
}()

// Step is the demo's own step counter.
func (p *Loading) Step() int { return p.residency.step }

// State is one station's outcome as of the last tick the demo surveyed: nil
// where the file loaded, and the reason it did not otherwise.
func (p *Loading) State(i int) error { return p.residency.states[i] }

// NodeCount is how many addressable nodes one station's selector covered, or -1
// where the query answered false - which is a failed path and an unmatched
// selector alike.
func (p *Loading) NodeCount(i int) int { return p.residency.nodeCount[i] }

// Entities is how many Entities setup spawned.
func (p *Loading) Entities() int { return p.residency.entities }

// Settled reports whether every station has reached the residency its table row
// expects, which is when the frame is the one the reference screenshot shows.
func (p *Loading) Settled() bool { return p.residency.settledAll() }

func (r *Residency) settledAll() bool {
	for i := range stations {
		if !r.known[i] || (r.states[i] == nil) != stations[i].loads {
			return false
		}
	}
	return true
}

// TotalPoseBytes and TotalMorphBytes are the lookup-wide totals as of the last
// tick, which is what an unload visibly moves.
func (p *Loading) TotalPoseBytes() int  { return p.residency.poseBytes }
func (p *Loading) TotalMorphBytes() int { return p.residency.morphBytes }

// stationForPath finds the station a report's path names. Two stations may
// share a path - six share the truck - and the first is enough here: the
// allow-list only asks whether this demo named the file on purpose.
func stationForPath(path string) (*station, bool) {
	for i := range stations {
		if stations[i].path == path {
			return &stations[i], true
		}
	}
	return nil, false
}

// stationForSelector reports whether some station carries exactly this
// selector, which is what keeps the allow-list narrow: a report about a node
// name no station wrote is a bug, not a demonstration.
func stationForSelector(path, sceneName, node string) bool {
	for i := range stations {
		s := &stations[i]
		if s.path == path && s.scene == sceneName && s.node == node && s.draws == 0 {
			return true
		}
	}
	return false
}
