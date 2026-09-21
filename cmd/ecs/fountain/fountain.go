package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Fountain is the state of the fountain itself, shared by the Systems as a
// resource: the clock, the random stream, the meshes baked at startup, the
// tallies and what the HUD shows.
type Fountain struct {
	step       int
	sinceSpawn float32
	random     uint64

	cube, disc scene.MeshRef

	spawned, retired int

	counted HUD // this step's census, shown once scene has flushed it
	shown   HUD
	views   []scene.PassView
}

const (
	stepsPerSecond = 60
	fixedStep      = float32(1) / stepsPerSecond
)

// referenceStep is the step reference.png was captured at.
const referenceStep = 600

func (f *Fountain) clock() float32 { return float32(f.step) * fixedStep }

const randomSeed uint64 = 0x9E3779B97F4A7C15

// next is xorshift64*.
func (f *Fountain) next() uint64 {
	f.random ^= f.random >> 12
	f.random ^= f.random << 25
	f.random ^= f.random >> 27
	return f.random * 2685821657736338717
}

// unit is the next value in [0, 1).
func (f *Fountain) unit() float32 { return float32(f.next()>>40) / float32(1<<24) }

// The nozzle: WaterBottle is 0.260 model units tall with its origin at its
// middle.
const (
	nozzleScale = 4
	nozzleLift  = nozzleScale * 0.130
	nozzleMouth = 2 * nozzleLift
)

const (
	gravity       = 9.8
	riseSpeed     = 5.4
	riseJitter    = 1.6
	spreadSpeed   = 0.7
	spreadJitter  = 1.1
	spawnInterval = 0.012
	flightShare   = 0.96
	moteWidth     = 0.07
	moteStretch   = 0.035 // extra length per unit of speed
)

var (
	moteHot  = m.NewColorSrgb(0.85, 0.97, 1.00, 1)
	moteCold = m.NewColorSrgb(0.05, 0.22, 0.55, 1)
)

// spanOf is a share of the time a mote thrown upward at rise takes to fall back
// to the ground, so every mote expires in the air.
func spanOf(rise float32) float32 {
	flight := (rise + float32(math.Sqrt(float64(rise*rise+2*gravity*nozzleMouth)))) / gravity
	return flight * flightShare
}

// spray decides one mote.
func (f *Fountain) spray() mote {
	rise := riseSpeed + riseJitter*f.unit()
	bearing := 2 * math.Pi * float64(f.unit())
	spread := spreadSpeed + spreadJitter*f.unit()
	motion := Velocity{V: m.Vec3{
		X: spread * float32(math.Cos(bearing)),
		Y: rise,
		Z: spread * float32(math.Sin(bearing)),
	}}
	span := spanOf(rise)
	age := Life{Remaining: span, Span: span}
	place := stretch(m.Vec3{Y: nozzleMouth}, motion)
	return mote{
		Place:  place,
		Draw:   ecsscene.Mesh{Ref: f.cube, Bounds: moteBounds},
		Shade:  moteMaterial(),
		Tint:   ecsscene.Params{Values: ecs.NewList(gfx.ColorParam(moteTintParam, tint(age)))},
		Motion: motion,
		Age:    age,
	}
}

// moteBounds is the unit cube's circumsphere: a custom vertex layout has no
// baked sphere, and a draw without one is never culled.
var moteBounds = m.Vec4{W: 0.87}

// stretch places a mote at position with its local Y along its velocity and
// its length growing with its speed - a non-uniform scale.
func stretch(position m.Vec3, motion Velocity) m.Transform {
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
		Scale:    m.Vec3{X: moteWidth, Y: moteWidth + moteStretch*speed, Z: moteWidth},
	}
}

// tint fades a mote from hot to cold as its life runs out.
func tint(age Life) m.Color {
	left := float32(0)
	if age.Span > 0 {
		left = m.Clamp01(age.Remaining / age.Span)
	}
	return m.Color{
		R: m.Lerp(moteCold.R, moteHot.R, left),
		G: m.Lerp(moteCold.G, moteHot.G, left),
		B: m.Lerp(moteCold.B, moteHot.B, left),
		A: 1,
	}
}

// The fox circles the basin at a speed that swells and ebbs, so its blend
// slides between Walk and Run.
const (
	foxScale  = 0.014
	foxRadius = 4.2
	foxRate   = 0.34 // radians a second, on average
	foxSurge  = 0.62 // how far the angle leads and lags the average
	foxSwell  = 0.45 // radians a second of the surge's own cycle
	foxWalk   = "Walk"
	foxRun    = "Run"
	// The ground speeds, in world units a second, at which the fox is all
	// walk and all run, and at which each clip's authored cycle looks right.
	walkPace = 0.9
	runPace  = 2.3
)

// foxAngle is where on its circle the fox is at time t.
func foxAngle(t float32) float32 {
	return foxRate*t + foxSurge*float32(math.Sin(float64(foxSwell*t)))
}

// foxSpeed is the fox's ground speed at time t.
func foxSpeed(t float32) float32 {
	return foxRadius * (foxRate + foxSurge*foxSwell*float32(math.Cos(float64(foxSwell*t))))
}

// foxPlace stands the fox on its circle at time t, facing along it.
func foxPlace(t float32) m.Transform {
	angle := foxAngle(t)
	sin, cos := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return m.Transform{
		Position: m.Vec3{X: foxRadius * cos, Z: foxRadius * sin},
		Rotation: m.QuatRotationY(-angle),
		Scale:    m.NewVec3(foxScale),
	}
}

// foxBlend is the run weight at a ground speed; walk is the rest.
func foxBlend(speed float32) (walk, run float32) {
	run = m.Clamp01((speed - walkPace) / (runPace - walkPace))
	return 1 - run, run
}

// The lamp over the nozzle, and the spot that follows the fox.
const (
	lampHeight    = 3.2
	lampIntensity = 14
	lampRange     = 11
	spotHeight    = 3.4
	spotIntensity = 30
	spotInner     = 0.22
	spotOuter     = 0.42
)

var (
	lampColor = m.NewColorSrgb(1.00, 0.78, 0.50, 1)
	spotColor = m.NewColorSrgb(0.80, 0.90, 1.00, 1)
)

// spotPlace hangs the spot straight above the fox, aimed down at it.
func spotPlace(t float32) m.Transform {
	target := foxPlace(t).Position
	return m.LookAt(target.Add(m.Vec3{Y: spotHeight}), target, m.Vec3{X: 1})
}

// The camera orbits the basin on the demo clock.
const (
	orbitRadius    = 13
	orbitElevation = 0.40
	orbitRate      = 0.12
	startAzimuth   = -1.62
	fieldOfViewY   = 0.9
)

var orbitTarget = m.Vec3{Y: 0.9}

func cameraPlace(t float32) m.Transform {
	azimuth := float64(startAzimuth + orbitRate*t)
	flat := orbitRadius * float32(math.Cos(orbitElevation))
	eye := orbitTarget.Add(m.Vec3{
		X: flat * float32(math.Sin(azimuth)),
		Y: orbitRadius * float32(math.Sin(orbitElevation)),
		Z: flat * float32(math.Cos(azimuth)),
	})
	return m.LookAt(eye, orbitTarget, m.Vec3{Y: 1})
}

var (
	backdropColor = m.NewColorSrgb(0.03, 0.04, 0.06, 1)
	sunColor      = m.NewColorSrgb(0.60, 0.66, 0.80, 1)
	skyColor      = m.NewColorSrgb(0.10, 0.13, 0.20, 1)
	earthColor    = m.NewColorSrgb(0.06, 0.05, 0.05, 1)
)

// camera is the camera Component: a ground pass that clears colour and depth,
// then the forward pass that clears depth and keeps the ground's colour.
func camera() ecsscene.Camera {
	return ecsscene.Camera{
		ID:            CameraMain,
		FovY:          fieldOfViewY,
		Near:          0.1,
		Far:           100,
		SunDirection:  m.Vec3{X: -0.3, Y: -1, Z: -0.5},
		SunColor:      sunColor,
		SunIntensity:  0.35,
		AmbientSky:    skyColor,
		AmbientGround: earthColor,
		Passes: ecs.NewList(
			scene.Pass{Tag: tagGround, ClearColor: m.Some(backdropColor), ClearDepth: m.Some[float32](1)},
			scene.Pass{Tag: scene.TagForward, ClearDepth: m.Some[float32](1), Order: 1},
		),
	}
}
