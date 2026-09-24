// Command animated is the scene plugin's animation demo, and the web canary:
// four Khronos sample models arranged as a row of stations, each one moving.
// Every model is an Entity carrying a scene.Model and, where it moves, a
// scene.Animation its Systems fill from the demo clock.
//
//	go run ./cmd/scene/animated
//	bash cmd/web/build.sh animated       # and the same demo in a browser
//
// It is the canary because it touches the most storage-buffer bindings, the
// budget has no spare, and a single unbound binding silently kills the whole
// frame. The bundled scene shader declares seven storage buffers against the
// browser floor of eight; group 2's three - scenePoses, sceneSkinJoints and
// sceneMorphDeltas - exist only for animation, so nothing before this demo
// bound all seven at once, and nothing before it compiled the shader at all.
// Every ticket in the effort so far verified headless, against a backend with no
// GPU. A module that fails to compile and a binding that was never bound fail
// identically on the web: a blank canvas and nothing logged.
//
// Making a second demo run in a browser would double the verification for no
// proportionate gain; leaving this one desktop-only would defer the one class
// of failure a desktop run provably cannot catch, because a native adapter
// reports hardware limits far above the WebGPU floor.
//
// What it exercises: baked poses and the 60 Hz grid; a ClipMachine crossfading
// on a timed trigger; the four-play cap, which the Animation Component's fixed
// array now states as a type; the rest frame, which is a real pose rather than
// a collapse; PoseBytes and MorphBytes; sparse morph weights; the attribute
// mask intersected with what the base primitive authored; degenerate
// single-joint skins on a file with no skins key at all; and u8 index widening.
//
// # The four stations
//
// Keys 1-4 fly the camera to a station; 0 or R returns to the overview, and R
// also rewinds the clock. The order is left to right, which is the order of
// this list, of the stations table below, and of the HUD.
//
//	1 fox      Fox                     a real 24-joint rig: a gait machine's crossfade, and the rest frame beside it
//	2 interp   InterpolationTest       the 3x3 interpolation grid, and the four-play cap beside it
//	3 cube     AnimatedMorphCube x2    the attribute mask: the same cube at two strides
//	4 stress   MorphStressTest         eight shapes, sparse weights, and two of the file's clips side by side
//
// # The Systems
//
//	setup      spawns the camera, the ground and every station's Entities    (InitEvent)
//	steer      the keys, and the demo clock                                   (After input's advance)
//	orbit      the camera's Transform on its orbit
//	rig        the fox's gait machine, once Fox.glb answers Clips
//	gait       steps the gait machine to the clock and fills the fox's Animation
//	contrast   which clip the stress station's second copy plays, per key o
//	play       every other Animation, from its Playlist and the clock
//	residency  which files are resident, and what their animation costs
//	hud        the numbers, on screen
//
// # Why it opens paused
//
// The demo starts paused at startTime, and reference.png beside this file is
// the frame at that time and at the documented camera pose. Space starts the
// clock.
//
// pbr could be a still life and take its reference screenshot at any moment,
// because nothing in its frame moved. Here motion is the subject, so a
// reference capture would otherwise land on whatever step the screenshotter
// happened to reach - and a headless capture cannot press a key to stop it.
// Opening paused at a stated time is what makes the picture reproducible:
// launch the demo, capture it, and the frame is the same one every time.
//
// Demo time is accumulated fixed steps, never wall clock. The update event's Dt
// is deliberately ignored, so the same step number is the same frame on any
// machine, and the HUD's frames-per-second meter is the demo's only clock that
// is not.
//
// # What only eyes can judge
//
// On the fox station (key 1), with the clock running: the left fox runs and the
// right one stands still in a real standing pose - four legs under it, tail out,
// head up - rather than collapsing into a heap at the origin, and as the left
// fox's blend slides from Walk to Run its gait changes rate and stride together
// rather than one fox-shaped pose snapping to another.
//
// One sentence, four failures. A rest frame that is not row 0 of the bake but a
// zeroed pose buffer puts the standing fox's every bone at the origin, which
// reads as a spike of triangles rather than a fox. A crossfade that normalises
// nothing shrinks every bone toward the origin as the second play's weight
// arrives, so the running fox visibly deflates mid-blend. A frame pair that
// takes the floor of the sample position for both rows makes the gait snap at
// 60 Hz instead of sliding. And an inverse bind premultiplied into the pose
// record - the design's own overturned recommendation - injects the bind pose's
// non-uniform scale into a record that is then decomposed to TRS, which shows
// up as the fox's limbs shearing as they swing.
//
// The two foxes share one mesh and one material, so scene draws them as one
// instanced draw; each instance reads its own animation block, which is why
// the one beside a running fox can still stand at rest.
//
// Two more, across the row. On the interpolation grid (key 2), under the label
// board the file prints for itself, the middle column steps between poses with
// no motion in between while the left column slides evenly and the right column
// overshoots and settles back - three columns visibly disagreeing in one frame,
// which is a difference between three things rather than a judgement about one;
// a demo where all three columns matched would mean the sampler's interpolation
// mode never reached the bake. And the two morph
// cubes (key 3) deform identically while the HUD prints different byte counts
// for them, which is the attribute mask being intersected with what each file
// actually authored rather than taken from its targets.
//
// # The known gaps
//
// No model here is both skinned and morphed, so morph-then-skin ordering - the
// one thing glTF is emphatic about - is untested by this demo. No file in the
// Khronos repository mixes the two either, so this is a gap to record rather
// than one to close by picking a better asset. The packing half of it is
// covered in scene by a built fixture that asserts both counts land in one
// block; what has no coverage anywhere, here included, is the shader applying
// them in that order.
//
// An Entity has no morph-weight override: its weights are always the animated
// ones. The stress station's second copy therefore plays a different clip of
// the file's own rather than holding a hand-built weight array.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"

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

// The logical screen the HUD is laid out in. The window is fitted to it, so the
// HUD keeps its proportions at any window size, and windowWidth and
// windowHeight are the logical size the window opens at - the size the
// reference screenshot is of.
const (
	screenWidth  = 960
	screenHeight = 540
	windowWidth  = 1280
	windowHeight = 720
)

// CameraMain is the demo's only camera, at a negative id so it sorts below
// layerHUD and above layerBackdrop.
const CameraMain scene.CameraID = -100

// The canvas layers, which are gfx orders directly. The camera declares no
// passes, and its default pass preserves colour rather than clearing it, so
// the frame's one colour clear is canvas's on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.
			WithTitle("cog examples: scene animated").
			// Launched at the size the reference screenshot was taken at
			// rather than resized into it: a runtime resize leaves the
			// viewport un-refitted. These are logical units, so the file
			// beside this one is larger on a display that scales.
			WithSize(windowWidth, windowHeight),
	}
	permanentfs.Configure(config)

	// The demo plugin is last because its Systems read the Components and
	// resources the plugins before it declare.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		modelplugin.New(),
		gogpuplugin.New(),
		mcpplugin.New(),
		ecsplugin.New(), sceneplugin.New(),
		New(),
	}

	engine := kernel.New(config).Handler(report).WithPlugins(plugins...)
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

// report is the demo's error handler: everything is logged and survived.
//
// The default handler logs and returns true, which terminates the engine. A
// demo is a thing you look at, on the desktop and in the browser alike: one
// failed texture should cost that texture, not the window.
func report(err error) error {
	log.Printf("animated: %v", err)
	return nil
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "animated"

// Animated is the demo's gameplay plugin. It owns the Demo resource its
// Systems share, and keeps a pointer to it so a test can read what the HUD
// printed.
type Animated struct {
	demo *Demo
}

// New builds the demo plugin at its documented starting pose, paused, with the
// stress station's contrast on.
func New() *Animated {
	return &Animated{demo: newDemo()}
}

func (a *Animated) Name() kernel.PluginName { return Name }

func (a *Animated) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{
		canvas.Name, ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name,
	}
}

type (
	setupSystem     kernel.Subscription[app.InitEvent]
	steerSystem     kernel.Subscription[app.UpdateEvent]
	orbitSystem     kernel.Subscription[app.UpdateEvent]
	rigSystem       kernel.Subscription[app.UpdateEvent]
	gaitSystem      kernel.Subscription[app.UpdateEvent]
	contrastSystem  kernel.Subscription[app.UpdateEvent]
	playSystem      kernel.Subscription[app.UpdateEvent]
	residencySystem kernel.Subscription[app.UpdateEvent]
	hudSystem       kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (a *Animated) Register(registrar *kernel.Registrar, _ any) error {
	// storage mounts nothing by default, and the vendored asset set lives in
	// the repository rather than beside the executable, which `go run` builds
	// into a temporary directory - so the demo contributes it explicitly and
	// refuses to start without it.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	// One fox has a gait, one stress copy a contrast, and every other moving
	// Entity a Playlist: nine grid cubes, the cap copy, two cubes and two
	// stress copies.
	ecs.RegisterComponent[Gait](registrar, 1)
	ecs.RegisterComponent[Contrast](registrar, 1)
	ecs.RegisterComponent[Playlist](registrar, 16)
	registrar.InitResource(a.demo)

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	// The keys are read after input has promoted this tick's edges, so a
	// focus key is one press rather than sixty, and every System that reads
	// the clock runs after it, so a held arrow moves the camera on the very
	// frame it is pressed.
	registrar.Subscribe[steerSystem](ecs.ToHandler[app.UpdateEvent](registrar, steer)).
		After[input.AdvanceOnUpdate]()
	// Everything that writes a Transform or an Animation runs Before scene's
	// recording System, so a step draws the world as that step left it rather
	// than whichever side of the tie the scheduler happened to break.
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		After[steerSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[rigSystem](ecs.ToHandler[app.UpdateEvent](registrar, rig)).
		After[steerSystem]()
	registrar.Subscribe[gaitSystem](ecs.ToHandler[app.UpdateEvent](registrar, gait)).
		After[rigSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[contrastSystem](ecs.ToHandler[app.UpdateEvent](registrar, contrast)).
		After[steerSystem]()
	registrar.Subscribe[playSystem](ecs.ToHandler[app.UpdateEvent](registrar, play)).
		After[contrastSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[residencySystem](ecs.ToHandler[app.UpdateEvent](registrar, residency)).
		After[rigSystem]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[residencySystem]().After[gaitSystem]().After[playSystem]().
		Before[canvas.FlushOnUpdate]()

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

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock: the update event's Dt is deliberately ignored.
//
// It is also the rate the poses are baked at, which is the reason 60 rather
// than 30 is the number: InterpolationTest's nine clips are exactly STEP,
// LINEAR and CUBICSPLINE over translation, rotation and scale, and a
// cubic-spline curve resampled at 30 Hz loses the overshoot that is the whole
// visible difference between it and a linear one.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose and time, where the reference screenshot is
// taken. The demo opens paused at startTime; space starts the clock.
const (
	// startTime is chosen against all four stations at once, not one of them,
	// and that is harder than it sounds: the clips run 0.71s to 4.2s and no
	// two periods line up, so most times leave something at an uninformative
	// point of its own cycle. At 26.1 seconds the morph cube is a box rather
	// than the wedge it spends most of its 4.2 seconds as, and the
	// interpolation grid's scale row is inside the fifth of its two seconds
	// where all three cubes are large enough to compare - outside it two of
	// the three shrink to specks and the row reads as missing geometry rather
	// than as an animation.
	startTime      = 26.1
	overviewRadius = 23.0
	startAzimuth   = 0.0
	startElevation = 0.11
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
	nearPlane      = 0.1
	farPlane       = 200
)

// orbitTarget is the point the overview camera looks at and orbits, a little
// above the ground so the row fills the middle of the frame.
var orbitTarget = m.Vec3{Y: 1.9}

// The frame's own colours, written in sRGB and converted on the way in: Color
// holds linear components, and a demo that typed linear literals would be
// picking its palette in a space no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.09, 1)
	groundColor   = m.NewColorSrgb(0.30, 0.31, 0.34, 1)
	sunColor      = m.NewColorSrgb(1, 0.97, 0.92, 1)
	ambientSky    = m.NewColorSrgb(0.24, 0.29, 0.38, 1)
	ambientGround = m.NewColorSrgb(0.12, 0.11, 0.10, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
)

// No punctual lights at all. This demo's subject is motion, and one directional
// sun with a hemispheric ambient is what makes a moving silhouette legible;
// pbr is the demo that exercises the light path, and sixteen lights here would
// only make it harder to see whether a limb is where it should be.
var sunDirection = m.Vec3{X: -0.4, Y: -1, Z: -0.45}

// The ground the row stands on: a stage the width of the row rather than a
// plane out to the far clip. An endless ground fills the whole lower half of
// the frame with one flat grey, which is a lot of picture spent saying nothing;
// a stage that runs out puts the backdrop back under the row and gives the
// overview a horizontal to read the stations against.
const (
	groundWidth = 50
	groundDepth = 15
)

// The vendored files, one constant each because a path typed twice is a path
// that can be typed differently.
const (
	foxPath       = "assets/Fox/Fox.glb"
	interpPath    = "assets/InterpolationTest/InterpolationTest.glb"
	cubePath      = "assets/AnimatedMorphCube/AnimatedMorphCube.glb"
	quantizedPath = "assets/AnimatedMorphCube/AnimatedMorphCube-Quantized.glb"
	stressPath    = "assets/MorphStressTest/MorphStressTest.glb"
)

// ModelPaths is every file the demo loads, which is five rather than four:
// AnimatedMorphCube and its quantized twin are two files describing one cube,
// and having both is the whole of the attribute-mask station.
var ModelPaths = [...]string{foxPath, interpPath, cubePath, quantizedPath, stressPath}

// station is one place along the row, for the camera and the HUD. Unlike pbr's,
// it carries no placement: what stands at a station here is several Entities
// with several different jobs, and setup places each one.
type station struct {
	name string
	// x is the middle of the station along the row, height how far above the
	// ground the close-up camera looks, and radius how far back it stands.
	x      float32
	height float32
	radius float32
}

// stationSpacing is how far apart the four stations stand. It is wide enough
// that the interpolation station's two grids clear the fox on one side and the
// morph cubes on the other at the overview pose.
const stationSpacing = 11.5

var stations = [...]station{
	{name: "fox", x: -1.5 * stationSpacing, height: 1.4, radius: 7.0},
	{name: "interp", x: -0.5 * stationSpacing, height: 2.4, radius: 9.5},
	{name: "cube", x: 0.5 * stationSpacing, height: 1.5, radius: 9.0},
	{name: "stress", x: 1.5 * stationSpacing, height: 1.4, radius: 6.5},
}

// The station indices setup refers to by name.
const (
	stationFox = iota
	stationInterp
	stationCube
	stationStress
)

// The fox clips, by the file's own names. Survey is the third and is not played
// here: two clips are what a crossfade needs, and a third would make the blend
// a judgement about which one is showing rather than a slide between two.
const (
	foxWalk = "Walk"
	foxRun  = "Run"
	// FoxSurvey is the clip the demo deliberately leaves unplayed. It is named
	// so the test can assert the file carries three and the demo plays two.
	FoxSurvey = "Survey"
)

// interpNode is one cube of the interpolation grid: the clip that steers it and
// the node that clip steers.
//
// The pairing is the file's, and it is not derivable from either name alone -
// "Step Rotation" drives Cube.003 while "CubicSpline Rotation" drives Cube.004
// and "Linear Rotation" drives Cube.005, so the file's own order is neither the
// grid's nor alphabetical. There is no Cube.007.
type interpNode struct {
	clip string
	node string
}

// The interpolation grid, laid out as it is drawn: three rows by three columns,
// row 0 at the bottom.
//
// Columns are the interpolation mode and rows the path steered, which is the
// arrangement the file itself is authored in and the one that makes the
// station legible: reading down a column is one mode applied to three different
// things, and reading across a row is three modes applied to the same thing.
//
// The column order is LINEAR, STEP, CUBICSPLINE, and it is the file's own
// rather than this demo's choice: the tenth node of InterpolationTest is a
// label board printed with those three words in that order, and the cap copy
// beside this grid draws it. Two grids in one frame under one label have to
// agree, so this table is ordered to match the board rather than to match the
// order the file happens to list its animations in - which is a third order
// again, and the reason the node each clip steers is tabulated here instead of
// inferred.
//
// Each cube is its own Entity drawing one Node, so each gets its own clip at
// full weight and all nine move at once. One Entity of the whole file cannot
// do that - its Animation holds four plays, and the blend is a weighted mean,
// so the survivors would move at a quarter amplitude. That copy stands beside
// this one and is exactly what the cap station shows.
var interpGrid = [3][3]interpNode{
	// Row 0: scale.
	{{"Linear Scale", "Cube.001"}, {"Step Scale", "Cube"}, {"CubicSpline Scale", "Cube.002"}},
	// Row 1: rotation.
	{{"Linear Rotation", "Cube.005"}, {"Step Rotation", "Cube.003"}, {"CubicSpline Rotation", "Cube.004"}},
	// Row 2: translation.
	{{"Linear Translation", "Cube.009"}, {"Step Translation", "Cube.006"}, {"CubicSpline Translation", "Cube.008"}},
}

// InterpClips is how many clips the file declares, which is how many the cap
// copy is offered.
const InterpClips = 9

// The weight each row of the grid is offered at on the cap copy, bottom row
// first. They are not arbitrary and they are not rigged: the cap keeps the four
// heaviest, so a translation row at 1.0 takes three of the four places and the
// first rotation clip offered takes the last. Which four survive falls out of
// the rule rather than being chosen, and the picture it makes - the top row
// moving, one cube of the middle row moving, the bottom row frozen - is what
// the cap looks like from outside.
//
// The survivors move at a fraction of their amplitude, and that is the honest
// half of the lesson rather than a defect: weights are normalised across an
// Entity's plays because the blend is a weighted mean of TRS, so nine clips
// through a four-play cap is not nine animations, it is four at about a
// quarter each.
var interpRowWeights = [3]float32{0.25, 0.5, 1.0}

// CapPlays is how many of the offered plays survive, which is model's own
// per-draw limit and the length of scene.Animation's array. It is spelled out
// here so the HUD and the assertions read the same number.
const CapPlays = model.MaxClipPlays

// The morph stress station. Eight named shapes on a two-primitive mesh, and
// three weights-only clips - a weights channel produces no joint, so the file
// bakes no poses at all and every draw of it takes the morph-only variant:
// group 2 binding 2 alone, with no pose bindings declared at all.
const (
	stressClip = "TheWave"
	// stressContrastClip is the clip the second copy plays while the contrast
	// is on: the file's own walk through its shapes one at a time, which is
	// the sparsest weight vector the file animates.
	stressContrastClip = "Individuals"
	// StressTargets is the file's flattened target count: one node, so one run
	// of eight slots rather than eight per primitive.
	StressTargets = 8
)

// The counts the frame is spelled out against once every file is resident: two
// foxes of one primitive each, nine single-primitive Node Entities and one
// ten-primitive whole-file Entity for the interpolation station, two
// single-primitive morph cubes, two two-primitive stress copies, and the
// ground.
//
// They are spelled out rather than measured at runtime so that an asset
// swapped underneath the demo fails an assertion instead of quietly changing
// the picture.
const (
	foxPrimitives = 1
	// Nine cubes and a plane, which is what the file's one scene flattens to.
	interpPrimitives = 10
	cubePrimitives   = 1
	stressPrimitives = 2
)

// DrawnInstances is how many instances of the bundled scene shader the frame
// draws. scene batches Entities that share a mesh and a material into one
// instanced draw, so this is the number that does not depend on how they
// batch.
const DrawnInstances = 2*foxPrimitives +
	9 + interpPrimitives +
	2*cubePrimitives +
	2*stressPrimitives +
	1
