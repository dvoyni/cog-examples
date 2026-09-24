package main

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Vertex is the fountain's own layout, the procedural example's approach: a
// custom layout needs a custom material, and material.go is that material.
type Vertex struct {
	Position m.Vec3
	Normal   m.Vec3
}

func (Vertex) VertexLayout() []gfx.VertexAttr { return vertexLayout[:] }

var vertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Normal)), gfx.Float32x3),
}

// CubeGeometry is a unit cube about the origin, four vertices a face.
func CubeGeometry() ([]Vertex, []uint32) {
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

// BasinBounds is the basin's bounding sphere.
var BasinBounds = m.Vec4{W: basinRadius}

// DiscGeometry is the basin, a disc of basinRadius on the ground.
func DiscGeometry() ([]Vertex, []uint32) {
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
