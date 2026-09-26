package headless

import (
	"strings"
	"sync/atomic"

	"github.com/dvoyni/cog/slots/gfx"
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
	// nextShader is every shader's id. It is its own atomic counter, as
	// gogpu's is, because ResourceQueue.NewShader reserves on the update thread
	// while the replay creates on the render thread.
	nextShader atomic.Uint32
	// shaders remembers each shader's label, which is its resource path and
	// supply, so ShaderPath and ShaderSupply can answer for it.
	shaders map[gfx.ShaderID]string
	// created is the reflected layout of every shader CreateShader made, by
	// id, which PipelineResources answers with.
	created map[gfx.ShaderID]gfx.ShaderLayout
	// formats remembers the format each texture was baked or allocated in, so
	// TextureFormat can key a pipeline to the target it renders into.
	formats map[gfx.TextureID]gfx.TextureFormat
	// allocated remembers each allocated texture's description, so an upload
	// that fills a whole layer counts as a bake and one into a mipmapped
	// texture counts toward MippedTextures. An atlas's region uploads do not.
	allocated map[gfx.TextureID]gfx.TextureDesc

	Passes   []gfx.PassDesc
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
	// recorded because DrawState is where glTF's alphaMode, doubleSided and
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
	// Nothing else can see that: the drop is silent by design.
	//
	// The bytes are copied rather than retained, because the queue's arenas are
	// reused frame to frame and a retained slice would report the newest frame
	// for every step a test took.
	Baked map[gfx.BufferID][]byte
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
//
// Pass is the index into Backend.Passes of the pass the draw was made in, which
// is what says which of a camera's passes drew it: a pass carries the label its
// renderer gave it, and a draw carries none.
type DrawCall struct {
	First, Count, Instances, FirstInstance int
	Indexed                                bool
	Pipeline                               gfx.PipelineID
	Pass                                   int
}

// sceneShaderPath is the bundled scene shader, the one shader whose reflected
// bindings scene's own packing depends on.
const sceneShaderPath = "builtin/model/scene.wgsl"

// Ready is true from the start: the fake has no device to wait for.
func (b *Backend) Ready() bool { return true }

func (b *Backend) NewTexture() gfx.TextureID { b.nextTexture++; return b.nextTexture }
func (b *Backend) NewBuffer() gfx.BufferID   { b.nextBuffer++; return b.nextBuffer }

func (b *Backend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	b.nextID++
	return gfx.SamplerID(b.nextID), nil
}

func (b *Backend) FreeSampler(gfx.SamplerID) {}

func (b *Backend) FreeShader(gfx.ShaderID) {}

// ReserveShader mints the id ResourceQueue.NewShader hands out.
func (b *Backend) ReserveShader() gfx.ShaderID {
	return gfx.ShaderID(b.nextShader.Add(1))
}

// CreateShader creates the module of a reserved id. It remembers the label, so
// ShaderPath and IsScenePipeline answer for it, and it reflects the bytes
// again, as gogpu does, so a module naga refuses is refused here too and
// PipelineResources answers with what the program was built against.
func (b *Backend) CreateShader(id gfx.ShaderID, desc gfx.ShaderDesc) error {
	layout, err := reflectShaderLayout(string(desc.Code))
	if err != nil {
		return err
	}
	if b.shaders == nil {
		b.shaders = map[gfx.ShaderID]string{}
	}
	if b.created == nil {
		b.created = map[gfx.ShaderID]gfx.ShaderLayout{}
	}
	b.shaders[id] = desc.Label
	b.created[id] = layout
	return nil
}

// ReflectShader is gfx's reflection port, and the real one: naga's parse and
// lowering, as gogpu runs it. It touches no state of the fake, so it is safe
// from any thread, as the port requires.
func (b *Backend) ReflectShader(code []byte) (gfx.ShaderLayout, error) {
	return reflectShaderLayout(string(code))
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

// ShaderPath is the root source a shader was built from. A label carries the
// variant's supply after the path, and this drops it: what a test picks out of a
// frame is scene's shader, whichever variant this draw needed. A shader built
// from inline source has textShaderLabel instead, so this is also how a test
// tells a demo's own WGSL from a bundled shader.
func (b *Backend) ShaderPath(id gfx.ShaderID) string {
	path, _, _ := strings.Cut(b.shaders[id], " [")
	return path
}

// ShaderSupply is the defines and consts a shader was built with, as the label
// spells them, and empty for a shader built with none. It is what a test uses to
// tell one variant of the bundled shader from another.
func (b *Backend) ShaderSupply(id gfx.ShaderID) string {
	_, supply, ok := strings.Cut(b.shaders[id], " [")
	if !ok {
		return ""
	}
	return strings.TrimSuffix(supply, "]")
}

// SceneShaderPath is the bundled scene shader's resource path. It is exported
// because it is how a test picks scene's own pipelines and bindings out of a
// frame that also drew a HUD: gfx labels every pipeline "gfx.pipeline", and the
// shader behind it is what separates them.
const SceneShaderPath = sceneShaderPath

// IsScenePipeline reports whether a pipeline was built from the bundled scene
// shader.
func (b *Backend) IsScenePipeline(id gfx.PipelineID) bool {
	desc, ok := b.pipelines[id]
	return ok && b.ShaderPath(desc.Shader) == sceneShaderPath
}

// PipelineSupply is the supply of the shader a pipeline was built from, which is
// what says which variant of the bundled scene shader it draws. Draws through
// one pipeline all share one variant, so it is also how a test groups a frame's
// bindings by what the shader declared.
func (b *Backend) PipelineSupply(id gfx.PipelineID) string {
	desc, ok := b.pipelines[id]
	if !ok {
		return ""
	}
	return b.ShaderSupply(desc.Shader)
}

// PipelineResources is every binding the shader behind a pipeline declares,
// as naga reflected it when the shader was created. It is how a test says
// which slots a draw through that pipeline had to bind.
func (b *Backend) PipelineResources(id gfx.PipelineID) []gfx.ShaderResource {
	desc, ok := b.pipelines[id]
	if !ok {
		return nil
	}
	return b.created[desc.Shader].Resources
}

func (b *Backend) FreePipeline(gfx.PipelineID) {}

// ScreenFramebuffer reports the physical surface the present pass draws into,
// which is also the size the frame buffer is allocated at.
func (b *Backend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return 1, FramebufferWidth, FramebufferHeight
}

// Limits reports the web floor rather than a generous native device's, so a
// headless run fails on a limit a browser would fail on.
func (b *Backend) Limits() gfx.Limits { return gfx.DefaultLimits() }

// TextureFormat reports the format a texture was baked or allocated in, and
// that a texture this backend has not seen is unknown.
func (b *Backend) TextureFormat(id gfx.TextureID) (gfx.TextureFormat, bool) {
	format, ok := b.formats[id]
	return format, ok
}

func (b *Backend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	b.nextID++
	return gfx.TextureViewID(b.nextID)
}

func (b *Backend) Execute(queue *gfx.Queue) {
	queue.ReplayBakes(b)
	queue.ReplayPasses(b)
	queue.ReplayReleases(b)
}

func (b *Backend) BeginPass(desc gfx.PassDesc) gfx.RenderPass {
	b.Passes = append(b.Passes, desc)
	return b
}

func (b *Backend) EndPass(gfx.RenderPass) {}
func (b *Backend) Present()               { b.Presents++ }

// Capture and TakeCapture exist so a recording backend satisfies gfx's
// interfaces, and do nothing else. This backend records the calls gfx makes and
// rasterizes nothing, so there are no pixels to hand back: TakeCapture always
// says it has none, which is the honest answer rather than an empty image that
// a differencing test could mistake for a frame.
func (b *Backend) Capture(gfx.CaptureDesc) {}

func (b *Backend) TakeCapture() (gfx.Capture, bool) { return gfx.Capture{}, false }

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

func (b *Backend) BakeUniforms([]byte) {}

func (b *Backend) BakeBuffer(id gfx.BufferID, _ gfx.BufferKind, _ int, data []byte) {
	b.Bakes++
	if b.Baked == nil {
		b.Baked = map[gfx.BufferID][]byte{}
	}
	b.Baked[id] = append(b.Baked[id][:0], data...)
}

// BakeTexture and a whole-layer UpdateTexture count texture uploads, which is
// how a test observes scene's texture cache: nine glTF textures over three
// images have to reach the GPU as three, not nine.
//
// MippedTextures counts the uploads that asked for a mip chain or went into a
// texture allocated with one. It is recorded apart from the total because it is
// the only observable for one of the four WebGPU gaps the loader papers over -
// there is no mipmap generation API, so the chain is a CPU box filter built at
// load and handed over with the base level - and a count of uploads alone
// cannot tell a filtered texture from an unfiltered one.
func (b *Backend) BakeTexture(
	id gfx.TextureID, _, _ int, format gfx.TextureFormat, _ []byte, mipmaps bool,
) {
	b.rememberFormat(id, format)
	b.BakedTextures++
	if mipmaps {
		b.MippedTextures++
	}
}
func (b *Backend) AllocateTexture(id gfx.TextureID, desc gfx.TextureDesc) {
	b.rememberFormat(id, desc.Format)
	if b.allocated == nil {
		b.allocated = map[gfx.TextureID]gfx.TextureDesc{}
	}
	b.allocated[id] = desc
}
func (b *Backend) UpdateTexture(id gfx.TextureID, _ int, region gfx.Region, _ []byte) {
	desc := b.allocated[id]
	if region != (gfx.Region{Width: desc.Width, Height: desc.Height}) {
		return
	}
	b.BakedTextures++
	if desc.Mipmaps {
		b.MippedTextures++
	}
}

func (b *Backend) rememberFormat(id gfx.TextureID, format gfx.TextureFormat) {
	if b.formats == nil {
		b.formats = map[gfx.TextureID]gfx.TextureFormat{}
	}
	b.formats[id] = format
}

func (b *Backend) SetPipeline(id gfx.PipelineID)      { b.current = id }
func (b *Backend) SetUniformBlock(int, int, int, int) {}
func (b *Backend) SetTexture(gfx.TextureID, int, int) {}
func (b *Backend) SetSampler(gfx.SamplerID, int, int) {}
func (b *Backend) SetVertexBuffer(gfx.BufferID, int)  {}

// SetIndexBuffer takes the width scene derived from the mesh's vertex count.
// A recording backend has no index buffer to bind, so the width is recorded
// nowhere - it is here because gfx.RenderPass carries it.
func (b *Backend) SetIndexBuffer(gfx.BufferID, int, gfx.IndexWidth) {}
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
		Pass: len(b.Passes) - 1,
	})
}

func (b *Backend) ReleaseBuffer(gfx.BufferID)   {}
func (b *Backend) ReleaseTexture(gfx.TextureID) {}
