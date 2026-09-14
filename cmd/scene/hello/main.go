// Command hello is the smallest cog application: it opens a window through the
// storage, input, gfx, canvas and wgpu plugins and draws one canvas rectangle.
// It exists to prove that this module builds and runs against the sibling cog
// checkout, so the scene prototypes that follow start from a known-good app.
//
//	go run ./cmd/scene/hello
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasimpl"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputimpl"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gfximpl"
	"github.com/dvoyni/cog/extensions/wgpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// Logical viewport the demo draws in. The window is fitted to it, so the
// rectangle keeps its proportions at any window size.
const (
	screenWidth  = 800
	screenHeight = 600
)

// layerHello is the only canvas layer this demo records into.
const layerHello canvas.Layer = 0

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: hello"),
	}

	// hello is last because it records into the op queue the plugins before it
	// declare.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputimpl.New(),
		gfximpl.New(),
		canvasimpl.New(),
		wgpu.New(),
		newHello(),
	}

	kernel.New(config).WithPlugins(plugins...).Run(ctx)
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "hello"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// hello is the demo's gameplay plugin. It holds no state: every frame is drawn
// from constants.
type hello struct{}

func newHello() *hello { return &hello{} }

func (p *hello) Name() kernel.PluginName { return Name }

func (p *hello) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, storage.Name}
}

func (p *hello) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](draw)
	return nil
}

// setViewport fits the logical screen inside the window, swapping the axes when
// the window is taller than it is wide.
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

// draw records the whole frame: a dark clear plus one centred rectangle.
func draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*canvas.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*canvas.OpQueue]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			q := queue.Get()
			q.Clear(layerHello, m.NewColorSrgb(0.06, 0.07, 0.09, 1))
			q.SetLayerTransform(layerHello,
				m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)
			q.FillRect(layerHello, m.Rect{
				X: screenWidth/2 - 120, Y: screenHeight/2 - 80,
				Width: 240, Height: 160,
			}, canvas.ShapeDraw{Color: m.NewColorSrgb(0.42, 0.71, 0.94, 1)})
			return nil
		}
}
