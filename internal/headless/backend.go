package headless

import (
	"github.com/dvoyni/cog/gfx"
)

// Backend is a gfx.Backend that mints ids and records what the frame asked the
// GPU to do. It renders nothing: every number a demo test asserts was decided
// in scene's update-thread flush, before anything here was called.
type Backend struct {
	nextTexture gfx.TextureID
	nextBuffer  gfx.BufferID
	nextID      uint32
	// shaders remembers each shader's label, which is its resource path, so
	// ShaderLayout can answer for the right one.
	shaders map[gfx.ShaderID]string

	Passes   []gfx.GpuPassDesc
	Draws    []DrawCall
	Presents int
	Bakes    int
}

// DrawCall is one draw as it reached the backend.
type DrawCall struct {
	First, Count, Instances, FirstInstance int
	Indexed                                bool
}

// sceneShaderPath is the bundled scene shader, the one shader whose reflected
// bindings scene's own packing depends on. The real WGSL is reflected for real
// in the wgpu package, the only tree with a front end; here the layout stands
// in so scene's bindings reach the backend at the group and binding the shader
// declares.
const sceneShaderPath = "builtin/scene/scene.wgsl"

// sceneShaderLayout mirrors scene/builtin/scene/scene.wgsl's declared bindings.
var sceneShaderLayout = gfx.ShaderLayout{Resources: []gfx.ShaderResource{
	{Name: "sceneFrame", StorageBuffer: true, Group: 0, Binding: 0},
	{Name: "sceneInstances", StorageBuffer: true, Group: 0, Binding: 1},
	{Name: "scenePbrMaterial", StorageBuffer: true, Group: 1, Binding: 0},
	{Name: "baseColorTexture", Group: 1, Binding: 1},
	{Name: "baseColorSampler", Sampler: true, Group: 1, Binding: 2},
	{Name: "metallicRoughnessTexture", Group: 1, Binding: 3},
	{Name: "metallicRoughnessSampler", Sampler: true, Group: 1, Binding: 4},
	{Name: "normalTexture", Group: 1, Binding: 5},
	{Name: "normalSampler", Sampler: true, Group: 1, Binding: 6},
	{Name: "occlusionTexture", Group: 1, Binding: 7},
	{Name: "occlusionSampler", Sampler: true, Group: 1, Binding: 8},
	{Name: "emissiveTexture", Group: 1, Binding: 9},
	{Name: "emissiveSampler", Sampler: true, Group: 1, Binding: 10},
}}

func (b *Backend) NewTexture() gfx.TextureID { b.nextTexture++; return b.nextTexture }
func (b *Backend) NewBuffer() gfx.BufferID   { b.nextBuffer++; return b.nextBuffer }

func (b *Backend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	b.nextID++
	return gfx.SamplerID(b.nextID), nil
}

func (b *Backend) FreeSampler(gfx.SamplerID) {}

func (b *Backend) NewShader(desc gfx.ShaderDesc) (gfx.ShaderID, error) {
	b.nextID++
	id := gfx.ShaderID(b.nextID)
	if b.shaders == nil {
		b.shaders = map[gfx.ShaderID]string{}
	}
	b.shaders[id] = desc.Label
	return id, nil
}

func (b *Backend) FreeShader(gfx.ShaderID) {}

// ShaderLayout answers for the bundled scene shader and reports nothing for
// every other shader. Canvas's own bindings are not what a demo test asserts,
// and a fake union layout would bind canvas's parameters at scene's slots.
func (b *Backend) ShaderLayout(id gfx.ShaderID) gfx.ShaderLayout {
	if b.shaders[id] == sceneShaderPath {
		return sceneShaderLayout
	}
	return gfx.ShaderLayout{}
}

func (b *Backend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	b.nextID++
	return gfx.PipelineID(b.nextID), nil
}

func (b *Backend) FreePipeline(gfx.PipelineID) {}

// ScreenFramebuffer reports the physical surface the present pass draws into,
// which is also the size the frame buffer is allocated at.
func (b *Backend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return 1, FramebufferWidth, FramebufferHeight
}

// Limits reports the web floor rather than a generous native device's, so a
// headless run fails on a limit a browser would fail on.
func (b *Backend) Limits() gfx.Limits { return gfx.DefaultLimits }

func (b *Backend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	b.nextID++
	return gfx.TextureViewID(b.nextID)
}

func (b *Backend) Execute(queue *gfx.GpuQueue) {
	queue.ReplayBakes(b)
	queue.ReplayPasses(b)
	queue.ReplayReleases(b)
}

func (b *Backend) BeginPass(desc gfx.GpuPassDesc) gfx.RenderPass {
	b.Passes = append(b.Passes, desc)
	return b
}

func (b *Backend) EndPass(gfx.RenderPass) {}
func (b *Backend) Present()               { b.Presents++ }

func (b *Backend) BakeBuffer(gfx.BufferID, gfx.BufferKind, int, []byte)                 { b.Bakes++ }
func (b *Backend) BakeTexture(gfx.TextureID, int, int, gfx.TextureFormat, []byte, bool) {}
func (b *Backend) AllocateTexture(gfx.TextureID, gfx.TextureDesc)                       {}
func (b *Backend) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte)                 {}

func (b *Backend) SetPipeline(gfx.PipelineID)                                          {}
func (b *Backend) SetParams([]byte)                                                    {}
func (b *Backend) SetTexture(gfx.TextureID, int, int)                                  {}
func (b *Backend) SetSampler(gfx.SamplerID, int, int)                                  {}
func (b *Backend) SetVertexBuffer(gfx.BufferID, int)                                   {}
func (b *Backend) SetIndexBuffer(gfx.BufferID, int)                                    {}
func (b *Backend) SetBuffer(group, binding int, buffer gfx.BufferID, offset, size int) {}

func (b *Backend) Draw(first, count, instances, firstInstance int, indexed bool) {
	b.Draws = append(b.Draws, DrawCall{
		First: first, Count: count, Instances: instances,
		FirstInstance: firstInstance, Indexed: indexed,
	})
}

func (b *Backend) ReleaseBuffer(gfx.BufferID)   {}
func (b *Backend) ReleaseTexture(gfx.TextureID) {}
