// Command animated is the scene plugin's animation demo, and the web canary:
// four Khronos sample models arranged as a row of stations, each one moving.
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
// Every ticket in the effort so far verified against a published flush with no
// GPU. A module that fails to compile and a binding that was never bound fail
// identically on the web: a blank canvas and nothing logged.
//
// Making a second demo run in a browser would double the verification for no
// proportionate gain; leaving this one desktop-only would defer the one class
// of failure a desktop run provably cannot catch, because a native adapter
// reports hardware limits far above the WebGPU floor.
//
// What it exercises: baked poses and the 60 Hz grid; ClipPlay crossfade; the
// four-play cap and its report; the rest frame, which is a real pose rather
// than a collapse; PoseBytes and MorphBytes; sparse morph weights; the
// MorphWeights override winning over the animated result; the attribute mask
// intersected with what the base primitive authored; degenerate single-joint
// skins on a file with no skins key at all; and u8 index widening.
//
// # The four stations
//
// Keys 1-4 fly the camera to a station; 0 or R returns to the overview, and R
// also rewinds the clock. The order is left to right, which is the order of
// this list, of the stations table below, and of the HUD.
//
//	1 fox      Fox                     a real 24-joint rig: crossfade, and the rest frame beside it
//	2 interp   InterpolationTest       the 3x3 interpolation grid, and the four-play cap beside it
//	3 cube     AnimatedMorphCube x2    the attribute mask: the same cube at two strides
//	4 stress   MorphStressTest         eight shapes, sparse weights, and the MorphWeights override
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
// # The known gap
//
// No model here is both skinned and morphed, so morph-then-skin ordering - the
// one thing glTF is emphatic about - is untested by this demo. No file in the
// Khronos repository mixes the two either, so this is a gap to record rather
// than one to close by picking a better asset. The packing half of it is
// covered in scene by a built fixture that asserts both counts land in one
// block; what has no coverage anywhere, here included, is the shader applying
// them in that order.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
	"github.com/dvoyni/cog/wgpu"
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
// passes, and the implicit forward pass preserves colour rather than clearing
// it, so the frame's one colour clear is canvas's on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The vendored asset set is not reachable through storage's default read
	// mount - that is the executable's own directory, and `go run` builds into
	// a temporary one - so the demo mounts it explicitly and refuses to start
	// without it.
	storageConfig, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	config := map[kernel.PluginName]any{
		storage.Name: storageConfig,
		wgpu.Name: wgpu.DefaultConfig().
			WithTitle("cog examples: scene animated").
			// Launched at the size the reference screenshot was taken at
			// rather than resized into it: a runtime resize leaves the
			// viewport un-refitted. These are logical units, so the file
			// beside this one is larger on a display that scales.
			WithSize(windowWidth, windowHeight),
	}

	// The demo plugin is last because it records into the queues the plugins
	// before it declare.
	plugins := []kernel.Plugin{
		storage.New(),
		input.New(),
		gfx.New(),
		canvas.New(),
		scene.New(),
		wgpu.New(),
		New(),
	}

	kernel.New(config).Handler(report).WithPlugins(plugins...).Run(ctx)
}

// report is the demo's error handler, and it is not optional here.
//
// The default handler logs and returns true, which terminates the engine. This
// demo provokes a report on purpose - the interpolation station offers nine
// clip plays against a cap of four - so under the default it would log once and
// shut down on the first frame the model became resident, on the desktop and in
// the browser alike. The cap is a report rather than a failure precisely
// because dropping the lightest plays is a recoverable degradation, and a demo
// built to show that has to survive it.
//
// Everything else is logged and survived too. A demo is a thing you look at:
// one failed texture should cost that texture, not the window.
func report(err error) bool {
	log.Printf("animated: %v", err)
	return false
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "animated"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

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
// it carries no placement: what stands at a station here is several draws with
// several different jobs, and each one is placed by the code that records it.
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

// The station indices the frame refers to by name.
const (
	stationFox = iota
	stationInterp
	stationCube
	stationStress
)

// Fox is the skinned station: a real 24-joint rig with an inverse bind per
// joint, every vertex weighted across four influences, and three clips. Its
// JOINTS_0 are u16, which is the ordinary width; the u8 case this demo covers
// is InterpolationTest's index buffer, not a joint index.
//
// The file is authored in centimetres and lies along Z - 25 wide, 79 tall and
// 155 long - so it is scaled down hard and stood at its own middle in Z. The
// numbers are the file's own POSITION accessor bounds, tabulated here because a
// resident model cannot yet be asked for them through the public surface;
// Bounds on the lookup facade is what will let a demo compute this instead.
const (
	foxScale = 0.027
	// The file's middle in Z, in model units: (-88.095 + 66.625) / 2.
	foxCenterZ = -10.735
	// minY is -0.122, so the lift that stands it on the ground is negligible
	// and stated rather than dropped, because the next file swapped in here
	// will not have one.
	foxLift = 0.122 * foxScale
	// The two foxes stand this far either side of the station's middle.
	foxSpread = 2.4
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

// crossfadePeriod is how long the walk-to-run-and-back blend takes. It is long
// against both clips - Walk runs 0.71s and Run 1.16s - so the gait cycles many
// times inside one crossfade and the blend reads as a change of gait rather
// than as a stutter.
const crossfadePeriod = 4.0

// crossfadePhase shifts the blend within its period, as a fraction of one.
//
// It exists because startTime is already spoken for. The reference time is
// pinned by the two stations that read from a file's own clip lengths, and the
// crossfade is the one curve in the demo that is not - it is this demo's own
// function of the clock - so it is the one that can be moved to meet the
// others. Without it, every time that suits the interpolation grid lands the
// blend on one clip or the other exactly, because the grid's clips run two
// seconds and the crossfade four.
const crossfadePhase = 0.65

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
// Each cube is drawn as its own Node draw, so each gets its own clip at full
// weight and all nine move at once. Drawing the model once with nine plays
// cannot do that - four is the cap, and the blend is a weighted mean, so the
// survivors would move at a quarter amplitude. That copy stands beside this one
// and is exactly what the cap station shows.
var interpGrid = [3][3]interpNode{
	// Row 0: scale.
	{{"Linear Scale", "Cube.001"}, {"Step Scale", "Cube"}, {"CubicSpline Scale", "Cube.002"}},
	// Row 1: rotation.
	{{"Linear Rotation", "Cube.005"}, {"Step Rotation", "Cube.003"}, {"CubicSpline Rotation", "Cube.004"}},
	// Row 2: translation.
	{{"Linear Translation", "Cube.009"}, {"Step Translation", "Cube.006"}, {"CubicSpline Translation", "Cube.008"}},
}

// InterpClips is how many clips the file declares, which is how many the cap
// copy offers.
const InterpClips = 9

// The interpolation grid's own geometry. The cubes are unit cubes either side
// of the origin, so a cell is 2 model units and the spacing has to clear the
// translation clips, which move a cube a whole cell.
const (
	interpScale  = 0.34
	interpCell   = 2.9 * interpScale
	interpBaseY  = 1.2
	interpSpread = 2.4 // how far the two grids stand either side of the station
)

// The weight each row of the grid is offered at on the cap copy, bottom row
// first. They are not arbitrary and they are not rigged: the cap keeps the four
// heaviest, so a translation row at 1.0 takes three of the four places and the
// first rotation clip offered takes the last. Which four survive falls out of
// the rule rather than being chosen, and the picture it makes - the top row
// moving, one cube of the middle row moving, the bottom row frozen - is what
// the cap looks like from outside.
//
// The survivors move at a fraction of their amplitude, and that is the honest
// half of the lesson rather than a defect: weights are normalised across a
// draw's plays because the blend is a weighted mean of TRS, so nine clips
// through a four-play cap is not nine animations, it is four at about a
// quarter each.
var interpRowWeights = [3]float32{0.25, 0.5, 1.0}

// CapPlays is how many of the offered plays survive, which is scene's own
// per-draw limit. It is spelled out here so the HUD and the assertions read the
// same number.
const CapPlays = 4

// The morph cube station. The two files are the same cube: the plain one
// authors a TANGENT and carries tangent deltas, the quantized one authors
// neither, so their delta records are three slots and two. The node inside
// each already carries a scale of 100 over a mesh authored at a hundredth
// scale, so the model arrives two units across and needs none of its own.
const (
	cubeSpread = 1.7
	cubeLift   = 1.35
	cubeYaw    = 0.9
	cubeClip   = "Square"
)

// The morph stress station. Eight named shapes on a two-primitive mesh, and
// three weights-only clips - a weights channel produces no joint, so the file
// bakes no poses at all and every draw of it binds the null skin's pose row
// while binding its own deltas.
const (
	stressScale  = 0.70
	stressLift   = 1.30
	stressSpread = 1.7
	stressClip   = "TheWave"
	// StressTargets is the file's flattened target count: one node, so one run
	// of eight slots rather than eight per primitive.
	StressTargets = 8
	// shapeDwell is how long the override holds each shape before moving to
	// the next.
	shapeDwell = 0.9
)

// StressShapeNames are the file's own target names, in the order MorphWeights
// is positional over. They are tabulated so the HUD can name the shape the
// override is holding without a per-frame lookup; the demo asserts they match
// what MorphTargets reports rather than trusting the table.
var StressShapeNames = [StressTargets]string{
	"Key 1", "Key 2", "Key 3", "Key 4", "Key 5", "Key 6", "Key 7", "Key 8",
}

// Animated is the demo's gameplay plugin: it records the whole frame and owns
// the step counter, the orbit, the clip times and the numbers the HUD prints.
type Animated struct {
	step   int
	paused bool
	// override is whether the stress station's second copy holds a
	// MorphWeights array of its own. Toggling it off is the direct A/B: the
	// copy falls back to the animated weights and starts moving with its
	// neighbour.
	override  bool
	azimuth   float32
	elevation float32
	// focus is 0 for the overview and 1..4 for a station close-up, which is
	// the key that selected it.
	focus int
	stats stats
	rate  rate
	// plays and weights are the scratch slices the record path builds into, so
	// a frame that offers nine plays and eight morph weights allocates nothing.
	plays   []scene.ClipPlay
	weights []float32
	// clips is the scratch ClipInfo slice the residency read fills.
	clips []scene.ClipInfo
	// resident is which paths reported residency on the last frame, for the
	// HUD. Clips' ok is the residency predicate the API has before the lookup
	// facade lands, and it is false for a missing, loading and failed path
	// alike, which is exactly what "not drawable yet" means.
	resident [len(ModelPaths)]bool
	// memory is the pose and delta bytes each model reported on the last
	// frame, and the two totals. They are the numbers a rig's storage is read
	// off, and the reason the query exists.
	memory memory
}

// memory is what the lookup facade reported about GPU-resident animation data
// on the last frame.
type memory struct {
	pose       [len(ModelPaths)]int
	morph      [len(ModelPaths)]int
	totalPose  int
	totalMorph int
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is the driver's fixed timestep,
// nor from the step counter, which is the same number however long a frame took.
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
type stats struct {
	passes    int
	ops       int
	recorded  int
	culled    int
	instances int
	batches   int
}

// New builds the demo plugin at its documented starting pose, paused, with the
// morph override on.
func New() *Animated {
	return &Animated{
		paused:    true,
		override:  true,
		azimuth:   startAzimuth,
		elevation: startElevation,
		step:      int(startTime * stepsPerSecond),
	}
}

func (a *Animated) Name() kernel.PluginName { return Name }

func (a *Animated) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (a *Animated) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](a.draw)
	return nil
}

// setViewport fits the logical screen inside the window, swapping the axes when
// the window is taller than it is wide.
func setViewport() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
	var setDesiredViewport func(kernel.Kernel, app.SetDesiredViewportRequest) (app.SetDesiredViewportResponse, error)
	return func(access kernel.ResourceAccess) {
			setDesiredViewport = access.Uses[app.SetDesiredViewportCmd]()
		}, func(k kernel.Kernel, event app.WindowSizeChangeEvent) error {
			if event.Width <= 0 || event.Height <= 0 {
				return nil
			}
			width, height := float32(screenWidth), float32(screenHeight)
			if event.Height > event.Width {
				width, height = height, width
			}
			_, err := setDesiredViewport(k,
				app.SetDesiredViewportRequest{Mode: app.ViewportFit, Width: width, Height: height})
			return err
		}
}

// draw records the whole frame: the camera, the ground, the four stations and
// the HUD.
//
// It holds the Lookup and the filesystem because the clip and memory reads need
// a LookupAccess, which is the facade a demo builds from them. Each read is a
// map hit and a copy into a scratch slice; nothing here parses or uploads.
func (a *Animated) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var inputState kernel.Read[*input.State]
	var lookup kernel.Write[*scene.Lookup]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			inputState = access.GetRead[*input.State]()
			lookup = access.GetWrite[*scene.Lookup]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			q := sceneQueue.Get()
			a.rate.measure(time.Now())
			a.readStats(q)
			a.advance(inputState.Get())
			a.record(q)
			a.readLookup(scene.NewLookupAccess(k, lookup.Get()))
			a.hud(canvasQueue.Get())
			return nil
		}
}

// advance steps the demo's own clock and applies the input. Input is read
// before the step so a held arrow moves the camera on the very frame it is
// pressed.
func (a *Animated) advance(state *input.State) {
	if state != nil {
		if state.JustPressed(input.KeySpace) {
			a.paused = !a.paused
		}
		if state.JustPressed(input.KeyO) {
			a.override = !a.override
		}
		step := orbitSpeed * fixedStep
		if state.Pressed(input.KeyLeft) {
			a.azimuth -= step
		}
		if state.Pressed(input.KeyRight) {
			a.azimuth += step
		}
		if state.Pressed(input.KeyUp) {
			a.elevation = m.Clamp(a.elevation+step, -0.2, 1.4)
		}
		if state.Pressed(input.KeyDown) {
			a.elevation = m.Clamp(a.elevation-step, -0.2, 1.4)
		}
		for i, key := range focusKeys {
			if state.JustPressed(key) {
				a.setFocus(i + 1)
			}
		}
		if state.JustPressed(input.Key0) {
			a.setFocus(0)
		}
		if state.JustPressed(input.KeyR) {
			a.setFocus(0)
			a.step = int(startTime * stepsPerSecond)
			a.paused = true
			a.override = true
		}
	}
	if !a.paused {
		a.step++
	}
}

// focusKeys are the number keys that fly the camera to a station, in station
// order.
var focusKeys = [len(stations)]input.Key{
	input.Key1, input.Key2, input.Key3, input.Key4,
}

// setFocus points the camera at a station, or back at the overview, returning
// the orbit to the documented azimuth and elevation so a focus key always lands
// on the same picture.
func (a *Animated) setFocus(focus int) {
	a.focus = focus
	a.azimuth, a.elevation = startAzimuth, startElevation
}

// Time is the demo's clock: accumulated fixed steps. Every clip play's Time is
// this number, so the whole row is on one timeline and a step is a step
// everywhere.
func (a *Animated) Time() float32 { return float32(a.step) * fixedStep }

// target and radius are the orbit the camera is on, which is the overview's or
// the focused station's.
func (a *Animated) target() m.Vec3 {
	if a.focus == 0 {
		return orbitTarget
	}
	s := &stations[a.focus-1]
	return m.Vec3{X: s.x, Y: s.height}
}

func (a *Animated) radius() float32 {
	if a.focus == 0 {
		return overviewRadius
	}
	return stations[a.focus-1].radius
}

// eye is the camera's position on its orbit.
func (a *Animated) eye() m.Vec3 {
	return eyeAt(a.target(), a.radius(), a.azimuth, a.elevation)
}

// eyeAt is a camera position on an orbit: azimuth about Y from +Z, elevation
// above the horizontal.
func eyeAt(target m.Vec3, radius, azimuth, elevation float32) m.Vec3 {
	cosElevation := float32(math.Cos(float64(elevation)))
	return target.Add(m.Vec3{
		X: radius * cosElevation * float32(math.Sin(float64(azimuth))),
		Y: radius * float32(math.Sin(float64(elevation))),
		Z: radius * cosElevation * float32(math.Cos(float64(azimuth))),
	})
}

// record records the frame: one camera, the ground and the four stations.
func (a *Animated) record(q *scene.OpQueue) {
	q.Camera(CameraMain, scene.CameraDescr{
		Transform: scene.LookAt(a.eye(), a.target(), m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      nearPlane,
		Far:       farPlane,
		// Everything else is left at its zero value, and every zero is the
		// default: Projection is Perspective, CullMask is LayersAll,
		// SunIntensity and AmbientIntensity are 1, and Passes is empty, which
		// emits one implicit forward pass at the camera's own id.
		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	})
	q.Plane(0, m.Vec3{}, m.Vec2{X: groundWidth, Y: groundDepth}, groundColor)

	a.recordFox(q)
	a.recordInterp(q)
	a.recordCube(q)
	a.recordStress(q)
}

// recordFox draws the skinned station: a fox crossfading between two clips, and
// a fox with no plays at all.
//
// The still one is the point of the pair. An empty Plays draws row 0 of the
// bake, which is the authored hierarchy resolved once - a real standing pose,
// not a collapse - and the difference between "the rest frame is a pose" and
// "the rest frame is a zeroed buffer" is only visible against a moving fox
// beside it.
func (a *Animated) recordFox(q *scene.OpQueue) {
	station := &stations[stationFox]
	walk, run := a.FoxBlend()
	a.plays = append(a.plays[:0],
		scene.ClipPlay{Clip: foxWalk, Time: a.Time(), Loop: true, Weight: walk},
		scene.ClipPlay{Clip: foxRun, Time: a.Time(), Loop: true, Weight: run},
	)
	q.Model(0, foxPath, scene.ModelDraw{
		Transform: foxTransform(station.x - foxSpread),
		Plays:     a.plays,
	})
	// No Plays at all: the rest frame.
	q.Model(0, foxPath, scene.ModelDraw{Transform: foxTransform(station.x + foxSpread)})
}

// foxTransform stands one fox on the ground at x, turned to face the camera's
// side of the row and carried to its own middle in Z so the two line up with
// each other rather than with the file's origin.
func foxTransform(x float32) scene.Transform {
	yaw := m.QuatAxisAngle(m.Vec3{Y: 1}, foxYaw)
	middle := m.TRS4(m.Vec3{}, yaw, m.Vec3{X: foxScale, Y: foxScale, Z: foxScale}).
		TransformPoint(m.Vec3{Z: foxCenterZ})
	return scene.Transform{
		Position: m.Vec3{X: x - middle.X, Y: foxLift, Z: -middle.Z},
		Rotation: yaw,
		Scale:    foxScale,
	}
}

// foxYaw turns the fox broadside to the overview camera, which stands on +Z.
// A gait is read from the side; head-on, a walk and a run look the same.
const foxYaw = math.Pi / 2

// FoxBlend is the two crossfade weights at the current time, Walk first. They
// slide between 1/0 and 0/1 over crossfadePeriod and back, so both are real for
// most of the cycle and the reference frame catches the blend mid-slide rather
// than at either end.
//
// They are not normalised here. Scene normalises a draw's plays before anything
// is packed - the blend is a weighted mean of TRS, so it must - and a demo that
// pre-normalised would be hiding the one property worth demonstrating.
func (a *Animated) FoxBlend() (walk, run float32) {
	phase := 2 * math.Pi * (float64(a.Time())/crossfadePeriod + crossfadePhase)
	run = float32(0.5 - 0.5*math.Cos(phase))
	return 1 - run, run
}

// recordInterp draws the interpolation station: the nine-cube grid, each cube a
// Node draw with its own clip at full weight, and beside it the same file drawn
// once with all nine clips offered at once.
//
// Nine draws rather than one is the whole difference. A draw's instances share
// its plays, so nine independently animated things are nine calls - and here
// they have to be, because one call could carry only four of the nine and would
// dilute those four to a quarter each.
func (a *Animated) recordInterp(q *scene.OpQueue) {
	station := &stations[stationInterp]
	left := station.x - interpSpread
	for row := range interpGrid {
		for column := range interpGrid[row] {
			cell := interpGrid[row][column]
			a.plays = append(a.plays[:0],
				scene.ClipPlay{Clip: cell.clip, Time: a.Time(), Loop: true, Weight: 1})
			q.Model(0, interpPath, scene.ModelDraw{
				// A Node draw re-roots: the cube's authored place in the
				// file's own grid is discarded and this transform replaces it,
				// which is what lets the demo lay the grid out itself. The
				// clip still reaches the node, because the pose buffer holds
				// bone world transforms and the re-root cancels the authored
				// rest transform rather than the animated one.
				Node:      cell.node,
				Transform: interpCellTransform(left, row, column),
				Plays:     a.plays,
			})
		}
	}
	a.recordInterpCap(q, station.x+interpSpread)
}

// interpCellTransform places one cube of a grid whose middle column stands at x.
func interpCellTransform(x float32, row, column int) scene.Transform {
	return scene.Transform{
		Position: m.Vec3{
			X: x + float32(column-1)*interpCell,
			Y: interpBaseY + float32(row)*interpCell,
		},
		Scale: interpScale,
	}
}

// recordInterpCap draws the second grid: one draw of the whole file, offered
// every clip it declares, so scene's four-play cap decides which four survive.
//
// This is one Model call and one report. The cap drops the lightest plays and
// reports once per model rather than failing the draw, which is why the grid
// beside it still renders nine cubes - five of them standing at their rest
// pose, which is what a dropped play looks like from outside.
func (a *Animated) recordInterpCap(q *scene.OpQueue, x float32) {
	a.plays = a.plays[:0]
	for row := range interpGrid {
		for column := range interpGrid[row] {
			a.plays = append(a.plays, scene.ClipPlay{
				Clip:   interpGrid[row][column].clip,
				Time:   a.Time(),
				Loop:   true,
				Weight: interpRowWeights[row],
			})
		}
	}
	// No Node selector: the whole scene, in the file's own layout, which is
	// the same 3x3 grid this demo lays out by hand next to it. It is scaled and
	// lifted to sit level with the grid beside it; the file's own cell is 3.4
	// units against this demo's interpCell, so the two are close but not
	// identical, and the cap copy is the wider of the two.
	q.Model(0, interpPath, scene.ModelDraw{
		Transform: scene.Transform{
			Position: m.Vec3{X: x, Y: interpBaseY},
			Scale:    interpScale,
		},
		Plays: a.plays,
	})
}

// recordCube draws the attribute-mask station: the morph cube and its quantized
// twin, playing the same clip side by side.
//
// They deform identically and cost different amounts of GPU memory, because the
// mask a delta record is packed against is intersected with what the base
// primitive authored rather than taken from the targets. The plain file authors
// a TANGENT and its record is three slots; the quantized file authors none and
// its record is two. A mask read off the targets alone would give both the same
// stride, and nothing else in the vendored set can tell the two rules apart.
func (a *Animated) recordCube(q *scene.OpQueue) {
	station := &stations[stationCube]
	a.plays = append(a.plays[:0],
		scene.ClipPlay{Clip: cubeClip, Time: a.Time(), Loop: true, Weight: 1})
	q.Model(0, cubePath, scene.ModelDraw{
		Transform: cubeTransform(station.x - cubeSpread),
		Plays:     a.plays,
	})
	q.Model(0, quantizedPath, scene.ModelDraw{
		Transform: cubeTransform(station.x + cubeSpread),
		Plays:     a.plays,
	})
}

// cubeTransform stands one morph cube at x, turned off square so two of its
// faces catch the sun at different angles. Face-on it reads as a grey rectangle
// and the deformation is only visible at its silhouette; turned, the shape
// change reads across the whole model.
//
// Both cubes take the same yaw, and the close-up camera stands well back
// instead of close in. Yawing each one to face the camera would present the
// same face from both, but it would also stand them at different angles to a
// directional sun, and two identical cubes at visibly different brightnesses
// invite exactly the conclusion this station exists to rule out. One shared
// yaw keeps the light identical; the distance is what keeps the perspective
// skew between them down to a few degrees, which is far less misleading than a
// difference in shading.
func cubeTransform(x float32) scene.Transform {
	return scene.Transform{
		Position: m.Vec3{X: x, Y: cubeLift},
		Rotation: m.QuatAxisAngle(m.Vec3{Y: 1}, cubeYaw),
	}
}

// recordStress draws the morph station: one copy taking its weights from a
// clip, and one copy overriding them.
//
// Both copies play the same clip. The second also carries a MorphWeights array,
// which overrides the animated result wholesale - so with the override on the
// two copies disagree, and with it off (key o) they move together. That is the
// contract stated as a picture: a non-nil MorphWeights wins, and a nil one
// falls back to the animated weights, then the node's, then the mesh's, then
// zero.
//
// The array is also sparse on purpose: exactly one of the eight shapes is
// nonzero at a time. Only the nonzero weights are packed into the frame's anim
// block, so what reaches the GPU is one target rather than eight, which is the
// whole reason the blend is CPU-side.
func (a *Animated) recordStress(q *scene.OpQueue) {
	station := &stations[stationStress]
	a.plays = append(a.plays[:0],
		scene.ClipPlay{Clip: stressClip, Time: a.Time(), Loop: true, Weight: 1})
	q.Model(0, stressPath, scene.ModelDraw{
		Transform: stressTransform(station.x - stressSpread),
		Plays:     a.plays,
	})
	draw := scene.ModelDraw{
		Transform: stressTransform(station.x + stressSpread),
		Plays:     a.plays,
	}
	if a.override {
		draw.MorphWeights = a.OverrideWeights()
	}
	q.Model(0, stressPath, draw)
}

func stressTransform(x float32) scene.Transform {
	return scene.Transform{Position: m.Vec3{X: x, Y: stressLift}, Scale: stressScale}
}

// OverrideWeights is the sparse MorphWeights array the second stress copy
// carries: one shape at full weight, the rest zero, moving on every shapeDwell
// seconds.
//
// It is built into the demo's own scratch slice and handed straight to the
// draw. That is safe because ModelDraw copies MorphWeights into the frame's
// arena, so the caller may reuse its backing the moment the call returns.
func (a *Animated) OverrideWeights() []float32 {
	a.weights = append(a.weights[:0], make([]float32, StressTargets)...)
	a.weights[a.OverrideShape()] = 1
	return a.weights
}

// OverrideShape is which of the eight shapes the override is holding.
func (a *Animated) OverrideShape() int {
	shape := int(a.Time()/shapeDwell) % StressTargets
	if shape < 0 {
		shape += StressTargets
	}
	return shape
}

// readLookup asks the facade what it knows about each file: whether it is
// drawable yet, and what its baked animation costs.
//
// Clips' ok is the residency predicate the API has before the lookup facade
// lands - it is false for a missing, loading and failed path alike - and it is
// the apt one here, because a demo that cannot name a clip has nothing to play.
func (a *Animated) readLookup(la scene.LookupAccess) {
	for i, path := range ModelPaths {
		a.clips, a.resident[i] = la.Clips(path, a.clips[:0])
		a.memory.pose[i], _ = la.PoseBytes(path)
		a.memory.morph[i], _ = la.MorphBytes(path)
	}
	a.memory.totalPose = la.TotalPoseBytes()
	a.memory.totalMorph = la.TotalMorphBytes()
}

// ResidentCount is how many files reported residency on the last frame.
func (a *Animated) ResidentCount() int {
	n := 0
	for _, ok := range a.resident {
		if ok {
			n++
		}
	}
	return n
}

// readStats reads the previous frame's flush result back out of the queue.
// Passes publishes the frame the last flush consumed, which is what a HUD can
// print without stalling the pipeline to ask about the frame it is recording.
func (a *Animated) readStats(q *scene.OpQueue) {
	views := q.Passes(nil)
	a.stats = stats{passes: len(views), ops: len(q.Ops(nil))}
	for i := range views {
		a.stats.recorded += views[i].Recorded
		a.stats.culled += views[i].Culled
		a.stats.instances += views[i].Instances
		a.stats.batches += len(views[i].Batches)
	}
}

// RecordedDraws is how many draws the frame flushes to once every file is
// resident: two foxes of one primitive each, nine single-primitive Node draws
// and one ten-primitive whole-file draw for the interpolation station, two
// single-primitive morph cubes, two two-primitive stress copies, and the
// ground.
//
// The counts are spelled out rather than measured at runtime so that an asset
// swapped underneath the demo fails an assertion instead of quietly changing
// the picture.
const (
	foxPrimitives = 1
	// Nine cubes and a plane, which is what the file's one scene flattens to.
	interpPrimitives = 10
	cubePrimitives   = 1
	stressPrimitives = 2
)

const RecordedDraws = 2*foxPrimitives +
	9 + interpPrimitives +
	2*cubePrimitives +
	2*stressPrimitives +
	1
