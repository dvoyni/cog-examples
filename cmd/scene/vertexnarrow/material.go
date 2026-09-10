package main

// THROWAWAY. The demo's two materials, each generated as one WGSL source that
// carries every rung of its ladder at once and picks between them per fragment.
//
// # Why the comparison is inside one draw
//
// The obvious arrangement - one sphere per encoding, side by side - does not
// run. On this machine a scene frame that records more than one draw of a
// mesh with a caller-supplied material never reaches the GPU: the swapchain
// reports that no submission is ready to present, nothing is reported through
// the kernel's error handler, and the window stays black. cmd/scene/procedural,
// which is the tree's only other custom-material demo, fails the same way, so
// this is not something this prototype introduced. It is exactly the failure
// class issue 172 was told to expect - with no pixel readback anywhere, a frame
// that does not build looks identical to a mesh that failed to load.
//
// One draw is stable, so the whole comparison lives inside one. The vertex
// stage decodes every candidate and hands them all to the fragment stage, which
// picks by where the fragment sits across the frame. That turns out to be a
// better A/B than the row would have been: the rungs meet along a seam, on the
// same surface, at the same distance, under the same light, at pixels that are
// neighbours. A terrace that stops at the seam is the encoding; one that
// crosses it is the geometry.
//
// The stripe is cut in view space rather than in screen space, because a
// fragment shader cannot know the framebuffer's size and this demo would
// otherwise have to be told. View-space x is the horizontal offset from the
// camera axis in world units, so on a sphere of radius 1 the four stripes fall
// on fixed quarters of the silhouette however far away the camera stands.

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/scene"
)

// The prefix of scene's SceneFrame these shaders read, and the 64-byte instance
// record, copied the way cmd/scene/procedural copies them: a caller-supplied
// material gets no scene helpers, so the declarations are the price of a custom
// vertex layout.
const scenePrelude = `
struct SceneFrame {
    view: mat4x4<f32>,
    projection: mat4x4<f32>,
    viewProjection: mat4x4<f32>,
    cameraPosition: vec4<f32>,
    sunDirection: vec4<f32>,
    sunColor: vec4<f32>,
    ambientSky: vec4<f32>,
    ambientGround: vec4<f32>,
};

struct SceneInstance {
    world0: vec4<f32>,
    world1: vec4<f32>,
    world2: vec4<f32>,
    animOffset: u32,
    flags: u32,
    spare: vec2<u32>,
};

struct SceneInstances { data: array<SceneInstance>, };

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;
@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;

const PI: f32 = 3.14159265359;

fn worldOf(instance: SceneInstance, local: vec3<f32>) -> vec3<f32> {
    let p = vec4<f32>(local, 1.0);
    return vec3<f32>(dot(instance.world0, p), dot(instance.world1, p), dot(instance.world2, p));
}

fn worldNormal(instance: SceneInstance, n: vec3<f32>) -> vec3<f32> {
    return normalize(vec3<f32>(
        dot(instance.world0.xyz, n), dot(instance.world1.xyz, n), dot(instance.world2.xyz, n)));
}

// heat is the shared error ramp. Blue is nothing, gold is the top of whatever
// scale the caller set, so two views at different scales cannot be confused.
fn heat(t: f32) -> vec3<f32> {
    let x = clamp(t, 0.0, 1.0);
    let cold = vec3<f32>(0.04, 0.06, 0.12);
    let warm = vec3<f32>(0.15, 0.55, 0.95);
    let hot = vec3<f32>(1.0, 0.85, 0.25);
    if x < 0.5 { return mix(cold, warm, x * 2.0); }
    return mix(warm, hot, (x - 0.5) * 2.0);
}
`

// The octahedral decode, written the way issue 171's published include would
// have to be written: one function, no state, no bindings, no assumptions about
// what the caller's vertex looks like.
const octPrelude = `
fn octDecode(e: vec2<f32>) -> vec3<f32> {
    let z = 1.0 - abs(e.x) - abs(e.y);
    if z < 0.0 {
        return normalize(vec3<f32>(
            (1.0 - abs(e.y)) * select(-1.0, 1.0, e.x >= 0.0),
            (1.0 - abs(e.x)) * select(-1.0, 1.0, e.y >= 0.0),
            z));
    }
    return normalize(vec3<f32>(e.x, e.y, z));
}

// oct22 arrives as a raw u32 because there is no 11/11 vertex format. It is the
// rung that shows what "decode in WGSL rather than in the fetch unit" costs at
// its worst: two shifts and two divides that Unorm16x2 gets for free.
fn unpackOct22(packed: u32) -> vec2<f32> {
    return vec2<f32>(
        f32(packed & 0x7FFu) / 2047.0 * 2.0 - 1.0,
        f32((packed >> 11u) & 0x7FFu) / 2047.0 * 2.0 - 1.0);
}
`

// rung names one candidate normal encoding, in the order the stripes run left
// to right across the frame.
type rung struct {
	Name  string
	Bytes int
	Bits  uint // octahedral bits per axis; 0 is the exact rung
}

var rungs = [...]rung{
	{"Float32x3 exact", 12, 0},
	{"oct32  Unorm16x2", 4, 16},
	{"oct22  Uint32 11+11", 4, 11},
	{"oct16  Unorm8x2", 2, 8},
}

// ladderShader builds the normal ladder. solo is -1 for the four stripes, or a
// rung index to fill the frame with one encoding alone.
func ladderShader(mode string, roughness float32, solo int) string {
	return fmt.Sprintf(`%s%s
const ROUGHNESS: f32 = %f;
const SOLO: i32 = %d;
const ERROR_FULL: f32 = %f;

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) exact: vec3<f32>,
    @location(2) oct32: vec2<f32>,
    @location(3) oct16: vec2<f32>,
    @location(4) oct22: u32,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) exact: vec3<f32>,
    @location(1) n32: vec3<f32>,
    @location(2) n22: vec3<f32>,
    @location(3) n16: vec3<f32>,
    @location(4) world: vec3<f32>,
    @location(5) viewX: f32,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    var out: VertexOut;
    out.world = worldOf(instance, vertex.position);
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(out.world, 1.0);
    // Every rung is decoded here, in the vertex stage, which is where issue 171
    // put the work. Three of the four are dead in any given fragment and the
    // compiler cannot know it, so this shader pays four decodes where a
    // shipping one pays a single one.
    out.exact = worldNormal(instance, normalize(vertex.exact));
    out.n32 = worldNormal(instance, octDecode(vertex.oct32 * 2.0 - 1.0));
    out.n22 = worldNormal(instance, octDecode(unpackOct22(vertex.oct22)));
    out.n16 = worldNormal(instance, octDecode(vertex.oct16 * 2.0 - 1.0));
    out.viewX = (sceneFrame.view * vec4<f32>(out.world, 1.0)).x;
    return out;
}

// stripe cuts the frame into the four rungs across a two-unit span of view
// space, which is the sphere's own diameter.
fn stripe(viewX: f32) -> i32 {
    if SOLO >= 0 { return SOLO; }
    return clamp(i32(floor((viewX + 1.0) * 2.0)), 0, 3);
}

// Lambert plus a GGX lobe. The specular is where a quantised normal shows
// first: the diffuse term is a cosine, flat near its peak and forgiving of a
// tilt, while the lobe's width divides any angular error by the roughness.
fn shade(n: vec3<f32>, world: vec3<f32>) -> vec3<f32> {
    let l = -normalize(sceneFrame.sunDirection.xyz);
    let v = normalize(sceneFrame.cameraPosition.xyz - world);
    let h = normalize(l + v);
    let nDotL = max(dot(n, l), 0.0);
    let nDotV = max(dot(n, v), 1e-4);
    let nDotH = max(dot(n, h), 0.0);

    let a = ROUGHNESS * ROUGHNESS;
    let a2 = a * a;
    let denominator = nDotH * nDotH * (a2 - 1.0) + 1.0;
    let distribution = a2 / (PI * denominator * denominator);
    let k = a * 0.5;
    let geometry = (nDotV / (nDotV * (1.0 - k) + k)) * (nDotL / (nDotL * (1.0 - k) + k));
    let fresnel = 0.04 + 0.96 * pow(1.0 - max(dot(h, v), 0.0), 5.0);
    let specular = distribution * geometry * fresnel / (4.0 * nDotV * nDotV + 1e-4);

    let albedo = vec3<f32>(0.60, 0.61, 0.64);
    let ambient = mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb, n.y * 0.5 + 0.5);
    return (albedo / PI * nDotL + vec3<f32>(specular) * nDotL) * sceneFrame.sunColor.rgb
         + albedo * ambient;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    let which = stripe(in.viewX);
    var n = normalize(in.exact);
    if which == 1 { n = normalize(in.n32); }
    else if which == 2 { n = normalize(in.n22); }
    else if which == 3 { n = normalize(in.n16); }
    let exact = normalize(in.exact);
    %s
}
`, scenePrelude, octPrelude, roughness, solo, ladderErrorFull, ladderModes[mode])
}

// ladderErrorFull is the top of the error ramp, in degrees. It is set a little
// above oct16's own worst case so the coarsest rung saturates and the two finer
// ones stay readable against it rather than all three reading as black.
const ladderErrorFull = 1.2

var ladderModes = map[string]string{
	// The picture the decision is actually about.
	"lit": `return vec4<f32>(shade(n, in.world), 1.0);`,
	// The angle between the decoded normal and the exact one. This is not the
	// judgement - the map's Notes say fidelity here is judged by eye and never
	// by test - but it says where on the surface to point the eye.
	"error": `let degrees = acos(clamp(dot(n, exact), -1.0, 1.0)) * 180.0 / PI;
    return vec4<f32>(heat(degrees / ERROR_FULL), 1.0);`,
	// The decoded normal straight out as colour: the harshest readout there is,
	// with no cosine to flatten it and no albedo to hide it.
	"raw": `return vec4<f32>(n * 0.5 + 0.5, 1.0);`,
	// What the shader is actually reading out of the frame block. Left third is
	// sunDirection, middle is the normalised cameraPosition, right is the exact
	// world normal. If the first two match, this shader's copy of SceneFrame is
	// misaligned with scene's and every number in it is a field late.
	"probe": `if in.viewX < -0.33 { return vec4<f32>(sceneFrame.sunDirection.xyz * 0.5 + 0.5, 1.0); }
    if in.viewX < 0.33 { return vec4<f32>(normalize(sceneFrame.cameraPosition.xyz) * 0.5 + 0.5, 1.0); }
    return vec4<f32>(exact * 0.5 + 0.5, 1.0);`,
	// Where the light actually is. A rig that puts the terminator somewhere
	// other than where the arithmetic says is worth catching before any
	// judgement is made on top of it.
	"ndl": `let l = -normalize(sceneFrame.sunDirection.xyz);
    return vec4<f32>(vec3<f32>(max(dot(n, l), 0.0)), 1.0);`,
}

func newLadderMaterial(mode string, roughness float32, solo int) scene.Material {
	return scene.Material{{
		Descr: gfx.MaterialWithState(
			gfx.ShaderWithText(ladderShader(mode, roughness, solo)), gfx.StateOpaque3D),
	}}
}

// --- the coordinate ladder ---------------------------------------------------

type uvRung struct {
	Name  string
	Bytes int
}

var uvRungs = [...]uvRung{
	{"Float32x2 exact", 8},
	{"Unorm16x2 + range", 4},
	{"Float16x2", 4},
}

// uvShader builds the coordinate ladder: one band, three horizontal stripes,
// the same texture sampled through three coordinates.
//
// The per-mesh scale and bias are compiled in as constants because one mesh has
// exactly one range. In the real thing they ride a storage buffer at
// @group(0) @binding(3) reached through a mesh index in the instance record,
// and neither of those changes what the picture looks like.
func uvShader(mode string, scale, bias [2]float32) string {
	return fmt.Sprintf(`%s
@group(1) @binding(0) var gridTexture: texture_2d<f32>;
@group(1) @binding(1) var gridSampler: sampler;

const UV_SCALE: vec2<f32> = vec2<f32>(%f, %f);
const UV_BIAS: vec2<f32> = vec2<f32>(%f, %f);
const GRID_WIDTH: f32 = %f;
const BAND_HEIGHT: f32 = %f;

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) exact: vec2<f32>,
    @location(3) narrow: u32,
    @location(4) half: vec2<f32>,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) exact: vec2<f32>,
    @location(1) narrow: vec2<f32>,
    @location(2) half: vec2<f32>,
    @location(3) viewY: f32,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    var out: VertexOut;
    let world = worldOf(instance, vertex.position);
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(world, 1.0);
    out.exact = vertex.exact;
    // The dequantisation runs here, at the top of the vertex stage, which is
    // where issue 171 put it. Nothing downstream knows the coordinate was ever
    // narrow.
    let fixed = vec2<f32>(f32(vertex.narrow & 0xFFFFu), f32(vertex.narrow >> 16u)) / 65535.0;
    out.narrow = fixed * UV_SCALE + UV_BIAS;
    out.half = vertex.half;
    out.viewY = (sceneFrame.view * vec4<f32>(world, 1.0)).y;
    return out;
}

// The three stripes, top to bottom: exact, Unorm16x2, Float16x2. The reference
// is on top because the eye follows a line downwards off a known-good one.
fn stripe(viewY: f32) -> i32 {
    let third = BAND_HEIGHT / 3.0;
    return clamp(i32(floor((BAND_HEIGHT * 0.5 - viewY) / third)), 0, 2);
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    // All three are sampled and then chosen between, rather than sampled inside
    // the branch: a texture sample in non-uniform control flow is not allowed,
    // and the two dead samples cost nothing worth counting here.
    let s0 = textureSample(gridTexture, gridSampler, in.exact);
    let s1 = textureSample(gridTexture, gridSampler, in.narrow);
    let s2 = textureSample(gridTexture, gridSampler, in.half);
    let which = stripe(in.viewY);
    var uv = in.exact;
    var sampled = s0;
    if which == 1 { uv = in.narrow; sampled = s1; }
    else if which == 2 { uv = in.half; sampled = s2; }
    %s
}
`, scenePrelude, scale[0], scale[1], bias[0], bias[1],
		float32(gridWidth), float32(bandHeight), uvModes[mode])
}

var uvModes = map[string]string{
	// The vernier. Three stripes sample one texture at one u, so a coordinate
	// that drifted shows as a line that fails to meet the line above it.
	"textured": `return sampled;`,
	// The drift itself, in texels of a GRID_WIDTH-wide texture, which is the
	// unit every figure in issue 171 is quoted in. The ramp tops out at eight
	// texels, which is two half-float steps at the ordinary case.
	// What each attribute actually carries, straight out: the raw Unorm16x2 in
	// red, the raw Float16x2 rescaled in green. Both should sweep 0 to 1 across
	// the band. A channel that stays black is an attribute the pipeline did not
	// deliver, which is the failure this prototype has to be able to tell apart
	// from a coordinate that merely drifted.
	"probe": `return vec4<f32>(in.narrow.x / 32.0, in.half.x / 32.0, in.exact.x / 32.0, 1.0);`,
	"error": `let texels = abs(uv.x - in.exact.x) * GRID_WIDTH;
    return vec4<f32>(heat(texels / 8.0), 1.0);`,
}

func newUVMaterial(mode string, rng uvRange) scene.Material {
	state := gfx.StateOpaque3D
	state.Cull = gfx.CullNone
	return scene.Material{{
		Descr: gfx.MaterialWithState(
			gfx.ShaderWithText(uvShader(mode,
				[2]float32{rng.Scale.X, rng.Scale.Y}, [2]float32{rng.Bias.X, rng.Bias.Y})),
			state,
			// Nearest everywhere, and no mip chain at all. Both are deliberate:
			// a linear filter smears a one-texel step into a gradient the eye
			// forgives, and a mip chain replaces the whole question with a
			// blur. The judgement is about where a texel lands, so the texels
			// have to keep their edges.
			gfx.SamplerParam("gridSampler", gfx.SamplerDesc{
				AddressU: gfx.AddressRepeat,
				AddressV: gfx.AddressRepeat,
				Mag:      gfx.FilterNearest,
				Min:      gfx.FilterNearest,
				Mip:      gfx.FilterNearest,
			})),
	}}
}

// --- the tangent-frame ladder, under an environment and a normal map --------
//
// Added for https://github.com/dvoyni/cog/issues/179. Same one-mesh-one-draw
// arrangement and the same view-space seam as the normal ladder above, for the
// same inherited reason: a frame recording more than one custom-material draw
// never reaches the GPU on this machine.
//
// What changes is what the stripes are. They are no longer four rungs of the
// normal's ladder but four candidate vertex FRAMES - normal and tangent
// together - because issue 171 gave the tangent its own encoding and issue 179
// leaves a split on the table. See environment.go for the four.

// tangentPrelude decodes the two tangent words. The handedness bit is decoded
// and carried through even though this sphere's is +1 everywhere: the probe
// view reads it back, so a word that arrives wrong shows as a tinted band
// rather than as a tangent frame that happens to look plausible.
const tangentPrelude = `
fn decodeTangent15(word: u32) -> vec4<f32> {
    let x = f32(word & 0x7FFFu) / 32767.0 * 2.0 - 1.0;
    let y = f32((word >> 15u) & 0x7FFFu) / 32767.0 * 2.0 - 1.0;
    return vec4<f32>(octDecode(vec2<f32>(x, y)), select(-1.0, 1.0, (word & 0x40000000u) != 0u));
}

fn decodeTangent8(word: u32) -> vec4<f32> {
    let x = f32(word & 0xFFu) / 255.0 * 2.0 - 1.0;
    let y = f32((word >> 8u) & 0xFFu) / 255.0 * 2.0 - 1.0;
    return vec4<f32>(octDecode(vec2<f32>(x, y)), select(-1.0, 1.0, (word & 0x10000u) != 0u));
}

// dirToEquirect is the environment's only geometry. gfx has no cube view
// dimension, so the panorama is a plain 2D texture and this is what turns a
// reflection vector into a texel: azimuth across u, elevation down v.
fn dirToEquirect(d: vec3<f32>) -> vec2<f32> {
    return vec2<f32>(atan2(d.z, d.x) / (2.0 * PI) + 0.5, acos(clamp(d.y, -1.0, 1.0)) / PI);
}

fn angleBetween(a: vec3<f32>, b: vec3<f32>) -> f32 {
    return acos(clamp(dot(normalize(a), normalize(b)), -1.0, 1.0)) * 180.0 / PI;
}
`

// reflectShader builds the frame ladder.
//
// envMix is 1 for the metal-under-environment case and 0 for the dielectric
// under a single sun, which is the first prototype's own lighting and therefore
// the way to look at the normal map alone. Both textures are sampled either
// way, at a weight of zero where they are switched off, so neither binding can
// be optimised out from under the material's parameter list.
func reflectShader(mode string, roughness, envLod, mapStrength, envMix float32, solo int) string {
	return fmt.Sprintf(`%s%s%s
@group(1) @binding(0) var envTexture: texture_2d<f32>;
@group(1) @binding(1) var envSampler: sampler;
@group(1) @binding(2) var mapTexture: texture_2d<f32>;
@group(1) @binding(3) var mapSampler: sampler;

const ROUGHNESS: f32 = %f;
const ENV_LOD: f32 = %f;
const MAP_STRENGTH: f32 = %f;
const ENV_MIX: f32 = %f;
const SOLO: i32 = %d;
const ERROR_FULL: f32 = %f;
const SWIM_FULL: f32 = %f;
const ENV_EXPOSURE: f32 = 1.7;
const METAL_F0: vec3<f32> = vec3<f32>(0.94, 0.91, 0.86);

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) nOct32: vec2<f32>,
    @location(3) nOct16: vec2<f32>,
    @location(4) tOct30: u32,
    @location(5) tOct16: u32,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) world: vec3<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) nExact: vec3<f32>,
    @location(3) tExact: vec3<f32>,
    @location(4) n32: vec3<f32>,
    @location(5) n16: vec3<f32>,
    @location(6) t30: vec4<f32>,
    @location(7) t16: vec4<f32>,
    @location(8) viewX: f32,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    var out: VertexOut;
    out.world = worldOf(instance, vertex.position);
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(out.world, 1.0);
    out.uv = vertex.uv;

    // The exact frame is DERIVED here rather than carried as two attributes:
    // on a unit sphere at the origin the normal is the position, and the
    // tangent is one cross product. Both sides compute it the same way, so the
    // reference stripe is exact rather than float32-rounded, and it is
    // interpolated exactly like the candidates rather than evaluated per
    // fragment - a per-fragment reference would differ from the candidates by
    // the interpolation and not by the quantisation.
    let nLocal = normalize(vertex.position);
    var tLocal = cross(vec3<f32>(0.0, 1.0, 0.0), nLocal);
    if length(tLocal) < 1e-4 {
        tLocal = vec3<f32>(1.0, 0.0, 0.0);
    } else {
        tLocal = normalize(tLocal);
    }
    out.nExact = worldNormal(instance, nLocal);
    out.tExact = worldNormal(instance, tLocal);

    out.n32 = worldNormal(instance, octDecode(vertex.nOct32 * 2.0 - 1.0));
    out.n16 = worldNormal(instance, octDecode(vertex.nOct16 * 2.0 - 1.0));
    let d30 = decodeTangent15(vertex.tOct30);
    let d16 = decodeTangent8(vertex.tOct16);
    out.t30 = vec4<f32>(worldNormal(instance, d30.xyz), d30.w);
    out.t16 = vec4<f32>(worldNormal(instance, d16.xyz), d16.w);

    out.viewX = (sceneFrame.view * vec4<f32>(out.world, 1.0)).x;
    return out;
}

fn stripe(viewX: f32) -> i32 {
    if SOLO >= 0 { return SOLO; }
    return clamp(i32(floor((viewX + 1.0) * 2.0)), 0, 3);
}

// mapped applies the tangent-space normal map to one frame. It is called twice
// per fragment - once for the candidate frame and once for the exact one - so
// every error view compares two surfaces that have had the same map applied,
// and what is left is the frame.
fn mapped(n: vec3<f32>, tangent: vec4<f32>, uv: vec2<f32>) -> vec3<f32> {
    let t = normalize(tangent.xyz - n * dot(n, tangent.xyz));
    let b = cross(n, t) * tangent.w;
    let sampled = textureSample(mapTexture, mapSampler, uv).xyz * 2.0 - 1.0;
    let perturbed = normalize(t * sampled.x + b * sampled.y + n * sampled.z);
    return normalize(mix(n, perturbed, MAP_STRENGTH));
}

// shade is a metal under the environment when ENV_MIX is 1 and the first
// prototype's dielectric under one sun when it is 0. Both paths sample the
// environment; the dielectric one weights it to nothing.
fn shade(n: vec3<f32>, world: vec3<f32>) -> vec3<f32> {
    let v = normalize(sceneFrame.cameraPosition.xyz - world);
    let r = reflect(-v, n);

    let sampled = textureSampleLevel(envTexture, envSampler, dirToEquirect(r), ENV_LOD).rgb;
    let plain = mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb, r.y * 0.5 + 0.5);
    let incoming = mix(plain, sampled * ENV_EXPOSURE, ENV_MIX);

    let f0 = mix(vec3<f32>(0.04, 0.04, 0.04), METAL_F0, ENV_MIX);
    let nDotV = max(dot(n, v), 1e-4);
    let fresnel = f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * pow(1.0 - nDotV, 5.0);

    // The sun lobe is kept on both paths so the roughness key means the same
    // thing at this station as at the last one.
    let l = -normalize(sceneFrame.sunDirection.xyz);
    let h = normalize(l + v);
    let nDotL = max(dot(n, l), 0.0);
    let nDotH = max(dot(n, h), 0.0);
    let a = ROUGHNESS * ROUGHNESS;
    let a2 = a * a;
    let denominator = nDotH * nDotH * (a2 - 1.0) + 1.0;
    let distribution = a2 / (PI * denominator * denominator);
    let k = a * 0.5;
    let geometry = (nDotV / (nDotV * (1.0 - k) + k)) * (nDotL / (nDotL * (1.0 - k) + k));
    let specular = distribution * geometry / (4.0 * nDotV * nDotV + 1e-4);

    let albedo = mix(vec3<f32>(0.60, 0.61, 0.64), vec3<f32>(0.0, 0.0, 0.0), ENV_MIX);
    let direct = (albedo / PI * nDotL + vec3<f32>(specular, specular, specular) * nDotL * (1.0 - ENV_MIX * 0.75))
        * sceneFrame.sunColor.rgb;
    return incoming * fresnel + direct;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    let which = stripe(in.viewX);

    var n = normalize(in.nExact);
    var t = vec4<f32>(normalize(in.tExact), 1.0);
    if which == 1 {
        n = normalize(in.n32);
        t = vec4<f32>(normalize(in.t30.xyz), in.t30.w);
    } else if which == 2 {
        n = normalize(in.n16);
        t = vec4<f32>(normalize(in.t30.xyz), in.t30.w);
    } else if which == 3 {
        n = normalize(in.n16);
        t = vec4<f32>(normalize(in.t16.xyz), in.t16.w);
    } else if which == 4 {
        n = normalize(in.n32);
        t = vec4<f32>(normalize(in.t16.xyz), in.t16.w);
    }

    let exactN = normalize(in.nExact);
    let exactT = vec4<f32>(normalize(in.tExact), 1.0);
    let shadingN = mapped(n, t, in.uv);
    let exactShadingN = mapped(exactN, exactT, in.uv);
    let v = normalize(sceneFrame.cameraPosition.xyz - in.world);
    %s
}
`, scenePrelude, octPrelude, tangentPrelude,
		roughness, envLod, mapStrength, envMix, solo, ladderErrorFull, swimErrorFull, reflectModes[mode])
}

// swimErrorFull is the top of the reflection-error ramp, in degrees. A
// reflection vector carries twice the normal's angular error, which is the
// whole reason this station exists, so its ramp is twice the normal ramp's and
// the two views can be read against each other.
const swimErrorFull = 2 * ladderErrorFull

var reflectModes = map[string]string{
	// The picture the decision is about.
	"lit": `return vec4<f32>(shade(shadingN, in.world), 1.0);`,
	// The angle between the candidate shading normal and the exact one, after
	// the same normal map has been applied to both.
	"error": `return vec4<f32>(heat(angleBetween(shadingN, exactShadingN) / ERROR_FULL), 1.0);`,
	// The quantity this station is actually about: the angle between the
	// candidate REFLECTION vector and the exact one, which is about twice the
	// normal error and is what moves an edge in the environment.
	"swim": `let a = reflect(-v, shadingN);
    let b = reflect(-v, exactShadingN);
    return vec4<f32>(heat(angleBetween(a, b) / SWIM_FULL), 1.0);`,
	// The tangent alone, which is the only thing the second and third stripes
	// differ by and the only readout that isolates issue 171's tangent word.
	"tangent": `return vec4<f32>(heat(angleBetween(t.xyz, exactT.xyz) / ERROR_FULL), 1.0);`,
	// The shading normal straight out as colour: no cosine to flatten it and no
	// albedo to hide it.
	"raw": `return vec4<f32>(shadingN * 0.5 + 0.5, 1.0);`,
	// What actually arrived. Four horizontal bands from the north pole down -
	// oct32 normal, oct16 normal, oct30 tangent, oct16 tangent - each shown as
	// the decoded direction in colour. An attribute that arrives as exactly
	// zero decodes to one constant direction, so a dead attribute is a BAND OF
	// FLAT COLOUR rather than a shaded surface, which is the check issue 185
	// makes a precondition. A band tinted red is a handedness bit that came
	// back negative, which on this sphere it never should.
	"probe": `let band = i32(clamp(floor(in.uv.y * 4.0), 0.0, 3.0));
    var d = normalize(in.n32);
    var bad = false;
    if band == 1 { d = normalize(in.n16); }
    else if band == 2 { d = normalize(in.t30.xyz); bad = in.t30.w < 0.0; }
    else if band == 3 { d = normalize(in.t16.xyz); bad = in.t16.w < 0.0; }
    let tint = select(vec3<f32>(1.0, 1.0, 1.0), vec3<f32>(1.0, 0.25, 0.25), bad);
    return vec4<f32>((d * 0.5 + 0.5) * tint, 1.0);`,
	// The environment itself, sampled straight through the surface normal
	// rather than through a reflection, so the panorama can be read and its
	// four quadrants located before anything is judged on top of it.
	"env": `let e = textureSampleLevel(envTexture, envSampler, dirToEquirect(shadingN), ENV_LOD).rgb;
    return vec4<f32>(e * ENV_EXPOSURE, 1.0);`,
}

func newReflectMaterial(mode string, roughness, envLod, mapStrength, envMix float32, solo int) scene.Material {
	return scene.Material{{
		Descr: gfx.MaterialWithState(
			gfx.ShaderWithText(reflectShader(mode, roughness, envLod, mapStrength, envMix, solo)),
			gfx.StateOpaque3D),
	}}
}
