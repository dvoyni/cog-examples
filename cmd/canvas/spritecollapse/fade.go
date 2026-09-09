package main

import (
	"os"

	"github.com/dvoyni/cog/gfx"
)

// CustomMaterialWorks turns the third column on. It is read from the
// environment rather than compiled in, so the same source runs against both
// engines: cog on main, where canvas routes any sprite carrying a material to
// the per-draw uniform shader and a material written against the instanced
// layout could never be reached, and cog on proto/sprite-collapse, where it can.
//
//	PROTO153_CUSTOM=0 go run ./cmd/canvas/spritecollapse   # cog on main
//	go run ./cmd/canvas/spritecollapse                     # cog on the branch
var CustomMaterialWorks = os.Getenv("PROTO153_CUSTOM") != "0"

// fadeShader is builtin/canvas/spritebatch.wgsl with exactly two differences,
// which is the whole claim under test: one member appended to the uniform
// block, and one line changed in fs_main. Groups, bindings, resource kinds, the
// SpriteInstance record and the vertex input are copied verbatim from the
// built-in, because the shape ticket froze them.
//
// The include is absolute: a relative include from inline text is an error by
// gfx/docs/specs/preprocessor.md, and this source has no directory to be
// relative to.
const fadeShader = `
struct BatchUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    // Appended by this material. The lens already does this in production, so
    // the mechanism is not new; what is new is doing it on the instanced shader.
    fade: f32,
};

struct SpriteInstance {
    transform0: vec4<f32>,
    transform1: vec4<f32>,
    frame: vec4<f32>,
    tint: vec4<f32>,
    misc: vec4<f32>,
    keyColor: vec4<f32>,
};

struct Instances {
    data: array<SpriteInstance>,
};

@group(0) @binding(0) var<uniform> u: BatchUniforms;
@group(1) @binding(0) var<storage, read> instances: Instances;
@group(2) @binding(0) var canvasSampler: sampler;
@group(2) @binding(1) var canvasTexture: texture_2d_array<f32>;

//#include builtin/canvas/keycolor.wgsl

struct VertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) keyColor: vec4<f32>,
};

@vertex
fn vs_main(@location(0) quad: vec2<f32>, @builtin(instance_index) instance: u32) -> VertexOut {
    let s = instances.data[instance];
    let origin = s.transform1.xy;
    let sine = s.transform1.z;
    let cosine = s.transform1.w;
    let scaled = (quad - origin) * s.transform0.zw;
    let rotated = vec2<f32>(
        scaled.x * cosine - scaled.y * sine,
        scaled.x * sine + scaled.y * cosine,
    );
    let local = s.transform0.xy + rotated;
    let world = u.canvasLayer * vec4<f32>(local, 0.0, 1.0);
    let viewport = u.canvasViewport.xy;
    var out: VertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.canvasPosition = local;
    out.uv = mix(s.frame.xy, s.frame.zw, quad);
    out.atlasLayer = i32(s.misc.x);
    out.tint = s.tint;
    out.keyColor = s.keyColor;
    return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if u.canvasViewport.z > 0.5 && (
        in.canvasPosition.x < u.canvasClip.x || in.canvasPosition.y < u.canvasClip.y ||
        in.canvasPosition.x > u.canvasClip.z || in.canvasPosition.y > u.canvasClip.w
    ) {
        discard;
    }
    let sampled = keyColorRamp(textureSample(canvasTexture, canvasSampler, in.uv, in.atlasLayer), in.keyColor.rgb);
    // The one changed line. Everything above is the built-in verbatim.
    return sampled * in.tint * vec4<f32>(1.0, 1.0, 1.0, u.fade);
}
`

// FadeMaterial is the custom material the third column draws with: the built-in
// instanced shader with an appended uniform member and one changed line.
func FadeMaterial() *gfx.MaterialDescr { return &fadeMaterial }

var fadeMaterial = gfx.MaterialWithState(gfx.ShaderWithText(fadeShader), gfx.StateOverlay2D,
	// fade is a uniform member the shader declared, so it is a MATERIAL
	// parameter, per batch. Naming it at the draw call instead would be the
	// authoring error cog#152 rules on and cog#158 asks gfx to detect.
	gfx.FloatParam("fade", 0.45),
)
