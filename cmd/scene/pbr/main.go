// Command pbr is the scene plugin's material and lighting demo: six Khronos
// sample models, arranged as a still life, shaded by the bundled PBR.
//
//	go run ./cmd/scene/pbr
//
// The assets are the point. Every model here was authored and
// screenshot-verified by Khronos against exactly the BRDF scene implements, so
// "the model looks wrong" is a bug in this tree rather than an open question
// about which approximation was picked. That is what makes this the one demo
// whose visible criterion is falsifiable without a second implementation to
// compare against.
//
// What it exercises: the whole material contract - glTF's verbatim parameter
// names, the five texture slots, the two 1x1 defaults an empty slot binds, the
// Khronos BRDF and EnvBRDFApprox, COLOR_0, and KHR_texture_transform as flat
// per-slot members; alphaMode mapped onto pipeline state, Cull and FrontFace;
// the back-to-front blend bucket; point and spot lights, Range zero meaning
// infinite, the sixteen-light cap and its silent drop; and
// KHR_materials_emissive_strength folded in at load beside KHR_lights_punctual
// reaching the app as data it declares itself.
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
// correct order interleaves them and a material-keyed sort groups them, which is
// what pbr_test.go asserts.
//
// # Lights, and the cap
//
// Twenty-one punctual lights are recorded and sixteen are packed. The eight
// that PointLightIntensityTest declares are read back through ModelLights and
// re-declared at the station's world transform, because scene converts none of
// a file's lights automatically: a lamp prop placed forty times would blow the
// cap silently, and which of a file's lights matter is the app's judgement. One
// fill light leaves Range at zero, which is glTF's own default and means
// infinite - it is the only light here that cannot be culled, and so always
// survives to the cap. A spot stands over the bottle and another over the alpha
// panes. Five rim lamps line the front edge with a range long enough to reach
// the camera, and five deep lamps stand behind the back row with a range that
// does not.
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
// eight, none of which reaches the eye either. The count on the HUD never moves
// off 16 while any of that happens, and nothing is reported.
//
// That is the whole reason the drop is silent rather than reported once: which
// sixteen survive is dynamic and camera-shaped, so there is no natural moment
// to report at. It is also where the rule's edge shows, and this demo is the
// first thing in the tree that can see it: contribution is measured at the eye,
// so a lamp lighting the ground behind a model scores nothing there while
// contributing plenty to what the camera is looking at.
//
// # The reference pose
//
// The demo starts at a documented fixed pose - the camera orbits orbitTarget at
// radius overviewRadius, azimuth startAzimuth and elevation startElevation - and
// reference.png beside this file is the frame at that pose.
//
// Nothing in the recorded frame moves on its own. The clock is still
// accumulated fixed steps, and the HUD prints it, but it drives only the orbit
// rate: every frame at the reference pose is the same frame, so the reference
// screenshot can be retaken by launching the demo and capturing it, with no step
// to hit. A still life is what a set of material test cards wants to be, and it
// buys exact reproducibility for the one demo whose acceptance is a picture.
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
// KHR_materials_emissive_strength never reached the record. Each of the six
// panels (key 6) takes the colour of the lamp in front of it, and each pool
// stops short of its neighbour because Range 1.125 closes the falloff before it
// gets there; a pool that reaches its neighbour is a Range that did not pack.
// And through the near alpha pane the far one shows tinted rather than
// punched through (key 3), which is the back-to-front sort doing its only job.
//
// The plinths are the one thing here to look at for the normal transform. Each
// is a slab turned to its own yaw, so each is a rotated non-uniform basis - the
// first the bundled PBR shades anywhere in this tree - and every top reads as
// the same flat horizontal surface it is: the sun strikes them all at one angle
// whatever a plinth is turned to, so two plinths the same distance from the same
// lamp come out the same brightness. A wrong inverse-transpose tilts each top by
// its own yaw instead, and the six go out of key with one another, which is a
// difference between six things in one frame rather than a judgement about one.
package main

import (
	"context"
	"fmt"
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
	// without it. A pbr demo that came up with six missing models would render
	// an empty room and blame the loader.
	storageConfig, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	config := map[kernel.PluginName]any{
		storage.Name: storageConfig,
		wgpu.Name: wgpu.DefaultConfig().
			WithTitle("cog examples: scene pbr").
			// Launched at the size the reference screenshot was taken at
			// rather than resized into it: a runtime resize leaves the
			// viewport un-refitted, and stating the size here means the
			// picture does not move if the default changes underneath. These
			// are logical units, so the file beside this one is larger than
			// this on a display that scales.
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

	kernel.New(config).WithPlugins(plugins...).Run(ctx)
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "pbr"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

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

// orbitTarget is the point the overview camera looks at and orbits, a little
// above the plinths so the ground fills the lower third of the frame.
var orbitTarget = m.Vec3{Y: 2.4}

// Pbr is the demo's gameplay plugin: it records the whole frame and owns the
// step counter, the orbit, the station table and the numbers the HUD prints.
type Pbr struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
	// focus is 0 for the overview and 1..6 for a station close-up, which is the
	// key that selected it.
	focus int
	stats stats
	rate  rate
	// modelLights is the scratch ModelLights reads into, kept so a frame that
	// re-declares a file's eight lights allocates nothing.
	modelLights []scene.ModelLight
	// resident is which stations reported residency on the last frame, for the
	// HUD. ModelLights' ok is the only residency predicate the API has before
	// the lookup facade lands, and it is false for a missing, loading and
	// failed path alike, which is exactly what "not drawable yet" means.
	resident [len(stations)]bool
	// declared counts the punctual lights the last frame recorded, which is the
	// number the pass's own Lights is capped from.
	declared int
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is the driver's fixed timestep,
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
type stats struct {
	passes    int
	ops       int
	recorded  int
	culled    int
	instances int
	lights    int
	batches   int
}

// New builds the demo plugin at its documented starting pose.
func New() *Pbr { return &Pbr{azimuth: startAzimuth, elevation: startElevation} }

func (p *Pbr) Name() kernel.PluginName { return Name }

func (p *Pbr) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *Pbr) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.draw)
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

// The ground the whole still life stands on.
const groundSide = 60

// The plinth every station stands on: a slab, and the demo's only non-uniform
// scale. It goes through Transform.Matrix, because Transform.Scale is scalar by
// design and a slab is not; that is what puts a rotated non-uniform basis
// through the bundled PBR's lit path, which nothing in box or procedural does -
// Line3D and WireBox build such a matrix but are self-lit and never read a
// normal.
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
// own, read out of its POSITION accessors. They are constants here because a
// resident model cannot yet be asked for its bounds through the public surface;
// the lookup facade's Bounds is what will let a demo compute this instead of
// tabulating it, and until then a file swapped underneath the demo moves its
// model off its plinth rather than failing anything.
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

// The station indices the frame refers to by name.
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
// distinct depths - which is what makes the blend bucket's order an assertion
// rather than a coin flip - and a little in x so the two are separable by eye.
// The offset stays inside the plinth's own footprint: the copy is a second pane
// standing on the same slab, not a model floating behind it.
var alphaSecondCopy = m.Vec3{X: 0.9, Z: -1.7}

// referenceEye is where the camera stands at the documented pose. It is
// computed once rather than typed, and the stations are yawed to face it, so
// moving the pose turns them with it.
var referenceEye = eyeAt(orbitTarget, overviewRadius, startAzimuth, startElevation)

// placement is one station's world transform and the plinth beneath it, built
// once at package init. They are held as matrices rather than rebuilt per frame
// because a ModelDraw's Transform is read at record time and the plinth's is
// pointed at.
type placement struct {
	model  scene.Transform
	plinth m.Mat4
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
		// scale and rotation the draw is given, so a file authored from a
		// corner stands where a file authored from its centre does.
		middle := m.TRS4(m.Vec3{}, yaw, m.Vec3{X: s.scale, Y: s.scale, Z: s.scale}).
			TransformPoint(m.Vec3{X: s.centerX, Z: s.centerZ})
		out[i] = placement{
			model: scene.Transform{
				Position: m.Vec3{X: s.x - middle.X, Y: s.lift, Z: s.z - middle.Z},
				Rotation: yaw,
				Scale:    s.scale,
			},
			plinth: m.TRS4(
				m.Vec3{X: s.x, Y: plinthHeight / 2, Z: s.z},
				yaw,
				m.Vec3{X: plinthWidth, Y: plinthHeight, Z: plinthDepth},
			),
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
// PointLightIntensityTest. They are recorded in the order declared here, and
// that order is load-bearing: past the cap a light replaces the weakest kept one
// only if it beats it, so among lights that all score the same - which is what
// every light whose range window is closed at the eye scores - the first sixteen
// offered are the sixteen kept.
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

// RecordedLights is how many punctual lights the frame records once every model
// is resident: one fill, two spots, the eight PointLightIntensityTest declares,
// five rim lamps and five deep ones. It is five more than the cap on purpose.
const RecordedLights = 1 + 2 + ModelDeclaredLights + rimCount + deepCount

// ModelDeclaredLights is how many KHR_lights_punctual lights the lights station
// contributes. They are the file's own, re-declared by this demo at the
// station's world transform.
const ModelDeclaredLights = 8

// MaxLights is scene's per-pass cap. It is a fixed constant there rather than a
// Config knob, and it is spelled out here so the HUD and the assertions read the
// same number.
const MaxLights = 16

// RecordedDraws is how many draws the frame flushes to once every model is
// resident: one per primitive of each model, the alpha model twice, plus the six
// plinths and the ground.
const RecordedDraws = bottlePrimitives + comparePrimitives + 2*alphaPrimitives +
	vertexPrimitives + emissivePrimitives + lightsPrimitives + len(stations) + 1

// The primitive counts of the six files, which are what a model draw expands
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
	lightsPrimitives = 6*2 + 1
)

// BlendPrimitives is how many of the alpha model's primitives are alphaMode
// BLEND: TestBlendMesh and DecalBlendMesh, both MatBlend. Everything else in the
// frame is OPAQUE or MASK, and MASK sorts with opaque.
const BlendPrimitives = 2

// draw records the whole frame: the camera, the still life, the lights and the
// HUD.
//
// It holds the Lookup and the filesystem because a ModelLights read needs a
// LookupAccess, which is the facade a demo builds from them. The read is a map
// hit and a copy into a scratch slice; nothing here parses or uploads.
func (p *Pbr) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
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
			p.rate.measure(time.Now())
			p.readStats(q)
			p.advance(inputState.Get())
			p.record(q, scene.NewLookupAccess(k, lookup.Get()))
			p.hud(canvasQueue.Get())
			return nil
		}
}

// advance steps the demo's own clock and applies the input. Input is read before
// the step so a held arrow moves the camera on the very frame it is pressed.
func (p *Pbr) advance(state *input.State) {
	if state != nil {
		if state.JustPressed(input.KeySpace) {
			p.paused = !p.paused
		}
		step := orbitSpeed * fixedStep
		if state.Pressed(input.KeyLeft) {
			p.azimuth -= step
		}
		if state.Pressed(input.KeyRight) {
			p.azimuth += step
		}
		if state.Pressed(input.KeyUp) {
			p.elevation = m.Clamp(p.elevation+step, -0.2, 1.4)
		}
		if state.Pressed(input.KeyDown) {
			p.elevation = m.Clamp(p.elevation-step, -0.2, 1.4)
		}
		for i, key := range focusKeys {
			if state.JustPressed(key) {
				p.setFocus(i + 1)
			}
		}
		if state.JustPressed(input.Key0) {
			p.setFocus(0)
		}
		if state.JustPressed(input.KeyR) {
			p.setFocus(0)
			p.step = 0
		}
	}
	if !p.paused {
		p.step++
	}
}

// focusKeys are the number keys that fly the camera to a station, in station
// order.
var focusKeys = [len(stations)]input.Key{
	input.Key1, input.Key2, input.Key3, input.Key4, input.Key5, input.Key6,
}

// setFocus points the camera at a station, or back at the overview, returning
// the orbit to the documented azimuth and elevation so a focus key always lands
// on the same picture.
func (p *Pbr) setFocus(focus int) {
	p.focus = focus
	p.azimuth, p.elevation = startAzimuth, startElevation
}

// target and radius are the orbit the camera is on, which is the overview's or
// the focused station's.
func (p *Pbr) target() m.Vec3 {
	if p.focus == 0 {
		return orbitTarget
	}
	return placements[p.focus-1].center
}

func (p *Pbr) radius() float32 {
	if p.focus == 0 {
		return overviewRadius
	}
	return stations[p.focus-1].radius
}

// time is the demo's clock: accumulated fixed steps. Nothing in the recorded
// frame reads it - the still life is deliberately still - so it is the HUD's
// number and the orbit's rate, and every frame at a given pose is the same
// frame.
func (p *Pbr) time() float32 { return float32(p.step) * fixedStep }

// eye is the camera's position on its orbit.
func (p *Pbr) eye() m.Vec3 {
	return eyeAt(p.target(), p.radius(), p.azimuth, p.elevation)
}

// record records the frame: one camera, the ground, six plinths, seven model
// draws and twenty-one lights.
func (p *Pbr) record(q *scene.OpQueue, la scene.LookupAccess) {
	q.Camera(CameraMain, scene.CameraDescr{
		Transform: scene.LookAt(p.eye(), p.target(), m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      nearPlane,
		Far:       farPlane,
		// Everything else is left at its zero value, and every zero is the
		// default: Projection is Perspective, CullMask is LayersAll,
		// SunIntensity and AmbientIntensity are 1, and Passes is empty, which
		// emits one implicit forward pass at the camera's own id.
		//
		// SunColor carries sunStrength, and the ambient is cool: a bright sun would
		// wash out twenty-one punctual lights and the demo would be a test of
		// one directional light.
		SunDirection:  m.Vec3{X: -0.35, Y: -1, Z: -0.55},
		SunColor:      sunColor.MulS(sunStrength),
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	})

	q.Plane(0, m.Vec3{}, m.Vec2{X: groundSide, Y: groundSide}, groundColor)
	for i := range placements {
		q.Box(0, scene.Transform{Matrix: &placements[i].plinth}, plinthColor)
		q.Model(0, stations[i].path, scene.ModelDraw{Transform: placements[i].model})
	}

	// The alpha station's second copy, at its own depth. Two copies is what
	// makes the blend bucket's back-to-front order observable: one copy's two
	// blended primitives cannot separate a depth sort from a mesh-id sort.
	second := placements[stationAlpha].model
	second.Position = second.Position.Add(alphaSecondCopy)
	q.Model(0, stations[stationAlpha].path, scene.ModelDraw{Transform: second})

	p.recordLights(q, la)
	p.readResidency(la)
}

// recordLights declares every punctual light in the frame, and counts them.
//
// Order matters here and is the reason this reads as a script rather than a
// loop over a table: the cap keeps the sixteen with the highest contribution at
// the eye, and every light whose range window is closed there scores exactly
// zero, so among a tie the first sixteen offered win. The fill, the two spots,
// the station's own eight and the five rim lamps are offered first and are the
// sixteen the reference pose keeps; the five deep lamps are offered last and are
// the five it drops.
func (p *Pbr) recordLights(q *scene.OpQueue, la scene.LookupAccess) {
	declared := 0

	// The fill light. Range is left at zero, which means infinite: it is not
	// culled by any frustum and its falloff window is open everywhere, so it is
	// the one light in the frame with a real score at the reference pose.
	q.PointLight(0, scene.LightDescr{
		Position:  m.Vec3{X: 0, Y: fillHeight, Z: fillDepth},
		Color:     fillColor,
		Intensity: fillIntensity,
	})
	declared++

	q.SpotLight(0, scene.LightDescr{
		Position:  placements[stationBottle].center.Add(m.Vec3{Y: spotHeight}),
		Direction: m.Vec3{Y: -1},
		Color:     bottleSpot,
		Intensity: spotIntensity,
		Range:     spotRange,
		InnerCone: spotInner,
		OuterCone: spotOuter,
	})
	declared++

	q.SpotLight(0, scene.LightDescr{
		Position:  placements[stationAlpha].center.Add(m.Vec3{Y: spotHeight, Z: -1}),
		Direction: m.Vec3{Y: -1, Z: 0.25},
		Color:     alphaSpot,
		Intensity: spotIntensity,
		Range:     spotRange,
		InnerCone: spotInner,
		OuterCone: spotOuter,
	})
	declared++

	declared += p.recordModelLights(q, la)

	for i := range rimColors {
		q.PointLight(0, scene.LightDescr{
			Position:  m.Vec3{X: spread(i, rimCount, rimSpacing), Y: rimY, Z: rimZ},
			Color:     rimColors[i],
			Intensity: rimIntensity,
			Range:     rimRange,
		})
		declared++
	}

	for i := range deepColors {
		q.PointLight(0, scene.LightDescr{
			Position:  m.Vec3{X: spread(i, deepCount, deepSpacing), Y: deepY, Z: deepZ},
			Color:     deepColors[i],
			Intensity: deepIntensity,
			Range:     deepRange,
		})
		declared++
	}

	p.declared = declared
}

// spread is the x of the i-th of n lamps in a row centred on the origin.
func spread(i, n int, spacing float32) float32 {
	return (float32(i) - float32(n-1)/2) * spacing
}

// recordModelLights declares the lights station's own KHR_lights_punctual
// lights, and reports how many it declared.
//
// They come out of the file as data, in the file's own space, and nothing in
// scene converts one: a lamp prop placed forty times would blow the cap
// silently, and which of a file's lights matter is the app's judgement. So this
// carries each one through the station's world matrix - the position through
// the matrix, the direction through its basis, and the Range through the scale,
// because a range authored in model units is a different distance once the model
// is drawn three quarters the size.
//
// A file's directional light is skipped: scene has no recording call for one,
// because the single directional light it shades with is the camera's own sun.
// This file declares none, and the branch is here because a demo reading a
// file's lights as data has to decide what to do with the kind it cannot
// declare.
func (p *Pbr) recordModelLights(q *scene.OpQueue, la scene.LookupAccess) int {
	station := &placements[stationLights]
	lights, ok := la.ModelLights(stations[stationLights].path, p.modelLights[:0])
	if !ok {
		return 0
	}
	p.modelLights = lights
	world := station.model.Mat4()
	declared := 0
	for i := range lights {
		if lights[i].Directional {
			continue
		}
		descr := lights[i].Descr
		descr.Position = world.TransformPoint(descr.Position)
		descr.Direction = world.TransformDirection(descr.Direction)
		descr.Range *= station.scale
		if descr.Kind == scene.LightSpot {
			q.SpotLight(0, descr)
		} else {
			q.PointLight(0, descr)
		}
		declared++
	}
	return declared
}

// readResidency asks each station whether its model is drawable yet, for the
// HUD alone. ModelLights' ok is the residency predicate the API has before the
// lookup facade lands: it is false for a missing, loading and failed path alike.
func (p *Pbr) readResidency(la scene.LookupAccess) {
	for i := range stations {
		_, ok := la.ModelLights(stations[i].path, nil)
		p.resident[i] = ok
	}
}

// ResidentCount is how many stations reported residency on the last frame.
func (p *Pbr) ResidentCount() int {
	n := 0
	for _, ok := range p.resident {
		if ok {
			n++
		}
	}
	return n
}

// readStats reads the previous frame's flush result back out of the queue.
// Passes publishes the frame the last flush consumed, which is what a HUD can
// print without stalling the pipeline to ask about the frame it is recording.
func (p *Pbr) readStats(q *scene.OpQueue) {
	views := q.Passes(nil)
	p.stats = stats{passes: len(views), ops: len(q.Ops(nil))}
	for i := range views {
		p.stats.recorded += views[i].Recorded
		p.stats.culled += views[i].Culled
		p.stats.instances += views[i].Instances
		p.stats.lights += views[i].Lights
		p.stats.batches += len(views[i].Batches)
	}
}
