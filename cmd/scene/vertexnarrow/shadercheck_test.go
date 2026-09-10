package main

// THROWAWAY diagnostic, not a test of anything cog does.
//
// It exists because of the failure mode issue 172 warned about: gfx swallows a
// shader that will not compile, the frame's command buffer never reaches the
// GPU, and the only symptom is a swapchain with nothing to present. With no
// pixel readback anywhere, a shader that does not build looks exactly like a
// mesh that failed to load. This runs every generated shader through the same
// front end the backend uses and prints the error, which is the difference
// between five minutes and an afternoon.
//
//	go test ./cmd/scene/vertexnarrow/ -run Shaders -v

import (
	"fmt"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/spirv"
)

func TestShadersCompile(t *testing.T) {
	sources := map[string]string{}
	for _, mode := range ladderModeNames {
		for solo := stripesAll; solo < len(rungs); solo++ {
			sources[fmt.Sprintf("ladder %s solo=%d", mode, solo)] = ladderShader(mode, 0.22, solo)
		}
	}
	for _, mode := range uvModeNames {
		sources["uv "+mode] = uvShader(mode, [2]float32{18.5, 1}, [2]float32{0, 0})
	}
	// The frame ladder, in both lighting arrangements and with the map on and
	// off, because every one of those is a different generated source.
	for _, mode := range reflectModeNames {
		for _, envMix := range []float32{0, 1} {
			for _, strength := range []float32{0, 1} {
				name := fmt.Sprintf("reflect %s env=%.0f map=%.0f", mode, envMix, strength)
				sources[name] = reflectShader(mode, 0.06, envLod(0.06), strength, envMix, stripesAll)
			}
		}
	}
	for solo := range frames {
		sources[fmt.Sprintf("reflect lit solo=%d", solo)] =
			reflectShader("lit", 0.06, envLod(0.06), 1, 1, solo)
	}

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			ast, err := naga.Parse(source)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			module, err := naga.LowerWithSource(ast, source)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			problems, err := naga.Validate(module)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			for _, problem := range problems {
				t.Errorf("validation: %v", problem)
			}
			if _, err := naga.GenerateSPIRV(module, spirv.Options{}); err != nil {
				t.Fatalf("spirv: %v", err)
			}
		})
	}
}
