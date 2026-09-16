package main

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// Census is one tick's reading of the scene, taken after Solve. Every field is
// one clause of the sentence the demo exists to make falsifiable, so a test
// that asserts the sentence and a HUD that prints it read the very same
// numbers.
type Census struct {
	// Step is how many ticks have been solved.
	Step int

	// Contacts is the whole tick's Contact count and Jointed is how many pairs
	// a Joint holds apart. Jointed is the mechanism the figure depends on, and
	// a scene where it read 0 would be a scene where the limbs were merely far
	// enough apart.
	Contacts int
	Jointed  int

	// The stack. Speed is the fastest box's, in m/s; Spin the fastest turn, in
	// rad/s; Drift the furthest any box has strayed from the column, in metres;
	// Tilt the worst lean, in radians; and Height the top box's centre.
	StackSpeed  float64
	StackSpin   float64
	StackDrift  float64
	StackTilt   float64
	StackHeight float64

	// The ramp. Crept is how far the gripping crate has moved from where it was
	// placed, and Slid how far the slipping one has travelled down the slope.
	// SlipSpeed is the slipping crate's down-slope speed and SlipWanted the
	// closed form it is measured against — both in m/s, and equal while the
	// crate is still on the ramp.
	Crept      float64
	Slid       float64
	SlipSpeed  float64
	SlipWanted float64

	// The figure. Stretch is the worst separation, in metres, between the two
	// world anchors of any of its eight pivots — how far the figure has come
	// apart. LimbContacts is how many of the tick's Contacts have a limb at
	// both ends, which is the count JointedPairs keeps at zero.
	Stretch      float64
	LimbContacts int
}

// CensusCmd is how a test asks for the last tick's reading. It is the demo's
// own command, so what a test sees is what the HUD drew.
type CensusCmd kernel.Command[CensusRequest, Census]

// CensusRequest carries nothing: there is one scene and one census of it.
type CensusRequest struct{}

// readCensus answers the command out of the Scene Resource.
func readCensus(_ CensusRequest, state *ecs.Read[*Scene], answer *ecs.Resp[Census]) {
	answer.Set(state.Get().Census)
}

// survey reads the settled tick back into the census. It runs
// After[ecsphysics2d.SolveOnUpdate] — cp's PostSolve — so every number here is
// the tick as the solver left it rather than as Detect found it.
//
// It reaches the Bodies through ecs.Get rather than a Query because what it
// wants is twenty-odd named Entities out of the scene and not a walk over every
// Body: a Query would visit the floor and the ramp to find the five boxes.
func survey(
	state *ecs.Write[*Scene],
	places *ecs.Get[ecsphysics2d.Position],
	velocities *ecs.Get[ecsphysics2d.Velocity],
	joints *ecs.Get[ecsphysics2d.Joint],
	contacts *ecs.Read[*ecsphysics2d.Contacts],
	jointed *ecs.Read[*ecsphysics2d.JointedPairs],
) {
	s := state.Get()
	c := Census{Step: s.Census.Step + 1}

	all := contacts.Get().All()
	c.Contacts = len(all)
	c.Jointed = jointed.Get().Len()
	for _, entry := range all {
		if s.isLimb(entry.A) && s.isLimb(entry.B) {
			c.LimbContacts++
		}
	}

	surveyStack(s, places, velocities, &c)
	surveyRamp(s, places, velocities, &c)
	surveyFigure(s, places, joints, &c)

	s.Census = c
}

// surveyStack measures the column: how fast anything in it still is, how far it
// has strayed and how far it leans.
func surveyStack(
	s *Scene,
	places *ecs.Get[ecsphysics2d.Position],
	velocities *ecs.Get[ecsphysics2d.Velocity],
	c *Census,
) {
	for _, e := range s.stack {
		place, ok := places.Of(e)
		if !ok {
			continue
		}
		velocity, _ := velocities.Of(e)
		c.StackSpeed = max(c.StackSpeed, velocity.Linear.Length())
		c.StackSpin = max(c.StackSpin, math.Abs(velocity.Angular))
		c.StackDrift = max(c.StackDrift, math.Abs(place.Current.X-stackColumnX))
		c.StackTilt = max(c.StackTilt, math.Abs(place.Angle))
		c.StackHeight = max(c.StackHeight, place.Current.Y)
	}
}

// surveyRamp measures the two crates in the slope's own frame: the gripping one
// against where it was put, the slipping one against the closed form.
func surveyRamp(
	s *Scene,
	places *ecs.Get[ecsphysics2d.Position],
	velocities *ecs.Get[ecsphysics2d.Velocity],
	c *Census,
) {
	down, _ := rampFrame()
	c.SlipWanted = float64(c.Step) * fixedStep * slipAccel()

	if place, ok := places.Of(s.gripper); ok {
		c.Crept = place.Current.Sub(s.gripStart).Length()
	}
	if place, ok := places.Of(s.slipper); ok {
		c.Slid = place.Current.Sub(s.slipStart).Dot(down)
	}
	if velocity, ok := velocities.Of(s.slipper); ok {
		c.SlipSpeed = velocity.Linear.Dot(down)
	}
}

// surveyFigure measures how far the figure has come apart: for each of its
// eight pivots, how far the two world anchors the Joint holds together have
// drifted from each other.
//
// A pivot's anchors are local to their Bodies' centres of gravity, so each is
// turned by its own Body's Angle and moved to its own Body's Position, which is
// what the Joint's own solver does at the top of every tick.
func surveyFigure(
	s *Scene,
	places *ecs.Get[ecsphysics2d.Position],
	joints *ecs.Get[ecsphysics2d.Joint],
	c *Census,
) {
	for i := range figurePivotCount {
		joint, ok := joints.Of(s.joints[i])
		if !ok {
			continue
		}
		anchorA, anchorB := joint.Anchors()
		worldA, okA := worldPoint(places, joint.A, anchorA)
		worldB, okB := worldPoint(places, joint.B, anchorB)
		if okA && okB {
			c.Stretch = max(c.Stretch, worldA.Sub(worldB).Length())
		}
	}
}

// worldPoint is where a Body's local anchor stands in the world.
func worldPoint(places *ecs.Get[ecsphysics2d.Position], e ecs.Entity, local m.Vec2d) (m.Vec2d, bool) {
	place, ok := places.Of(e)
	if !ok {
		return m.Vec2d{}, false
	}
	return place.Current.Add(local.Rotate(m.ForAngle(place.Angle))), true
}

// The sentence the demo exists to make falsifiable, one clause a line, drawn on
// the HUD beside the numbers that decide it.
const (
	claimStack  = "the stack rests without buzzing"
	claimRamp   = "the ramp behaves at its friction limit"
	claimFigure = "the figure's limbs stay attached and do not collide with each other"
)

// lines is the HUD's text, as a function so a test reads exactly what is drawn.
func (c Census) lines() []string {
	return []string{
		fmt.Sprintf("ecsphysics2d  step %06d  time %6.2fs  contacts %2d  jointed pairs %2d",
			c.Step, float64(c.Step)*fixedStep, c.Contacts, c.Jointed),
		fmt.Sprintf("stack   %-52s  v %7.4f m/s  w %7.4f rad/s  drift %6.4f m  tilt %6.4f rad",
			claimStack, c.StackSpeed, c.StackSpin, c.StackDrift, c.StackTilt),
		fmt.Sprintf("ramp    %-52s  grip crept %6.4f m  slip %6.3f m at %6.3f m/s, want %6.3f",
			claimRamp, c.Crept, c.Slid, c.SlipSpeed, c.SlipWanted),
		fmt.Sprintf("figure  %-52s  stretch %6.4f m  limb-on-limb contacts %d",
			claimFigure, c.Stretch, c.LimbContacts),
	}
}
