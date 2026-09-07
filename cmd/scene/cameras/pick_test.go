package main

import (
	"testing"

	"github.com/dvoyni/cog/m"
)

// The three spheres the picking tests shoot at: two on the +X axis, one off it.
func pickables() []pickable {
	return []pickable{
		{name: "far", sphere: m.Sphere{Center: m.Vec3{X: 10}, Radius: 1}},
		{name: "near", sphere: m.Sphere{Center: m.Vec3{X: 4}, Radius: 1}},
		{name: "aside", sphere: m.Sphere{Center: m.Vec3{X: 4, Y: 9}, Radius: 1}},
	}
}

func TestPickingKeepsTheNearestHitRatherThanTheFirst(t *testing.T) {
	// "far" is first in the list and second along the ray, which is the whole
	// reason the loop keeps a smallest t instead of returning on the first hit.
	targets := pickables()
	index, ok := pick(m.NewRay(m.Vec3{}, m.Vec3{X: 1}), targets)
	if !ok {
		t.Fatal("a ray straight down the row hit nothing")
	}
	if targets[index].name != "near" {
		t.Errorf("picked %q, want the nearer sphere", targets[index].name)
	}
}

func TestPickingMissesWhenTheRayPassesEverything(t *testing.T) {
	if _, ok := pick(m.NewRay(m.Vec3{Y: 40}, m.Vec3{X: 1}), pickables()); ok {
		t.Error("a ray forty units above the row reported a hit")
	}
}

func TestPickingRejectsWhatIsBehindTheRay(t *testing.T) {
	// m.Ray.IntersectSphere rejects a hit behind the origin, which is what
	// stops a click picking the cube standing behind the camera.
	if _, ok := pick(m.NewRay(m.Vec3{}, m.Vec3{X: -1}), pickables()); ok {
		t.Error("a ray pointing away from the row reported a hit")
	}
}

func TestPickingFromInsideASphereTakesThatSphere(t *testing.T) {
	// A ray starting inside returns t = 0, which is smaller than any other
	// hit's, so standing inside a thing picks it.
	targets := pickables()
	index, ok := pick(m.NewRay(m.Vec3{X: 4}, m.Vec3{X: 1}), targets)
	if !ok || targets[index].name != "near" {
		t.Errorf("picked %v (ok %v) from inside the near sphere", index, ok)
	}
}

func TestPickingNothingIsNotPickingTheFirstThing(t *testing.T) {
	// The empty case has to be a miss and not index zero: the demo clears its
	// highlight on a click that hits nothing, and an index it never checked the
	// bool of would keep the last cube tinted for ever.
	if index, ok := pick(m.NewRay(m.Vec3{}, m.Vec3{X: 1}), nil); ok || index >= 0 {
		t.Errorf("picking an empty world returned (%d, %v)", index, ok)
	}
}
