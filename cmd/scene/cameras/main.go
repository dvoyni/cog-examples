// Command cameras is the scene plugin's multi-camera demo: three Camera
// Entities, two render targets, a depth prepass, and the coordinate helpers
// that let a 2D label and a mouse click agree with what a 3D camera drew.
//
//	go run ./cmd/scene/cameras
//
// It adds no assets. The world is scene's debug shapes and one of pbr's
// Khronos models, every one an Entity, arranged so that two cameras looking at
// it from different places have different things to say.
//
// What it exercises: multiple Camera Components and the shared pass-ordering
// space; orthographic projection beside perspective; CullMask against each
// Entity's Layers; negative camera ids and the duplicate-id error;
// gfx.TemporaryTarget named by a Pass's Target and composited by canvas as the
// split-screen answer; DepthAuto beside an explicit DepthTarget; Pass.Order as
// an offset and gfx's pass merging; a two-tag Material and a NoTarget()
// depth-only pass; scene.ViewProjection feeding m.WorldToScreen and
// m.ScreenToRay, the per-target viewport, the behind-the-camera ok, and
// m.Ray.IntersectSphere.
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
// # The Systems
//
// One per file, each named for its System, in the order they run on every
// update:
//
//	boundssystem.go     the model's pick sphere, once model answers Bounds
//	controlsystem.go    the keys, the clock, the pointer and the pick
//	duplicatesystem.go  the impostor camera while D is held
//	tracksystem.go      the main camera and its riders along the track
//	tintsystem.go       the picked cube's colour and the wire box around a pick
//	targetsystem.go     this frame's render targets, into each Camera's Passes
//	hudsystem.go        the composite, the nameplates and the HUD
//
// setupsystem.go spawns the world once, on the init event.
//
// # One pass some backends skip
//
// The minimap's depth-only prepass - no colour attachment, one depth texture,
// the shape a shadow map takes - runs on Vulkan and in a browser, and is
// declined on GLES, where cog's gogpu backend reports it once rather than
// encoding it: the GLES HAL binds no framebuffer for a colourless pass and would
// draw into whatever was bound last. Nothing samples that depth texture, so the
// skip changes no pixel: the reports counter goes to one and the frame is the
// frame.
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
//	d       hold to spawn a second camera under the main camera's id, provoking the duplicate-id error
//	click   pick a cube, the obelisk or the model, in either panel
package main

import (
	"errors"
	"fmt"
	"log"
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
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// The canvas layers, which are gfx orders directly, and they interleave with
// the camera ids in one flat space with no API for it on either side.
const (
	layerBackdrop canvas.Layer = -100
	layerViews    canvas.Layer = 0
	layerHUD      canvas.Layer = 10
)

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.
			WithTitle("cog examples: scene cameras").
			// Launched at the size the reference screenshot was taken at rather
			// than resized into it: a runtime resize leaves the viewport
			// un-refitted.
			WithSize(screenWidth, screenHeight),
	}
	permanentfs.Configure(config)

	// The demo plugin is last because its Systems order themselves against
	// scene's, which the plugins before it register.
	demo := New()
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		modelplugin.New(),
		gogpuplugin.New(),
		mcpplugin.New(),
		ecsplugin.New(), sceneplugin.New(),
		demo,
	}

	engine := kernel.New(config).Handler(demo.report).WithPlugins(plugins...)
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
const Name kernel.PluginName = "cameras"

type (
	setupSystem     kernel.Subscription[app.InitEvent]
	boundsSystem    kernel.Subscription[app.UpdateEvent]
	controlSystem   kernel.Subscription[app.UpdateEvent]
	duplicateSystem kernel.Subscription[app.UpdateEvent]
	trackSystem     kernel.Subscription[app.UpdateEvent]
	tintSystem      kernel.Subscription[app.UpdateEvent]
	targetSystem    kernel.Subscription[app.UpdateEvent]
	hudSystem       kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

// Cameras is the demo's gameplay plugin: it registers the Components and the
// Systems that drive the world. Its state is a resource, so every System names
// what it touches and the scheduler keeps them apart.
type Cameras struct {
	state   *State
	reports *reportLog
}

// New builds the demo plugin at its documented starting pose, paused, with
// nothing picked.
func New() *Cameras {
	reports := &reportLog{}
	return &Cameras{state: newState(reports), reports: reports}
}

func (p *Cameras) Name() kernel.PluginName { return Name }

func (p *Cameras) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name}
}

func (p *Cameras) Register(registrar *kernel.Registrar, _ any) error {
	// The one Khronos model this demo draws lives beside the repository rather
	// than beside the executable, so the demo contributes the asset set
	// explicitly and refuses to start without it. A cameras demo that came up
	// with a missing model would render two viewports of an empty flank and
	// blame the loader.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	ecs.RegisterComponent[Cube](registrar, uint32(len(cubes)))
	ecs.RegisterComponent[Rider](registrar, riders)
	ecs.RegisterComponent[Outline](registrar, 1)
	ecs.RegisterComponent[Impostor](registrar, 1)
	registrar.InitResource(p.state)

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))

	// The update Systems run as one chain, because each reads what the one
	// before it decided: the pick needs the model's bounds, the impostor and
	// the tint need the keys, and the composite needs this frame's targets.
	// Every one that spawns, moves or edits an Entity runs Before the scene
	// System that reads it, so a step draws the world as that step left it.
	registrar.Subscribe[boundsSystem](ecs.ToHandler[app.UpdateEvent](registrar, bounds)).First()
	registrar.Subscribe[controlSystem](ecs.ToHandler[app.UpdateEvent](registrar, control)).
		After[boundsSystem]()
	registrar.Subscribe[duplicateSystem](ecs.ToHandler[app.UpdateEvent](registrar, duplicate)).
		After[controlSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[trackSystem](ecs.ToHandler[app.UpdateEvent](registrar, track)).
		After[controlSystem]().Before[scene.RecordOnUpdate]()
	// A debug shape's colour and size are baked into its mesh and params by
	// scene's debug Systems, so an edit to one runs Before the first of them.
	registrar.Subscribe[tintSystem](ecs.ToHandler[app.UpdateEvent](registrar, tint)).
		After[controlSystem]().Before[scene.DebugOnUpdate]()
	registrar.Subscribe[targetSystem](ecs.ToHandler[app.UpdateEvent](registrar, target)).
		After[duplicateSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[targetSystem]()

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
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
// The second entry is not a failure this demo provokes, it is one some backends
// have: GLES cannot encode a pass with a depth attachment and no colour
// attachment, so cog's gogpu backend declines the pass there and reports it
// once, as it does for any backend it does not know can encode one. Vulkan and
// the browser run it. Where it is declined, this demo's depth prepass is
// skipped and its depth texture is left untouched - which changes no pixel of
// the frame, because the prepass writes into a texture of its own that nothing
// samples.
func (p *Cameras) report(err error) error {
	p.reports.add(err)
	if duplicate, ok := err.(scene.ErrCameraAlreadyRecorded); ok && duplicate.Camera == CameraMain {
		log.Printf("cameras: %v (expected: D is held)", err)
		return nil
	}
	var depthOnly gogpu.ErrDepthOnlyPassUnsupported
	if errors.As(err, &depthOnly) {
		log.Printf("cameras: %v (expected: this backend has no depth-only pass)", err)
		return nil
	}
	log.Printf("cameras: %v", err)
	return err
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
