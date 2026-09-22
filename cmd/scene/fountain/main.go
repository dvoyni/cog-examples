// Command fountain is cmd/ecs/fountain's frame drawn through scene.OpQueue: a
// nozzle throwing motes into a basin while a fox circles it, every drawable one
// scene call a step.
//
//	go run ./cmd/scene/fountain
//
// The two fountains share internal/fountain - the spray, the clock, the random
// stream, the layout, the geometry and shaders, and the HUD's text - so a
// difference between their frames can only be a difference between the
// renderers. This program keeps only its recording:
//
//	the motes     a plain slice, each mote one Mesh call with its own tint in Params
//	the basin     one Mesh call whose material serves two pass tags, ground and forward
//	the nozzle    one Model call (WaterBottle)
//	the fox       one Model call, its walk and run blended by its speed
//	the lights    a point lamp over the nozzle, and a spot following the fox
//	the camera    one camera orbiting on the demo clock, its two passes clearing via m.Maybe
//
// There is no input. Demo time is fixed steps and the spray's random stream is
// seeded by a constant, so step N is the same frame on every machine.
// reference.png is step fountain.ReferenceStep, taken with mcpplugin.New() added
// to the plugin list and docs/narrowing/capture.py; fountain_test.go asserts the
// frame at that step, and that its HUD reads as the image's does.
//
// The HUD's frame figures are read off gfx's frame snapshot, exactly as
// cmd/ecs/fountain reads them. The HUD holds gfx's one snapshot slot every tick,
// so an agent asking for a snapshot of its own while this runs is refused as
// busy.
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
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
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

const (
	CameraMain scene.CameraID = fountain.CameraID
	layerHUD   canvas.Layer   = 0
)

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.WithTitle("cog examples: scene fountain"),
	}
	permanentfs.Configure(config)

	engine := kernel.New(config).WithPlugins(
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(), gfxplugin.New(),
		canvasplugin.New(), modelplugin.New(), sceneplugin.New(), gogpuplugin.New(), New(),
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

// Name is the demo plugin's name.
const Name kernel.PluginName = "fountain"

type (
	setupHandler  kernel.Subscription[app.InitEvent]
	stepHandler   kernel.Subscription[app.UpdateEvent]
	rearmHandler  kernel.Subscription[app.UpdateEvent]
	resizeHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (p *Fountain) Name() kernel.PluginName { return Name }

func (p *Fountain) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, model.Name, scene.Name}
}

func (p *Fountain) Register(registrar *kernel.Registrar, _ any) error {
	// The nozzle and the fox are vendored models, and storage mounts nothing
	// by default, so the demo contributes the asset set itself.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	registrar.Subscribe[setupHandler](p.setup)
	registrar.Subscribe[stepHandler](p.step)
	// The snapshot of a tick is taken inside gfx's present, so the handler
	// that collects it and arms the next runs after present, at the very end
	// of the tick: the arm lands between two ticks, and the next tick answers
	// it.
	registrar.Subscribe[rearmHandler](p.rearm).Last().After[gfx.PresentOnUpdate]()
	registrar.Subscribe[resizeHandler](setViewport)
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
