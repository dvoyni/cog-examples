package main

import (
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

// The demo's material is not scene's: it is WGSL that includes model.PbrPath and
// binds what every renderer binds, so the same shader and state draw under
// ecsscene as an ecsscene.MaterialTag. This composes ecsscene in scene's place,
// spawns the ridge as an Entity carrying that material and a camera lit like
// the demo's, and checks the frame reached the backend: the include resolved
// through model's mount, nothing was reported, and the custom-material draw
// bound sceneFrame and sceneInstances.
func TestTheMaterialDrawsUnderECSScene(t *testing.T) {
	engine := headless.NewECS(t, ecsRidge{})
	engine.Backend().TextShaderLayout = demoShaderLayout
	engine.Steps(3)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}

	backend := engine.Backend()
	custom := map[gfx.PipelineID]bool{}
	drawn := 0
	for _, call := range backend.Draws {
		if backend.ShaderPath(backend.PipelineOf(call.Pipeline).Shader) != textShaderLabel {
			continue
		}
		custom[call.Pipeline] = true
		drawn += call.Instances
	}
	if drawn == 0 {
		t.Fatal("nothing drew with the demo's material under ecsscene")
	}
	bound := map[headless.BufferBinding]bool{}
	for _, binding := range backend.Buffers {
		if custom[binding.Pipeline] {
			bound[headless.BufferBinding{Group: binding.Group, Binding: binding.Binding}] = true
		}
	}
	for _, want := range demoShaderLayout.Resources {
		if !bound[headless.BufferBinding{Group: want.Group, Binding: want.Binding}] {
			t.Errorf("the custom-material draws never bound %s", want.Name)
		}
	}
}

// textShaderLabel is the label gfx gives a shader built from inline source,
// which is how a draw with the demo's material is told from a bundled one.
const textShaderLabel = "gfx.shader"

// ecsRidge is the least ecsscene app that draws the demo's material: one ridge
// and one camera, spawned on the init event.
type ecsRidge struct{}

type (
	ridgeEntity struct {
		Place m.Transform
		Draw  ecsscene.Mesh
		Shade ecsscene.Material
	}
	eyeEntity struct {
		Place  m.Transform
		Camera ecsscene.Camera
	}
	ecsRidgeSetup kernel.Subscription[app.InitEvent]
)

func (ecsRidge) Name() kernel.PluginName { return "procedural-ecs-ridge" }

func (ecsRidge) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsscene.Name, model.Name}
}

func (ecsRidge) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[ecsRidgeSetup](ecs.ToHandler[app.InitEvent](registrar, spawnRidge))
	return nil
}

func spawnRidge(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	ridges *ecs.Spawn[ridgeEntity],
	eyes *ecs.Spawn[eyeEntity],
) {
	vertices, indices := ridgeGeometry(ridgeCells, 0)
	ref := model.NewLookupAccess(k, lookup.Get()).BakeMesh(vertices, indices, gfx.TopologyTriangleList)
	ridges.New(ridgeEntity{
		Place: m.At(ridgePosition.X, ridgePosition.Y, ridgePosition.Z).WithScale(ridgeScale),
		Draw:  ecsscene.Mesh{Ref: ref, Bounds: m.Vec4{W: ridgeBounds}},
		Shade: ecsscene.Material{Tags: m.NewList(ecsscene.MaterialTag{
			Shader: materialShader(), State: materialState(),
		})},
	})
	eyes.New(eyeEntity{
		Place: m.LookAt(m.Vec3{Y: 8, Z: 18}, orbitTarget, m.Vec3{Y: 1}),
		Camera: ecsscene.Camera{
			FovY: fieldOfViewY, Near: 0.1, Far: 100,
			SunDirection: sunDirection, SunColor: sunColor,
			AmbientSky: ambientSky, AmbientGround: ambientGround,
		},
	})
}
