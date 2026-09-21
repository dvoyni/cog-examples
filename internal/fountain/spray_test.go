package fountain

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// Every mote expires above the ground: its span is derived from its own rise,
// and a mote is retired by its Life and by nothing else.
func TestEveryMoteExpiresInTheAir(t *testing.T) {
	s := NewSpray()
	for i := range 2000 {
		born := s.Throw()
		span := born.Age.Span
		y := born.Place.Position.Y + born.Motion.V.Y*span - Gravity*span*span/2
		if y <= 0 {
			t.Fatalf("mote %d thrown at %v with a span of %vs expires at y=%v", i, born.Motion.V, span, y)
		}
	}
}

// A mote's local Y lies along its velocity, and its length alone grows with its
// speed.
func TestAMoteIsStretchedAlongItsVelocity(t *testing.T) {
	motion := Velocity{V: m.Vec3{X: 1, Y: 5, Z: -2}}
	place := Stretch(m.Vec3{}, motion)
	along := place.Rotation.Rotate(m.Vec3{Y: 1})
	if d := along.Sub(motion.V.Normalize()).Length(); d > 1e-4 {
		t.Errorf("local Y points along %v, want %v", along, motion.V.Normalize())
	}
	speed := motion.V.Length()
	if place.Scale.X != MoteWidth || place.Scale.Z != MoteWidth ||
		math.Abs(float64(place.Scale.Y-(MoteWidth+MoteStretch*speed))) > 1e-5 {
		t.Errorf("scale %v, want width %v and length %v", place.Scale, MoteWidth, MoteWidth+MoteStretch*speed)
	}
}

// A tint fades from hot at birth to cold at death.
func TestATintFadesWithLife(t *testing.T) {
	if got := Tint(Life{Remaining: 2, Span: 2}); got != (m.Color{R: MoteHot.R, G: MoteHot.G, B: MoteHot.B, A: 1}) {
		t.Errorf("a newborn mote is %v, want %v", got, MoteHot)
	}
	if got := Tint(Life{Remaining: 0, Span: 2}); got != (m.Color{R: MoteCold.R, G: MoteCold.G, B: MoteCold.B, A: 1}) {
		t.Errorf("a spent mote is %v, want %v", got, MoteCold)
	}
}

// The fox's blend reaches pure walk and pure run over its surge, so a run of the
// demo shows both gaits.
func TestTheFoxBlendReachesBothGaits(t *testing.T) {
	pureWalk, pureRun := false, false
	period := 2 * math.Pi / FoxSwell
	for step := range int(period * StepsPerSecond) {
		walk, run := FoxBlend(FoxSpeed(Clock(step)))
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

// The spray is owed one mote every spawnInterval of demo time, so a second of
// steps throws the same number of motes whichever way the steps fall.
func TestTheSprayThrowsAtItsInterval(t *testing.T) {
	s := NewSpray()
	owed := 0
	for range StepsPerSecond {
		owed += s.Advance()
	}
	if want := int(math.Floor(1 / spawnInterval)); owed < want || owed > want+1 {
		t.Errorf("one second owes %d motes, want %d", owed, want)
	}
	if s.Step() != StepsPerSecond {
		t.Errorf("the spray reached step %d, want %d", s.Step(), StepsPerSecond)
	}
}

// A pass tag is what a label ends in, so canvas's pass is not the camera's.
func TestAPassTagIsWhatTheLabelEndsIn(t *testing.T) {
	for label, want := range map[string]string{
		"scene.camera-100.ground":  TagGround,
		"scene.camera-100.forward": TagForward,
		"canvas.layer":             "layer",
		"forward":                  TagForward,
	} {
		if got := PassTag(label); got != want {
			t.Errorf("PassTag(%q) = %q, want %q", label, got, want)
		}
	}
}
