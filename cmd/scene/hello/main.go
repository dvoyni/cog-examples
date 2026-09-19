// Command hello is the smallest cog application: it opens a window through the
// storage, input, gfx, canvas and gogpu plugins and draws one canvas rectangle.
// It exists to prove that this module builds and runs against the sibling cog
// checkout, so the scene prototypes that follow start from a known-good app.
//
//	go run ./cmd/scene/hello
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
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

// Logical viewport the demo draws in. The window is fitted to it, so the
// rectangle keeps its proportions at any window size.
const (
	screenWidth  = 800
	screenHeight = 600
)

// layerHello is the only canvas layer this demo records into.
const layerHello canvas.Layer = 0

func main() {
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: hello"),
	}
	permanentfs.Configure(config)

	// hello is last because it records into the op queue the plugins before it
	// declare.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		gogpuplugin.New(),
		newHello(),
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

// draw records the whole frame: a dark clear plus one centred rectangle.
func draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*canvas.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*canvas.OpQueue]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) {
			q := queue.Get()
			q.Clear(layerHello, m.NewColorSrgb(0.06, 0.07, 0.09, 1))
			q.SetLayerTransform(layerHello,
				m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)
			q.FillRect(layerHello, m.Rect{
				X: screenWidth/2 - 120, Y: screenHeight/2 - 80,
				Width: 240, Height: 160,
			}, canvas.ShapeDraw{Color: m.NewColorSrgb(0.42, 0.71, 0.94, 1)})
		}
}
