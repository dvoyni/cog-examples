package main

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// The demo declares no Component of its own: everything it draws is scene's
// Components beside m.Transform, and everything it steers is the Pbr resource.
// These are the Component sets it spawns, one per kind of thing in the still
// life.
type (
	// slab is a lit mesh under a colour: the ground, and each plinth.
	slab struct {
		Place m.Transform
		Draw  scene.Mesh
		Tint  scene.Params
	}
	// exhibit is one Khronos model standing on its plinth.
	exhibit struct {
		Place m.Transform
		Model scene.Model
	}
	// lamp is one punctual light.
	lamp struct {
		Place m.Transform
		Light scene.Light
	}
	// eye is the camera.
	eye struct {
		Place  m.Transform
		Camera scene.Camera
	}
)
