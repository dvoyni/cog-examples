package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// The Component sets setupSystem spawns: one per kind of thing in the frame.
// A debug shape is drawn in its Entity's local space, under its Transform, so
// every shape carries one.
type (
	eye struct {
		Place  m.Transform
		Camera scene.Camera
	}
	ground struct {
		Place m.Transform
		Shape scene.DebugPlane
	}
	spinningBox struct {
		Place m.Transform
		Shape scene.DebugBox
		Spin  Spin
	}
	restingBox struct {
		Place m.Transform
		Shape scene.DebugBox
	}
	orbitingSphere struct {
		Place m.Transform
		Shape scene.DebugSphere
		Orbit Orbit
	}
	wireBox struct {
		Place m.Transform
		Shape scene.DebugWireBox
	}
	axis struct {
		Place m.Transform
		Shape scene.DebugLine
	}
	lamp struct {
		Place m.Transform
		Light scene.Light
		Orbit Orbit
	}
	cone struct {
		Place m.Transform
		Light scene.Light
	}
)

// setupSystem spawns everything the demo draws, once, on the init event. From
// here on nothing is spawned or despawned: the Systems only move what is here.
func setupSystem(
	state *ecs.Read[*State],
	eyes *ecs.Spawn[eye],
	grounds *ecs.Spawn[ground],
	spinning *ecs.Spawn[spinningBox],
	rests *ecs.Spawn[restingBox],
	spheres *ecs.Spawn[orbitingSphere],
	wires *ecs.Spawn[wireBox],
	axes *ecs.Spawn[axis],
	lamps *ecs.Spawn[lamp],
	cones *ecs.Spawn[cone],
) {
	s := state.Get()
	eyes.New(eye{Place: s.eyePlace(), Camera: camera()})

	// The ground is a plane through the origin facing up. Its X side runs
	// along world X and its Y side along world Z, so a square needs no
	// rotation at all; the zero Transform is the identity.
	grounds.New(ground{Shape: scene.DebugPlane{
		Normal: m.Vec3{Y: 1},
		Size:   m.Vec2{X: groundSide, Y: groundSide},
		Color:  groundColor,
	}})

	// The spinning box is the TRS case: a position, a rotation and a scalar
	// Scale, all three at once. spinSystem rewrites the rotation every step
	// and leaves the other two as they are here.
	spinAxis := m.Vec3{X: 0.3, Y: 1, Z: 0}.Normalize()
	spinning.New(spinningBox{
		Place: m.At(0, 0.5, 0).
			WithRotation(m.QuatAxisAngle(spinAxis, s.time())).
			WithScale(0.9),
		Shape: scene.DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: spinColor},
		Spin:  Spin{Axis: spinAxis},
	})

	rests.New(resting())

	spheres.New(orbitingSphere{
		Place: m.Transform{Position: s.spinCenter()},
		Shape: scene.DebugSphere{Radius: sphereRadius, Color: sphereColor},
	})

	wires.New(wireBox{
		Place: m.Transform{Position: m.Vec3{Y: 0.5}},
		Shape: scene.DebugWireBox{
			Size: m.Vec3{X: 1.6, Y: 1.6, Z: 1.6}, Width: wireThickness, Color: wireColor,
		},
	})

	// The axis lines keep an identity Transform, so their ends are world
	// positions.
	for _, line := range []scene.DebugLine{
		{To: m.Vec3{X: axisLength}, Color: axisXColor},
		{To: m.Vec3{Y: axisLength}, Color: axisYColor},
		{To: m.Vec3{Z: axisLength}, Color: axisZColor},
	} {
		line.Width = axisThickness
		axes.New(axis{Shape: line})
	}

	// A warm lamp rides above the orbiting sphere, so it moves with it, and a
	// cool spot hangs over the resting box pointing straight down. A Light
	// stands at its Transform's position; the Descr's own Position and
	// Direction are not read.
	lamps.New(lamp{
		Place: m.Transform{Position: s.spinCenter().Add(m.Vec3{Y: lampHeight})},
		Light: scene.Light{Descr: model.LightDescr{
			Kind:      model.LightPoint,
			Color:     lampColor,
			Intensity: lampIntensity,
			Range:     lampRange,
		}},
		Orbit: Orbit{Lift: lampHeight},
	})
	cones.New(spot())
}

// resting is the resting box, the zero-value case: an unrotated, unscaled
// transform whose Scale field is never written, and a zero Scale means 1.
func resting() restingBox {
	return restingBox{
		Place: m.At(restPosition.X, restPosition.Y, restPosition.Z),
		Shape: scene.DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: restColor},
	}
}

// spot is the cool spot hanging coneHeight over the resting box. A Light is
// placed by its Transform, and a spot faces the way its rotation turns -Z,
// which a quarter turn about X takes to straight down.
func spot() cone {
	return cone{
		Place: m.Transform{Position: restPosition.Add(m.Vec3{Y: coneHeight})}.
			WithRotation(m.QuatAxisAngle(m.Vec3{X: 1}, -math.Pi/2)),
		Light: scene.Light{Descr: model.LightDescr{
			Kind:      model.LightSpot,
			Color:     coneColor,
			Intensity: coneIntensity,
			Range:     coneRange,
			InnerCone: coneInner,
			OuterCone: coneOuter,
		}},
	}
}

// camera is the demo's one Camera Component.
func camera() scene.Camera {
	return scene.Camera{
		ID:   CameraMain,
		FovY: fieldOfViewY,
		Near: 0.1,
		Far:  100,
		// Everything else is left at its zero value on purpose, and every zero
		// is the default: Projection is Perspective, CullMask is LayersAll,
		// SunIntensity and AmbientIntensity are 1, and Passes is empty, which
		// emits one implicit forward pass at the camera's own id.
		SunDirection:  m.Vec3{X: -0.45, Y: -1, Z: -0.35},
		SunColor:      m.NewColorSrgb(1, 0.98, 0.94, 1),
		AmbientSky:    m.NewColorSrgb(0.18, 0.22, 0.30, 1),
		AmbientGround: m.NewColorSrgb(0.10, 0.09, 0.08, 1),
	}
}
