package main

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// The obelisk is the demo's multi-tag material, and it is the only thing in the
// frame that appears in more than one kind of pass.
//
// # Why it takes a whole mesh and two shaders
//
// Tag participation is purely a material property. A draw gets no say in which
// passes it appears in - layers give per-camera exclusion, the pass list gives
// per-pass control, and a material lacking an entry for a pass's tag is
// skipped in that pass. So "what does a depth pass draw" is answered entirely
// by which materials carry a depth entry, and the bundled PBR carries only
// forward. That is not a gap: every debug shape and every glTF model in this
// frame is bundled-PBR, so the depth pass draws exactly one thing, and that one
// number is the whole demonstration.
//
// The bundled PBR is also unreachable as an entry - scene does not export it -
// so a caller wanting two tags supplies both, which is the honest shape anyway:
// a tag entry is a whole gfx.MaterialDescr rather than a shader, because
// pipeline state is strictly per material and the depth entry wants different
// state from the forward one.
//
// # The two entries, and why their state differs
//
//   - forward is ordinary opaque 3D state: the prepass writes into a depth
//     texture of its own that nothing else reads, so this entry is not
//     depth-testing against its own earlier values. A prepass that did feed the
//     colour pass would need CompareLessEqual here, because a Less test against
//     its own written depth fails everywhere and the geometry vanishes - the
//     classic depth-prepass trap, and one field.
//   - depth writes depth and nothing else. Its shader has no fs_main at all,
//     which a depth-only pass is the only place that is legal: gfx builds no
//     fragment stage for a pipeline with no colour target, so there is no entry
//     point to name and no target to declare.
//
// # What the shaders declare
//
// Scene binds three parameters on every draw whatever the material: sceneFrame,
// sceneInstances and scenePbrMaterial. Those are the whole contract, and a
// declared binding that nothing binds fails CreateBindGroup, whose error is
// swallowed - the frame's entire command buffer vanishes with nothing reported.
// So each shader declares only the prefix it reads and no parameters of its
// own. The depth shader declares no fragment stage and so binds nothing beyond
// the two storage buffers its vertex stage reads.
//
// The group and binding numbers are these shaders' own. gfx binds by reflected
// name, never by slot.

// obeliskShared is the declarations both shaders need: scene's frame block and
// its instance record. It is a string constant concatenated into each shader
// rather than shared through an include, because gfx does no shader
// preprocessing of any kind - a ShaderDescr is inline text or a storage path
// handed straight to the backend.
const obeliskShared = `
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

// Scene's 64-byte instance record. world0..world2 are the rows of the 4x3 world
// matrix, translation in w, so a row-wise dot is the matrix product.
struct SceneInstance {
    world0: vec4<f32>,
    world1: vec4<f32>,
    world2: vec4<f32>,
    animOffset: u32,
    flags: u32,
    spare: vec2<u32>,
};

struct SceneInstances {
    data: array<SceneInstance>,
};

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;
@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;

fn worldOf(instance: SceneInstance, local: vec3<f32>) -> vec3<f32> {
    let point = vec4<f32>(local, 1.0);
    return vec3<f32>(
        dot(instance.world0, point),
        dot(instance.world1, point),
        dot(instance.world2, point),
    );
}
`

// obeliskForwardShader shades the obelisk in a colour pass: Lambert plus
// scene's hemispheric ambient, which is the bundled PBR's own diffuse term for
// a rough dielectric with the specular lobe left off. The ground beside it
// takes the bundled PBR, and two materials reading the same sun out of the same
// sceneFrame should agree about where it is.
const obeliskForwardShader = obeliskShared + `
const PI: f32 = 3.14159265359;

// Linear, not sRGB: everything past the vertex stage is.
const stone: vec3<f32> = vec3<f32>(0.62, 0.44, 0.26);

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) normal: vec3<f32>,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    var out: VertexOut;
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(worldOf(instance, vertex.position), 1.0);
    // The obelisk scales uniformly, so its basis is its own normal matrix and
    // no inverse-transpose is needed.
    out.normal = normalize(vec3<f32>(
        dot(instance.world0.xyz, vertex.normal),
        dot(instance.world1.xyz, vertex.normal),
        dot(instance.world2.xyz, vertex.normal),
    ));
    return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    let normal = normalize(in.normal);
    // sunDirection is the sun's direction of travel, so the direction towards
    // it is its negation, and it is already normalised. Every colour in
    // sceneFrame is linear radiance with its intensity premultiplied.
    let nDotL = max(dot(normal, -sceneFrame.sunDirection.xyz), 0.0);
    let ambient = mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb,
                      normal.y * 0.5 + 0.5);
    return vec4<f32>(stone / PI * sceneFrame.sunColor.rgb * nDotL + stone * ambient, 1.0);
}
`

// obeliskDepthShader writes the obelisk's depth and nothing else. It has no
// fragment entry point, which is the shape a depth-only pass wants and the only
// place it is legal: a pipeline built for a pass with no colour attachment
// carries no fragment stage, so there is no fs_main to find.
//
// It reads only location 0. A shader may read fewer attributes than the
// pipeline's vertex layout supplies, so the other seven of scene.Vertex cost
// nothing to leave undeclared.
const obeliskDepthShader = obeliskShared + `
@vertex
fn vs_main(@location(0) position: vec3<f32>, @builtin(instance_index) index: u32) -> @builtin(position) vec4<f32> {
    let instance = sceneInstances.data[index];
    return sceneFrame.viewProjection * vec4<f32>(worldOf(instance, position), 1.0);
}
`

// TagDepth is the pass tag the depth prepass runs under, and the tag the
// obelisk's second material entry serves. It is an ordinary string: scene
// interns a tag once per pass rather than once per draw, so a readable name
// costs nothing and needs no registration handshake.
const TagDepth scene.PassTag = "depth"

// newObeliskMaterial builds the two-entry material.
//
// It is a plain value with no GPU handle in it, so the demo builds it once at
// construction and passes the same slice every frame: scene keys a material by
// content, so this interns to one id whatever it is called from.
func newObeliskMaterial() scene.Material {
	return scene.Material{
		{Tag: scene.TagForward, Descr: gfx.MaterialWithState(gfx.ShaderWithText(obeliskForwardShader), gfx.StateOpaque3D)},
		{Tag: TagDepth, Descr: gfx.MaterialWithState(gfx.ShaderWithText(obeliskDepthShader), gfx.StateOpaque3D)},
	}
}

// The obelisk's shape: a square pillar that tapers, so its four faces take four
// different amounts of sun and a wrong normal is visible rather than merely
// suspected.
const (
	obeliskBase   = 0.42 // half-width at the ground
	obeliskTop    = 0.16 // half-width at the tip
	obeliskHeight = 3.1
)

// obeliskMesh builds the pillar: four tapered side quads and a cap, flat-shaded,
// which needs its own four vertices per face rather than eight shared corners.
//
// It uses scene.Vertex - the standard layout - even though the shaders read two
// of its eight attributes. A custom layout would oblige every consumer of this
// mesh to match it, and the twenty vertices here are not where this demo's
// bytes go.
func obeliskMesh() ([]scene.Vertex, []uint32) {
	base := [4]m.Vec3{
		{X: -obeliskBase, Z: obeliskBase},
		{X: obeliskBase, Z: obeliskBase},
		{X: obeliskBase, Z: -obeliskBase},
		{X: -obeliskBase, Z: -obeliskBase},
	}
	top := [4]m.Vec3{
		{X: -obeliskTop, Y: obeliskHeight, Z: obeliskTop},
		{X: obeliskTop, Y: obeliskHeight, Z: obeliskTop},
		{X: obeliskTop, Y: obeliskHeight, Z: -obeliskTop},
		{X: -obeliskTop, Y: obeliskHeight, Z: -obeliskTop},
	}
	var vertices []scene.Vertex
	var indices []uint32
	quad := func(a, b, c, d m.Vec3) {
		// Counter-clockwise seen from outside, which is what gfx.FrontCCW and
		// StateOpaque3D's back-face cull between them mean by front.
		normal := b.Sub(a).Cross(d.Sub(a)).Normalize()
		start := uint32(len(vertices))
		for _, corner := range [4]m.Vec3{a, b, c, d} {
			vertices = append(vertices, scene.Vertex{Position: corner, Normal: normal})
		}
		indices = append(indices, start, start+1, start+2, start, start+2, start+3)
	}
	for i := range base {
		next := (i + 1) % 4
		quad(base[i], base[next], top[next], top[i])
	}
	quad(top[0], top[1], top[2], top[3])
	return vertices, indices
}
