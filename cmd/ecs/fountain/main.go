// Command fountain is the ecsscene showcase: a nozzle throwing motes into a
// basin while a fox circles it, every drawable an Entity and every Component of
// cog's ecs-to-scene binding doing its one job.
//
//	go run ./cmd/ecs/fountain
//
//	Transform   every Entity's place; a mote's Vec3 scale stretches it along its velocity  (drift)
//	Mesh        the motes, a unit cube baked once at startup, and the basin disc           (setup, hatch)
//	Params      each mote's own tint, fading with its life                                (drift)
//	Material    the mote shader, and the basin's two pass tags: ground and forward        (material.go, setup, hatch)
//	Model       the nozzle (WaterBottle) and the fox                                      (setup)
//	Animation   the fox's walk and run, blended by its speed                              (prowl)
//	Light       a point lamp over the nozzle, and a spot light following the fox         (setup, prowl)
//	Camera      one camera orbiting on the demo clock, its two passes clearing via m.Maybe (setup, orbit)
//
// There is no input. Demo time is fixed steps and the spray's random stream is
// seeded by a constant, so step N is the same frame on every machine.
// reference.png is step referenceStep, taken with mcpplugin.New() added to the
// plugin list and docs/narrowing/capture.py; fountain_test.go asserts the frame
// at that step, and that its HUD reads as the image's does.
//
// Why the binding is shaped the way it is lives in ecsscene's README:
// https://github.com/dvoyni/cog/blob/main/bundles/ecsscene/README.md
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/ecsscene/ecssceneplugin"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/sceneplugin"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gfximpl"
	"github.com/dvoyni/cog/extensions/wgpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"

	"github.com/dvoyni/cog-examples/internal/assets"
)

const (
	screenWidth  = 960
	screenHeight = 540
)

const (
	CameraMain scene.CameraID = -100
	layerHUD   canvas.Layer   = 0
)

const (
	nozzlePath = "assets/WaterBottle/WaterBottle.glb"
	foxPath    = "assets/Fox/Fox.glb"
)

// prewarmEntities is how many Entities the world reserves room for up front,
// which is a hint and not a limit: the spray settles near a hundred and twenty
// motes, plus six Entities of furniture.
const prewarmEntities = 512

// peakMotes is the population the mote Stores reserve for.
const peakMotes = 256

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	assetConfig, err := assets.Config(storage.Config{})
	if err != nil {
		panic(err)
	}
	config := map[kernel.PluginName]any{
		storage.Name: assetConfig,
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: ecs fountain"),
		ecs.Name:     ecs.Config{PrewarmEntities: prewarmEntities},
	}
	permanentfs.Configure(config)

	kernel.New(config).WithPlugins(
		storageplugin.New(), permanentfs.New(), inputplugin.New(), gfximpl.New(), canvasplugin.New(), sceneplugin.New(), wgpu.New(),
		ecsplugin.New(), ecssceneplugin.New(), New(),
	).Run(ctx)
}

const Name kernel.PluginName = "fountain"

// Velocity is how fast a mote travels, in world units a second.
type Velocity struct{ V m.Vec3 }

// Life is how much longer a mote lasts, and how long it had.
type Life struct{ Remaining, Span float32 }

// The Component sets: one per act of creation.
type (
	mote struct {
		Place  ecsscene.Transform
		Draw   ecsscene.Mesh
		Shade  ecsscene.Material
		Tint   ecsscene.Params
		Motion Velocity
		Age    Life
	}
	nozzle struct {
		Place ecsscene.Transform
		Model ecsscene.Model
	}
	fox struct {
		Place ecsscene.Transform
		Model ecsscene.Model
		Gait  ecsscene.Animation
	}
	basin struct {
		Place ecsscene.Transform
		Draw  ecsscene.Mesh
		Shade ecsscene.Material
	}
	lamp struct {
		Place ecsscene.Transform
		Light ecsscene.Light
	}
	eye struct {
		Place  ecsscene.Transform
		Camera ecsscene.Camera
	}
)

type Demo struct{}

func New() *Demo { return &Demo{} }

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, ecsscene.Name, scene.Name}
}

type (
	setupSystem      kernel.Subscription[app.InitEvent]
	hatchSystem      kernel.Subscription[app.UpdateEvent]
	accelerateSystem kernel.Subscription[app.UpdateEvent]
	driftSystem      kernel.Subscription[app.UpdateEvent]
	reapSystem       kernel.Subscription[app.UpdateEvent]
	prowlSystem      kernel.Subscription[app.UpdateEvent]
	orbitSystem      kernel.Subscription[app.UpdateEvent]
	hudSystem        kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Velocity](registrar, peakMotes)
	ecs.RegisterComponent[Life](registrar, peakMotes)
	registrar.InitResource(&Fountain{random: randomSeed})

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	registrar.Subscribe[hatchSystem](ecs.ToHandler[app.UpdateEvent](registrar, hatch)).First()
	registrar.Subscribe[accelerateSystem](ecs.ToHandler[app.UpdateEvent](registrar, accelerate))
	// Every System that moves or retires an Entity runs Before the binding's
	// recording System, so a step draws the world as that step left it rather
	// than whichever side of the tie the scheduler happened to break.
	registrar.Subscribe[driftSystem](ecs.ToHandler[app.UpdateEvent](registrar, drift)).
		After[accelerateSystem]().Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[reapSystem](ecs.ToHandler[app.UpdateEvent](registrar, reap)).
		After[driftSystem]().Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[prowlSystem](ecs.ToHandler[app.UpdateEvent](registrar, prowl)).
		Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		Before[ecsscene.RecordOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[reapSystem]()
	registrar.HandleCommand[HUDCmd](ecs.ToExecute[HUDRequest, HUD](registrar, readHUD))

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	return nil
}

// setViewport fits the logical screen inside the window.
func setViewport() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
	var setDesiredViewport func(kernel.Kernel, gfx.SetDesiredViewportRequest) (gfx.SetDesiredViewportResponse, error)
	return func(access kernel.ResourceAccess) {
			setDesiredViewport = access.Uses[gfx.SetDesiredViewportCmd]()
		}, func(k kernel.Kernel, event app.WindowSizeChangeEvent) error {
			if event.Width <= 0 || event.Height <= 0 {
				return nil
			}
			width, height := float32(screenWidth), float32(screenHeight)
			if event.Height > event.Width {
				width, height = height, width
			}
			_, err := setDesiredViewport(k,
				gfx.SetDesiredViewportRequest{Mode: gfx.ViewportFit, Width: width, Height: height})
			return err
		}
}
