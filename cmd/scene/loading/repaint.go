package main

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/scene"
)

// The repainted station's material: the whole of what "a replacement Material"
// means, in one small shader.
//
// # Why a replacement cannot reuse the bundled shader
//
// ModelDraw.Material replaces the file's materials wholesale, and the
// replacement takes glTF's own defaults for its record - which scene implements
// by binding no record at all. The bundled scene shader declares
// scenePbrMaterial and five texture-and-sampler pairs, and a declared binding
// must be bound: a bind group missing an entry fails CreateBindGroup, the error
// is swallowed, encoder.Finish()'s error is dropped, and the frame's whole
// command buffer vanishes with no error anywhere. So naming the bundled shader
// as a replacement would not repaint the model, it would delete the frame.
//
// A replacement is therefore always a shader of the caller's own, which is also
// exactly the case the feature exists for: the dissolve, the silhouette and the
// depth-only pass, where binding the artist's numbers under a shader that never
// heard of them would be a wrong picture with nothing in the frame to explain
// it.
//
// # What it declares
//
// Two of the three parameters scene binds on every draw, and no textures. The
// vertex stage reads locations 0 and 1 of scene.Vertex; a shader may read fewer
// attributes than the pipeline's vertex layout supplies, so the other six cost
// nothing to leave undeclared. Group 2 - the pose, joint and morph buffers -
// is undeclared too, which is why this station draws the truck's body rather
// than its wheels: the body node is not animated, so its placement is in the
// instance record where this shader can see it, and the two wheel nodes are
// animated, so theirs is in a pose buffer this shader does not read.
//
// The group and binding numbers are this shader's own. gfx binds by reflected
// name, never by slot, so they have to be consistent here and nowhere else.
const repaintShaderSource = `
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

// The paint. It is a constant in the shader rather than a parameter because a
// parameter is a binding, and the point of this station is a material that
// binds nothing of the file's - a replacement carrying its own uniform would
// blur that. Linear, not sRGB: everything past the vertex stage is.
const paint: vec3<f32> = vec3<f32>(0.55, 0.57, 0.62);

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
    let local = vec4<f32>(vertex.position, 1.0);
    let world = vec3<f32>(
        dot(instance.world0, local),
        dot(instance.world1, local),
        dot(instance.world2, local),
    );
    var out: VertexOut;
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(world, 1.0);
    // This station scales uniformly, so the basis is its own normal matrix and
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
    return vec4<f32>(paint / PI * sceneFrame.sunColor.rgb * nDotL + paint * ambient, 1.0);
}
`

// newRepaintMaterial builds the replacement: one forward entry, opaque, no
// parameters.
//
// It is a plain value with no GPU handle in it, so the demo builds it once at
// construction rather than waiting for a backend, and passes the same slice on
// every frame: scene keys a material by content, so this interns to one id.
func newRepaintMaterial() scene.Material {
	return scene.Material{{
		Descr: gfx.MaterialWithState(gfx.ShaderWithText(repaintShaderSource), gfx.StateOpaque3D),
	}}
}
