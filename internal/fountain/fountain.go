// Package fountain is what the two fountains share: the spray simulation, the
// clock, the xorshift stream, the layout constants, the geometry and shaders,
// the HUD's text, and the figures the frame at ReferenceStep is expected to
// have.
//
// cmd/ecs/fountain draws the frame through ecsscene and cmd/scene/fountain
// draws it through scene.OpQueue. Neither can import the other, and two copies
// of the simulation would drift until a difference in simulation passed for a
// difference between the renderers. So everything that decides what the frame
// shows lives here, and each command keeps only its recording.
package fountain

import (
	"math"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// The logical screen the HUD is laid out in. The window is fitted to it.
const (
	ScreenWidth  = 960
	ScreenHeight = 540
)

// Demo time is fixed steps, never the wall clock, so step N is the same frame
// on every machine and a test drives N steps directly.
const (
	StepsPerSecond = 60
	FixedStep      = float32(1) / StepsPerSecond
)

// ReferenceStep is the step each fountain's reference.png was captured at.
const ReferenceStep = 600

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

// The nozzle: WaterBottle is 0.260 model units tall with its origin at its
// middle.
const (
	NozzleScale = 4
	NozzleLift  = NozzleScale * 0.130
	NozzleMouth = 2 * NozzleLift
)

// The vendored models, as paths in the asset set.
const (
	NozzlePath = "assets/WaterBottle/WaterBottle.glb"
	FoxPath    = "assets/Fox/Fox.glb"
)

// NozzlePlace stands the nozzle on the ground at the basin's centre.
func NozzlePlace() m.Transform {
	return m.Transform{Position: m.Vec3{Y: NozzleLift}, Scale: m.NewVec3(NozzleScale)}
}

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

// Velocity is how fast a mote travels, in world units a second.
type Velocity struct{ V m.Vec3 }

// Life is how much longer a mote lasts, and how long it had.
type Life struct{ Remaining, Span float32 }

// Mote is one mote as the simulation sees it: where it is, how it moves and
// how long it has left. How it is drawn is the renderer's business.
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

// The fox circles the basin at a speed that swells and ebbs, so its gait
// changes between Walk and Run.
const (
	foxScale  = 0.014
	foxRadius = 4.2
	foxRate   = 0.34 // radians a second, on average
	foxSurge  = 0.62 // how far the angle leads and lags the average
	FoxSwell  = 0.45 // radians a second of the surge's own cycle
	FoxWalk   = "Walk"
	FoxRun    = "Run"
	// The ground speeds, in world units a second, at which each clip's
	// authored cycle looks right: its stride over its own duration.
	WalkPace = 0.9
	RunPace  = 2.3
	// The fox breaks into a run above RunAbove and drops back to a walk below
	// WalkBelow. The gap between them is what keeps a speed hovering at one
	// threshold from flicking the gait back and forth.
	RunAbove  = 1.9
	WalkBelow = 1.3
	// GaitFade is how far, in world units of ground covered, a change of gait
	// crossfades over. It is a distance because the gait machine's clock is.
	GaitFade = 0.8
)

// The triggers the fox's gait machine takes, fired on the speed thresholds.
const (
	TriggerRun  = "run"
	TriggerWalk = "walk"
)

// foxAngle is where on its circle the fox is at time t.
func foxAngle(t float32) float32 {
	return foxRate*t + foxSurge*float32(math.Sin(float64(FoxSwell*t)))
}

// FoxSpeed is the fox's ground speed at time t.
func FoxSpeed(t float32) float32 {
	return foxRadius * (foxRate + foxSurge*FoxSwell*float32(math.Cos(float64(FoxSwell*t))))
}

// FoxPlace stands the fox on its circle at time t, facing along it.
func FoxPlace(t float32) m.Transform {
	angle := foxAngle(t)
	sin, cos := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return m.Transform{
		Position: m.Vec3{X: foxRadius * cos, Z: foxRadius * sin},
		Rotation: m.QuatRotationY(-angle),
		Scale:    m.NewVec3(foxScale),
	}
}

// NewFoxGait builds the fox's gait machine over the clips Fox.glb declares, as
// the lookup's Clips reports them: Walk and Run, each fading to the other on
// its trigger.
//
// The machine's clock is distance, not time. FoxGait steps it by the ground
// the fox covered, and each state's Rate is one over its gait's pace, so a clip
// advances by exactly as much of its cycle as the fox's stride covered and the
// feet stay planted whatever the speed. That is a rate only the game knows,
// which is why it is the game that owns the clock.
func NewFoxGait(clips []model.ClipInfo) (model.ClipMachine, error) {
	return model.NewClipMachine(clips,
		[]model.ClipState{
			{Name: FoxWalk, Clip: FoxWalk, Loop: true, Rate: 1 / WalkPace},
			{Name: FoxRun, Clip: FoxRun, Loop: true, Rate: 1 / RunPace},
		},
		[]model.ClipTransition{
			{From: FoxWalk, To: FoxRun, On: TriggerRun, Crossfade: GaitFade, Ease: model.EaseCubicInOut},
			{From: FoxRun, To: FoxWalk, On: TriggerWalk, Crossfade: GaitFade, Ease: model.EaseCubicInOut},
		})
}

// FoxGaitAt builds the fox's gait machine as it stands after steps 1 to step.
// A fountain rigs the fox once Fox.glb is resident, which may be some steps in,
// and picking the gait up there rather than from a fresh machine is what keeps
// step N the same frame however late the file loaded.
func FoxGaitAt(clips []model.ClipInfo, step int) (model.ClipMachine, error) {
	gait, err := NewFoxGait(clips)
	if err != nil {
		return gait, err
	}
	for s := 1; s <= step; s++ {
		FoxGait(&gait, Clock(s))
	}
	return gait, nil
}

// FoxGait is one step of the fox's gait at time t: the trigger its speed
// calls for, then a Step by the ground covered. A trigger for the gait the fox
// is already in finds no transition and does nothing, so it is fired every
// step the speed calls for it.
func FoxGait(gait *model.ClipMachine, t float32) {
	speed := FoxSpeed(t)
	switch {
	case speed > RunAbove:
		gait.Fire(TriggerRun)
	case speed < WalkBelow:
		gait.Fire(TriggerWalk)
	}
	gait.Step(FixedStep*speed, nil)
}

// The lamp over the nozzle, and the spot that follows the fox.
const (
	LampHeight    = 3.2
	LampIntensity = 14
	LampRange     = 11
	spotHeight    = 3.4
	SpotIntensity = 30
	SpotInner     = 0.22
	SpotOuter     = 0.42
)

var (
	LampColor = m.NewColorSrgb(1.00, 0.78, 0.50, 1)
	SpotColor = m.NewColorSrgb(0.80, 0.90, 1.00, 1)
)

// SpotPlace hangs the spot straight above the fox, aimed down at it.
func SpotPlace(t float32) m.Transform {
	target := FoxPlace(t).Position
	return m.LookAt(target.Add(m.Vec3{Y: spotHeight}), target, m.Vec3{X: 1})
}

// Facing is the direction an unrotated Transform faces, which is the way
// m.LookAt aims and so the way a spot placed by SpotPlace shines.
var Facing = m.Vec3{Z: -1}

// The camera orbits the basin on the demo clock.
const (
	orbitRadius    = 13
	orbitElevation = 0.40
	orbitRate      = 0.12
	startAzimuth   = -1.62
	FieldOfViewY   = 0.9
	Near           = 0.1
	Far            = 100
)

var orbitTarget = m.Vec3{Y: 0.9}

// CameraPlace is the camera on its orbit at time t.
func CameraPlace(t float32) m.Transform {
	azimuth := float64(startAzimuth + orbitRate*t)
	flat := orbitRadius * float32(math.Cos(orbitElevation))
	eye := orbitTarget.Add(m.Vec3{
		X: flat * float32(math.Sin(azimuth)),
		Y: orbitRadius * float32(math.Sin(orbitElevation)),
		Z: flat * float32(math.Cos(azimuth)),
	})
	return m.LookAt(eye, orbitTarget, m.Vec3{Y: 1})
}

// The camera's lighting, and the colour its ground pass clears to.
var (
	BackdropColor = m.NewColorSrgb(0.03, 0.04, 0.06, 1)
	SunColor      = m.NewColorSrgb(0.60, 0.66, 0.80, 1)
	SkyColor      = m.NewColorSrgb(0.10, 0.13, 0.20, 1)
	EarthColor    = m.NewColorSrgb(0.06, 0.05, 0.05, 1)
	SunDirection  = m.Vec3{X: -0.3, Y: -1, Z: -0.5}
)

// SunIntensity scales the camera's sun.
const SunIntensity = 0.35
