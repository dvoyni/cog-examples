package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// The logical screen the HUD is laid out in. The window is fitted to it.
const (
	ScreenWidth  = 960
	ScreenHeight = 540
)

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

// camera is the camera Component: a ground pass that clears colour and depth,
// then the forward pass that clears depth and keeps the ground's colour.
func camera() scene.Camera {
	return scene.Camera{
		ID:            CameraMain,
		FovY:          FieldOfViewY,
		Near:          Near,
		Far:           Far,
		SunDirection:  SunDirection,
		SunColor:      SunColor,
		SunIntensity:  SunIntensity,
		AmbientSky:    SkyColor,
		AmbientGround: EarthColor,
		Passes: m.NewList(
			scene.Pass{Tag: tagGround, ClearColor: m.Some(BackdropColor), ClearDepth: m.Some[float32](1)},
			scene.Pass{Tag: scene.TagForward, ClearDepth: m.Some[float32](1), Order: 1},
		),
	}
}
