package main

import "github.com/dvoyni/cog/m"

// pickable is one thing in this demo's world that a click can name: a
// world-space bounding sphere and a label for the HUD.
//
// The demo owns this list. That is the point of the file.
type pickable struct {
	name   string
	sphere m.Sphere
}

// pick is object picking, whole, and it is deliberately here rather than in
// scene.
//
// # Why there is no scene.Raycast
//
// Scene has no list to raycast against. Draws are frame-local and consumed at
// flush - by the time a click arrives the frame that drew the cubes is gone -
// and the retained PassView and BatchView carry dense integers: a mesh id, a
// material id, an instance range. None of that is a thing in the world. A
// scene.Raycast would have to retain a whole second structure, one that exists
// for no other query, kept in step with every draw call in the frame, to answer
// a question the caller can already answer about its own entities.
//
// So it is not a decree that picking is out of scope; it is that the app has
// the list and scene does not. This function is the ten lines that replaces it,
// and the demo ships it rather than an assurance that it is short.
//
// # What the caller supplies
//
// A world-space sphere per candidate. Where each one comes from is the app's
// business and this demo has both cases: the debug cubes know their own half
// extent, and the glTF model asks LookupAccess.Bounds for its local sphere and
// puts it through m.Sphere.Transform - which is exact under the uniform scale a
// scene.Transform carries, and conservative otherwise. That pairing is what
// Bounds exists for.
//
// A model with an AABB that is tighter than its sphere - a long thin thing,
// laid along an axis - takes AABB instead and transforms the *ray* into local
// space with ray.Transform(m.InverseAffine(model)), because an AABB rotated
// into world space is no longer axis-aligned and m.Box3.Transform refits a
// looser box that reports hits on empty air.
//
// # The rules that come for free
//
// m.Ray.IntersectSphere rejects a hit behind the origin, so a cube behind the
// camera is never picked however the ray was built. A ray starting inside a
// sphere returns t = 0, so a camera inside a thing picks it. And Dir is unit
// length by construction - m.NewRay normalises, and ScreenToRay returns a ray
// that was built with it - so t is a world distance and comparing two of them
// is comparing distances rather than parameters of two differently scaled
// lines.
func pick(ray m.Ray, targets []pickable) (int, bool) {
	best, distance := -1, float32(0)
	for i := range targets {
		t, ok := ray.IntersectSphere(targets[i].sphere)
		if !ok || (best >= 0 && t >= distance) {
			continue
		}
		best, distance = i, t
	}
	return best, best >= 0
}
