package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The Component sets: one per kind of thing setup spawns.
type (
	// viewer is the camera. orbit places it every tick.
	viewer struct {
		Place  m.Transform
		Camera scene.Camera
	}
	// stage is the ground the row stands on.
	stage struct {
		Place m.Transform
		Draw  scene.Mesh
		Tint  scene.Params
	}
	// runner is the fox whose gait machine fills its Animation.
	runner struct {
		Place m.Transform
		Model scene.Model
		Gait  Gait
		Pose  scene.Animation
	}
	// statue is a model with no Animation at all, which draws its rest frame.
	statue struct {
		Place m.Transform
		Model scene.Model
	}
	// player is a model playing its Playlist on the demo clock.
	player struct {
		Place m.Transform
		Model scene.Model
		Clips Playlist
		Pose  scene.Animation
	}
	// contrasted is a player whose first clip contrast chooses.
	contrasted struct {
		Place    m.Transform
		Model    scene.Model
		Clips    Playlist
		Contrast Contrast
		Pose     scene.Animation
	}
)

// setup spawns the whole row once, on the init event: the camera, the ground,
// and every station's Entities. Nothing is spawned or despawned after it; what
// moves is the camera's Transform and every Animation, which other Systems
// write each tick.
//
// It holds the Lookup because the ground is a mesh baked from geometry rather
// than a file: a stage lit by the same sun as the row, which is what makes the
// row's shadowless figures read as standing on something.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	state *ecs.Write[*Demo],
	viewers *ecs.Spawn[viewer],
	stages *ecs.Spawn[stage],
	runners *ecs.Spawn[runner],
	statues *ecs.Spawn[statue],
	players *ecs.Spawn[player],
	contrasts *ecs.Spawn[contrasted],
) {
	d := state.Get()
	vertices, indices := groundGeometry()
	ground := model.NewLookupAccess(k, lookup.Get()).
		BakeMesh(vertices, indices, gfx.TopologyTriangleList)

	viewers.New(viewer{Camera: scene.Camera{
		ID:   CameraMain,
		FovY: fieldOfViewY,
		Near: nearPlane,
		Far:  farPlane,
		// Everything else is left at its zero value, and every zero is the
		// default: Projection is Perspective, CullMask is LayersAll,
		// SunIntensity and AmbientIntensity are 1, and Passes is empty, which
		// is one default pass at the camera's own id.
		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	}})
	stages.New(stage{
		Draw: scene.Mesh{Ref: ground},
		Tint: scene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", groundColor))},
	})
	d.spawned = 2

	// The fox station: a fox whose gait machine crossfades between two clips,
	// and a fox with no Animation at all.
	//
	// The still one is the point of the pair. An Entity with no Animation
	// draws row 0 of the bake, which is the authored hierarchy resolved once -
	// a real standing pose, not a collapse - and the difference between "the
	// rest frame is a pose" and "the rest frame is a zeroed buffer" is only
	// visible against a moving fox beside it.
	fox := &stations[stationFox]
	runners.New(runner{
		Place: foxTransform(fox.x - foxSpread),
		Model: scene.Model{Ref: model.ModelRef{Path: foxPath}},
	})
	statues.New(statue{
		Place: foxTransform(fox.x + foxSpread),
		Model: scene.Model{Ref: model.ModelRef{Path: foxPath}},
	})
	d.spawned += 2

	// The interpolation station: the nine-cube grid, each cube an Entity
	// drawing one Node with its own clip at full weight, and beside it the
	// same file drawn whole with the four clips the cap lets it keep.
	//
	// Nine Entities rather than one is the whole difference. An Entity's
	// primitives share its Animation, so nine independently animated things
	// are nine Entities - and here they have to be, because one could carry
	// only four of the nine and would dilute those four to a quarter each.
	interp := &stations[stationInterp]
	left := interp.x - interpSpread
	for row := range interpGrid {
		for column := range interpGrid[row] {
			cell := interpGrid[row][column]
			players.New(player{
				// A Node re-roots: the cube's authored place in the file's
				// own grid is discarded and this transform replaces it, which
				// is what lets the demo lay the grid out itself. The clip
				// still reaches the node, because the pose buffer holds bone
				// world transforms and the re-root cancels the authored rest
				// transform rather than the animated one.
				Place: interpCellTransform(left, row, column),
				Model: scene.Model{Ref: model.ModelRef{Path: interpPath, Node: cell.node}},
				Clips: Playlist{Clips: [CapPlays]string{cell.clip}, Weights: [CapPlays]float32{1}},
			})
		}
	}
	// No Node selector: the whole scene, in the file's own layout, which is
	// the same 3x3 grid this demo lays out by hand next to it. It is scaled
	// and lifted to sit level with the grid beside it; the file's own cell is
	// 3.4 units against this demo's interpCell, so the two are close but not
	// identical, and the cap copy is the wider of the two.
	players.New(player{
		Place: m.Transform{
			Position: m.Vec3{X: interp.x + interpSpread, Y: interpBaseY},
			Scale:    m.NewVec3(interpScale),
		},
		Model: scene.Model{Ref: model.ModelRef{Path: interpPath}},
		Clips: capPlaylist(),
	})
	d.spawned += InterpClips + 1

	// The attribute-mask station: the morph cube and its quantized twin,
	// playing the same clip side by side.
	//
	// They deform identically and cost different amounts of GPU memory,
	// because the mask a delta record is packed against is intersected with
	// what the base primitive authored rather than taken from the targets. The
	// plain file authors a TANGENT and its record is three slots; the
	// quantized file authors none and its record is two. A mask read off the
	// targets alone would give both the same stride, and nothing else in the
	// vendored set can tell the two rules apart.
	cube := &stations[stationCube]
	square := Playlist{Clips: [CapPlays]string{cubeClip}, Weights: [CapPlays]float32{1}}
	players.New(player{
		Place: cubeTransform(cube.x - cubeSpread),
		Model: scene.Model{Ref: model.ModelRef{Path: cubePath}},
		Clips: square,
	})
	players.New(player{
		Place: cubeTransform(cube.x + cubeSpread),
		Model: scene.Model{Ref: model.ModelRef{Path: quantizedPath}},
		Clips: square,
	})
	d.spawned += 2

	// The morph station: two copies of one file, the second marked to play a
	// clip of its own while the contrast is on. With it on the two disagree,
	// and with it off (key o) they move together.
	//
	// The contrast clip is sparse on purpose: it walks the eight shapes one at
	// a time, so most of its weights are zero at any moment. Only the nonzero
	// weights are packed into the frame's anim block, so what reaches the GPU
	// is a target or two rather than eight, which is the whole reason the
	// blend is CPU-side.
	stress := &stations[stationStress]
	wave := Playlist{Clips: [CapPlays]string{stressClip}, Weights: [CapPlays]float32{1}}
	players.New(player{
		Place: stressTransform(stress.x - stressSpread),
		Model: scene.Model{Ref: model.ModelRef{Path: stressPath}},
		Clips: wave,
	})
	contrasts.New(contrasted{
		Place:    stressTransform(stress.x + stressSpread),
		Model:    scene.Model{Ref: model.ModelRef{Path: stressPath}},
		Clips:    wave,
		Contrast: Contrast{Clip: stressContrastClip},
	})
	d.spawned += 2
}

// groundGeometry is the stage: one quad on the ground plane, facing up, in the
// standard vertex so the bundled PBR draws it lit under the sun.
func groundGeometry() ([]model.Vertex, []uint32) {
	x, z := float32(groundWidth)/2, float32(groundDepth)/2
	up := m.Vec3{Y: 1}
	corner := func(px, pz float32) model.Vertex {
		return model.Vertex{
			Position: m.Vec3{X: px, Z: pz},
			Normal:   up,
			Tangent:  m.Vec4{X: 1, W: 1},
			Color:    m.White,
		}
	}
	// Counter-clockwise seen from above, which is the front face.
	vertices := []model.Vertex{corner(-x, -z), corner(-x, z), corner(x, z), corner(x, -z)}
	return vertices, []uint32{0, 1, 2, 0, 2, 3}
}

// Fox is the skinned station: a real 24-joint rig with an inverse bind per
// joint, every vertex weighted across four influences, and three clips. Its
// JOINTS_0 are u16, which is the ordinary width; the u8 case this demo covers
// is InterpolationTest's index buffer, not a joint index.
//
// The file is authored in centimetres and lies along Z - 25 wide, 79 tall and
// 155 long - so it is scaled down hard and stood at its own middle in Z. The
// numbers are the file's own POSITION accessor bounds, tabulated here because a
// resident model cannot yet be asked for them through the public surface;
// Bounds on the lookup facade is what will let a demo compute this instead.
const (
	foxScale = 0.027
	// The file's middle in Z, in model units: (-88.095 + 66.625) / 2.
	foxCenterZ = -10.735
	// minY is -0.122, so the lift that stands it on the ground is negligible
	// and stated rather than dropped, because the next file swapped in here
	// will not have one.
	foxLift = 0.122 * foxScale
	// The two foxes stand this far either side of the station's middle.
	foxSpread = 2.4
)

// foxYaw turns the fox broadside to the overview camera, which stands on +Z.
// A gait is read from the side; head-on, a walk and a run look the same.
const foxYaw = math.Pi / 2

// foxTransform stands one fox on the ground at x, turned to face the camera's
// side of the row and carried to its own middle in Z so the two line up with
// each other rather than with the file's origin.
func foxTransform(x float32) m.Transform {
	yaw := m.QuatAxisAngle(m.Vec3{Y: 1}, foxYaw)
	middle := m.TRS4(m.Vec3{}, yaw, m.Vec3{X: foxScale, Y: foxScale, Z: foxScale}).
		TransformPoint(m.Vec3{Z: foxCenterZ})
	return m.Transform{
		Position: m.Vec3{X: x - middle.X, Y: foxLift, Z: -middle.Z},
		Rotation: yaw,
		Scale:    m.NewVec3(foxScale),
	}
}

// The interpolation grid's own geometry. The cubes are unit cubes either side
// of the origin, so a cell is 2 model units and the spacing has to clear the
// translation clips, which move a cube a whole cell.
const (
	interpScale  = 0.34
	interpCell   = 2.9 * interpScale
	interpBaseY  = 1.2
	interpSpread = 2.4 // how far the two grids stand either side of the station
)

// interpCellTransform places one cube of a grid whose middle column stands at x.
func interpCellTransform(x float32, row, column int) m.Transform {
	return m.Transform{
		Position: m.Vec3{
			X: x + float32(column-1)*interpCell,
			Y: interpBaseY + float32(row)*interpCell,
		},
		Scale: m.NewVec3(interpScale),
	}
}

// capPlaylist is the cap copy's Playlist: every clip the file declares offered
// at its row's weight, cut to the four an Animation holds by the rule model
// applies to a draw's plays - fill four, then let a newcomer displace the
// lightest only when it is heavier, so ties go to whoever was offered first.
//
// The demo applies the cut itself because a scene.Animation is four plays by
// type: nine offers cannot reach model at all. The rule is model's, so the four
// that survive here are the four a draw offered all nine would have kept.
func capPlaylist() Playlist {
	var kept Playlist
	count := 0
	for row := range interpGrid {
		for column := range interpGrid[row] {
			clip, weight := interpGrid[row][column].clip, interpRowWeights[row]
			if count < CapPlays {
				kept.Clips[count], kept.Weights[count] = clip, weight
				count++
				continue
			}
			lightest := 0
			for i := 1; i < count; i++ {
				if kept.Weights[i] < kept.Weights[lightest] {
					lightest = i
				}
			}
			if weight > kept.Weights[lightest] {
				kept.Clips[lightest], kept.Weights[lightest] = clip, weight
			}
		}
	}
	return kept
}

// The morph cube station. The two files are the same cube: the plain one
// authors a TANGENT and carries tangent deltas, the quantized one authors
// neither, so their delta records are three slots and two. The node inside
// each already carries a scale of 100 over a mesh authored at a hundredth
// scale, so the model arrives two units across and needs none of its own.
const (
	cubeSpread = 1.7
	cubeLift   = 1.35
	cubeYaw    = 0.9
	cubeClip   = "Square"
)

// cubeTransform stands one morph cube at x, turned off square so two of its
// faces catch the sun at different angles. Face-on it reads as a grey rectangle
// and the deformation is only visible at its silhouette; turned, the shape
// change reads across the whole model.
//
// Both cubes take the same yaw, and the close-up camera stands well back
// instead of close in. Yawing each one to face the camera would present the
// same face from both, but it would also stand them at different angles to a
// directional sun, and two identical cubes at visibly different brightnesses
// invite exactly the conclusion this station exists to rule out. One shared
// yaw keeps the light identical; the distance is what keeps the perspective
// skew between them down to a few degrees, which is far less misleading than a
// difference in shading.
func cubeTransform(x float32) m.Transform {
	return m.Transform{
		Position: m.Vec3{X: x, Y: cubeLift},
		Rotation: m.QuatAxisAngle(m.Vec3{Y: 1}, cubeYaw),
	}
}

// The morph stress station's placement.
const (
	stressScale  = 0.70
	stressLift   = 1.30
	stressSpread = 1.7
)

func stressTransform(x float32) m.Transform {
	return m.Transform{Position: m.Vec3{X: x, Y: stressLift}, Scale: m.NewVec3(stressScale)}
}
