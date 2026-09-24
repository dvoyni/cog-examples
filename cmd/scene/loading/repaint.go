package main

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The repainted station's material: the whole of what "a replacement" means,
// in one small shader.
//
// # Why a replacement is a shader and not a second mode
//
// A Material is laid over the file's own material, never put in its place: for
// each primitive, scene resolves the tag's shader over the primitive's own
// params - its textures, samplers and numbers - and hands them all to gfx. gfx
// binds a param by the name a shader declares and ignores one no binding
// declares. So a shader that declares none of the file's bindings simply never
// reads them, and that is the replacement: no base colour, no factors, no
// texture transforms, and nothing for them to be stale against.
//
// That is also exactly the case the feature exists for: the dissolve, the
// silhouette and the depth-only pass, where binding the artist's numbers under
// a shader that never heard of them would be a wrong picture with nothing in
// the frame to explain it.
//
// # What it declares
//
// The bundled vertex stage whole, and a fragment stage of its own. Including
// model.VertexStagePath places, decodes and deforms a vertex exactly as the
// bundled shader does, under whatever variant scene supplies for the
// primitive's geometry - which is what lets this station draw the whole body
// subtree, wheels included, where a hand-written vertex stage that read only
// the instance record would leave the animated wheel nodes out of place.
//
// The fragment stage lights the paint with model.FramePath's sun and
// hemispheric ambient, the same frame the bundled shader reads, rather than a
// hand-copied prefix of it that drifts the first time a field is added in
// front of the ones it reads. It binds no texture and no sampler, and reads
// nothing of the material's uniform block.
//
// It still declares that block, through model's published prologue, fields
// and epilogue, and it has to: the block is group 1, and the skinned and
// morphed variants the vertex stage takes for the truck bind group 2. A shader
// that leaves group 1 empty beneath a group 2 loses the whole frame on the
// GPU with nothing reported, so declaring the block the bundled shader
// declares keeps the groups dense.
const repaintShaderSource = "//#include " + model.VertexStagePath + `
//#include ` + model.FramePath + `
//#include ` + model.MaterialProloguePath + `
//#include ` + model.MaterialFieldsPath + `
//#include ` + model.MaterialEpiloguePath + `

const PI: f32 = 3.14159265359;

// The paint. It is a constant in the shader rather than a parameter because a
// parameter is a binding, and the point of this station is a material that
// reads nothing of the file's - a replacement carrying its own uniform would
// blur that. Linear, not sRGB: everything past the vertex stage is.
const paint: vec3<f32> = vec3<f32>(0.55, 0.57, 0.62);

@fragment
fn fs_main(in: SceneVertexOut) -> @location(0) vec4<f32> {
    let normal = normalize(in.normal);
    // sceneSun's direction is towards the sun, and its radiance is linear
    // with the intensity premultiplied, as is every colour in sceneFrame.
    let sun = sceneSun();
    let nDotL = max(dot(normal, sun.direction), 0.0);
    return vec4<f32>(paint / PI * sun.radiance * nDotL + paint * sceneAmbient(normal), 1.0);
}
`

// repaintMaterial builds the replacement: one forward tag, opaque, no params.
//
// It is plain data with no GPU handle in it, and every call returns an equal
// value, so scene keys it to one material however many Entities carry it.
func repaintMaterial() scene.Material {
	return scene.Material{Tags: m.NewList(scene.MaterialTag{
		Tag:    scene.TagForward,
		Shader: gfx.ShaderWithText(repaintShaderSource),
		State:  gfx.StateOpaque3D(),
	})}
}
