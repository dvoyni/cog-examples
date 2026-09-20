// Command physics2dtable is ecsphysics2d's second demo: a top-down table with a
// hundred balls breaking inside four cushions, a crate dropped wherever the
// mouse is clicked, and space to break the table again.
//
//	go run ./cmd/ecs/physics2dtable
//	go run ./cmd/ecs/physics2dtable -seed 12345
//
// What the hands do:
//
//	space   re-deal every ball a fresh direction, speed and spin — the same deal
//	        the opening rack was dealt from, off the same stream. It is what the
//	        cloth is for: the table stops in about twenty seconds and this starts
//	        it again. Once per press and not once per tick held, on any tick and
//	        not only on a table that has stopped, and the balls alone — a crate
//	        is an intruder a click added and is four times a ball's mass, which
//	        is over the speed ceiling the cushions are cut for. See rebreak.
//	click   drop a crate at the cursor.
//
// Its sibling cmd/ecs/physics2d shows what the solver does under gravity. This
// one shows what it does with none, which is the more interesting half:
//
//	**the port ships no gravity, so a top-down game writes none**. There is no
//	weigh System here and no Force is written anywhere in this program. The
//	whole of "top down" is that absence — not a flag, not a Config field, not a
//	plane to choose. A Force Component is still on every Dynamic body, because
//	a Dynamic body without one falls out of the velocity integrator's Query and
//	silently never moves, and it stays zero from the first tick to the last.
//
// That survives space, and deliberately: a break is a kick rather than a
// sustained push, so both breaks are written as a Velocity. A Force would be
// about 300 N for a 5 m/s kick on a 1 kg ball at this step, and it would arrive
// a tick late. dealBall is the one place either break is dealt and says so at
// length.
//
// What the eyes are asked is one sentence, and it is the same sentence
// physics2dtable_test.go asserts:
//
//	nothing leaves the table, nothing sinks into anything, and the break comes
//	to rest.
//
// What the scene is:
//
//	cushions  four Static boxes on the screen edges, overlapping at the corners
//	          so the box is closed. Boxes rather than segments deliberately: the
//	          step is discrete for everything that is not a Sensor, so what keeps
//	          a fast ball from tunnelling is the depth of the thing it is aimed
//	          at. A segment is zero-thickness and a ball that crosses it inside
//	          one tick meets nothing at all; a 0.5 m cushion is four ticks deep
//	          at the fastest speed anything here is allowed to reach, which is
//	          the margin, and it is quoted and asserted rather than hoped for.
//	balls     a hundred Dynamic circles on a jittered lattice, each dealt a
//	          random direction, speed and spin. Restitution 0.98 and Friction
//	          0.05 apiece — and a pair's values are the plain products of the
//	          two Shapes', so ball on ball bounces at 0.9604 and slides at
//	          0.0025, which is what reads as snooker.
//	crates    one 0.7 m box per click, dropped at the cursor. Heavier than a
//	          ball, dull rather than bouncy, and clamped inside the cushions by
//	          its own half-diagonal so a click on the cushion — or outside the
//	          window — never puts a Body inside a wall.
//
// What stops the table is Damping and nothing else, because there is no floor
// under it to rub on. Damping is per Body, a rate in 1/s, and integration is
// exponential in it: v ← v·exp(−damping·h). 0.6/s takes the fastest ball this
// break deals from 5 m/s to a thirtieth of a millimetre a second in twenty
// seconds, which is the cloth.
//
// Every break is dealt from one seeded stream — the opening rack and each press
// of space alike, in that order — and the seed is on the HUD and on stdout.
// -seed takes another, so a run that shows something wrong is reported by its
// number and reproduced by it. The default is a constant and the stream is the
// only source of randomness in the program, so step N of a default run in which
// space was pressed on the same ticks is the same frame on every machine, as the
// sibling demos are. A click draws nothing from the stream, so where the crates
// went does not move the balls off the sequence they would have had.
//
// What is on screen is the Shapes themselves and nothing else: every Body is its
// own outline, straight off the Component, with a spoke on each ball so a
// turning one looks like it is turning. The HUD prints the three clauses above
// beside the numbers that decide them, so what an eye reads and what a test
// reads are the same figures.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/ecsphysics2dplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/extensions/gogpu"
	"github.com/dvoyni/cog/extensions/gogpu/gogpuplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/config"
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
	seed := flag.Uint64("seed", DefaultSeed,
		"the seed the break is dealt from; a run is reported and reproduced by this number")

	cfg := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: ecsphysics2d table"),
		ecs.Name:     ecs.Config{PrewarmEntities: prewarmEntities},
		// The solver's own settings are left at their documented defaults: the
		// Slop is 0.005 m, the solver takes 10 Iterations, and both index cell
		// sizes are 2 m. Every tolerance this demo quotes is quoted against
		// them, so naming one here would move the goalposts. The world is
		// scaled to suit them rather than the other way round — a ball's
		// 0.18 m radius is thirty-six Slops, and the 15 by 8 m cloth is seven
		// and a half cells by four.
		ecsphysics2d.Name: ecsphysics2d.Config{},
	}
	permanentfs.Configure(cfg)
	// The engine's own arguments are taken out of os.Args here, so the -seed
	// below still parses next to a --cog.gogpu.Width=640. That is why the
	// cfg is built before the flags are read rather than after: Inject has
	// to see every plugin an override may name, and flag.Parse has to run
	// after Inject has taken what is not its business.
	cfg = config.Inject(cfg)

	flag.Parse()
	// On stdout as well as on the HUD: a screenshot carries the HUD and a bug
	// report pasted from a terminal carries this.
	fmt.Printf("physics2dtable: seed %#016x\n", *seed)

	engine := kernel.New(cfg).WithPlugins(
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(),
		gfxplugin.New(), canvasplugin.New(), gogpuplugin.New(),
		ecsplugin.New(), ecsphysics2dplugin.New(), New(*seed),
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
const Name kernel.PluginName = "physics2dtable"

// prewarmEntities is how many Entities the world reserves room for up front. It
// is a hint and not a limit: the scene starts at four cushions and a hundred
// balls, and every click after that adds one.
const prewarmEntities = 256

// Demo is the gameplay plugin. Its state is the scratch the drawing System
// refills each frame; everything the scene is lives in Components, and
// everything the census is lives in the Table Resource.
type Demo struct {
	// seed is what the break is dealt from, handed to the Table Resource at
	// registration so the stream is the Resource's and not the plugin's.
	seed uint64
	// verts is the run the drawing System copies a Polygon's vertices into
	// before projecting them. It lives here rather than in the frame because a
	// local would be nil at the top of every frame and would allocate on every
	// one of them.
	verts []m.Vec2d
}

// New builds the demo plugin for one seed.
func New(seed uint64) *Demo { return &Demo{seed: seed} }

// Name reports the plugin name.
func (p *Demo) Name() kernel.PluginName { return Name }

// Dependencies names canvas, because the drawing System holds its queue;
// ecsphysics2d, because every System here locks a Store physics owns; input and
// gfx, because the click System reads the pointer out of one and the viewport it
// arrived in out of the other.
func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, ecs.Name, ecsphysics2d.Name, gfx.Name, input.Name}
}

// The demo's own Systems. There is deliberately no fifth one writing gravity:
// see the package doc.
type (
	// rackSystem builds the table and deals the break once, on the init event.
	rackSystem kernel.Subscription[app.InitEvent]
	// dropSystem turns a click into a crate.
	dropSystem kernel.Subscription[app.UpdateEvent]
	// rebreakSystem re-deals every ball's Velocity when space is pressed.
	rebreakSystem kernel.Subscription[app.UpdateEvent]
	// surveySystem reads the settled world back into the census, once the
	// solver has finished with the tick.
	surveySystem kernel.Subscription[app.UpdateEvent]
	// drawSystem records the world and the HUD into canvas.
	drawSystem kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

// Register declares the demo's one Component, publishes the census and chains
// the three update Systems against physics' own.
func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Piece](registrar, prewarmEntities)
	registrar.InitResource(&Table{random: p.seed, Census: Census{Seed: p.seed}})

	registrar.Subscribe[rackSystem](ecs.ToHandler[app.InitEvent](registrar, rack))

	// The click is read after input has promoted this tick's edges and the
	// crate is spawned before anything integrates, so a Body created this tick
	// is indexed, detected and solved on the tick it was asked for rather than
	// appearing a frame late inside whatever it landed on.
	//
	// It costs the frame its parallelism, and that is worth saying plainly: an
	// ecs.Spawn handle declares write{*Entities}, which is a total barrier, and
	// the lock set is static — so the barrier is paid on every tick and not
	// only on the ticks something is clicked. Moving the spawn behind a Command
	// would not change it, because a Command's handler declares the same set.
	// It is the right price here (one demo, one click) and it is the reason
	// this System does nothing but read the pointer and spawn.
	registrar.Subscribe[dropSystem](ecs.ToHandler[app.UpdateEvent](registrar, drop)).
		After[input.AdvanceOnUpdate]().Before[ecsphysics2d.IntegrateOnUpdate]()

	// The re-break is ordered exactly as the click is, and for the same two
	// reasons: After input's advance, so JustPressed is this tick's edge and a
	// held key is one break rather than sixty; Before Integrate, so the
	// velocities it deals are the ones this tick moves rather than the next.
	//
	// It costs the frame nothing, unlike the click beside it. It writes the
	// Velocity Store and the Table Resource and spawns nothing, so it is an
	// ordinary parallel System that happens to be excluded by the barrier drop
	// already pays.
	registrar.Subscribe[rebreakSystem](ecs.ToHandler[app.UpdateEvent](registrar, rebreak)).
		After[input.AdvanceOnUpdate]().Before[ecsphysics2d.IntegrateOnUpdate]()

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
//
// It is also half of the click: the pointer arrives in window pixels and the
// table is laid out in the logical screen this fit produces, so what undoes the
// fit is the Viewport this writes. See pointerWorld.
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
