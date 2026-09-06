// Command box is the scene plugin's zero-asset demo: the whole debug
// vocabulary, one camera, and a HUD, with no file on disk anywhere in the
// frame. Even the HUD's text costs nothing: canvas embeds a font and draws with
// it when a text op names none. It is the floor of the API and the smoke test
// that still works when the asset story breaks, which is why it is kept beside
// pbr despite covering nothing pbr misses.
//
//	go run ./cmd/scene/box
//
// What it exercises: the debug vocabulary (Box, Sphere, Plane, Line3D,
// WireBox); Transform TRS and the scalar Scale; LookAt; an empty Passes
// yielding the implicit forward pass at the camera id; sun and hemispheric
// ambient; a point light and a spot light; the linear pipeline and the present
// pass; every-zero-value-is-the-default; and the m additions.
//
// # The reference pose
//
// The demo starts at a documented fixed pose: the camera orbits orbitTarget at
// radius orbitRadius, azimuth startAzimuth and elevation startElevation, and
// stays there until an arrow key is held. reference.png beside this file is the
// frame at that pose, captured paused at step 238 so the same picture can be
// retaken; R returns to the pose exactly, resetting the step counter with it,
// so a run that has been orbited around can be brought back to the picture
// rather than restarted.
//
// Input may orbit and pause freely, and touching it voids nothing: the
// assertions live in box_test.go rather than in the running app.
//
// # What only eyes can judge
//
// The sphere carries a smooth light-to-dark terminator and the spinning box's
// top face stays brighter than its sides all the way round, while the wire box
// and the three axis lines hold their exact colours whichever way they face.
//
// One sentence, three failures: a dead sun or a dead ambient flattens the
// terminator, a wrong normal matrix makes the box's shading swim or pop as it
// turns, and a self-lit shape wrongly routed through the lit path changes
// brightness with its facing instead of staying the colour it was given.
//
// A warm pool of light travels the ground with the orbiting sphere and fades
// to nothing before it reaches the resting box, and a cool cone sits on the
// resting box with a soft edge on the ground around it. A pool that reaches
// everywhere is a Range that did not pack; a cone with a hard edge is a cone
// whose inner and outer angles collapsed together.
package main

import (
	"context"
	"math"
	"os"
	"os/signal"
	"time"

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
// HUD keeps its proportions at any window size.
const (
	screenWidth  = 960
	screenHeight = 540
)

// CameraMain is the demo's only camera. It takes a negative id so it sorts
// below layerHUD, which is where a scene camera goes when 2D draws over it, and
// above layerBackdrop, which is what clears the frame.
const CameraMain scene.CameraID = -100

// The canvas layers, which are gfx orders directly.
//
// The scene camera declares no passes at all, and the implicit forward pass a
// camera with an empty Passes emits preserves colour rather than clearing it -
// a colour clear there would let a second camera silently erase the first. So
// the frame's one colour clear is canvas's, on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-examples"),
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: scene box"),
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
const Name kernel.PluginName = "box"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock, so frame N is reproducible and a test drives N steps directly: the
// update event's Dt is deliberately ignored.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose, where the reference screenshot is taken.
const (
	orbitRadius    = 5.6
	startAzimuth   = 0.9
	startElevation = 0.38
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
)

// orbitTarget is the point the camera looks at and orbits, a little above the
// ground plane so the plane fills the lower half of the frame.
var orbitTarget = m.Vec3{Y: 0.5}

// Box is the demo's gameplay plugin: it records the whole frame and owns the
// step counter, the orbit and the numbers the HUD prints.
type Box struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
	stats     stats
	rate      rate
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock.
//
// It cannot come from the update event's Dt: that is the driver's *fixed*
// timestep, so reading it back would report the rate the demo was asked to run
// at rather than the rate it managed. Nor can it come from the step counter,
// which is the whole point of accumulated fixed steps - step 120 is step 120
// whether it took two seconds or twenty. So this is measured against
// time.Now(), and it is deliberately the only thing in the demo that is: it
// feeds the HUD and nothing else, and no assertion reads it.
// It counts frames over a window rather than averaging 1/interval per frame.
// A per-frame average is dominated by its own worst sample: two ticks a
// microsecond apart during startup are a reading of a million, and an
// exponential average carries a thousandth of that for a hundred frames
// afterwards, so the HUD spends its first seconds reporting a number that never
// happened. A count over a window cannot do that - a spike is one frame in the
// count, whatever its interval was.
type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

// ratePeriod is how long the meter counts before republishing. Long enough that
// the number holds still to be read, short enough that a stall shows up while
// the human is still looking at what caused it.
const ratePeriod = 250 * time.Millisecond

// measure counts one update tick, which is one recorded frame here, and
// republishes the rate once the window is full.
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
// frame's because Passes publishes the frame the last flush consumed, and the
// flush runs at the end of the update tick this handler is part of.
type stats struct {
	passes    int
	ops       int
	recorded  int
	culled    int
	instances int
	lights    int
	batches   int
	// sphereVisible is whether the published frustum contains the orbiting
	// sphere's world bounds, computed here from the pass's own m.Frustum
	// rather than from the counts, so the HUD says which shape survived and
	// not only how many did.
	sphereVisible bool
}

// New builds the demo plugin at its documented starting pose.
func New() *Box { return &Box{azimuth: startAzimuth, elevation: startElevation} }

func (p *Box) Name() kernel.PluginName { return Name }

func (p *Box) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *Box) Register(registrar *kernel.Registrar, _ any) error {
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

// The frame's colours. Every one is written in sRGB and converted on the way
// in: Color holds linear components, and a demo that typed linear literals
// would be picking its palette in a space no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.06, 0.07, 0.09, 1)
	groundColor   = m.NewColorSrgb(0.52, 0.55, 0.60, 1)
	spinColor     = m.NewColorSrgb(0.42, 0.71, 0.94, 1)
	restColor     = m.NewColorSrgb(0.94, 0.55, 0.35, 1)
	sphereColor   = m.NewColorSrgb(0.85, 0.82, 0.45, 1)
	wireColor     = m.NewColorSrgb(0.95, 0.95, 1.00, 1)
	axisXColor    = m.NewColorSrgb(0.90, 0.25, 0.25, 1)
	axisYColor    = m.NewColorSrgb(0.30, 0.85, 0.35, 1)
	axisZColor    = m.NewColorSrgb(0.30, 0.45, 0.95, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
	lampColor     = m.NewColorSrgb(1.00, 0.72, 0.40, 1)
	coneColor     = m.NewColorSrgb(0.55, 0.75, 1.00, 1)
)

// The scene's fixed geometry.
const (
	groundSide    = 12
	spinRadius    = 2.4 // how far the orbiting sphere stands from the origin
	sphereRadius  = 0.6
	wireThickness = 0.03
	axisLength    = 2
	axisThickness = 0.02
)

// The two punctual lights. Intensity is unitless radiance at one world unit,
// so a lamp a unit above the ground puts about its intensity over pi onto the
// floor beneath it; Range is where its falloff window closes, so the pool
// stays a pool.
const (
	lampHeight    = 1.0 // above the orbiting sphere's centre
	lampIntensity = 4
	lampRange     = 5
	coneHeight    = 3.5 // above the resting box
	coneIntensity = 30
	coneRange     = 8
	coneInner     = 0.25
	coneOuter     = 0.45
)

// restPosition is where the resting box stands, and the point the spot light
// aims at.
var restPosition = m.Vec3{X: -3.4, Y: 0.5, Z: -2.1}

// draw records the whole frame: the camera, the eight debug shapes, the two lights, and the HUD.
func (p *Box) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var inputState kernel.Read[*input.State]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			inputState = access.GetRead[*input.State]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			q := sceneQueue.Get()
			p.rate.measure(time.Now())
			p.readStats(q)
			p.advance(inputState.Get())
			p.record(q)
			p.hud(canvasQueue.Get())
			return nil
		}
}

// advance steps the demo's own clock and applies the orbit. Input is read
// before the step so a held arrow moves the camera on the very frame it is
// pressed, and pausing stops the spin without freezing the orbit: a paused
// frame is still one a human wants to look around.
func (p *Box) advance(state *input.State) {
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
			p.elevation = m.Clamp(p.elevation+step, -1.4, 1.4)
		}
		if state.Pressed(input.KeyDown) {
			p.elevation = m.Clamp(p.elevation-step, -1.4, 1.4)
		}
		if state.JustPressed(input.KeyR) {
			p.azimuth, p.elevation, p.step = startAzimuth, startElevation, 0
		}
	}
	if !p.paused {
		p.step++
	}
}

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine and a test can drive N steps directly.
func (p *Box) time() float32 { return float32(p.step) * fixedStep }

// eye is the camera's position on its orbit.
func (p *Box) eye() m.Vec3 {
	cosElevation := float32(math.Cos(float64(p.elevation)))
	return orbitTarget.Add(m.Vec3{
		X: orbitRadius * cosElevation * float32(math.Sin(float64(p.azimuth))),
		Y: orbitRadius * float32(math.Sin(float64(p.elevation))),
		Z: orbitRadius * cosElevation * float32(math.Cos(float64(p.azimuth))),
	})
}

// spinCenter is where the orbiting sphere stands at the current step.
func (p *Box) spinCenter() m.Vec3 {
	angle := float64(p.time())
	return m.Vec3{
		X: spinRadius * float32(math.Cos(angle)),
		Y: sphereRadius,
		Z: spinRadius * float32(math.Sin(angle)),
	}
}

// record records the frame's camera, its eight draw calls and its two lights.
func (p *Box) record(q *scene.OpQueue) {
	q.Camera(CameraMain, scene.CameraDescr{
		Transform: scene.LookAt(p.eye(), orbitTarget, m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      0.1,
		Far:       100,
		// Everything else is left at its zero value on purpose, and every zero
		// is the default: Projection is Perspective, CullMask is LayersAll,
		// SunIntensity and AmbientIntensity are 1, and Passes is empty, which
		// emits one implicit forward pass at the camera's own id.
		SunDirection:  m.Vec3{X: -0.45, Y: -1, Z: -0.35},
		SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
		AmbientSky:    m.NewColorSrgb(0.18, 0.22, 0.30, 1),
		AmbientGround: m.NewColorSrgb(0.10, 0.09, 0.08, 1),
	})

	// Lit shapes. The ground is a Plane, which is two-sided, so orbiting under
	// it shows the floor rather than nothing.
	q.Plane(0, m.Vec3{}, m.Vec2{X: groundSide, Y: groundSide}, groundColor)

	// The spinning box is the TRS case: a position, a rotation and a scalar
	// Scale, all three at once.
	q.Box(0, scene.At(0, 0.5, 0).
		WithRotation(m.QuatAxisAngle(m.Vec3{X: 0.3, Y: 1, Z: 0}.Normalize(), p.time())).
		WithScale(0.9), spinColor)

	// The resting box is the zero-value case: an unrotated, unscaled transform
	// whose Scale field is never written, and a zero Scale means 1.
	q.Box(0, scene.At(restPosition.X, restPosition.Y, restPosition.Z), restColor)

	q.Sphere(0, p.spinCenter(), sphereRadius, sphereColor)

	// A warm lamp rides above the orbiting sphere, so its pool on the ground
	// moves, and a cool spot hangs over the resting box pointing straight
	// down. Neither writes Kind: PointLight and SpotLight set it.
	q.PointLight(0, scene.LightDescr{
		Position:  p.spinCenter().Add(m.Vec3{Y: lampHeight}),
		Color:     lampColor,
		Intensity: lampIntensity,
		Range:     lampRange,
	})
	q.SpotLight(0, scene.LightDescr{
		Position:  restPosition.Add(m.Vec3{Y: coneHeight}),
		Direction: m.Vec3{Y: -1},
		Color:     coneColor,
		Intensity: coneIntensity,
		Range:     coneRange,
		InnerCone: coneInner,
		OuterCone: coneOuter,
	})

	// Self-lit shapes: base colour black, the given colour as emissive, so they
	// keep their exact colour in a frame with no sun at all.
	q.WireBox(0, m.Vec3{Y: 0.5}, m.Vec3{X: 1.6, Y: 1.6, Z: 1.6}, wireThickness, wireColor)
	q.Line3D(0, m.Vec3{}, m.Vec3{X: axisLength}, axisThickness, axisXColor)
	q.Line3D(0, m.Vec3{}, m.Vec3{Y: axisLength}, axisThickness, axisYColor)
	q.Line3D(0, m.Vec3{}, m.Vec3{Z: axisLength}, axisThickness, axisZColor)
}

// RecordedDraws is how many draws the frame's eight calls flush to: the plane,
// two boxes and the sphere are one each, the three axis lines are one each, and
// the wire box is twelve, because each edge is culled on its own.
const RecordedDraws = 1 + 2 + 1 + 3 + 12

// RecordedOps is how many operations Ops reports: the camera registration, the
// eight shape calls, whatever they flush to, and the two lights.
const RecordedOps = 1 + 8 + RecordedLights

// RecordedLights is how many punctual lights the frame records: the lamp and
// the cone.
const RecordedLights = 2

// readStats reads the previous frame's flush result back out of the queue.
// Passes publishes the frame the last flush consumed, so these are the numbers
// for the frame before this one - which is what a HUD can print without
// stalling the pipeline to ask about the frame it is still recording.
func (p *Box) readStats(q *scene.OpQueue) {
	views := q.Passes(nil)
	p.stats = stats{passes: len(views), ops: len(q.Ops(nil))}
	for i := range views {
		p.stats.recorded += views[i].Recorded
		p.stats.culled += views[i].Culled
		p.stats.instances += views[i].Instances
		p.stats.lights += views[i].Lights
		p.stats.batches += len(views[i].Batches)
	}
	if len(views) > 0 {
		// m.Frustum.ContainsSphere against the pass's own published frustum:
		// the point of publishing it is that a caller can ask about one shape
		// rather than only read a count.
		p.stats.sphereVisible = views[0].Frustum.ContainsSphere(p.spinCenter(), sphereRadius)
	}
}
