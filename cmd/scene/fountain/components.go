package main

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// Velocity is how fast a mote travels, in world units a second.
type Velocity struct{ V m.Vec3 }

// Life is how much longer a mote lasts, and how long it had.
type Life struct{ Remaining, Span float32 }

// The Component sets: one per act of creation. The fox's gait machine,
// model.ClipMachine, is a Component of the demo's too, registered in Register
// beside Velocity and Life.
type (
	mote struct {
		Place  m.Transform
		Draw   scene.Mesh
		Shade  scene.Material
		Tint   scene.Params
		Motion Velocity
		Age    Life
	}
	nozzle struct {
		Place m.Transform
		Model scene.Model
	}
	fox struct {
		Place m.Transform
		Model scene.Model
		Gait  model.ClipMachine
		Pose  scene.Animation
	}
	basin struct {
		Place m.Transform
		Draw  scene.Mesh
		Shade scene.Material
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
