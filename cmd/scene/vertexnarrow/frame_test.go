package main

// THROWAWAY diagnostics for the frame ladder, in the same spirit as
// shadercheck_test.go: not a test of anything cog does, but the cheapest way to
// know that what is on screen is what was meant, on a map whose Notes say there
// is no pixel readback anywhere and every fidelity question is judged by eye.
//
// The eye judges the picture. These check the things the eye CANNOT check: that
// the encoders round-trip, that the vertex struct is laid out the way gfx will
// read it, and that the two generated textures actually contain the structure
// they were built to contain. A panorama that came out uniformly dark would
// look like an encoding that produced no artefact.
//
//	go test ./cmd/scene/vertexnarrow/ -run Frame -v

import (
	"fmt"
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/m"
)

// TestFrameVertexLayout is the check for issue 172's finding (a): gfx takes the
// vertex stride from the largest attribute end offset while scene uploads
// unsafe.Sizeof bytes, so a struct Go pads at the tail is written at one stride
// and read at another, silently, and the mesh comes out as garbage triangles.
func TestFrameVertexLayout(t *testing.T) {
	size := int(unsafe.Sizeof(FrameVertex{}))
	// gfx.VertexAttr keeps its offset unexported, so the check is on the struct
	// the layout was declared from: the last attribute is TOct16, and it has to
	// end exactly at the struct's size.
	end := int(unsafe.Offsetof(FrameVertex{}.TOct16)) + int(unsafe.Sizeof(FrameVertex{}.TOct16))
	if end != size {
		t.Fatalf("last attribute ends at %d, unsafe.Sizeof is %d: every vertex after the first "+
			"would be misaligned with nothing reported", end, size)
	}
	t.Logf("FrameVertex is %d bytes and the layout ends exactly there", size)
}

// TestFrameEncoders round-trips both tangent words, handedness included, and
// prints each candidate frame's error on the sphere the station actually draws.
func TestFrameEncoders(t *testing.T) {
	vertices, _ := frameSphere(sphereSlices, sphereStacks, sphereRadius)

	for _, handedness := range []bool{true, false} {
		for _, bits := range []uint{15, 8} {
			direction := m.Vec3{X: 0.3, Y: -0.5, Z: 0.81}.Normalize()
			word := encodeOctWord(direction, bits, handedness)
			shift := 2 * bits
			if got := word&(1<<shift) != 0; got != handedness {
				t.Errorf("bits=%d handedness=%v: read back %v", bits, handedness, got)
			}
			// Nothing above the handedness bit may be set: those bits are
			// reserved, and a decoder that assumes zero there has to be right.
			if word>>(shift+1) != 0 {
				t.Errorf("bits=%d: reserved bits above %d are not zero (%#x)", bits, shift, word)
			}
		}
	}

	for i, f := range frames {
		if f.NBits == 0 {
			t.Logf("%-24s %2d B frame, %2d B vertex   exact", f.Name, f.Bytes, f.Stride)
			continue
		}
		var nTotal, nWorst, tTotal, tWorst float64
		for _, v := range vertices {
			normal, tangent := sphereFrame(v.Position)
			ne, te := float64(octError(normal, f.NBits)), float64(octError(tangent, f.TBits))
			nTotal, tTotal = nTotal+ne, tTotal+te
			nWorst, tWorst = math.Max(nWorst, ne), math.Max(tWorst, te)
		}
		count := float64(len(vertices))
		t.Logf("%-24s %2d B frame, %2d B vertex   n mean %.4f max %.4f   t mean %.4f max %.4f",
			f.Name, f.Bytes, f.Stride, nTotal/count, nWorst, tTotal/count, tWorst)
		if nWorst == 0 || tWorst == 0 {
			t.Errorf("frame %d reports no error at all, which means an encoder is not running", i)
		}
	}
}

// TestFrameEnvironment measures the panorama instead of looking at it. The
// numbers that matter are per quadrant: how much of it is bright, and how many
// bright/dark transitions a horizontal scan crosses. A quadrant with no
// transitions has no edge for a reflection to swim across, which is the
// property the whole station rests on - and it is exactly what the fourth
// quadrant is supposed to be.
func TestFrameEnvironment(t *testing.T) {
	pixels := environmentTexture()
	if len(pixels) != envWidth*envHeight*4 {
		t.Fatalf("panorama is %d bytes, want %d", len(pixels), envWidth*envHeight*4)
	}

	quadrant := envWidth / 4
	names := [4]string{"0-90 frequency ladder", "90-180 softbox", "180-270 checker", "270-360 control"}
	for q := 0; q < 4; q++ {
		var bright, total, edges int
		for y := 0; y < envHeight; y++ {
			previous := false
			for x := q * quadrant; x < (q+1)*quadrant; x++ {
				lit := pixels[(y*envWidth+x)*4] > 128
				if lit {
					bright++
				}
				if x > q*quadrant && lit != previous {
					edges++
				}
				previous = lit
				total++
			}
		}
		t.Logf("%-24s bright %5.1f%%   %6.1f edges per scanline",
			names[q], 100*float64(bright)/float64(total), float64(edges)/float64(envHeight))
		if q < 3 && edges == 0 {
			t.Errorf("%s has no bright/dark transition anywhere: nothing for a reflection to move across", names[q])
		}
	}

	// The horizon line has to be unbroken all the way round, because it is the
	// one edge that can be followed through every quadrant and compared with
	// itself.
	row := envHeight / 2
	for x := 0; x < envWidth; x++ {
		if pixels[(row*envWidth+x)*4] < 200 {
			t.Fatalf("the horizon line is broken at x=%d", x)
		}
	}
}

// TestFrameNormalMaps reports how far each preset actually tilts the surface.
// The slope is the one number at this station that was tuned rather than
// derived, and a map that turns out to perturb by half a degree would answer
// the normal-map case with a picture of nothing.
func TestFrameNormalMaps(t *testing.T) {
	for preset := mapGrain; preset < mapPresetCount; preset++ {
		pixels := normalMapTexture(preset, mapStrength)
		var total, worst float64
		count := 0
		for i := 0; i < len(pixels); i += 4 {
			n := m.Vec3{
				X: float32(pixels[i])/255*2 - 1,
				Y: float32(pixels[i+1])/255*2 - 1,
				Z: float32(pixels[i+2])/255*2 - 1,
			}.Normalize()
			angle := math.Acos(math.Min(1, float64(n.Z))) * 180 / math.Pi
			total += angle
			worst = math.Max(worst, angle)
			count++
		}
		t.Log(fmt.Sprintf("%-12s tilt mean %.2f deg   max %.2f deg",
			mapPresetNames[preset], total/float64(count), worst))
		if worst < 5 {
			t.Errorf("%s barely perturbs the surface at all (max %.2f deg)", mapPresetNames[preset], worst)
		}
	}
}
