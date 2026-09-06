package assets_test

import (
	"sync"
	"testing"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// The vendored files the Scene and Node selectors are judged against.
// CesiumMilkTruck is the re-rooting asset: two wheel nodes at ±1.4 on X under a
// Yup2Zup root, four deep, so a Node draw has a real authored world transform
// to discard. MultipleScenes is the only file in the whole Khronos repository
// with more than one scenes entry.
const (
	truckAsset  = "assets/CesiumMilkTruck/CesiumMilkTruck.glb"
	scenesAsset = "assets/MultipleScenes/MultipleScenes.glb"
)

// truckPrimitives is what the whole file flattens to: the body's three
// primitives plus one per wheel node, both wheels sharing a mesh.
const truckPrimitives = 5

const cameraMain scene.CameraID = 1

// recorder is a demo plugin the size of one frame: a camera that sees the whole
// model and whatever model draws a test handed it. The real demos are their own
// tickets; this exists so the selectors can be asserted against the real bytes
// rather than against a document built in memory.
type recorder struct {
	mu    sync.Mutex
	path  string
	draws []scene.ModelDraw
}

type updateEventHandler kernel.Subscription[app.UpdateEvent]

func (*recorder) Name() kernel.PluginName { return "assets-recorder" }

func (*recorder) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name}
}

func (r *recorder) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[updateEventHandler](r.record)
	return nil
}

func (r *recorder) record() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*scene.OpQueue]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			q := queue.Get()
			// Well back, and far enough through, that nothing in the frame is
			// culled: these tests are about which primitives a selector picked,
			// and a frustum with an opinion would confuse the two.
			q.Camera(cameraMain, scene.CameraDescr{
				Transform: scene.LookAt(m.Vec3{Z: 40}, m.Vec3{}, m.Vec3{Y: 1}),
				FovY:      1.0472,
				Near:      0.1, Far: 500,
			})
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, draw := range r.draws {
				q.Model(scene.LayersAll, r.path, draw)
			}
			return nil
		}
}

// drawing runs an engine that draws one file every frame, and steps it until
// the model is resident. It returns the engine at the first frame the draws
// actually reached the pass.
func drawing(t *testing.T, path string, draws ...scene.ModelDraw) *headless.Engine {
	t.Helper()
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	e := headless.NewOver(t, config, &recorder{path: path, draws: draws})
	if !resident(t, e, path) {
		t.Fatalf("%s never became resident; engine reported %v", path, e.Errors())
	}
	e.Steps(1)
	return e
}

// batches is the pass's batch count, which for these draws is one per selected
// primitive: every draw is a single instance, so nothing collapses.
func batches(t *testing.T, e *headless.Engine) int {
	t.Helper()
	passes := e.Passes()
	if len(passes) != 1 {
		t.Fatalf("passes = %d, want the camera's one", len(passes))
	}
	return len(passes[0].Batches)
}

// A Node draw takes that node's slice and nothing else. One wheel out of a
// five-primitive file is the whole claim of the feature: one file, many
// independent props.
func TestANodeSelectorTakesOneWheelOfTheMilkTruck(t *testing.T) {
	e := drawing(t, truckAsset, scene.ModelDraw{Node: "Wheels"})
	if got := batches(t, e); got != 1 {
		t.Errorf("the Wheels node drew %d batches, want the one wheel primitive", got)
	}
}

// A subtree comes out whole. The truck body node has the two wheel nodes under
// it, two levels down, so selecting it is the whole model bar the Yup2Zup root
// - which is exactly the contiguous slice depth-first order made of it.
func TestANodeSelectorTakesTheWholeSubtreeOfTheMilkTruck(t *testing.T) {
	e := drawing(t, truckAsset, scene.ModelDraw{Node: "Cesium_Milk_Truck"})
	if got := batches(t, e); got != truckPrimitives {
		t.Errorf("the truck body node drew %d batches, want the subtree's %d",
			got, truckPrimitives)
	}
}

// An empty Node is the whole scene, which is what every draw before this ticket
// got and what every draw without a selector still gets.
func TestAnEmptyNodeStillDrawsTheWholeMilkTruck(t *testing.T) {
	e := drawing(t, truckAsset, scene.ModelDraw{})
	if got := batches(t, e); got != truckPrimitives {
		t.Errorf("the whole file drew %d batches, want %d", got, truckPrimitives)
	}
}

// Two nodes of one file, drawn in one frame, each take their own slice - the
// case the feature exists for, and the one a shared entry would break.
func TestTwoNodesOfOneFileDrawSideBySide(t *testing.T) {
	e := drawing(t, truckAsset,
		scene.ModelDraw{Node: "Wheels", Transform: scene.At(-3, 0, 0)},
		scene.ModelDraw{Node: "Wheels.001", Transform: scene.At(3, 0, 0)})
	if got := batches(t, e); got != 2 {
		t.Errorf("two wheel draws made %d batches, want one each", got)
	}
}

// A typo'd node against a real file skips the draw and reports it, rather than
// falling back to the whole truck at the origin.
func TestATypoedNodeOnARealFileSkipsAndReports(t *testing.T) {
	e := drawing(t, truckAsset, scene.ModelDraw{Node: "Wheel"})
	if got := batches(t, e); got != 0 {
		t.Errorf("a typo'd node drew %d batches, want nothing at all", got)
	}
	var missing scene.ErrModelNodeMissing
	if !anyErrorAs(e.Errors(), &missing) {
		t.Fatalf("errors = %v, want a missing-node report", e.Errors())
	}
	if missing.Model != truckAsset || missing.Node != "Wheel" {
		t.Errorf("report names %q/%q, want the truck and Wheel", missing.Model, missing.Node)
	}
}

// MultipleScenes is the only multi-scene file there is, and both of its scenes
// are unnamed - as is every node in them. A name-keyed selector therefore
// reaches neither: what the file has is its declared default, which is what an
// empty Scene draws. The unmatched name is reported rather than silently
// falling back to that default.
func TestMultipleScenesHasNoAddressableSceneButItsDefault(t *testing.T) {
	e := drawing(t, scenesAsset,
		scene.ModelDraw{},
		scene.ModelDraw{Scene: "second", Transform: scene.At(3, 0, 0)})
	if got := batches(t, e); got != 1 {
		t.Errorf("drew %d batches, want the default scene's one and nothing for the named one", got)
	}
	var missing scene.ErrModelSceneMissing
	if !anyErrorAs(e.Errors(), &missing) {
		t.Fatalf("errors = %v, want a missing-scene report", e.Errors())
	}
	if missing.Scene != "second" {
		t.Errorf("report names scene %q, want second", missing.Scene)
	}
}
