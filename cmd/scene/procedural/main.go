// Command procedural is the scene plugin's caller-owned-geometry demo: every
// shape in the frame but the ground is built by this program, out of a vertex
// type scene has never seen, drawn with a WGSL material this program wrote.
// There are no assets, and the shader is a Go string rather than a file, so the
// whole demo is one `go run` with nothing mounted.
//
//	go run ./cmd/scene/procedural
//
// custom-shader is merged into this demo rather than dropped: a custom vertex
// layout *requires* a custom material - the bundled PBR is one module with one
// vertex stage reading scene.Vertex's eight attributes - so the two cannot be
// demonstrated apart.
//
// What it exercises: TemporaryMesh against BakeMesh; the generic VertexLayout;
// UpdateMesh at a changing size; ReleaseMesh and the generations that make a
// stale ref detectable; MeshDraw.Bounds and NeverCull; the null skin and
// SCENE_NOSKIN, which every buffer-built draw carries; and, one layer down,
// gfx's BakeBuffer, ReBakeBuffer and BufferWithBytes - which is what the three
// mesh surfaces are:
//
//	BakeMesh   -> gfx.BakeBuffer      a durable buffer, uploaded once
//	UpdateMesh -> gfx.ReBakeBuffer    the same buffer id, new bytes, any length
//	TemporaryMesh -> gfx.BufferWithBytes  inline bytes, re-baked where recorded
//
// # The objects
//
// The ground and the reference sphere are the demo's two bundled-PBR draws, and
// they are here to be compared against. A plane has one normal, so it can show
// that the sun reaches the frame but not which way it points; the sphere carries
// a terminator, which is the thing a custom material can get wrong.
//
// Everything else is caller-owned geometry drawn with the caller's material. The
// ridge is durable: baked once, then rebuilt and re-baked every frame through
// UpdateMesh, changing resolution as it goes. The ribbon is temporary: minted
// fresh every frame and gone at the frame boundary. The beacon is durable and
// short-lived: released and re-baked every beaconPeriod steps, and drawn a
// second time as a stray far behind the camera, which is the draw the culler
// throws away.
//
// # The reference pose
//
// The demo starts at a documented fixed pose: the camera orbits orbitTarget at
// radius orbitRadius, azimuth startAzimuth and elevation startElevation, and
// stays there until an arrow key is held. R returns to it exactly, resetting
// the step counter with it. Demo time is accumulated fixed steps, never wall
// clock, so step N is the same frame on every machine and a test drives N steps
// directly.
//
// Input may orbit and pause freely, and touching it voids nothing: the
// assertions live in procedural_test.go rather than in the running app.
//
// # What only eyes can judge
//
// The reference sphere's bright side and the ridge's lit crests face the same
// way, and both fall to the same cool ambient where they turn away from it -
// even though the sphere is shaded by the bundled PBR and the ridge by a shader
// this program wrote.
//
// One sentence, three failures: a sceneFrame struct declared with a field out
// of place reads the sun from the wrong bytes and lights the ridge from a
// direction the ground disagrees with; a normal built from the instance record
// the wrong way round makes the beacon's shading swim as it spins while the
// ground stays put; and a material that lost its bindings renders nothing at
// all, because a bind group that fails to build takes the whole frame's command
// buffer with it.
//
// The ribbon's two faces are lit as one surface - no seam runs along the band
// where its front side meets its back - and the beacon changes colour every
// three seconds without ever showing two of itself.
package main

import (
	"context"
	"errors"
	"log"
	"math"
	"os"
	"os/signal"
	"sync/atomic"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-examples"),
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: scene procedural"),
	}

	// The demo plugin is last because it records into the queues the plugins
	// before it declare.
	demo := New()
	plugins := []kernel.Plugin{
		storage.New(),
		input.New(),
		gfx.New(),
		canvas.New(),
		scene.New(),
		wgpu.New(),
		demo,
	}

	kernel.New(config).Handler(demo.report).WithPlugins(plugins...).Run(ctx)
}

// report is the demo's error handler, and it is here because this demo reports
// an error on purpose.
//
// The kernel's default handler logs and returns true, which terminates the
// engine: a cog app that reports anything at all stops, and that is the right
// default, because most reports are bugs. The one report this demo means to
// provoke - a draw of the ref its own ReleaseMesh invalidated - would therefore
// shut the window three seconds after it opened.
//
// So exactly that report is logged and survived, matched by the mesh id the
// last release staled, and everything else still terminates. A demo that
// swallowed the whole class would be a demo that cannot tell you its material
// stopped binding.
func (p *Procedural) report(err error) bool {
	var unavailable scene.ErrMeshUnavailable
	if errors.As(err, &unavailable) && unavailable.Mesh == p.staleID.Load() {
		log.Printf("procedural: %v (expected: the released ref is drawn once on purpose)", err)
		return false
	}
	log.Printf("procedural: %v", err)
	return true
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "procedural"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// The demo's fixed timestep. The update event's Dt is deliberately ignored.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose, where the reference screenshot is taken.
const (
	orbitRadius    = 11.0
	startAzimuth   = 0.9
	startElevation = 0.34
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
)

// orbitTarget is the point the camera looks at and orbits, a little above the
// ground plane so the plane fills the lower half of the frame.
var orbitTarget = m.Vec3{Y: 1.1}

// The frame's cadence, in steps.
const (
	// ridgeResizePeriod is how often the ridge swaps between its two
	// resolutions. The re-bake itself happens every frame; this is what makes
	// the size change as well as the contents, which is the part of UpdateMesh
	// that has no capacity concept behind it.
	ridgeResizePeriod = 150
	// beaconPeriod is how often the beacon is released and re-baked. Three
	// seconds: long enough to read the generation off the HUD, short enough
	// that a run started to look at something else still shows one.
	beaconPeriod = 180
	// ridgeSpeed is how fast the ridge's wave travels, in radians per second.
	ridgeSpeed = 0.9
	// ribbonSpeed is how fast the ribbon's twist turns, in radians per second.
	ribbonSpeed = 0.7
	// beaconSpin is how fast the beacon turns, in radians per second.
	beaconSpin = 0.8
)

// Where the demo's objects stand. The ridge is centred on the origin and scaled
// up from its unit authoring size; the ribbon is authored in world units and
// only lifted; the beacon stands to one side and the reference sphere mirrors it
// on the other, at the same height and the same radius so the eye compares two
// round things rather than a round thing and a flat one. The stray copy of the
// beacon sits behind the camera at the starting pose, which is the only draw in
// the frame the culler rejects.
var (
	ridgePosition     = m.Vec3{Y: 0.72}
	ribbonPosition    = m.Vec3{Y: 2.7}
	beaconPosition    = m.Vec3{X: -6.0, Y: 1.4, Z: 1.6}
	referencePosition = m.Vec3{X: 6.0, Y: 1.4, Z: 1.6}
	strayPosition     = m.Vec3{X: 14.0, Y: 7.4, Z: 11.1}
)

// The sizes the transforms apply. Both meshes are authored at unit size, so
// these are also what scales their declared bounds into world space, and the
// reference sphere's radius is the beacon's world radius exactly.
const (
	ridgeScale      = 7.0
	beaconScale     = 0.7
	referenceRadius = beaconScale
	groundSide      = 22
)

// The frame's colours. Every one is written in sRGB and converted on the way
// in: linear components are what the shader and the plane's material both want,
// and a demo that typed linear literals would be picking its palette in a space
// no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.08, 1)
	groundColor   = m.NewColorSrgb(0.48, 0.50, 0.55, 1)
	// The reference sphere is a neutral grey on purpose: the comparison it is
	// here for is about where the light comes from, not what colour anything
	// is, and a hue of its own would invite reading it as one.
	referenceColor = m.NewColorSrgb(0.70, 0.70, 0.72, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)

	// The vertex tints, which are the only colour the custom material has: it
	// declares no parameters, so nothing but the vertices can carry one.
	ridgeLow   = linear(m.NewColorSrgb(0.16, 0.28, 0.42, 1))
	ridgeHigh  = linear(m.NewColorSrgb(0.85, 0.78, 0.52, 1))
	ribbonLow  = linear(m.NewColorSrgb(0.72, 0.28, 0.38, 1))
	ribbonHigh = linear(m.NewColorSrgb(0.96, 0.72, 0.42, 1))

	// beaconTints cycles one per generation, so a release and a re-bake are
	// visible as a colour change and not only as a number in the HUD.
	beaconTints = []m.Vec3{
		linear(m.NewColorSrgb(0.35, 0.90, 0.65, 1)),
		linear(m.NewColorSrgb(0.95, 0.55, 0.30, 1)),
		linear(m.NewColorSrgb(0.55, 0.60, 0.98, 1)),
		linear(m.NewColorSrgb(0.92, 0.90, 0.35, 1)),
	}
)

// linear drops a colour's alpha and hands back its linear components, which is
// what a vertex tint is. Colours are still written in sRGB above; this is only
// the shape change.
func linear(color m.Color) m.Vec3 {
	return m.Vec3{X: color.R, Y: color.G, Z: color.B}
}

// The camera's lighting. The custom material reads all three out of sceneFrame,
// so they light the ridge and the ground alike - which is the agreement the
// demo's one visible criterion rests on.
var (
	sunDirection  = m.Vec3{X: -0.45, Y: -1, Z: -0.35}
	sunColor      = m.NewColorSrgb(1, 0.97, 0.92, 1)
	ambientSky    = m.NewColorSrgb(0.24, 0.30, 0.42, 1)
	ambientGround = m.NewColorSrgb(0.15, 0.14, 0.12, 1)
)

// Procedural is the demo's gameplay plugin: it owns the geometry, the meshes
// minted from it, the step counter, the orbit and the numbers the HUD prints.
type Procedural struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32

	// material is built once at construction: it holds no GPU handle, so it
	// needs no backend, and scene keys a material by content, so handing the
	// same value to every draw interns to one id.
	material scene.Material

	// ridge is the durable mesh rebuilt every frame, and ridgeCellCount the
	// resolution it currently carries. ribbon is the frame-local one, valid
	// only in the frame that minted it.
	ridge          scene.MeshRef
	ridgeCellCount int
	ribbon         scene.MeshRef
	ribbonVertices int

	// beacon is the durable mesh released and re-baked every beaconPeriod
	// steps, and generation counts how many times that has happened. stale is
	// the ref the last release invalidated, drawn for exactly one frame to show
	// that a released ref is skipped rather than drawing whatever now occupies
	// its slot; staleDraws counts how many times the demo has done that.
	beacon     scene.MeshRef
	generation int
	stale      scene.MeshRef
	staleDrawn bool
	staleDraws int
	// staleID is the id of the ref this frame's ghost draw names, or zero on
	// every other frame. It is what report matches on, and it is atomic because
	// the error handler is called from the kernel's own goroutine rather than
	// from the update thread that writes it.
	staleID atomic.Uint32

	// submitted is how many draw calls the last recorded frame made, which is
	// the number the pass's own Recorded is compared against in the HUD: a
	// stale ref counts here and not there.
	submitted int

	stats stats
	rate  rate
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// counts frames over a window rather than averaging 1/interval per frame, so a
// pair of ticks a microsecond apart during startup is one frame in the count
// rather than a reading of a million that decays for a hundred frames after.
type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

// ratePeriod is how long the meter counts before republishing.
const ratePeriod = 250 * time.Millisecond

// measure counts one update tick and republishes the rate once the window is
// full.
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
	// strayVisible is whether the published frustum contains the stray
	// beacon's world bounds. It is what turns the culled count into a claim
	// about a named object rather than an anonymous one.
	strayVisible bool
}

// New builds the demo plugin at its documented starting pose.
func New() *Procedural {
	return &Procedural{
		azimuth: startAzimuth, elevation: startElevation,
		ridgeCellCount: ridgeCells, material: newMaterial(),
	}
}

func (p *Procedural) Name() kernel.PluginName { return Name }

func (p *Procedural) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *Procedural) Register(registrar *kernel.Registrar, _ any) error {
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

// draw records the whole frame, and is also where the demo's meshes are minted.
//
// It holds the *scene.Lookup write lock and the filesystem read lock so it can
// build a LookupAccess, which is the only way to bake, update or release a
// durable mesh. Taking them in the same handler that records is deliberate:
// BakeMesh mints its ref immediately and queues the upload onto the Lookup for
// scene's own flush to drain, so a mesh baked here is drawable here, in this
// frame, and LookupAccess itself never touches the GPU.
func (p *Procedural) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var lookup kernel.Write[*scene.Lookup]
	var filesystem kernel.Read[storage.FileSystem]
	var inputState kernel.Read[*input.State]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			lookup = access.GetWrite[*scene.Lookup]()
			filesystem = access.GetRead[storage.FileSystem]()
			inputState = access.GetRead[*input.State]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			q := sceneQueue.Get()
			p.rate.measure(time.Now())
			p.readStats(q)
			p.advance(inputState.Get())
			p.mint(q, scene.NewLookupAccess(k, lookup.Get(), filesystem.Get()))
			p.record(q)
			p.hud(canvasQueue.Get())
			return nil
		}
}

// advance steps the demo's own clock and applies the orbit. Input is read
// before the step so a held arrow moves the camera on the very frame it is
// pressed, and pausing stops the geometry without freezing the orbit: a paused
// frame is still one a human wants to look around.
func (p *Procedural) advance(state *input.State) {
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
func (p *Procedural) time() float32 { return float32(p.step) * fixedStep }

// eye is the camera's position on its orbit.
func (p *Procedural) eye() m.Vec3 {
	cosElevation := float32(math.Cos(float64(p.elevation)))
	return orbitTarget.Add(m.Vec3{
		X: orbitRadius * cosElevation * float32(math.Sin(float64(p.azimuth))),
		Y: orbitRadius * float32(math.Sin(float64(p.elevation))),
		Z: orbitRadius * cosElevation * float32(math.Cos(float64(p.azimuth))),
	})
}

// mint brings the frame's geometry up to date: the three mesh surfaces, in the
// three lifetimes they exist for.
//
// The ridge is baked on the first frame and updated on every one after. The
// ribbon is minted fresh every frame and is invalid the moment the frame ends.
// The beacon is released and re-baked on the beat, and the ref the release
// invalidated is kept for exactly one frame so record can prove it is skipped.
func (p *Procedural) mint(q *scene.OpQueue, la scene.LookupAccess) {
	p.stale, p.staleDrawn = scene.MeshRef{}, false
	p.staleID.Store(0)

	// The ridge. UpdateMesh replaces the geometry wholesale at any size while
	// keeping the ref and the buffer id, and refuses a change of vertex layout
	// or topology - both of which the pipeline key and the sort assume are
	// fixed for a ref's life. Only the size changes here, and only every
	// ridgeResizePeriod steps.
	if p.step/ridgeResizePeriod%2 == 1 {
		p.ridgeCellCount = ridgeCoarseCells
	} else {
		p.ridgeCellCount = ridgeCells
	}
	vertices, indices := ridgeGeometry(p.ridgeCellCount, p.time()*ridgeSpeed)
	if p.ridge == (scene.MeshRef{}) {
		p.ridge = la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)
	} else {
		la.UpdateMesh(p.ridge, vertices, indices)
	}

	// The ribbon. A temporary mesh copies the caller's bytes into the queue's
	// own arena, so this slice is free the moment the call returns, and the ref
	// carries the frame it was minted in: used in a later frame it is reported
	// and skipped rather than silently drawing whatever now holds its slot.
	band := ribbonGeometry(ribbonSegments, p.time()*ribbonSpeed)
	p.ribbon = q.TemporaryMesh(band, nil, gfx.TopologyTriangleStrip)
	p.ribbonVertices = len(band)

	// The beacon. A release makes the old ref stale at once - anything drawing
	// it afterwards is reported and skipped - while the buffers themselves are
	// freed at the frame boundary, so nothing the frame already recorded draws
	// from a dead buffer.
	switch {
	case p.beacon == (scene.MeshRef{}):
		p.beacon = bakeBeacon(la, p.generation)
	case p.step > 0 && p.step%beaconPeriod == 0:
		p.stale, p.staleDrawn = p.beacon, true
		p.staleID.Store(p.beacon.ID())
		la.ReleaseMesh(p.beacon)
		p.generation++
		p.beacon = bakeBeacon(la, p.generation)
		p.staleDraws++
	}
}

// record records the frame's camera and its five draws.
func (p *Procedural) record(q *scene.OpQueue) {
	q.Camera(CameraMain, scene.CameraDescr{
		Transform: scene.LookAt(p.eye(), orbitTarget, m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      0.1,
		Far:       100,
		// Everything else is left at its zero value: Projection is Perspective,
		// CullMask is LayersAll, the two intensities are 1, and Passes is
		// empty, which emits one implicit forward pass at the camera's own id.
		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	})

	// The two bundled-PBR draws, both of them here to be compared against: two
	// materials reading the same sun out of the same sceneFrame should agree
	// about where it is.
	//
	// The plane alone would not settle that. It has one normal, so it is one
	// brightness however the sun is read, and a shader reading sunDirection out
	// of the wrong offset would still light it. The sphere carries a
	// terminator, which is a direction rather than a level, and it stands at
	// the beacon's own height and radius on the opposite side.
	q.Plane(0, m.Vec3{}, m.Vec2{X: groundSide, Y: groundSide}, groundColor)
	q.Sphere(0, referencePosition, referenceRadius, referenceColor)

	// The ridge. Bounds is given explicitly because a custom vertex layout gets
	// no baked sphere - scene cannot locate POSITION in bytes it has never seen
	// a layout for - and a draw with no bounds at all is never culled.
	q.Mesh(0, p.ridge, scene.MeshDraw{
		Transform: scene.At(ridgePosition.X, ridgePosition.Y, ridgePosition.Z).WithScale(ridgeScale),
		Material:  p.material,
		Bounds:    m.Vec4{W: ridgeBounds},
	})

	// The ribbon. NeverCull rather than Bounds: a temporary mesh never
	// auto-computes a sphere either, and this one is rebuilt every frame at a
	// size the demo already knows is on screen, so the honest answer is to say
	// so rather than to maintain a number that would only ever be right by
	// accident.
	q.Mesh(0, p.ribbon, scene.MeshDraw{
		Transform: scene.At(ribbonPosition.X, ribbonPosition.Y, ribbonPosition.Z),
		Material:  p.material,
		NeverCull: true,
	})

	// The beacon, and the stray copy of it behind the camera. One mesh, two
	// draws, one declared sphere each: the beacon survives the frustum and the
	// stray does not, which is what makes the HUD's culled count a claim about
	// a named object.
	q.Mesh(0, p.beacon, scene.MeshDraw{
		Transform: scene.At(beaconPosition.X, beaconPosition.Y, beaconPosition.Z).
			WithRotation(m.QuatAxisAngle(m.Vec3{X: 0.35, Y: 1, Z: 0.2}.Normalize(), p.time()*beaconSpin)).
			WithScale(beaconScale),
		Material: p.material,
		Bounds:   m.Vec4{W: 1},
	})
	q.Mesh(0, p.beacon, scene.MeshDraw{
		Transform: scene.At(strayPosition.X, strayPosition.Y, strayPosition.Z).WithScale(beaconScale),
		Material:  p.material,
		Bounds:    m.Vec4{W: 1},
	})
	p.submitted = SteadyDraws

	// The one-frame ghost. On the step the beacon was released, the ref that
	// release invalidated is drawn once more, at the beacon's own place. It
	// draws nothing: the flush reports it unavailable, once for the ref rather
	// than once per draw, and the pass packs one fewer instance than it
	// recorded. If a second beacon ever appears here, a released ref has
	// started drawing whatever took its slot.
	if p.staleDrawn {
		q.Mesh(0, p.stale, scene.MeshDraw{
			Transform: scene.At(beaconPosition.X, beaconPosition.Y, beaconPosition.Z).
				WithScale(beaconScale),
			Material: p.material,
			Bounds:   m.Vec4{W: 1},
		})
		p.submitted++
	}
}

// BundledDraws is how many of a steady frame's draws take the bundled PBR: the
// ground plane and the reference sphere.
const BundledDraws = 2

// CustomDraws is how many take the demo's own material and survive the frustum:
// the ridge, the ribbon and the beacon.
const CustomDraws = 3

// CulledDraws is how many draws the frustum rejects at the documented pose: the
// stray beacon, which sits behind the camera there.
const CulledDraws = 1

// SteadyDraws is how many draws the demo records on a frame with no beacon swap
// in it.
const SteadyDraws = BundledDraws + CustomDraws + CulledDraws

// SteadyOps is how many operations Ops reports on such a frame: the camera
// registration and every draw.
const SteadyOps = 1 + SteadyDraws

// readStats reads the previous frame's flush result back out of the queue.
// Passes publishes the frame the last flush consumed, so these are the numbers
// for the frame before this one - which is what a HUD can print without
// stalling the pipeline to ask about the frame it is still recording.
func (p *Procedural) readStats(q *scene.OpQueue) {
	views := q.Passes(nil)
	p.stats = stats{passes: len(views), ops: len(q.Ops(nil))}
	for i := range views {
		p.stats.recorded += views[i].Recorded
		p.stats.culled += views[i].Culled
		p.stats.instances += views[i].Instances
		p.stats.batches += len(views[i].Batches)
	}
	if len(views) > 0 {
		p.stats.strayVisible = views[0].Frustum.ContainsSphere(strayPosition, beaconScale)
	}
}

// bakeBeacon bakes one generation of the beacon. It exists because BakeMesh
// takes a topology alongside the geometry, so a builder returning vertices and
// indices cannot be spread into the call.
func bakeBeacon(la scene.LookupAccess, generation int) scene.MeshRef {
	vertices, indices := beaconGeometry(beaconTint(generation))
	return la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)
}
