package headless

import (
	"github.com/dvoyni/cog/gfx"
)

// Backend is a gfx.Backend that mints ids and records what the frame asked the
// GPU to do. It renders nothing: every number a demo test asserts was decided
// in scene's update-thread flush, before anything here was called.
type Backend struct {
	BakedTextures  int
	MippedTextures int
	nextTexture    gfx.TextureID
	nextBuffer     gfx.BufferID
	nextID         uint32
	// shaders remembers each shader's label, which is its resource path, so
	// ShaderLayout can answer for the right one.
	shaders map[gfx.ShaderID]string

	Passes   []gfx.GpuPassDesc
	Draws    []DrawCall
	Presents int
	Bakes    int
	// Transitions is every texture barrier gfx placed, in order, each tagged
	// with the pass it precedes. It is the only observable for the
	// render-then-sample ordering: the hazard it fixes is invisible to a
	// headless run and to any capture taken while the app is redrawing.
	Transitions []PlacedTransition
	// Buffers is every storage-buffer binding the frame made, in the order it
	// made them. It is recorded for the one assertion a demo carrying its own
	// WGSL cannot make otherwise: which of scene's per-draw parameters its
	// material actually bound, and so that declaring fewer of them is safe.
	Buffers []BufferBinding
	// Pipelines is every pipeline the frame created, in creation order. It is
	// recorded because MaterialState is where glTF's alphaMode, doubleSided and
	// a mirrored node transform actually land - as Blend, DepthWrite, Cull and
	// FrontFace - and a pipeline description is the only place a test with no
	// GPU can read them back. gfx interns pipelines, so this is one entry per
	// distinct state the frame asked for rather than one per draw.
	Pipelines []gfx.PipelineDesc
	// pipelines is the same descriptions by id, and current the pipeline the
	// replay last set, so a binding can say which pipeline read it.
	pipelines map[gfx.PipelineID]gfx.PipelineDesc
	current   gfx.PipelineID
	// Baked is the bytes of every durable buffer upload, by id. Scene's frame
	// arenas reach the backend this way, so it is how a test reads back what
	// the flush packed - the per-pass sceneFrame block above all, whose
	// sixteen-light array is the only record of which lights survived the cap.
	// Nothing else can see that: PassView reports how many were packed and the
	// drop is silent by design.
	//
	// The bytes are copied rather than retained, because the queue's arenas are
	// reused frame to frame and a retained slice would report the newest frame
	// for every step a test took.
	Baked map[gfx.BufferID][]byte
	// TextShaderLayout is the layout reported for a shader built from inline
	// source rather than from a resource path. Only a demo with its own WGSL
	// has one, and only that demo knows what it declares, so a test sets this
	// rather than the file mirroring it the way it mirrors the bundled
	// shader's below. Left zero, an inline shader reflects nothing, which is
	// what every demo that has none wants.
	TextShaderLayout gfx.ShaderLayout
}

// BufferBinding is one storage buffer bound to one slot of one draw.
//
// Buffer names the buffer the range came from, which is what BoundBytes needs
// to answer with the bytes rather than only the offsets, and Pipeline the
// pipeline that was bound when the range was. Neither is part of what a test
// comparing bindings writes: a demo asking which slots its material bound
// compares Group and Binding alone, and a demo asking which of them scene's own
// shader read filters on Pipeline first.
type BufferBinding struct {
	Group, Binding int
	Buffer         gfx.BufferID
	Pipeline       gfx.PipelineID
	Offset, Size   int
}

// DrawCall is one draw as it reached the backend.
//
// Pipeline is the pipeline that was bound when the draw was made, which is the
// only thing separating scene's draws from canvas's in a frame that also drew a
// HUD: a draw carries no label of its own, and one canvas Text op is a single
// instanced draw of a few hundred glyphs, which is indistinguishable by its
// numbers alone from an instanced field. Filter on IsScenePipeline first.
type DrawCall struct {
	First, Count, Instances, FirstInstance int
	Indexed                                bool
	Pipeline                               gfx.PipelineID
}

// sceneShaderPath is the bundled scene shader, the one shader whose reflected
// bindings scene's own packing depends on. The real WGSL is reflected for real
// in the wgpu package, the only tree with a front end; here the layout stands
// in so scene's bindings reach the backend at the group and binding the shader
// declares.
const sceneShaderPath = "builtin/scene/scene.wgsl"

// sceneShaderLayout mirrors scene/builtin/scene/scene.wgsl's declared bindings,
// all seventeen of them.
//
// It has to be all seventeen rather than the ones a given assertion cares
// about, because gfx resolves a recorder's parameters by name against the
// reflected layout: a binding this list omits is silently dropped on the way to
// the backend, which is indistinguishable here from a flush that never packed
// it. Group 2 and sceneAnim were missing until the animated demo needed to
// assert that a skinned draw binds its poses, and the omission read as scene
// not binding them at all.
//
// The seven storage buffers are also the whole of scene's budget against the
// browser floor of eight, so a mirror that has drifted short of the real
// shader would let a demo pass a limit check the browser will fail.
var sceneShaderLayout = gfx.ShaderLayout{Resources: []gfx.ShaderResource{
	{Name: "sceneFrame", StorageBuffer: true, Group: 0, Binding: 0},
	{Name: "sceneInstances", StorageBuffer: true, Group: 0, Binding: 1},
	{Name: "sceneAnim", StorageBuffer: true, Group: 0, Binding: 2},
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
	{Name: "scenePoses", StorageBuffer: true, Group: 2, Binding: 0},
	{Name: "sceneSkinJoints", StorageBuffer: true, Group: 2, Binding: 1},
	{Name: "sceneMorphDeltas", StorageBuffer: true, Group: 2, Binding: 2},
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

// textShaderLabel is the label gfx gives a shader built from inline source.
// Every resource shader is labelled by its path instead, so this is exactly the
// set of shaders a demo wrote itself.
const textShaderLabel = "gfx.shader"

// ShaderLayout answers for the bundled scene shader, for whatever inline shader
// the test declared, and for nothing else. Canvas's own bindings are not what a
// demo test asserts, and a fake union layout would bind canvas's parameters at
// scene's slots.
func (b *Backend) ShaderLayout(id gfx.ShaderID) gfx.ShaderLayout {
	switch b.shaders[id] {
	case sceneShaderPath:
		return sceneShaderLayout
	case textShaderLabel:
		return b.TextShaderLayout
	}
	return gfx.ShaderLayout{}
}

func (b *Backend) NewPipeline(desc gfx.PipelineDesc) (gfx.PipelineID, error) {
	b.nextID++
	id := gfx.PipelineID(b.nextID)
	b.Pipelines = append(b.Pipelines, desc)
	if b.pipelines == nil {
		b.pipelines = map[gfx.PipelineID]gfx.PipelineDesc{}
	}
	b.pipelines[id] = desc
	return id, nil
}

// PipelineOf is one pipeline's description, by the id a binding names.
func (b *Backend) PipelineOf(id gfx.PipelineID) gfx.PipelineDesc { return b.pipelines[id] }

// ShaderPath is the resource path a shader was built from, which is its label.
// A shader built from inline source has textShaderLabel instead, so this is also
// how a test tells a demo's own WGSL from a bundled shader.
func (b *Backend) ShaderPath(id gfx.ShaderID) string { return b.shaders[id] }

// SceneShaderPath is the bundled scene shader's resource path. It is exported
// because it is how a test picks scene's own pipelines and bindings out of a
// frame that also drew a HUD: gfx labels every pipeline "gfx.pipeline", and the
// shader behind it is what separates them.
const SceneShaderPath = sceneShaderPath

// IsScenePipeline reports whether a pipeline was built from the bundled scene
// shader.
func (b *Backend) IsScenePipeline(id gfx.PipelineID) bool {
	desc, ok := b.pipelines[id]
	return ok && b.shaders[desc.Shader] == sceneShaderPath
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

// TransitionTextures records the barriers gfx placed, each tagged with the pass
// it precedes, so a demo test can assert that a render target it composites was
// actually ordered against the pass that wrote it. Without the barrier the
// failure is a Vulkan-only flicker that no headless run and no screen capture
// can see, so this is the only place it is observable at all.
//
// Like Passes and Draws, these accumulate across every frame since the engine
// started - scan backwards.
func (b *Backend) TransitionTextures(transitions []gfx.TextureTransition) {
	for _, transition := range transitions {
		b.Transitions = append(b.Transitions, PlacedTransition{
			TextureTransition: transition, BeforePass: len(b.Passes),
		})
	}
}

// PlacedTransition is one barrier and the index into Passes of the pass it was
// recorded ahead of.
type PlacedTransition struct {
	gfx.TextureTransition
	BeforePass int
}

func (b *Backend) BakeBuffer(id gfx.BufferID, _ gfx.BufferKind, _ int, data []byte) {
	b.Bakes++
	if b.Baked == nil {
		b.Baked = map[gfx.BufferID][]byte{}
	}
	b.Baked[id] = append(b.Baked[id][:0], data...)
}

// BakeTexture counts durable texture uploads, which is how a test observes
// scene's texture cache: nine glTF textures over three images have to reach
// the GPU as three, not nine.
//
// MippedTextures counts the uploads that asked for a mip chain. It is recorded
// apart from the total because it is the only observable for one of the four
// WebGPU gaps the loader papers over - there is no mipmap generation API, so
// the chain is a CPU box filter built at load and handed over with the base
// level - and a count of uploads alone cannot tell a filtered texture from an
// unfiltered one.
func (b *Backend) BakeTexture(
	_ gfx.TextureID, _, _ int, _ gfx.TextureFormat, _ []byte, mipmaps bool,
) {
	b.BakedTextures++
	if mipmaps {
		b.MippedTextures++
	}
}
func (b *Backend) AllocateTexture(gfx.TextureID, gfx.TextureDesc)       {}
func (b *Backend) UpdateTexture(gfx.TextureID, int, gfx.Region, []byte) {}

func (b *Backend) SetPipeline(id gfx.PipelineID)      { b.current = id }
func (b *Backend) SetParams([]byte)                   {}
func (b *Backend) SetTexture(gfx.TextureID, int, int) {}
func (b *Backend) SetSampler(gfx.SamplerID, int, int) {}
func (b *Backend) SetVertexBuffer(gfx.BufferID, int)  {}
func (b *Backend) SetIndexBuffer(gfx.BufferID, int)   {}
func (b *Backend) SetBuffer(group, binding int, buffer gfx.BufferID, offset, size int) {
	b.Buffers = append(b.Buffers, BufferBinding{
		Group: group, Binding: binding, Buffer: buffer, Pipeline: b.current,
		Offset: offset, Size: size,
	})
}

// BoundBytes is the bytes one binding read, sliced out of the buffer it named.
// It is nil when the buffer was never baked, which is what a temporary that
// reached the backend by some other route looks like.
func (b *Backend) BoundBytes(binding BufferBinding) []byte {
	data := b.Baked[binding.Buffer]
	if binding.Offset < 0 || binding.Offset+binding.Size > len(data) {
		return nil
	}
	return data[binding.Offset : binding.Offset+binding.Size]
}

func (b *Backend) Draw(first, count, instances, firstInstance int, indexed bool) {
	b.Draws = append(b.Draws, DrawCall{
		First: first, Count: count, Instances: instances,
		FirstInstance: firstInstance, Indexed: indexed, Pipeline: b.current,
	})
}

func (b *Backend) ReleaseBuffer(gfx.BufferID)   {}
func (b *Backend) ReleaseTexture(gfx.TextureID) {}
