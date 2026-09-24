// Command pbr is the scene plugin's material and lighting demo: six Khronos
// sample models, arranged as a still life, shaded by the bundled PBR.
//
//	go run ./cmd/scene/pbr
//
// The assets are the point. Every model here was authored and
// screenshot-verified by Khronos against exactly the BRDF scene draws with, so
// "the model looks wrong" is a bug in this tree rather than an open question
// about which approximation was picked. That is what makes this the one demo
// whose visible criterion is falsifiable without a second implementation to
// compare against.
//
// What it exercises: the whole material contract - glTF's verbatim parameter
// names, the five texture slots, the two 1x1 defaults an empty slot binds, the
// Khronos BRDF and EnvBRDFApprox, COLOR_0, and KHR_texture_transform as flat
// per-slot members; alphaMode mapped onto pipeline state, Cull and FrontFace;
// the back-to-front blend sort; point and spot lights, Range zero meaning
// infinite, the sixteen-light cap and its silent drop; and
// KHR_materials_emissive_strength folded in at load beside KHR_lights_punctual
// reaching the app as data it declares itself.
//
// Every drawable is an Entity: a Model Component for each of the seven model
// draws, a Mesh and a Params for the ground and each plinth, a Light for each
// of the twenty-one lamps and a Camera for the eye. The Systems that spawn and
// steer them are one to a file, each named for its System.
//
// # The six stations
//
// Each model stands on its own plinth, scaled into a slot and yawed to face the
// reference eye. Keys 1-6 fly the camera to one of them; 0 or R returns to the
// overview. The keys follow the still life itself - the front row left to
// right, then the back row - which is the order of the stations table below,
// the order the HUD names, and the order this list is in.
//
//	1 bottle    WaterBottle              front left    all five texture slots at once, with real tangents
//	2 compare   CompareBaseColor         front centre  KHR_texture_transform's flat members, and a base-colour texture beside a bare factor
//	3 alpha     AlphaBlendModeTest       front right   the whole alphaMode matrix, drawn twice for the blend sort
//	4 vertex    BoxVertexColors          back left     COLOR_0, and the only primitive here with no material at all
//	5 emissive  EmissiveStrengthTest     back centre   KHR_materials_emissive_strength, 1 through 16
//	6 lights    PointLightIntensityTest  back right    eight KHR_lights_punctual lights, re-declared by this demo
//
// The alpha station is drawn twice, at two depths. One copy's two blended
// primitives cannot tell a depth sort from a mesh-id sort - with two copies the
// correct order interleaves them and a material-keyed sort groups them - and
// pbr_test.go asserts the blended draws reach the backend farthest first.
//
// # Lights, and the cap
//
// Twenty-one punctual lights are declared and sixteen are packed. The eight
// that PointLightIntensityTest declares are read back through ModelLights and
// spawned as Light Entities at the station's world transform, because scene
// converts none of a file's lights automatically: a lamp prop placed forty
// times would blow the cap silently, and which of a file's lights matter is the
// app's judgement. One fill light leaves Range at zero, which is glTF's own
// default and means infinite - it is the only light here that cannot be
// culled, and so always survives to the cap. A spot stands over the bottle and
// another over the alpha panes. Five rim lamps line the front edge with a range
// long enough to reach the camera, and five deep lamps stand behind the back
// row with a range that does not.
//
// The five deep lamps are the five the cap drops, and the arrangement is what
// makes the rule visible rather than merely stated. The ranking is each light's
// own falloff evaluated at the camera position times its colour's luminance, so
// a light whose range window is closed where the camera stands scores zero
// however much it matters to the geometry it sits beside. At the reference pose
// the deep lamps are twenty-three units from the eye against a range of eight,
// so they score nothing and lose; orbiting round behind the still life brings
// the camera inside that range, they start scoring, and they take the places of
// lights that score zero from there - the two spots and the station's own
// eight, none of which reaches the eye either. The count never moves off 16
// while any of that happens, and nothing is reported.
//
// That is the whole reason the drop is silent rather than reported once: which
// sixteen survive is dynamic and camera-shaped, so there is no natural moment
// to report at. It is also where the rule's edge shows, and this demo is the
// first thing in the tree that can see it: contribution is measured at the eye,
// so a lamp lighting the ground behind a model scores nothing there while
// contributing plenty to what the camera is looking at.
//
// Among lights that tie - and every light whose window is closed at the eye
// ties at zero - the first sixteen offered are kept, and scene offers its Light
// Entities in the order its Query walks them. lampsystem.go spawns the lamps
// with that walk in mind; see lampSystem for what that rests on.
//
// # The reference pose
//
// The demo starts at a documented fixed pose - the camera orbits orbitTarget at
// radius overviewRadius, azimuth startAzimuth and elevation startElevation - and
// reference.png beside this file is the frame at that pose.
//
// Nothing in the drawn frame moves on its own. The clock is still accumulated
// fixed steps, and the HUD prints it, but it drives only the orbit rate: every
// frame at the reference pose is the same frame, so the reference screenshot
// can be retaken by launching the demo and capturing it, with no step to hit. A
// still life is what a set of material test cards wants to be, and it buys
// exact reproducibility for the one demo whose acceptance is a picture.
//
// Input may orbit, focus and pause freely, and touching it voids nothing: the
// assertions live in pbr_test.go rather than in the running app.
//
// # What only eyes can judge
//
// On the bottle (key 1): the body is brushed metal and the cap is matte
// plastic, so one bright specular streak runs down the body and slides round it
// as the camera orbits while the cap beside it takes none at all, and the dark
// label band around the base stays matte throughout with its embossed mark
// catching the light along one edge.
//
// One sentence, four failures. A dead metallicRoughness slot makes the cap and
// the body the same finish, either both mirror or both chalk. A dead normal map
// flattens the label's embossing into the print. A base colour sampled in the
// wrong space turns the gold grey and the cap orange. And an occlusion slot
// bound to the wrong default fills in the crease under the cap, which is the
// only place in the frame where that texture does visible work.
//
// Three more, across the row. The five emissive cubes (key 5) double left to
// right - strength 1, 2, 4, 8, 16 - and the last two are the same white,
// because there is no tonemapping anywhere in this pipeline and both are past
// the top of the range; a demo where all five matched would mean
// KHR_materials_emissive_strength never reached the material. Each of the six
// panels (key 6) takes the colour of the lamp in front of it, and each pool
// stops short of its neighbour because Range 1.125 closes the falloff before it
// gets there; a pool that reaches its neighbour is a Range that did not pack.
// And through the near alpha pane the far one shows tinted rather than
// punched through (key 3), which is the back-to-front sort doing its only job.
//
// The plinths are the one thing here that puts a rotated non-uniform basis
// through the bundled PBR's lit path, and they are worth nothing at all to the
// eye - which is a correction to what this comment said when the demo landed.
//
// It claimed each plinth top would tilt by its own yaw under a wrong
// inverse-transpose, and that is false. A plinth is an axis-aligned box, so
// every one of its face normals is an eigenvector of its own scale, and a
// rotation carries that through: world and its inverse-transpose send such a
// normal the same way and differ only in a length that normalising removes. The
// plinths shade identically with SCENE_NONUNIFORM and without it, and a capture
// of each proves it. What does show the flag is a curved surface under the same
// kind of scale, which is the instancing demo's eye criterion
// (https://github.com/dvoyni/cog/issues/96); the claim is retracted here rather
// than deleted, because a falsifiable sentence that was never falsifiable is
// worth saying out loud once.
package main

import (
	"fmt"
	"math"
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
// passes, and its default forward pass preserves colour rather than clearing
// it, so the frame's one colour clear is canvas's on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.
			WithTitle("cog examples: scene pbr").
			// Launched at the size the reference screenshot was taken at
			// rather than resized into it: a runtime resize leaves the
			// viewport un-refitted, and stating the size here means the
			// picture does not move if the default changes underneath. These
			// are logical units, so the file beside this one is larger than
			// this on a display that scales.
			WithSize(windowWidth, windowHeight),
	}
	permanentfs.Configure(config)

	// The demo plugin is last because its Systems read the Components and
	// resources the plugins before it register.
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
		New(),
	}

	engine := kernel.New(config).WithPlugins(plugins...)
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

// Name is the demo plugin's name.
const Name kernel.PluginName = "pbr"

// Demo is the demo's gameplay plugin. It registers the Systems and the one
// resource they share, and keeps a pointer to that resource so a test can read
// what the HUD reads.
type Demo struct {
	pbr *Pbr
}

// New builds the demo plugin at its documented starting pose.
func New() *Demo {
	return &Demo{pbr: &Pbr{azimuth: startAzimuth, elevation: startElevation}}
}

func (d *Demo) Name() kernel.PluginName { return Name }

func (d *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name}
}

type (
	setupSystem   kernel.Subscription[app.InitEvent]
	steerSystem   kernel.Subscription[app.UpdateEvent]
	orbitSystem   kernel.Subscription[app.UpdateEvent]
	lampSystem    kernel.Subscription[app.UpdateEvent]
	hudSystem     kernel.Subscription[app.UpdateEvent]
	viewportFixer kernel.Subscription[app.WindowSizeChangeEvent]
)

func (d *Demo) Register(registrar *kernel.Registrar, _ any) error {
	// storage mounts nothing by default, and the vendored asset set lives in
	// the repository rather than beside the executable, which `go run` builds
	// into a temporary directory - so the demo contributes it explicitly and
	// refuses to start without it. A pbr demo that came up with six missing
	// models would render an empty room and blame the loader.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	registrar.InitResource(d.pbr)

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	registrar.Subscribe[steerSystem](ecs.ToHandler[app.UpdateEvent](registrar, steer)).First()
	// Every System that places an Entity runs Before scene.RecordOnUpdate,
	// so a step draws the world as that step left it rather than whichever side
	// of the tie the scheduler happened to break.
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		After[steerSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[lampSystem](ecs.ToHandler[app.UpdateEvent](registrar, lamps)).
		After[scene.LoadOnUpdate]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[lampSystem]()

	registrar.Subscribe[viewportFixer](setViewport)
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
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose, where the reference screenshot is taken.
const (
	overviewRadius = 14.0
	startAzimuth   = 0.0
	startElevation = 0.22
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
	nearPlane      = 0.1
	farPlane       = 200

	// The sun is deliberately weak: at full strength one directional light
	// washes out twenty-one punctual ones and the demo becomes a test of the
	// sun. It is a multiplier on SunColor rather than SunIntensity so that
	// every intensity on the camera stays at its zero default, which is 1.
	sunStrength = 0.5
)

// sunDirection is the direction the camera's sun travels in.
var sunDirection = m.Vec3{X: -0.35, Y: -1, Z: -0.55}

// orbitTarget is the point the overview camera looks at and orbits, a little
// above the plinths so the ground fills the lower third of the frame.
var orbitTarget = m.Vec3{Y: 2.4}

// The frame's own colours, written in sRGB and converted on the way in: Color
// holds linear components, and a demo that typed linear literals would be
// picking its palette in a space no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.08, 1)
	groundColor   = m.NewColorSrgb(0.34, 0.35, 0.37, 1)
	plinthColor   = m.NewColorSrgb(0.62, 0.60, 0.56, 1)
	sunColor      = m.NewColorSrgb(1, 0.97, 0.92, 1)
	ambientSky    = m.NewColorSrgb(0.17, 0.21, 0.28, 1)
	ambientGround = m.NewColorSrgb(0.11, 0.10, 0.09, 1)
	fillColor     = m.NewColorSrgb(1.00, 0.94, 0.86, 1)
	bottleSpot    = m.NewColorSrgb(1.00, 0.96, 0.90, 1)
	alphaSpot     = m.NewColorSrgb(0.72, 0.86, 1.00, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
)

// rimColors are the five rim lamps' colours, left to right along the front edge,
// and deepColors the five deep ones behind the back row. The two sets are warm
// and cool so that a frame in which the cap has changed its mind is legible from
// the colour on the ground rather than only from the HUD.
var rimColors = [5]m.Color{
	m.NewColorSrgb(1.00, 0.52, 0.34, 1),
	m.NewColorSrgb(1.00, 0.82, 0.45, 1),
	m.NewColorSrgb(0.95, 0.95, 1.00, 1),
	m.NewColorSrgb(1.00, 0.82, 0.45, 1),
	m.NewColorSrgb(1.00, 0.52, 0.34, 1),
}

var deepColors = [5]m.Color{
	m.NewColorSrgb(0.35, 0.85, 1.00, 1),
	m.NewColorSrgb(0.55, 0.65, 1.00, 1),
	m.NewColorSrgb(0.40, 1.00, 0.80, 1),
	m.NewColorSrgb(0.55, 0.65, 1.00, 1),
	m.NewColorSrgb(0.35, 0.85, 1.00, 1),
}

// The ground the whole still life stands on: a unit quad under a Scale of
// groundSide in x and z.
const groundSide = 60

// The plinth every station stands on: a slab. Transform.Scale is per axis, so a
// slab is the unit box Mesh under a flattened Scale and a yaw; that is what
// puts a rotated non-uniform basis through the bundled PBR's lit path, which
// the debug shapes cannot - they are self-lit and never read a normal.
const (
	plinthWidth  = 4.8
	plinthDepth  = 3.6
	plinthHeight = 0.16
)

// The still life's own grid: three stations to a row, the front row short and
// the back row tall so the overview sees both over the top of the plinths. The
// spacing is what keeps a yawed plinth clear of its neighbour - a 4.8 by 3.6
// slab turned half a radian is nearly six units wide across x.
const (
	stationSpacing = 6.5
	frontRowZ      = 3.5
	backRowZ       = -4.0
)

// station is one model's place in the still life.
//
// The bounds each entry is scaled, lifted and centred against are the file's
// own, read out of its POSITION accessors. They are constants here rather than
// read through the device facade's Bounds, because a placement computed from a
// loaded file would move with the file: a file swapped underneath the demo
// moves its model off its plinth rather than silently re-arranging the still
// life, and the reference screenshot keeps meaning one arrangement.
type station struct {
	name string
	path string
	// x and z place the plinth; scale, lift and centre place the model on top
	// of it. lift is -minY*scale plus the plinth's own height, and centre is
	// the file's own middle in x and z, in model units, which is not the origin
	// for every file - BoxVertexColors is authored from a corner.
	x, z    float32
	scale   float32
	lift    float32
	centerX float32
	centerZ float32
	// radius is how far the close-up camera stands from the station's centre,
	// and height how far above the plinth that centre sits.
	radius float32
	height float32
}

// The six stations, front row first. The front row is short and the back row
// tall, so the overview sees both over the top of the plinths.
var stations = [...]station{
	// size 0.109 x 0.260 x 0.109, minY -0.130.
	{name: "bottle", path: "assets/WaterBottle/WaterBottle.glb",
		x: -stationSpacing, z: frontRowZ, scale: 10,
		lift: plinthHeight + 1.30, radius: 4.2, height: 1.5},
	// size 3.200 x 0.980 x 1.000, minY -0.490.
	{name: "compare", path: "assets/CompareBaseColor/CompareBaseColor.glb",
		x: 0, z: frontRowZ, scale: 1.6,
		lift: plinthHeight + 0.784, radius: 4.6, height: 1.0},
	// size 8.600 x 2.400 x 1.300, minY -0.100.
	{name: "alpha", path: "assets/AlphaBlendModeTest/AlphaBlendModeTest.glb",
		x: stationSpacing, z: frontRowZ, scale: 0.55,
		lift: plinthHeight + 0.055, radius: 5.4, height: 0.9},
	// size 1.000 x 1.000 x 1.000, minY 0.000 - authored from a corner, so it is
	// the one file whose middle is not its origin.
	{name: "vertex", path: "assets/BoxVertexColors/BoxVertexColors.glb",
		x: -stationSpacing, z: backRowZ, scale: 2.6,
		lift: plinthHeight, centerX: 0.5, centerZ: 0.5, radius: 5.0, height: 1.3},
	// size 16.004 x 10.010 x 3.999, minY -6.001.
	{name: "emissive", path: "assets/EmissiveStrengthTest/EmissiveStrengthTest.glb",
		x: 0, z: backRowZ, scale: 0.30,
		lift: plinthHeight + 1.80, radius: 6.4, height: 1.6},
	// size 6.602 x 4.917 x 0.063, minY -3.866.
	{name: "lights", path: "assets/PointLightIntensityTest/PointLightIntensityTest.glb",
		x: stationSpacing, z: backRowZ, scale: 0.75,
		lift: plinthHeight + 2.90, radius: 5.6, height: 1.8},
}

// The station indices the Systems refer to by name.
const (
	stationBottle = iota
	stationCompare
	stationAlpha
	stationVertex
	stationEmissive
	stationLights
)

// alphaSecondCopy is where the alpha station's second copy stands, relative to
// the first. It is offset in z so the four blended primitives have four
// distinct depths - which is what makes the blend sort's order an assertion
// rather than a coin flip - and a little in x so the two are separable by eye.
// The offset stays inside the plinth's own footprint: the copy is a second pane
// standing on the same slab, not a model floating behind it.
var alphaSecondCopy = m.Vec3{X: 0.9, Z: -1.7}

// referenceEye is where the camera stands at the documented pose. It is
// computed once rather than typed, and the stations are yawed to face it, so
// moving the pose turns them with it.
var referenceEye = eyeAt(orbitTarget, overviewRadius, startAzimuth, startElevation)

// placement is one station's world transform and the plinth beneath it, built
// once at package init because neither moves.
type placement struct {
	model  m.Transform
	plinth m.Transform
	// center is the station's own middle, which the close-up camera looks at.
	center m.Vec3
	// scale is the uniform scale the model is drawn at, which a light declared
	// in the file's own space has to have its Range multiplied by.
	scale float32
}

var placements = buildPlacements()

func buildPlacements() [len(stations)]placement {
	var out [len(stations)]placement
	for i := range stations {
		s := &stations[i]
		ground := m.Vec3{X: s.x, Z: s.z}
		yaw := m.QuatAxisAngle(m.Vec3{Y: 1}, yawToward(ground, referenceEye))
		// The model's own middle is carried to the plinth's, through the same
		// scale and rotation the Entity is given, so a file authored from a
		// corner stands where a file authored from its centre does.
		middle := m.TRS4(m.Vec3{}, yaw, m.Vec3{X: s.scale, Y: s.scale, Z: s.scale}).
			TransformPoint(m.Vec3{X: s.centerX, Z: s.centerZ})
		out[i] = placement{
			model: m.Transform{
				Position: m.Vec3{X: s.x - middle.X, Y: s.lift, Z: s.z - middle.Z},
				Rotation: yaw,
				Scale:    m.NewVec3(s.scale),
			},
			plinth: m.Transform{
				Position: m.Vec3{X: s.x, Y: plinthHeight / 2, Z: s.z},
				Rotation: yaw,
				Scale:    m.Vec3{X: plinthWidth, Y: plinthHeight, Z: plinthDepth},
			},
			center: m.Vec3{X: s.x, Y: plinthHeight + s.height, Z: s.z},
			scale:  s.scale,
		}
	}
	return out
}

// yawToward is the rotation about Y that turns a station's front, which is +Z in
// every one of these files, toward eye.
func yawToward(ground, eye m.Vec3) float32 {
	return float32(math.Atan2(float64(eye.X-ground.X), float64(eye.Z-ground.Z)))
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

// The lights the demo declares itself, beside the eight it reads out of
// PointLightIntensityTest. They are ranked in the order declared here, and
// that order is load-bearing: past the cap a light replaces the weakest kept
// one only if it beats it, so among lights that all score the same - which is
// what every light whose range window is closed at the eye scores - the first
// sixteen offered are the sixteen kept.
//
// The fill light leaves Range at zero, which is glTF's own default and means
// infinite. It is the one light that cannot be culled - the packed record holds
// 1/range^4, and zero there makes the falloff term exactly 1 with no branch, so
// there is no sphere to test it with - and it is the one light with a real score
// at the reference pose.
const (
	fillHeight    = 9.0
	fillDepth     = 4.0
	fillIntensity = 2.2

	spotIntensity = 26
	spotRange     = 9
	spotInner     = 0.22
	spotOuter     = 0.44
	spotHeight    = 5.0

	// The rim lamps stand in front of the front row. Their range is long
	// enough to reach the camera at the reference pose, so they carry a real
	// contribution there; the pool each one puts on the ground is small even
	// so, because the falloff a fragment sees is inverse-square and Range only
	// closes the window on it.
	rimCount     = len(rimColors)
	rimZ         = 6.0
	rimY         = 1.6
	rimSpacing   = 4.0
	rimIntensity = 3.0
	rimRange     = 14.0

	// The deep lamps stand behind the back row, out of range of the reference
	// eye by a factor of three. They are the five the cap drops.
	deepCount     = len(deepColors)
	deepZ         = -10.0
	deepY         = 2.5
	deepSpacing   = 3.0
	deepIntensity = 3.0
	deepRange     = 8.0
)

// DeclaredLights is how many Light Entities the demo spawns once the lights
// station is resident: one fill, two spots, the eight PointLightIntensityTest
// declares, five rim lamps and five deep ones. It is five more than the cap on
// purpose.
const DeclaredLights = 1 + 2 + ModelDeclaredLights + rimCount + deepCount

// ModelDeclaredLights is how many KHR_lights_punctual lights the lights station
// contributes. They are the file's own, re-declared by this demo at the
// station's world transform.
const ModelDeclaredLights = 8

// MaxLights is scene's per-pass cap, model's MaxLights. It is a fixed constant
// there rather than a Config knob, and it is spelled out here so the HUD and
// the assertions read the same number.
const MaxLights = 16

// Instances is how many instances the frame draws once every model is
// resident: one per primitive of each model, the alpha model twice, plus the
// six plinths and the ground.
const Instances = bottlePrimitives + comparePrimitives + 2*alphaPrimitives +
	vertexPrimitives + emissivePrimitives + lightsPrimitives + len(stations) + 1

// The primitive counts of the six files, which are what a Model Entity expands
// to. They are spelled out rather than counted at runtime so that an asset
// swapped underneath the demo fails an assertion instead of quietly changing the
// picture.
const (
	bottlePrimitives   = 1
	comparePrimitives  = 3
	alphaPrimitives    = 9
	vertexPrimitives   = 1
	emissivePrimitives = 6
	// Six "Test" nodes share a two-primitive mesh, and the label board is a
	// seventh node with one.
	lightsPrimitives = lightsTestNodes*lightsTestPrimitives + 1
	lightsTestNodes  = 6
	// lightsTestPrimitives is the primitive count of the mesh the six "Test"
	// nodes share.
	lightsTestPrimitives = 2
)

// SceneDraws is how many draws scene makes once every model is resident. A
// Batch is one instanced draw of every opaque instance whose key is equal, and
// a blended instance is a draw of its own, so it is Instances less what
// shares a Batch: the six plinths are one Batch, the alpha model's opaque
// primitives pair up across its two copies, and the lights model's six "Test"
// nodes are one Batch per primitive of the mesh they share. Nothing else in the
// frame shares a key with anything.
const SceneDraws = Instances - (len(stations) - 1) - (alphaPrimitives - BlendPrimitives) -
	lightsTestPrimitives*(lightsTestNodes-1)

// BlendPrimitives is how many of the alpha model's primitives are alphaMode
// BLEND: TestBlendMesh and DecalBlendMesh, both MatBlend. Everything else in the
// frame is OPAQUE or MASK, and MASK sorts with opaque.
const BlendPrimitives = 2
