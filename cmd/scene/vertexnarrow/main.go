// Command vertexnarrow is a THROWAWAY prototype for
// https://github.com/dvoyni/cog/issues/172. It is not a demo of anything cog
// does, it is not meant to be merged, and nothing in this directory should be
// read as a proposal for an API.
//
//	go run ./cmd/scene/vertexnarrow
//
// # The question
//
// Issue 171 narrowed scene's vertex from 84 bytes to 40 and left exactly two
// things for the eye to settle. Everything else in that decision is either
// exact, unchanged, or rejected at load, and has no picture to look at.
//
//  1. Octahedral normals. The decision took oct32 in Unorm16x2, four bytes, on
//     an error bound. oct16 in Unorm8x2 is two bytes and is the only remaining
//     way under the 40-byte stride. Does the eye agree that oct16 is too
//     coarse - and does it agree that oct32 is enough?
//
//  2. Unorm16 texture coordinates against a per-primitive range. The decision
//     took them over Float16x2, which saves the same four bytes and needs no
//     per-mesh metadata at all, on the grounds that a half float's step doubles
//     at every power of two while a fixed-point range's does not. Does that
//     show?
//
// There is no pixel readback anywhere in gfx or wgpu, so neither of these can
// be a test. Both are pictures, and the answer is whatever a human says while
// looking at them.
//
// # How it is arranged
//
// Each station is ONE mesh in ONE draw, and every candidate encoding rides in
// the same vertex and is decoded in the same vertex stage. The fragment stage
// picks between them by where the fragment sits across the frame, so the rungs
// meet along a seam: same surface, same light, same distance, neighbouring
// pixels. A terrace that stops at the seam is the encoding; one that crosses it
// is the geometry.
//
// One draw is not a stylistic choice. See material.go: on this machine a scene
// frame recording more than one draw of a custom-material mesh never reaches
// the GPU, and cmd/scene/procedural fails the same way, so the constraint is
// inherited rather than introduced.
//
// # Why the surfaces are built rather than vendored
//
// This ticket originally named WaterBottle and Fox. The measurement behind
// issue 171 ruled both out for the normal question: of 52 primitives in the
// fifteen vendored assets, 43 author a normal and none is both smooth and large
// enough in frame to terrace, while Fox authors no NORMAL at all and gets
// generated flat ones, which tilt rather than band. A sphere is the cruellest
// smooth surface there is, and a surface that cannot show the artefact cannot
// answer the question. The coordinate station is built for the same reason: no
// vendored asset ships a 4K texture, and the claim is quoted in texels of one.
//
// # Controls
//
//	tab      switch station: normals / texture coordinates
//	1-4      normals: fill the frame with one rung alone
//	0        normals: the four stripes again
//	1-2      coordinates: the ordinary case (u 0..1) or the worst (u 0..18.5)
//	m        cycle the view: the lit surface, the error, the raw normal
//	q        cycle roughness, which is what sharpens or hides a normal error
//	arrows   orbit the sphere / pan along u and zoom
//	r        back to the documented starting pose
package main

import (
	"context"
	"math"
	"os"
	"os/signal"
	"strconv"
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

const (
	screenWidth  = 1280
	screenHeight = 720
)

const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

// CameraMain is the demo's only camera, at a negative id so it sorts below the
// HUD layer.
const CameraMain scene.CameraID = -100

// The grid texture. gridWidth is the number every figure in issue 171 is quoted
// against: "two texels adrift at 4K" is a statement about a 4096-texel axis. It
// is short on v because v carries no part of the question, and a square one
// would be 64 MiB of prototype.
const (
	gridWidth   = 4096
	gridHeight  = 128
	minorPeriod = 32
	majorPeriod = 256
)

// The sphere station. The tessellation is deliberately far finer than any rung
// of the ladder: faceting is a different artefact that looks like banding, and
// at this density the geometric step between adjacent normals is well under
// oct16's own error, so every terrace on screen is quantisation.
const (
	sphereSlices = 160
	sphereStacks = 80
	sphereRadius = 1.0
	orbitRadius  = 2.75
)

// The coordinate station.
//
// tileWorld is how many world units one UV tile spans, and it is the number
// that makes the picture legible. The band is three units tall and the camera
// frames it, so about six units are visible across; at 40 units to the tile
// that is a sixth of a tile, or roughly 650 texels of a 4096-wide texture
// spread over 1280 logical pixels. Texels come out a little larger than pixels,
// which is the only zoom at which a one-texel question can be looked at
// honestly.
const (
	bandSegments = 512
	bandHeight   = 3.0
	tileWorld    = 40.0
	panSpeed     = 0.35 // UV units per second held down
)

// The two coordinate cases, both real. 1.0 is the ordinary one - 32 of 52
// primitives carry a TEXCOORD_0, most land inside 0..1, and eight reach exactly
// 1.0 - and 18.5 is EmissiveStrengthTest's tiling, the worst in the set.
var bandCases = [...]struct {
	Name string
	UMax float32
}{
	{"ordinary   u 0 .. 1", 1.0},
	{"worst case u 0 .. 18.5", 18.5},
}

// The roughness presets. A near-mirror shows a normal error as a highlight that
// jumps; a chalky surface buries it in the cosine. Sweeping this is what keeps
// the judgement from being an artefact of one arbitrary material.
var roughnessPresets = [...]float32{0.08, 0.22, 0.45}

var (
	ladderModeNames = [...]string{"lit", "error", "raw", "ndl", "probe"}
	uvModeNames     = [...]string{"textured", "error", "probe"}
)

const (
	stationNormals = 0
	stationUV      = 1
)

const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
	orbitSpeed     = 1.2
	fieldOfViewY   = 1.0472
)

// The documented starting pose, where the reference captures are taken. The
// azimuth and elevation put the specular highlight near the middle of the
// frame, so the sharpest part of the picture straddles a seam.
const (
	startAzimuth   = 0.55
	startElevation = 0.30
	startPan       = 0.5
	startZoom      = 1.0
	stripesAll     = -1
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-examples"),
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: vertexnarrow (prototype)"),
	}

	plugins := []kernel.Plugin{
		storage.New(), input.New(), gfx.New(), canvas.New(), scene.New(), wgpu.New(), New(),
	}
	kernel.New(config).WithPlugins(plugins...).Run(ctx)
}

const Name kernel.PluginName = "vertexnarrow"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// Demo holds the two stations and the pose the human is looking from.
type Demo struct {
	step       int
	station    int
	solo       int // stripesAll, or a rung index shown alone
	ladderMode int
	uvMode     int
	roughness  int
	bandCase   int
	azimuth    float32
	elevation  float32
	panU       float32
	zoom       float32
	sunUp      float32
	sunRight   float32

	sphere    scene.MeshRef
	bands     [len(bandCases)]scene.MeshRef
	bandRange [len(bandCases)]uvRange
	grid      gfx.TextureDescr
	minted    bool

	// octStats is each rung's angular error over the sphere's own normals,
	// computed once at bake. It is not the judgement, but a picture with no
	// number beside it is hard to argue with six months later.
	octStats [len(rungs)]struct{ Mean, Max float32 }

	rate rate
}

func New() *Demo {
	d := &Demo{
		azimuth: startAzimuth, elevation: startElevation,
		panU: startPan, zoom: startZoom, solo: stripesAll,
	}
	d.configure()
	return d
}

// configure lets the environment set the opening pose, so a capture can be
// retaken byte-for-byte from a shell instead of by pressing keys in the right
// order. Every one of these has a key that does the same thing; nothing here is
// reachable only this way.
//
//	VN_STATION 0 normals, 1 coordinates
//	VN_MODE    index into the station's view list
//	VN_SOLO    -1 stripes, or a rung index
//	VN_ROUGH   index into roughnessPresets
//	VN_CASE    index into bandCases
//	VN_PAN     u at frame centre
//	VN_ZOOM    framing multiplier, smaller is closer
func (d *Demo) configure() {
	number := func(key string, fallback float64) float64 {
		v, err := strconv.ParseFloat(os.Getenv(key), 64)
		if err != nil {
			return fallback
		}
		return v
	}
	d.station = int(number("VN_STATION", 0))
	d.solo = int(number("VN_SOLO", stripesAll))
	d.roughness = int(number("VN_ROUGH", 0))
	d.bandCase = int(number("VN_CASE", 0))
	d.panU = float32(number("VN_PAN", startPan))
	d.zoom = float32(number("VN_ZOOM", startZoom))
	d.azimuth = float32(number("VN_AZ", startAzimuth))
	d.elevation = float32(number("VN_EL", startElevation))
	d.sunUp = float32(number("VN_SUNUP", 1.15))
	d.sunRight = float32(number("VN_SUNRIGHT", -0.35))
	mode := int(number("VN_MODE", 0))
	if d.station == stationNormals {
		d.ladderMode = mode % len(ladderModeNames)
	} else {
		d.uvMode = mode % len(uvModeNames)
	}
}

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.draw)
	return nil
}

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

func (p *Demo) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var lookup kernel.Write[*scene.Lookup]
	var inputState kernel.Read[*input.State]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			lookup = access.GetWrite[*scene.Lookup]()
			inputState = access.GetRead[*input.State]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			p.rate.measure(time.Now())
			p.mint(scene.NewLookupAccess(k, lookup.Get()))
			p.advance(inputState.Get())
			p.record(sceneQueue.Get())
			p.hud(canvasQueue.Get())
			p.step++
			return nil
		}
}

// mint bakes the three meshes and the grid texture, once.
func (p *Demo) mint(la scene.LookupAccess) {
	if p.minted {
		return
	}
	p.minted = true

	vertices, indices := sphereGeometry(sphereSlices, sphereStacks, sphereRadius)
	p.sphere = la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)

	// What each rung actually costs on this surface. The sphere's own normals
	// cover every direction evenly enough to stand in for "any".
	for i, r := range rungs {
		if r.Bits == 0 {
			continue
		}
		var total, worst float32
		for _, v := range vertices {
			e := octError(v.NormalF32, r.Bits)
			total += e
			worst = max(worst, e)
		}
		p.octStats[i].Mean = total / float32(len(vertices))
		p.octStats[i].Max = worst
	}

	for i, c := range bandCases {
		band := bandGeometry(bandSegments, c.UMax*tileWorld, bandHeight, c.UMax)
		p.bands[i] = la.BakeMesh(band, bandIndices(bandSegments), gfx.TopologyTriangleList)
		uvs := make([]m.Vec2, len(band))
		for j, v := range band {
			uvs[j] = v.UVf32
		}
		p.bandRange[i] = newUVRange(uvs)
	}

	p.grid = gfx.TextureWithBytes(gridWidth, gridHeight, gfx.FormatRGBA8Srgb, gridTexture(), true, false)
}

func (p *Demo) advance(state *input.State) {
	if state == nil {
		return
	}
	if state.JustPressed(input.KeyTab) {
		p.station = 1 - p.station
	}
	if state.JustPressed(input.KeyM) {
		if p.station == stationNormals {
			p.ladderMode = (p.ladderMode + 1) % len(ladderModeNames)
		} else {
			p.uvMode = (p.uvMode + 1) % len(uvModeNames)
		}
	}
	if state.JustPressed(input.KeyQ) {
		p.roughness = (p.roughness + 1) % len(roughnessPresets)
	}
	if state.JustPressed(input.KeyR) {
		p.azimuth, p.elevation = startAzimuth, startElevation
		p.panU, p.zoom, p.solo = startPan, startZoom, stripesAll
	}

	step := orbitSpeed * fixedStep
	if p.station == stationNormals {
		if state.JustPressed(input.Key0) {
			p.solo = stripesAll
		}
		for i := range rungs {
			if state.JustPressed(input.Key1 + input.Key(i)) {
				p.solo = i
			}
		}
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
		return
	}

	for i := range bandCases {
		if state.JustPressed(input.Key1 + input.Key(i)) {
			p.bandCase, p.panU = i, bandCases[i].UMax*0.5
		}
	}
	pan := panSpeed * fixedStep * p.zoom
	if state.Pressed(input.KeyLeft) {
		p.panU = m.Clamp(p.panU-pan, 0, bandCases[p.bandCase].UMax)
	}
	if state.Pressed(input.KeyRight) {
		p.panU = m.Clamp(p.panU+pan, 0, bandCases[p.bandCase].UMax)
	}
	if state.Pressed(input.KeyUp) {
		p.zoom = m.Clamp(p.zoom*0.98, 0.02, 20)
	}
	if state.Pressed(input.KeyDown) {
		p.zoom = m.Clamp(p.zoom*1.02, 0.02, 20)
	}
}

func (p *Demo) record(q *scene.OpQueue) {
	if p.station == stationNormals {
		p.recordNormals(q)
		return
	}
	p.recordUV(q)
}

func (p *Demo) recordNormals(q *scene.OpQueue) {
	cosElevation := float32(math.Cos(float64(p.elevation)))
	eye := m.Vec3{
		X: orbitRadius * cosElevation * float32(math.Sin(float64(p.azimuth))),
		Y: orbitRadius * float32(math.Sin(float64(p.elevation))),
		Z: orbitRadius * cosElevation * float32(math.Cos(float64(p.azimuth))),
	}

	q.Camera(CameraMain, scene.CameraDescr{
		Transform: scene.LookAt(eye, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:      fieldOfViewY,
		Near:      0.05,
		Far:       50,
		// One hard sun and a dim ambient. A busy environment would give the
		// surface a second gradient to hide terracing in, and the point of this
		// station is that there is nowhere for the artefact to hide.
		// The sun is rigged to the camera rather than to the world, so the
		// specular highlight stays in the same place on screen at every orbit
		// position - up and to the left of centre, where the stripes cut
		// through it. A world-fixed sun puts the highlight somewhere different
		// after every arrow press, and the one thing this station must not do
		// is move the artefact away from the seam being judged.
		SunDirection:  p.sun(eye).Negate(),
		SunIntensity:  3.0,
		SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
		AmbientSky:    m.NewColorSrgb(0.10, 0.13, 0.18, 1),
		AmbientGround: m.NewColorSrgb(0.05, 0.045, 0.04, 1),
	})

	q.Mesh(0, p.sphere, scene.MeshDraw{
		Material: newLadderMaterial(
			ladderModeNames[p.ladderMode], roughnessPresets[p.roughness], p.solo),
	})
}

// sun returns the direction light arrives FROM, built in the camera's own
// basis: mostly along the view axis, lifted and pushed left just enough that
// the mirror direction lands inside the disc rather than on the silhouette.
func (p *Demo) sun(eye m.Vec3) m.Vec3 {
	view := eye.Normalize()
	right := m.Vec3{Y: 1}.Cross(view).Normalize()
	up := view.Cross(right)
	return view.Add(up.MulS(p.sunUp)).Add(right.MulS(p.sunRight)).Normalize()
}

func (p *Demo) recordUV(q *scene.OpQueue) {
	framed := bandHeight * 1.08 * p.zoom
	distance := framed / (2 * float32(math.Tan(float64(fieldOfViewY/2))))
	centre := m.Vec3{X: p.panU * tileWorld}

	q.Camera(CameraMain, scene.CameraDescr{
		Transform:     scene.LookAt(centre.Add(m.Vec3{Z: distance}), centre, m.Vec3{Y: 1}),
		FovY:          fieldOfViewY,
		Near:          0.01,
		Far:           max(100, distance*4),
		SunDirection:  m.Vec3{Z: -1},
		SunColor:      m.NewColorSrgb(1, 1, 1, 1),
		AmbientSky:    m.NewColorSrgb(1, 1, 1, 1),
		AmbientGround: m.NewColorSrgb(1, 1, 1, 1),
	})

	q.Mesh(0, p.bands[p.bandCase], scene.MeshDraw{
		Material:  newUVMaterial(uvModeNames[p.uvMode], p.bandRange[p.bandCase]),
		Params:    []gfx.ParameterDescr{gfx.TextureParam("gridTexture", p.grid)},
		NeverCull: true,
	})
}

// --- the frame rate meter, lifted from cmd/scene/box -------------------------

type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

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

// --- predicted error, for the HUD --------------------------------------------

// float16Step is the distance between adjacent half floats at u, which is the
// grid a Float16x2 coordinate is stored on. It doubles at every power of two,
// and that is the whole of the argument against half-float UVs.
func float16Step(u float32) float32 {
	if u == 0 {
		return float32(math.Ldexp(1, -24))
	}
	exponent := math.Floor(math.Log2(math.Abs(float64(u))))
	if exponent < -14 {
		exponent = -14
	}
	return float32(math.Ldexp(1, int(exponent)-10))
}

// unormStep is the step of a 16-bit fixed-point coordinate over a range, which
// does not depend on where in the range it is read.
func unormStep(extent float32) float32 { return extent / 65535 }

func texels(uvStep float32) float32 { return uvStep * gridWidth }
