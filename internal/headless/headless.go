// Package headless runs a cog engine with no GPU, so a demo's assertions can be
// a plain `go test` beside its main.go.
//
// This is available because gfx takes its Backend adapter from whichever
// plugin provides one, and app takes its MainLoop the same way, so the adapter
// plugin below stands in for the gogpu plugin without it being present at
// all. scene records to gfx itself and publishes no frame of its own to read,
// so a demo asserts what reached the fake Backend below - the passes, draws
// and bound buffers - and gfx's frame snapshot.
//
// It lives here rather than beside one demo because seven demos would otherwise
// carry seven copies of the same twenty-odd stub methods. Nothing in it decides
// anything: a demo's own main.go stays self-contained.
package headless

import (
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/sceneplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// The viewport every headless run starts at: a 16:9 window at 2x scale, so a
// demo laying itself out against a framebuffer size gets a plausible one.
const (
	WindowWidth       = 960
	WindowHeight      = 540
	FramebufferWidth  = 1920
	FramebufferHeight = 1080
)

// Step is the fixed step every headless run is configured with, and the real
// time each frame Steps drives hands app's loop: exactly one tick a frame.
const Step = time.Second / 60

// Engine is a running headless kernel with a demo plugin in it.
type Engine struct {
	t      testing.TB
	kernel kernel.Executioner
	backend  *Backend
	mainLoop *mainLoop
	// reported is guarded because a model load reports from its own goroutine
	// rather than from the flush a test drives - which is the whole of "an
	// error can outlive the draw call that caused it".
	mu       sync.Mutex
	reported []error
}

func (e *Engine) report(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reported = append(e.reported, err)
}

// New starts an engine with storage, input, app, gfx, canvas, model, ecs and
// scene, plus the given demo plugins, composes storage's diskstorage Adapter
// (through permanentfs), a fake backend adapter and a headless app MainLoop,
// has app's Loop publish InitEvent, and sets the viewport. Every error the
// engine reports is collected rather than fatal, so a test can assert on the
// whole list at once. The given plugins must not compose ecs or scene again.
//
// It mounts nothing of its own: a headless demo run reads exactly the mounts its
// plugins contribute - canvas's shaders, model's, scene's, and the asset set a
// demo's own plugin provides - so it does not depend on the working directory
// `go test` happens to choose. A test that needs files no plugin in it provides
// adds them with Mounting.
func New(t testing.TB, plugins ...kernel.Plugin) *Engine {
	t.Helper()
	return start(t, append([]kernel.Plugin{
		ecsplugin.New(), sceneplugin.New(), &probe{},
	}, plugins...))
}

func start(t testing.TB, plugins []kernel.Plugin) *Engine {
	t.Helper()
	engine := &Engine{t: t, backend: &Backend{}, mainLoop: &mainLoop{}}

	config := map[kernel.PluginName]any{
		app.Name: app.Config{Step: Step},
	}
	permanentfs.Configure(config)
	all := append([]kernel.Plugin{
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(), gfxplugin.New(),
		adapter{backend: engine.backend, mainLoop: engine.mainLoop}, canvasplugin.New(), modelplugin.New(),
	}, plugins...)

	running := kernel.New(config).
		Handler(func(err error) error { engine.report(err); return nil }).
		WithPlugins(all...)

	// A composition that failed refuses every dispatch, so a demo would assert
	// against a frame that never ran. It closes Ready from WithPlugins, before
	// Run is ever called, and Run answers with the cause instead of starting
	// anything - so asking here, while Run has not run, is the one place the
	// two states are still told apart without a race.
	select {
	case <-running.Ready():
		t.Fatalf("composition failed: %v", running.Run())
	default:
	}

	// Quit is the whole of how a run ends, and a headless engine has no Host, so
	// Run blocks on it until Cleanup asks. The MainLoop's own Quit - what app
	// calls for QuitCmd - is the same ask, and it is bound before anything can
	// dispatch one.
	engine.mainLoop.quit = running.Quit
	done := make(chan error, 1)
	go func() { done <- running.Run() }()
	t.Cleanup(func() {
		running.Quit()
		<-done
	})
	<-running.Ready()

	engine.kernel = running.Executioner()
	// app attaches the Loop from its Start, and a Start failure this handler
	// collects rather than terminates on skips that without failing
	// composition; its errors are collected like any other, and nothing ticks.
	if engine.mainLoop.loop != nil {
		engine.mainLoop.loop.Init(engine.kernel)
	}
	engine.kernel.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: WindowWidth, Height: WindowHeight,
		FramebufferWidth: FramebufferWidth, FramebufferHeight: FramebufferHeight,
	})
	return engine
}

// Steps drives n frames the way a MainLoop does, through app's Loop. Each is one
// frame of exactly one Step of real time, so app publishes exactly one update
// tick, and one render: the flush that decides the frame runs at the end of
// the update, and the render replays what it decided.
//
// A demo's own clock is accumulated fixed steps, so n here is exactly the step
// number the demo reaches, whatever the wall clock did.
func (e *Engine) Steps(n int) {
	if e.mainLoop.loop == nil {
		return
	}
	for range n {
		e.mainLoop.loop.Frame(e.kernel, Step.Seconds())
		e.mainLoop.loop.Render(e.kernel)
	}
}

// Errors is every error the engine reported since it started.
func (e *Engine) Errors() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.reported...)
}

// Backend is the fake the frame was rendered through, for the rare assertion
// that wants what actually reached the GPU.
func (e *Engine) Backend() *Backend { return e.backend }

// Executioner is the running engine's dispatch handle, which is how a test
// invokes a demo's own command:
//
//	reply, err := engine.Executioner().ExecuteCommand[fountain.HUDCmd](fountain.HUDRequest{})
//
// It is the undeclared synchronous dispatch, and a test harness is one of the
// two callers that legitimately holds one - the other being a host callback. A
// handler inside the engine receives a plain kernel.Kernel and cannot obtain
// this, which is the rule this method does not weaken: it lets a test stand
// where the host stands, not where a plugin does.
func (e *Engine) Executioner() kernel.Executioner { return e.kernel }

// Input applies a batch of input changes, exactly as the window backend does
// when a real key or pointer moves. It is how a test drives a demo's own key
// handling rather than reaching past it and setting the field the key would
// have set - which is the difference between asserting the demo and asserting
// the test.
//
// A key stays down until a change says otherwise, and JustPressed is an edge
// the state clears at the next apply, so holding a key across two frames means
// two frames between the down change and the up one.
func (e *Engine) Input(changes ...input.Change) {
	e.kernel.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: changes})
}

// Lookup runs fn with a scoped LookupAccess, which is the half of the facade
// that bakes meshes, unloads a model and reads the memory totals - the same
// facade a demo's own handler builds.
func (e *Engine) Lookup(fn func(model.LookupAccess)) {
	e.kernel.ExecuteCommand[lookupCmd](lookupRequest{run: fn})
}

// LookupDevice runs fn with a scoped LookupDeviceAccess, which is how a test
// preloads a model or asks what is in one. It is a second method rather than a
// wider first one because the facade split is the thing being demonstrated: the
// loading half costs three locks and the other half costs one.
func (e *Engine) LookupDevice(fn func(model.LookupDeviceAccess)) {
	e.kernel.ExecuteCommand[lookupDeviceCmd](lookupDeviceRequest{run: fn})
}

// probe is the plugin that lends a test model's Lookup lock. It depends on
// scene, so it registers after the renderer.
type probe struct{}

type lookupCmd kernel.Command[lookupRequest, lookupResponse]
type lookupRequest struct{ run func(model.LookupAccess) }
type lookupResponse struct{}

type lookupDeviceCmd kernel.Command[lookupDeviceRequest, lookupDeviceResponse]
type lookupDeviceRequest struct {
	run func(model.LookupDeviceAccess)
}
type lookupDeviceResponse struct{}

func (*probe) Name() kernel.PluginName { return "headless-probe" }

func (*probe) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{model.Name, scene.Name, canvas.Name}
}

func (*probe) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[lookupCmd](lookupCmdImpl)
	registrar.HandleCommand[lookupDeviceCmd](lookupDeviceCmdImpl)
	return nil
}

func lookupCmdImpl() (kernel.Lock, kernel.Execute[lookupRequest, lookupResponse]) {
	var lookup kernel.Write[*model.Lookup]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*model.Lookup]()
		}, func(k kernel.Kernel, req lookupRequest) lookupResponse {
			req.run(model.NewLookupAccess(k, lookup.Get()))
			return lookupResponse{}
		}
}

func lookupDeviceCmdImpl() (kernel.Lock, kernel.Execute[lookupDeviceRequest, lookupDeviceResponse]) {
	var lookup kernel.Write[*model.Lookup]
	var files kernel.Read[storage.FileSystem]
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*model.Lookup]()
			files = access.GetRead[storage.FileSystem]()
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(k kernel.Kernel, req lookupDeviceRequest) lookupDeviceResponse {
			req.run(model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get()))
			return lookupDeviceResponse{}
		}
}

// adapter provides the fake Backend to gfx and the headless MainLoop to app,
// both Slots that take their Adapter from whichever plugin provides one - on a
// desktop, the gogpu plugin.
type adapter struct {
	backend  *Backend
	mainLoop *mainLoop
}

func (adapter) Name() kernel.PluginName           { return "headlessbackend" }
func (adapter) Dependencies() []kernel.PluginName { return nil }

func (a adapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[gfxBackendAdapter](gfx.Backend(a.backend))
	registrar.ProvideAdapter[appMainLoopAdapter](app.MainLoop(a.mainLoop))
	return nil
}

// Mounting is a plugin that contributes mount to storage, for a test whose
// plugins read files none of them provides - a stand-in for a demo, or no demo
// at all:
//
//	mount, err := assets.Mount()
//	engine := headless.New(t, headless.Mounting(mount), &recorder{})
//
// Its name carries the mount id, so two Mountings of different mounts compose
// side by side; the same mount twice is storage.ErrDuplicateMount either way.
func Mounting(mount storage.ReadMount) kernel.Plugin {
	return mounting{mount: mount}
}

type mounting struct{ mount storage.ReadMount }

func (m mounting) Name() kernel.PluginName {
	return kernel.PluginName("headless-mount-" + string(m.mount.Id))
}

func (mounting) Dependencies() []kernel.PluginName { return nil }

func (m mounting) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[storageReadMount](m.mount)
	return nil
}

// storageReadMount is the Adapter Mounting contributes its mount to storage as.
type storageReadMount kernel.Adapter[storage.ReadMountPort]

// gfxBackendAdapter is the Adapter the headless engine fills gfx's backend Port as.
type gfxBackendAdapter kernel.Adapter[gfx.BackendPort]

// appMainLoopAdapter is the Adapter the headless engine fills app's MainLoop Port as.
type appMainLoopAdapter kernel.Adapter[app.MainLoopPort]

// mainLoop is app's MainLoop for a headless run. It has no platform loop of its own:
// it keeps the Loop app attaches, Steps drives that Loop, and quitting asks the
// engine to stop, which is what ending a loop nothing else is running comes to.
type mainLoop struct {
	loop app.Loop
	quit func()
}

func (d *mainLoop) Attach(loop app.Loop) { d.loop = loop }
func (d *mainLoop) Quit()                { d.quit() }

// ClipboardWrite has no clipboard to write: a headless run has no system.
func (d *mainLoop) ClipboardWrite(string) error { return nil }
