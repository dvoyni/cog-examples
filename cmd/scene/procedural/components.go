package main

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// The Component sets: one per kind of Entity setup spawns. The demo declares
// no Component of its own; every one here is scene's, beside the ecs plugin's
// m.Transform.
type (
	// shape is a bundled-PBR draw: a standard-layout Mesh with no Material,
	// its colour riding in Params as the bundled material's baseColorFactor.
	shape struct {
		Place m.Transform
		Draw  scene.Mesh
		Tint  scene.Params
	}
	// piece is caller-owned geometry: a custom-layout Mesh, which cannot draw
	// without a Material, and the demo's own.
	piece struct {
		Place m.Transform
		Draw  scene.Mesh
		Shade scene.Material
	}
	eye struct {
		Place  m.Transform
		Camera scene.Camera
	}
)
