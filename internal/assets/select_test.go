//go:build !js

package assets_test

import (
	"testing"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
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

// modelDraw is one Model Entity a test asks for: which part of the file, where
// it stands, and the clips it blends. The file itself is the one drawing names,
// so a test cannot spawn two files by accident.
type modelDraw struct {
	Scene, Node string
	At          m.Transform
	Plays       []model.ClipPlay
}

// spawner is a demo plugin the size of one frame: a camera that sees the whole
// model and one Model Entity per draw a test handed it, spawned once when the
// app starts. The real demos are their own tickets; this exists so the
// selectors can be asserted against the real bytes rather than against a
// document built in memory.
type spawner struct {
	path  string
	draws []modelDraw
}

// The Entities the spawner makes. A still model and a playing one are two
// shapes rather than one with an empty Animation, because a model with no
// Animation Component is the case a static prop is and the one worth drawing
// as such.
type (
	stillModel struct {
		Place m.Transform
		Draw  scene.Model
	}
	playingModel struct {
		Place m.Transform
		Draw  scene.Model
		Play  scene.Animation
	}
	eyeEntity struct {
		Place  m.Transform
		Camera scene.Camera
	}
	spawnOnInit kernel.Subscription[app.InitEvent]
)

func (*spawner) Name() kernel.PluginName { return "assets-spawner" }

func (*spawner) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name, model.Name}
}

func (s *spawner) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[spawnOnInit](ecs.ToHandler[app.InitEvent](registrar, s.spawn))
	return nil
}

func (s *spawner) spawn(
	still *ecs.Spawn[stillModel],
	playing *ecs.Spawn[playingModel],
	eyes *ecs.Spawn[eyeEntity],
) {
	// Well back, and far enough through, that nothing in the frame is culled:
	// these tests are about which primitives a selector picked, and a frustum
	// with an opinion would confuse the two.
	eyes.New(eyeEntity{
		Place:  m.LookAt(m.Vec3{Z: 40}, m.Vec3{}, m.Vec3{Y: 1}),
		Camera: scene.Camera{FovY: 1.0472, Near: 0.1, Far: 500},
	})
	for _, draw := range s.draws {
		ref := scene.Model{Ref: model.ModelRef{Path: s.path, Scene: draw.Scene, Node: draw.Node}}
		if len(draw.Plays) == 0 {
			still.New(stillModel{Place: draw.At, Draw: ref})
			continue
		}
		var animation scene.Animation
		copy(animation.Plays[:], draw.Plays)
		playing.New(playingModel{Place: draw.At, Draw: ref, Play: animation})
	}
}

// drawing runs an engine with one Model Entity per draw of one file, and steps
// it until the model is resident and the draws have reached the backend.
func drawing(t *testing.T, path string, draws ...modelDraw) *headless.Engine {
	t.Helper()
	mount, err := assets.Mount()
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	e := headless.New(t, headless.Mounting(mount), &spawner{path: path, draws: draws})
	preload(t, e, path)
	if !resident(t, e, path) {
		t.Fatalf("%s never became resident; engine reported %v", path, e.Errors())
	}
	e.Steps(2)
	return e
}

// sceneDraws steps one more frame and reports how many draws it made with
// model's bundled shader, and how many instances they drew between them. Every
// Entity here is drawn once, so the instances are one per selected primitive
// per Entity; the draws are one per Batch, which is one per distinct primitive
// and material however many Entities share it.
func sceneDraws(t *testing.T, e *headless.Engine) (draws, instances int) {
	t.Helper()
	backend := e.Backend()
	from := len(backend.Draws)
	e.Steps(1)
	for _, call := range backend.Draws[from:] {
		if backend.IsScenePipeline(call.Pipeline) {
			draws++
			instances += call.Instances
		}
	}
	return draws, instances
}

// drawn is the frame's instance count with model's bundled shader, which for
// these Entities is one per selected primitive. It is instances rather than
// draws because a draw is a Batch, and the truck's two wheels share a mesh and
// a material: the whole file is five primitives in four Batches.
func drawn(t *testing.T, e *headless.Engine) int {
	t.Helper()
	_, instances := sceneDraws(t, e)
	return instances
}

// A Node draw takes that node's slice and nothing else. One wheel out of a
// five-primitive file is the whole claim of the feature: one file, many
// independent props.
func TestANodeSelectorTakesOneWheelOfTheMilkTruck(t *testing.T) {
	e := drawing(t, truckAsset, modelDraw{Node: "Wheels"})
	if got := drawn(t, e); got != 1 {
		t.Errorf("the Wheels node drew %d primitives, want the one wheel primitive", got)
	}
}

// A subtree comes out whole. The truck body node has the two wheel nodes under
// it, two levels down, so selecting it is the whole model bar the Yup2Zup root
// - which is exactly the contiguous slice depth-first order made of it.
func TestANodeSelectorTakesTheWholeSubtreeOfTheMilkTruck(t *testing.T) {
	e := drawing(t, truckAsset, modelDraw{Node: "Cesium_Milk_Truck"})
	if got := drawn(t, e); got != truckPrimitives {
		t.Errorf("the truck body node drew %d primitives, want the subtree's %d",
			got, truckPrimitives)
	}
}

// An empty Node is the whole scene, which is what a Model without a selector
// gets.
func TestAnEmptyNodeStillDrawsTheWholeMilkTruck(t *testing.T) {
	e := drawing(t, truckAsset, modelDraw{})
	if got := drawn(t, e); got != truckPrimitives {
		t.Errorf("the whole file drew %d primitives, want %d", got, truckPrimitives)
	}
}

// Two nodes of one file, drawn in one frame, each take their own slice - the
// case the feature exists for, and the one a shared entry would break. The two
// wheels share a mesh, so the frame may well draw them as one Batch; what it
// may not do is lose one, so the count is of instances.
func TestTwoNodesOfOneFileDrawSideBySide(t *testing.T) {
	e := drawing(t, truckAsset,
		modelDraw{Node: "Wheels", At: m.At(-3, 0, 0)},
		modelDraw{Node: "Wheels.001", At: m.At(3, 0, 0)})
	if got := drawn(t, e); got != 2 {
		t.Errorf("two wheel Entities drew %d instances, want one each", got)
	}
}

// A typo'd node against a real file skips the draw and reports it, rather than
// falling back to the whole truck at the origin.
func TestATypoedNodeOnARealFileSkipsAndReports(t *testing.T) {
	e := drawing(t, truckAsset, modelDraw{Node: "Wheel"})
	if got := drawn(t, e); got != 0 {
		t.Errorf("a typo'd node drew %d primitives, want nothing at all", got)
	}
	var missing model.ErrModelNodeMissing
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
		modelDraw{},
		modelDraw{Scene: "second", At: m.At(3, 0, 0)})
	if got := drawn(t, e); got != 1 {
		t.Errorf("drew %d primitives, want the default scene's one and nothing for the named one", got)
	}
	var missing model.ErrModelSceneMissing
	if !anyErrorAs(e.Errors(), &missing) {
		t.Fatalf("errors = %v, want a missing-scene report", e.Errors())
	}
	if missing.Scene != "second" {
		t.Errorf("report names scene %q, want second", missing.Scene)
	}
}
