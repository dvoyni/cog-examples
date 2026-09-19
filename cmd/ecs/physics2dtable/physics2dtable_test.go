package main

import (
	"math"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/ecsphysics2dplugin"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"

	"github.com/dvoyni/cog-examples/internal/headless"
)

// The demo's one claim, and the whole of what these tests are:
//
//	nothing leaves the table, nothing sinks into anything, and the break comes
//	to rest.
//
// One test a clause, each reading the very census the HUD draws, so a run that
// passes here is a run that looks right on screen. Everything is driven through
// internal/headless, GPU-free, exactly as the other demos' acceptance tests are.
//
// Every run here is the default seed's, which is what makes a failure something
// to reproduce rather than something to re-roll. A failure worth reporting is
// reported with the seed the HUD prints.

// breakTicks is how long the break is watched for the two clauses that are
// about every tick and not about the end: five seconds, by which time every
// ball has crossed the table at least twice and met a cushion at speed.
const breakTicks = 300

// settleTicks is how long the table is given to stop. Damping is exponential at
// 0.6/s, so twenty seconds takes the fastest ball this break deals from 6 m/s
// to 6·exp(−12) = 0.00004 m/s, four hundred times under restSpeed, and the
// bounces take their own share out on the way.
const settleTicks = 1200

// rig is the demo composed exactly as main does, minus the GPU, with the census
// read back through the demo's own command.
type rig struct {
	engine *headless.Engine
	t      *testing.T
}

func start(t *testing.T) *rig {
	t.Helper()
	return &rig{
		engine: headless.New(t, ecsplugin.New(), ecsphysics2dplugin.New(), New(DefaultSeed)),
		t:      t,
	}
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

// click drives one press at a point in the demo's logical screen, through the
// window pixels the pointer actually arrives in. The headless viewport is
// 960x540 and so is the demo's screen, so this conversion is the identity here
// — which is why the fit itself is tested over values in
// TestTheScreenAndTheTableAgreeOnWhereAPointIs rather than through the engine.
func (r *rig) click(at m.Vec2) Census {
	r.t.Helper()
	logical := canvas.WorldToScreen(
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe,
		m.Vec2{X: headless.WindowWidth, Y: headless.WindowHeight}, at)
	r.engine.Input(
		input.PointerChange(input.Pos{X: float64(logical.X), Y: float64(logical.Y)}),
		input.KeyChange(input.KeyMouseLeft, 0, true),
	)
	census := r.steps(1)
	r.engine.Input(input.KeyChange(input.KeyMouseLeft, 0, false))
	return census
}

// press drives one press of the re-break key: down for a tick, then up. That is
// one edge, which is what a person pressing space produces and what the demo
// counts. hold, below, is the other thing a key can do.
func (r *rig) press() Census {
	r.t.Helper()
	r.engine.Input(input.KeyChange(rebreakKey, 0, true))
	census := r.steps(1)
	r.engine.Input(input.KeyChange(rebreakKey, 0, false))
	return census
}

// hold puts the re-break key down and leaves it there for n ticks, the way a
// finger resting on space does. The key is re-asserted on every one of them,
// because a platform that repeats a held key delivers exactly that and it is the
// version of "held" most likely to break an edge that is really a level.
func (r *rig) hold(n int) Census {
	r.t.Helper()
	var census Census
	for range n {
		r.engine.Input(input.KeyChange(rebreakKey, 0, true))
		census = r.steps(1)
	}
	return census
}

func TestTheTableRacksUpAsAHundredBallsInsideFourCushions(t *testing.T) {
	first := start(t).steps(1)

	if first.Balls != ballCount {
		t.Errorf("the break dealt %d balls, want %d", first.Balls, ballCount)
	}
	if first.Crates != 0 {
		t.Errorf("the table starts with %d crates, want none until something is clicked", first.Crates)
	}
	if first.Seed != DefaultSeed {
		t.Errorf("the census reports seed %#x, want the default %#x", first.Seed, DefaultSeed)
	}
	// The break is dealt clear of itself: every ball is placed with room around
	// it, so the first tick reports no Contact at all. A rack that started
	// overlapping would be a rack the solver blew apart, and every number after
	// it would be about that rather than about the break.
	if first.Contacts != 0 {
		t.Errorf("the first tick reports %d Contacts, want a rack dealt clear of itself", first.Contacts)
	}
	if first.Escaped != 0 {
		t.Errorf("%d balls were dealt outside the cushions", first.Escaped)
	}
	// And the room is arithmetic and not luck: the narrower lattice spacing,
	// less twice the jitter, is still wider than a ball.
	spacing := min(2*tableHalfWidth/latticeColumns, 2*tableHalfHeight/latticeRows)
	if gap := spacing - 2*latticeJitter - 2*ballRadius; gap <= 0 {
		t.Errorf("two lattice neighbours can be %v m apart at the worst jitter, which is not clear of %v m of ball",
			spacing-2*latticeJitter, 2*ballRadius)
	}
}

func TestNothingLeavesTheTable(t *testing.T) {
	// The margin first, because it is the reason and the run is only the
	// evidence. The step is discrete for everything that is not a Sensor, so
	// what keeps a ball from crossing a cushion unseen is the cushion being
	// deeper than one tick of the fastest thing on the table.
	perTick := speedCeiling * fixedStep
	if cushionDepth < 4*perTick {
		t.Fatalf("a cushion is %v m deep and the fastest allowed Body covers %v m a tick, which is only %.1f ticks of margin",
			cushionDepth, perTick, cushionDepth/perTick)
	}
	t.Logf("a cushion is %.1f ticks deep at the ceiling of %v m/s", cushionDepth/perTick, speedCeiling)

	r := start(t)
	worst := 0.0
	for n := 1; n <= breakTicks; n++ {
		c := r.steps(1)
		if c.Escaped != 0 {
			t.Fatalf("at step %d, %d bodies are outside the table, the worst by %v m; seed %#016x",
				n, c.Escaped, c.Outside, c.Seed)
		}
		worst = math.Max(worst, c.Fastest)
	}
	// Nothing on the table is ever faster than the ceiling, which is the other
	// half of the margin holding: the depth above is a statement about a speed,
	// and it is worth nothing if the speed is not the one the table keeps to.
	if worst > speedCeiling {
		t.Errorf("the quickest anything moved was %v m/s, want no more than the ceiling's %v m/s; seed %#016x",
			worst, speedCeiling, DefaultSeed)
	}
	t.Logf("the quickest anything moved over %d ticks was %.3f m/s, against a deal of %v and a ceiling of %v",
		breakTicks, worst, breakFastest, speedCeiling)
}

// maxFirstTouch is how deep the discrete step may legitimately find a pair the
// first time it sees them. Detection runs after integration, so a pair closing
// at twice the ceiling has already covered a tick of it before anybody looked;
// deeper than that and something went part-way through something.
const maxFirstTouch = 2 * speedCeiling * fixedStep

// maxSettled is what the solver may leave between two things that have both
// stopped: the Slop it is told to leave alone, and one more for the tick the
// last of the overlap is still coming out on.
const maxSettled = 2 * slop

// maxCrateBirth is the deepest a click can put a Body inside another: a crate
// asked for exactly where a ball already is overlaps it by the crate's own
// half-diagonal plus the ball's radius. It bounds nothing but the ticks after a
// click, and it is the one place in this demo where a Shape starts inside
// another rather than arriving there.
const maxCrateBirth = crateSide*math.Sqrt2/2 + ballRadius

func TestNothingSinksIntoAnything(t *testing.T) {
	r := start(t)
	worstTouch, worstSettled, at := 0.0, 0.0, 0
	for n := 1; n <= breakTicks; n++ {
		c := r.steps(1)
		if c.Deepest > worstTouch {
			worstTouch, at = c.Deepest, n
		}
		worstSettled = math.Max(worstSettled, c.Settled)
		if c.Deepest > maxFirstTouch {
			t.Fatalf("at step %d two Shapes overlap by %v m, want under the %v m one tick of closing can make; seed %#016x",
				n, c.Deepest, maxFirstTouch, c.Seed)
		}
		// The clause itself, and it holds on every tick of the break rather
		// than only at the end: whatever is happening elsewhere on the table,
		// two things that have stopped are a Slop apart and not sunk together.
		if c.Settled > maxSettled {
			t.Fatalf("at step %d two stopped Bodies overlap by %v m = %.1f Slops, want under %v m; seed %#016x",
				n, c.Settled, c.Settled/slop, maxSettled, c.Seed)
		}
	}
	t.Logf("over %d ticks the deepest first touch was %.4f m at step %d, and the worst overlap between two stopped Bodies was %.4f m = %.1f Slops",
		breakTicks, worstTouch, at, worstSettled, worstSettled/slop)

	// And at rest, which is where an eye judges it. Whatever the table has left
	// touching is a Slop apart and no more.
	//
	// What it has left touching is usually nothing, and that is worth saying
	// rather than hiding: with no gravity there is nothing pressing the balls
	// together, so they stop where the cloth stopped them and the tick's Contact
	// list empties. It is the same fact TestThereIsNoGravityBecauseNothingWrites
	// Any reads off the spread, seen from the other side — and it is why the
	// pair that really exercises a resting overlap is a clicked crate, which
	// TestACrateDroppedOnTheBreakResolvesRatherThanExploding asserts.
	settled := r.steps(settleTicks)
	if settled.Deepest > maxSettled {
		t.Errorf("on a table that has stopped the deepest overlap is %v m = %.1f Slops, want under %v m; seed %#016x",
			settled.Deepest, settled.Deepest/slop, maxSettled, settled.Seed)
	}
	t.Logf("the table stopped at step %d with %d Contacts left and %.4f m of overlap in them",
		settled.Step, settled.Contacts, settled.Deepest)
}

func TestTheBreakComesToRest(t *testing.T) {
	r := start(t)
	settled := r.steps(settleTicks)

	if settled.Moving != 0 || settled.Fastest > restSpeed {
		t.Errorf("after %d ticks %d pieces still move and the quickest does %v m/s, want everything under %v m/s; seed %#016x",
			settled.Step, settled.Moving, settled.Fastest, restSpeed, settled.Seed)
	}
	// And it has stopped rather than crept to a halt this once: a further two
	// seconds moves nothing, which is what rules out a table shivering at a
	// speed too small to read.
	rested := r.steps(120)
	if rested.Fastest > settled.Fastest {
		t.Errorf("between step %d and step %d the table sped up from %v to %v m/s, want a table that has stopped",
			settled.Step, rested.Step, settled.Fastest, rested.Fastest)
	}
	t.Logf("the table was still at %.5f m/s by step %d and %.5f m/s by step %d",
		settled.Fastest, settled.Step, rested.Fastest, rested.Step)
}

// The clause above is only a clause about this demo if the table stops because
// of the cloth. This is what says so: Damping is exponential in a rate per
// second, so the closed form is what a ball with the table to itself follows,
// and the settling ticks are chosen against it rather than found by trying.
func TestTheClothIsExponentialAndNotAGuess(t *testing.T) {
	left := breakFastest * math.Exp(-ballDamping*settleTicks*fixedStep)
	if left > restSpeed/10 {
		t.Errorf("after %d ticks the cloth leaves the fastest ball %v m/s, which is not comfortably under %v m/s",
			settleTicks, left, restSpeed)
	}
	t.Logf("the cloth alone takes %v m/s to %.3g m/s over %d ticks", breakFastest, left, settleTicks)
}

// There is no gravity in this program, and this is how that is read off the
// table rather than taken on trust: a settled table is still spread over the
// cloth. Write m*g into Force anywhere and the hundred balls end as a heap a few
// radii deep against the bottom cushion, and the spread collapses.
func TestThereIsNoGravityBecauseNothingWritesAny(t *testing.T) {
	c := start(t).steps(settleTicks)

	// Eight metres of cloth spread uniformly is a standard deviation of
	// 8/sqrt(12) = 2.31 m. A heap is the other thing it could be: a hundred
	// balls packed against the 15 m bottom cushion is about three rows, under a
	// metre of table, whose spread is a quarter of a metre. 1.5 m is between the
	// two and near neither.
	const heaped = 1.5
	if c.SpreadY < heaped {
		t.Errorf("the settled balls are spread %v m in y, want more than %v m; under gravity they would be a heap; seed %#016x",
			c.SpreadY, heaped, c.Seed)
	}
	t.Logf("the settled balls are spread %.2f m in y, against %.2f m for a uniform cloth",
		c.SpreadY, 2*tableHalfHeight/math.Sqrt(12))
}

func TestAClickedCrateLandsWhereItWasAskedFor(t *testing.T) {
	r := start(t)
	r.steps(30)

	for _, at := range []m.Vec2{
		{X: screenWidth / 2, Y: screenHeight / 2}, // the middle of the cloth
		{X: 200, Y: 140},
		{X: 760, Y: 400},
	} {
		want := unproject(at)
		c := r.click(at)

		if c.AskedAt != want {
			t.Errorf("a click at screen %v asked for world %v, want %v", at, c.AskedAt, want)
		}
		// The crate is spawned Before Integrate, so the tick it was clicked on
		// is a tick it was solved on: the only thing that can have moved it is
		// the de-penetration bias pushing it off whatever it landed on, and
		// that is a tenth of the overlap in one tick.
		if moved := c.LandedAt.Sub(want).Length(); moved > 0.05 {
			t.Errorf("a crate asked for at %v is at %v, %v m away; seed %#016x",
				want, c.LandedAt, moved, c.Seed)
		}
		r.steps(30)
	}

	if c := r.steps(0); c.Crates != 3 {
		t.Errorf("three clicks made %d crates", c.Crates)
	}
}

// A crate dropped on top of the break has to resolve rather than explode, which
// is the one thing a click can do to this scene that the scene cannot do to
// itself: it is the only Body that ever appears inside another.
func TestACrateDroppedOnTheBreakResolvesRatherThanExploding(t *testing.T) {
	r := start(t)
	r.steps(30)
	dropped := r.click(m.Vec2{X: screenWidth / 2, Y: screenHeight / 2})
	if dropped.Crates != 1 {
		t.Fatalf("the click made %d crates, want 1", dropped.Crates)
	}

	// A crate dropped at the cursor is the one Body in this scene that can be
	// born inside another, and on a table of a hundred balls it usually is:
	// what the click promises is the cursor and not an empty space. So the
	// overlap it starts with is not bounded by a tick of closing at all — it is
	// bounded by how far inside something a Body can be put, which is the
	// crate's half-diagonal plus a ball's radius — and what is asserted is that
	// it goes. The solver's answer is a position bias of a tenth of the overlap
	// a tick, so the overlap leaves rather than the crate.
	const resolved = 60
	worst := 0.0
	for n := 1; n <= breakTicks; n++ {
		c := r.steps(1)
		if c.Escaped != 0 {
			t.Fatalf("%d ticks after the crate landed, %d bodies are outside the table, the worst by %v m; seed %#016x",
				n, c.Escaped, c.Outside, c.Seed)
		}
		if c.Deepest > maxCrateBirth {
			t.Fatalf("%d ticks after the crate landed two Shapes overlap by %v m, want no more than the %v m a click can put one inside another; seed %#016x",
				n, c.Deepest, maxCrateBirth, c.Seed)
		}
		if n >= resolved {
			// And by then it is an ordinary tick again: the birth is out and
			// nothing is left but what the solver keeps.
			if c.Deepest > maxFirstTouch {
				t.Fatalf("%d ticks after the crate landed two Shapes still overlap by %v m, want under the %v m one tick of closing can make; seed %#016x",
					n, c.Deepest, maxFirstTouch, c.Seed)
			}
			if c.Settled > maxSettled {
				t.Fatalf("%d ticks after the crate landed two stopped Bodies still overlap by %v m = %.1f Slops; seed %#016x",
					n, c.Settled, c.Settled/slop, c.Seed)
			}
		}
		worst = math.Max(worst, c.Fastest)
	}
	// And nothing is flung: the crate arrives with no velocity of its own and
	// the de-penetration bias is a position delta and not an impulse, so the
	// table cannot come out of it faster than the ceiling it went in under.
	if worst > speedCeiling {
		t.Errorf("after the crate landed something reached %v m/s, want no more than the ceiling's %v m/s; seed %#016x",
			worst, speedCeiling, DefaultSeed)
	}
	t.Logf("the crate landed on the break and the quickest anything reached afterwards was %.3f m/s", worst)
}

// The screen-to-world conversion, over values, with no engine at all. It is the
// likeliest bug in this demo — a click that lands somewhere other than under the
// cursor — and it is two separate mistakes to make: inverting the projection,
// and undoing the fit the window was laid out with.
func TestTheScreenAndTheTableAgreeOnWhereAPointIs(t *testing.T) {
	// The projection and its inverse are a pair, and a pair is what makes a
	// click land under the cursor.
	for _, world := range []m.Vec2d{
		{}, {X: tableHalfWidth, Y: tableHalfHeight}, {X: -tableHalfWidth, Y: -tableHalfHeight}, {X: 3.25, Y: -1.75},
	} {
		if back := unproject(project(world)); back.Sub(world).Length() > 1e-9 {
			t.Errorf("%v projects to %v and comes back as %v", world, project(world), back)
		}
	}
	// The corners of the world are the corners of the screen, which is what
	// says the whole table is on the picture and none of it is off it.
	if got, want := project(m.Vec2d{X: -tableHalfWidth - cushionDepth, Y: tableHalfHeight + cushionDepth}),
		(m.Vec2{}); got != want {
		t.Errorf("the top-left corner of the cushions is at screen %v, want %v", got, want)
	}
	if got, want := project(m.Vec2d{X: tableHalfWidth + cushionDepth, Y: -tableHalfHeight - cushionDepth}),
		(m.Vec2{X: screenWidth, Y: screenHeight}); got != want {
		t.Errorf("the bottom-right corner of the cushions is at screen %v, want %v", got, want)
	}

	for _, item := range []struct {
		name    string
		view    gfx.Viewport
		pointer input.Pos
		want    m.Vec2d
	}{
		// A window the demo's screen fits exactly: the pointer is already in
		// the screen's own pixels.
		{"unscaled", viewportFor(960, 540), input.Pos{X: 480, Y: 270}, m.Vec2d{}},
		{"unscaled corner", viewportFor(960, 540), input.Pos{X: 0, Y: 0},
			m.Vec2d{X: -tableHalfWidth - cushionDepth, Y: tableHalfHeight + cushionDepth}},
		// A window of the same shape but bigger: the fit is a pure scale, and a
		// conversion that forgot it would put every click at a third of the way
		// to where it was made.
		{"scaled", viewportFor(1920, 1080), input.Pos{X: 960, Y: 540}, m.Vec2d{}},
		{"scaled off-centre", viewportFor(1920, 1080), input.Pos{X: 1440, Y: 270}, m.Vec2d{X: 4, Y: 2.25}},
		// A window taller than the demo's screen: AspectInscribe leaves a
		// letterbox bar top and bottom, and the middle of the window is still
		// the middle of the table.
		{"letterboxed", viewportFor(1600, 1200), input.Pos{X: 800, Y: 600}, m.Vec2d{}},
		// And a click in the bar is off the table rather than on its edge,
		// which is why crateSpot exists.
		{"in the bar", viewportFor(1600, 1200), input.Pos{X: 800, Y: 10}, m.Vec2d{Y: 5.9}},
	} {
		t.Run(item.name, func(t *testing.T) {
			got, ok := pointerWorld(item.pointer, item.view)
			if !ok {
				t.Fatalf("the pointer at %v in a %vx%v window converted to nothing",
					item.pointer, item.view.WindowWidth, item.view.WindowHeight)
			}
			if got.Sub(item.want).Length() > 1e-6 {
				t.Errorf("the pointer at %v is world %v, want %v", item.pointer, got, item.want)
			}
		})
	}

	// A Viewport with no size yet — every tick before the first window event —
	// converts to nothing rather than to the middle of the table.
	if _, ok := pointerWorld(input.Pos{X: 10, Y: 10}, gfx.Viewport{}); ok {
		t.Error("a pointer converted against a sizeless Viewport, want no answer at all")
	}

	// Whatever the click said, a crate is put on the cloth: a click in a
	// letterbox bar, on a cushion, or off the window entirely is clamped inside
	// the rails by the crate's own half-diagonal, so no Body is ever born in a
	// wall.
	reach := crateSide * math.Sqrt2 / 2
	for _, asked := range []m.Vec2d{
		{}, {X: 100, Y: 100}, {X: -100, Y: -100}, {X: 0, Y: 5.9}, {X: tableHalfWidth, Y: 0},
	} {
		spot := crateSpot(asked)
		if math.Abs(spot.X) > tableHalfWidth-reach+1e-12 || math.Abs(spot.Y) > tableHalfHeight-reach+1e-12 {
			t.Errorf("a crate asked for at %v goes to %v, which is not clear of the cushions by %v m",
				asked, spot, reach)
		}
	}
}

// The re-break, which is one more clause of the same kind as the three above:
//
//	space breaks the table again, once per press, from the same deal.
//
// "The break comes to rest" is what makes it worth having — a table that has
// stopped is a table with nothing left to watch — so these four tests all start
// from a settled table, which is the state a person actually presses space in.

func TestSpaceBreaksTheTableAgain(t *testing.T) {
	r := start(t)
	settled := r.steps(settleTicks)
	// The premise, asserted rather than assumed: there is nothing moving to
	// confuse the count that follows for a break.
	if settled.Moving != 0 {
		t.Fatalf("after %d ticks %d pieces still move, so this test cannot tell a re-break from what was left over",
			settled.Step, settled.Moving)
	}
	if settled.Breaks != 1 || settled.Dealt != ballCount {
		t.Fatalf("the opening rack counts as %d break(s) of %d pieces, want 1 of %d",
			settled.Breaks, settled.Dealt, ballCount)
	}

	broken := r.press()

	if broken.Breaks != 2 {
		t.Errorf("one press of space made %d breaks, want the rack's one and this one", broken.Breaks)
	}
	// Every ball, and not merely some of them: the press re-deals the whole
	// rack, so a table that was wholly still is wholly moving on the tick the key
	// went down. A re-break that reached ninety of them would pass a "the table
	// is moving again" test and fail this one.
	if broken.Moving != ballCount {
		t.Errorf("one tick after space, %d of %d pieces are above %v m/s, want every ball moving; seed %#016x",
			broken.Moving, ballCount, restSpeed, broken.Seed)
	}
	// And it is a break rather than a nudge: the slowest thing the deal can
	// produce is breakSlowest, so the quickest is at least that.
	if broken.Fastest < breakSlowest {
		t.Errorf("the quickest ball after the re-break does %v m/s, want at least the deal's slowest %v m/s",
			broken.Fastest, breakSlowest)
	}
	t.Logf("a settled table at %.5f m/s was broken again to %.3f m/s with all %d balls moving",
		settled.Fastest, broken.Fastest, broken.Moving)

	// And it comes back to rest, because the cloth is the same cloth: the clause
	// the demo exists to make is about the table and not about its first break.
	rested := r.steps(settleTicks)
	if rested.Moving != 0 || rested.Fastest > restSpeed {
		t.Errorf("%d ticks after the re-break %d pieces still move and the quickest does %v m/s, want everything under %v m/s; seed %#016x",
			settleTicks, rested.Moving, rested.Fastest, restSpeed, rested.Seed)
	}
}

// The obvious bug in a key that does something: firing every tick it is held
// rather than once when it goes down. Sixty breaks a second is not a break at
// all — it is a table of balls with a fresh random velocity every frame, which
// looks like noise and never comes to rest.
func TestAHeldSpaceBreaksOnceAndNotSixtyTimesASecond(t *testing.T) {
	r := start(t)
	r.steps(settleTicks)

	// A full second with the key down, re-asserted every tick.
	const heldTicks = 60
	held := r.hold(heldTicks)
	if held.Breaks != 2 {
		t.Errorf("space held for %d ticks made %d breaks, want the rack's one and one more",
			heldTicks, held.Breaks)
	}
	// It is an edge and not a level in the direction that matters too: the deal
	// happened on the first of those ticks, so the table has had the rest of the
	// second to damp. A re-break on the last tick would leave it at full speed.
	if held.Fastest >= breakFastest {
		t.Errorf("after %d ticks of a held key the table is still doing %v m/s, which is a table being re-dealt rather than one running",
			heldTicks, held.Fastest)
	}

	// Releasing and pressing again is a second edge, so the key is not a
	// one-shot either: what makes it fire is the transition, both times.
	r.engine.Input(input.KeyChange(rebreakKey, 0, false))
	again := r.press()
	if again.Breaks != 3 {
		t.Errorf("a release and a second press made %d breaks, want 3", again.Breaks)
	}
	t.Logf("%d ticks of held space made one break and left the table at %.3f m/s; the next press made another",
		heldTicks, held.Fastest)
}

// The speed ceiling is the number the cushion depth is a margin over, so a new
// way of putting balls in motion is a new way of breaking it. A re-break is
// arithmetically the opening break, dealt from the same range — but it is dealt
// onto a table where the balls have settled wherever they stopped rather than
// onto a lattice dealt clear of itself, which is the harder case for a solver
// that does not conserve peak speed through a chain of touching bodies.
func TestTheSpeedCeilingHoldsAcrossAReBreak(t *testing.T) {
	r := start(t)
	r.steps(settleTicks)
	r.press()

	worst, at := 0.0, 0
	for n := 1; n <= breakTicks; n++ {
		c := r.steps(1)
		if c.Escaped != 0 {
			t.Fatalf("%d ticks after the re-break, %d bodies are outside the table, the worst by %v m; seed %#016x",
				n, c.Escaped, c.Outside, c.Seed)
		}
		if c.Fastest > worst {
			worst, at = c.Fastest, n
		}
	}
	if worst > speedCeiling {
		t.Errorf("after the re-break something reached %v m/s at step %d, want no more than the ceiling's %v m/s; seed %#016x",
			worst, at, speedCeiling, DefaultSeed)
	}
	t.Logf("over %d ticks after a re-break the quickest anything reached was %.3f m/s, against a deal of %v and a ceiling of %v",
		breakTicks, worst, breakFastest, speedCeiling)
}

// Space breaks the balls and not the crates, and that is a decision rather than
// an accident. A crate is four times a ball's mass, so a crate dealt the break's
// fastest into a ball at rest hands it 2·4/(4+1)·5 = 8 m/s, which is over the
// 7.5 m/s ceiling the cushions are cut for: kicking the crates would mean
// re-deriving cushionDepth, and it would make a crate a cue ball rather than the
// intruder a click put on the cloth.
//
// It is asserted as the count the break dealt to rather than as what moves
// afterwards, because what moves afterwards is not the same claim: a crate the
// re-broken balls immediately shove is the table working exactly as it should.
func TestSpaceBreaksTheBallsAndNotTheCrates(t *testing.T) {
	r := start(t)
	r.steps(30)
	dropped := r.click(m.Vec2{X: screenWidth / 2, Y: screenHeight / 2})
	if dropped.Crates != 1 {
		t.Fatalf("the click made %d crates, want 1", dropped.Crates)
	}
	r.steps(settleTicks)

	broken := r.press()
	if broken.Crates != 1 {
		t.Fatalf("the table has %d crates on it, want the one that was clicked", broken.Crates)
	}
	if broken.Dealt != ballCount {
		t.Errorf("the re-break dealt to %d pieces on a table of %d balls and %d crate, want the %d balls alone",
			broken.Dealt, ballCount, broken.Crates, ballCount)
	}
	// And the crate is still on the cloth rather than launched off it, which is
	// the consequence the ceiling argument above is really about.
	after := r.steps(breakTicks)
	if after.Escaped != 0 {
		t.Errorf("%d ticks after a re-break beside a crate, %d bodies are outside the table, the worst by %v m; seed %#016x",
			breakTicks, after.Escaped, after.Outside, after.Seed)
	}
	t.Logf("a re-break on a table carrying a crate dealt %d pieces and left the quickest at %.3f m/s",
		broken.Dealt, after.Fastest)
}

// Determinism survives the key, which is the promise the seed on the HUD is
// worth anything at all for: the re-break draws from the demo's own seeded
// stream rather than from a clock or a fresh source, so the same seed and the
// same presses on the same ticks are the same table, tick for tick.
//
// It is run as two engines side by side rather than against recorded numbers,
// because what is being asserted is that nothing outside the stream reaches the
// deal — and a golden file would also pass for a demo that had quietly become
// reproducible only on this machine.
func TestTheSameSeedAndTheSamePressesGiveTheSameTable(t *testing.T) {
	// A script with everything in it that can move the stream or the table: a
	// wait, a press, a second press before the table has settled, and a click in
	// between, which draws nothing from the stream and must not shift the deal.
	script := func(r *rig) []Census {
		var seen []Census
		seen = append(seen, r.steps(120))
		seen = append(seen, r.press())
		seen = append(seen, r.steps(40))
		seen = append(seen, r.click(m.Vec2{X: 300, Y: 200}))
		seen = append(seen, r.steps(40))
		seen = append(seen, r.press())
		seen = append(seen, r.steps(200))
		return seen
	}

	first, second := script(start(t)), script(start(t))
	if len(first) != len(second) {
		t.Fatalf("the two runs took %d and %d readings", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("reading %d differs between two runs of the same seed and the same presses:\n  %+v\n  %+v",
				i, first[i], second[i])
		}
	}
	last := first[len(first)-1]
	if last.Breaks != 3 {
		t.Errorf("the script made %d breaks, want the rack's one and two presses", last.Breaks)
	}
	t.Logf("two runs of the same seed and the same presses agreed on all %d readings, the last at step %d: %s",
		len(first), last.Step, last.lines()[0])
}

// viewportFor is the Viewport a window of that size produces with no desired
// viewport set, which is what gfx resolves and what the pointer arrives against.
func viewportFor(width, height float32) gfx.Viewport {
	return gfx.Viewport{Width: width, Height: height, WindowWidth: width, WindowHeight: height}
}

// The HUD prints the census it was handed, line for line, so what a test reads
// and what an eye reads are the same numbers. It records into a bare OpQueue,
// which needs no plugins at all.
func TestTheHUDPrintsTheCensusItWasGiven(t *testing.T) {
	census := start(t).steps(breakTicks)
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
	for i, claim := range []string{claimInside, claimApart, claimRest} {
		if !strings.Contains(drawn[i+1], claim) {
			t.Errorf("HUD line %d reads %q, want it to carry %q", i+1, drawn[i+1], claim)
		}
	}
	// And the seed is on it, because a run that shows something wrong is
	// reported by its number.
	if !strings.Contains(drawn[0], "seed") {
		t.Errorf("the HUD's first line reads %q, want the seed on it", drawn[0])
	}
	// The break count is beside it, because it is the one thing about a running
	// table a picture cannot otherwise say: a re-broken table looks exactly like
	// a freshly racked one.
	if !strings.Contains(drawn[0], "breaks") {
		t.Errorf("the HUD's first line reads %q, want the break count on it", drawn[0])
	}
	// And the footer names the key that does it, off rebreakKey itself, so the
	// HUD cannot advertise a key the demo does not read.
	if footer := drawn[len(drawn)-1]; !strings.Contains(footer, rebreakKey.String()) {
		t.Errorf("the HUD's footer reads %q, want it to name the %v key that breaks the table again",
			footer, rebreakKey)
	}
}
