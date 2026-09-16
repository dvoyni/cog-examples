package main

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// Census is one tick's reading of the table, taken after Solve. Every field is
// one clause of the sentence the demo exists to make falsifiable, so a test that
// asserts the sentence and a HUD that prints it read the very same numbers.
type Census struct {
	// Step is how many ticks have been solved, and Seed is what the break was
	// dealt from. The seed never changes and is carried here anyway, because the
	// whole point of printing it is that it travels with the numbers beside it.
	Step int
	Seed uint64

	// Balls and Crates are the pieces on the cloth. Balls is fixed at the break
	// and Crates counts the clicks that have landed.
	Balls, Crates int

	// Contacts is the whole tick's Contact count and Deepest is the worst
	// overlap on any of their points, in metres.
	//
	// Deepest is not the "nothing sinks into anything" clause and cannot be:
	// the step detects after it integrates, so the tick a fast pair is first
	// seen it has already covered a tick of its closing speed, and an overlap of
	// a fifth of a metre on the tick two balls meet head-on is the discrete step
	// working rather than failing. What Deepest is bounded by is that —
	// anything worse would be a pair that had passed part-way through each
	// other before anybody looked.
	//
	// Settled is the clause: the worst overlap on a pair whose two Bodies are
	// both slower than restSpeed. Two things that have stopped cannot have just
	// arrived, so what is left is what the solver chose to leave, and that is a
	// Slop by design.
	Contacts int
	Deepest  float64
	Settled  float64

	// Escaped is how many pieces have their centre outside the playing surface,
	// which is the tunnelling clause, and Outside is the furthest any centre has
	// strayed past a cushion face, in metres. Both are zero on a run that
	// behaved.
	Escaped int
	Outside float64

	// Fastest is the quickest piece, in m/s, and Moving is how many are still
	// above restSpeed. Together they are the "comes to rest" clause: Fastest
	// alone would be satisfied by a hundred balls all creeping.
	Fastest float64
	Moving  int

	// SpreadY is the standard deviation of the balls' y, in metres, and it is
	// how the absence of gravity is read off a settled table rather than taken
	// on trust. The break is dealt over 8 m of cloth, whose uniform spread is
	// 8/sqrt(12) = 2.31 m, and with no gravity the balls stop roughly where the
	// break left them, so it stays near that. A run that had m*g written into
	// Force anywhere would end as a heap a few radii deep against the bottom
	// cushion, and this would collapse to well under a metre.
	SpreadY float64

	// The last click: where the pointer was over the table, and where the crate
	// it made actually is this tick. They are the same point until the table
	// pushes the crate off it, which is what makes the pair a test of the
	// coordinate conversion rather than of the solver.
	AskedAt  m.Vec2d
	LandedAt m.Vec2d
}

// CensusCmd is how a test asks for the last tick's reading. It is the demo's own
// command, so what a test sees is what the HUD drew.
type CensusCmd kernel.Command[CensusRequest, Census]

// CensusRequest carries nothing: there is one table and one census of it.
type CensusRequest struct{}

// readCensus answers the command out of the Table Resource.
func readCensus(_ CensusRequest, state *ecs.Read[*Table], answer *ecs.Resp[Census]) {
	answer.Set(state.Get().Census)
}

// pieceQuery drives the census walk: every Dynamic thing on the cloth. It names
// a Velocity, which is what leaves the four cushions out — a Static body has
// none at all — so the walk needs no marker to tell a piece from a rail.
type pieceQuery struct {
	Place    ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
	Shape    ecsphysics2d.Shape
	Look     Piece
}

// survey reads the settled tick back into the census. It runs
// After[ecsphysics2d.SolveOnUpdate] — cp's PostSolve — so every number here is
// the tick as the solver left it rather than as Detect found it.
func survey(
	state *ecs.Write[*Table],
	pieces *ecs.Query[pieceQuery],
	places *ecs.Get[ecsphysics2d.Position],
	velocities *ecs.Get[ecsphysics2d.Velocity],
	contacts *ecs.Read[*ecsphysics2d.Contacts],
) {
	t := state.Get()
	c := Census{Step: t.Census.Step + 1, Seed: t.Census.Seed, Crates: t.crates, AskedAt: t.asked}

	for _, entry := range contacts.Get().All() {
		c.Contacts++
		deepest := 0.0
		for i := range int(entry.Count) {
			deepest = max(deepest, entry.Points[i].Depth)
		}
		c.Deepest = max(c.Deepest, deepest)
		// A Static cushion has no Velocity at all, which is stopped by
		// definition: Of answers the zero Velocity for it and the pair counts as
		// settled the moment the ball against it has.
		if stopped(velocities, entry.A) && stopped(velocities, entry.B) {
			c.Settled = max(c.Settled, deepest)
		}
	}

	var sumY, sumYY float64
	for _, it := range pieces.All() {
		if !it.Look.Crate {
			c.Balls++
			sumY += it.Place.Current.Y
			sumYY += it.Place.Current.Y * it.Place.Current.Y
		}
		speed := it.Velocity.Linear.Length()
		c.Fastest = max(c.Fastest, speed)
		if speed > restSpeed {
			c.Moving++
		}
		if out := outside(it.Place.Current); out > 0 {
			c.Escaped++
			c.Outside = max(c.Outside, out)
		}
	}

	if c.Balls > 0 {
		mean := sumY / float64(c.Balls)
		c.SpreadY = math.Sqrt(max(sumYY/float64(c.Balls)-mean*mean, 0))
	}

	if t.crates > 0 {
		if place, ok := places.Of(t.last); ok {
			c.LandedAt = place.Current
		}
	}

	t.Census = c
}

// stopped reports whether a Body is slower than restSpeed. A Body with no
// Velocity Component — every cushion — reads as the zero Velocity and so is
// stopped, which is what it is.
func stopped(velocities *ecs.Get[ecsphysics2d.Velocity], e ecs.Entity) bool {
	velocity, _ := velocities.Of(e)
	return velocity.Linear.Length() <= restSpeed
}

// outside is how far a centre has strayed past the nearer cushion face, in
// metres, and zero for a centre on the cloth.
//
// It is the centre and not the surface deliberately. A ball resting against a
// cushion has its surface a Slop inside it, which is the solver working, and
// measuring the surface would make that a stray of 0.005 m on a table where
// nothing is wrong. A centre past the face means the Body is in the wall, and a
// centre past the far face means it tunnelled.
func outside(centre m.Vec2d) float64 {
	return max(math.Abs(centre.X)-tableHalfWidth, math.Abs(centre.Y)-tableHalfHeight, 0)
}

// The sentence the demo exists to make falsifiable, one clause a line, drawn on
// the HUD beside the numbers that decide it.
const (
	claimInside = "nothing leaves the table"
	claimApart  = "nothing sinks into anything"
	claimRest   = "the break comes to rest"
)

// lines is the HUD's text, as a function so a test reads exactly what is drawn.
func (c Census) lines() []string {
	landed := "no crate yet: click the cloth"
	if c.Crates > 0 {
		landed = fmt.Sprintf("last crate asked at %6.2f,%6.2f  landed at %6.2f,%6.2f",
			c.AskedAt.X, c.AskedAt.Y, c.LandedAt.X, c.LandedAt.Y)
	}
	return []string{
		fmt.Sprintf("ecsphysics2d table  step %06d  time %6.2fs  seed %#016x", c.Step, float64(c.Step)*fixedStep, c.Seed),
		fmt.Sprintf("inside  %-30s  balls %3d  crates %2d  escaped %d  worst %6.4f m",
			claimInside, c.Balls, c.Crates, c.Escaped, c.Outside),
		fmt.Sprintf("apart   %-30s  contacts %3d  deepest %6.4f m  at rest %6.4f m = %4.1f Slop",
			claimApart, c.Contacts, c.Deepest, c.Settled, c.Settled/slop),
		fmt.Sprintf("rest    %-30s  fastest %7.4f m/s  still moving %3d  y spread %5.2f m",
			claimRest, c.Fastest, c.Moving, c.SpreadY),
		landed,
	}
}
