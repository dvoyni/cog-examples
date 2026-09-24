// Command instancing is scene's instanced-draw demo: a field of crates that is
// one draw however many of them stand, per-instance culling, and what splits a
// Batch made visible.
//
//	go run ./cmd/scene/instancing
//
// It adds no assets. The crates, the bottles and the two glass screens are
// three of pbr's six files, and the ground is a debug shape, because what this
// demo is about is not what a model looks like but how many draw calls a
// thousand of them cost.
//
// Every crate is an Entity of its own, carrying an m.Transform and a Model that
// names the crate file. Nothing groups them: scene keys every drawable Entity
// into a Batch as it changes - the file's primitive, its material and the
// Entity's Params - and draws each Batch as one instanced draw. What the demo
// proves is a number: how many Entities it spawned, how many Batch keys they
// share, and so how many draws that is. The HUD prints the numbers the demo
// decided itself; the draws are the test's to count, at the backend.
//
// What it exercises: explicit per-axis Transform.Scale on Model Entities;
// per-instance culling and the contiguous packing of the survivors;
// SCENE_NONUNIFORM set per instance rather than per Batch; firstInstance and
// the one instance range every draw of a pass binds; the split a blended
// Batch takes; and Params as part of the Batch key.
//
// # The courtyard
//
// A 25 by 25 lattice of crates on 1.8-unit centres, with a 7 by 7 square left
// out of the middle. That is 576 crate Entities - the tile floor the API doc
// names, at the size where the difference between one draw call and five
// hundred is the whole point.
//
// A colonnade stands on the sub-lattice where both indices are 2 mod 6. Those
// crates are stretched into pillars by a per-axis Transform.Scale. They are
// crates like the cubes around them, the same file under the same key, so one
// draw carries both - which is what makes SCENE_NONUNIFORM per instance rather
// than per draw. The courtyard takes the one pillar site that falls inside it;
// the lattice is the rule and the courtyard is the hole, and the hole wins.
//
// Five water bottles stand in the courtyard, every other one squashed into a
// wide low one by the same kind of per-axis Scale, and two glass screens stand in
// front of them at two different depths. The screens are the exception to
// batching: their two BLEND primitives split back into one single-instance
// draw each, because sorting a Batch by its nearest instance would composite
// visibly wrong, while their seven opaque primitives stay seven draws of two.
//
// The squat bottles rather than the pillars are what makes SCENE_NONUNIFORM
// visible, and the reason is worth stating because it is easy to get backwards.
// Every face normal of an axis-aligned box is an eigenvector of an axis-aligned
// scale, so the world matrix and its inverse-transpose send it the same way and
// differ only in length - which normalising removes. A stretched cube therefore
// shades identically with the flag and without it, however non-uniform it is. A
// bottle's shoulder is curved, so its normals are not eigenvectors of anything,
// and there the flag decides where the highlight lands.
//
// A stack of three crates stands in one corner of the courtyard, spawned apart
// from the field and at its own scale. It shares the field's file, material and
// (empty) Params, so it shares the field's Batch key, and its three crates pack
// into the field's one draw beside the field's survivors: a Batch is every
// Entity whose key is equal, not everything one spawn placed.
//
// # One draw or five hundred
//
// Key 1 lets every crate share one Batch key and key 2 gives each crate a key
// of its own, by writing into its Params a value naming its serial. No shader
// declares that parameter, so gfx binds it nowhere and the picture does not
// change, nor does the packed instance array. What changes is the key: the
// field goes from one instanced draw to one draw per surviving crate.
//
// That pair of frames is what batching costs and buys: the same instances,
// byte for byte, in one draw or in five hundred. Equal Params batch, and
// different ones split, whatever the difference is.
//
// # The reference pose
//
// The demo starts at a documented fixed pose - the camera orbits orbitTarget at
// radius overviewRadius, azimuth startAzimuth and elevation startElevation -
// and reference.png beside this file is the frame at that pose.
//
// Nothing in the frame moves on its own. The clock is accumulated fixed steps
// and the HUD prints it, but it drives only the orbit rate, so every frame at
// the reference pose is the same frame and the reference screenshot is retaken
// by launching the demo and capturing it, with no step number to hit. A demo
// whose acceptance is a picture is better off drawing nothing that moves, and
// per-instance culling is a still subject: what makes it visible is the camera
// turning, which is input.
//
// Input may orbit, pause and switch modes freely, and touching it voids
// nothing: the assertions live in instancing_test.go rather than in the running
// app.
//
// # What only eyes can judge
//
// The field reads as a regular grid running out to the edge of the frame with
// even gaps all the way, and the two squashed bottles in the row keep the same
// bright roll of metal highlight round their shoulders as the tall bottles
// standing beside them.
//
// One sentence, two failures. A draw that does not read its own slice of the
// instance range - a firstInstance that is not pass-relative, a bound range
// that starts in the wrong place - piles the field onto one square or shears
// the lattice into a fan, and a grid is the one arrangement where that is
// unmissable. And a squat bottle whose SCENE_NONUNIFORM never reached the
// shader takes its normals through the world matrix instead of the
// inverse-transpose: its highlight goes out entirely and it drops to a dull
// olive disc beside a neighbour that did not change, which is a difference
// between two things in one frame rather than a judgement about one.
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog-examples/internal/assets"
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
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// The logical screen the HUD is laid out in, and the logical size the window
// opens at - the size the reference screenshot is of. The window is launched at
// it rather than resized into it: a runtime resize leaves the viewport
// un-refitted.
const (
	screenWidth  = 960
	screenHeight = 540
	windowWidth  = 1280
	windowHeight = 720
)

// CameraMain is the demo's only camera, at a negative id so it sorts below
// layerHUD and above layerBackdrop.
const CameraMain scene.CameraID = -100

// The canvas layers, which are gfx orders directly. The camera declares no
// passes, and the default pass clears depth but keeps colour, so the frame's
// one colour clear is canvas's on a layer below the camera.
const (
	layerBackdrop canvas.Layer = -200
	layerHUD      canvas.Layer = 0
)

// prewarmEntities is how many Entities the world reserves room for up front,
// which is a hint and not a limit: 579 crates, the courtyard's seven models,
// the ground, the lamp, its marker and the camera.
const prewarmEntities = 1024

func main() {
	config := map[kernel.PluginName]any{
		gogpu.Name: gogpu.Config{}.
			WithTitle("cog examples: scene instancing").
			WithSize(windowWidth, windowHeight),
		ecs.Name: ecs.Config{PrewarmEntities: prewarmEntities},
	}
	permanentfs.Configure(config)

	// The demo plugin is last because its Systems order themselves against
	// scene's, which has to be registered before them.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(), // storage's PermanentFS Adapter for this platform
		inputplugin.New(),
		appplugin.New(),
		gfxplugin.New(),
		canvasplugin.New(),
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
const Name kernel.PluginName = "instancing"

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock: the update event's Dt is deliberately ignored.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The documented starting pose, where the reference screenshot is taken.
//
// The radius is what decides how much of the field the frustum keeps, which is
// the number this demo is about: at this pose a little over half the lattice is
// inside the frustum and the rest is behind the camera or off to the sides.
const (
	overviewRadius = 15.0
	startAzimuth   = 0.0
	startElevation = 0.34
	orbitSpeed     = 1.2 // radians per second held down
	fieldOfViewY   = 1.0472
	nearPlane      = 0.1
	// The far plane is past the far corner of the lattice on purpose. A far
	// plane that cut the field would cull more, and visibly, but the cut is a
	// straight edge across the ground that reads as a bug in the picture.
	farPlane = 200
)

// orbitTarget is the point the camera looks at and orbits, a little above the
// courtyard floor so the ground fills the lower part of the frame.
var orbitTarget = m.Vec3{Y: 1.2}

// Instancing is the demo's gameplay plugin. It registers the demo's Component
// and Systems, and keeps the Demo resource it registers, so a test can read the
// pose and the counts it steps.
type Instancing struct {
	demo *Demo
}

// New builds the demo plugin at its documented starting pose.
func New() *Instancing { return &Instancing{demo: newDemo()} }

func (p *Instancing) Name() kernel.PluginName { return Name }

func (p *Instancing) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{
		canvas.Name, ecs.Name, gfx.Name, input.Name, model.Name, scene.Name, storage.Name,
	}
}

type (
	setupSystem   kernel.Subscription[app.InitEvent]
	controlSystem kernel.Subscription[app.UpdateEvent]
	splitSystem   kernel.Subscription[app.UpdateEvent]
	orbitSystem   kernel.Subscription[app.UpdateEvent]
	hudSystem     kernel.Subscription[app.UpdateEvent]

	windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
)

func (p *Instancing) Register(registrar *kernel.Registrar, _ any) error {
	// storage mounts nothing by default, and the vendored asset set lives in
	// the repository rather than beside the executable, which `go run` builds
	// into a temporary directory - so the demo contributes it explicitly and
	// refuses to start without it.
	mount, err := assets.Mount()
	if err != nil {
		return err
	}
	registrar.ProvideAdapter[assets.StorageReadMount](mount)

	ecs.RegisterComponent[Crate](registrar, prewarmEntities)
	registrar.InitResource(p.demo)

	registrar.Subscribe[setupSystem](ecs.ToHandler[app.InitEvent](registrar, setup))
	// Input rolls its per-step edges first, and a key's JustPressed is only
	// this step's once it has.
	registrar.Subscribe[controlSystem](ecs.ToHandler[app.UpdateEvent](registrar, control)).
		After[input.AdvanceOnUpdate]()
	// A mode switch rewrites the crates' Params, and the load System keys what
	// changed, so the split runs before it and the new keys draw in the step
	// whose key press asked for them.
	registrar.Subscribe[splitSystem](ecs.ToHandler[app.UpdateEvent](registrar, split)).
		After[controlSystem]().Before[scene.LoadOnUpdate]()
	registrar.Subscribe[orbitSystem](ecs.ToHandler[app.UpdateEvent](registrar, orbit)).
		After[controlSystem]().Before[scene.RecordOnUpdate]()
	registrar.Subscribe[hudSystem](ecs.ToHandler[app.UpdateEvent](registrar, hud)).
		After[splitSystem]()

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
