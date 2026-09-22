package main

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/slots/gfx"
)

// The demo's own material: a whole gfx.MaterialDescr with inline WGSL, which is
// what a custom vertex layout obliges a caller to supply.
//
// # What it includes
//
// model.PbrPath, by absolute storage name, and through it model.FramePath. That
// is the engine's own lighting: SceneFrame and the sceneFrame binding, the
// accessors that read the sun, the ambient and the punctual lights, and
// sceneShadeSurface, which lights a SceneSurface exactly as the bundled PBR
// lights the ground and the reference sphere. The shader fills a SceneSurface
// from its own vertices and hands it over, so nothing about the frame's layout
// or the BRDF is re-typed here. Before the prelude was published this shader
// carried a hand-copied prefix of SceneFrame, and the copy had already drifted:
// it predated viewDirection, so it read the sun out of the view-direction slot.
//
// PbrPath declares names this file must not declare again. model.PbrPath's doc
// lists them; everything below that is not theirs is prefixed or plainly the
// demo's own.
//
// # What a caller-supplied material may declare
//
// Every binding a material declares has to be bound on every draw that uses it,
// or gfx drops the draw and reports gfx.ErrStorageBufferUnsupplied. The renderer
// binds three parameters on every draw: sceneFrame, sceneInstances and
// scenePbrMaterial. The prelude brings sceneFrame; this material declares
// sceneInstances itself, because the instance record is not published and the
// vertex stage needs the world rows. It declares no parameter and no storage
// buffer of its own - the bundled shader already holds all eight the browser
// floor allows - so every colour the demo shows rides in its vertices.
//
// scenePbrMaterial is bound but useless here: a mesh draw's record is the
// bundled PBR's white paint, and a MeshDraw carries no colour to change it with.
//
// The group and binding numbers of sceneInstances are scene's own, because gfx
// binds by reflected name and scene binds that name at 0/1.
//
// # Shading
//
// sceneShadeSurface on a rough dielectric: the sun, every punctual light in the
// pass and the hemispheric ambient, with the vertex tint as the base colour.
// The ground plane beside the ridge takes the bundled PBR, and two materials
// lighting through the same function out of the same sceneFrame agree about
// where the sun is by construction rather than by care.
const shaderSource = "//#include " + model.PbrPath + `

// Scene's 64-byte instance record. world0..world2 are the *rows* of the 4x3
// world matrix, translation in w, so a row-wise dot is the matrix product.
// animOffset and flags are read by the bundled shader's skinning path and not
// by this one: every buffer-built draw carries SCENE_NOSKIN and animates
// nothing, so there is no pose to fetch and the fields are declared only to
// keep the record's size right.
struct ProceduralInstance {
    world0: vec4<f32>,
    world1: vec4<f32>,
    world2: vec4<f32>,
    animOffset: u32,
    flags: u32,
    spare: vec2<u32>,
};

struct ProceduralInstances {
    data: array<ProceduralInstance>,
};

@group(0) @binding(1) var<storage, read> sceneInstances: ProceduralInstances;

// The surface every piece of caller geometry is: a dielectric rough enough that
// the specular lobe stays a sheen rather than a highlight.
const proceduralRoughness: f32 = 0.9;

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) tint: vec3<f32>,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) world: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) tint: vec3<f32>,
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
    out.world = world;
    // Every instance this demo records scales uniformly, so the basis is its
    // own normal matrix and no inverse-transpose is needed. A demo that
    // squashed something through a non-uniform Scale would have to take one,
    // exactly as the bundled shader does for SCENE_NONUNIFORM.
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
    let surface = SceneSurface(in.world, normal, in.tint, 0.0, proceduralRoughness, 1.0);
    return vec4<f32>(sceneShadeSurface(surface), 1.0);
}
`

// materialShader is the demo's shader and materialState the state it draws
// with, apart so that scene's Material and ecsscene's MaterialTag are built from
// the same two values.
//
// Two-sided because half the demo is surfaces with no inside - a rebuilt band
// and an undulating sheet - and back-face culling on those means holes that
// appear and vanish as the camera orbits. It is a material property, not a
// draw's, so the beacon is two-sided too and pays a little for it; a demo that
// minded would carry a second material.
func materialShader() gfx.ShaderDescr { return gfx.ShaderWithText(shaderSource) }

func materialState() gfx.MaterialState {
	state := gfx.StateOpaque3D()
	state.Cull = gfx.CullNone
	return state
}

// newMaterial builds the demo's scene material: one forward entry, no
// parameters, and two-sided.
//
// It is a plain value with no GPU handle in it, so the demo builds it once at
// construction rather than waiting for a backend, and passes the same slice on
// every draw: scene keys a material by content, so two draws naming this one
// intern to a single id and sort together.
func newMaterial() scene.Material {
	return scene.Material{{
		Descr: gfx.MaterialWithState(materialShader(), materialState()),
	}}
}
