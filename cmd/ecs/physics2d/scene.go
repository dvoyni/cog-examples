package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// The world, in metres, with +y up and the origin on the floor in the middle of
// the picture. Everything here is a length or a mass; nothing is a pixel.
const (
	// fixedStep is the step app publishes every tick, and the rate every number
	// this demo quotes is quoted at. It is the demo's own copy of what app is
	// configured with, used to spell a time on the HUD and a closed form in the
	// census; the Systems themselves are fed the step out of the event.
	fixedStep = 1.0 / 60

	// gravityAccel is the acceleration the weigh System writes as m*g. It is an
	// acceleration and not a Force, because the Force a Body is pushed by is
	// its own mass times this and the scene carries five different masses.
	gravityAccel = 9.81

	// floorFriction is the floor's and the ramp's, and it is 1 so that a pair's
	// Friction — the plain product of the two Shapes' — is the crate's own
	// number and the ramp arithmetic reads without a second factor in it.
	floorFriction = 1.0
)

// The stack: five boxes in a column, each spawned a centimetre clear of the one
// below, so the settling is something that happens rather than something staged.
const (
	stackHeight  = 5
	stackColumnX = -5.0
	stackBox     = 0.5
	stackGap     = 0.01
	stackMass    = 1.0
	// stackFriction is each box's own. A pair of them is 0.49 and a box on the
	// floor is 0.7, both far above anything the column has to resist, which is
	// what makes it a column rather than a heap: a frictionless stack shuffles
	// a few centimetres before it settles.
	stackFriction = 0.7
)

// The ramp: one 30 degree slope carrying two crates that differ in nothing but
// Friction.
const (
	rampAngle  = math.Pi / 6
	rampLength = 6.0
	// crateSink is how far into the slope each crate is placed: less than the
	// Slop of 0.005 m, so the de-penetration bias is zero from the first tick
	// and the closed form is the solver's own arithmetic rather than that plus
	// a nudge.
	crateSink = 0.002

	crateWidth  = 0.6
	crateHeight = 0.28
	crateMass   = 2.0

	// gripFriction beats tan 30 = 0.57735 and slipFriction does not, which is
	// the whole of the iff the ramp is here to show. Both are multiplied by the
	// slope's 1 to make the pair's.
	gripFriction = 0.8
	slipFriction = 0.25

	// Where along the slope each crate starts, measured from its top. The
	// slipping crate is the lower of the two, so it slides away down an empty
	// ramp and never reaches the one that grips.
	gripAlong = 1.0
	slipAlong = 2.6
)

// rampTop is the slope's upper end. Its lower end is rampLength down the slope
// from there, which lands it on the floor.
var rampTop = m.Vec2d{X: -2.0, Y: 3.0}

// rampFrame is the slope's own two directions: the unit vector down the slope
// and the unit normal out of it, which are what every number about the ramp is
// quoted in.
func rampFrame() (down, out m.Vec2d) {
	down = m.ForAngle(-rampAngle)
	return down, down.Perp()
}

// slipAccel is the closed form the slipping crate slides at: g(sin t - u cos t),
// with u the pair's Friction. It is what the census measures against.
func slipAccel() float64 {
	return gravityAccel * (math.Sin(rampAngle) - slipFriction*floorFriction*math.Cos(rampAngle))
}

// The figure: a torso, a head, two arms and two jointed legs, hanging from a
// shapeless Static anchor by a pivot at the crown.
//
// Every limb is built to overlap the one it is jointed to, by five centimetres
// or so, and to overlap nothing else. That is the whole point of the figure:
// the overlaps are real, and what keeps them from being Contacts is
// JointedPairs and not spacing. TestEveryOverlapInTheFigureIsAJointedPair
// asserts both halves of that through ecsphysics2d.Penetration, over values,
// with no engine running.
const (
	// figureX is far enough right that the slipping crate, which runs off the
	// foot of the ramp and stops on the floor around x = 5.1, comes to rest
	// clear of it rather than under its feet.
	figureX      = 6.2
	figureAnchor = 5.0
	// figureSwing is the horizontal velocity every limb starts with, so the
	// figure arrives swinging rather than hanging dead. It is written as a
	// Velocity and not a Force: a Velocity is immediate and a Force is not,
	// which is also why the crown pivot is a step behind it on the first tick.
	figureSwing = 0.9

	// How far each hinge may turn. The rotary limits are the second Joint kind
	// the figure uses and they are what keeps it a figure rather than a tangle:
	// a limb that may swing a third of a radian against its parent never
	// reaches the limb beside it, so the set of pairs that can ever touch stays
	// the set the pivots hold.
	shoulderSpan = 0.30
	hipSpan      = 0.30
	kneeSpan     = 0.25
)

// The figure's parts, in the order the Scene records them.
const (
	partHead = iota
	partTorso
	partArmLeft
	partArmRight
	partThighLeft
	partThighRight
	partShinLeft
	partShinRight
	figurePartCount
)

// The figure's pivot Joints, in the order the Scene records them. The six
// rotary limits sit after these in the same array, because a limit holds a pair
// a pivot already holds and has no anchors of its own to measure — which is
// also why JointedPairs holds eight pairs and not fourteen.
const (
	jointCrown = iota
	jointNeck
	jointShoulderLeft
	jointShoulderRight
	jointHipLeft
	jointHipRight
	jointKneeLeft
	jointKneeRight
	figurePivotCount
)

// figureJointCount is every Joint Entity the figure carries: the eight pivots
// and the six rotary limits.
const figureJointCount = figurePivotCount + 6

// limb describes one part of the figure: a Shape of that size and mass centred
// there, and the colour it draws in. A radius makes it a circle and a width and
// a height make it a box, which is the head against everything else.
type limb struct {
	centre        m.Vec2d
	radius        float64
	width, height float64
	mass          float64
	tint          m.Color
}

// shape builds the limb's Shape, with the Friction it would need the day one of
// them reached the floor. Between two limbs a Friction never matters: a jointed
// pair makes no Contact for a material to be read off.
func (l limb) shape() ecsphysics2d.Shape {
	var shape ecsphysics2d.Shape
	if l.radius > 0 {
		shape = ecsphysics2d.NewCircleShape(l.radius, m.Vec2d{})
	} else {
		shape = ecsphysics2d.NewBoxShape(l.width, l.height, 0)
	}
	shape.Friction = stackFriction
	return shape
}

// moment is the limb's moment of inertia about its own centre of gravity.
func (l limb) moment() float64 {
	if l.radius > 0 {
		return ecsphysics2d.MomentForCircle(l.mass, 0, l.radius, m.Vec2d{})
	}
	return ecsphysics2d.MomentForBox(l.mass, l.width, l.height)
}

// figureLimbs is the figure as a table, indexed by the part constants. Every x
// below is an offset from figureX, and every y is absolute:
//
//	head   a circle, crown at 4.80, its underside 0.04 inside the torso
//	torso  x −0.22..0.22, y 3.60..4.50
//	arm    x −0.24..−0.12 (mirrored), y 3.90..4.40 — 0.10 of it inside the torso
//	thigh  x −0.21..−0.05 (mirrored), y 3.15..3.65 — 0.05 of it inside the torso
//	shin   x −0.20..−0.06 (mirrored), y 2.70..3.20 — 0.05 of it inside the thigh
//
// Nothing else meets anything: the arms end 0.25 above the thighs, the left
// limbs end 0.10 short of the right ones, and the head clears the arms by 0.11.
var figureLimbs = [figurePartCount]limb{
	partHead:       {centre: m.Vec2d{X: figureX, Y: 4.63}, radius: 0.17, mass: 1.0, tint: skinTint},
	partTorso:      {centre: m.Vec2d{X: figureX, Y: 4.05}, width: 0.44, height: 0.90, mass: 3.0, tint: torsoTint},
	partArmLeft:    {centre: m.Vec2d{X: figureX - 0.18, Y: 4.15}, width: 0.12, height: 0.50, mass: 0.6, tint: limbTint},
	partArmRight:   {centre: m.Vec2d{X: figureX + 0.18, Y: 4.15}, width: 0.12, height: 0.50, mass: 0.6, tint: limbTint},
	partThighLeft:  {centre: m.Vec2d{X: figureX - 0.13, Y: 3.40}, width: 0.16, height: 0.50, mass: 1.0, tint: limbTint},
	partThighRight: {centre: m.Vec2d{X: figureX + 0.13, Y: 3.40}, width: 0.16, height: 0.50, mass: 1.0, tint: limbTint},
	partShinLeft:   {centre: m.Vec2d{X: figureX - 0.13, Y: 2.95}, width: 0.14, height: 0.50, mass: 0.8, tint: limbTint},
	partShinRight:  {centre: m.Vec2d{X: figureX + 0.13, Y: 2.95}, width: 0.14, height: 0.50, mass: 0.8, tint: limbTint},
}

// pivot is one hinge of the figure: which two parts it holds, and where in the
// world the two of them are pinned together.
type pivot struct {
	a, b  int
	world m.Vec2d
}

// figurePivots is every hinge, in the order the Scene records them. jointCrown
// is the one whose B is the Static anchor rather than a limb, and it is spelled
// with b = -1 for that reason.
var figurePivots = [figurePivotCount]pivot{
	jointCrown:         {a: partHead, b: -1, world: m.Vec2d{X: figureX, Y: 4.80}},
	jointNeck:          {a: partHead, b: partTorso, world: m.Vec2d{X: figureX, Y: 4.50}},
	jointShoulderLeft:  {a: partArmLeft, b: partTorso, world: m.Vec2d{X: figureX - 0.22, Y: 4.40}},
	jointShoulderRight: {a: partArmRight, b: partTorso, world: m.Vec2d{X: figureX + 0.22, Y: 4.40}},
	jointHipLeft:       {a: partThighLeft, b: partTorso, world: m.Vec2d{X: figureX - 0.13, Y: 3.62}},
	jointHipRight:      {a: partThighRight, b: partTorso, world: m.Vec2d{X: figureX + 0.13, Y: 3.62}},
	jointKneeLeft:      {a: partShinLeft, b: partThighLeft, world: m.Vec2d{X: figureX - 0.13, Y: 3.18}},
	jointKneeRight:     {a: partShinRight, b: partThighRight, world: m.Vec2d{X: figureX + 0.13, Y: 3.18}},
}

// figureLimits is every rotary limit: which two parts, and how far the first
// may turn against the second.
var figureLimits = [figureJointCount - figurePivotCount]struct {
	a, b int
	span float64
}{
	{partArmLeft, partTorso, shoulderSpan},
	{partArmRight, partTorso, shoulderSpan},
	{partThighLeft, partTorso, hipSpan},
	{partThighRight, partTorso, hipSpan},
	{partShinLeft, partThighLeft, kneeSpan},
	{partShinRight, partThighRight, kneeSpan},
}

// The Component sets: one per kind of thing the scene creates.
type (
	// wall is static geometry — the floor and the ramp. The Shape, the Position
	// and the Tag arrive in one Spawn, so the Shape hook and the Position reach
	// Index together; a Static whose Shape arrives without a Position is
	// skipped silently and never enters the index.
	wall struct {
		Place  ecsphysics2d.Position
		Marker ecsphysics2d.Static
		Shape  ecsphysics2d.Shape
		Tint   Look
	}
	// hook is the shapeless Static the figure hangs from: a Position and the
	// Tag and nothing else, so it is in neither index and collides with
	// nothing, and is still the zero-inverse-mass row a Joint anchors to.
	hook struct {
		Place  ecsphysics2d.Position
		Marker ecsphysics2d.Static
	}
	// body is every Dynamic thing: the stack, the two crates and the figure's
	// limbs. The Force is not optional — a Dynamic body without one falls out
	// of the velocity integrator's Query and never moves.
	body struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Force    ecsphysics2d.Force
		Dynamic  ecsphysics2d.Dynamic
		Shape    ecsphysics2d.Shape
		Tint     Look
	}
	// hinge is a Joint as an app spawns one: an Entity of its own carrying
	// nothing but the Joint, which holds its two Bodies by Reference.
	hinge struct {
		Joint ecsphysics2d.Joint
	}
)

// Look is the one Component the demo declares: what colour a thing draws in.
// Physics has no opinion about it, which is the point — a Component of the
// app's own rides beside the engine's on the same Entity.
type Look struct{ Tint m.Color }

// Scene is the census and the handles it is read through. It is a Resource
// rather than a Component because it is the demo's view of the whole world and
// there is one of it.
type Scene struct {
	// Census is what the last tick left, refilled by survey and drawn by the
	// HUD. A test asks for it through CensusCmd.
	Census Census

	stack   [stackHeight]ecs.Entity
	gripper ecs.Entity
	slipper ecs.Entity

	// gripStart and slipStart are where the two crates were placed, which is
	// what "crept" and "slid" are measured from.
	gripStart, slipStart m.Vec2d

	parts  [figurePartCount]ecs.Entity
	joints [figureJointCount]ecs.Entity
}

// isLimb reports whether an Entity is one of the figure's parts. The figure is
// eight limbs, so a linear scan is the whole of it and a map would be a heap
// allocation to answer eight comparisons.
func (s *Scene) isLimb(e ecs.Entity) bool {
	for _, part := range s.parts {
		if part == e {
			return true
		}
	}
	return false
}

// setup builds the whole scene once, on the init event: the floor and the ramp,
// the stack, the two crates and the figure. Nothing is spawned afterwards, so
// step N is the same frame on every machine.
func setup(
	state *ecs.Write[*Scene],
	walls *ecs.Spawn[wall],
	hooks *ecs.Spawn[hook],
	bodies *ecs.Spawn[body],
	hinges *ecs.Spawn[hinge],
) {
	s := state.Get()
	setupGround(walls)
	setupStack(s, bodies)
	setupRamp(s, bodies)
	setupFigure(s, hooks, bodies, hinges)
}

// setupGround spawns the floor and the ramp, both Static.
func setupGround(walls *ecs.Spawn[wall]) {
	floor := ecsphysics2d.NewBoxShapeFor(ecsphysics2d.NewBB(-7.4, -0.6, 7.4, 0), 0)
	floor.Friction = floorFriction
	walls.New(wall{Shape: floor, Tint: Look{Tint: floorTint}})

	down, _ := rampFrame()
	slope := ecsphysics2d.NewSegmentShape(rampTop, rampTop.Add(down.MulS(rampLength)), 0)
	slope.Friction = floorFriction
	walls.New(wall{Shape: slope, Tint: Look{Tint: rampTint}})
}

// setupStack drops five boxes into a column above the floor.
func setupStack(s *Scene, bodies *ecs.Spawn[body]) {
	shape := ecsphysics2d.NewBoxShape(stackBox, stackBox, 0)
	shape.Friction = stackFriction
	moment := ecsphysics2d.MomentForBox(stackMass, stackBox, stackBox)
	for i := range stackHeight {
		s.stack[i] = bodies.New(body{
			Place: ecsphysics2d.Position{Current: m.Vec2d{
				X: stackColumnX,
				Y: stackBox/2 + float64(i)*(stackBox+stackGap),
			}},
			Dynamic: dynamic(stackMass, moment),
			Shape:   shape,
			Tint:    Look{Tint: stackTint(i)},
		})
	}
}

// setupRamp places the two crates on the slope, flat against it and just inside
// the Slop, one gripping and one slipping.
func setupRamp(s *Scene, bodies *ecs.Spawn[body]) {
	moment := ecsphysics2d.MomentForBox(crateMass, crateWidth, crateHeight)
	place := func(along, friction float64, tint m.Color) (ecs.Entity, m.Vec2d) {
		down, out := rampFrame()
		at := rampTop.Add(down.MulS(along)).Add(out.MulS(crateHeight/2 - crateSink))
		shape := ecsphysics2d.NewBoxShape(crateWidth, crateHeight, 0)
		shape.Friction = friction
		return bodies.New(body{
			// The crate lies flat on the slope, so its Angle is the slope's.
			Place:   ecsphysics2d.Position{Current: at, Angle: -rampAngle},
			Dynamic: dynamic(crateMass, moment),
			Shape:   shape,
			Tint:    Look{Tint: tint},
		}), at
	}
	s.gripper, s.gripStart = place(gripAlong, gripFriction, gripTint)
	s.slipper, s.slipStart = place(slipAlong, slipFriction, slipTint)
}

// setupFigure spawns the eight limbs, the anchor they hang from, the eight
// pivots that hold them together and the six rotary limits that keep the figure
// from folding into itself.
//
// Every Joint says CollideBodies false, which is what puts its pair into
// JointedPairs: Detect drops the pair after the bit filter and the bounding-box
// test, so the Contact is never created and never reported. Without it the arms
// would fight the torso they overlap and the figure would blow itself apart.
func setupFigure(s *Scene, hooks *ecs.Spawn[hook], bodies *ecs.Spawn[body], hinges *ecs.Spawn[hinge]) {
	anchor := hooks.New(hook{Place: ecsphysics2d.Position{Current: m.Vec2d{X: figureX, Y: figureAnchor}}})

	for i, part := range figureLimbs {
		s.parts[i] = bodies.New(body{
			Place:    ecsphysics2d.Position{Current: part.centre},
			Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: figureSwing}},
			Dynamic:  dynamic(part.mass, part.moment()),
			Shape:    part.shape(),
			Tint:     Look{Tint: part.tint},
		})
	}

	for i, p := range figurePivots {
		a := s.parts[p.a]
		b := anchor
		at := m.Vec2d{X: figureX, Y: figureAnchor}
		if p.b >= 0 {
			b = s.parts[p.b]
			at = figureLimbs[p.b].centre
		}
		// PivotAnchors splits the one world pivot into the two anchors, each
		// local to its own Body's centre of gravity. Both limbs start unturned,
		// so the angles handed to it are zero.
		anchorA, anchorB := ecsphysics2d.PivotAnchors(figureLimbs[p.a].centre, 0, at, 0, p.world)
		joint := ecsphysics2d.NewPivotJoint(a, b, anchorA, anchorB)
		joint.CollideBodies = false
		s.joints[i] = hinges.New(hinge{Joint: joint})
	}

	for i, limit := range figureLimits {
		joint := ecsphysics2d.NewRotaryLimitJoint(s.parts[limit.a], s.parts[limit.b], -limit.span, limit.span)
		joint.CollideBodies = false
		s.joints[figurePivotCount+i] = hinges.New(hinge{Joint: joint})
	}
}

// dynamic is NewDynamic with the two Damping rates left at zero, which is what
// every Body here wants: nothing in this scene is meant to coast to a halt on
// its own, so what stops the stack is Friction and what stops the figure is the
// Joints, both of which are the things being shown.
//
// NewDynamic refuses a bad mass or Moment by handing back the zero Dynamic,
// which moves under nothing. Every argument here is a constant this file wrote,
// so the error is dropped rather than plumbed: there is no run in which it is
// not nil, and a demo that returned it would be inventing a failure mode.
func dynamic(mass, moment float64) ecsphysics2d.Dynamic {
	value, _ := ecsphysics2d.NewDynamic(mass, moment, 0, 0)
	return value
}

// weightQuery drives the gravity System: every Dynamic body's Force is written
// and its Dynamic is read, which is the whole of this System's lock set. A
// Static has no Dynamic and a Kinematic has no Dynamic either, so neither
// enters the walk and neither is pushed — which is exactly right.
type weightQuery struct {
	Force   *ecsphysics2d.Force
	Dynamic ecsphysics2d.Dynamic
}

// weigh adds m*g to every Dynamic body. This is the port's addition 4: cp
// carries gravity in its space and this port ships none, so a scene that wants
// it says so in an ordinary System of its own, ordered Before Integrate.
//
// It adds rather than assigns, because Force is what gameplay adds this tick
// and Solve clears it — so a second System pushing the same Body would
// otherwise be silently overwritten by whichever of the two ran last.
func weigh(bodies *ecs.Query[weightQuery]) {
	for _, it := range bodies.All() {
		it.Force.Force.Y -= it.Dynamic.Mass() * gravityAccel
	}
}
