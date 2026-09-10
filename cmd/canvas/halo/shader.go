package main

// haloShader is the prototype sprite material: a soft outward fade in one
// colour, behind whatever the sprite family draws.
//
// It replaces BOTH entry points, which is why it includes spritebindings.wgsl
// alone rather than spritevertex.wgsl - vs_main has to expand the quad, and the
// published one does not. Everything else it takes from canvas: the bindings,
// the clip test and the key-colour ramp are included, and the instance record is
// read exactly as the built-in reads it.
//
// Two mechanisms, branched per instance on the frame rect:
//
//   - A sprite or a glyph carries its silhouette in the texture, so the halo is
//     a ring kernel over alpha. Taps that leave s.frame are rejected outright,
//     because canvas's sampler clamps to the 4096 page and not to this sprite:
//     an out-of-frame tap reads a NEIGHBOUR's art. A sprite's own 2px pad is no
//     help either, since the atlas extrudes its edge in alpha as well as colour.
//   - A FillRect, a Line or a StrokeRect segment is one white texel stretched
//     over a rectangle. Its frame is a degenerate point, so there is nothing to
//     sample and the texel-to-world conversion collapses to zero. Its silhouette
//     is its geometry, so the halo comes from an analytic distance to the rect.
const haloShader = `
// The prefix of canvas's block this material reads, then its own members. An
// extending material hand-writes these six lines and must NOT include
// uniforms.wgsl: include-once by resolved path would have declared the struct
// already, and WGSL cannot add a member to a struct declared elsewhere.
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    // x: halo reach in canvas world units. y: falloff exponent. z: taps per
    // ring. w: rings.
    haloShape: vec4<f32>,
    // x: the fraction of the reach held at full strength before the fade
    // begins. The baked art this has to match is a plateau and then a ramp, not
    // a ramp from the ink outward.
    haloTune: vec4<f32>,
    haloColor: vec4<f32>,
    // x: 1 suppresses the halo, which is the A/B's other side. y: 1 shows the
    // halo alone with the mark punched out. z: 1 outlines each expanded quad.
    haloDebug: vec4<f32>,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;

//#include builtin/canvas/spritebindings.wgsl
//#include builtin/canvas/clip.wgsl
//#include builtin/canvas/keycolor.wgsl

// The published VertexOut is not frozen and is named nowhere in Go, so a
// material declares its own beside it under its own name. This one carries two
// more things: the instance index, flat, and the position within the sprite's
// own unit quad.
//
// The index rather than the frame rect, because every canvas binding is bound
// Vertex|Fragment - so one component buys the whole 96-byte record, where the
// frame alone would cost four. The quad coordinate cannot come the same way: it
// is the one thing here that varies across the fragment.
struct HaloVertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) keyColor: vec4<f32>,
    @location(5) @interpolate(flat) index: u32,
    @location(6) quad: vec2<f32>,
};

// haloFalloff maps a distance, as a fraction of the reach, to coverage. The
// plateau is what makes it match hand-painted art: measured across the outer
// band of feuds' unit sprites, alpha holds near 0.82 for the first fifth of the
// band and then falls away almost exactly linearly.
fn haloFalloff(t: f32) -> f32 {
    let plateau = clamp(u.haloTune.x, 0.0, 0.99);
    let x = clamp((clamp(t, 0.0, 1.0) - plateau) / (1.0 - plateau), 0.0, 1.0);
    return pow(1.0 - x, max(u.haloShape.y, 0.001));
}

@vertex
fn vs_main(@location(0) quad: vec2<f32>, @builtin(instance_index) instance: u32) -> HaloVertexOut {
    let s = instances.data[instance];
    let size = max(abs(s.transform0.zw), vec2<f32>(0.0001));
    // The whole of the mechanism, in one line: grow the unit quad outward by the
    // reach on every side. Expressed as a fraction of the sprite's own size, so
    // the band is the same width in world units however large the sprite is.
    //
    // Nothing in canvas reads a sprite's extent - no clip intersection, no batch
    // key field, no culling of any kind - so a quad larger than the sprite is
    // simply drawn. That is what makes this legal without a contract change.
    let grow = vec2<f32>(u.haloShape.x) / size;
    let expanded = quad + (quad * 2.0 - 1.0) * grow;

    let origin = s.transform1.xy;
    let sine = s.transform1.z;
    let cosine = s.transform1.w;
    let scaled = (expanded - origin) * s.transform0.zw;
    let rotated = vec2<f32>(
        scaled.x * cosine - scaled.y * sine,
        scaled.x * sine + scaled.y * cosine,
    );
    let local = s.transform0.xy + rotated;
    let world = u.canvasLayer * vec4<f32>(local, 0.0, 1.0);
    let viewport = u.canvasViewport.xy;

    var out: HaloVertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.canvasPosition = local;
    // The uv extrapolates past the frame at the same texels-per-world-unit the
    // sprite itself is drawn at, which is what lets the fragment stage measure a
    // world-unit radius in uv space without being told the scale.
    out.uv = mix(s.frame.xy, s.frame.zw, expanded);
    out.atlasLayer = i32(s.misc.x);
    out.tint = s.tint;
    out.keyColor = s.keyColor;
    out.index = instance;
    out.quad = expanded;
    return out;
}

@fragment
fn fs_main(in: HaloVertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }

    let s = instances.data[in.index];
    let size = max(abs(s.transform0.zw), vec2<f32>(0.0001));
    let lo = min(s.frame.xy, s.frame.zw);
    let hi = max(s.frame.xy, s.frame.zw);
    let span = hi - lo;
    let reach = max(u.haloShape.x, 0.0001);

    // The mark itself, sampled and shaded exactly as the built-in sprite
    // material does, so the A/B toggle compares like with like. This sample sits
    // in uniform control flow deliberately: it is textureSample, with the same
    // automatic LOD the built-in gets. The kernel below cannot be.
    var mark = keyColorRamp(
        textureSample(canvasTexture, canvasSampler, in.uv, in.atlasLayer),
        in.keyColor.rgb,
    ) * in.tint;
    // Outside the original quad there is no mark, whatever the extrapolated uv
    // happens to land on. The expansion is the halo's, and only the halo's.
    // Component-wise rather than any(): naga's SPIR-V backend cannot lower
    // ir.ExprRelational, so every any()/all() in this shader is spelled out.
    if in.quad.x < 0.0 || in.quad.y < 0.0 || in.quad.x > 1.0 || in.quad.y > 1.0 {
        mark = vec4<f32>(0.0);
    }

    var coverage = 0.0;
    if span.x <= 0.0 || span.y <= 0.0 {
        // The degenerate frame: a fill. One texel over a rectangle, so the
        // texture holds no silhouette and the geometry is the silhouette.
        let p = (in.quad - 0.5) * size;
        let outside = length(max(abs(p) - size * 0.5, vec2<f32>(0.0)));
        coverage = haloFalloff(outside / reach);
    } else {
        // A sprite or a glyph. The reach in uv, per instance, from the frozen
        // record alone: how much uv one world unit spans is exactly the frame's
        // span over the sprite's size.
        let duv = reach * span / size;
        let rings = max(i32(u.haloShape.w), 1);
        let spokes = max(i32(u.haloShape.z), 1);
        var best = 0.0;
        for (var ring = 1; ring <= rings; ring = ring + 1) {
            let t = f32(ring) / f32(rings);
            for (var spoke = 0; spoke < spokes; spoke = spoke + 1) {
                // Half a step of rotation on alternate rings, so the taps do not
                // line up into spokes of their own.
                let angle = 6.2831853 * (f32(spoke) + 0.5 * f32(ring % 2)) / f32(spokes);
                let tap = in.uv + vec2<f32>(cos(angle), sin(angle)) * duv * t;
                // The rejection that makes this safe at any reach. Without it a
                // tap past the frame reads whatever the atlas packed next door,
                // or this sprite's own edge extruded into its padding.
                if tap.x < lo.x || tap.y < lo.y || tap.x > hi.x || tap.y > hi.y {
                    continue;
                }
                let alpha = textureSampleLevel(canvasTexture, canvasSampler, tap, in.atlasLayer, 0.0).a;
                best = max(best, alpha * haloFalloff(t));
            }
        }
        // Under the mark the halo is at full strength, so a soft glyph edge does
        // not show the backdrop through a gap between the ink and the band.
        if in.uv.x >= lo.x && in.uv.y >= lo.y && in.uv.x <= hi.x && in.uv.y <= hi.y {
            best = max(best, textureSampleLevel(canvasTexture, canvasSampler, in.uv, in.atlasLayer, 0.0).a);
        }
        coverage = best;
    }

    if u.haloDebug.z > 0.5 {
        // Instrumentation: one world unit inside the expanded quad's own edge.
        // Where two of these overlap is where one mark's halo is free to paint
        // over its neighbour's ink.
        let grow = vec2<f32>(reach) / size;
        let inset = min(
            min(in.quad.x + grow.x, 1.0 + grow.x - in.quad.x) * size.x,
            min(in.quad.y + grow.y, 1.0 + grow.y - in.quad.y) * size.y,
        );
        if inset < 1.0 {
            return vec4<f32>(1.0, 0.0, 1.0, 0.5);
        }
    }

    let haloAlpha = coverage * u.haloColor.a * (1.0 - clamp(u.haloDebug.x, 0.0, 1.0));
    if u.haloDebug.y > 0.5 {
        return vec4<f32>(u.haloColor.rgb, haloAlpha);
    }
    // The mark over the halo, in one fragment, in straight alpha - which is what
    // canvas blends. Compositing here rather than in two passes is why the halo
    // costs no extra draw, and also why it can only ever sit behind ITS OWN
    // mark: a neighbour drawn later brings its own band over this one's ink.
    let markAlpha = mark.a;
    let outAlpha = markAlpha + haloAlpha * (1.0 - markAlpha);
    if outAlpha <= 0.0 {
        discard;
    }
    let rgb = (mark.rgb * markAlpha + u.haloColor.rgb * haloAlpha * (1.0 - markAlpha)) / outAlpha;
    return vec4<f32>(rgb, outAlpha);
}
`
