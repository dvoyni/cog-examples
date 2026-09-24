package main

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Vertex is the demo's own vertex, and the reason it needs a material of its
// own: 36 bytes and three attributes, where model.Vertex is 72 and six. A mesh
// built from it cannot take the bundled PBR, which knows two layouts and no
// others - the standard six and the glTF loader's skinned eight; pairing the
// two is reported and the draw skipped, which is a contract this demo would
// rather demonstrate the right side of.
//
// Tint is linear RGB, not sRGB. Colour rides in the vertices because the
// material declares no parameters of its own - see material.go for why - so
// per-object colour has nowhere else to live. The palette is still written in
// sRGB and converted on the way in, the same as every other demo.
type Vertex struct {
	Position m.Vec3
	Normal   m.Vec3
	Tint     m.Vec3
}

// VertexLayout reports the attribute layout, in @location order, and it must
// agree with both the struct's memory layout and vs_main's inputs. The offsets
// come from the struct rather than from arithmetic, so a field inserted above
// moves the attribute with it.
func (Vertex) VertexLayout() []gfx.VertexAttr { return demoVertexLayout[:] }

var demoVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Normal)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Tint)), gfx.Float32x3),
}

// The two bundled-PBR shapes are model.Vertex meshes rather than the Vertex
// above, because they are here to be drawn with no Material at all: a Mesh
// with none draws the bundled PBR, which reads only the standard layout.
// Scene's own debug shapes will not do instead, because they are self-lit, and
// the whole reason these two are in the frame is to be lit by the same sun as
// the caller's material.

// groundGeometry builds the ground: a side x side square in the XZ plane
// centred on the origin, facing +Y, and its mirror facing -Y at the same
// place. Two faces because the bundled PBR culls back faces and the orbit may
// dip below the ground; only the face towards the camera survives the cull, so
// the two never fight for depth.
func groundGeometry(side float32) ([]model.Vertex, []uint32) {
	half := side / 2
	vertices := make([]model.Vertex, 0, 8)
	indices := make([]uint32, 0, 12)
	for _, up := range [2]float32{1, -1} {
		base := uint32(len(vertices))
		normal := m.Vec3{Y: up}
		// Seen from the side the normal points to, x then z*up runs
		// counter-clockwise, so the mirrored face swaps the Z of its corners.
		for _, corner := range [4]m.Vec2{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}} {
			vertices = append(vertices, model.Vertex{
				Position: m.Vec3{X: corner.X * half, Z: -corner.Y * half * up},
				Normal:   normal,
				Tangent:  m.Vec4{X: 1, W: 1},
				UV0:      m.Vec2{X: (corner.X + 1) / 2, Y: (corner.Y + 1) / 2},
				Color:    m.White,
			})
		}
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}
	return vertices, indices
}

// The reference sphere's tessellation, the one scene's lit debug sphere had
// when the demo drew it through that: sixteen segments round and twelve rings
// from pole to pole.
const (
	sphereSegments = 16
	sphereRings    = 12
)

// sphereGeometry builds the radius-1 UV sphere: sphereRings+1 rows of
// sphereSegments+1 vertices from the north pole down, the extra column closing
// the seam, with smooth normals equal to the position. The rows touching a
// pole contribute one triangle per segment rather than two, since the other
// would have no area.
func sphereGeometry() ([]model.Vertex, []uint32) {
	const columns = sphereSegments + 1
	vertices := make([]model.Vertex, 0, columns*(sphereRings+1))
	for ring := range sphereRings + 1 {
		v := float64(ring) / sphereRings
		y, radius := float32(math.Cos(v*math.Pi)), float32(math.Sin(v*math.Pi))
		for segment := range columns {
			u := float64(segment) / sphereSegments
			sin, cos := math.Sincos(u * 2 * math.Pi)
			position := m.Vec3{X: radius * float32(cos), Y: y, Z: radius * float32(sin)}
			vertices = append(vertices, model.Vertex{
				Position: position,
				Normal:   position,
				Tangent:  m.Vec4{X: float32(-sin), Z: float32(cos), W: 1},
				UV0:      m.Vec2{X: float32(u), Y: float32(v)},
				Color:    m.White,
			})
		}
	}
	indices := make([]uint32, 0, sphereSegments*(2*sphereRings-2)*3)
	for ring := range sphereRings {
		for segment := range sphereSegments {
			a := uint32(ring*columns + segment)
			b, c := a+1, a+columns
			d := c + 1
			// Rows run north to south and columns eastward, so seen from
			// outside (a, b, d) and (a, d, c) run counter-clockwise.
			if ring != 0 {
				indices = append(indices, a, b, d)
			}
			if ring != sphereRings-1 {
				indices = append(indices, a, d, c)
			}
		}
	}
	return vertices, indices
}

// The ridge: a heightfield over the unit square in X and Z, rebuilt and
// re-baked every frame through UpdateMesh. It is authored at unit size and
// placed by its transform's uniform Scale, so the sphere below is local space
// and the world sphere follows the scale for free.
const (
	// ridgeCells is the grid the ridge starts at, and ridgeCoarseCells the one
	// it alternates with. The two differ so that UpdateMesh is seen changing a
	// mesh's size, not only its contents: there is no capacity concept, and a
	// re-bake at any length keeps the buffer id.
	ridgeCells       = 48
	ridgeCoarseCells = 17
	ridgeAmplitude   = 0.085
	ridgeWavesX      = 2.0
	ridgeWavesZ      = 1.5
)

// ridgeBounds is the local-space radius the ridge's Mesh declares. A custom
// vertex layout gets no baked sphere - scene cannot locate POSITION in bytes it
// has never seen a layout for - so without this the ridge would be exempt from
// culling entirely. It is the circumradius of the unit square plus the wave's
// own amplitude, with room to spare.
const ridgeBounds = 0.72

// ridgeGeometry builds the ridge at one resolution and one phase: an indexed
// grid of cells x cells cells over the unit square, its height the product of
// two sines, its normals taken from that product's own derivatives rather than
// from the triangles, and its tint ramped by height.
//
// Analytic normals rather than area-weighted face normals because the surface
// is known in closed form, so the correct answer is cheaper than the
// approximation - and because a normal that is right at every resolution is
// what makes the coarse grid look like the same surface, coarsely sampled,
// instead of a different one.
func ridgeGeometry(cells int, phase float32) ([]Vertex, []uint32) {
	const (
		waveX = ridgeWavesX * 2 * math.Pi
		waveZ = ridgeWavesZ * 2 * math.Pi
	)
	side := cells + 1
	vertices := make([]Vertex, 0, side*side)
	for j := range side {
		z := float32(j)/float32(cells) - 0.5
		for i := range side {
			x := float32(i)/float32(cells) - 0.5
			alongX, acrossZ := float64(waveX*x+phase), float64(waveZ*z+phase*0.7)
			height := ridgeAmplitude * float32(math.Sin(alongX)*math.Cos(acrossZ))
			slopeX := ridgeAmplitude * waveX * float32(math.Cos(alongX)*math.Cos(acrossZ))
			slopeZ := -ridgeAmplitude * waveZ * float32(math.Sin(alongX)*math.Sin(acrossZ))
			vertices = append(vertices, Vertex{
				Position: m.Vec3{X: x, Y: height, Z: z},
				Normal:   m.Vec3{X: -slopeX, Y: 1, Z: -slopeZ}.Normalize(),
				Tint:     ridgeLow.Lerp(ridgeHigh, height/ridgeAmplitude*0.5+0.5),
			})
		}
	}

	// Two triangles per cell, wound counter-clockwise seen from above: the
	// first edge runs along +X and the second along -Z, which crosses to +Y.
	indices := make([]uint32, 0, cells*cells*6)
	for j := range cells {
		for i := range cells {
			near := uint32(j*side + i)
			far := uint32((j+1)*side + i)
			indices = append(indices, far, far+1, near, far+1, near+1, near)
		}
	}
	return vertices, indices
}

// The ribbon: a closed band around the origin, rebuilt from scratch every frame
// as a fresh mesh, with the previous frame's released as it goes. It is
// authored in world units and placed by a translation alone.
const (
	ribbonSegments  = 96
	ribbonRadius    = 3.1
	ribbonHalfWidth = 0.3
	ribbonTwists    = 3
)

// ribbonGeometry builds the ribbon at one moment: a band of ribbonSegments
// quads around a circle of ribbonRadius, twisting ribbonTwists times as it goes
// round, emitted as one non-indexed triangle strip.
//
// A strip rather than a list because the ribbon is a strip: it halves the
// vertices a list would need for the same band, and it is the one topology in
// the demo that is not the zero value, so the topology argument is seen being
// something other than the default.
func ribbonGeometry(segments int, phase float32) []Vertex {
	vertices := make([]Vertex, 0, 2*(segments+1))
	for i := range segments + 1 {
		around := float64(i) / float64(segments) * 2 * math.Pi
		sin, cos := math.Sincos(around)
		radial := m.Vec3{X: float32(cos), Z: float32(sin)}
		along := m.Vec3{X: float32(-sin), Z: float32(cos)}
		twist := around*ribbonTwists + float64(phase)
		twistSin, twistCos := math.Sincos(twist)
		// across spans the band: upright where the twist is zero, lying flat a
		// quarter turn later.
		across := m.Vec3{Y: float32(twistCos)}.Add(radial.MulS(float32(twistSin)))
		normal := along.Cross(across).Normalize()
		centre := radial.MulS(ribbonRadius)
		tint := ribbonLow.Lerp(ribbonHigh, float32(twistSin)*0.5+0.5)
		vertices = append(vertices,
			Vertex{Position: centre.Sub(across.MulS(ribbonHalfWidth)), Normal: normal, Tint: tint},
			Vertex{Position: centre.Add(across.MulS(ribbonHalfWidth)), Normal: normal, Tint: tint},
		)
	}
	return vertices
}

// beaconGeometry builds the beacon: a unit octahedron, six vertices carrying
// eight faces, wound outward. Its normals are its positions, so the faceting is
// smooth across the shared vertices - which is the point of an indexed mesh
// this small, six positions doing the work of twenty-four.
func beaconGeometry(tint m.Vec3) ([]Vertex, []uint32) {
	positions := [6]m.Vec3{
		{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {Z: 1}, {Z: -1},
	}
	vertices := make([]Vertex, 0, len(positions))
	for _, position := range positions {
		vertices = append(vertices, Vertex{Position: position, Normal: position, Tint: tint})
	}
	indices := []uint32{
		0, 2, 4, 4, 2, 1, 1, 2, 5, 5, 2, 0, // the four faces above the equator
		0, 4, 3, 4, 1, 3, 1, 5, 3, 5, 0, 3, // and the four below it
	}
	return vertices, indices
}

// beaconTint is the colour of one beacon generation, cycling through the
// palette so a release and a re-bake are visible as a colour change rather than
// only as a number in the HUD.
func beaconTint(generation int) m.Vec3 {
	return beaconTints[generation%len(beaconTints)]
}
