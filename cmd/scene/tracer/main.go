// Command tracer is the scene plugin's first pixel: one camera, one box, no
// lighting. It is the narrowest complete path through every layer — record,
// flush, pack instances, declare a gfx pass, draw — and it exists to be looked
// at, because a projection sign error is exactly the bug an assertion written
// by the author of the projection will happily confirm.
//
//	go run ./cmd/scene/tracer
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
	"github.com/dvoyni/cog/wgpu"
)

// cameraMain sits below canvas's layer 0, which is where a scene camera goes
// when 2D draws over it.
const cameraMain scene.CameraID = -100

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-examples"),
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: scene tracer"),
	}

	plugins := []kernel.Plugin{
		storage.New(),
		input.New(),
		gfx.New(),
		scene.New(),
		wgpu.New(),
		newTracer(),
	}

	kernel.New(config).WithPlugins(plugins...).Run(ctx)
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
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.time += float32(event.Dt)
			q := queue.Get()
			q.Camera(cameraMain, scene.CameraDescr{
				Transform: scene.LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
				FovY:      1.0472,
				Near:      0.1,
				Far:       100,
				Passes:    []scene.Pass{{ClearColor: &clearColor, ClearDepth: clearDepth()}},
			})
			q.Box(0, scene.At(0, 0, 0).WithRotation(m.QuatAxisAngle(m.Vec3{Y: 1}, p.time)),
				m.NewColorSrgb(0.42, 0.71, 0.94, 1))
			// A second, smaller box behind the first: depth testing is only
			// visible when something can be behind something else.
			q.Box(0, scene.At(-1.2, 0, -1.2).WithScale(0.6), m.NewColorSrgb(0.94, 0.55, 0.35, 1))
			return nil
		}
}

// clearDepth is the far plane. Clearing to zero would clear to the near plane
// and hide the whole scene.
func clearDepth() *float32 {
	far := float32(1)
	return &far
}
