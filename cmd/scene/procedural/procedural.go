package main

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

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
	cameraNear     = 0.1
	cameraFar      = 100
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
// beacon sits behind the camera at the starting pose, which is the only Entity
// in the frame the culler rejects.
var (
	ridgePosition     = m.Vec3{Y: 0.72}
	ribbonPosition    = m.Vec3{Y: 2.7}
	beaconPosition    = m.Vec3{X: -6.0, Y: 1.4, Z: 1.6}
	referencePosition = m.Vec3{X: 6.0, Y: 1.4, Z: 1.6}
	strayPosition     = m.Vec3{X: 14.0, Y: 7.4, Z: 11.1}
)

// The sizes the transforms apply. The ridge, the beacon and the sphere are
// authored at unit size, so these are also what scales their bounds into world
// space, and the reference sphere's radius is the beacon's world radius
// exactly. The ground is authored at its full side.
const (
	ridgeScale      = 7.0
	beaconScale     = 0.7
	referenceRadius = beaconScale
	groundSide      = 22
)

// The frame's colours. Every one is written in sRGB and converted on the way
// in: linear components are what the shader and the bundled PBR both want, and
// a demo that typed linear literals would be picking its palette in a space no
// colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.08, 1)
	groundColor   = m.NewColorSrgb(0.48, 0.50, 0.55, 1)
	// The reference sphere is a neutral grey on purpose: the comparison it is
	// here for is about where the light comes from, not what colour anything
	// is, and a hue of its own would invite reading it as one.
	referenceColor = m.NewColorSrgb(0.70, 0.70, 0.72, 1)
	hudColor       = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor    = m.NewColorSrgb(0.45, 0.48, 0.55, 1)

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

// camera is the camera Component. Everything not set is left at its zero
// value: Projection is Perspective, CullMask is every layer, the two
// intensities are 1, and Passes is empty, which is one default forward pass at
// the camera's own id, clearing depth and keeping the backdrop's colour.
func camera() scene.Camera {
	return scene.Camera{
		ID:            CameraMain,
		FovY:          fieldOfViewY,
		Near:          cameraNear,
		Far:           cameraFar,
		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	}
}

// A steady frame's draws, as the backend sees them. Every Entity carrying one
// mesh and one material is its own Batch, so each is one draw of one instance,
// but for the beacon and its stray, which share a mesh and are one Batch.
const (
	// BundledDraws is how many take the bundled PBR: the ground and the
	// reference sphere.
	BundledDraws = 2
	// CustomDraws is how many take the demo's own material and survive the
	// frustum: the ridge, the ribbon and the beacon.
	CustomDraws = 3
	// CulledDraws is how many the frustum rejects at the documented pose: the
	// stray beacon, which sits behind the camera there.
	CulledDraws = 1
)

// Procedural is the state the demo's Systems share, as one resource: the step
// counter, the orbit, the meshes minted from the geometry, the Entities that
// draw them, and the numbers the HUD prints.
type Procedural struct {
	step      int
	paused    bool
	azimuth   float32
	elevation float32
	// advanced is whether this update moved the clock. The beacon swaps on
	// the step that reaches its period, not on every paused frame spent
	// standing on it.
	advanced bool

	// The Entities whose Mesh or Transform a System rewrites. They are kept
	// here rather than found through a marker Component each, because each is
	// one Entity that lives as long as the demo.
	ridgeEntity, ribbonEntity, beaconEntity, strayEntity, ghostEntity ecs.Entity

	// ridge is the durable mesh rebuilt every frame, and ridgeCellCount the
	// resolution it currently carries. ribbon is the mesh baked this frame,
	// released when the next frame bakes its own.
	ridge          model.MeshRef
	ridgeCellCount int
	ribbon         model.MeshRef
	ribbonVertices int

	// beacon is the durable mesh released and re-baked every beaconPeriod
	// steps, and generation counts how many times that has happened. stale is
	// the ref the last release invalidated, drawn for exactly one frame by the
	// ghost Entity to show that a released ref is skipped rather than drawing
	// whatever now occupies its slot; staleDraws counts how many times the
	// demo has done that.
	beacon     model.MeshRef
	generation int
	stale      model.MeshRef
	staleDraws int
	// staleID is the id of the ref the ghost draws this frame, or zero on
	// every other frame. It is what the error handler matches on, and it is
	// atomic because that handler is called from outside every System.
	staleID atomic.Uint32

	// The mesh calls the demo has made, which is what the HUD reports in place
	// of anything about the frame: scene publishes no frame of its own, and
	// these are numbers the demo knows because it made them.
	bakes, updates, releases int

	// strayVisible is whether the camera's own frustum, rebuilt by the HUD
	// from the Camera Component, contains the stray beacon's world bounds. It
	// is what makes the culled stray a claim about a named object.
	strayVisible bool

	rate rate
}

// newProcedural is the demo's state at its documented starting pose.
func newProcedural() *Procedural {
	return &Procedural{
		azimuth: startAzimuth, elevation: startElevation, ridgeCellCount: ridgeCells,
	}
}

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine and a test can drive N steps directly.
func (p *Procedural) time() float32 { return float32(p.step) * fixedStep }

// eye is the camera's placement on its orbit.
func (p *Procedural) eye() m.Transform {
	cosElevation := float32(math.Cos(float64(p.elevation)))
	position := orbitTarget.Add(m.Vec3{
		X: orbitRadius * cosElevation * float32(math.Sin(float64(p.azimuth))),
		Y: orbitRadius * float32(math.Sin(float64(p.elevation))),
		Z: orbitRadius * cosElevation * float32(math.Cos(float64(p.azimuth))),
	})
	return m.LookAt(position, orbitTarget, m.Vec3{Y: 1})
}

// bakeBeacon bakes one generation of the beacon. It exists because BakeMesh
// takes a topology alongside the geometry, so a builder returning vertices and
// indices cannot be spread into the call.
func (p *Procedural) bakeBeacon(la model.LookupAccess) model.MeshRef {
	vertices, indices := beaconGeometry(beaconTint(p.generation))
	p.bakes++
	return la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)
}

// bakeRibbon bakes the ribbon at this step's twist.
func (p *Procedural) bakeRibbon(la model.LookupAccess) model.MeshRef {
	band := ribbonGeometry(ribbonSegments, p.time()*ribbonSpeed)
	p.ribbonVertices = len(band)
	p.bakes++
	return la.BakeMesh(band, nil, gfx.TopologyTriangleStrip)
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

// beaconPlace is where the beacon stands at time t, turned about a tilted axis
// so every face passes the sun.
func beaconPlace(t float32) m.Transform {
	return m.At(beaconPosition.X, beaconPosition.Y, beaconPosition.Z).
		WithRotation(m.QuatAxisAngle(m.Vec3{X: 0.35, Y: 1, Z: 0.2}.Normalize(), t*beaconSpin)).
		WithScale(beaconScale)
}
