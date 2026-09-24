package main

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// Demo time is fixed steps, never the wall clock, so step N is the same frame
// on every machine and a test drives N steps directly.
const (
	StepsPerSecond = 60
	FixedStep      = float32(1) / StepsPerSecond
)

// Clock is the demo time at step.
func Clock(step int) float32 { return float32(step) * FixedStep }

// RandomSeed seeds the spray's random stream.
const RandomSeed uint64 = 0x9E3779B97F4A7C15

// Random is the spray's xorshift64* stream.
type Random struct{ state uint64 }

// NewRandom is the stream at RandomSeed.
func NewRandom() Random { return Random{state: RandomSeed} }

// Next is the stream's next value.
func (r *Random) Next() uint64 {
	r.state ^= r.state >> 12
	r.state ^= r.state << 25
	r.state ^= r.state >> 27
	return r.state * 2685821657736338717
}

// Unit is the next value in [0, 1).
func (r *Random) Unit() float32 { return float32(r.Next()>>40) / float32(1<<24) }

const (
	Gravity       = 9.8
	riseSpeed     = 5.4
	riseJitter    = 1.6
	spreadSpeed   = 0.7
	spreadJitter  = 1.1
	spawnInterval = 0.012
	flightShare   = 0.96
	MoteWidth     = 0.07
	MoteStretch   = 0.035 // extra length per unit of speed
)

var (
	MoteHot  = m.NewColorSrgb(0.85, 0.97, 1.00, 1)
	MoteCold = m.NewColorSrgb(0.05, 0.22, 0.55, 1)
)

// Mote is one mote as the simulation decides it: where it is, how it moves and
// how long it has left. How it is drawn is left to the Components throw
// dresses it in.
type Mote struct {
	Place  m.Transform
	Motion Velocity
	Age    Life
}

// Spray is the fountain's clock and random stream: everything that decides
// when a mote is thrown and how.
type Spray struct {
	step       int
	sinceSpawn float32
	random     Random
}

// NewSpray is the spray at step zero, its stream at RandomSeed.
func NewSpray() Spray { return Spray{random: NewRandom()} }

// Step is how many steps the spray has taken.
func (s *Spray) Step() int { return s.step }

// Clock is the demo time the spray has reached.
func (s *Spray) Clock() float32 { return Clock(s.step) }

// Advance takes one step and reports how many motes it is owed. The caller
// throws exactly that many, in order, before anything else draws on the
// stream.
func (s *Spray) Advance() (owed int) {
	s.step++
	s.sinceSpawn += FixedStep
	for s.sinceSpawn >= spawnInterval {
		s.sinceSpawn -= spawnInterval
		owed++
	}
	return owed
}

// Throw decides one mote.
func (s *Spray) Throw() Mote {
	rise := riseSpeed + riseJitter*s.random.Unit()
	bearing := 2 * math.Pi * float64(s.random.Unit())
	spread := spreadSpeed + spreadJitter*s.random.Unit()
	motion := Velocity{V: m.Vec3{
		X: spread * float32(math.Cos(bearing)),
		Y: rise,
		Z: spread * float32(math.Sin(bearing)),
	}}
	span := SpanOf(rise)
	return Mote{
		Place:  Stretch(m.Vec3{Y: NozzleMouth}, motion),
		Motion: motion,
		Age:    Life{Remaining: span, Span: span},
	}
}

// SpanOf is a share of the time a mote thrown upward at rise takes to fall back
// to the ground, so every mote expires in the air.
func SpanOf(rise float32) float32 {
	flight := (rise + float32(math.Sqrt(float64(rise*rise+2*Gravity*NozzleMouth)))) / Gravity
	return flight * flightShare
}

// Fall is one step of gravity on a mote.
func Fall(motion *Velocity) { motion.V.Y -= Gravity * FixedStep }

// Drift moves, stretches and ages a mote by one step, and hands back the tint
// it fades to.
func Drift(place *m.Transform, age *Life, motion Velocity) m.Color {
	age.Remaining -= FixedStep
	*place = Stretch(place.Position.Add(motion.V.MulS(FixedStep)), motion)
	return Tint(*age)
}

// Spent reports whether a mote's life has run out, which is the one thing that
// retires it.
func Spent(age Life) bool { return age.Remaining <= 0 }

// MoteBounds is the unit cube's circumsphere: a custom vertex layout has no
// baked sphere, and a draw without one is never culled.
var MoteBounds = m.Vec4{W: 0.87}

// Stretch places a mote at position with its local Y along its velocity and
// its length growing with its speed - a non-uniform scale.
func Stretch(position m.Vec3, motion Velocity) m.Transform {
	speed := motion.V.Length()
	up := m.Vec3{Y: 1}
	var turn m.Quat
	if speed > 0 {
		along := motion.V.DivS(speed)
		angle := float32(math.Acos(float64(m.Clamp(up.Dot(along), -1, 1))))
		turn = m.QuatAxisAngle(up.Cross(along), angle)
	}
	return m.Transform{
		Position: position,
		Rotation: turn,
		Scale:    m.Vec3{X: MoteWidth, Y: MoteWidth + MoteStretch*speed, Z: MoteWidth},
	}
}

// Tint fades a mote from hot to cold as its life runs out.
func Tint(age Life) m.Color {
	left := float32(0)
	if age.Span > 0 {
		left = m.Clamp01(age.Remaining / age.Span)
	}
	return m.Color{
		R: m.Lerp(MoteCold.R, MoteHot.R, left),
		G: m.Lerp(MoteCold.G, MoteHot.G, left),
		B: m.Lerp(MoteCold.B, MoteHot.B, left),
		A: 1,
	}
}
