// Package headless runs a cog engine with no GPU, so a demo's assertions can be
// a plain `go test` beside its main.go.
//
// This is available because scene decides everything a demo asserts - culling,
// sorting, packing - in the update-thread flush, publishes the result as
// Passes(dst []PassView) including the frustum, gfx takes its Backend adapter
// from whichever plugin provides one, and app takes its MainLoop the same way, so
// the adapter plugin below stands in for the wgpu plugin without it being
// present at all. What the
// fake backend below does with the translated queue is therefore beside the
// point: it exists so the frame reaches the end of the pipe, and the numbers a
// test reads were already decided before it was called.
//
// It lives here rather than beside one demo because seven demos would otherwise
// carry seven copies of the same twenty-odd stub methods. Nothing in it decides
// anything: a demo's own main.go stays self-contained.
package headless

import (
	"context"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/canvasplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/inputplugin"
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
	kernel   kernel.Executioner
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

// New starts an engine with storage, input, app, gfx, canvas and scene, plus
// the given demo plugins, composes storage's diskfs Adapter (through
// permanentfs), a fake backend adapter and a headless app MainLoop, has app's
// Loop publish InitEvent, and sets the viewport. Every
// error the engine reports is collected rather than fatal, so a test can assert
// on the whole list at once.
//
// Storage is mounted over an empty filesystem: a headless demo run must not
// depend on the working directory `go test` happens to choose, and the plugins'
// own builtin mounts (canvas's shaders, scene's) install themselves regardless.
func New(t testing.TB, plugins ...kernel.Plugin) *Engine {
	t.Helper()
	return NewOver(t, storage.Config{}.
		WithReadFS("headless", 10, fs.FS(fstest.MapFS{})), plugins...)
}

// NewOver is New over a storage configuration the caller chose, which is how a
// test that loads real assets reaches them: storage mounts nothing by default,
// and `go test` runs from a package directory rather than the module root.
//
//	config, err := assets.Config(storage.Config{})
func NewOver(t testing.TB, storageConfig storage.Config, plugins ...kernel.Plugin) *Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	engine := &Engine{backend: &Backend{}, mainLoop: &mainLoop{quit: cancel}}

	config := map[kernel.PluginName]any{
		storage.Name: storageConfig,
		app.Name:     app.Config{Step: Step},
	}
	permanentfs.Configure(config)
	all := append([]kernel.Plugin{
		storageplugin.New(), permanentfs.New(), inputplugin.New(), appplugin.New(), gfxplugin.New(),
		adapter{backend: engine.backend, mainLoop: engine.mainLoop}, canvasplugin.New(), sceneplugin.New(), &probe{},
	}, plugins...)

	running := kernel.New(config).
		Handler(func(err error) bool { engine.report(err); return false }).
		WithPlugins(all...)
	go running.Run(ctx)
	<-running.Ready()

	engine.kernel = running.Executioner()
	// A composition that failed never started app, so no Loop was attached;
	// its errors are collected like any other, and nothing ticks.
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

// Passes reads the last flush's pass results out of scene's queue.
func (e *Engine) Passes() []scene.PassView {
	var out []scene.PassView
	e.inspect(func(q *scene.OpQueue) { out = q.Passes(nil) })
	return out
}

// Ops reads the last flush's recorded operations out of scene's queue.
func (e *Engine) Ops() []scene.Op {
	var out []scene.Op
	e.inspect(func(q *scene.OpQueue) { out = q.Ops(nil) })
	return out
}

// Errors is every error the engine reported since it started.
func (e *Engine) Errors() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.reported...)
}

// Backend is the fake the frame was rendered through, for the rare assertion
// that wants what actually reached the GPU rather than what scene decided.
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

// Lookup runs fn with a scoped LookupAccess, which is how a test preloads a
// model or asks what is in one - the same facade a demo's own handler builds.
func (e *Engine) Lookup(fn func(scene.LookupAccess)) {
	e.kernel.ExecuteCommand[lookupCmd](lookupRequest{run: fn})
}

// inspect runs fn inside a handler holding scene's OpQueue, so a test reads the
// queue the way a recorder does rather than racing the update thread.
func (e *Engine) inspect(fn func(*scene.OpQueue)) {
	e.kernel.ExecuteCommand[inspectCmd](inspectRequest{run: fn})
}

// probe is the plugin that lends a test scene's OpQueue lock.
type probe struct{}

type inspectCmd kernel.Command[inspectRequest, inspectResponse]
type inspectRequest struct{ run func(*scene.OpQueue) }
type inspectResponse struct{}

type lookupCmd kernel.Command[lookupRequest, lookupResponse]
type lookupRequest struct{ run func(scene.LookupAccess) }
type lookupResponse struct{}

func (*probe) Name() kernel.PluginName { return "headless-probe" }

func (*probe) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name, canvas.Name}
}

func (*probe) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
	registrar.HandleCommand[lookupCmd](lookupCmdImpl)
	return nil
}

func inspectCmdImpl() (kernel.Lock, kernel.Execute[inspectRequest, inspectResponse]) {
	var queue kernel.Write[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*scene.OpQueue]()
		}, func(_ kernel.Kernel, req inspectRequest) (inspectResponse, error) {
			req.run(queue.Get())
			return inspectResponse{}, nil
		}
}

func lookupCmdImpl() (kernel.Lock, kernel.Execute[lookupRequest, lookupResponse]) {
	var lookup kernel.Write[*scene.Lookup]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*scene.Lookup]()
		}, func(k kernel.Kernel, req lookupRequest) (lookupResponse, error) {
			req.run(scene.NewLookupAccess(k, lookup.Get()))
			return lookupResponse{}, nil
		}
}

// adapter provides the fake Backend to gfx and the headless MainLoop to app, both
// Slots that take their Adapter from whichever plugin provides one - on a
// desktop, the wgpu plugin.
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

// gfxBackendAdapter is the Adapter the headless engine fills gfx's backend Port as.
type gfxBackendAdapter kernel.Adapter[gfx.BackendPort]

// appMainLoopAdapter is the Adapter the headless engine fills app's MainLoop Port as.
type appMainLoopAdapter kernel.Adapter[app.MainLoopPort]

// mainLoop is app's MainLoop for a headless run. It has no platform loop of its own:
// it keeps the Loop app attaches, Steps drives that Loop, and quitting cancels
// the engine, which is what ending a loop nothing else is running comes to.
type mainLoop struct {
	loop app.Loop
	quit context.CancelFunc
}

func (d *mainLoop) Attach(loop app.Loop) { d.loop = loop }
func (d *mainLoop) Quit()                { d.quit() }
