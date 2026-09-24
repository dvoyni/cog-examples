package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

type (
	cubeQuery struct {
		Shape *scene.DebugBox
		Cube  Cube
	}
	outlineQuery struct {
		Place *m.Transform
		Shape *scene.DebugWireBox
		_     ecs.With[Outline]
	}
)

// tint is the highlight: the picked cube takes the highlight colour and every
// other cube keeps its own, and a wire box on the overlay layer surrounds
// whatever is picked, so the minimap says what the click chose even when the
// thing itself is off the main view's edge.
//
// It is a colour swap on the shape's own Component rather than a material of
// the demo's, because a debug shape's colour is one of its fields: scene's
// debug Systems repaint it, and the one Entity every camera draws changes in
// every camera at once - which is what makes the criterion checkable through
// the composited minimap as well as the main view.
//
// It writes only what differs. A write to a shape is an edit scene's debug
// Systems act on, and a highlight that has not moved is not one.
func tint(shapes *ecs.Query[cubeQuery], outlines *ecs.Query[outlineQuery], state *ecs.Read[*State]) {
	s := state.Get()
	picked := s.pickedName()
	for _, it := range shapes.All() {
		want := cubeColor(it.Cube.Index, picked)
		if it.Shape.Color != want {
			it.Shape.Color = want
		}
	}

	// A box of no size is a shape with nothing to draw, which is how the wire
	// box is hidden while nothing is picked.
	var place m.Transform
	var size m.Vec3
	if picked != "" {
		sphere := s.targets[s.picked].sphere
		place = m.At(sphere.Center.X, sphere.Center.Y, sphere.Center.Z)
		size = m.Vec3{X: sphere.Radius * 2, Y: sphere.Radius * 2, Z: sphere.Radius * 2}
	}
	for _, it := range outlines.All() {
		*it.Place = place
		if it.Shape.Size != size {
			it.Shape.Size = size
		}
	}
}

// cubeColor is one cube's colour given what is picked.
func cubeColor(index int, picked string) m.Color {
	if cubes[index].name == picked {
		return highlightColor
	}
	return cubes[index].color
}
