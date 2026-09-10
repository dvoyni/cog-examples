package main

// THROWAWAY. The candidate encodings from
// https://github.com/dvoyni/cog/issues/171, written out longhand so the demo
// can put four of them on screen at once. None of this is meant to ship: a real
// implementation would pack once at bake, and would not carry every rung of the
// ladder in the same vertex.

import (
	"math"

	"github.com/dvoyni/cog/m"
)

// --- octahedral normals -----------------------------------------------------
//
// Cigolle et al., JCGT 2014. The unit sphere is folded onto the [-1,1] square:
// the upper hemisphere projects straight down onto the diamond |x|+|y| <= 1,
// and the lower hemisphere is reflected out into the four corners. The mapping
// is area-preserving enough that quantising the square quantises the sphere
// evenly, which is the whole reason it beats storing xyz.

// signNotZero is the paper's sign, which returns +1 at zero rather than 0. A
// plain sign folds the lower hemisphere onto the axes and loses a quadrant.
func signNotZero(v float32) float32 {
	if v >= 0 {
		return 1
	}
	return -1
}

// octEncode projects a unit vector onto the octahedral square, in [-1,1]^2.
func octEncode(n m.Vec3) m.Vec2 {
	l1 := float32(math.Abs(float64(n.X)) + math.Abs(float64(n.Y)) + math.Abs(float64(n.Z)))
	if l1 == 0 {
		return m.Vec2{}
	}
	p := m.Vec2{X: n.X / l1, Y: n.Y / l1}
	if n.Z > 0 {
		return p
	}
	return m.Vec2{
		X: (1 - float32(math.Abs(float64(p.Y)))) * signNotZero(p.X),
		Y: (1 - float32(math.Abs(float64(p.X)))) * signNotZero(p.Y),
	}
}

// octDecode is the Go twin of the WGSL in material.go, kept here so the error
// numbers the HUD prints are computed against exactly what the shader does
// rather than against the ideal encoding.
func octDecode(p m.Vec2) m.Vec3 {
	n := m.Vec3{X: p.X, Y: p.Y, Z: 1 - float32(math.Abs(float64(p.X))) - float32(math.Abs(float64(p.Y)))}
	if n.Z < 0 {
		n.X = (1 - float32(math.Abs(float64(p.Y)))) * signNotZero(p.X)
		n.Y = (1 - float32(math.Abs(float64(p.X)))) * signNotZero(p.Y)
	}
	return n.Normalize()
}

// quantizeUnorm rounds a value in [-1,1] to bits of unsigned normalised
// precision and returns the stored integer. This is the plain round, not the
// paper's error-minimising refinement search: a bake would plausibly do the
// refinement, so the ladder here is the pessimistic rung of each step.
func quantizeUnorm(v float32, bits uint) uint32 {
	maximum := float32(uint32(1)<<bits - 1)
	stored := (m.Clamp(v, -1, 1)*0.5 + 0.5) * maximum
	return uint32(math.Round(float64(stored)))
}

func dequantizeUnorm(stored uint32, bits uint) float32 {
	maximum := float32(uint32(1)<<bits - 1)
	return float32(stored)/maximum*2 - 1
}

// encodeOct32 stores 16 bits per axis, which reaches gfx as Unorm16x2.
func encodeOct32(n m.Vec3) [2]uint16 {
	p := octEncode(n)
	return [2]uint16{uint16(quantizeUnorm(p.X, 16)), uint16(quantizeUnorm(p.Y, 16))}
}

// encodeOct22 stores 11 bits per axis packed into one u32, which reaches gfx as
// Uint32 and is unpacked in WGSL. There is no 11/11 vertex format, and that is
// the point of the rung: it is the accuracy Unorm1010102 reaches, at a format
// the shader has to open itself.
func encodeOct22(n m.Vec3) uint32 {
	p := octEncode(n)
	return quantizeUnorm(p.X, 11) | quantizeUnorm(p.Y, 11)<<11
}

// encodeOct16 stores 8 bits per axis, which reaches gfx as Unorm8x2. Two bytes
// per normal is the only remaining way under the 40-byte stride.
func encodeOct16(n m.Vec3) [2]uint8 {
	p := octEncode(n)
	return [2]uint8{uint8(quantizeUnorm(p.X, 8)), uint8(quantizeUnorm(p.Y, 8))}
}

// octError reports the angle in degrees between a unit vector and its value
// after a round trip through bits of octahedral precision. The HUD prints the
// worst and mean of these over a mesh, so the picture on screen has the number
// that produced it standing beside it.
func octError(n m.Vec3, bits uint) float32 {
	p := octEncode(n)
	round := m.Vec2{
		X: dequantizeUnorm(quantizeUnorm(p.X, bits), bits),
		Y: dequantizeUnorm(quantizeUnorm(p.Y, bits), bits),
	}
	dot := float64(m.Clamp(n.Normalize().Dot(octDecode(round)), -1, 1))
	return float32(math.Acos(dot) * 180 / math.Pi)
}

// --- half floats ------------------------------------------------------------

// encodeFloat16 converts a float32 to IEEE binary16 with round-to-nearest-even,
// flushing subnormals and overflow the way a GPU does. It exists because the UV
// question is a question about binades: a coordinate that reaches exactly 1.0
// crosses into [1,2) and doubles its step for the whole primitive, and nothing
// but a real half float shows that.
func encodeFloat16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits >> 16 & 0x8000)
	exponent := int32(bits>>23&0xFF) - 127
	mantissa := bits & 0x7FFFFF

	switch {
	case exponent == 128: // Inf or NaN
		if mantissa != 0 {
			return sign | 0x7E00
		}
		return sign | 0x7C00
	case exponent > 15: // overflows to infinity
		return sign | 0x7C00
	case exponent < -24: // underflows to zero
		return sign
	case exponent < -14: // subnormal
		mantissa |= 0x800000
		shift := uint32(-exponent - 14)
		half := mantissa >> (shift + 13)
		if mantissa>>(shift+12)&1 == 1 {
			half++
		}
		return sign | uint16(half)
	default:
		half := uint16(exponent+15)<<10 | uint16(mantissa>>13)
		// Round to nearest, ties to even, on the 13 bits being dropped.
		dropped := mantissa & 0x1FFF
		if dropped > 0x1000 || (dropped == 0x1000 && half&1 == 1) {
			half++
		}
		return sign | half
	}
}

func decodeFloat16(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exponent := uint32(h >> 10 & 0x1F)
	mantissa := uint32(h & 0x3FF)
	switch {
	case exponent == 0:
		if mantissa == 0 {
			return math.Float32frombits(sign)
		}
		// Subnormal: renormalise into a float32 exponent.
		exponent = 1
		for mantissa&0x400 == 0 {
			mantissa <<= 1
			exponent--
		}
		mantissa &= 0x3FF
		return math.Float32frombits(sign | (exponent+127-15)<<23 | mantissa<<13)
	case exponent == 31:
		return math.Float32frombits(sign | 0xFF<<23 | mantissa<<13)
	default:
		return math.Float32frombits(sign | (exponent+127-15)<<23 | mantissa<<13)
	}
}

// --- UV ranges --------------------------------------------------------------

// uvRange is one primitive's UV bounding box, which is what the per-mesh record
// in issue 171 holds: the scale and bias that turn a Unorm16x2 back into a
// coordinate. Scene would derive this at bake, where it already walks every
// vertex for the bounding sphere.
type uvRange struct {
	Bias  m.Vec2 // the minimum corner
	Scale m.Vec2 // the extent; zero on a degenerate axis
}

func newUVRange(uvs []m.Vec2) uvRange {
	if len(uvs) == 0 {
		return uvRange{}
	}
	low, high := uvs[0], uvs[0]
	for _, uv := range uvs[1:] {
		low = m.Vec2{X: min(low.X, uv.X), Y: min(low.Y, uv.Y)}
		high = m.Vec2{X: max(high.X, uv.X), Y: max(high.Y, uv.Y)}
	}
	return uvRange{Bias: low, Scale: high.Sub(low)}
}

// encode maps a coordinate into the range's 16-bit grid. A zero-width axis
// stores zero and the shader multiplies it by a zero scale, which reproduces
// the constant exactly - the CesiumMilkTruck case, where UV0 is a single point.
func (r uvRange) encode(uv m.Vec2) [2]uint16 {
	axis := func(value, bias, scale float32) uint16 {
		if scale == 0 {
			return 0
		}
		normalised := m.Clamp01((value - bias) / scale)
		return uint16(math.Round(float64(normalised) * 65535))
	}
	return [2]uint16{
		axis(uv.X, r.Bias.X, r.Scale.X),
		axis(uv.Y, r.Bias.Y, r.Scale.Y),
	}
}

func (r uvRange) decode(stored [2]uint16) m.Vec2 {
	return m.Vec2{
		X: float32(stored[0])/65535*r.Scale.X + r.Bias.X,
		Y: float32(stored[1])/65535*r.Scale.Y + r.Bias.Y,
	}
}

// encodePacked is encode with the pair folded into one u32, which is how this
// prototype actually carries the fixed-point coordinate. The bytes are
// identical to a Unorm16x2 attribute's; only the declared format differs. See
// the note on UVVertex for why.
func (r uvRange) encodePacked(uv m.Vec2) uint32 {
	pair := r.encode(uv)
	return uint32(pair[0]) | uint32(pair[1])<<16
}

// encodeOctWord packs an octahedral direction plus a handedness bit into one
// u32: bits per axis on the low bits, x then y, and the handedness bit
// immediately above them. Everything above that is reserved and written as
// zero.
//
// This is issue 171's tangent word at bits=15 - oct 15+15, handedness, one
// spare bit - and its oct16 counterpart at bits=8, where the handedness bit
// lands at 16 and fifteen bits go unused. Two bytes of a four-byte attribute
// sitting empty is not a saving anyone would ship; the oct16 tangent is only
// two bytes if the handedness bit finds somewhere else to live, and where that
// is, is a packing question with no picture attached. The prototype spends the
// whole word and the HUD quotes the two bytes the frame would actually weigh.
func encodeOctWord(direction m.Vec3, bits uint, handedness bool) uint32 {
	p := octEncode(direction)
	word := quantizeUnorm(p.X, bits) | quantizeUnorm(p.Y, bits)<<bits
	if handedness {
		word |= 1 << (2 * bits)
	}
	return word
}
