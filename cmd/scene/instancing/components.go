package main

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// Crate marks one crate of the field or the stack, pillars included, and
// carries its serial: its place in the order the setup System spawned the
// crates. Key 2 hands every crate a Params value naming its serial, which is
// what gives each one a Batch key of its own.
//
// It is the demo's only Component. It exists because a Query for Params alone
// would also find the ground and the lamp marker, whose Params the debug shapes'
// own Systems own.
type Crate struct {
	Serial int32
}

// The Component sets, one per act of creation. They are what the setup System
// spawns and nothing else: every Entity in the frame is one of these, placed
// once and never moved, except the camera.
type (
	// crate is a crate of the field or the stack. Tint starts empty, which
	// hashes as no Params at all, so every crate shares one Batch key until key
	// 2 rewrites it.
	crate struct {
		Place m.Transform
		Model scene.Model
		Tint  scene.Params
		Crate Crate
	}
	// prop is a bottle or a glass screen: a Model at a Transform and nothing
	// else, so each file is one Batch per primitive however many stand.
	prop struct {
		Place m.Transform
		Model scene.Model
	}
	ground struct {
		Place m.Transform
		Plane scene.DebugPlane
	}
	marker struct {
		Place  m.Transform
		Sphere scene.DebugSphere
	}
	lamp struct {
		Place m.Transform
		Light scene.Light
	}
	eye struct {
		Place  m.Transform
		Camera scene.Camera
	}
)
