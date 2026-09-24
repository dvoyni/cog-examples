// Command fountain is the scene showcase: a nozzle throwing motes into a
// basin while a fox circles it, every drawable an Entity and every Component of
// scene doing its one job.
//
//	go run ./cmd/scene/fountain
//
//	Transform   every Entity's place; a mote's Vec3 scale stretches it along its velocity  (drift)
//	Mesh        the motes, a unit cube baked once at startup, and the basin disc           (setup, hatch)
//	Params      each mote's own tint, fading with its life                                (drift)
//	Material    the mote shader, and the basin's two pass tags: ground and forward        (material.go, setup, hatch)
//	Model       the nozzle (WaterBottle) and the fox                                      (setup)
//	Animation   the fox's walk and run, read off its gait machine                         (prowl)
//	Light       a point lamp over the nozzle, and a spot light following the fox         (setup, prowl)
//	Camera      one camera orbiting on the demo clock, its two passes clearing via m.Maybe (setup, orbit)
//
// The demo's own Components and Component sets are in components.go, and each
// System sits in the file named for it. What decides the frame without being
// a System - the spray and its clock and random stream (spray.go), the fox's
// circle and gait (gait.go), the layout of the stage (stage.go), the geometry
// and shaders (geometry.go, material.go) and the HUD's text (hud.go) - is plain
// Go the Systems call.
//
// There is no input. Demo time is fixed steps and the spray's random stream is
// seeded by a constant, so step N is the same frame on every machine.
// reference.png is step 600, taken with mcpplugin.New() added to the plugin
// list and docs/narrowing/capture.py; fountain_test.go asserts the frame at
// that step, and that its HUD reads as the image's does.
//
// The HUD's frame figures are read off gfx's frame snapshot. The HUD holds
// gfx's one snapshot slot every tick, so an agent asking for a snapshot of its
// own while this runs is refused as busy.
//
// Why scene is shaped the way it is lives in its README:
// https://github.com/dvoyni/cog/blob/main/bundles/scene/docs/README.md
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
	"github.com/dvoyni/cog/bundles/input/inputplugin"
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

	"github.com/dvoyni/cog-examples/internal/assets"
)

const (
	CameraMain scene.CameraID = -100
	layerHUD   canvas.Layer   = 0
)

// prewarmEntities is how many Entities the world reserves room for up front,
// which is a hint and not a limit: the spray settles near a hundred and twenty
// motes, plus six Entities of furniture.
const prewarmEntities = 512

// peakMotes is the population the mote Stores reserve for.
const peakMotes = 256

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.WithTitle("cog examples: scene fountain"),
		ecs.Name:   ecs.Config{PrewarmEntities: prewarmEntities},
	}
	permanentfs.Configure(config)

	engine := kernel.New(config).WithPlugins(
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(), gfxplugin.New(), canvasplugin.New(), modelplugin.New(), gogpuplugin.New(),
		ecsplugin.New(), sceneplugin.New(), New(),
	)
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

const Name kernel.PluginName = "fountain"

type Demo struct{}

func New() *Demo { return &Demo{} }

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, scene.Name, gfx.Name, model.Name, storage.Name}
}

type (
	setupSystem      kernel.Subscription[app.InitEvent]
	hatchSystem      kernel.Subscription[app.UpdateEvent]
	accelerateSystem kernel.Subscription[app.UpdateEvent]
	driftSystem      kernel.Subscription[app.UpdateEvent]
	reapSystem       kernel.Subscription[app.UpdateEvent]
	rigSystem        kernel.Subscription[app.UpdateEvent]
	prowlSystem      kernel.Subscription[app.UpdateEvent]
	orbitSystem      kernel.Subscription[app.UpdateEvent]
	hudSystem        kernel.Subscription[app.UpdateEvent]
	rearmHandler     kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	// The nozzle and the fox are vendored models, and storage mounts nothing
	// by default, so the demo contributes the asset set itself.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	ecs.RegisterComponent[Velocity](registrar, peakMotes)
	ecs.RegisterComponent[Life](registrar, peakMotes)
	// The fox's gait machine is a Component of its own, beside the Animation
	// it fills: one fox, so one row.
	ecs.RegisterComponent[model.ClipMachine](registrar, 1)
	registrar.InitResource(&Fountain{spray: NewSpray()})

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	registrar.Subscribe[hatchSystem](ecs.ToHandler[app.UpdateEvent](registrar, hatch)).First()
	registrar.Subscribe[accelerateSystem](ecs.ToHandler[app.UpdateEvent](registrar, accelerate))
	// Every System that moves or retires an Entity runs Before
	// scene.RecordOnUpdate, the System that turns the world into draws, so a
	// step draws the world as that step left it rather than whichever side of
	// the tie the scheduler happened to break.
	registrar.Subscribe[driftSystem](ecs.ToHandler[app.UpdateEvent](registrar, drift)).
		After[accelerateSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[reapSystem](ecs.ToHandler[app.UpdateEvent](registrar, reap)).
		After[driftSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[rigSystem](ecs.ToHandler[app.UpdateEvent](registrar, rig)).
		After[hatchSystem]()
	registrar.Subscribe[prowlSystem](ecs.ToHandler[app.UpdateEvent](registrar, prowl)).
		After[rigSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		Before[scene.RecordOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[reapSystem]()
	// The snapshot of a tick is taken inside gfx's present, so the handler
	// that collects it and arms the next runs after present, at the very end
	// of the tick: the arm lands between two ticks, and the next tick answers
	// it.
	registrar.Subscribe[rearmHandler](rearm).Last().After[gfx.PresentOnUpdate]()
	registrar.HandleCommand[HUDCmd](ecs.ToExecute[HUDRequest, Shown](registrar, readHUD))

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	return nil
}

// setViewport fits the logical screen inside the window.
func setViewport() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
	var setDesiredViewport func(kernel.Kernel, gfx.SetDesiredViewportRequest) gfx.SetDesiredViewportResponse
	return func(access kernel.ResourceAccess) {
			setDesiredViewport = access.Uses[gfx.SetDesiredViewportCmd]()
		}, func(k kernel.Kernel, event app.WindowSizeChangeEvent) {
			if event.Width <= 0 || event.Height <= 0 {
				return
			}
			width, height := float32(ScreenWidth), float32(ScreenHeight)
			if event.Height > event.Width {
				width, height = height, width
			}
			setDesiredViewport(k,
				gfx.SetDesiredViewportRequest{Mode: gfx.ViewportFit, Width: width, Height: height})
		}
}
