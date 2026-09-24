package main

import "github.com/dvoyni/cog/libs/m"

// Spin marks an Entity spinSystem turns: its rotation is Axis turned through
// the demo's time in radians, and its position and scale are left as spawned.
// Axis is a unit vector.
type Spin struct {
	Axis m.Vec3
}

// Orbit marks an Entity orbitSystem carries round the origin with the sphere:
// it stands Lift above the sphere's centre, so the sphere itself has a Lift of
// zero and the lamp riding over it a Lift of lampHeight.
type Orbit struct {
	Lift float32
}
