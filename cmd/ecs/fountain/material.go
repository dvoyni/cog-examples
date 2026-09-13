package main

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/ecsscene"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// Vertex is the demo's own layout, the procedural example's approach: a custom
// layout needs a custom material, and both are this file.
type Vertex struct {
	Position m.Vec3
	Normal   m.Vec3
}

func (Vertex) VertexLayout() []gfx.VertexAttr { return vertexLayout[:] }

var vertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Normal)), gfx.Float32x3),
}

// cubeGeometry is a unit cube about the origin, four vertices a face.
func cubeGeometry() ([]Vertex, []uint32) {
	faces := [...]struct{ normal, u, v m.Vec3 }{
		{m.Vec3{X: 1}, m.Vec3{Y: 1}, m.Vec3{Z: 1}},
		{m.Vec3{X: -1}, m.Vec3{Z: 1}, m.Vec3{Y: 1}},
		{m.Vec3{Y: 1}, m.Vec3{Z: 1}, m.Vec3{X: 1}},
		{m.Vec3{Y: -1}, m.Vec3{X: 1}, m.Vec3{Z: 1}},
		{m.Vec3{Z: 1}, m.Vec3{X: 1}, m.Vec3{Y: 1}},
		{m.Vec3{Z: -1}, m.Vec3{Y: 1}, m.Vec3{X: 1}},
	}
	vertices := make([]Vertex, 0, 24)
	indices := make([]uint32, 0, 36)
	for _, face := range faces {
		base := uint32(len(vertices))
		centre := face.normal.MulS(0.5)
		for _, corner := range [...][2]float32{{-0.5, -0.5}, {0.5, -0.5}, {0.5, 0.5}, {-0.5, 0.5}} {
			position := centre.Add(face.u.MulS(corner[0])).Add(face.v.MulS(corner[1]))
			vertices = append(vertices, Vertex{Position: position, Normal: face.normal})
		}
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}
	return vertices, indices
}

// The basin: a flat disc on the ground.
const (
	basinRadius   = 6.5
	basinSegments = 96
)

var basinBounds = m.Vec4{W: basinRadius}

func discGeometry() ([]Vertex, []uint32) {
	vertices := make([]Vertex, 0, basinSegments+1)
	indices := make([]uint32, 0, basinSegments*3)
	vertices = append(vertices, Vertex{Normal: m.Vec3{Y: 1}})
	for i := range basinSegments {
		angle := 2 * math.Pi * float64(i) / basinSegments
		vertices = append(vertices, Vertex{
			Position: m.Vec3{X: basinRadius * float32(math.Cos(angle)), Z: basinRadius * float32(math.Sin(angle))},
			Normal:   m.Vec3{Y: 1},
		})
		indices = append(indices, 0, uint32(1+(i+1)%basinSegments), uint32(1+i))
	}
	return vertices, indices
}

// tagGround is the camera's first pass. Only the basin's material has an entry
// for it, so it draws the basin alone, beneath everything the forward pass
// draws.
const tagGround scene.PassTag = "ground"

// moteTintParam is the uniform member the mote shader reads its colour from.
const moteTintParam = "moteTint"

// sharedMote is the one mote material. Every mote's Material Component holds
// the same value, so scene keys them to one material, and the per-mote colour
// rides in Params instead: a colour inside the Material would make every mote a
// material of its own, fading every frame.
var sharedMote = ecsscene.Material{Tags: ecs.NewList(ecsscene.MaterialTag{
	Tag:    scene.TagForward,
	Shader: gfx.ShaderWithText(scenePrelude + moteShader),
	State:  twoSided(gfx.StateOpaque3D),
})}

func moteMaterial() ecsscene.Material { return sharedMote }

// basinMaterial serves two pass tags: lit stone in the ground pass, and
// ripples blended over it in the forward pass, where the motes and the fox
// occlude them.
func basinMaterial() ecsscene.Material {
	return ecsscene.Material{Tags: ecs.NewList(
		ecsscene.MaterialTag{
			Tag:    tagGround,
			Shader: gfx.ShaderWithText(scenePrelude + stoneShader),
			State:  twoSided(gfx.StateOpaque3D),
		},
		ecsscene.MaterialTag{
			Tag:    scene.TagForward,
			Shader: gfx.ShaderWithText(scenePrelude + rippleShader),
			State:  twoSided(gfx.StateTransparent3D),
		},
	)}
}

func twoSided(state gfx.MaterialState) gfx.MaterialState {
	state.Cull = gfx.CullNone
	return state
}

// scenePrelude is the part of scene's shader-side contract these shaders read,
// copied because scene publishes no shader API: the frame block with its
// lights, the instance record, and the lighting every surface here shares.
const scenePrelude = `
struct SceneLight {
    position: vec3<f32>,
    invRange4: f32,
    direction: vec3<f32>,
    spotScale: f32,
    color: vec3<f32>,
    spotOffset: f32,
};

struct SceneFrame {
    view: mat4x4<f32>,
    projection: mat4x4<f32>,
    viewProjection: mat4x4<f32>,
    cameraPosition: vec4<f32>,
    viewDirection: vec4<f32>,
    sunDirection: vec4<f32>,
    sunColor: vec4<f32>,
    ambientSky: vec4<f32>,
    ambientGround: vec4<f32>,
    lightCount: u32,
    lights: array<SceneLight, 16>,
};

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

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;
@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;

const PI: f32 = 3.14159265359;

struct VertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
};

struct VertexOut {
    @builtin(position) clipPosition: vec4<f32>,
    @location(0) world: vec3<f32>,
    @location(1) normal: vec3<f32>,
};

@vertex
fn vs_main(vertex: VertexIn, @builtin(instance_index) index: u32) -> VertexOut {
    let instance = sceneInstances.data[index];
    let local = vec4<f32>(vertex.position, 1.0);
    let world = vec3<f32>(dot(instance.world0, local), dot(instance.world1, local), dot(instance.world2, local));
    // The cofactor matrix is the inverse-transpose up to a scale, which the
    // normalize removes: a mote is scaled non-uniformly.
    let c0 = vec3<f32>(instance.world0.x, instance.world1.x, instance.world2.x);
    let c1 = vec3<f32>(instance.world0.y, instance.world1.y, instance.world2.y);
    let c2 = vec3<f32>(instance.world0.z, instance.world1.z, instance.world2.z);
    var out: VertexOut;
    out.clipPosition = sceneFrame.viewProjection * vec4<f32>(world, 1.0);
    out.world = world;
    out.normal = normalize(cross(c1, c2) * vertex.normal.x + cross(c2, c0) * vertex.normal.y + cross(c0, c1) * vertex.normal.z);
    return out;
}

fn lambert(albedo: vec3<f32>, position: vec3<f32>, normal: vec3<f32>) -> vec3<f32> {
    var radiance = sceneFrame.sunColor.rgb * max(dot(normal, -sceneFrame.sunDirection.xyz), 0.0);
    for (var i = 0u; i < sceneFrame.lightCount; i++) {
        let light = sceneFrame.lights[i];
        let toLight = light.position - position;
        let d2 = dot(toLight, toLight);
        let direction = toLight * inverseSqrt(max(d2, 1e-12));
        let window = saturate(1.0 - d2 * d2 * light.invRange4);
        let cone = saturate(dot(-direction, light.direction) * light.spotScale + light.spotOffset);
        radiance += light.color * (window * cone / max(d2, 1e-6)) * max(dot(normal, direction), 0.0);
    }
    let ambient = mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb, normal.y * 0.5 + 0.5);
    return albedo / PI * radiance + albedo * ambient;
}
`

const moteShader = `
struct MoteParams {
    moteTint: vec4<f32>,
};

@group(1) @binding(0) var<uniform> mote: MoteParams;

@fragment
fn fs_main(in: VertexOut, @builtin(front_facing) front: bool) -> @location(0) vec4<f32> {
    var normal = normalize(in.normal);
    if !front {
        normal = -normal;
    }
    let tint = mote.moteTint.rgb;
    return vec4<f32>(lambert(tint, in.world, normal) + tint * 0.35, 1.0);
}
`

const stoneShader = `
@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    let r = length(in.world.xz);
    let rim = smoothstep(5.6, 6.4, r);
    let albedo = mix(vec3<f32>(0.30, 0.31, 0.33), vec3<f32>(0.52, 0.50, 0.46), rim);
    return vec4<f32>(lambert(albedo, in.world, vec3<f32>(0.0, 1.0, 0.0)), 1.0);
}
`

const rippleShader = `
@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    let r = length(in.world.xz);
    let ring = pow(0.5 + 0.5 * cos(r * 9.0), 12.0);
    let fade = 1.0 - smoothstep(1.0, 5.6, r);
    return vec4<f32>(0.35, 0.75, 1.0, ring * fade * 0.55);
}
`
