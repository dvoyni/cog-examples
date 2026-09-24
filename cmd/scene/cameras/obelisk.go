package main

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The obelisk is the demo's multi-tag material, and it is the only thing in the
// frame that appears in more than one kind of pass.
//
// # Why it takes a whole mesh and two shaders
//
// Tag participation is purely a material property. An Entity gets no say in
// which passes it appears in - layers give per-camera exclusion, the pass list
// gives per-pass control, and a material lacking an entry for a pass's tag is
// skipped in that pass. So "what does a depth pass draw" is answered entirely
// by which Material Components carry a depth tag, and a draw with no Material
// - every glTF model here - takes the default scene shader, which serves
// forward alone; the debug shapes' own material does the same. That is not a
// gap: the depth pass draws exactly one thing, and that one number is the whole
// demonstration.
//
// A Material tag names a shader and a state rather than a finished material,
// and each is laid over what the mesh's "file" provides - for a Mesh, the
// bundled PBR's ingredients. So a caller wanting two tags writes two entries,
// each with the pipeline state it wants, and the depth entry wants different
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
// The frame block is model's own, included by its published storage path, so
// it cannot drift from what scene packs. The instance record is not published,
// so its 64 bytes are spelled out here. Everything else scene would bind - the
// bundled PBR's textures, samplers and numbers - is matched by name against
// what a shader declares, and a shader declaring none of it simply never reads
// it, which is what lets these two replace the bundled PBR rather than extend
// it.
//
// The group and binding numbers are these shaders' own. gfx binds by reflected
// name, never by slot.

// obeliskInstance is scene's 64-byte instance record, which both shaders read.
// It is a string constant concatenated into each shader rather than shared
// through an include, because model publishes no source for it. What model
// does publish - the frame block and the vertex decode - is included by
// absolute storage name.
const obeliskInstance = `
// world0..world2 are the rows of the 4x3 world matrix, translation in w, so a
// row-wise dot is the matrix product.
struct SceneInstance {
    world0: vec4<f32>,
    world1: vec4<f32>,
    world2: vec4<f32>,
    animOffset: u32,
    flags: u32,
    joint: u32,
    mesh: u32,
};

struct SceneInstances {
    data: array<SceneInstance>,
};

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
// takes a flat colour, and the model the bundled PBR; two materials reading
// the same sun out of the same sceneFrame should agree about where it is.
//
// It includes model's published vertex decode, because the standard layout
// stores the normal as oct32 in four bytes: @location(1) is a vec2<f32> and
// sceneDecodeNormal makes a direction of it. Declaring a vec3<f32> is refused
// at pipeline time - gfx requires a declared input's type to equal what the
// layout supplies - which is the only reason it is not a silent mis-shade,
// since WebGPU would have filled the third component with zero and lit the
// obelisk from a direction lying in the XY plane.
const obeliskForwardShader = "//#include " + model.VertexDecodePath + "\n" +
	"//#include " + model.FramePath + obeliskInstance + `
// Linear, not sRGB: everything past the vertex stage is.
const stone: vec3<f32> = vec3<f32>(0.62, 0.44, 0.26);

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec2<f32>,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) normal: vec3<f32>,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    // The decode first, before anything else touches the normal, which is
    // where the bundled shader puts it too.
    let normal = sceneDecodeNormal(vertex.normal);
    var out: VertexOut;
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(worldOf(instance, vertex.position), 1.0);
    // The obelisk scales uniformly, so its basis is its own normal matrix and
    // no inverse-transpose is needed.
    out.normal = normalize(vec3<f32>(
        dot(instance.world0.xyz, normal),
        dot(instance.world1.xyz, normal),
        dot(instance.world2.xyz, normal),
    ));
    return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    let normal = normalize(in.normal);
    // sceneSun's direction is already towards the sun, and every colour in
    // sceneFrame is linear radiance with its intensity premultiplied.
    let sun = sceneSun();
    let nDotL = max(dot(normal, sun.direction), 0.0);
    return vec4<f32>(stone / 3.14159265359 * sun.radiance * nDotL + stone * sceneAmbient(normal), 1.0);
}
`

// obeliskDepthShader writes the obelisk's depth and nothing else. It has no
// fragment entry point, which is the shape a depth-only pass wants and the only
// place it is legal: a pipeline built for a pass with no colour attachment
// carries no fragment stage, so there is no fs_main to find.
//
// It reads only location 0. A shader may read fewer attributes than the
// pipeline's vertex layout supplies, so the other five of model.Vertex cost
// nothing to leave undeclared.
const obeliskDepthShader = "//#include " + model.FramePath + obeliskInstance + `
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

// obeliskMaterial is the two-entry Material Component.
//
// It is a plain value with no GPU handle in it, built once and held by the
// obelisk's Entity from its spawn: scene keys a material when its Component
// changes, and this one never does.
var obeliskMaterial = scene.Material{Tags: m.NewList(
	scene.MaterialTag{
		Tag:    scene.TagForward,
		Shader: gfx.ShaderWithText(obeliskForwardShader),
		State:  gfx.StateOpaque3D(),
	},
	scene.MaterialTag{
		Tag:    TagDepth,
		Shader: gfx.ShaderWithText(obeliskDepthShader),
		State:  gfx.StateOpaque3D(),
	},
)}

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
// It uses model.Vertex - the standard layout - even though the shaders read two
// of its six attributes. A custom layout would oblige every consumer of this
// mesh to match it, and the twenty vertices here are not where this demo's
// bytes go.
func obeliskMesh() ([]model.Vertex, []uint32) {
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
	var vertices []model.Vertex
	var indices []uint32
	quad := func(a, b, c, d m.Vec3) {
		// Counter-clockwise seen from outside, which is what gfx.FrontCCW and
		// StateOpaque3D's back-face cull between them mean by front.
		normal := b.Sub(a).Cross(d.Sub(a)).Normalize()
		start := uint32(len(vertices))
		for _, corner := range [4]m.Vec3{a, b, c, d} {
			vertices = append(vertices, model.Vertex{Position: corner, Normal: normal})
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
