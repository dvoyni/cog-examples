package main

// THROWAWAY. The third station, added for
// https://github.com/dvoyni/cog/issues/179: the three cases the first
// prototype named and could not build.
//
// Issue 172 put oct16 on a sphere under one sun and found no visible artefact
// at any rung, which left issue 171's oct32 standing on margin rather than on
// anything the eye had objected to. It also named exactly three places oct16
// would fail if it fails - a reflected environment, a normal-mapped surface,
// and motion - and could build none of them. This file builds the surface and
// the two textures those cases need; material.go builds the shader and main.go
// the pose and the keys.
//
// # What is different about this station
//
// The stripes are no longer four rungs of one ladder. They are four candidate
// VERTEX FRAMES, because the tangent is on the table here and its ladder is not
// the normal's: issue 171 put the normal in Unorm16x2 and the tangent in a
// Uint32 as oct 15+15 plus handedness plus one reserved bit. So the four
// stripes are the four answers the decision can actually reach:
//
//	exact          Float32x3 normal + Float32x4 tangent   28 B   the reference
//	oct32 + oct30  issue 171's answer                      8 B   a 40 B vertex
//	oct16 + oct30  the normal narrowed alone               6 B   a 38 B vertex
//	oct16 + oct16  both narrowed                           4 B   a 36 B vertex
//
// oct22 is dropped here. It is four bytes for worse accuracy than oct32's four,
// so it was never a candidate for the stride - it existed on the normal ladder
// to show what decoding a non-native format costs, and that question is
// answered. Dropping it also takes the vertex from seven attributes to six,
// which matters while https://github.com/dvoyni/cog/issues/185 is open.
//
// # Why the exact frame is derived rather than stored
//
// The reference normal and tangent are computed in the VERTEX STAGE from the
// vertex's own position, not carried as attributes. On a unit sphere at the
// origin the normal IS the position and the tangent is a cross product, so the
// derived frame is exact rather than merely float32-rounded, and it costs two
// attributes that this layout would otherwise have to find room for. The Go
// side derives it with the same two lines, so the encoder and the shader agree
// by construction.
//
// It is derived per VERTEX and interpolated, never per fragment. Deriving it
// per fragment would give the reference stripe the true surface normal at every
// pixel while the candidate stripes got an interpolated one, and that
// difference is not quantisation - it would show up as a seam and be read as
// one.

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// FrameVertex carries a tangent frame four ways. The field order obeys the same
// rule LadderVertex documents: gfx takes the stride from the largest attribute
// end offset while scene uploads unsafe.Sizeof, so the last attribute has to end
// exactly at the struct's size or every vertex after the first is misaligned
// with nothing reported. This one ends at 36.
type FrameVertex struct {
	Position m.Vec3    // @location(0)  offset  0  Float32x3
	UV       m.Vec2    // @location(1)  offset 12  Float32x2
	NOct32   [2]uint16 // @location(2)  offset 20  Unorm16x2
	NOct16   [2]uint8  // @location(3)  offset 24  Unorm8x2
	_        [2]uint8  //               offset 26  interior padding
	TOct30   uint32    // @location(4)  offset 28  Uint32, oct 15+15 + w + 1 spare
	TOct16   uint32    // @location(5)  offset 32  Uint32, oct 8+8 + w + 15 spare
}

func (FrameVertex) VertexLayout() []gfx.VertexAttr { return frameLayout[:] }

var frameLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(FrameVertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(FrameVertex{}.UV)), gfx.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(FrameVertex{}.NOct32)), gfx.Unorm16x2),
	gfx.Attr(int(unsafe.Offsetof(FrameVertex{}.NOct16)), gfx.Unorm8x2),
	gfx.Attr(int(unsafe.Offsetof(FrameVertex{}.TOct30)), gfx.Uint32),
	gfx.Attr(int(unsafe.Offsetof(FrameVertex{}.TOct16)), gfx.Uint32),
}

// frame names one candidate tangent frame, in the order the stripes run left to
// right across the screen. Bytes is what the frame costs in the vertex; Stride
// is what scene.Vertex would weigh with that frame in it, which is the number
// the decision is actually about.
type frame struct {
	Name   string
	Bytes  int
	Stride int
	// NBits and TBits are the octahedral bits per axis, 0 meaning the exact
	// derived frame.
	NBits, TBits uint
}

var frames = [...]frame{
	{"exact  F32 n + F32 t", 28, 64, 0, 0},
	{"oct32 n + oct30 t", 8, 40, 16, 15},
	{"oct16 n + oct30 t", 6, 38, 8, 15},
	{"oct16 n + oct16 t", 4, 36, 8, 8},
	// Added after the first captures. The whole-frame A/B showed the two rungs
	// failing in different WAYS - the normal's difference is coherent and
	// edge-shaped, the tangent's is incoherent speckle - so narrowing only the
	// tangent is a candidate the first four never put on screen. It is the same
	// 38 bytes as frame 3 and a different 38: that one narrows the normal and
	// keeps the tangent, this one does the opposite.
	//
	// The two bytes are notional. oct16 plus a handedness bit is seventeen, so a
	// two-byte tangent needs that bit housed somewhere else - in the normal's
	// spare bits, or in a slot this prototype has no opinion about. Where it
	// lives is a packing question with no picture attached; whether the eye
	// minds losing the accuracy is this one.
	{"oct32 n + oct16 t", 6, 38, 16, 8},
}

// stripeFrames is how many of them the four-stripe view can show. The fifth is
// reachable only on its own, with 5 or VN_SOLO=4, because the seam arrangement
// cuts the screen into four and the fifth candidate has nowhere to stand.
const stripeFrames = 4

// --- the surface -------------------------------------------------------------

// sphereFrame is the exact tangent frame at a point on the unit sphere,
// computed the way both the Go encoder and the vertex shader compute it. The
// tangent runs along -theta, which is an arbitrary choice; what matters is that
// there is exactly one definition of it and both sides use it.
//
// At the poles the cross product degenerates, so it falls back to a fixed axis.
// The two pole rings are one vertex ring each out of 81 and are edge-on at
// every pose this station uses, but a NaN there would propagate into the
// encoded bytes rather than staying local.
func sphereFrame(position m.Vec3) (normal, tangent m.Vec3) {
	normal = position.Normalize()
	tangent = m.Vec3{Y: 1}.Cross(normal)
	if tangent.LengthSquared() < 1e-8 {
		return normal, m.Vec3{X: 1}
	}
	return normal, tangent.Normalize()
}

// frameSphere builds the same sphere the normal ladder uses, with a texture
// coordinate and a tangent frame added: u sweeps 0..1 round the equator and v
// runs 0..1 from the north pole down, which is glTF's convention and the one
// the normal map is painted in.
func frameSphere(slices, stacks int, radius float32) ([]FrameVertex, []uint32) {
	vertices := make([]FrameVertex, 0, (slices+1)*(stacks+1))
	for stack := 0; stack <= stacks; stack++ {
		phi := math.Pi * float64(stack) / float64(stacks)
		sinPhi, cosPhi := math.Sin(phi), math.Cos(phi)
		for slice := 0; slice <= slices; slice++ {
			theta := 2 * math.Pi * float64(slice) / float64(slices)
			position := m.Vec3{
				X: float32(sinPhi * math.Cos(theta)),
				Y: float32(cosPhi),
				Z: float32(sinPhi * math.Sin(theta)),
			}
			normal, tangent := sphereFrame(position)
			vertices = append(vertices, FrameVertex{
				Position: position.MulS(radius),
				UV: m.Vec2{
					X: float32(slice) / float32(slices),
					Y: float32(stack) / float32(stacks),
				},
				NOct32: encodeOct32(normal),
				NOct16: encodeOct16(normal),
				TOct30: encodeOctWord(tangent, 15, true),
				TOct16: encodeOctWord(tangent, 8, true),
			})
		}
	}

	indices := make([]uint32, 0, slices*stacks*6)
	stride := uint32(slices + 1)
	for stack := 0; stack < stacks; stack++ {
		for slice := 0; slice < slices; slice++ {
			a := uint32(stack)*stride + uint32(slice)
			b := a + stride
			indices = append(indices, a, a+1, b, a+1, b+1, b)
		}
	}
	return vertices, indices
}

// --- the environment ---------------------------------------------------------
//
// gfx has no cube view dimension - TextureViewDimension is TextureView2D and
// TextureView2DArray and nothing else (gfx/contract.go) - so an environment can
// only be an equirectangular 2D texture sampled by direction. It also has no
// float texture format, only RGBA8 and RGBA8Srgb, so a real HDR panorama would
// be tonemapped to eight bits before it ever reached the shader, and its
// highlights would arrive clipped and SOFTER than what is painted here.
//
// This one is an instrument rather than a photograph, and it is built to be
// harsher than any real environment while staying inside eight bits. It is laid
// out by azimuth quadrant, so orbiting the camera walks the reflection through
// all four:
//
//	  0..90    a frequency ladder: vertical bars at 2, 4, 8 and 16 degrees of
//	           angular period, stacked in elevation bands. This is the one that
//	           says WHICH spatial frequency a 1.8 degree swim destroys - the
//	           finest band is just under one full period of phase flip, and the
//	           coarsest is a ninth of one.
//	 90..180   one hard-edged white softbox. A single straight edge is the
//	           classic thing a reflection is judged against.
//	180..270   a 6 degree checkerboard: edges in both axes at once.
//	270..360   a smooth gradient and nothing else. This is the CONTROL: if a
//	           stripe boundary shows here, it is not the environment.
//
// A bright horizon line runs the whole way round at elevation zero, unbroken,
// so one edge can be followed through all four quadrants and compared with
// itself.
// The size is 1024 x 512 and that is a MEASURED limit rather than a taste.
// gfx has no public way to bake a texture from bytes durably: the only two
// entries are TextureWithBytes, which is a frame-lifetime temporary re-baked on
// every draw that names it (gfx/opqueue.go bakeTextureIfNeeded), and
// TextureWithResource, which needs a file. A re-bake with mipmaps runs
// wgpu/gfxbackend.go uploadMipChain, which box-filters the whole chain on the
// CPU. At 2048 x 1024 that measured +5.8 ms a frame - 60 fps became 44 - while
// the same panorama with mipmaps off, and this one with them on, both hold 60.
//
// Mips are not optional here: without them a mirror sphere minifies the
// panorama hard toward its rim, and the aliasing that produces shimmers under
// exactly the camera motion the third case is trying to judge.
//
// 1024 gives 2.84 texels a degree, so the finest ladder band at 2 degrees is
// 5.7 texels a cycle - comfortably above Nyquist, where a 1 degree band would
// not have been.
const (
	envWidth  = 1024
	envHeight = 512
)

// The bands of the frequency ladder, in degrees: the elevation window each one
// occupies and the angular period of its bars.
var envLadder = [...]struct{ Low, High, Period float64 }{
	{54, 76, 2},
	{30, 52, 4},
	{6, 28, 8},
	{-30, -8, 16},
}

func environmentTexture() []byte {
	pixels := make([]byte, envWidth*envHeight*4)
	set := func(x, y int, r, g, b byte) {
		i := (y*envWidth + x) * 4
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = r, g, b, 255
	}
	for y := 0; y < envHeight; y++ {
		// Elevation in degrees: +90 at the top row, -90 at the bottom, which is
		// the inverse of the v = acos(y)/pi the shader samples with.
		elevation := 90 - 180*(float64(y)+0.5)/float64(envHeight)
		for x := 0; x < envWidth; x++ {
			azimuth := 360 * (float64(x) + 0.5) / float64(envWidth)
			r, g, b := envSample(azimuth, elevation)
			set(x, y, r, g, b)
		}
	}
	return pixels
}

// The environment's two levels. Dark is not black: a pure black surround makes
// the sphere read as a cutout and hides where the reflection actually lands.
var (
	envDark  = [3]byte{9, 10, 13}
	envBar   = [3]byte{228, 233, 244}
	envWarm  = [3]byte{255, 243, 214}
	envFloor = [3]byte{26, 24, 22}
)

func envSample(azimuth, elevation float64) (byte, byte, byte) {
	// The horizon line first, because it crosses every quadrant and nothing
	// else may overwrite it. Six tenths of a degree is roughly three texels at
	// this height: the sharpest edge the texture can hold.
	if math.Abs(elevation) < 0.3 {
		return envWarm[0], envWarm[1], envWarm[2]
	}
	if elevation < -62 {
		return envFloor[0], envFloor[1], envFloor[2]
	}
	if elevation > 80 {
		// A dim ceiling glow, so the top of the sphere is not a void.
		t := (elevation - 80) / 10
		v := byte(20 + 40*math.Min(t, 1))
		return v, v, byte(float64(v) * 1.1)
	}

	switch {
	case azimuth < 90: // the frequency ladder
		for _, band := range envLadder {
			if elevation >= band.Low && elevation < band.High {
				if math.Mod(azimuth, band.Period) < band.Period/2 {
					return envBar[0], envBar[1], envBar[2]
				}
				return envDark[0], envDark[1], envDark[2]
			}
		}
	case azimuth < 180: // one hard-edged softbox
		if elevation > 18 && elevation < 58 && azimuth > 108 && azimuth < 162 {
			return 255, 255, 255
		}
	case azimuth < 270: // a checkerboard, edges in both axes
		const cell = 6.0
		u := int(math.Floor((azimuth - 180) / cell))
		v := int(math.Floor((elevation + 60) / cell))
		if (u+v)%2 == 0 {
			return envBar[0], envBar[1], envBar[2]
		}
	default: // the control: a smooth gradient with no edge anywhere
		t := (elevation + 62) / 142
		v := byte(18 + 150*t*t)
		return v, byte(float64(v) * 0.96), byte(float64(v) * 0.88)
	}
	return envDark[0], envDark[1], envDark[2]
}

// --- the normal map ----------------------------------------------------------
//
// Two presets, because issue 179's second case is the interaction of a COARSE
// vertex frame with a FINE map, and "fine" is the whole question. A grain whose
// features are a couple of screen pixels across beats against the vertex frame
// differently from a dent field whose features are a tenth of the sphere.
//
// The map is tangent space and therefore LINEAR: FormatRGBA8, never the sRGB
// twin. A normal map read through an sRGB decode is a silent error of exactly
// the kind this map's Notes say there is no readback to catch.
const (
	normalMapWidth  = 1024
	normalMapHeight = 512
)

const (
	mapOff = iota
	mapGrain
	mapDents
	mapPresetCount
)

var mapPresetNames = [...]string{"off", "fine grain", "dents"}

// heightAt is the two presets' height field, in the same units as the texel
// spacing so the derivative below is a slope rather than an arbitrary number.
func heightAt(preset int, u, v float64) float64 {
	switch preset {
	case mapGrain:
		// Two octaves, the finer one at roughly four texels a cycle, which is
		// about two screen pixels at this station's framing.
		return 0.6*math.Sin(2*math.Pi*u*256)*math.Sin(2*math.Pi*v*128) +
			0.4*math.Sin(2*math.Pi*u*97+1.7)*math.Sin(2*math.Pi*v*53+0.4)
	case mapDents:
		// A grid of round dimples, about a tenth of the sphere across.
		const cells = 16.0
		fu := u*cells - math.Floor(u*cells) - 0.5
		fv := v*cells/2 - math.Floor(v*cells/2) - 0.5
		d := math.Hypot(fu, fv) / 0.42
		if d >= 1 {
			return 0
		}
		// The amplitude is set so the rim of a dent tilts about as far as the
		// grain's steepest facet does. Two presets that perturb by different
		// AMOUNTS would answer a question about amplitude; these two differ
		// only in the frequency, which is the question.
		return -math.Cos(d*math.Pi/2) * 9.0
	}
	return 0
}

// normalMapTexture differentiates the height field and stores the tangent-space
// normal. strength scales the slope; it is the knob the station exposes.
func normalMapTexture(preset int, strength float64) []byte {
	pixels := make([]byte, normalMapWidth*normalMapHeight*4)
	du := 1.0 / float64(normalMapWidth)
	dv := 1.0 / float64(normalMapHeight)
	for y := 0; y < normalMapHeight; y++ {
		v := (float64(y) + 0.5) * dv
		for x := 0; x < normalMapWidth; x++ {
			u := (float64(x) + 0.5) * du
			// Central differences, wrapped on u because the sphere's texture
			// wraps there and a seam in the map would read as a seam in the
			// picture.
			hx := heightAt(preset, math.Mod(u+du+1, 1), v) - heightAt(preset, math.Mod(u-du+1, 1), v)
			hy := heightAt(preset, u, math.Min(v+dv, 1)) - heightAt(preset, u, math.Max(v-dv, 0))
			n := m.Vec3{
				X: float32(-hx * strength),
				Y: float32(-hy * strength),
				Z: 1,
			}.Normalize()
			i := (y*normalMapWidth + x) * 4
			pixels[i] = byte(m.Clamp01(n.X*0.5+0.5)*255 + 0.5)
			pixels[i+1] = byte(m.Clamp01(n.Y*0.5+0.5)*255 + 0.5)
			pixels[i+2] = byte(m.Clamp01(n.Z*0.5+0.5)*255 + 0.5)
			pixels[i+3] = 255
		}
	}
	return pixels
}
