package main

import (
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// Compositing a camera's target onto the screen, which is what split-screen
// actually is.
//
// Scene has no viewport rectangle and no projection escape hatch, because a
// projection-baked sub-rect does not clip: a point at NDC x = 1.5, which the
// clipper would have discarded, is remapped to 0.25 and rasterises into the
// neighbouring camera's half, and gfx exposes no scissor. So each camera
// renders into its own gfx.TemporaryTarget and canvas draws the result. That
// also means split-screen, minimap, picture-in-picture and render scale are all
// one mechanism rather than four, and the composited panel can be bordered,
// faded and animated because it is an ordinary 2D draw.
//
// # Why it does not go through canvas.Sprite, and why it does not take the
// built-in triangle material either
//
// canvas.Sprite names a texture by resource path. A frame-local render target
// has no path and never will, so the route is DrawTriangles with the texture
// bound through gfx.TextureParam - which is exactly the escape hatch it exists
// to be.
//
// The material has to be this demo's own, and that is a finding rather than a
// preference. Canvas's built-in triangle shader runs every texel through the
// key-colour ramp, and the ramp is not a no-op on a rendered image. A texel is
// keyed when its red and blue agree within 0.2 in sRGB and its green is below
// 0.2 in sRGB, and a keyed texel comes out grey at its own red intensity - so
// every dark, low-green pixel of a 3D render is silently desaturated. A dark
// warm shadow at linear (0.10, 0.02, 0.02) is keyed, because sRGB-encoding its
// red and blue puts them 0.19 apart, and it leaves the shader as neutral grey.
// The default key colour does not save it: no key colour makes the ramp an
// identity, because the ramp's output is a function of red alone.
//
// So the composite declares a shader that samples and returns, with no ramp
// and no clip test. It declares two of the three uniforms canvas binds and
// neither of the two it does not read: gfx resolves a recorder's parameters by
// name against the reflected layout and drops the ones the shader never
// declared, so canvasClip and keyColor cost nothing to omit.
const compositeShader = `
// The prefix of canvas's uniform block this shader reads. canvasClip and
// keyColor follow it and are not declared, which is what makes this shader a
// passthrough rather than canvas's own.
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;
@group(1) @binding(0) var canvasSampler: sampler;
@group(1) @binding(1) var canvasTexture: texture_2d<f32>;

struct VertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) uv: vec2<f32>,
};

@vertex
fn vs_main(
    @location(0) position: vec2<f32>,
    @location(1) color: vec4<f32>,
    @location(2) uv: vec2<f32>,
) -> VertexOut {
    let world = u.canvasLayer * vec4<f32>(position, 0.0, 1.0);
    let viewport = u.canvasViewport.xy;
    var out: VertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.color = color;
    out.uv = uv;
    return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    return textureSample(canvasTexture, canvasSampler, in.uv) * in.color;
}
`

// compositeMaterial is the composite's whole material: 2D state, no parameters
// of its own. The texture and sampler ride on the draw instead, because they
// change per panel and a material carrying an inline texture would re-bake it
// every frame.
var compositeMaterial = gfx.MaterialWithState(gfx.ShaderWithText(compositeShader), gfx.StateOverlay2D)

// compositeSampler is what a composited render target wants: linear filtering,
// clamped, so the minimap's 512 texels resample smoothly into its 400 canvas
// units and neither panel wraps at its edge.
var compositeSampler = gfx.SamplerDesc{
	AddressU: gfx.AddressClamp, AddressV: gfx.AddressClamp,
	Mag: gfx.FilterLinear, Min: gfx.FilterLinear, Mip: gfx.FilterLinear,
}

// composite draws one panel's target onto the screen at that panel's rectangle.
//
// tint multiplies the sampled texel, which is how the demo dims the panel a
// click did not land in - proof, in one line, that a composited camera is an
// ordinary 2D draw and not a special case anywhere.
func composite(q *canvas.OpQueue, layer canvas.Layer, p panel, texture gfx.TextureDescr, tint m.Color) {
	left, top := p.rect.X, p.rect.Y
	right, bottom := left+p.rect.Width, top+p.rect.Height
	corner := func(x, y, u, v float32) canvas.Vertex {
		return canvas.Vertex{Position: m.Vec2{X: x, Y: y}, Color: tint, UV: m.Vec2{X: u, Y: v}}
	}
	topLeft := corner(left, top, 0, 0)
	topRight := corner(right, top, 1, 0)
	bottomRight := corner(right, bottom, 1, 1)
	bottomLeft := corner(left, bottom, 0, 1)
	q.DrawTriangles(layer,
		[]canvas.Vertex{topLeft, topRight, bottomRight, topLeft, bottomRight, bottomLeft},
		&compositeMaterial,
		gfx.TextureParam(canvas.TextureSlot, texture),
		gfx.SamplerParam(canvas.SamplerSlot, compositeSampler),
	)
}
