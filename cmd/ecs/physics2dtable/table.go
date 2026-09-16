package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The world, in metres, with +y up and the origin in the middle of the table.
// Everything here is a length, a mass or a rate; nothing is a pixel.
//
// The scale is chosen against the solver's defaults rather than against a real
// snooker table. A snooker ball is 52 mm across, so its radius is 26 mm, which
// is five Slops: a table at life size would rest its balls a fifth of a radius
// inside each other and every tolerance here would be a fraction of a ball.
// This one is seven times that — 0.18 m of radius, thirty-six Slops — and its
// cloth is seven and a half of the broadphase's 2 m cells by four.
const (
	// fixedStep is the step app publishes every tick, and the rate every number
	// this demo quotes is quoted at. It is the demo's own copy of what app is
	// configured with, used to spell a time on the HUD; the Systems themselves
	// are fed the step out of the event.
	fixedStep = 1.0 / 60

	// The playing surface, inside the cushions: 15 m by 8 m.
	tableHalfWidth  = 7.5
	tableHalfHeight = 4.0

	// slop is the solver's own, left at its default by main.go. It is the demo's
	// copy, used to spell an overlap on the HUD as a multiple of the tolerance
	// that produced it; the solver is never told it.
	slop = 0.005

	// cushionDepth is how thick each cushion is, and it is the whole of what
	// keeps a ball from tunnelling. The step is discrete for everything that is
	// not a Sensor — only a moving circle Sensor is Probed along its path — so a
	// Body that crosses a wall inside one tick meets nothing and comes out the
	// far side. The margin is this over the furthest a ball travels in a tick,
	// which is speedCeiling and not breakFastest: 0.5 / 0.125 = four ticks.
	// TestNothingLeavesTheTable asserts the margin as arithmetic and then again
	// against a running engine.
	cushionDepth = 0.5
)

// speedCeiling is the fastest anything on this table is allowed to be, in m/s,
// and the number every margin here is quoted against.
//
// It is half again the fastest the break deals, and the headroom is a measured
// fact rather than a superstition. A ball arriving into a queue of touching
// balls leaves the solver's ten iterations faster than it went in, because an
// impulse solver settles a chain approximately and the approximation does not
// conserve the peak: over sixty-four seeds of six hundred ticks apiece the
// quickest anything reached was 7.0 m/s against a deal of 5. That is cp's
// behaviour and not a defect for a demo to fix; what a demo owes is a margin
// that covers it, a cushion deep enough for the margin, and a test that says
// the number out loud rather than hoping.
const speedCeiling = 1.5 * breakFastest

// The break: a hundred balls on a jittered lattice, each dealt a direction, a
// speed and a spin.
const (
	ballCount = 100
	// The lattice: thirteen by eight is a hundred and four places and the deal
	// takes the first hundred, so the last row is the short one. Both spacings
	// are more than twice a ball's diameter, and the jitter is a fifth of the
	// narrower one, so no two balls overlap where they are placed however the
	// stream falls. TestTheBreakIsDealtClearOfItself asserts that over values.
	latticeColumns = 13
	latticeRows    = 8
	latticeJitter  = 0.2

	ballRadius = 0.18
	ballMass   = 1.0

	// A pair's values are the plain products of the two Shapes', so ball on ball
	// bounces at 0.9604 and slides at 0.0025, and ball on cushion bounces at
	// 0.9604 and slides at 0.005.
	ballRestitution = 0.98
	ballFriction    = 0.05

	// Damping is what stops the table, because a top-down table has no floor
	// under it to rub on and there is no gravity pressing it into one. It is a
	// rate in 1/s and integration is exponential in it — v ← v·exp(−d·h) — so
	// 0.6/s takes the fastest ball this break deals from 5 m/s to 0.00003 m/s
	// in twenty seconds, and the spin goes two and a half times faster than
	// that. TestTheClothIsExponentialAndNotAGuess is the closed form, and it is
	// where the settling ticks the other tests wait come from.
	ballDamping        = 0.6
	ballAngularDamping = 1.5

	// The speeds the break is dealt from. breakFastest feeds speedCeiling, which
	// the cushion's depth is a margin over, so raising it means raising that too.
	breakSlowest = 1.5
	breakFastest = 5.0
	breakSpin    = 3.0
)

// The cushions: high Restitution, so a ball bounces off the rail rather than
// dying against it, and almost no Friction so it does not scrub its way along.
const (
	cushionRestitution = 0.98
	cushionFriction    = 0.1
)

// The crate a click drops: bigger and heavier than a ball, and dull rather than
// bouncy, so that what a click adds to the table reads as an intruder.
const (
	crateSide        = 0.7
	crateMass        = 4.0
	crateRestitution = 0.2
	crateFriction    = 0.5
)

// DefaultSeed is what the break is dealt from when -seed says nothing. It is a
// constant rather than a clock: step N of a default run is the same frame on
// every machine, which is what the sibling demos promise and what a reference
// image would need.
const DefaultSeed uint64 = 0x2545F4914F6CDD1D

// restSpeed is how slow a piece has to be to count as stopped, in m/s. It is
// four Slops a second: a ball moving slower than that moves less than a
// thousandth of its own radius a tick, which is under the resolution of
// anything on screen.
const restSpeed = 0.02

// The Component sets: one per kind of thing the scene creates.
type (
	// cushion is one of the four rails. The Shape, the Position and the Tag
	// arrive in one Spawn, so the Shape hook and the Position reach Index
	// together; a Static whose Shape arrives without a Position is skipped
	// silently and never enters the index.
	cushion struct {
		Place  ecsphysics2d.Position
		Marker ecsphysics2d.Static
		Shape  ecsphysics2d.Shape
		Look   Piece
	}
	// piece is every Dynamic thing on the table: the hundred balls and every
	// crate a click adds. The Force is not optional even though nothing in this
	// program ever writes one — a Dynamic body without a Force falls out of the
	// velocity integrator's Query and silently never moves, so the Component is
	// here and stays zero for the whole run. That is the shape of "the port
	// ships no gravity": the field gravity would be written into exists, and
	// this demo simply writes nothing into it.
	piece struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Force    ecsphysics2d.Force
		Dynamic  ecsphysics2d.Dynamic
		Shape    ecsphysics2d.Shape
		Look     Piece
	}
)

// Piece is the one Component the demo declares: what a thing is and what colour
// it draws in. Physics has no opinion about either, which is the point — a
// Component of the app's own rides beside the engine's on the same Entity.
type Piece struct {
	Tint m.Color
	// Crate marks the pieces a click added, which is the only thing the census
	// cannot work out from physics' own Components: a crate and a ball are both
	// a Dynamic body with a Shape, and the Shape's Kind would be a proxy for
	// the question rather than an answer to it.
	Crate bool
}

// Table is the census, the random stream and the handles they are read through.
// It is a Resource rather than a Component because it is the demo's view of the
// whole world and there is one of it.
type Table struct {
	// Census is what the last tick left, refilled by survey and drawn by the
	// HUD. A test asks for it through CensusCmd.
	Census Census

	// random is the break's stream, seeded once at registration. It is here
	// rather than in the plugin because the deal is a System's write and a
	// System reaches its state through a Resource.
	random uint64

	// crates is how many clicks have landed, last is the newest crate and asked
	// is the world point the pointer was over when it was asked for — before
	// crateSpot clamped it. Together they are what makes "a crate lands where
	// it was asked for" a thing the census can answer.
	crates int
	last   ecs.Entity
	asked  m.Vec2d
}

// next is xorshift64*, the same stream the fountain demo deals from. A zero
// seed would stick at zero, so it is folded to the default; -seed 0 is a
// mistake rather than a request for a dead stream.
func (t *Table) next() uint64 {
	if t.random == 0 {
		t.random = DefaultSeed
	}
	t.random ^= t.random >> 12
	t.random ^= t.random << 25
	t.random ^= t.random >> 27
	return t.random * 2685821657736338717
}

// unit is the next value in [0, 1).
func (t *Table) unit() float64 { return float64(t.next()>>11) / float64(uint64(1)<<53) }

// between is the next value in [low, high).
func (t *Table) between(low, high float64) float64 { return low + t.unit()*(high-low) }

// rack builds the table once, on the init event: the four cushions and the
// break. Nothing else is spawned until somebody clicks.
func rack(state *ecs.Write[*Table], cushions *ecs.Spawn[cushion], pieces *ecs.Spawn[piece]) {
	rackCushions(cushions)
	rackBreak(state.Get(), pieces)
}

// rackCushions spawns the four rails.
//
// They are boxes rather than segments for the reason cushionDepth gives, and
// the two horizontal ones run the full width while the two vertical ones run
// only the interior height, so the four overlap at the corners and the box is
// closed. Two Statics overlapping costs nothing: they are in the same index and
// neither is ever the moving party, so no Contact is ever made between them.
func rackCushions(cushions *ecs.Spawn[cushion]) {
	const (
		outerX = tableHalfWidth + cushionDepth
		outerY = tableHalfHeight + cushionDepth
	)
	for _, box := range [4]ecsphysics2d.BB{
		ecsphysics2d.NewBB(-outerX, tableHalfHeight, outerX, outerY),   // top
		ecsphysics2d.NewBB(-outerX, -outerY, outerX, -tableHalfHeight), // bottom
		ecsphysics2d.NewBB(-outerX, -tableHalfHeight, -tableHalfWidth, tableHalfHeight),
		ecsphysics2d.NewBB(tableHalfWidth, -tableHalfHeight, outerX, tableHalfHeight),
	} {
		shape := ecsphysics2d.NewBoxShapeFor(box, 0)
		shape.Restitution = cushionRestitution
		shape.Friction = cushionFriction
		cushions.New(cushion{Shape: shape, Look: Piece{Tint: cushionTint}})
	}
}

// rackBreak deals the hundred balls: a jittered lattice of places, and for each
// a direction, a speed and a spin off the same stream.
//
// The velocities are written as a Velocity and not as a Force, and the
// difference is a tick: Solve turns a Force into velocity at the end of the tick
// it was written on and the next Integrate spends it, where a Velocity written
// directly is what the very next Integrate moves. A break that arrived a tick
// late would still look like a break, but it would be a worse demonstration of
// which of the two is immediate.
func rackBreak(t *Table, pieces *ecs.Spawn[piece]) {
	moment := ecsphysics2d.MomentForCircle(ballMass, 0, ballRadius, m.Vec2d{})
	dealt := 0
	for row := range latticeRows {
		for column := range latticeColumns {
			if dealt == ballCount {
				return
			}
			at := latticePlace(row, column)
			at.X += t.between(-latticeJitter, latticeJitter)
			at.Y += t.between(-latticeJitter, latticeJitter)

			heading := m.ForAngle(t.between(-math.Pi, math.Pi))
			shape := ecsphysics2d.NewCircleShape(ballRadius, m.Vec2d{})
			shape.Restitution = ballRestitution
			shape.Friction = ballFriction
			pieces.New(piece{
				// Previous is set with Current because a Body that has just
				// been placed has not moved: leaving it at the origin would
				// hand the frame's interpolation a metres-long first step.
				Place: ecsphysics2d.Position{Current: at, Previous: at},
				Velocity: ecsphysics2d.Velocity{
					Linear:  heading.MulS(t.between(breakSlowest, breakFastest)),
					Angular: t.between(-breakSpin, breakSpin),
				},
				Dynamic: dynamic(ballMass, moment, ballDamping, ballAngularDamping),
				Shape:   shape,
				Look:    Piece{Tint: ballTint(dealt)},
			})
			dealt++
		}
	}
}

// latticePlace is the centre of the lattice place in that row and column,
// before the jitter. The lattice fills the playing surface with a half-cell of
// margin all round, so the outermost ball is clear of the cushion by more than
// its own diameter even at the worst jitter.
func latticePlace(row, column int) m.Vec2d {
	spacingX := 2 * tableHalfWidth / latticeColumns
	spacingY := 2 * tableHalfHeight / latticeRows
	return m.Vec2d{
		X: -tableHalfWidth + (float64(column)+0.5)*spacingX,
		Y: -tableHalfHeight + (float64(row)+0.5)*spacingY,
	}
}

// dropQuery is nothing: the click System reads the pointer out of input's State
// and the fit out of gfx's Viewport, and both are Resources rather than
// Components. It spawns, so it holds write{*Entities} as well — see Register.

// drop turns a click into a crate at the cursor. It runs After input's advance,
// so JustPressed is this tick's edge, and Before Integrate, so the crate is
// indexed, detected and solved on the tick it was asked for.
func drop(
	state *ecs.Write[*Table],
	keys *ecs.Read[*input.State],
	view *ecs.Read[*gfx.Viewport],
	pieces *ecs.Spawn[piece],
) {
	keyboard, viewport := keys.Get(), view.Get()
	if keyboard == nil || viewport == nil || !keyboard.JustPressed(input.KeyMouseLeft) {
		return
	}
	asked, ok := pointerWorld(keyboard.Pointer(), *viewport)
	if !ok {
		return
	}
	t := state.Get()
	at := crateSpot(asked)
	shape := ecsphysics2d.NewBoxShape(crateSide, crateSide, 0)
	shape.Restitution = crateRestitution
	shape.Friction = crateFriction
	t.last = pieces.New(piece{
		Place:   ecsphysics2d.Position{Current: at, Previous: at},
		Dynamic: dynamic(crateMass, ecsphysics2d.MomentForBox(crateMass, crateSide, crateSide), ballDamping, ballAngularDamping),
		Shape:   shape,
		Look:    Piece{Tint: crateTint, Crate: true},
	})
	t.crates++
	t.asked = asked
}

// crateSpot is where a crate asked for at that point is actually put: inside the
// cushions by its own half-diagonal, so a click on a cushion — or, on a window
// whose fit leaves letterbox bars, a click outside the table altogether — drops
// a crate that is on the cloth rather than one born inside a wall. The
// half-diagonal and not the half-side, because a crate that is turning when the
// solver first sees it presents its corner.
func crateSpot(asked m.Vec2d) m.Vec2d {
	reach := crateSide * math.Sqrt2 / 2
	return m.Vec2d{
		X: min(max(asked.X, -tableHalfWidth+reach), tableHalfWidth-reach),
		Y: min(max(asked.Y, -tableHalfHeight+reach), tableHalfHeight-reach),
	}
}

// pointerWorld maps the pointer into the world, and it is the whole
// screen-to-world half of this demo. Three steps, and getting any of them wrong
// puts crates somewhere other than under the cursor:
//
//  1. the pointer arrives in window pixels, and canvas draws in the logical
//     viewport the window was fitted to, so it is scaled by the ratio of the
//     two the Viewport carries;
//  2. the demo's layers are recorded against a fixed screenWidth by
//     screenHeight screen laid inside that viewport with AspectInscribe, and
//     canvas.ScreenToWorld is the inverse of exactly that fit — including the
//     letterbox bars, which is why a click in a bar lands outside the table
//     rather than on its edge;
//  3. that screen point is in pixels with y down and the table is in metres
//     with y up, which is what unproject undoes.
//
// It returns false rather than a guess when the Viewport has no size yet, which
// is every tick before the first window event.
func pointerWorld(pointer input.Pos, view gfx.Viewport) (m.Vec2d, bool) {
	if view.Width <= 0 || view.Height <= 0 || view.WindowWidth <= 0 || view.WindowHeight <= 0 {
		return m.Vec2d{}, false
	}
	logical := m.Vec2{
		X: float32(pointer.X) * view.Width / view.WindowWidth,
		Y: float32(pointer.Y) * view.Height / view.WindowHeight,
	}
	return unproject(canvas.ScreenToWorld(
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe,
		m.Vec2{X: view.Width, Y: view.Height}, logical)), true
}

// dynamic is NewDynamic with the two Damping rates the caller wants.
//
// NewDynamic refuses a bad mass or Moment by handing back the zero Dynamic,
// which moves under nothing. Every argument here is a constant this file wrote,
// so the error is dropped rather than plumbed: there is no run in which it is
// not nil, and a demo that returned it would be inventing a failure mode.
func dynamic(mass, moment, damping, angularDamping float64) ecsphysics2d.Dynamic {
	value, _ := ecsphysics2d.NewDynamic(mass, moment, damping, angularDamping)
	return value
}
