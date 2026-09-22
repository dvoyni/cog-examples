// Command fountain is the ecsscene showcase: a nozzle throwing motes into a
// basin while a fox circles it, every drawable an Entity and every Component of
// ecsscene doing its one job. ecsscene is the renderer here: scene is not
// composed, because an app runs one or the other.
//
//	go run ./cmd/ecs/fountain
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
// There is no input. Demo time is fixed steps and the spray's random stream is
// seeded by a constant, so step N is the same frame on every machine.
// reference.png is step fountain.ReferenceStep, taken with mcpplugin.New() added
// to the plugin list and docs/narrowing/capture.py; fountain_test.go asserts the
// frame at that step, and that its HUD reads as the image's does.
//
// cmd/scene/fountain draws the same frame through scene.OpQueue, one call a
// mote, where ecsscene batches the motes whose tints are equal. The two share
// internal/fountain - the spray, the clock, the random stream, the layout, the
// geometry and shaders, the HUD's text and the figures the reference step is
// expected to have - and this program keeps only its recording: the Entities,
// their Components and the Systems above.
//
// The HUD's frame figures are read off gfx's frame snapshot, exactly as
// cmd/scene/fountain reads them. The HUD holds gfx's one snapshot slot every
// tick, so an agent asking for a snapshot of its own while this runs is refused
// as busy.
//
// Why ecsscene is shaped the way it is lives in its README:
// https://github.com/dvoyni/cog/blob/main/bundles/ecsscene/docs/README.md
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/ecsscene/ecssceneplugin"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
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

	"github.com/dvoyni/cog-examples/internal/assets"
)

const (
	CameraMain ecsscene.CameraID = fountain.CameraID
	layerHUD   canvas.Layer      = 0
)

// prewarmEntities is how many Entities the world reserves room for up front,
// which is a hint and not a limit: the spray settles near a hundred and twenty
// motes, plus six Entities of furniture.
const prewarmEntities = 512

// peakMotes is the population the mote Stores reserve for.
const peakMotes = 256

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.WithTitle("cog examples: ecs fountain"),
		ecs.Name:   ecs.Config{PrewarmEntities: prewarmEntities},
	}
	permanentfs.Configure(config)

	engine := kernel.New(config).WithPlugins(
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(), gfxplugin.New(), canvasplugin.New(), modelplugin.New(), gogpuplugin.New(),
		ecsplugin.New(), ecssceneplugin.New(), New(),
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

// The Component sets: one per act of creation.
type (
	mote struct {
		Place  m.Transform
		Draw   ecsscene.Mesh
		Shade  ecsscene.Material
		Tint   ecsscene.Params
		Motion fountain.Velocity
		Age    fountain.Life
	}
	nozzle struct {
		Place m.Transform
		Model ecsscene.Model
	}
	fox struct {
		Place m.Transform
		Model ecsscene.Model
		Gait  model.ClipMachine
		Pose  ecsscene.Animation
	}
	basin struct {
		Place m.Transform
		Draw  ecsscene.Mesh
		Shade ecsscene.Material
	}
	lamp struct {
		Place m.Transform
		Light ecsscene.Light
	}
	eye struct {
		Place  m.Transform
		Camera ecsscene.Camera
	}
)

type Demo struct{}

func New() *Demo { return &Demo{} }

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, ecsscene.Name, gfx.Name, model.Name, storage.Name}
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

	ecs.RegisterComponent[fountain.Velocity](registrar, peakMotes)
	ecs.RegisterComponent[fountain.Life](registrar, peakMotes)
	// The fox's gait machine is a Component of its own, beside the Animation
	// it fills: one fox, so one row.
	ecs.RegisterComponent[model.ClipMachine](registrar, 1)
	registrar.InitResource(&Fountain{spray: fountain.NewSpray()})

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	registrar.Subscribe[hatchSystem](ecs.ToHandler[app.UpdateEvent](registrar, hatch)).First()
	registrar.Subscribe[accelerateSystem](ecs.ToHandler[app.UpdateEvent](registrar, accelerate))
	// Every System that moves or retires an Entity runs Before ecsscene's
	// recording System, so a step draws the world as that step left it rather
	// than whichever side of the tie the scheduler happened to break.
	registrar.Subscribe[driftSystem](ecs.ToHandler[app.UpdateEvent](registrar, drift)).
		After[accelerateSystem]().Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[reapSystem](ecs.ToHandler[app.UpdateEvent](registrar, reap)).
		After[driftSystem]().Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[rigSystem](ecs.ToHandler[app.UpdateEvent](registrar, rig)).
		After[hatchSystem]()
	registrar.Subscribe[prowlSystem](ecs.ToHandler[app.UpdateEvent](registrar, prowl)).
		After[rigSystem]().Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		Before[ecsscene.RecordOnUpdate]()
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
			width, height := float32(fountain.ScreenWidth), float32(fountain.ScreenHeight)
			if event.Height > event.Width {
				width, height = height, width
			}
			setDesiredViewport(k,
				gfx.SetDesiredViewportRequest{Mode: gfx.ViewportFit, Width: width, Height: height})
		}
}
