package main

// THROWAWAY. The two surfaces the ladder is judged on, and the two vertex
// structs that carry every rung at once.
//
// Carrying all four encodings in one vertex is the trick that makes this a fair
// A/B: one mesh, one bake, one set of positions, and the only thing that
// changes between the pictures is which @location the material reads. Nothing
// about the geometry, the camera or the lighting can drift between the two
// frames being compared, because there is only one of each.

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// LadderVertex carries a normal four ways: exactly, and at the three rungs of
// the octahedral ladder. A shipping vertex would carry one.
// The field order is not arbitrary, and getting it wrong cost this prototype an
// hour. gfx derives the vertex stride from the layout as the largest attribute
// end offset (gfx/mesh.go stride), while scene uploads unsafe.Sizeof bytes per
// vertex. A struct whose last attribute ends before its own size - which is any
// struct Go pads at the tail - is therefore uploaded at one stride and read at
// another, and every vertex after the first is misaligned. Nothing reports it:
// the geometry simply comes out as garbage triangles. So the widest-aligned
// member goes last, and the 2-byte Oct16 sits in the padding ahead of it.
type LadderVertex struct {
	Position  m.Vec3    // @location(0)  offset 0
	NormalF32 m.Vec3    // @location(1)  offset 12, the reference, 12 bytes
	Oct32     [2]uint16 // @location(2)  offset 24, Unorm16x2, 4 bytes
	Oct16     [2]uint8  // @location(3)  offset 28, Unorm8x2, 2 bytes
	_         [2]uint8  // offset 30, the tail padding, now interior
	Oct22     uint32    // @location(4)  offset 32, ends at 36 == unsafe.Sizeof
}

func (LadderVertex) VertexLayout() []gfx.VertexAttr { return ladderLayout[:] }

var ladderLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(LadderVertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(LadderVertex{}.NormalF32)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(LadderVertex{}.Oct32)), gfx.Unorm16x2),
	gfx.Attr(int(unsafe.Offsetof(LadderVertex{}.Oct16)), gfx.Unorm8x2),
	gfx.Attr(int(unsafe.Offsetof(LadderVertex{}.Oct22)), gfx.Uint32),
}

// newLadderVertex fills every rung from one exact normal.
func newLadderVertex(position, normal m.Vec3) LadderVertex {
	n := normal.Normalize()
	return LadderVertex{
		Position:  position,
		NormalF32: n,
		Oct32:     encodeOct32(n),
		Oct22:     encodeOct22(n),
		Oct16:     encodeOct16(n),
	}
}

// UVVertex carries a texture coordinate three ways: exactly, through a
// per-primitive Unorm16 range, and as a half float.
type UVVertex struct {
	Position m.Vec3 // @location(0)
	Normal   m.Vec3 // @location(1)
	UVf32    m.Vec2 // @location(2)  the reference, 8 bytes
	// UVu16 is the fixed-point candidate. It is carried as a packed Uint32 and
	// unpacked in WGSL rather than declared as Unorm16x2 - the same four bytes
	// and the same arithmetic - because neither form reaches the shader.
	//
	// In THIS layout, whichever attribute carries the fixed-point coordinate
	// arrives as exactly zero, in every combination tried: Unorm16x2 and
	// Uint32, at byte offset 32 and at 36, at @location(2) and at @location(3).
	// The Float32x2 and Float16x2 attributes either side of it arrive intact,
	// the Go-side bytes are correct (probe_test.go prints them), and the same
	// five-attribute pattern works in the sphere's LadderVertex, where
	// @location(3) carries Unorm8x2 and decodes fine. Nothing is reported: no
	// validation error, no log line, no draw skipped. See the "probe" view in
	// material.go, which is what caught it - the red channel stays black while
	// green and blue sweep correctly.
	//
	// This is left as it is rather than worked around further, because the
	// workaround is not the interesting artefact: the silent zero is.
	UVu16 uint32    // @location(3)  Uint32, x | y<<16 - arrives as zero
	UVf16 [2]uint16 // @location(4)  Float16x2, the same 4 bytes with no metadata
}

func (UVVertex) VertexLayout() []gfx.VertexAttr { return uvLayout[:] }

var uvLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(UVVertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(UVVertex{}.Normal)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(UVVertex{}.UVf32)), gfx.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(UVVertex{}.UVu16)), gfx.Uint32),
	gfx.Attr(int(unsafe.Offsetof(UVVertex{}.UVf16)), gfx.Float16x2),
}

// --- the sphere -------------------------------------------------------------

// sphereGeometry builds a UV sphere whose normals are its own positions, so the
// surface is exactly smooth and every normal is exactly representable before
// quantisation. This is the cruellest case for octahedral banding and it is
// built rather than vendored: the measurement found no vendored asset that is
// both smooth and large in frame, and a surface that cannot band answers
// nothing.
//
// The tessellation is deliberately high. Faceting from too few triangles is a
// different artefact that looks like banding and would confound the judgement:
// at 128x64 the geometric step between adjacent normals is far below any rung
// of the ladder, so every terrace on screen is quantisation and not topology.
func sphereGeometry(slices, stacks int, radius float32) ([]LadderVertex, []uint32) {
	vertices := make([]LadderVertex, 0, (slices+1)*(stacks+1))
	for stack := 0; stack <= stacks; stack++ {
		phi := math.Pi * float64(stack) / float64(stacks)
		sinPhi, cosPhi := math.Sin(phi), math.Cos(phi)
		for slice := 0; slice <= slices; slice++ {
			theta := 2 * math.Pi * float64(slice) / float64(slices)
			normal := m.Vec3{
				X: float32(sinPhi * math.Cos(theta)),
				Y: float32(cosPhi),
				Z: float32(sinPhi * math.Sin(theta)),
			}
			vertices = append(vertices, newLadderVertex(normal.MulS(radius), normal))
		}
	}
	indices := make([]uint32, 0, slices*stacks*6)
	stride := uint32(slices + 1)
	for stack := 0; stack < stacks; stack++ {
		for slice := 0; slice < slices; slice++ {
			a := uint32(stack)*stride + uint32(slice)
			b := a + stride
			// Counter-clockwise seen from outside. The other winding is not a
			// harmless mirror: with CullBack it culls the near hemisphere and
			// draws the inside of the far one, whose normals point away from
			// the camera, so the sphere renders as a dark disc with a lit rim
			// and every lighting number looks wrong for the wrong reason.
			indices = append(indices, a, a+1, b, a+1, b+1, b)
		}
	}
	return vertices, indices
}

// --- the band ---------------------------------------------------------------

// bandGeometry builds a flat strip in the XY plane, width wide and height tall,
// whose u sweeps 0..uMax across the width and whose v sweeps 0..1 up it.
//
// It is tessellated along u rather than being one quad, and that is the whole
// point. A quad carries four UVs, so a quantisation error at the corners
// interpolates into a smooth stretch that the eye reads as nothing at all. Real
// geometry has a vertex every few centimetres, and the error resets at each
// one: what shows on screen is a kink at every vertex, which is what a texture
// sliding on a wall actually looks like.
func bandGeometry(segments int, width, height, uMax float32) []UVVertex {
	uvs := make([]m.Vec2, 0, (segments+1)*2)
	positions := make([]m.Vec3, 0, (segments+1)*2)
	for i := 0; i <= segments; i++ {
		t := float32(i) / float32(segments)
		x := t * width
		for _, v := range [2]float32{0, 1} {
			positions = append(positions, m.Vec3{X: x, Y: (v - 0.5) * height})
			uvs = append(uvs, m.Vec2{X: t * uMax, Y: v})
		}
	}

	// The range is derived from the coordinates themselves, which is what scene
	// would do at bake - it already walks every vertex for the bounding sphere,
	// so this costs no traversal that is not already paid for.
	rng := newUVRange(uvs)

	vertices := make([]UVVertex, len(positions))
	for i := range positions {
		vertices[i] = UVVertex{
			Position: positions[i],
			Normal:   m.Vec3{Z: 1},
			UVf32:    uvs[i],
			UVu16:    rng.encodePacked(uvs[i]),
			UVf16:    [2]uint16{encodeFloat16(uvs[i].X), encodeFloat16(uvs[i].Y)},
		}
	}
	return vertices
}

// bandIndices triangulates the strip bandGeometry produced as a triangle list.
func bandIndices(segments int) []uint32 {
	indices := make([]uint32, 0, segments*6)
	for i := 0; i < segments; i++ {
		a := uint32(i * 2)
		indices = append(indices, a, a+1, a+2, a+2, a+1, a+3)
	}
	return indices
}

// --- the grid texture -------------------------------------------------------

// gridTexture paints the vernier: a dark ground with a thin bright line every
// minorPeriod texels and a brighter, wider one every majorPeriod, repeated
// across a texture gridWidth texels wide.
//
// The width is the number the claim is about. "Two texels adrift at 4K" is a
// statement about a 4096-texel axis, so the texture is 4096 texels on u; it is
// short on v because v carries no part of the question and 4096x4096 would be
// 64 MiB of prototype.
func gridTexture() []byte {
	pixels := make([]byte, gridWidth*gridHeight*4)
	for x := 0; x < gridWidth; x++ {
		var r, g, b byte = 14, 16, 20
		switch {
		case x%majorPeriod < 3:
			r, g, b = 255, 236, 180
		case x%minorPeriod < 2:
			r, g, b = 90, 130, 170
		}
		for y := 0; y < gridHeight; y++ {
			// A horizontal rule across the middle of every band, so the eye has
			// something to follow when the vertical lines are the thing moving.
			pr, pg, pb := r, g, b
			if y%64 < 2 {
				pr, pg, pb = max(pr, 60), max(pg, 70), max(pb, 84)
			}
			i := (y*gridWidth + x) * 4
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = pr, pg, pb, 255
		}
	}
	return pixels
}
