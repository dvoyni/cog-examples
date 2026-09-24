// Command box is the scene plugin's zero-asset demo: the whole debug-shape
// vocabulary, one camera, and a HUD, with no file on disk anywhere in the
// frame. Even the HUD's text costs nothing: canvas embeds a font and draws with
// it when a text op names none. It is the floor of the API and the smoke test
// that still works when the asset story breaks, which is why it is kept beside
// pbr despite covering nothing pbr misses.
//
//	go run ./cmd/scene/box
//
// What it exercises: the five debug-shape Components (DebugBox, DebugSphere,
// DebugPlane, DebugLine, DebugWireBox), each on an Entity of its own placed by
// its m.Transform; Transform TRS and WithScale, the uniform spelling of its
// per-axis Scale; LookAt; an empty Passes yielding the implicit forward pass at
// the camera id; sun and hemispheric ambient; a point light and a spot light
// placed by their Transforms; the linear pipeline and the present pass;
// every-zero-value-is-the-default; and the m additions.
//
// Every System is in a file of its own, named for it, and the Components the
// demo declares are in components.go:
//
//	setupSystem  spawns the camera, the eight shapes and the two lights  (init)
//	clockSystem  steps the demo clock and reads the keys                 (update)
//	spinSystem   turns the spinning box                                  (update)
//	orbitSystem  carries the sphere and its lamp round the origin        (update)
//	eyeSystem    places the camera on its orbit                          (update)
//	hudSystem    counts the world and prints it                          (update)
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
// Every shape holds the exact colour it was given whichever way it faces: a
// debug shape is drawn unlit, its Color straight out of the fragment stage, so
// the sun, the ambient and the two lights reach none of them. A shape that
// darkens as it turns is one wrongly routed through the lit path.
//
// The spinning box turns about a tilted axis without its size or place
// drifting, which is the TRS order; the wire box's corners close, because each
// edge runs half a width past the corner it meets; and the three axis lines
// meet at the origin in their own colours, red along X, green along Y, blue
// along Z. The ground is one-sided and faces up, so orbiting below it shows
// the shapes from underneath with no floor in the way.
//
// The warm lamp riding above the orbiting sphere and the cool spot over the
// resting box are real Light Entities in the frame, so a Mesh or a Model
// added beside the shapes is lit by them; the debug shapes themselves are not.
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
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
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: scene box"),
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
const Name kernel.PluginName = "box"

// Box is the demo's gameplay plugin: it registers the demo's Components, its
// State and its Systems. Everything it draws is an Entity, and the plugin
// itself holds nothing.
type Box struct{}

// New builds the demo plugin. The documented starting pose is NewState's.
func New() *Box { return &Box{} }

func (p *Box) Name() kernel.PluginName { return Name }

func (p *Box) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, gfx.Name, input.Name, scene.Name, storage.Name}
}

type (
	setupOnInit   kernel.Subscription[app.InitEvent]
	clockOnUpdate kernel.Subscription[app.UpdateEvent]
	spinOnUpdate  kernel.Subscription[app.UpdateEvent]
	orbitOnUpdate kernel.Subscription[app.UpdateEvent]
	eyeOnUpdate   kernel.Subscription[app.UpdateEvent]
	hudOnUpdate   kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (p *Box) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Spin](registrar, 1)
	ecs.RegisterComponent[Orbit](registrar, 2)
	registrar.InitResource(NewState())
	registrar.InitResource(&Meter{})

	registrar.Subscribe[setupOnInit](ecs.ToHandler[app.InitEvent](registrar, setupSystem))
	// The clock runs first, so every System after it places the world at the
	// step it has just taken, and a held arrow moves the camera on the very
	// frame it is pressed.
	registrar.Subscribe[clockOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, clockSystem)).First()
	// Every System that moves an Entity runs Before scene.RecordOnUpdate,
	// so a step draws the world as that step left it rather than whichever side
	// of the tie the scheduler happened to break.
	registrar.Subscribe[spinOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, spinSystem)).
		After[clockOnUpdate]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[orbitOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, orbitSystem)).
		After[clockOnUpdate]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[eyeOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, eyeSystem)).
		After[clockOnUpdate]().Before[scene.RecordOnUpdate]()
	// The HUD asks whether the sphere is in the camera's frustum, so it runs
	// after both the orbit that moved the sphere and the eye that moved the
	// camera.
	registrar.Subscribe[hudOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, hudSystem)).
		After[orbitOnUpdate]().After[eyeOnUpdate]()

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
// hangs over.
var restPosition = m.Vec3{X: -3.4, Y: 0.5, Z: -2.1}

// DrawnShapes is how many debug shapes the demo spawns, and so how many draws
// its camera makes at the documented pose, where nothing is culled: the plane,
// two boxes, the sphere, the three axis lines and the wire box, whose twelve
// edges are one mesh and so one draw.
const DrawnShapes = 1 + 2 + 1 + 3 + 1

// SpawnedLights is how many punctual lights the demo spawns: the lamp and the
// cone.
const SpawnedLights = 2
