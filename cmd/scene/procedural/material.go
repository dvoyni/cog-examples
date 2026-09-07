package main

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/scene"
)

// The demo's own material: a whole gfx.MaterialDescr with inline WGSL, which is
// what a custom vertex layout obliges a caller to supply.
//
// # What a caller-supplied material may declare
//
// Scene binds three parameters on every draw, whatever material it uses:
// sceneFrame, sceneInstances and scenePbrMaterial. Those are the whole
// contract. A material that declares anything else has to bind it itself
// through MeshDraw.Params, and a binding a draw does not bind fails
// CreateBindGroup, whose error is swallowed - the frame's whole command buffer
// vanishes with no error anywhere. So this material declares two of the three
// and no parameters at all, and every colour the demo shows rides in its
// vertices instead.
//
// The third, scenePbrMaterial, is bound but useless here: a mesh draw's record
// is the bundled PBR's white paint, and a MeshDraw carries no colour to change
// it with - OverrideParams is a ModelDraw field, and MeshDraw.Params reach gfx
// rather than the record. Reading it would bind correctly and say nothing.
//
// # What it does not get
//
// No scene helper functions. gfx does no shader preprocessing of any kind -
// ShaderDescr is inline text or a storage path handed straight to the backend,
// with no include, macro or injection point - so a published shading contract
// would mean every consumer carrying its own copy of the BRDF. The struct
// declarations below are that copy-paste in miniature, and they are the price
// of the feature: keep them to the prefix the shader actually reads, because
// every field is one more thing to get out of step with scene.
//
// The group and binding numbers are this shader's own. gfx binds by reflected
// name, never by slot, so they have to be consistent here and nowhere else;
// they mirror scene's frequency convention because a reader comparing the two
// files should not have to hold two numbering schemes at once.
//
// # Shading
//
// Lambert plus scene's hemispheric ambient, which is the bundled PBR's own
// diffuse term for a rough dielectric with the specular lobe left off. That is
// deliberate: the ground plane beside the ridge takes the bundled PBR, and two
// materials reading the same sun out of the same sceneFrame should agree about
// where it is.
const shaderSource = `
// The prefix of scene's SceneFrame this shader reads. The binding is longer
// than this struct - the punctual light array follows - and a storage binding
// larger than the type it is read as is legal, so the tail costs nothing to
// leave undeclared.
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

// Scene's 64-byte instance record. world0..world2 are the *rows* of the 4x3
// world matrix, translation in w, so a row-wise dot is the matrix product.
// animOffset and flags are read by the bundled shader's skinning path and not
// by this one: every buffer-built draw carries SCENE_NOSKIN and animates
// nothing, so there is no pose to fetch and the fields are declared only to
// keep the record's size right.
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

const PI: f32 = 3.14159265359;

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) tint: vec3<f32>,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) normal: vec3<f32>,
    @location(1) tint: vec3<f32>,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    let local = vec4<f32>(vertex.position, 1.0);
    let world = vec3<f32>(
        dot(instance.world0, local),
        dot(instance.world1, local),
        dot(instance.world2, local),
    );
    var out: VertexOut;
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(world, 1.0);
    // Every instance this demo records scales uniformly, so the basis is its
    // own normal matrix and no inverse-transpose is needed. A demo that reached
    // for the Matrix escape hatch and squashed something would have to take
    // one, exactly as the bundled shader does for SCENE_NONUNIFORM.
    out.normal = normalize(vec3<f32>(
        dot(instance.world0.xyz, vertex.normal),
        dot(instance.world1.xyz, vertex.normal),
        dot(instance.world2.xyz, vertex.normal),
    ));
    out.tint = vertex.tint;
    return out;
}

@fragment
fn fs_main(in: VertexOut, @builtin(front_facing) frontFacing: bool) -> @location(0) vec4<f32> {
    // The material draws both sides, so a back face is lit by the normal it
    // would have had if it were a front one. Without the flip the ribbon's
    // underside and the ridge seen from below are lit as if the sun had moved.
    var normal = normalize(in.normal);
    if !frontFacing {
        normal = -normal;
    }
    // sunDirection is the sun's direction of travel, so the direction towards
    // it is its negation, and it is already normalised. Every colour in
    // sceneFrame is linear radiance with its intensity premultiplied.
    let nDotL = max(dot(normal, -sceneFrame.sunDirection.xyz), 0.0);
    let ambient = mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb,
                      normal.y * 0.5 + 0.5);
    let lit = in.tint / PI * sceneFrame.sunColor.rgb * nDotL + in.tint * ambient;
    return vec4<f32>(lit, 1.0);
}
`

// newMaterial builds the demo's scene material: one forward entry, no
// parameters, and two-sided.
//
// Two-sided because half the demo is surfaces with no inside - a rebuilt band
// and an undulating sheet - and back-face culling on those means holes that
// appear and vanish as the camera orbits. It is a material property, not a
// draw's, so the beacon is two-sided too and pays a little for it; a demo that
// minded would carry a second material.
//
// It is a plain value with no GPU handle in it, so the demo builds it once at
// construction rather than waiting for a backend, and passes the same slice on
// every draw: scene keys a material by content, so two draws naming this one
// intern to a single id and sort together.
func newMaterial() scene.Material {
	state := gfx.StateOpaque3D
	state.Cull = gfx.CullNone
	return scene.Material{{
		Descr: gfx.MaterialWithState(gfx.ShaderWithText(shaderSource), state),
	}}
}
