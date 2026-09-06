// Package headless runs a cog engine with no GPU, so a demo's assertions can be
// a plain `go test` beside its main.go.
//
// This is available because scene decides everything a demo asserts - culling,
// sorting, packing - in the update-thread flush, publishes the result as
// Passes(dst []PassView) including the frustum, and gfx installs a Backend
// through SetBackendCmd without the wgpu plugin being present at all. What the
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
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// The viewport every headless run starts at: a 16:9 window at 2x scale, so a
// demo laying itself out against a framebuffer size gets a plausible one.
const (
	WindowWidth       = 960
	WindowHeight      = 540
	FramebufferWidth  = 1920
	FramebufferHeight = 1080
)

// Engine is a running headless kernel with a demo plugin in it.
type Engine struct {
	kernel   kernel.Executioner
	backend  *Backend
	reported []error
}

// New starts an engine with storage, input, gfx, canvas and scene, plus the
// given demo plugins, installs a fake backend and sets the viewport. Every
// error the engine reports is collected rather than fatal, so a test can assert
// on the whole list at once.
//
// Storage is mounted over an empty filesystem: a headless demo run must not
// depend on the working directory `go test` happens to choose, and the plugins'
// own builtin mounts (canvas's shaders, scene's) install themselves regardless.
func New(t testing.TB, plugins ...kernel.Plugin) *Engine {
	t.Helper()
	engine := &Engine{backend: &Backend{}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-examples").
			WithReadFS("headless", 10, fs.FS(fstest.MapFS{})),
		scene.Name: scene.DefaultConfig(),
	}
	all := append([]kernel.Plugin{
		storage.New(), input.New(), gfx.New(), canvas.New(), scene.New(), &probe{},
	}, plugins...)

	running := kernel.New(config).
		Handler(func(err error) bool { engine.reported = append(engine.reported, err); return false }).
		WithPlugins(all...)
	go running.Run(ctx)
	<-running.Ready()

	engine.kernel = running.Executioner()
	engine.kernel.PublishEvent(app.InitEvent{}).Wait()
	engine.kernel.ExecuteCommand[gfx.SetBackendCmd](gfx.SetBackendRequest{Backend: engine.backend})
	engine.kernel.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: WindowWidth, Height: WindowHeight,
		FramebufferWidth: FramebufferWidth, FramebufferHeight: FramebufferHeight,
	})
	return engine
}

// Steps drives n frames. Each is one update tick and one render, which is the
// pair the driver publishes: the flush that decides the frame runs at the end
// of the update, and the render replays what it decided.
//
// A demo's own clock is accumulated fixed steps, so n here is exactly the step
// number the demo reaches, whatever the wall clock did.
func (e *Engine) Steps(n int) {
	for range n {
		e.kernel.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
		e.kernel.PublishEvent(app.RenderEvent{}).Wait()
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
func (e *Engine) Errors() []error { return e.reported }

// Backend is the fake the frame was rendered through, for the rare assertion
// that wants what actually reached the GPU rather than what scene decided.
func (e *Engine) Backend() *Backend { return e.backend }

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

func (*probe) Name() kernel.PluginName { return "headless-probe" }

func (*probe) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name, canvas.Name}
}

func (*probe) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
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
