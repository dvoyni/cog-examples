// Command cameras is the scene plugin's multi-camera demo: two cameras, two
// render targets, a depth prepass, and the coordinate helpers that let a 2D
// label and a mouse click agree with what a 3D camera drew.
//
//	go run ./cmd/scene/cameras
//
// It adds no assets. The world is box's debug vocabulary and one of pbr's
// Khronos models, arranged so that two cameras looking at it from different
// places have different things to say.
//
// What it exercises: multiple cameras and the shared pass-ordering space;
// orthographic projection beside perspective; CullMask; negative camera ids and
// the duplicate-id error; gfx.TemporaryTarget composited by canvas as the
// split-screen answer; DepthAuto beside an explicit DepthTarget; Pass.Order as
// an offset and gfx's pass merging; a multi-tag material and a NoTarget()
// depth-only pass; WorldToScreen, ScreenToRay, the per-target viewport, the
// behind-the-camera ok, and m.Ray.IntersectSphere.
//
// # The two panels
//
// The main view is a perspective camera flying down a corridor of four cubes
// and back. The minimap is an orthographic camera looking straight down at the
// same world, with the main camera's own frustum drawn on it.
//
// Neither renders to the screen. Each renders into a gfx.TemporaryTarget that
// canvas composites, because scene has no viewport rectangle and will not grow
// one: a projection-baked sub-rect does not clip, so a point at NDC x = 1.5 -
// which the clipper would have discarded - is remapped to 0.25 and rasterises
// into the neighbouring camera's half. One target per camera has no such edge,
// and it makes minimap, picture-in-picture and render scale the same feature
// rather than three.
//
// The minimap takes that literally: it renders 512 texels into 400 canvas
// units. There is no RenderScale field in scene because a temporary target
// smaller or larger than where it lands already is one.
//
// # The reference pose
//
// The demo opens paused at step 240, which is a quarter of the way through the
// camera's track and puts it at the centre of the corridor. reference.png
// beside this file is that frame. Space runs and pauses, R returns to exactly
// step 240, and the arrow keys step the camera along its track by hand while
// paused. A moving demo cannot take a reproducible reference capture while
// running, so it opens stopped at the picture.
//
// Input may run, pause and pick freely, and touching it voids nothing: the
// assertions live in cameras_test.go rather than in the running app.
//
// # One pass the desktop build skips
//
// The minimap's depth-only prepass - no colour attachment, one depth texture,
// the shape a shadow map takes - runs in a browser and is declined on the
// desktop, where cog's wgpu backend reports it once rather than encoding it.
// gogpu's Vulkan HAL never begins a render pass with no colour attachments and
// then faults ending it, so declining the pass is what turns a segfault into a
// line on the HUD. Nothing samples that depth texture, so the skip changes no
// pixel: the reports counter goes to one and the frame is the frame. Run the
// demo through cmd/web/build.sh to see the pass actually execute.
//
// # What only eyes can judge
//
// Two criteria, and both are deliberately visible rather than asserted, because
// a sign error in a Y flip is exactly the bug an assertion written by the author
// of the flip will happily confirm.
//
// The nameplate: each cube's label stays glued to the cube in both viewports,
// and disappears rather than mirroring when the cube passes behind the camera.
// One sentence covering three things at once - the Y flip, the per-target size
// rule, and the ok contract. A plate that drifts as the camera moves is a Y
// flip applied in one direction and not the other; a plate that is right in the
// main view and wrong on the minimap is the main view's target size used for
// both; and a plate that appears behind the camera, mirrored across the middle
// of the frame, is a w divide nobody checked the sign of. Watch a cube's plate
// as the camera passes it: it slides to the frame edge and stops existing. On
// the minimap the same plate stays, because an orthographic camera's clip w is
// 1 everywhere and it never fails the eye-plane test.
//
// The click: clicking a cube tints that cube and no other, including through
// the composited minimap. The nameplate proves world-to-screen and the click
// proves screen-to-world, and getting both right with one wrong sign is
// impossible. Clicking the minimap picks the same cube as clicking the main
// view, which is the whole per-target rule in one gesture: the click is mapped
// into the panel's own texels first and the panel's own camera second.
//
// # The keys
//
//	space   run and pause
//	left/right  step the camera along its track while paused
//	r       return to the reference pose
//	d       hold to record the main camera twice, provoking the duplicate-id error
//	click   pick a cube, the obelisk or the model, in either panel
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
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

// The canvas layers, which are gfx orders directly, and they interleave with
// the camera ids in one flat space with no API for it on either side.
const (
	layerBackdrop canvas.Layer = -100
	layerViews    canvas.Layer = 0
	layerHUD      canvas.Layer = 10
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The one Khronos model this demo draws lives beside the repository rather
	// than beside the executable, so the asset set is mounted explicitly and
	// the demo refuses to start without it. A cameras demo that came up with a
	// missing model would render two viewports of an empty flank and blame the
	// loader.
	storageConfig, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	config := map[kernel.PluginName]any{
		storage.Name: storageConfig,
		wgpu.Name: wgpu.DefaultConfig().
			WithTitle("cog examples: scene cameras").
			// Launched at the size the reference screenshot was taken at rather
			// than resized into it: a runtime resize leaves the viewport
			// un-refitted.
			WithSize(screenWidth, screenHeight),
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

// Name is the demo plugin's name.
const Name kernel.PluginName = "cameras"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// Cameras is the demo's gameplay plugin: it records the whole frame and owns
// the clock, the pick, and the numbers the HUD prints.
type Cameras struct {
	step   int
	paused bool

	// picked indexes targets, and is -1 when the last click hit nothing. The
	// bool from pick is checked rather than the index compared, because an
	// index nobody checked would leave the last thing tinted for ever.
	picked  int
	targets []pickable

	// obelisk is the durable mesh the multi-tag material draws, baked once on
	// the first frame that has a lookup to bake it with. material is built at
	// construction: it holds no GPU handle, and scene keys a material by
	// content, so the same value interns to one id every frame.
	obelisk  scene.MeshRef
	material scene.Material

	// The frame's two composited textures, published by record for draw2D to
	// sample. They are frame-local handles and are rebuilt every frame; holding
	// one across a frame boundary would name a texture the pool has already
	// handed to something else.
	mainTexture gfx.TextureDescr
	mapTexture  gfx.TextureDescr

	// duplicate is held while D is down, and makes the frame record CameraMain
	// twice. reports counts what the engine reported and lastReport is the most
	// recent one, both printed by the HUD: an error a demo provokes on purpose
	// has to be visible in the demo, or it is indistinguishable from one it did
	// not provoke.
	duplicate  bool
	reports    int
	lastReport string

	// pointer is the last pointer position in canvas coordinates, kept so the
	// HUD can say which panel the cursor is over before anything is clicked.
	pointer m.Vec2

	stats stats
	rate  rate
}

// New builds the demo plugin at its documented starting pose, paused, with
// nothing picked.
func New() *Cameras {
	return &Cameras{step: startStep, paused: true, picked: -1, material: newObeliskMaterial()}
}

func (p *Cameras) Name() kernel.PluginName { return Name }

func (p *Cameras) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *Cameras) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.frame)
	return nil
}

// report is the demo's error handler, and it is not optional here: the kernel's
// default logs and returns true, which terminates the engine, and this demo
// provokes a report on purpose every frame the D key is held.
//
// The allow-list is two errors, each matched on its own fields rather than on
// its text. A duplicate camera report about some other camera would still
// terminate, which is the difference between a demo that survives the failure
// it means to show and one that cannot tell you scene stopped working.
//
// The second entry is not a failure this demo provokes, it is one the desktop
// backend has: it cannot encode a pass with a depth attachment and no colour
// attachment, because gogpu's Vulkan HAL never begins such a render pass and
// then faults ending it. cog's wgpu backend declines the pass and reports it
// once rather than dying inside the driver. So on desktop this demo's depth
// prepass is skipped and its depth texture is left untouched - which changes no
// pixel of the frame, because the prepass writes into a texture of its own that
// nothing samples. The same demo runs the pass for real in a browser, whose
// WebGPU implementation has no such gap.
func (p *Cameras) report(err error) bool {
	p.reports++
	p.lastReport = err.Error()
	if duplicate, ok := err.(scene.ErrCameraAlreadyRecorded); ok && duplicate.Camera == CameraMain {
		log.Printf("cameras: %v (expected: D is held)", err)
		return false
	}
	var depthOnly wgpu.ErrDepthOnlyPassUnsupported
	if errors.As(err, &depthOnly) {
		log.Printf("cameras: %v (expected: this backend has no depth-only pass)", err)
		return false
	}
	log.Printf("cameras: %v", err)
	return true
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

// frame records everything: the two cameras, the world, the overlay, the
// composite and the HUD.
//
// It holds gfx's queue as well as scene's and canvas's, which no sibling demo
// does. Minting a render target takes the gfx queue, and scene deliberately
// offers no allocator of its own: a scene recorder does not hold that lock, so
// an app that renders a camera into a temporary target locks both and hands the
// target across. That is the whole handshake, and it is three lines.
func (p *Cameras) frame() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	var lookup kernel.Write[*scene.Lookup]
	var inputState kernel.Read[*input.State]
	var viewport kernel.Read[*app.Viewport]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
			lookup = access.GetWrite[*scene.Lookup]()
			inputState = access.GetRead[*input.State]()
			viewport = access.GetRead[*app.Viewport]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			q := sceneQueue.Get()
			la := scene.NewLookupAccess(k, lookup.Get())
			p.rate.measure(time.Now())
			p.readStats(q)
			p.advance(inputState.Get())
			p.buildTargets(la)
			p.click(inputState.Get(), viewport.Get())
			p.record(q, gfxQueue.Get(), la)
			p.draw2D(canvasQueue.Get())
			return nil
		}
}

// advance steps the demo's own clock and reads the keys.
//
// Input is read before the step so a held arrow moves the camera on the very
// frame it is pressed. The arrows work only while paused, because they are for
// walking the track to a frame worth looking at rather than for driving.
func (p *Cameras) advance(state *input.State) {
	if state != nil {
		if state.JustPressed(input.KeySpace) {
			p.paused = !p.paused
		}
		if state.JustPressed(input.KeyR) {
			p.step, p.picked = startStep, -1
		}
		p.duplicate = state.Pressed(input.KeyD)
		if p.paused {
			if state.Pressed(input.KeyRight) {
				p.step++
			}
			if state.Pressed(input.KeyLeft) {
				p.step--
			}
		}
	}
	if !p.paused {
		p.step++
	}
}

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine and a test drives N steps directly.
func (p *Cameras) time() float32 { return float32(p.step) * fixedStep }

// buildTargets assembles this frame's pickable list: four cubes, the obelisk,
// and the model if it is resident.
//
// The model is the entry that has to ask scene anything. Its bounds are the
// file's, which the app does not know and cannot hard-code without going stale
// the first time the asset changes, so it comes from LookupAccess.Bounds and
// goes through m.Sphere.Transform - exact under the uniform scale a
// scene.Transform carries. Until it is resident it is simply not in the list,
// which is the right answer: a click cannot pick what is not drawn.
func (p *Cameras) buildTargets(la scene.LookupAccess) {
	p.targets = p.targets[:0]
	for i := range cubes {
		p.targets = append(p.targets, pickable{name: cubes[i].name, sphere: cubeSphere(i)})
	}
	p.targets = append(p.targets, pickable{name: "obelisk", sphere: obeliskSphere()})
	if bounds, ok := la.Bounds(scene.ModelRef{Path: modelPath}); ok {
		local := m.Sphere{Center: m.Vec3{X: bounds.X, Y: bounds.Y, Z: bounds.Z}, Radius: bounds.W}
		p.targets = append(p.targets, pickable{
			name:   "model",
			sphere: local.Transform(modelTransform().Mat4()),
		})
	}
}

// click turns a mouse press into a pick, and it is the whole screen-to-world
// half of this demo.
//
// The order is what matters. A click is a point on the screen; a camera answers
// about points on its own target; so the click is mapped into a panel's texels
// first - which is where the panel's rect and its target size stop being
// interchangeable - and only then handed to that panel's camera. Hitting
// neither panel picks nothing rather than picking through the nearer camera.
//
// The two panels are two cameras and one world, so clicking the same cube in
// either picks the same entry. That is the criterion, and it is also the
// reason nothing here is written twice.
func (p *Cameras) click(state *input.State, view *app.Viewport) {
	if state == nil || view == nil || view.WindowWidth <= 0 || view.WindowHeight <= 0 {
		return
	}
	// The pointer arrives in window pixels; canvas draws in the logical screen
	// the window is fitted to, and ScreenToWorld undoes that fit.
	pointer := state.Pointer()
	logical := m.Vec2{
		X: float32(pointer.X) * view.Width / view.WindowWidth,
		Y: float32(pointer.Y) * view.Height / view.WindowHeight,
	}
	p.pointer = canvas.ScreenToWorld(
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe,
		m.Vec2{X: view.Width, Y: view.Height}, logical)
	if !state.JustPressed(input.KeyMouseLeft) {
		return
	}
	for _, panel := range p.panels() {
		texel, ok := panel.view.texel(p.pointer)
		if !ok {
			continue
		}
		ray, ok := scene.ScreenToRay(panel.camera, panel.view.size, texel)
		if !ok {
			continue
		}
		if hit, ok := pick(ray, p.targets); ok {
			p.picked = hit
		} else {
			p.picked = -1
		}
		return
	}
	p.picked = -1
}

// composited is one panel and the camera that fills it, which is the pair every
// coordinate question in this demo is asked of.
type composited struct {
	view   panel
	camera scene.CameraDescr
}

// panels is the two of them, main first: a click is tested against them in
// order, and they do not overlap, so the order is documentation rather than
// policy.
func (p *Cameras) panels() [2]composited {
	return [2]composited{
		{view: mainPanel, camera: mainCamera(p.time())},
		{view: mapPanel, camera: mapCamera(LayerWorld)},
	}
}

// The depth and colour clears the passes name. They are package-level because
// Pass takes pointers - nil preserves, and a pointer is the only zero value
// that can mean "do not clear" without a companion flag.
//
// The depth clear is 1.0, which is worth saying out loud: depth here is
// conventional, near maps to 0 and far to 1 and the compare is Less, so the
// naive ClearDepth of zero clears to the near plane and hides the whole scene.
var (
	clearFar     float32 = 1
	clearMain            = mainClearColor
	clearMinimap         = mapClearColor
)

// record allocates this frame's targets, declares the three cameras with their
// passes, and records the world.
func (p *Cameras) record(q *scene.OpQueue, g *gfx.OpQueue, la scene.LookupAccess) {
	mainTarget, mainTexture := g.TemporaryTarget(
		int(mainPanel.size.X), int(mainPanel.size.Y), gfx.FormatRGBA8Srgb)
	mapTarget, mapTexture := g.TemporaryTarget(
		int(mapPanel.size.X), int(mapPanel.size.Y), gfx.FormatRGBA8Srgb)
	// Two depth textures, and they are deliberately not the same one.
	//
	// mapDepth is the minimap's own, named rather than pooled because
	// DepthStore is inferred as StoreKeep exactly when a pass names a depth
	// texture - which is what lets the minimap's colour pass and the overlay
	// camera's pass merge into one GPU pass.
	//
	// prepassDepth is the depth-only pass's, and nothing else in the frame
	// touches it. That independence is the point: a NoTarget() pass has no
	// colour attachment to take a size from, so it must name a depth texture or
	// it is a reported error, and this is the shape a shadow map takes - render
	// depth from somewhere, sample it later. Scene v1 has no shadows, so
	// nothing samples it, and a backend that cannot encode a colourless pass
	// therefore drops it without changing a pixel of the frame. Feeding it into
	// the minimap's colour pass instead would have made that skip render the
	// whole minimap against undefined depth.
	_, mapDepthTexture := g.TemporaryTarget(
		int(mapPanel.size.X), int(mapPanel.size.Y), gfx.FormatDepth32F)
	_, prepassDepthTexture := g.TemporaryTarget(
		int(mapPanel.size.X), int(mapPanel.size.Y), gfx.FormatDepth32F)
	mapDepth := gfx.DepthTarget(mapDepthTexture)
	prepassDepth := gfx.DepthTarget(prepassDepthTexture)
	p.mainTexture, p.mapTexture = mainTexture, mapTexture

	camera := mainCamera(p.time())
	camera.Passes = []scene.Pass{{
		Target: mainTarget, ClearColor: &clearMain, ClearDepth: &clearFar,
		// Depth is left at its zero value, which is DepthAuto: a pooled texture
		// shared with every other same-size automatic pass in the frame. That
		// is why it must clear depth - it would otherwise inherit whatever the
		// last pass at this size left there.
	}}
	q.Camera(CameraMain, camera)
	if p.duplicate {
		// The same id, recorded twice in one frame. Camera is a registration,
		// not a free parameter, so a repeat means two systems each believe they
		// own that camera: it is reported and the first record wins. The second
		// record here is deliberately absurd - looking at the ground from
		// underneath - so that "the first record wins" is a thing the picture
		// says rather than a thing this comment says.
		wrong := camera
		wrong.Transform = scene.LookAt(m.Vec3{Y: -4}, m.Vec3{}, m.Vec3{Y: 1})
		q.Camera(CameraMain, wrong)
	}

	minimap := mapCamera(LayerWorld)
	minimap.Passes = []scene.Pass{
		{
			// The depth prepass: no colour target at all, its own depth
			// texture, one pass earlier than the camera. Order is an offset
			// from the camera id rather than an absolute, so -1 here means
			// "just before this camera" without the demo knowing what number
			// the camera took.
			Tag: TagDepth, Target: gfx.NoTarget(), Depth: prepassDepth,
			ClearDepth: &clearFar, Order: -1,
		},
		{
			Target: mapTarget, Depth: mapDepth,
			ClearColor: &clearMinimap, ClearDepth: &clearFar,
		},
	}
	q.Camera(CameraMap, minimap)

	// The overlay camera: the same pose and projection as the minimap, a
	// different cull mask, and a pass that clears nothing. Clearing nothing is
	// what makes it merge with the pass above into one GPU pass - same target,
	// same depth texture, both loads preserving - so the second camera costs a
	// pass in scene's bookkeeping and none on the GPU.
	overlay := mapCamera(LayerOverlay)
	overlay.Passes = []scene.Pass{{Target: mapTarget, Depth: mapDepth}}
	q.Camera(CameraOverlay, overlay)

	p.recordWorld(q, la)
	p.recordOverlay(q, camera)
}

// recordWorld records everything on the world layer: the ground, the four
// cubes, the obelisk and the model. Every camera that draws the world layer
// sees all of it.
func (p *Cameras) recordWorld(q *scene.OpQueue, la scene.LookupAccess) {
	q.Plane(LayerWorld, m.Vec3{}, m.Vec2{X: groundSide, Y: groundSide}, groundColor)
	for i := range cubes {
		q.Box(LayerWorld, cubeTransform(i), p.tint(cubes[i].name, cubes[i].color))
	}
	if p.obelisk.ID() == 0 {
		vertices, indices := obeliskMesh()
		p.obelisk = la.BakeMesh(vertices, indices, gfx.TopologyTriangleList)
	}
	q.Mesh(LayerWorld, p.obelisk, scene.MeshDraw{
		Transform: obeliskTransform(),
		Material:  p.material,
	})
	q.Model(LayerWorld, modelPath, scene.ModelDraw{Transform: modelTransform()})
}

// tint is the highlight: the picked thing takes the highlight colour and every
// other thing keeps its own.
//
// It is a colour swap rather than a material override because a debug shape's
// colour is a parameter of the call that recorded it, so highlighting one costs
// nothing and reaches every camera at once - which is what makes the criterion
// checkable through the composited minimap as well as the main view.
func (p *Cameras) tint(name string, color m.Color) m.Color {
	if p.pickedName() == name {
		return highlightColor
	}
	return color
}

// pickedName is what the last click landed on, or the empty string.
func (p *Cameras) pickedName() string {
	if p.picked < 0 || p.picked >= len(p.targets) {
		return ""
	}
	return p.targets[p.picked].name
}

// recordOverlay draws the main camera's frustum onto the overlay layer, which
// only the minimap's overlay camera sees.
//
// The outline is built with ScreenToRay from the main panel's four corners, so
// it is the same helper the click uses, evaluated for the same camera at the
// same size. Two things that must agree are therefore computed by one function
// rather than by two that can drift.
func (p *Cameras) recordOverlay(q *scene.OpQueue, camera scene.CameraDescr) {
	corners, ok := frustumCorners(camera)
	if !ok {
		return
	}
	eye := camera.Transform.Position
	q.Sphere(LayerOverlay, eye, 0.22, eyeColor)
	for i, corner := range corners {
		q.Line3D(LayerOverlay, eye, corner, 0.05, frustumColor)
		q.Line3D(LayerOverlay, corner, corners[(i+1)%len(corners)], 0.05, frustumColor)
	}
	if picked := p.picked; picked >= 0 && picked < len(p.targets) {
		// A wire box around whatever is picked, on the overlay layer, so the
		// minimap says what the click chose even when the thing itself is off
		// the main view's edge.
		sphere := p.targets[picked].sphere
		size := m.Vec3{X: sphere.Radius * 2, Y: sphere.Radius * 2, Z: sphere.Radius * 2}
		q.WireBox(LayerOverlay, sphere.Center, size, 0.05, highlightColor)
	}
}

// RecordedWorldDraws is how many draws the world layer flushes to: the ground
// plane, four cubes, the obelisk's one mesh, and the model's one primitive.
const RecordedWorldDraws = 1 + len(cubes) + 1 + 1

// RecordedOverlayDraws is how many the overlay layer flushes to with nothing
// picked: the eye sphere and the frustum's eight lines. A pick adds a wire box,
// which is twelve draws, because each edge is culled on its own.
const (
	RecordedOverlayDraws = 1 + 8
	WireBoxDraws         = 12
)

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
	depthDraw int // what the depth-tagged pass packed, which is the multi-tag material's whole story
	batches   int
}

// readStats reads the previous frame's flush result back out of the queue.
func (p *Cameras) readStats(q *scene.OpQueue) {
	views := q.Passes(nil)
	p.stats = stats{passes: len(views), ops: len(q.Ops(nil))}
	for i := range views {
		p.stats.recorded += views[i].Recorded
		p.stats.culled += views[i].Culled
		p.stats.instances += views[i].Instances
		p.stats.batches += len(views[i].Batches)
		if views[i].Tag == TagDepth {
			p.stats.depthDraw += views[i].Instances
		}
	}
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// counts frames over a window rather than averaging 1/interval per frame,
// because a per-frame average is dominated by its own worst sample: two ticks a
// microsecond apart during startup read as a million, and an exponential
// average carries a thousandth of that for a hundred frames afterwards.
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
