package main

import (
	"math"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/ecsphysics2dplugin"
	"github.com/dvoyni/cog/libs/m"

	"github.com/dvoyni/cog-examples/internal/headless"
)

// The demo's one claim, and the whole of what these tests are:
//
//	the stack rests without buzzing, the ramp behaves at its friction limit,
//	and the figure's limbs stay attached and do not collide with each other.
//
// One test a clause, each reading the very census the HUD draws, so a run that
// passes here is a run that looks right on screen. Everything is driven through
// internal/headless, GPU-free, exactly as the other demos' acceptance tests are.

// settleTicks is how long each scene is given: about six and a half seconds, by
// which time the stack has stopped, the slipping crate has left the ramp and
// come to rest on the floor, and the figure has swung twice.
const settleTicks = 400

// restTicks is the second reading, taken far enough after the first that a
// column that was buzzing rather than resting would have moved by then.
const restTicks = 600

// slop is the solver's own, left at its default by main.go, and is the unit
// every resting tolerance here is quoted in.
const slop = 0.005

// rig is the demo composed exactly as main does, minus the GPU, with the census
// read back through the demo's own command.
type rig struct {
	engine *headless.Engine
	t      *testing.T
}

func start(t *testing.T) *rig {
	t.Helper()
	return &rig{engine: headless.New(t, ecsplugin.New(), ecsphysics2dplugin.New(), New()), t: t}
}

// steps drives n more ticks and reads the census the last of them left.
func (r *rig) steps(n int) Census {
	r.t.Helper()
	r.engine.Steps(n)
	reply := r.engine.Executioner().ExecuteCommand[CensusCmd](CensusRequest{})
	// The check comes after the dispatch, because a dispatch the kernel could
	// not perform is reported rather than returned: one look covers the ticks
	// and the census both.
	if errs := r.engine.Errors(); len(errs) > 0 {
		r.t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return reply
}

func TestTheStackRestsWithoutBuzzing(t *testing.T) {
	r := start(t)
	settled := r.steps(settleTicks)

	// Resting: the five boxes are a column, square, above where they were
	// dropped, and each interface has sunk no further than a Slop.
	if want := stackBox/2 + (stackHeight-1)*stackBox; math.Abs(settled.StackHeight-want) > stackHeight*slop {
		t.Errorf("the top box rests at y = %v, want %v within %d Slops",
			settled.StackHeight, want, stackHeight)
	}
	if settled.StackDrift > 0.01 {
		t.Errorf("a box strayed %v m from the column, want it above where it was dropped", settled.StackDrift)
	}
	if settled.StackTilt > 0.01 {
		t.Errorf("a box leans %v rad, want the column square", settled.StackTilt)
	}

	// Not buzzing, which is two separate things. Nothing is moving, at either
	// reading —
	for _, c := range []Census{settled, r.steps(restTicks - settleTicks)} {
		if c.StackSpeed > 1e-4 || c.StackSpin > 1e-4 {
			t.Errorf("at step %d the stack still moves at %v m/s and %v rad/s, want neither",
				c.Step, c.StackSpeed, c.StackSpin)
		}
	}
	// — and two hundred ticks apart it is in the same place, which is what
	// rules out a column shivering in place at a speed too small to read.
	rested := r.steps(0)
	if moved := math.Abs(rested.StackHeight - settled.StackHeight); moved > 1e-4 {
		t.Errorf("the top box moved %v m between step %d and step %d, want a column that has stopped",
			moved, settled.Step, rested.Step)
	}
}

func TestTheRampBehavesAtItsFrictionLimit(t *testing.T) {
	// The rule is an iff about one number, so the scene is only a test of it if
	// the two crates fall on opposite sides of that number. A pair's Friction
	// is the plain product of the two Shapes'.
	limit := math.Tan(rampAngle)
	if gripFriction*floorFriction <= limit {
		t.Fatalf("the gripping pair is %v, which does not beat tan %v = %v",
			gripFriction*floorFriction, rampAngle, limit)
	}
	if slipFriction*floorFriction >= limit {
		t.Fatalf("the slipping pair is %v, which is not beaten by tan %v = %v",
			slipFriction*floorFriction, rampAngle, limit)
	}

	r := start(t)
	// While it is on the ramp the slipping crate slides at g(sin t - u cos t),
	// and the increment is counted from the first tick and not the second: the
	// Force gravity writes this tick is turned into velocity by this tick's
	// Solve, so after n ticks the crate has taken n of them.
	const onRamp = 60
	worst := 0.0
	for n := 1; n <= onRamp; n++ {
		c := r.steps(1)
		worst = math.Max(worst, math.Abs(c.SlipSpeed-c.SlipWanted))
		if math.Abs(c.SlipSpeed-c.SlipWanted) > 1e-6 {
			t.Fatalf("after %d ticks the crate slides at %.17g m/s, want the closed form's %.17g",
				n, c.SlipSpeed, c.SlipWanted)
		}
		// The other half of the iff, every tick of the way: the crate whose
		// pair beats the slope does not creep, and a crate that crept a
		// millimetre a tick would be a crate that slides.
		if c.Crept > 1e-5 {
			t.Fatalf("after %d ticks the gripping crate has moved %v m, want none", n, c.Crept)
		}
	}
	t.Logf("the closed form held to %.3g m/s over %d ticks", worst, onRamp)

	// And it keeps holding: by the end the slipping crate has run off the ramp
	// and stopped on the floor, and the gripping one has still not moved.
	done := r.steps(settleTicks - onRamp)
	if done.Slid < rampLength-slipAlong {
		t.Errorf("the slipping crate travelled %v m down a %v m run of ramp, want it off the end",
			done.Slid, rampLength-slipAlong)
	}
	if done.Crept > 1e-5 {
		t.Errorf("after %d ticks the gripping crate has moved %v m, want none", done.Step, done.Crept)
	}
}

func TestTheFiguresLimbsStayAttachedAndDoNotCollideWithEachOther(t *testing.T) {
	r := start(t)

	// The figure comes apart by exactly one step of its own starting swing and
	// never by more. Integrate runs before Solve, so the Velocity every limb is
	// given carries the head a whole step before any Joint has had a chance to
	// answer it; from there the Joint's ErrorBias pulls the error out at the
	// documented rate — the reading falls by a tenth a tick, monotonically, for
	// the first 27 of them — and it never opens again.
	openedByTheSwing := figureSwing * fixedStep
	// recovered is when the error is inside a Slop and stays there: half a
	// second, which is the ErrorBias's 0.9-a-tick applied to 15 mm.
	const recovered = 30
	worst := 0.0
	for n := 1; n <= settleTicks; n++ {
		c := r.steps(1)
		if c.LimbContacts != 0 {
			t.Fatalf("at step %d the tick reports %d Contacts with a limb at both ends, want none",
				n, c.LimbContacts)
		}
		if c.Jointed != figurePivotCount {
			t.Fatalf("at step %d JointedPairs holds %d pairs, want the figure's %d",
				n, c.Jointed, figurePivotCount)
		}
		if c.Stretch > openedByTheSwing+1e-9 {
			t.Fatalf("at step %d a pivot's two anchors are %v m apart, want no more than the %v m one step of the swing opened",
				n, c.Stretch, openedByTheSwing)
		}
		if n >= recovered {
			worst = math.Max(worst, c.Stretch)
		}
	}
	if worst > slop {
		t.Errorf("from step %d on the worst a pivot came apart was %v m, want under a Slop of %v",
			recovered, worst, slop)
	}
	t.Logf("the worst pivot stretch from step %d on was %.3g m, over %d ticks", recovered, worst, settleTicks)
}

// The clause above is only worth asserting if the limbs would otherwise collide.
// This is what says they would: every pair the figure joints genuinely overlaps
// where it is spawned, and no other pair does — so what keeps the figure from
// tearing itself apart is JointedPairs and not the spacing.
//
// It runs over values through ecsphysics2d.Penetration, with no engine at all,
// which is what that primitive is for.
func TestEveryOverlapInTheFigureIsAJointedPair(t *testing.T) {
	jointed := map[[2]int]bool{}
	for _, p := range figurePivots {
		if p.b >= 0 {
			jointed[pairKey(p.a, p.b)] = true
		}
	}

	for a := range figurePartCount {
		for b := a + 1; b < figurePartCount; b++ {
			first, second := figureLimbs[a], figureLimbs[b]
			_, depth, ok := ecsphysics2d.Penetration(
				first.shape(), first.centre, 0, nil,
				second.shape(), second.centre, 0, nil)
			overlaps := ok && depth > 0
			if want := jointed[pairKey(a, b)]; overlaps != want {
				t.Errorf("limbs %d and %d overlap by %v (ok %v); jointed is %v, so they must%s",
					a, b, depth, ok, want, map[bool]string{true: "", false: " not"}[want])
			}
			// An overlap a Joint holds is a real one and not a shared edge: a
			// pair touching at a hair would make no Contact either way and
			// would prove nothing about JointedPairs.
			if jointed[pairKey(a, b)] && depth < 0.02 {
				t.Errorf("limbs %d and %d overlap by only %v m, too little to be a test of anything",
					a, b, depth)
			}
		}
	}
}

func pairKey(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}

// The three halves of the engine are in one scene, which is the whole of what
// this demo is for: the tick's Contact list holds the stack's, the ramp's and
// nothing of the figure's, and JointedPairs is why.
func TestTheOneSceneHoldsAStackARampAndAJointedFigure(t *testing.T) {
	c := start(t).steps(settleTicks)

	// Four interfaces inside the column and one under it, the gripping crate on
	// the ramp, and the slipping crate where it stopped on the floor.
	if want := stackHeight + 2; c.Contacts != want {
		t.Errorf("the settled scene reports %d Contacts, want %d: %d inside the column, 1 under it, "+
			"1 crate on the ramp and 1 on the floor", c.Contacts, want, stackHeight-1)
	}
	if c.Jointed != figurePivotCount {
		t.Errorf("JointedPairs holds %d pairs, want the figure's %d", c.Jointed, figurePivotCount)
	}
	if c.LimbContacts != 0 {
		t.Errorf("%d of those Contacts have a limb at both ends, want none", c.LimbContacts)
	}
	if c.Step != settleTicks {
		t.Errorf("the census is of step %d, want %d", c.Step, settleTicks)
	}
}

// The HUD prints the census it was handed, line for line, so what a test reads
// and what an eye reads are the same numbers. It records into a bare OpQueue,
// which needs no plugins at all.
func TestTheHUDPrintsTheCensusItWasGiven(t *testing.T) {
	census := start(t).steps(settleTicks)
	q := &canvas.OpQueue{}
	drawHUD(q, census)

	var drawn []string
	for _, op := range q.Ops(nil) {
		if op.Kind != canvas.OpText {
			t.Fatalf("the HUD recorded a %v, want text alone", op.Kind)
		}
		if op.Layer != layerHUD {
			t.Errorf("a HUD line landed on layer %d, want %d", op.Layer, layerHUD)
		}
		drawn = append(drawn, op.Text)
	}

	lines := census.lines()
	if len(drawn) != len(lines)+1 {
		t.Fatalf("the HUD drew %d lines, want the census's %d and the footer", len(drawn), len(lines))
	}
	for i, line := range lines {
		if drawn[i] != line {
			t.Errorf("HUD line %d reads %q, want %q", i, drawn[i], line)
		}
	}
	// Each of the three clauses is on the HUD beside the numbers that decide
	// it, because the sentence is the deliverable and a number with no claim
	// beside it is not a claim.
	for i, claim := range []string{claimStack, claimRamp, claimFigure} {
		if !strings.Contains(drawn[i+1], claim) {
			t.Errorf("HUD line %d reads %q, want it to carry %q", i+1, drawn[i+1], claim)
		}
	}
}

// Each of the five Shape kinds draws as its own outline, straight off the
// Component. What is checked is the count of pieces, because that is what says
// the routine read the Shape's geometry rather than drawing a placeholder.
func TestEveryShapeKindDrawsAsItsOwnOutline(t *testing.T) {
	demo := New()
	place := ecsphysics2d.Position{Current: m.Vec2d{X: 1, Y: 2}, Angle: 0.3}

	for _, item := range []struct {
		name  string
		shape ecsphysics2d.Shape
		want  int
	}{
		// A circle is its rim plus the spoke that says which way it is facing.
		{"circle", ecsphysics2d.NewCircleShape(0.5, m.Vec2d{}), circleSegments + 1},
		// A bare segment is one line; fattened by a Radius it is a capsule, and
		// the two parallels are where its surface actually is.
		{"segment", ecsphysics2d.NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0), 1},
		{"capsule", ecsphysics2d.NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.2), 3},
		{"box", ecsphysics2d.NewBoxShape(1, 1, 0), 4},
	} {
		t.Run(item.name, func(t *testing.T) {
			q := &canvas.OpQueue{}
			demo.strokeShape(q, item.shape, ecsphysics2d.Polygon{}, place, floorTint)
			ops := q.Ops(nil)
			if len(ops) != item.want {
				t.Fatalf("a %s drew %d pieces, want %d", item.name, len(ops), item.want)
			}
			for _, op := range ops {
				if op.Layer != layerWorld {
					t.Fatalf("a %s drew on layer %d, want the world's %d", item.name, op.Layer, layerWorld)
				}
			}
		})
	}

	// A polygon of more than four vertices cannot carry them inline and keeps
	// them in a Polygon Component beside the Shape. The drawing routine reads
	// that Component, which a routine written only for the inline kinds would
	// silently draw as nothing.
	corners := make([]m.Vec2d, 0, 6)
	for i := range 6 {
		corners = append(corners, m.ForAngle(-2*math.Pi*float64(i)/6).MulS(0.5))
	}
	shape, polygon, err := ecsphysics2d.NewPolygonShape(corners, 0)
	if err != nil {
		t.Fatalf("hulling a hexagon: %v", err)
	}
	if shape.Kind != ecsphysics2d.ShapePoly {
		t.Fatalf("a hexagon is kind %v, want ShapePoly", shape.Kind)
	}
	q := &canvas.OpQueue{}
	demo.strokeShape(q, shape, polygon, place, floorTint)
	if got := len(q.Ops(nil)); got != len(corners) {
		t.Errorf("a hexagon drew %d pieces, want %d", got, len(corners))
	}
}
