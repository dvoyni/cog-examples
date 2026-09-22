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
// Sixteen pads in four rows, one model draw over each, whether or not there is
// anything to draw. Every pad is drawn unconditionally: a station whose model
// is still loading, failed, or whose selector matched nothing shows a bare pad,
// because scene skips the draw rather than substituting anything for it. That
// is the whole of "skip, never substitute", and the pad is what makes it
// visible rather than merely absent.
//
//   - Row 1, addressing: the whole truck through a Scene selector that matches,
//     then its body and each of its two wheel pairs through a Node selector
//     that re-roots.
//   - Row 2, materials and selectors: the body tinted through OverrideParams and
//     the body repainted through a replacement Material, then MultipleScenes at
//     its declared default and MultipleScenes under a Scene name no file in the
//     repository carries.
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
// its side because a Node draw discards that root's rotation and replaces it
// with the draw's own Transform. The last two are wheel *pairs* - the file has
// one mesh for an axle and hangs it off two nodes - and they are identical to
// each other and centred on their pads, standing on end because the axle runs
// along the model's own Y once Yup2Zup is gone. That they are identical is the
// assertion: they are the same mesh under the same local rotation hanging off
// two parents with different offsets, so a re-root that left any ancestor's
// transform in would put them in two different places.
//
// In row 2 the tinted body keeps its livery under a colour wash, and the
// repainted one is flat grey with no livery at all. One sentence, two
// contracts: OverrideParams merges over the file's own material and keeps its
// textures, and a replacement Material unbinds the file's record entirely, so
// its base colours, factors and texture transforms do not survive.
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
//	   that the next frame's draws trigger bakes no new texture
//	t  unload the truck's textures too, which is the separate, deliberate lever
//	x  unload everything
//	r  unload the three failed paths and preload them again, which is the only
//	   retry there is - and all three fail again, because they are still broken
//
// # The reference pose
//
// A documented fixed camera pose, no orbit and no clock in the frame. Demo time
// is accumulated fixed steps all the same, because the HUD prints it and
// because a demo whose numbers came off the wall clock could not be captured
// twice. reference.png beside this file is the frame once every station has
// settled.
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
// passes, and the implicit forward pass preserves colour rather than clearing
// it, so the frame's one colour clear is canvas's on a layer below the camera.
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

	// The demo plugin is last because it records into the queues the plugins
	// before it declare.
	demo := New()
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		modelplugin.New(), sceneplugin.New(),
		gogpuplugin.New(),
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
// scene's own errors carry the path and the selector that produced them, which
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
	var unavailable scene.ErrModelUnavailable
	if errors.As(err, &unavailable) {
		if station, ok := stationForPath(unavailable.Model); ok && !station.loads {
			return true, station.name + " names a file that cannot be loaded, on purpose"
		}
	}
	var invalid scene.ErrModelPathInvalid
	if errors.As(err, &invalid) {
		if station, ok := stationForPath(invalid.Model); ok && !station.loads {
			return true, station.name + " names a path that is not a resource path at all"
		}
	}
	var sceneMissing scene.ErrModelSceneMissing
	if errors.As(err, &sceneMissing) {
		if stationForSelector(sceneMissing.Model, sceneMissing.Scene, "") {
			return true, "the unmatched Scene selector reports once and draws nothing"
		}
	}
	var nodeMissing scene.ErrModelNodeMissing
	if errors.As(err, &nodeMissing) {
		if stationForSelector(nodeMissing.Model, nodeMissing.Scene, nodeMissing.Node) {
			return true, "the unmatched Node selector reports once and draws nothing"
		}
	}
	var skipped scene.ErrModelPrimitiveSkipped
	if errors.As(err, &skipped) && skipped.Model == pathPrimitiveModes {
		return true, "MeshPrimitiveModes carries a POINTS primitive, which has no gfx topology"
	}
	return false, ""
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "loading"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

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
	// scene and node are the draw's selectors, and ref() pairs them with path
	// for a query. They are held apart from a ModelDraw because two stations
	// carry a material as well, and a scene.Material is not a comparable value.
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
	// draws is how many draw records this station flushes to once it has
	// settled: one per primitive the selector's subtree covers, and zero for a
	// station that never draws anything.
	draws int
	// tint, when non-zero, is the base colour this station merges over the
	// file's own materials through OverrideParams. repaint replaces them.
	tint    m.Color
	repaint bool
	// note is what this station is here for, printed beside its residency.
	// The HUD lays the sixteen out as two columns, so a note has a width
	// budget rather than a style: NoteBudget characters, checked by a test,
	// because the failure of overrunning it is one column overwriting the
	// other and both becoming unreadable.
	note string
}

// ref is the station's selectors as the lookup facade takes them.
func (s *station) ref() scene.ModelRef {
	return scene.ModelRef{Path: s.path, Scene: s.scene, Node: s.node}
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

	// Row 2: what a draw may say about a resident model's materials, and what a
	// selector that matches nothing does.
	{
		name: "tinted", path: pathTruck, node: "Cesium_Milk_Truck",
		column: 0, row: 1, scale: 0.62,
		size: m.Vec3{X: 4.8689, Y: 2.792, Z: 2.6532}, minY: -1.396,
		loads: true, draws: TruckPrimitives,
		tint: m.NewColorSrgb(1.0, 0.45, 0.30, 1),
		note: "OverrideParams merges: the livery survives",
	},
	{
		name: "repainted", path: pathTruck, node: "Cesium_Milk_Truck",
		column: 1, row: 1, scale: 0.62,
		size: m.Vec3{X: 4.8689, Y: 2.792, Z: 2.6532}, minY: -1.396,
		loads: true, draws: TruckPrimitives,
		repaint: true,
		note:    "Material replaces: the file's records are gone",
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

// Loading is the demo's gameplay plugin: it records the whole frame, owns the
// step counter and the residency table the HUD prints, and holds the unload
// levers a key press pulls.
type Loading struct {
	step int
	// preloaded is whether the one Preload pass has run. It runs on the first
	// update rather than at construction because Preload needs a device facade,
	// which only a handler holding the three locks can build.
	preloaded bool
	// pending is the unload a key press asked for, applied at the top of the
	// next update while the facade is in hand.
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
	// nodes is the scratch Nodes reads into, kept so the HUD's per-frame query
	// allocates nothing.
	nodes []string
	// nodeCount is how many addressable nodes each station's selector covers.
	nodeCount [len(stations)]int
	stats     stats
	rate      rate
	// repaint is the replacement material the repainted station binds. It holds
	// no GPU handle, so it is built once at construction.
	repaint scene.Material
}

// pendingUnload is what the next update should give up before it records.
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

// stats is what the previous frame's flush decided, read back out of the scene
// queue at the top of each update and printed by the HUD. It is the previous
// frame's because Passes publishes the frame the last flush consumed.
//
// resident belongs here rather than beside the table the HUD prints because it
// is the residency *that* flush drew against, and a draw count is only
// interpretable against the residency of its own frame. The two are read in the
// same breath at the top of the update, which is the first moment after the
// flush at which either can be observed at all.
type stats struct {
	passes    int
	ops       int
	recorded  int
	culled    int
	instances int
	batches   int
	// resident is each station's residency as of the frame these counts
	// describe, and known is false before the first flush, when there is no
	// such frame.
	resident [len(stations)]bool
	known    bool
}

// New builds the demo plugin.
func New() *Loading { return &Loading{repaint: newRepaintMaterial()} }

func (p *Loading) Name() kernel.PluginName { return Name }

func (p *Loading) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name}
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

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.draw)
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

// draw records the whole frame. It takes the Lookup, the filesystem and the
// resource queue, because Preload, State and the node queries all load the file
// they name, and the texture unloads free a GPU texture at the call. UnloadModel
// needs none of that and comes off the other facade over the same resource.
//
// This is where the cost of a synchronous load is meant to be visible: a demo
// that loads models declares what loading needs.
func (p *Loading) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var lookup kernel.Write[*scene.Lookup]
	var inputState kernel.Read[*input.State]
	var files kernel.Read[storage.FileSystem]
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			lookup = access.GetWrite[*scene.Lookup]()
			inputState = access.GetRead[*input.State]()
			files = access.GetRead[storage.FileSystem]()
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) {
			q := sceneQueue.Get()
			la := scene.NewLookupAccess(k, lookup.Get())
			device := scene.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())
			p.rate.measure(time.Now())
			p.readStats(q, device)
			p.advance(inputState.Get())
			p.applyUnloads(la, device)
			p.preload(device)
			p.record(q)
			p.readLookup(la, device)
			p.hud(canvasQueue.Get())
		}
}

// advance steps the demo's own clock and turns key presses into the unload the
// next block applies. The keys are queued rather than applied here because this
// runs before the facade is in hand, and because an unload landing between two
// of the frame's own queries would make the HUD disagree with itself.
func (p *Loading) advance(state *input.State) {
	p.step++
	if state == nil {
		return
	}
	if state.JustPressed(input.KeyU) {
		p.pending.model = true
	}
	if state.JustPressed(input.KeyT) {
		p.pending.texture = true
	}
	if state.JustPressed(input.KeyX) {
		p.pending.all = true
	}
	if state.JustPressed(input.KeyR) {
		p.pending.retry = true
	}
}

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine.
func (p *Loading) time() float32 { return float32(p.step) * fixedStep }

// UnloadModel, UnloadTexture, UnloadAll and Retry queue what the four keys
// queue. They are exported so loading_test.go drives the same levers a human
// does rather than a second path built for it.
func (p *Loading) UnloadModel()   { p.pending.model = true }
func (p *Loading) UnloadTexture() { p.pending.texture = true }
func (p *Loading) UnloadAll()     { p.pending.all = true }
func (p *Loading) Retry()         { p.pending.retry = true }

// applyUnloads gives up whatever the last key press asked for. Every one of
// these frees at the call, and the frame this update is about to record loads
// back whatever it still draws: a free followed by a get is a reload.
func (p *Loading) applyUnloads(la scene.LookupAccess, device scene.LookupDeviceAccess) {
	pending := p.pending
	p.pending = pendingUnload{}
	if pending.all {
		device.UnloadAll()
	}
	if pending.model {
		// The truck's geometry, baked poses and material records. Not its
		// textures: with no refcount the lookup cannot know whether another
		// resident model binds the same image by path, so freeing one that is
		// still bound would be a dead texture in a live bind group rather than
		// a missing picture.
		la.UnloadModel(pathTruck)
	}
	if pending.texture {
		// The separate, deliberate lever. For a glb the path names the
		// container, so this releases every image embedded in it.
		device.UnloadTexture(pathTruck)
	}
	if pending.retry {
		// The only retry there is. A failed path clears here and nowhere else,
		// and Preload is what asks again - there is no Retry, because a Retry
		// that did not first free would be a second name for the idempotent
		// load that already exists. The two calls sit in one handler because
		// freeing is immediate: the preload behind the unload loads afresh.
		// All three fail again: they are still broken.
		for _, path := range []string{pathTruncated, pathMissing, pathInvalid} {
			la.UnloadModel(path)
			device.Preload(path)
		}
	}
}

// preload asks for every distinct path once, on the first update, before
// anything is drawn.
//
// It is the loading screen in miniature: Preload is the same command a draw
// fires, fired without one, so an app moves the decode into a screen it
// controls instead of into the first frame that names the file. Nothing here
// waits for it - the stations are recorded on the very next line whatever their
// state, because a draw of a model that is not resident is skipped, never
// substituted, and that is what the bare pads show.
func (p *Loading) preload(la scene.LookupDeviceAccess) {
	if p.preloaded {
		return
	}
	p.preloaded = true
	for _, path := range PreloadOrder {
		la.Preload(path)
	}
}

// PreloadOrder is every distinct path the grid names, in the order the one
// Preload pass asks for them. Exported because it is the list a test checks the
// station table against: a station whose path is missing here would load off
// its first draw instead, which is a different code path and a silent one.
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

// record records the frame: one camera, sixteen pads and sixteen model draws.
func (p *Loading) record(q *scene.OpQueue) {
	q.Camera(CameraMain, scene.CameraDescr{
		Transform: m.LookAt(cameraEye, cameraTarget, m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      nearPlane,
		Far:       farPlane,
		// Everything else is left at its zero value: Projection is Perspective,
		// CullMask is LayersAll, both intensities are 1, and Passes is empty,
		// which emits one implicit forward pass at the camera's own id.
		SunDirection:  m.Vec3{X: -0.35, Y: -1, Z: -0.45},
		SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
		AmbientSky:    m.NewColorSrgb(0.30, 0.34, 0.42, 1),
		AmbientGround: m.NewColorSrgb(0.10, 0.09, 0.08, 1),
	})

	for i := range stations {
		station := &stations[i]
		pad := padColor
		if !station.loads {
			pad = padFailColor
		}
		q.Plane(0, stationPad(station), m.Vec2{X: padSize, Y: padSize}, pad)

		draw := scene.ModelDraw{
			Transform: stationPlacement(station),
			Scene:     station.scene,
			Node:      station.node,
		}
		switch {
		case station.repaint:
			// A replacement unbinds the file's own record along with its
			// bindings, so the draw takes glTF's defaults under a shader that
			// never heard of the file's livery.
			draw.Material = p.repaint
		case station.tint != (m.Color{}):
			// A merge over the file's own material, by name. glTF's parameter
			// names are the user-facing contract, so this is the whole of a
			// team colour.
			draw.OverrideParams = []gfx.ParameterDescr{
				gfx.ColorParam("baseColorFactor", station.tint),
			}
		}
		q.Model(0, station.path, draw)
	}
}

// RecordedOps is how many operations Ops reports: the camera registration, the
// sixteen pads and the sixteen model calls. A Model call is one op whatever it
// expands to, exactly as a WireBox is one.
const RecordedOps = 1 + len(stations) + len(stations)

// RecordedDraws is how many draw records the frame flushes to once every
// station has settled: one per pad, plus each station's own primitive count.
var RecordedDraws = func() int {
	total := len(stations)
	for i := range stations {
		total += stations[i].draws
	}
	return total
}()

// stationPad is the centre of one station's pad, on the ground plane.
func stationPad(s *station) m.Vec3 {
	return m.Vec3{
		X: (float32(s.column) - float32(columns-1)/2) * colSpacing,
		Z: (float32(rows-1)/2 - float32(s.row)) * rowSpacing,
	}
}

// stationPlacement is the transform one station's model is drawn at: its own
// scale, lifted so the model's lowest point rests on the pad.
//
// The lift is the file's own minY through the scale, which is why the table
// carries minY: a Node draw re-roots, so the number is the subtree's, not the
// scene's, and the two differ by a whole axis on this asset.
func stationPlacement(s *station) m.Transform {
	pad := stationPad(s)
	return m.At(pad.X, pad.Y-s.minY*s.scale, pad.Z).WithScale(s.scale)
}

// readStats reads the previous frame's flush result back out of the queue,
// together with the residency that flush drew against.
//
// Passes publishes the frame the last flush consumed, so these are the numbers
// for the frame before this one - which is what a HUD can print without
// stalling the pipeline to ask about the frame it is still recording.
//
// The residency is read here, and not from the table readLookup fills, because
// a load lands at a frame boundary: readLookup runs inside the update, before
// scene's own flush handler, so a model that installs between the two is one
// this frame's flush draws and that table calls loading. Judging a draw count
// against a residency read on the wrong side of the flush is a skew, not a
// substitution, and it is what made this demo's own substitution test flake.
// Here both numbers describe the same frame: nothing can install between the
// flush and this update's start without also being visible to the query below.
//
// The first update has no flush behind it, so it takes no snapshot at all -
// which also keeps Preload the first thing in the demo that names a path,
// rather than a query fired to fill a table for a frame that does not exist.
func (p *Loading) readStats(q *scene.OpQueue, la scene.LookupDeviceAccess) {
	if p.step == 0 {
		return
	}
	views := q.Passes(nil)
	p.stats = stats{passes: len(views), ops: len(q.Ops(nil)), known: true}
	for i := range views {
		p.stats.recorded += views[i].Recorded
		p.stats.culled += views[i].Culled
		p.stats.instances += views[i].Instances
		p.stats.batches += len(views[i].Batches)
	}
	for i := range stations {
		p.stats.resident[i] = la.State(stations[i].path) == nil
	}
}

// LastFlush is the frame the last flush consumed: how many draws it recorded
// and which stations were resident when it recorded them. ok is false until a
// frame has flushed.
//
// Exported as one value rather than as two queries because the pairing is the
// whole point - see readStats.
func (p *Loading) LastFlush() (recorded int, resident [len(stations)]bool, ok bool) {
	return p.stats.recorded, p.stats.resident, p.stats.known
}

// readLookup asks the facade what it knows, once per frame, for the HUD.
//
// Every query here loads the file it names, which is why it runs after record
// rather than before: a station's state as printed is the state its own draw
// saw. State is the only one of these that says why a file is not there - every
// other query answers false for a typo and for a broken file alike, and a
// loading screen watching only ok can never print a reason.
//
// Two facades: the two totals need neither the filesystem nor the queue, so
// they sit on the half a caller builds from the Lookup alone.
func (p *Loading) readLookup(totals scene.LookupAccess, la scene.LookupDeviceAccess) {
	for i := range stations {
		state := la.State(stations[i].path)
		if !p.known[i] || (p.states[i] == nil) != (state == nil) {
			p.settled[i] = p.step
		}
		p.states[i], p.known[i] = state, true
		p.nodes = p.nodes[:0]
		if names, ok := la.Nodes(stations[i].ref(), p.nodes); ok {
			p.nodes = names
			p.nodeCount[i] = len(names)
		} else {
			p.nodeCount[i] = -1
		}
	}
	p.poseBytes, p.morphBytes = totals.TotalPoseBytes(), totals.TotalMorphBytes()
}

// State is one station's outcome as of the last frame the demo recorded: nil
// where the file loaded, and the reason it did not otherwise.
func (p *Loading) State(i int) error { return p.states[i] }

// NodeCount is how many addressable nodes one station's selector covered, or -1
// where the query answered false - which is a failed path and an unmatched
// selector alike.
func (p *Loading) NodeCount(i int) int { return p.nodeCount[i] }

// Settled reports whether every station has reached the residency its table row
// expects, which is when the frame is the one the reference screenshot shows.
func (p *Loading) Settled() bool {
	for i := range stations {
		if !p.known[i] || (p.states[i] == nil) != stations[i].loads {
			return false
		}
	}
	return true
}

// TotalPoseBytes and TotalMorphBytes are the lookup-wide totals as of the last
// frame, which is what an unload visibly moves.
func (p *Loading) TotalPoseBytes() int  { return p.poseBytes }
func (p *Loading) TotalMorphBytes() int { return p.morphBytes }

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
