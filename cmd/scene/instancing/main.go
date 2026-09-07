// Command instancing is the scene plugin's instanced-draw demo: one call that
// places a field of crates, per-instance culling, and the sort key made
// visible.
//
//	go run ./cmd/scene/instancing
//
// It adds no assets. The crates, the bottles and the two glass screens are
// three of pbr's six files, and the ground is box's debug vocabulary, because
// what this demo is about is not what a model looks like but how many draw
// calls a thousand of them cost.
//
// It is the demo where Passes(dst) earns its retained-by-default design. Most
// of what it proves is a number: how many instances a call recorded, how many
// the frustum kept, how many batches they packed into, and in what order those
// batches came out. The HUD reads all of it out of the published PassView every
// frame, into a slice it keeps, so a human running the demo sees the numbers
// without a second command and without the frame allocating for them.
//
// What it exercises: explicit Transforms on ModelDraw; per-instance culling and
// the contiguous packing of the survivors; the materialID/meshID sort key;
// SCENE_NONUNIFORM through the Matrix escape hatch, since Transform.Scale is
// scalar; firstInstance, the per-batch material record and the one instance
// arena every batch binds a range of; the split a blend-class instanced call
// takes; and Passes(dst) itself.
//
// # The courtyard
//
// A 25 by 25 lattice of crates on 1.8-unit centres, with a 7 by 7 square left
// out of the middle. That is 576 crates from one Model call - the tile floor
// the API doc names, at the size where the difference between one draw call and
// five hundred is the whole point.
//
// A colonnade stands on the sub-lattice where both indices are 2 mod 6. Those
// crates are stretched into pillars through Transform.Matrix, which is the
// deliberate route for non-uniform scale: Transform.Scale is a single float, so
// a slab or a column has to replace the transform whole. They are entries in
// the same Transforms slice as the cubes around them, so one call carries both
// - which is what makes SCENE_NONUNIFORM per instance rather than per draw.
// The courtyard takes the one pillar site that falls inside it; the lattice is
// the rule and the courtyard is the hole, and the hole wins.
//
// Five water bottles stand in the courtyard, every other one squashed into a
// wide low one through the same escape hatch, and two glass screens stand in
// front of them at two different depths. The screens are the exception to
// batching: their two BLEND primitives split back into one single-instance
// batch each, because sorting an instanced set by its nearest instance would
// composite visibly wrong, while their seven opaque primitives stay seven
// batches of two.
//
// The squat bottles rather than the pillars are what makes SCENE_NONUNIFORM
// visible, and the reason is worth stating because it is easy to get backwards.
// Every face normal of an axis-aligned box is an eigenvector of an axis-aligned
// scale, so the world matrix and its inverse-transpose send it the same way and
// differ only in length - which normalising removes. A stretched cube therefore
// shades identically with the flag and without it, however non-uniform it is. A
// bottle's shoulder is curved, so its normals are not eigenvectors of anything,
// and there the flag decides where the highlight lands.
//
// A stack of three crates stands in one corner of the courtyard, and it is a
// second call of the same model. It stays a batch of its own beside the
// field's, because the batch is the call rather than the key - collapsing two
// calls that share a mesh and a material is the deferred automatic collapse,
// and doing it here would make that ticket unfalsifiable. It is also the one
// draw in the frame the sort has to move: recorded after the bottles and the
// screens, it carries a material interned before either of theirs, so a
// material-keyed sort lifts it back beside the field.
//
// # One call or five hundred
//
// Key 1 draws the field as one instanced call and key 2 draws it as one call
// per crate. The picture does not change and neither does the packed instance
// array: the crates share a mesh and a material, so they share a sort key, the
// sort's final tiebreak is the recording ordinal, and the two orders coincide.
// What changes is the batch count on the HUD - from one to as many crates as
// survived the frustum.
//
// That is the deferred automatic collapse of consecutive equal draws, seen from
// the other side. When it lands it has to be output-identical to the instanced
// form, and this key is the pair of frames that says what identical means.
//
// # The reference pose
//
// The demo starts at a documented fixed pose - the camera orbits orbitTarget at
// radius overviewRadius, azimuth startAzimuth and elevation startElevation -
// and reference.png beside this file is the frame at that pose.
//
// Nothing in the recorded frame moves on its own. The clock is accumulated
// fixed steps and the HUD prints it, but it drives only the orbit rate, so
// every frame at the reference pose is the same frame and the reference
// screenshot is retaken by launching the demo and capturing it, with no step
// number to hit. A demo whose acceptance is a picture is better off recording
// nothing that moves, and per-instance culling is a still subject: what makes
// it visible is the camera turning, which is input.
//
// Input may orbit, pause and switch modes freely, and touching it voids
// nothing: the assertions live in instancing_test.go rather than in the running
// app.
//
// # What only eyes can judge
//
// The field reads as a regular grid running out to the edge of the frame with
// even gaps all the way, and the two squashed bottles in the row keep the same
// bright roll of metal highlight round their shoulders as the tall bottles
// standing beside them.
//
// One sentence, two failures. A batch that does not read its own slice of the
// instance arena - a firstInstance that is not pass-relative, a bound range
// that starts in the wrong place - piles the field onto one square or shears
// the lattice into a fan, and a grid is the one arrangement where that is
// unmissable. And a squat bottle whose SCENE_NONUNIFORM never reached the
// shader takes its normals through the world matrix instead of the
// inverse-transpose: its highlight goes out entirely and it drops to a dull
// olive disc beside a neighbour that did not change, which is a difference
// between two things in one frame rather than a judgement about one.
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

// The logical screen the HUD is laid out in, and the logical size the window
// opens at - the size the reference screenshot is of. The window is launched at
// it rather than resized into it: a runtime resize leaves the viewport
// un-refitted.
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
			WithTitle("cog examples: scene instancing").
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
const Name kernel.PluginName = "instancing"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock: the update event's Dt is deliberately ignored.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose, where the reference screenshot is taken.
//
// The radius is what decides how much of the field the frustum keeps, which is
// the number this demo is about: at this pose a little over half the lattice is
// inside the frustum and the rest is behind the camera or off to the sides.
const (
	overviewRadius = 15.0
	startAzimuth   = 0.0
	startElevation = 0.34
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
	nearPlane      = 0.1
	// The far plane is past the far corner of the lattice on purpose. A far
	// plane that cut the field would cull more, and visibly, but the cut is a
	// straight edge across the ground that reads as a bug in the picture.
	farPlane = 200
)

// orbitTarget is the point the camera looks at and orbits, a little above the
// courtyard floor so the ground fills the lower part of the frame.
var orbitTarget = m.Vec3{Y: 1.2}

// The three files, all of them pbr's.
const (
	cratePath  = "assets/BoxVertexColors/BoxVertexColors.glb"
	bottlePath = "assets/WaterBottle/WaterBottle.glb"
	panePath   = "assets/AlphaBlendModeTest/AlphaBlendModeTest.glb"
)

// modelPaths is what the residency counter watches, in the order the HUD names
// them.
var modelPaths = [...]string{cratePath, bottlePath, panePath}

// The files' own measurements, in model units, taken from the same POSITION
// accessors pbr's station table reads. They are constants here for the same
// reason they are there: a placement computed from LookupAccess.Bounds would
// have to wait for residency and rebuild the whole lattice when it arrived,
// where the lattice is this demo's fixed subject. A file swapped underneath the
// demo moves its models off the floor rather than failing anything.
const (
	// BoxVertexColors is a unit cube authored from a corner, so it spans 0..1
	// on every axis and a crate standing at (x, z) has its transform at
	// (x - width/2, 0, z - depth/2).
	crateMinY = 0
	// WaterBottle is 0.109 x 0.260 x 0.109 about its own middle, minY -0.130.
	bottleMinY = -0.130
	// AlphaBlendModeTest is 8.600 x 2.400 x 1.300 about its own middle,
	// minY -0.100.
	paneMinY = -0.100
)

// The crate lattice. The side is odd so the courtyard has a centre index, and
// the courtyard is the square of lattice sites left out of the middle for the
// bottles and the screens to stand in.
const (
	gridSide      = 25
	gridSpacing   = 1.8
	courtyardHalf = 3 // in lattice steps, so the hole is 7 by 7
	crateSize     = 0.8
)

// The colonnade: the sub-lattice both of whose indices are pillarOffset mod
// pillarStride carries a pillar instead of a crate. A pillar is the same cube
// under a non-uniform scale, which Transform.Scale cannot express, so it goes
// through Transform.Matrix.
const (
	pillarStride = 6
	pillarOffset = 2
	pillarWidth  = 0.5
	pillarHeight = 3.4
)

// The five bottles, in a row across the courtyard.
const (
	bottleCount   = 5
	bottleScale   = 10.0
	bottleSpacing = 2.6
	bottleZ       = -2.4

	// Every other bottle is squashed through Transform.Matrix into a wide, low
	// one, and yawed while it is at it. That is the frame's one non-uniform
	// basis on a curved surface, and it is here because a box cannot show what
	// SCENE_NONUNIFORM is for: every face normal of an axis-aligned box is an
	// eigenvector of an axis-aligned scale, so the world matrix and its
	// inverse-transpose point it the same way and only its length differs. A
	// bottle's shoulder is curved, so its normals are not, and the flag decides
	// where the highlight lands.
	squatWiden   = 2.1
	squatFlatten = 0.55
	squatYaw     = 0.7
)

// The stack: a second instanced call of the same crate model, standing in a
// corner of the courtyard where the reference pose keeps all of it. It is
// recorded after the other two models and drawn at its own scale, so it is
// unmistakable in the picture and out of key order in the recording.
const (
	stackCount = 3
	stackScale = 1.2
)

// stackCorner is where the stack stands, on the courtyard floor.
var stackCorner = m.Vec2{X: 5.4, Y: -5.4}

// The two glass screens, at two depths so the four blended primitives they
// contribute have four distinct distances from the eye. One depth could not
// tell a back-to-front sort from a mesh-id sort.
const paneScale = 0.7

// paneStands is where each screen stands, in world units on the courtyard
// floor. They overlap across x as seen from the reference eye, so the near one
// tints the far one rather than standing beside it.
var paneStands = [...]m.Vec2{{X: -2.2, Y: 2.0}, {X: 2.2, Y: 4.4}}

// The ground the whole courtyard stands on. It is wider than the lattice so the
// field ends on ground rather than on the edge of the world.
const groundSide = 96

// The frame's own colours, written in sRGB and converted on the way in: Color
// holds linear components, and a demo that typed linear literals would be
// picking its palette in a space no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.09, 1)
	groundColor   = m.NewColorSrgb(0.30, 0.31, 0.34, 1)
	sunColor      = m.NewColorSrgb(1, 0.97, 0.92, 1)
	ambientSky    = m.NewColorSrgb(0.19, 0.23, 0.31, 1)
	ambientGround = m.NewColorSrgb(0.11, 0.10, 0.09, 1)
	lampColor     = m.NewColorSrgb(1.00, 0.84, 0.62, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
)

// The one punctual light: a warm lamp over the courtyard, so the bottles and
// the screens read against a field lit only by the sun. Lighting is pbr's
// subject and this demo declares the least of it that keeps the picture legible.
const (
	lampHeight       = 4.5
	lampIntensity    = 26
	lampRange        = 16
	lampMarkerRadius = 0.25
)

// lampPosition is where the lamp hangs, and where its marker sphere stands.
var lampPosition = m.Vec3{Y: lampHeight, Z: bottleZ}

// Instancing is the demo's gameplay plugin: it records the whole frame and owns
// the step counter, the orbit, the draw mode and the numbers the HUD prints.
type Instancing struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
	// perCall draws the field as one Model call per crate instead of one
	// instanced call. The picture is the same and the batch count is not.
	perCall bool
	rate    rate
	// views is the scratch Passes(dst) appends into, kept across frames so
	// reading the published frame allocates nothing. It is the whole reason
	// Passes takes a destination.
	views []scene.PassView
	stats stats
	// resident is which of the three files reported residency on the last
	// frame, for the HUD.
	resident [len(modelPaths)]bool
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// cannot come from the update event's Dt, which is the driver's fixed timestep,
// nor from the step counter, which is the same number however long a frame
// took. It counts frames over a window rather than averaging 1/interval per
// frame, so a startup spike is one frame in the count instead of a reading that
// never happened decaying for a hundred frames afterwards.
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
	// biggest is the instance count of the largest batch, which is the crate
	// field's when the field is one instanced call. It is the number that says
	// the batching happened at all.
	biggest int
}

// New builds the demo plugin at its documented starting pose.
func New() *Instancing {
	return &Instancing{azimuth: startAzimuth, elevation: startElevation}
}

func (p *Instancing) Name() kernel.PluginName { return Name }

func (p *Instancing) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *Instancing) Register(registrar *kernel.Registrar, _ any) error {
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

// The three transform lists, built once at package init. They are package-level
// because they never change: the field is the demo's fixed subject, and a
// Transform carrying a Matrix points at a matrix that has to outlive the flush
// that reads it.
var (
	crateTransforms, pillarMatrices = buildField()
	bottleTransforms, squatMatrices = buildBottles()
	paneTransforms                  = buildPanes()
	stackTransforms                 = buildStack()
)

// buildField lays out the crate lattice, leaving the courtyard out of the
// middle and stretching the colonnade's sites into pillars.
//
// The pillar matrices are returned alongside because a Transform.Matrix is a
// pointer: the slice they live in is what keeps them addressable, and the count
// of it is what the HUD and the assertions call the colonnade rather than a
// number anyone worked out by hand.
func buildField() ([]scene.Transform, []m.Mat4) {
	center := gridSide / 2
	pillars := make([]m.Mat4, 0, gridSide*gridSide/(pillarStride*pillarStride)+1)
	// The pillar matrices are sized and written before any transform points at
	// one, because appending to the slice while a Transform already held a
	// pointer into it would move the matrix out from under that pointer.
	for i := range gridSide {
		for j := range gridSide {
			if inCourtyard(i, j, center) || !isPillar(i, j) {
				continue
			}
			pillars = append(pillars, m.TRS4(
				m.Vec3{X: latticeAt(i, center) - pillarWidth/2, Z: latticeAt(j, center) - pillarWidth/2},
				m.Quat{W: 1},
				m.Vec3{X: pillarWidth, Y: pillarHeight, Z: pillarWidth},
			))
		}
	}
	transforms := make([]scene.Transform, 0, gridSide*gridSide)
	at := 0
	for i := range gridSide {
		for j := range gridSide {
			if inCourtyard(i, j, center) {
				continue
			}
			if isPillar(i, j) {
				transforms = append(transforms, scene.Transform{Matrix: &pillars[at]})
				at++
				continue
			}
			transforms = append(transforms, scene.Transform{
				Position: m.Vec3{
					X: latticeAt(i, center) - crateSize/2,
					Y: -crateMinY * crateSize,
					Z: latticeAt(j, center) - crateSize/2,
				},
				Scale: crateSize,
			})
		}
	}
	return transforms, pillars
}

// latticeAt is the world coordinate of lattice index i.
func latticeAt(i, center int) float32 { return float32(i-center) * gridSpacing }

// inCourtyard reports whether a lattice site falls in the square left out of
// the middle.
func inCourtyard(i, j, center int) bool {
	return abs(i-center) <= courtyardHalf && abs(j-center) <= courtyardHalf
}

// isPillar reports whether a lattice site carries a pillar rather than a crate.
// A site inside the courtyard is neither: the lattice is the rule and the
// courtyard is the hole, so the colonnade loses the one site that falls in it.
func isPillar(i, j int) bool {
	return i%pillarStride == pillarOffset && j%pillarStride == pillarOffset
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// buildBottles stands the bottles in a row across the courtyard, each lifted by
// its own minY so it rests on the floor rather than sinking into it.
func buildBottles() ([]scene.Transform, []m.Mat4) {
	squats := make([]m.Mat4, 0, bottleCount)
	for i := range bottleCount {
		if !isSquat(i) {
			continue
		}
		squats = append(squats, m.TRS4(
			m.Vec3{X: spread(i, bottleCount, bottleSpacing), Z: bottleZ},
			m.QuatAxisAngle(m.Vec3{Y: 1}, squatYaw),
			m.Vec3{X: bottleScale * squatWiden, Y: bottleScale * squatFlatten, Z: bottleScale * squatWiden},
		))
	}
	out := make([]scene.Transform, bottleCount)
	at := 0
	for i := range out {
		if isSquat(i) {
			out[i] = scene.Transform{Matrix: &squats[at]}
			at++
			continue
		}
		out[i] = scene.Transform{
			Position: m.Vec3{
				X: spread(i, bottleCount, bottleSpacing),
				Y: -bottleMinY * bottleScale,
				Z: bottleZ,
			},
			Scale: bottleScale,
		}
	}
	return out, squats
}

// isSquat reports whether the i-th bottle in the row is one of the squashed
// ones. Every other bottle is, so a squat one always stands beside a tall one
// and the pair can be compared without moving the eye.
func isSquat(i int) bool { return i%2 == 1 }

// buildPanes stands the two screens at their own depths.
func buildPanes() []scene.Transform {
	out := make([]scene.Transform, len(paneStands))
	for i, stand := range paneStands {
		out[i] = scene.Transform{
			Position: m.Vec3{X: stand.X, Y: -paneMinY * paneScale, Z: stand.Y},
			Scale:    paneScale,
		}
	}
	return out
}

// buildStack piles the stack's crates on one another in the courtyard's corner.
func buildStack() []scene.Transform {
	out := make([]scene.Transform, stackCount)
	for i := range out {
		out[i] = scene.Transform{
			Position: m.Vec3{
				X: stackCorner.X - stackScale/2,
				Y: float32(i) * stackScale,
				Z: stackCorner.Y - stackScale/2,
			},
			Scale: stackScale,
		}
	}
	return out
}

// spread is the coordinate of the i-th of n things in a row centred on zero.
func spread(i, n int, spacing float32) float32 {
	return (float32(i) - float32(n-1)/2) * spacing
}

// draw records the whole frame: the camera, the field, the courtyard, one lamp
// and the HUD.
//
// It holds the Lookup because the HUD's residency line needs a LookupAccess,
// which is the facade a demo builds from the kernel and the resource. The read
// is a map hit; nothing here parses or uploads.
func (p *Instancing) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
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
			p.record(q)
			// OpCount reads the recording in progress rather than the
			// published frame, so it is asked after the frame is recorded and
			// not beside the pass numbers, which are the previous frame's. It
			// is asked at all because Ops(nil) would copy six hundred ops a
			// frame to count them.
			p.stats.ops = q.OpCount()
			p.readResidency(scene.NewLookupAccess(k, lookup.Get()))
			p.hud(canvasQueue.Get())
			return nil
		}
}

// advance steps the demo's own clock and applies the input. Input is read
// before the step so a held arrow moves the camera on the very frame it is
// pressed.
func (p *Instancing) advance(state *input.State) {
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
			p.elevation = m.Clamp(p.elevation+step, 0.05, 1.4)
		}
		if state.Pressed(input.KeyDown) {
			p.elevation = m.Clamp(p.elevation-step, 0.05, 1.4)
		}
		if state.JustPressed(input.Key1) {
			p.perCall = false
		}
		if state.JustPressed(input.Key2) {
			p.perCall = true
		}
		if state.JustPressed(input.KeyR) {
			p.azimuth, p.elevation, p.step, p.perCall = startAzimuth, startElevation, 0, false
		}
	}
	if !p.paused {
		p.step++
	}
}

// time is the demo's clock: accumulated fixed steps. Nothing in the recorded
// frame reads it - the courtyard is deliberately still - so it is the HUD's
// number and the orbit's rate, and every frame at a given pose is the same
// frame.
func (p *Instancing) time() float32 { return float32(p.step) * fixedStep }

// eye is the camera's position on its orbit.
func (p *Instancing) eye() m.Vec3 {
	cosElevation := float32(math.Cos(float64(p.elevation)))
	return orbitTarget.Add(m.Vec3{
		X: overviewRadius * cosElevation * float32(math.Sin(float64(p.azimuth))),
		Y: overviewRadius * float32(math.Sin(float64(p.elevation))),
		Z: overviewRadius * cosElevation * float32(math.Cos(float64(p.azimuth))),
	})
}

// record records the frame: one camera, the ground, the field, the courtyard
// and one lamp.
func (p *Instancing) record(q *scene.OpQueue) {
	q.Camera(CameraMain, scene.CameraDescr{
		Transform: scene.LookAt(p.eye(), orbitTarget, m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      nearPlane,
		Far:       farPlane,
		// Everything else is left at its zero value, and every zero is the
		// default: Projection is Perspective, CullMask is LayersAll,
		// SunIntensity and AmbientIntensity are 1, and Passes is empty, which
		// emits one implicit forward pass at the camera's own id.
		SunDirection:  m.Vec3{X: -0.4, Y: -1, Z: -0.45},
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	})

	q.Plane(0, m.Vec3{}, m.Vec2{X: groundSide, Y: groundSide}, groundColor)

	// The field, either way round. One call with a Transforms slice and N calls
	// with a Transform each pack the same instances in the same order; what
	// differs is that the first is one batch and the second is one batch per
	// surviving crate.
	if p.perCall {
		for i := range crateTransforms {
			q.Model(0, cratePath, scene.ModelDraw{Transform: crateTransforms[i]})
		}
	} else {
		q.Model(0, cratePath, scene.ModelDraw{Transforms: crateTransforms})
	}

	q.Model(0, bottlePath, scene.ModelDraw{Transforms: bottleTransforms})
	q.Model(0, panePath, scene.ModelDraw{Transforms: paneTransforms})

	// A second call of the crate model, recorded last, and the one draw in this
	// frame the sort has to move. It carries the crates' own material, which
	// was interned before the bottles' and the screens', so a material-keyed
	// sort lifts it back beside the field over two models recorded ahead of it
	// - and everything else here is already in key order as it is recorded.
	//
	// It stays a batch of its own beside the field's, which is the other half
	// of what it is here for: two separate calls of one mesh and one material
	// are two batches, because a batch is the call. Collapsing them is the
	// deferred automatic collapse, and doing it early would make that ticket
	// unfalsifiable.
	q.Model(0, cratePath, scene.ModelDraw{Transforms: stackTransforms})

	// The lamp, and a marker on it so the light has somewhere visible to come
	// from. The marker is a debug shape, so it shares the bundled material with
	// the ground plane; both are recorded before the models but reach the flush
	// ahead of them anyway, since a model call expands into the draw list at
	// flush time rather than at record time.
	q.PointLight(0, scene.LightDescr{
		Position:  lampPosition,
		Color:     lampColor,
		Intensity: lampIntensity,
		Range:     lampRange,
	})
	q.Sphere(0, lampPosition, lampMarkerRadius, lampColor)
}

// The primitive counts of the three files, which are what a model draw expands
// to per instance. They are spelled out rather than counted at runtime so that
// an asset swapped underneath the demo fails an assertion instead of quietly
// changing the numbers.
const (
	cratePrimitives  = 1
	bottlePrimitives = 1
	panePrimitives   = 9
	// PaneBlendPrimitives is how many of the screen's primitives are alphaMode
	// BLEND: TestBlendMesh and DecalBlendMesh. They are the ones that split.
	PaneBlendPrimitives = 2
)

// CrateCount is how many crates the lattice holds once the courtyard is taken
// out of it, and PillarCount how many of those are stretched into pillars.
// Both are counted from the layout rather than written down, so the rule and
// the number cannot drift apart.
var (
	CrateCount  = len(crateTransforms)
	PillarCount = len(pillarMatrices)
)

// debugShapes is how many draws the frame takes from box's vocabulary: the
// ground plane and the marker on the lamp. They share one material, the bundled
// PBR that every debug shape takes.
const debugShapes = 2

// RecordedDraws is how many draws the frame flushes to once every file is
// resident: one per primitive per instance, plus the debug shapes.
var RecordedDraws = (CrateCount+stackCount)*cratePrimitives +
	bottleCount*bottlePrimitives + len(paneTransforms)*panePrimitives + debugShapes

// InstancedBatches is how many batches the frame packs when the field is one
// call: the two debug shapes, the field, the stack, the bottles, the screens'
// opaque primitives, and one per blended primitive per screen. Culling does not
// change it - a batch is the call, not its survivors - until a whole call is
// culled away, which the reference pose does not do.
const InstancedBatches = debugShapes + 1 + 1 + 1 +
	(panePrimitives - PaneBlendPrimitives) + len(paneStands)*PaneBlendPrimitives

// readResidency asks each file whether it is drawable yet, for the HUD. State
// is the residency predicate rather than an ok from some other query, because
// it is the only one that tells "still loading" from "never coming".
func (p *Instancing) readResidency(la scene.LookupAccess) {
	for i, path := range modelPaths {
		p.resident[i] = la.State(path) == scene.ModelResident
	}
}

// ResidentCount is how many files reported residency on the last frame.
func (p *Instancing) ResidentCount() int {
	n := 0
	for _, ok := range p.resident {
		if ok {
			n++
		}
	}
	return n
}

// readStats reads the previous frame's flush result back out of the queue,
// through Passes(dst) into the slice the demo keeps.
//
// This is the call the demo is built around. The lists it returns are built
// during the flush regardless and retaining them costs a slice header, so there
// is no knob gating them and no second command to run: the batch table on
// screen is this slice, printed.
func (p *Instancing) readStats(q *scene.OpQueue) {
	p.views = q.Passes(p.views[:0])
	p.stats = stats{passes: len(p.views)}
	for i := range p.views {
		view := &p.views[i]
		p.stats.recorded += view.Recorded
		p.stats.culled += view.Culled
		p.stats.instances += view.Instances
		p.stats.batches += len(view.Batches)
		for _, batch := range view.Batches {
			p.stats.biggest = max(p.stats.biggest, batch.InstanceCount)
		}
	}
}

// Batches is the published frame's batches, in emission order, for the HUD and
// for an assertion that wants the list rather than the counts. It aliases the
// queue's flush storage and stays valid until the next flush.
func (p *Instancing) Batches() []scene.BatchView {
	if len(p.views) == 0 {
		return nil
	}
	return p.views[0].Batches
}
