package main

// THROWAWAY, and the diagnostic that explains a black sphere.
//
// The frame ladder's metal reflected the environment only in a thin rim at the
// silhouette and was black everywhere else - which is what a Fresnel term looks
// like when F0 is zero. METAL_F0 is a module-scope vec3 used as an operand of
// mix(), and that is exactly the form issue 181 says naga's SPIR-V backend
// drops, handing the shader (0, 0, 0) with nothing to say so.
//
// cog fixed this in 4ed0b85 by overriding naga in ITS go.mod with the fork
// carrying gogpu/naga#92. A replace directive in a dependency's go.mod does not
// apply to the main module, so cog-examples did not inherit it: every demo here
// was compiling against the unfixed backend, this prototype included.
//
//	go test ./cmd/scene/vertexnarrow/ -run SPIRV -v
//
// This is scene/shaderconstvector_test.go's test, pointed at this shader.

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/spirv"
)

func TestSPIRVCarriesTheMetalF0(t *testing.T) {
	source := reflectShader("lit", 0.02, envLod(0.02), 0, 1, stripesAll)

	parsed, err := naga.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	module, err := naga.LowerWithSource(parsed, source)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	blob, err := naga.GenerateSPIRV(module, spirv.Options{})
	if err != nil {
		t.Fatalf("spirv: %v", err)
	}

	for _, component := range []float32{0.94, 0.91, 0.86} {
		want := math.Float32bits(component)
		found := false
		for i := 0; i+4 <= len(blob); i += 4 {
			if binary.LittleEndian.Uint32(blob[i:]) == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the SPIR-V binary carries no %v, so METAL_F0 reaches the shader as zero "+
				"and the metal reflects nothing at normal incidence. If go.mod no longer "+
				"overrides naga, that is why (issue 181, gogpu/naga#92)", component)
		}
	}
}
