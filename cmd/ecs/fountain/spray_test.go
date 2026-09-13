package main

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
)

// Every mote expires above the ground: its span is derived from its own rise,
// and a mote is retired by its Life and by nothing else.
func TestEveryMoteExpiresInTheAir(t *testing.T) {
	f := &Fountain{random: randomSeed}
	for i := range 2000 {
		born := f.spray()
		span := born.Age.Span
		y := born.Place.Position.Y + born.Motion.V.Y*span - gravity*span*span/2
		if y <= 0 {
			t.Fatalf("mote %d thrown at %v with a span of %vs expires at y=%v", i, born.Motion.V, span, y)
		}
	}
}

// A mote's local Y lies along its velocity, and its length alone grows with its
// speed.
func TestAMoteIsStretchedAlongItsVelocity(t *testing.T) {
	motion := Velocity{V: m.Vec3{X: 1, Y: 5, Z: -2}}
	place := stretch(m.Vec3{}, motion)
	along := place.Rotation.Rotate(m.Vec3{Y: 1})
	if d := along.Sub(motion.V.Normalize()).Length(); d > 1e-4 {
		t.Errorf("local Y points along %v, want %v", along, motion.V.Normalize())
	}
	speed := motion.V.Length()
	if place.Scale.X != moteWidth || place.Scale.Z != moteWidth ||
		math.Abs(float64(place.Scale.Y-(moteWidth+moteStretch*speed))) > 1e-5 {
		t.Errorf("scale %v, want width %v and length %v", place.Scale, moteWidth, moteWidth+moteStretch*speed)
	}
}

// A tint fades from hot at birth to cold at death.
func TestATintFadesWithLife(t *testing.T) {
	if got := tint(Life{Remaining: 2, Span: 2}); got != (m.Color{R: moteHot.R, G: moteHot.G, B: moteHot.B, A: 1}) {
		t.Errorf("a newborn mote is %v, want %v", got, moteHot)
	}
	if got := tint(Life{Remaining: 0, Span: 2}); got != (m.Color{R: moteCold.R, G: moteCold.G, B: moteCold.B, A: 1}) {
		t.Errorf("a spent mote is %v, want %v", got, moteCold)
	}
}

// The fox's blend reaches pure walk and pure run over its surge, so a run of the
// demo shows both gaits.
func TestTheFoxBlendReachesBothGaits(t *testing.T) {
	pureWalk, pureRun := false, false
	period := 2 * math.Pi / foxSwell
	for step := range int(period * stepsPerSecond) {
		walk, run := foxBlend(foxSpeed(float32(step) * fixedStep))
		if walk+run != 1 {
			t.Fatalf("weights %v and %v do not total 1", walk, run)
		}
		pureWalk = pureWalk || walk == 1
		pureRun = pureRun || run == 1
	}
	if !pureWalk || !pureRun {
		t.Errorf("over one surge the fox is pure walk %v and pure run %v; want both", pureWalk, pureRun)
	}
}
