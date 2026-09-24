package main

import (
	"github.com/dvoyni/cog/libs/m"
)

// The three files, all of them pbr's.
const (
	cratePath  = "assets/BoxVertexColors/BoxVertexColors.glb"
	bottlePath = "assets/WaterBottle/WaterBottle.glb"
	panePath   = "assets/AlphaBlendModeTest/AlphaBlendModeTest.glb"
)

// modelPaths is what the residency counter watches, in the order the HUD names
// them.
var modelPaths = [...]string{cratePath, bottlePath, panePath}

// The files' own measurements, in model units, taken from the same POSITION
// accessors pbr's station table reads. They are constants here for the same
// reason they are there: a placement computed from LookupAccess.Bounds would
// have to wait for residency and move every Entity when it arrived, where the
// lattice is this demo's fixed subject. A file swapped underneath the demo moves
// its models off the floor rather than failing anything.
const (
	// BoxVertexColors is a unit cube authored from a corner, so it spans 0..1
	// on every axis and a crate standing at (x, z) has its transform at
	// (x - width/2, 0, z - depth/2).
	crateMinY = 0
	// WaterBottle is 0.109 x 0.260 x 0.109 about its own middle, minY -0.130.
	bottleMinY = -0.130
	// AlphaBlendModeTest is 8.600 x 2.400 x 1.300 about its own middle,
	// minY -0.100.
	paneMinY = -0.100
)

// The crate lattice. The side is odd so the courtyard has a centre index, and
// the courtyard is the square of lattice sites left out of the middle for the
// bottles and the screens to stand in.
const (
	gridSide      = 25
	gridSpacing   = 1.8
	courtyardHalf = 3 // in lattice steps, so the hole is 7 by 7
	crateSize     = 0.8
)

// The colonnade: the sub-lattice both of whose indices are pillarOffset mod
// pillarStride carries a pillar instead of a crate. A pillar is the same cube
// under a non-uniform Transform.Scale.
const (
	pillarStride = 6
	pillarOffset = 2
	pillarWidth  = 0.5
	pillarHeight = 3.4
)

// The five bottles, in a row across the courtyard.
const (
	bottleCount   = 5
	bottleScale   = 10.0
	bottleSpacing = 2.6
	bottleZ       = -2.4

	// Every other bottle is squashed by a per-axis Scale into a wide, low one,
	// and yawed while it is at it. That is the frame's one non-uniform
	// basis on a curved surface, and it is here because a box cannot show what
	// SCENE_NONUNIFORM is for: every face normal of an axis-aligned box is an
	// eigenvector of an axis-aligned scale, so the world matrix and its
	// inverse-transpose point it the same way and only its length differs. A
	// bottle's shoulder is curved, so its normals are not, and the flag decides
	// where the highlight lands.
	squatWiden   = 2.1
	squatFlatten = 0.55
	squatYaw     = 0.7
)

// The stack: three more crates of the same model, piled in a corner of the
// courtyard where the reference pose keeps all of them. They are spawned apart
// from the field and drawn at their own scale, so they are unmistakable in the
// picture, and they still land in the field's one draw: a Batch is every Entity
// whose key is equal, not every Entity one call placed.
const (
	stackCount = 3
	stackScale = 1.2
)

// stackCorner is where the stack stands, on the courtyard floor.
var stackCorner = m.Vec2{X: 5.4, Y: -5.4}

// The two glass screens, at two depths so the four blended primitives they
// contribute have four distinct distances from the eye. One depth could not
// tell a back-to-front sort from a mesh sort.
const paneScale = 0.7

// paneStands is where each screen stands, in world units on the courtyard
// floor. They overlap across x as seen from the reference eye, so the near one
// tints the far one rather than standing beside it.
var paneStands = [...]m.Vec2{{X: -2.2, Y: 2.0}, {X: 2.2, Y: 4.4}}

// The ground the whole courtyard stands on. It is wider than the lattice so the
// field ends on ground rather than on the edge of the world.
const groundSide = 96

// The frame's own colours, written in sRGB and converted on the way in: Color
// holds linear components, and a demo that typed linear literals would be
// picking its palette in a space no colour picker shows.
var (
	backdropColor = m.NewColorSrgb(0.05, 0.06, 0.09, 1)
	groundColor   = m.NewColorSrgb(0.30, 0.31, 0.34, 1)
	sunColor      = m.NewColorSrgb(1, 0.97, 0.92, 1)
	ambientSky    = m.NewColorSrgb(0.19, 0.23, 0.31, 1)
	ambientGround = m.NewColorSrgb(0.11, 0.10, 0.09, 1)
	lampColor     = m.NewColorSrgb(1.00, 0.84, 0.62, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
)

// sunDirection is the sun's direction of travel.
var sunDirection = m.Vec3{X: -0.4, Y: -1, Z: -0.45}

// The one punctual light: a warm lamp over the courtyard, so the bottles and
// the screens read against a field lit only by the sun. Lighting is pbr's
// subject and this demo declares the least of it that keeps the picture legible.
const (
	lampHeight       = 4.5
	lampIntensity    = 26
	lampRange        = 16
	lampMarkerRadius = 0.25
)

// lampPosition is where the lamp hangs, and where its marker sphere stands.
var lampPosition = m.Vec3{Y: lampHeight, Z: bottleZ}

// The transform lists, built once at package init. They are package-level
// because they never change: the field is the demo's fixed subject, and the
// test recomputes the frustum's verdict from the very same list.
var (
	crateTransforms, pillarCount = buildField()
	bottleTransforms, squatCount = buildBottles()
	paneTransforms               = buildPanes()
	stackTransforms              = buildStack()
)

// The primitive counts of the three files, which are what a Model Entity
// expands to. They are spelled out rather than counted at runtime so that an
// asset swapped underneath the demo fails an assertion instead of quietly
// changing the numbers.
const (
	cratePrimitives  = 1
	bottlePrimitives = 1
	panePrimitives   = 9
	// PaneBlendPrimitives is how many of the screen's primitives are alphaMode
	// BLEND: TestBlendMesh and DecalBlendMesh. They are the ones that split.
	PaneBlendPrimitives = 2
)

// CrateCount is how many crates the lattice holds once the courtyard is taken
// out of it, and PillarCount how many of those are stretched into pillars.
// Both are counted from the layout rather than written down, so the rule and
// the number cannot drift apart.
var (
	CrateCount  = len(crateTransforms)
	PillarCount = pillarCount
)

// debugShapes is how many draws the frame takes from scene's debug shapes: the
// ground plane and the marker on the lamp. Each is a mesh of its own, so each
// is a Batch of its own.
const debugShapes = 2

// Instances is how many instances the frame holds before the camera culls any:
// one per primitive per Model Entity, plus the debug shapes.
var Instances = (CrateCount+stackCount)*cratePrimitives +
	bottleCount*bottlePrimitives + len(paneTransforms)*panePrimitives + debugShapes

// InstancedDraws is how many draws the frame makes with every crate sharing
// one Batch key: the two debug shapes, the field with the stack in it, the
// bottles, the screens' opaque primitives, and one per blended primitive per
// screen. Culling does not change it - a Batch is its Entities, not its
// survivors - until a whole Batch is culled away, which the reference pose does
// not do.
const InstancedDraws = debugShapes + 1 + 1 +
	(panePrimitives - PaneBlendPrimitives) + len(paneStands)*PaneBlendPrimitives

// buildField lays out the crate lattice, leaving the courtyard out of the
// middle and stretching the colonnade's sites into pillars.
//
// The pillar count is returned alongside, and it is what the HUD and the
// assertions call the colonnade rather than a number anyone worked out by hand.
func buildField() ([]m.Transform, int) {
	center := gridSide / 2
	transforms := make([]m.Transform, 0, gridSide*gridSide)
	pillars := 0
	for i := range gridSide {
		for j := range gridSide {
			if inCourtyard(i, j, center) {
				continue
			}
			if isPillar(i, j) {
				transforms = append(transforms, m.Transform{
					Position: m.Vec3{X: latticeAt(i, center) - pillarWidth/2, Z: latticeAt(j, center) - pillarWidth/2},
					Rotation: m.Quat{W: 1},
					Scale:    m.Vec3{X: pillarWidth, Y: pillarHeight, Z: pillarWidth},
				})
				pillars++
				continue
			}
			transforms = append(transforms, m.Transform{
				Position: m.Vec3{
					X: latticeAt(i, center) - crateSize/2,
					Y: -crateMinY * crateSize,
					Z: latticeAt(j, center) - crateSize/2,
				},
				Scale: m.NewVec3(crateSize),
			})
		}
	}
	return transforms, pillars
}

// latticeAt is the world coordinate of lattice index i.
func latticeAt(i, center int) float32 { return float32(i-center) * gridSpacing }

// inCourtyard reports whether a lattice site falls in the square left out of
// the middle.
func inCourtyard(i, j, center int) bool {
	return abs(i-center) <= courtyardHalf && abs(j-center) <= courtyardHalf
}

// isPillar reports whether a lattice site carries a pillar rather than a crate.
// A site inside the courtyard is neither: the lattice is the rule and the
// courtyard is the hole, so the colonnade loses the one site that falls in it.
func isPillar(i, j int) bool {
	return i%pillarStride == pillarOffset && j%pillarStride == pillarOffset
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// buildBottles stands the bottles in a row across the courtyard, each lifted by
// its own minY so it rests on the floor rather than sinking into it.
func buildBottles() ([]m.Transform, int) {
	out := make([]m.Transform, bottleCount)
	squats := 0
	for i := range out {
		if isSquat(i) {
			out[i] = m.Transform{
				Position: m.Vec3{X: spread(i, bottleCount, bottleSpacing), Z: bottleZ},
				Rotation: m.QuatAxisAngle(m.Vec3{Y: 1}, squatYaw),
				Scale:    m.Vec3{X: bottleScale * squatWiden, Y: bottleScale * squatFlatten, Z: bottleScale * squatWiden},
			}
			squats++
			continue
		}
		out[i] = m.Transform{
			Position: m.Vec3{
				X: spread(i, bottleCount, bottleSpacing),
				Y: -bottleMinY * bottleScale,
				Z: bottleZ,
			},
			Scale: m.NewVec3(bottleScale),
		}
	}
	return out, squats
}

// isSquat reports whether the i-th bottle in the row is one of the squashed
// ones. Every other bottle is, so a squat one always stands beside a tall one
// and the pair can be compared without moving the eye.
func isSquat(i int) bool { return i%2 == 1 }

// buildPanes stands the two screens at their own depths.
func buildPanes() []m.Transform {
	out := make([]m.Transform, len(paneStands))
	for i, stand := range paneStands {
		out[i] = m.Transform{
			Position: m.Vec3{X: stand.X, Y: -paneMinY * paneScale, Z: stand.Y},
			Scale:    m.NewVec3(paneScale),
		}
	}
	return out
}

// buildStack piles the stack's crates on one another in the courtyard's corner.
func buildStack() []m.Transform {
	out := make([]m.Transform, stackCount)
	for i := range out {
		out[i] = m.Transform{
			Position: m.Vec3{
				X: stackCorner.X - stackScale/2,
				Y: float32(i) * stackScale,
				Z: stackCorner.Y - stackScale/2,
			},
			Scale: m.NewVec3(stackScale),
		}
	}
	return out
}

// spread is the coordinate of the i-th of n things in a row centred on zero.
func spread(i, n int, spacing float32) float32 {
	return (float32(i) - float32(n-1)/2) * spacing
}
