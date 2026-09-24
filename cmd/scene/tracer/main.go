// Command tracer is the scene plugin's first pixel: one camera, one spinning
// box and a smaller one behind it, no lights beyond the camera's own sun and
// ambient. It is the narrowest complete path through every layer - spawn the
// Entities, let scene turn them into draws, pack instances, declare a gfx
// pass, draw - and it exists to be looked at, because a projection sign error
// is exactly the bug an assertion written by the author of the projection
// will happily confirm.
//
//	go run ./cmd/scene/tracer
//
// The boxes are Meshes rather than debug shapes, because a debug shape is
// drawn unlit, its colour straight out of the fragment stage: a spinning box of
// one flat colour shows its silhouette and nothing of which face is which. A
// Mesh drawn with the bundled PBR is lit by the camera's sun and ambient, so
// every face of the spinning box reads apart from its neighbours.
//
// The Component the demo declares is in components.go, and each System sits in
// the file named for it:
//
//	setupSystem  bakes the box mesh and spawns the camera and both boxes  (init)
//	spinSystem   turns the front box about Y at a radian a second         (update)
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/input"
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
)

// CameraMain sits below canvas's layer 0, which is where a scene camera goes
// when 2D draws over it.
const CameraMain scene.CameraID = -100

func main() {
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: scene tracer"),
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
const Name kernel.PluginName = "tracer"

// Tracer is the demo's gameplay plugin: it registers the Spin Component and
// the two Systems. Everything it draws is an Entity, and the plugin itself
// holds nothing.
type Tracer struct{}

func New() *Tracer { return &Tracer{} }

func (p *Tracer) Name() kernel.PluginName { return Name }

func (p *Tracer) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name}
}

type (
	setupOnInit  kernel.Subscription[app.InitEvent]
	spinOnUpdate kernel.Subscription[app.UpdateEvent]
)

func (p *Tracer) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Spin](registrar, 1)

	registrar.Subscribe[setupOnInit](ecs.ToHandler[app.InitEvent](registrar, setupSystem))
	// The spin runs Before scene.RecordOnUpdate, the System that turns the
	// world into draws, so a frame draws the box where this frame turned it.
	registrar.Subscribe[spinOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, spinSystem,
		ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))).
		Before[scene.RecordOnUpdate]()
	return nil
}
