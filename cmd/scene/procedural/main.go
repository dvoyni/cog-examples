// Command procedural is scene's caller-owned-geometry demo: every shape in the
// frame but the ground and the reference sphere is built by this program, out
// of a vertex type scene has never seen, and drawn as a Mesh Entity with a
// WGSL Material this program wrote. There are no assets, and the shader is a Go
// string rather than a file, so the whole demo is one `go run` with nothing
// mounted.
//
//	go run ./cmd/scene/procedural
//
// custom-shader is merged into this demo rather than dropped: a custom vertex
// layout *requires* a custom material - the bundled PBR knows the two named
// layouts and nothing else - so the two cannot be demonstrated apart.
//
// What it exercises: BakeMesh and the generic VertexLayout; UpdateMesh at a
// changing size; ReleaseMesh and the generations that make a stale ref
// detectable; Mesh.Bounds and Mesh.NeverCull; a Material tag of inline WGSL
// over the static variant, which every Mesh draws; and, one layer down, gfx's
// NewBuffer, UploadBuffer and ReleaseBuffer - which is what the three mesh
// calls are, applied by scene's load System when it drains model's queues:
//
//	BakeMesh    -> gfx.UploadBuffer(NewBuffer())  a durable buffer, uploaded once
//	UpdateMesh  -> gfx.UploadBuffer               the same buffer id, new bytes, any length
//	ReleaseMesh -> gfx.ReleaseBuffer              freed at the frame boundary
//
// # The objects
//
// The ground and the reference sphere are the demo's two bundled-PBR draws:
// model.Vertex meshes with no Material, tinted through Params. They are here to
// be compared against. A plane has one normal, so it can show that the sun
// reaches the frame but not which way it points; the sphere carries a
// terminator, which is the thing a custom material can get wrong.
//
// Everything else is caller-owned geometry drawn with the caller's material.
// The ridge is durable: baked once, then rebuilt and re-baked every frame
// through UpdateMesh, changing resolution as it goes. The ribbon lives one
// frame: baked fresh every frame, the previous frame's released as it is. The
// beacon is durable and short-lived: released and re-baked every beaconPeriod
// steps, and drawn a second time as a stray far behind the camera, which is the
// Entity the culler throws away.
//
// # The Systems
//
//	setup    bakes every mesh and spawns the Entities                 (init)
//	advance  reads the keys and steps the demo clock                  (first)
//	orbit    places the camera on its orbit
//	mint     re-bakes the ridge and the ribbon, and swaps the beacon  (before scene's load)
//	spin     turns the beacon
//	hud      prints what the demo knows, and clears the frame
//
// # The reference pose
//
// The demo starts at a documented fixed pose: the camera orbits orbitTarget at
// radius orbitRadius, azimuth startAzimuth and elevation startElevation, and
// stays there until an arrow key is held. R returns to it exactly, resetting
// the step counter with it. Demo time is accumulated fixed steps, never wall
// clock, so step N is the same frame on every machine and a test drives N steps
// directly.
//
// Input may orbit and pause freely, and touching it voids nothing: the
// assertions live in procedural_test.go rather than in the running app.
//
// # What only eyes can judge
//
// The reference sphere's bright side and the ridge's lit crests face the same
// way, and both fall to the same cool ambient where they turn away from it -
// even though the sphere is shaded by the bundled PBR and the ridge by a shader
// this program wrote.
//
// One sentence, two failures: a normal built from the instance record the wrong
// way round makes the beacon's shading swim as it spins while the ground stays
// put; and a material that declared a binding nothing fills draws nothing, with
// gfx.ErrStorageBufferUnsupplied reported for it. A third it used to catch - a
// hand-copied SceneFrame with a field out of place, lighting the ridge from a
// direction the ground disagrees with - is gone: the material includes
// model.PbrPath and lights through the engine's own sceneShadeSurface.
//
// The ribbon's two faces are lit as one surface - no seam runs along the band
// where its front side meets its back - and the beacon changes colour every
// three seconds without ever showing two of itself.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
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

// The logical screen the HUD is laid out in. The window is fitted to it, so the
// HUD keeps its proportions at any window size.
const (
	screenWidth  = 960
	screenHeight = 540
)

// CameraMain is the demo's only camera, at a negative id so it sorts below the
// HUD and above the backdrop that clears the frame.
const CameraMain scene.CameraID = -100

// The canvas layers, which are gfx orders directly. The camera declares no
// passes, and its default pass clears depth but keeps colour, so the frame's
// one colour clear is canvas's, on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

func main() {
	config := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		gogpu.Name:   gogpu.Config{}.WithTitle("cog examples: scene procedural"),
	}
	permanentfs.Configure(config)

	demo := New()
	engine := kernel.New(config).Handler(demo.report).WithPlugins(
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
		modelplugin.New(),
		gogpuplugin.New(),
		ecsplugin.New(), sceneplugin.New(),
		demo,
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
const Name kernel.PluginName = "procedural"

// Demo is the demo's plugin. It keeps the one resource its Systems share, so
// the error handler - which is not a System and takes no lock - can read the
// one atomic field it needs out of it.
type Demo struct {
	state *Procedural
}

// New builds the demo plugin at its documented starting pose.
func New() *Demo { return &Demo{state: newProcedural()} }

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{
		canvas.Name, ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name,
	}
}

type (
	setupSystem   kernel.Subscription[app.InitEvent]
	advanceSystem kernel.Subscription[app.UpdateEvent]
	orbitSystem   kernel.Subscription[app.UpdateEvent]
	mintSystem    kernel.Subscription[app.UpdateEvent]
	spinSystem    kernel.Subscription[app.UpdateEvent]
	hudSystem     kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(p.state)

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	// Input is read before anything moves, so a held arrow moves the camera on
	// the very frame it is pressed.
	registrar.Subscribe[advanceSystem](ecs.ToHandler[app.UpdateEvent](registrar, advance)).First()
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		After[advanceSystem]().Before[scene.RecordOnUpdate]()
	// mint writes Mesh Components, so it runs Before scene's load System: a
	// ref it baked this step is keyed, uploaded and drawn this step.
	registrar.Subscribe[mintSystem](ecs.ToHandler[app.UpdateEvent](registrar, mint)).
		After[advanceSystem]().Before[scene.LoadOnUpdate]()
	registrar.Subscribe[spinSystem](ecs.ToHandler[app.UpdateEvent](registrar, spin)).
		After[advanceSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[mintSystem]().After[orbitSystem]()

	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	return nil
}

// report is the demo's error handler, and it is here because this demo reports
// an error on purpose.
//
// The kernel's default handler logs and returns true, which terminates the
// engine: a cog app that reports anything at all stops, and that is the right
// default, because most reports are bugs. The one report this demo means to
// provoke - a draw of the ref its own ReleaseMesh invalidated - would therefore
// shut the window three seconds after it opened.
//
// So exactly that report is logged and survived, matched by the mesh id the
// last release staled, and everything else still terminates. A demo that
// swallowed the whole class would be a demo that cannot tell you its material
// stopped binding.
func (p *Demo) report(err error) error {
	var unavailable model.ErrMeshUnavailable
	if errors.As(err, &unavailable) && unavailable.Mesh == p.state.staleID.Load() {
		log.Printf("procedural: %v (expected: the released ref is drawn once on purpose)", err)
		return nil
	}
	log.Printf("procedural: %v", err)
	return err
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
