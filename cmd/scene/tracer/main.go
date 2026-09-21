// Command tracer is the scene plugin's first pixel: one camera, one box, no
// lighting. It is the narrowest complete path through every layer — record,
// flush, pack instances, declare a gfx pass, draw — and it exists to be looked
// at, because a projection sign error is exactly the bug an assertion written
// by the author of the projection will happily confirm.
//
//	go run ./cmd/scene/tracer
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
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

// cameraMain sits below canvas's layer 0, which is where a scene camera goes
// when 2D draws over it.
const cameraMain scene.CameraID = -100

func main() {
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: scene tracer"),
	}
	permanentfs.Configure(config)

	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		sceneplugin.New(),
		gogpuplugin.New(),
		newTracer(),
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

const Name kernel.PluginName = "tracer"

type updateEventHandler kernel.Subscription[app.UpdateEvent]

// tracer records the same frame forever, except for the spin, which is there to
// show that every face of the cube is where it should be.
type tracer struct{ time float32 }

func newTracer() *tracer { return &tracer{} }

func (p *tracer) Name() kernel.PluginName { return Name }

func (p *tracer) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, input.Name, scene.Name, storage.Name}
}

func (p *tracer) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[updateEventHandler](p.draw)
	return nil
}

var clearColor = m.NewColorSrgb(0.06, 0.07, 0.09, 1)

func (p *tracer) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*scene.OpQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) {
			p.time += float32(event.Dt)
			q := queue.Get()
			q.Camera(cameraMain, scene.CameraDescr{
				Transform: m.LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
				FovY:      1.0472,
				Near:      0.1,
				Far:       100,
				// A box is lit, so a camera with no sun and no ambient renders
				// it black. These two fields are the floor of lighting.
				SunDirection:  m.Vec3{X: -0.3, Y: -1, Z: -0.2},
				SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
				AmbientSky:    m.NewColorSrgb(0.18, 0.22, 0.3, 1),
				AmbientGround: m.NewColorSrgb(0.1, 0.09, 0.08, 1),
				Passes:        []scene.Pass{{ClearColor: m.Some(clearColor), ClearDepth: m.Some(clearDepth)}},
			})
			q.Box(0, m.At(0, 0, 0).WithRotation(m.QuatAxisAngle(m.Vec3{Y: 1}, p.time)),
				m.NewColorSrgb(0.42, 0.71, 0.94, 1))
			// A second, smaller box behind the first: depth testing is only
			// visible when something can be behind something else.
			q.Box(0, m.At(-1.2, 0, -1.2).WithScale(0.6), m.NewColorSrgb(0.94, 0.55, 0.35, 1))
		}
}

// clearDepth is the far plane. Clearing to zero would clear to the near plane
// and hide the whole scene.
const clearDepth float32 = 1
