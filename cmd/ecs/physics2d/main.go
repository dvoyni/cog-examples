// Command physics2d is ecsphysics2d's one demo: a stack, a ramp and a jointed
// figure in a single scene, so the three halves of the engine are visible at
// once.
//
//	go run ./cmd/ecs/physics2d
//
// Every other verification the port carries answers a question with a number.
// This one answers *does this look like a physics engine*, which only eyes can
// judge, so the deliverable is something to look at. What the eyes are asked is
// one sentence, and it is the same sentence physics2d_test.go asserts:
//
//	the stack rests without buzzing, the ramp behaves at its friction limit,
//	and the figure's limbs stay attached and do not collide with each other.
//
// What each third of the scene is for:
//
//	stack   five boxes spawned a centimetre apart settle into a column and stop
//	        dead. Friction is what makes the column stay a column; without it a
//	        frictionless stack shuffles a few centimetres before it settles.
//	ramp    two crates on the same 30 degree slope, differing only in Friction.
//	        A pair's Friction is the plain product of the two Shapes', and the
//	        rule is an iff: the crate whose pair beats tan 30 never moves at
//	        all, and the one whose pair does not slides at g(sin t - u cos t).
//	figure  eight limbs held by eight pivot Joints and six rotary limits,
//	        hanging from a shapeless Static anchor and swinging. Its limbs
//	        genuinely overlap — each by five centimetres or so of the one it is
//	        jointed to — and still make no Contact, because a Joint that says
//	        CollideBodies false puts its pair into JointedPairs and Detect drops
//	        it before an arbiter exists. Nothing else in the figure overlaps
//	        anything, which is what makes that the mechanism on show rather
//	        than a spacing trick.
//
// Gravity is a Constant: physics registers ecsphysics2d.Constants with gravity
// 0, and the setup System writes Constants.Gravity once, on the init event,
// through ecs.Write[*ecsphysics2d.Constants]. Nothing writes it afterwards, so
// no System of the demo's waits on Solve for it on any tick. Solve adds it to
// every Dynamic body's velocity exactly as a Force of m*g would be added: this
// tick's Solve turns it into velocity and the next Integrate spends it, so the
// closed forms this demo quotes count that increment in, not out.
//
// What is on screen is the Shapes themselves and nothing else: every Body is
// its own outline, straight off the Component, with a red cross at each pivot
// and a red line between a pivot's two anchors when it is being stretched. The
// HUD prints the three clauses above beside the numbers that decide them, so
// what an eye reads and what a test reads are the same figures.
//
// There is no input and no clock but the fixed step, so step N is the same
// frame on every machine. physics2d_test.go drives the same composition through
// internal/headless and reads the census this demo's own HUD draws.
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/ecsphysics2dplugin"
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

	"github.com/dvoyni/cog-examples/internal/permanentfs"
)

func main() {
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: ecsphysics2d"),
		ecs.Name:     ecs.Config{PrewarmEntities: prewarmEntities},
		// The solver's own settings are left at their documented defaults. The
		// Slop is 0.005 m and every tolerance quoted in this demo is quoted
		// against it, so naming one here would move the goalposts.
		ecsphysics2d.Name: ecsphysics2d.Config{},
	}
	permanentfs.Configure(config)

	engine := kernel.New(config).WithPlugins(
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(),
		gfxplugin.New(), canvasplugin.New(), gogpuplugin.New(),
		ecsplugin.New(), ecsphysics2dplugin.New(), New(),
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
const Name kernel.PluginName = "physics2d"

// prewarmEntities is how many Entities the world reserves room for up front. It
// is a hint and not a limit: the scene is twenty-odd Bodies and a dozen Joints,
// and nothing is ever spawned after setup.
const prewarmEntities = 128

// Demo is the gameplay plugin. Its state is the scratch the drawing System
// refills each frame; everything the scene is lives in Components, and
// everything the census is lives in the Scene Resource.
type Demo struct {
	// verts is the run the drawing System copies a Polygon's vertices into
	// before projecting them. It lives here rather than in the frame because a
	// local would be nil at the top of every frame and would allocate on every
	// one of them.
	verts []m.Vec2d
}

// New builds the demo plugin.
func New() *Demo { return &Demo{} }

// Name reports the plugin name.
func (p *Demo) Name() kernel.PluginName { return Name }

// Dependencies names canvas, because the drawing System holds its queue, and
// ecsphysics2d, because every System here locks a Store physics owns.
func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, ecsphysics2d.Name}
}

// The demo's own Systems.
type (
	// setupSystem builds the whole scene once, on the init event.
	setupSystem kernel.Subscription[app.InitEvent]
	// surveySystem reads the settled world back into the census, once the
	// solver has finished with the tick.
	surveySystem kernel.Subscription[app.UpdateEvent]
	// drawSystem records the world and the HUD into canvas.
	drawSystem kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

// Register declares the demo's one Component, publishes the census and chains
// the three Systems against physics' own.
func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Look](registrar, prewarmEntities)
	registrar.InitResource(&Scene{})

	// Setup also writes the world's gravity, once: it is the one System that
	// takes Write on Constants, and it runs on the init event, so it serialises
	// against Solve on no tick at all.
	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))

	// The census is a reacting System — cp's PostSolve — so it reads the tick
	// as the solver left it rather than as Detect found it.
	registrar.Subscribe[surveySystem](ecs.ToHandler[app.UpdateEvent](registrar, survey)).
		After[ecsphysics2d.SolveOnUpdate]()

	registrar.Subscribe[drawSystem](ecs.ToHandler[app.UpdateEvent](registrar, p.draw)).
		After[surveySystem]().Before[canvas.FlushOnUpdate]()

	registrar.HandleCommand[CensusCmd](ecs.ToExecute[CensusRequest, Census](registrar, readCensus))

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
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
