package headless

import (
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// vertexEntryPoint is the vertex function every gfx pipeline is built against.
const vertexEntryPoint = "vs_main"

// reflectShaderLayout is the reflection gogpu's backend runs, repeated here
// because it lives in gogpu's internal package: naga's parse and lowering, then
// every buffer, texture and sampler binding and the vertex stage's inputs. It
// has to be the real reflection rather than a mirror, because a compiled
// program's layout is what every draw params set built over it binds against:
// a binding reflected short here is a binding every draw through it drops, and
// a shader naga refuses is one the real backend refuses too.
func reflectShaderLayout(source string) (gfx.ShaderLayout, error) {
	ast, err := naga.Parse(source)
	if err != nil {
		return gfx.ShaderLayout{}, err
	}
	mod, err := wgsl.Lower(ast)
	if err != nil {
		return gfx.ShaderLayout{}, err
	}
	return shaderLayoutFrom(mod), nil
}

// shaderLayoutFrom extracts every uniform and storage binding, told by its
// address space whatever type it is declared at, and every texture and sampler
// binding from a lowered module.
func shaderLayoutFrom(mod *ir.Module) gfx.ShaderLayout {
	var layout gfx.ShaderLayout
	for _, gv := range mod.GlobalVariables {
		if gv.Binding == nil {
			continue
		}
		group, binding := int(gv.Binding.Group), int(gv.Binding.Binding)
		switch gv.Space {
		case ir.SpaceStorage:
			kind := gfx.ResourceStorageBuffer
			if gv.Access == ir.StorageReadWrite {
				kind |= gfx.ResourceWritable
			}
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: kind,
				Group: group, Binding: binding, Members: bufferMembers(mod, gv.Type),
			})
			continue
		case ir.SpaceUniform:
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: gfx.ResourceUniformBuffer, Group: group, Binding: binding,
				Size: int(ir.TypeSize(mod, gv.Type)), Members: bufferMembers(mod, gv.Type),
			})
			continue
		}
		switch inner := mod.Types[gv.Type].Inner.(type) {
		case ir.ImageType:
			view := gfx.TextureView2D
			if inner.Dim == ir.Dim2D && inner.Arrayed {
				view = gfx.TextureView2DArray
			}
			kind := gfx.ResourceTexture
			if inner.Class == ir.ImageClassDepth {
				kind |= gfx.ResourceDepth
			}
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: kind, TextureView: view,
				Group: group, Binding: binding,
			})
		case ir.SamplerType:
			kind := gfx.ResourceSampler
			if inner.Comparison {
				kind |= gfx.ResourceComparison
			}
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: kind,
				Group: group, Binding: binding,
			})
		}
	}
	layout.VertexInputs = vertexInputs(mod)
	return layout
}

// vertexInputs reflects every @location the vs_main entry point declares,
// whether as an argument of its own or as a member of a struct argument.
func vertexInputs(mod *ir.Module) []gfx.ShaderVertexInput {
	var inputs []gfx.ShaderVertexInput
	for i := range mod.EntryPoints {
		entry := &mod.EntryPoints[i]
		if entry.Stage != ir.StageVertex || entry.Name != vertexEntryPoint {
			continue
		}
		for _, arg := range entry.Function.Arguments {
			if arg.Binding != nil {
				if input, ok := vertexInput(mod, arg.Name, arg.Type, *arg.Binding); ok {
					inputs = append(inputs, input)
				}
				continue
			}
			structure, ok := mod.Types[arg.Type].Inner.(ir.StructType)
			if !ok {
				continue
			}
			for _, member := range structure.Members {
				if member.Binding == nil {
					continue
				}
				if input, ok := vertexInput(mod, member.Name, member.Type, *member.Binding); ok {
					inputs = append(inputs, input)
				}
			}
		}
	}
	return inputs
}

// vertexInput reduces one @location declaration to the scalar kind it decodes
// to and its component count, and reports anything else as not an input.
func vertexInput(
	mod *ir.Module, name string, typ ir.TypeHandle, binding ir.Binding,
) (gfx.ShaderVertexInput, bool) {
	location, ok := binding.(ir.LocationBinding)
	if !ok {
		return gfx.ShaderVertexInput{}, false
	}
	input := gfx.ShaderVertexInput{Name: name, Location: int(location.Location)}
	switch inner := mod.Types[typ].Inner.(type) {
	case ir.ScalarType:
		input.Kind, input.Count = vertexScalar(inner.Kind), 1
	case ir.VectorType:
		input.Kind, input.Count = vertexScalar(inner.Scalar.Kind), int(inner.Size)
	default:
		return gfx.ShaderVertexInput{}, false
	}
	if input.Kind == gfx.VertexScalarNone {
		return gfx.ShaderVertexInput{}, false
	}
	return input, true
}

func vertexScalar(kind ir.ScalarKind) gfx.VertexScalar {
	switch kind {
	case ir.ScalarFloat:
		return gfx.VertexScalarFloat
	case ir.ScalarUint:
		return gfx.VertexScalarUint
	case ir.ScalarSint:
		return gfx.VertexScalarSint
	}
	return gfx.VertexScalarNone
}

// bufferMembers is the member layout of a buffer binding declared at a struct,
// one level deep, and nothing for one declared at any other type.
func bufferMembers(mod *ir.Module, typ ir.TypeHandle) []gfx.StorageMember {
	structure, ok := mod.Types[typ].Inner.(ir.StructType)
	if !ok {
		return nil
	}
	members := make([]gfx.StorageMember, 0, len(structure.Members))
	for _, member := range structure.Members {
		reflected := gfx.StorageMember{Name: member.Name, Offset: int(member.Offset)}
		if array, ok := mod.Types[member.Type].Inner.(ir.ArrayType); ok {
			reflected.Stride = int(array.Stride)
			if array.Size.Constant != nil {
				reflected.Count = int(*array.Size.Constant)
			}
		}
		members = append(members, reflected)
	}
	return members
}
