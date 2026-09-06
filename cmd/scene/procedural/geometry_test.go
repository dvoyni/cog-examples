package main

import (
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// The demo's vertex is not scene.Vertex, and that is the whole point: it is 36
// bytes with three attributes where the standard layout is 84 with eight, so
// nothing about it could be mistaken for the layout the bundled PBR reads.
func TestTheVertexIsTheDemosOwnThirtySixByteLayout(t *testing.T) {
	if size := unsafe.Sizeof(Vertex{}); size != 36 {
		t.Fatalf("Vertex is %d bytes, want 36", size)
	}
	want := []gfx.VertexAttr{
		gfx.Attr(0, gfx.Float32x3),  // position
		gfx.Attr(12, gfx.Float32x3), // normal
		gfx.Attr(24, gfx.Float32x3), // tint
	}
	layout := Vertex{}.VertexLayout()
	if len(layout) != len(want) {
		t.Fatalf("the layout has %d attributes, want %d", len(layout), len(want))
	}
	for i := range want {
		if layout[i] != want[i] {
			t.Errorf("attribute %d is %+v, want %+v", i, layout[i], want[i])
		}
	}
}

// The ridge is an indexed grid: one vertex per lattice point and two triangles
// per cell, every index inside the vertices, and every normal a unit vector.
func TestTheRidgeIsAnIndexedGridWithUnitNormals(t *testing.T) {
	const cells = 8
	vertices, indices := ridgeGeometry(cells, 0.4)
	if want := (cells + 1) * (cells + 1); len(vertices) != want {
		t.Fatalf("the ridge has %d vertices, want %d", len(vertices), want)
	}
	if want := cells * cells * 6; len(indices) != want {
		t.Fatalf("the ridge has %d indices, want %d", len(indices), want)
	}
	for _, index := range indices {
		if int(index) >= len(vertices) {
			t.Fatalf("index %d is past the %d vertices", index, len(vertices))
		}
	}
	for i, vertex := range vertices {
		if length := vertex.Normal.Length(); length < 0.999 || length > 1.001 {
			t.Errorf("vertex %d has normal %v of length %v, want a unit normal",
				i, vertex.Normal, length)
		}
	}
}

// The ridge spans the unit square in X and Z and stays inside ridgeBounds,
// which is the sphere the draw hands the culler: a custom layout gets no baked
// sphere of its own, so a ridge that outgrew this radius would be culled while
// still on screen.
func TestTheRidgeStaysInsideTheBoundsItDeclares(t *testing.T) {
	for _, phase := range []float32{0, 1.1, 2.7, 4.9} {
		vertices, _ := ridgeGeometry(ridgeCells, phase)
		for i, vertex := range vertices {
			if x := vertex.Position.X; x < -0.5 || x > 0.5 {
				t.Fatalf("vertex %d is at x %v, outside the unit square", i, x)
			}
			if z := vertex.Position.Z; z < -0.5 || z > 0.5 {
				t.Fatalf("vertex %d is at z %v, outside the unit square", i, z)
			}
			if radius := vertex.Position.Length(); radius > ridgeBounds {
				t.Fatalf("vertex %d at %v is %v from the origin, past the declared bounds %v",
					i, vertex.Position, radius, ridgeBounds)
			}
		}
	}
}

// The ridge's triangles wind counter-clockwise seen from above, so the surface
// a viewer looks down on is its front face. The material draws both sides, so
// this is what decides which way the fragment stage flips the normal.
func TestTheRidgeWindsCounterClockwiseFromAbove(t *testing.T) {
	vertices, indices := ridgeGeometry(6, 0)
	for i := 0; i < len(indices); i += 3 {
		a := vertices[indices[i]].Position
		b := vertices[indices[i+1]].Position
		c := vertices[indices[i+2]].Position
		if up := b.Sub(a).Cross(c.Sub(a)).Y; up <= 0 {
			t.Fatalf("triangle %d has upward component %v, so it faces down", i/3, up)
		}
	}
}

// The ridge's height changes with its phase. It is rebuilt and re-baked every
// frame, so a phase that did nothing would be a still picture through
// UpdateMesh - the thing this demo exists to show moving.
func TestTheRidgePhaseMovesTheSurface(t *testing.T) {
	first, _ := ridgeGeometry(ridgeCells, 0)
	second, _ := ridgeGeometry(ridgeCells, 1)
	moved := 0
	for i := range first {
		if first[i].Position.Y != second[i].Position.Y {
			moved++
		}
	}
	if moved*4 < len(first) {
		t.Errorf("only %d of %d vertices moved between phases", moved, len(first))
	}
}

// The ribbon is a triangle strip: two vertices per column, one extra column to
// close the loop, and no indices at all. It carries no index buffer, which is
// the non-indexed half of the mesh contract.
func TestTheRibbonIsANonIndexedClosedStrip(t *testing.T) {
	const segments = 12
	vertices := ribbonGeometry(segments, 0.3)
	if want := 2 * (segments + 1); len(vertices) != want {
		t.Fatalf("the ribbon has %d vertices, want %d", len(vertices), want)
	}
	for i := range 2 {
		first, last := vertices[i], vertices[len(vertices)-2+i]
		if first.Position.Distance(last.Position) > 1e-4 {
			t.Errorf("ribbon edge %d does not close: %v against %v",
				i, first.Position, last.Position)
		}
	}
	for i, vertex := range vertices {
		if length := vertex.Normal.Length(); length < 0.999 || length > 1.001 {
			t.Errorf("vertex %d has normal %v of length %v, want a unit normal",
				i, vertex.Normal, length)
		}
	}
}

// The beacon is the indexed case at its smallest: six vertices carrying eight
// faces, so twenty-four indices address six positions. Its radius is one, which
// is the sphere its draw declares and its transform scales.
func TestTheBeaconIsAUnitOctahedron(t *testing.T) {
	vertices, indices := beaconGeometry(m.Vec3{X: 1})
	if len(vertices) != 6 {
		t.Fatalf("the beacon has %d vertices, want 6", len(vertices))
	}
	if len(indices) != 24 {
		t.Fatalf("the beacon has %d indices, want 24", len(indices))
	}
	for i, vertex := range vertices {
		if radius := vertex.Position.Length(); math.Abs(float64(radius-1)) > 1e-6 {
			t.Errorf("vertex %d is %v from the origin, want the unit octahedron", i, radius)
		}
	}
	for i := 0; i < len(indices); i += 3 {
		a := vertices[indices[i]].Position
		b := vertices[indices[i+1]].Position
		c := vertices[indices[i+2]].Position
		normal := b.Sub(a).Cross(c.Sub(a))
		if outward := normal.Dot(a.Add(b).Add(c)); outward <= 0 {
			t.Errorf("beacon face %d winds inward: %v against centroid direction", i/3, a)
		}
	}
}

// Every beacon generation is a different colour, so the swap a release and a
// re-bake perform is something a human can see rather than only count.
func TestEachBeaconGenerationIsADifferentColour(t *testing.T) {
	seen := map[m.Vec3]int{}
	for generation := range len(beaconTints) {
		tint := beaconTint(generation)
		seen[tint]++
	}
	if len(seen) != len(beaconTints) {
		t.Errorf("%d generations produced %d distinct tints", len(beaconTints), len(seen))
	}
	if first, wrapped := beaconTint(0), beaconTint(len(beaconTints)); first != wrapped {
		t.Errorf("generation %d is %v, want it to wrap back to %v", len(beaconTints), wrapped, first)
	}
}
