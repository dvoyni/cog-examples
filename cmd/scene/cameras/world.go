package main

import (
	"math"

	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// The demo's world, and the two cameras that look at it. Everything here is a
// pure function of demo time, so a test can ask what frame N looks like without
// running one.

// The layers, and the one thing they are load-bearing for.
//
// A camera draws an item iff its layers and the camera's CullMask share a bit,
// and the overlay layer exists because the minimap draws the main camera's own
// frustum. Seen from above that outline is the single most useful thing on the
// map - it is where the main view's edge is, and therefore where a nameplate is
// about to disappear. Seen from inside the main camera it would be four lines
// radiating out of the viewer's own eye, across the whole frame, for ever. So
// the main camera masks the layer out, and that is a CullMask doing work no
// other mechanism in scene could do: the lines are recorded once, for every
// camera, and one camera declines them.
var (
	LayerWorld   = scene.Layer(0)
	LayerOverlay = scene.Layer(1)
)

// The camera ids, all negative, which is the convention when canvas draws
// entirely over a camera - and here it draws every pixel of it, since both
// cameras render into textures canvas then composites.
//
// They are also the demo's pass ordering. gfx sorts every pass in the frame by
// one flat Order and reserves no ranges, so these three and canvas's layers
// share one space: the two -300 and -200 cameras render their targets, the -150
// overlay draws onto the minimap's target, and canvas composites both at layer
// 0 and up. A camera ordered after the composite would render into a texture
// nobody had sampled yet - which is not an error anywhere, just a frame late.
const (
	CameraMain    scene.CameraID = -300
	CameraMap     scene.CameraID = -200
	CameraOverlay scene.CameraID = -150
)

// The demo's fixed timestep. Demo time is accumulated fixed steps, never wall
// clock, so frame N is reproducible and a test drives N steps directly: the
// update event's Dt is deliberately ignored.
const (
	stepsPerSecond = 60
	fixedStep      = 1.0 / float32(stepsPerSecond)
)

// The main camera's track, and the reference pose.
//
// The camera flies down the corridor and back, which is what makes a cube pass
// behind it - and a nameplate that mirrors instead of disappearing is the
// classic bug in WorldToScreen, so the demo has to be able to provoke it rather
// than describe it.
//
// A moving demo cannot take a reproducible reference screenshot while running,
// so this one opens paused at startStep, and R returns to exactly that step.
// startStep is a quarter of the way through the track's period, which puts the
// camera at the corridor's centre: two cubes ahead of it wearing nameplates,
// two behind it wearing none.
const (
	trackSpan   = 9.0 // how far either side of the origin the camera flies
	trackPeriod = 16.0
	startStep   = trackPeriod * stepsPerSecond / 4
	eyeHeight   = 1.5
	eyeTilt     = -0.14 // how far the aim point drops over one unit forward
	fieldOfView = 1.0
	nearPlane   = 0.2
	farPlane    = 60
)

// The minimap's camera: straight down, orthographic, wide enough to hold the
// whole corridor and both flanking props with room for the frustum outline.
//
// Its up vector is -Z, so the corridor runs up the map as it runs away from the
// main camera. Any vector not parallel to the view would do; this one is the
// only choice that makes the two panels agree about which way forward is.
const (
	mapHeight    = 24.0 // eye height above the ground
	mapExtent    = 26.0 // world units across the target's height
	mapNear      = 0.5
	mapFar       = 60
	frustumReach = 13.0 // how far down the main camera's rays the outline is drawn
)

// The corridor: four cubes in two rows, and the camera flies between them.
//
// Their names are what the nameplates print and what the HUD names when one is
// picked, so they are short and unlike each other - "amber" and "azure" share a
// prefix on purpose, because a plate that drew the wrong entry's name would
// otherwise be readable as the right one.
var cubes = [...]struct {
	name     string
	position m.Vec3
	color    m.Color
}{
	{"amber", m.Vec3{X: -1.4, Y: 0.5, Z: -7}, m.NewColorSrgb(0.94, 0.62, 0.28, 1)},
	{"azure", m.Vec3{X: 1.4, Y: 0.5, Z: -2}, m.NewColorSrgb(0.36, 0.66, 0.94, 1)},
	{"ivory", m.Vec3{X: -1.4, Y: 0.5, Z: 3}, m.NewColorSrgb(0.90, 0.89, 0.82, 1)},
	{"moss", m.Vec3{X: 1.4, Y: 0.5, Z: 8}, m.NewColorSrgb(0.44, 0.72, 0.44, 1)},
}

// The rest of the world: a ground plane, the obelisk on one flank and one
// Khronos model on the other.
//
// The model is pbr's own BoxVertexColors, and it is here for a reason beyond
// reusing an asset. It is the demo's one pickable whose bounds scene owns
// rather than the app: the cubes and the obelisk know their own extents, and
// the model asks LookupAccess.Bounds for its local sphere and puts it through
// m.Sphere.Transform. That pairing is exactly what Bounds exists for, and it is
// the half of the picking loop a demo with only debug shapes in it could not
// show.
const modelPath = "assets/BoxVertexColors/BoxVertexColors.glb"

const (
	groundSide  = 30
	cubeSide    = 1.0
	nameplateUp = 0.95 // above a cube's centre, where its plate is anchored
)

var (
	obeliskPosition = m.Vec3{X: 3.0, Z: 6}
	modelPosition   = m.Vec3{X: -2.6, Y: 0.6, Z: 5}
	modelScale      = float32(1.4)
)

// The frame's colours, every one written in sRGB and converted on the way in:
// m.Color holds linear components, and a demo that typed linear literals would
// be picking its palette in a space no colour picker shows.
var (
	backdropColor  = m.NewColorSrgb(0.05, 0.06, 0.08, 1)
	mainClearColor = m.NewColorSrgb(0.10, 0.13, 0.18, 1)
	mapClearColor  = m.NewColorSrgb(0.07, 0.09, 0.11, 1)
	groundColor    = m.NewColorSrgb(0.46, 0.48, 0.52, 1)
	highlightColor = m.NewColorSrgb(1.00, 0.42, 0.62, 1)
	frustumColor   = m.NewColorSrgb(0.98, 0.84, 0.36, 1)
	eyeColor       = m.NewColorSrgb(1.00, 0.96, 0.80, 1)
	sunColor       = m.NewColorSrgb(1, 0.98, 0.94, 1)
	ambientSky     = m.NewColorSrgb(0.20, 0.24, 0.32, 1)
	ambientGround  = m.NewColorSrgb(0.11, 0.10, 0.09, 1)
	hudColor       = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor    = m.NewColorSrgb(0.48, 0.51, 0.58, 1)
	hudWarnColor   = m.NewColorSrgb(0.96, 0.55, 0.42, 1)
	plateColor     = m.NewColorSrgb(0.98, 0.98, 1.00, 1)
)

// The sun travels forward and a little to the left, so it comes from behind and
// above the main camera's shoulder. Every face turned towards that camera takes
// some of it, and the four sides of the obelisk take four different amounts -
// which is what makes a wrong normal in a hand-written shader visible rather
// than merely possible.
var sunDirection = m.Vec3{X: 0.35, Y: -1, Z: 0.55}

// trackZ is where the main camera stands at a given demo time: a triangle wave
// down the corridor and back, so the motion is exactly periodic and step N is
// the same frame on every machine.
func trackZ(time float32) float32 {
	phase := float32(math.Mod(float64(time/trackPeriod), 1))
	if phase < 0 {
		phase++
	}
	if phase > 0.5 {
		phase = 1 - phase
	}
	return -trackSpan + 4*trackSpan*phase
}

// mainCamera is the perspective camera flying the corridor, resolved for a
// given demo time.
//
// It is a whole CameraDescr rather than a transform because the coordinate
// helpers take one: WorldToScreen and ScreenToRay are pure package-level
// functions over a camera and a target size, with no plugin instance and no
// lookup against last frame's state, so the demo hands them the same value it
// recorded. That is what makes the nameplate and the click agree with the
// picture by construction rather than by a frame's luck.
func mainCamera(time float32) scene.CameraDescr {
	eye := m.Vec3{Y: eyeHeight, Z: trackZ(time)}
	return scene.CameraDescr{
		Transform: scene.LookAt(eye, eye.Add(m.Vec3{Y: eyeTilt, Z: 1}), m.Vec3{Y: 1}),
		FovY:      fieldOfView,
		Near:      nearPlane,
		Far:       farPlane,
		CullMask:  LayerWorld,

		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	}
}

// mapCamera is the orthographic camera looking straight down.
//
// Height is the orthographic twin of FovY: world units across the target's
// height, with the width derived from the target's aspect. The minimap's target
// is square, so it shows mapExtent units each way; a camera whose passes
// targeted two different shapes would show different amounts of world in each,
// which is why the projection is resolved per pass rather than per camera.
//
// An orthographic camera never fails WorldToScreen's eye-plane test, because
// its clip w is 1 everywhere. That is not a nicety here: it is why the minimap
// keeps a nameplate the main view has dropped, and the two panels disagreeing
// about a plate is the visible half of the ok contract.
func mapCamera(mask scene.LayerMask) scene.CameraDescr {
	return scene.CameraDescr{
		Transform:  scene.LookAt(m.Vec3{Y: mapHeight}, m.Vec3{}, m.Vec3{Z: -1}),
		Projection: scene.Orthographic,
		Height:     mapExtent,
		Near:       mapNear,
		Far:        mapFar,
		CullMask:   mask,

		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	}
}

// cubeTransform places one corridor cube.
func cubeTransform(i int) scene.Transform {
	c := cubes[i].position
	return scene.At(c.X, c.Y, c.Z).WithScale(cubeSide)
}

// cubeSphere is a cube's bounding sphere in world space, which is what the
// picking loop tests. It is the circumsphere of the cube, so it reports a hit
// on a click that just misses a corner - the honest cost of picking against a
// sphere, and the reason a caller wanting corner-exact picking transforms the
// ray into local space and uses m.Ray.IntersectBox3 instead.
func cubeSphere(i int) m.Sphere {
	return m.Sphere{Center: cubes[i].position, Radius: cubeSide * 0.8660254}
}

// nameplateAnchor is the world point a cube's label is glued to: a little above
// its top face, so the plate sits over the cube rather than inside it.
func nameplateAnchor(i int) m.Vec3 {
	return cubes[i].position.Add(m.Vec3{Y: nameplateUp})
}

// obeliskTransform places the obelisk, and obeliskSphere bounds it for picking.
func obeliskTransform() scene.Transform {
	// Yawed off the world axes so no face of it is parallel to a cube's, which
	// is what puts its four sides at four angles to one sun.
	return scene.At(obeliskPosition.X, obeliskPosition.Y, obeliskPosition.Z).
		WithRotation(m.QuatAxisAngle(m.Vec3{Y: 1}, obeliskYaw))
}

// obeliskYaw is baked into the pick sphere too, in the sense that it does not
// have to be: a sphere is invariant under rotation about its own centre, which
// is exactly why the sphere is the bound a click tests against.
const obeliskYaw = 0.42

func obeliskSphere() m.Sphere {
	return m.Sphere{
		Center: obeliskPosition.Add(m.Vec3{Y: obeliskHeight / 2}),
		Radius: float32(math.Hypot(obeliskBase*math.Sqrt2, obeliskHeight/2)),
	}
}

// modelTransform places the Khronos model.
func modelTransform() scene.Transform {
	return scene.At(modelPosition.X, modelPosition.Y, modelPosition.Z).WithScale(modelScale)
}

// frustumCorners is the four points the main camera's view covers at
// frustumReach units down its corner rays, which is what the minimap draws.
//
// It goes through ScreenToRay rather than through a matrix of its own, and that
// is the point: the outline is built from the same helper the click is, so the
// two cannot disagree. m.Ray.Dir is unit length - NewRay normalises, and
// ScreenToRay builds its result with it - so At(frustumReach) is exactly that
// many world units along the ray from the near plane, with no scale to guess.
func frustumCorners(camera scene.CameraDescr) ([4]m.Vec3, bool) {
	corners := [4]m.Vec2{
		{},
		{X: mainPanel.size.X},
		{X: mainPanel.size.X, Y: mainPanel.size.Y},
		{Y: mainPanel.size.Y},
	}
	var out [4]m.Vec3
	for i, corner := range corners {
		ray, ok := scene.ScreenToRay(camera, mainPanel.size, corner)
		if !ok {
			return out, false
		}
		out[i] = ray.At(frustumReach)
	}
	return out, true
}
