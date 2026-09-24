package main

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// The two meshes the demo bakes itself: a unit box for the plinths and a unit
// quad for the ground. Both are the standard vertex, so they draw with the
// bundled PBR and are lit like the models beside them, and both are unit-sized
// so that a Transform's Scale is what shapes them - which is what makes every
// plinth and the ground a non-uniform basis.

// quadFace is one face of a unit shape: its outward normal, the tangent its UVs
// run along, and the two axes its corners spread across.
type quadFace struct {
	normal, tangent, right, up m.Vec3
}

// appendQuad appends one face centred at centre, wound counter-clockwise seen
// from outside, with its own flat normal and white vertex colour - a zero
// Color is transparent black, which the bundled PBR would multiply the base
// colour by.
func appendQuad(vertices []model.Vertex, indices []uint32, face quadFace, centre m.Vec3) ([]model.Vertex, []uint32) {
	base := uint32(len(vertices))
	for _, corner := range [...]m.Vec2{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}} {
		vertices = append(vertices, model.Vertex{
			Position: centre.Add(face.right.MulS(corner.X * 0.5)).Add(face.up.MulS(corner.Y * 0.5)),
			Normal:   face.normal,
			Tangent:  m.Vec4{X: face.tangent.X, Y: face.tangent.Y, Z: face.tangent.Z, W: 1},
			UV0:      m.Vec2{X: (corner.X + 1) / 2, Y: 1 - (corner.Y+1)/2},
			Color:    m.White,
		})
	}
	return vertices, append(indices, base, base+1, base+2, base, base+2, base+3)
}

// boxGeometry is the 1x1x1 cube centred on the origin, four vertices a face so
// every face keeps its own flat normal.
func boxGeometry() ([]model.Vertex, []uint32) {
	faces := [...]quadFace{
		{normal: m.Vec3{X: 1}, tangent: m.Vec3{Z: -1}, right: m.Vec3{Z: -1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{X: -1}, tangent: m.Vec3{Z: 1}, right: m.Vec3{Z: 1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{Y: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: -1}},
		{normal: m.Vec3{Y: -1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: 1}},
		{normal: m.Vec3{Z: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{Z: -1}, tangent: m.Vec3{X: -1}, right: m.Vec3{X: -1}, up: m.Vec3{Y: 1}},
	}
	vertices := make([]model.Vertex, 0, 4*len(faces))
	indices := make([]uint32, 0, 6*len(faces))
	for _, face := range faces {
		vertices, indices = appendQuad(vertices, indices, face, face.normal.MulS(0.5))
	}
	return vertices, indices
}

// quadGeometry is the 1x1 square in the XZ plane centred on the origin, facing
// +Y. It is one-sided, so the bundled PBR's back-face cull hides it from
// beneath, which is where the lowest elevation the orbit allows just dips the
// eye.
func quadGeometry() ([]model.Vertex, []uint32) {
	return appendQuad(nil, nil,
		quadFace{normal: m.Vec3{Y: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: -1}},
		m.Vec3{})
}
