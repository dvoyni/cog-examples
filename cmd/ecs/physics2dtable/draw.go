package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// The logical screen the demo lays itself out in. The window is fitted to it, so
// the picture keeps its proportions at any window size.
const (
	screenWidth  = 960
	screenHeight = 540
)

// The world is drawn as an outline and nothing else: a Shape's own geometry,
// straight from the Component, with no sprite and no mesh in between. That is
// deliberate — what there is to judge here is where the Bodies are, and a
// picture that hid the Shape behind artwork would hide the one thing being
// shown.
const (
	// pixelsPerMetre is chosen so the cushions' outer faces land exactly on the
	// screen's edges: 8 m of world either side of the origin at 60 px/m is the
	// 480 px half-width, and 4.5 m above and below it is the 270 px half-height.
	// The rails are therefore on screen and 30 px deep, rather than being the
	// edge of the picture, which is the difference between seeing a ball bounce
	// off something and seeing it turn around at the frame.
	pixelsPerMetre = 60.0

	// circleSegments is how many straight pieces a circle is drawn as, and the
	// spoke is what makes a turning ball look like it is turning rather than
	// standing still. A ball is eleven pixels across here, so a finer circle
	// would cost lines and buy nothing.
	circleSegments = 16

	outlineThickness = 2
)

// The two layers: the world, and the HUD above it.
const (
	layerWorld canvas.Layer = 0
	layerHUD   canvas.Layer = 1
)

// The palette. The cloth is snooker green and the rails are the dark wood
// around it; the balls run through a spread of hues so a single one can be
// followed through the break with the eye, which is how tunnelling is actually
// spotted.
var (
	clothTint   = m.NewColorSrgb(0.04, 0.22, 0.13, 1)
	cushionTint = m.NewColorSrgb(0.55, 0.36, 0.18, 1)
	crateTint   = m.NewColorSrgb(0.93, 0.86, 0.62, 1)
	hudTint     = m.NewColorSrgb(0.88, 0.94, 0.90, 1)
	hudDimTint  = m.NewColorSrgb(0.48, 0.60, 0.52, 1)
)

// ballTint is the i-th ball's colour: a walk round the hue circle at a stride
// that never repeats over a hundred, so two neighbours in the rack are never
// the same colour.
func ballTint(i int) m.Color {
	turn := 2 * math.Pi * float64(i) * 7 / ballCount
	return m.NewColorSrgb(
		float32(0.55+0.40*math.Sin(turn)),
		float32(0.55+0.40*math.Sin(turn+2*math.Pi/3)),
		float32(0.55+0.40*math.Sin(turn+4*math.Pi/3)),
		1)
}

const (
	hudSize = 11
	hudLeft = 14
	hudTop  = 14
	hudLine = hudSize * 7 / 5
	hudFoot = hudTop + hudSize*2/3
)

// project turns a point in the world into a point on the screen, and unproject
// turns it back. They are the one place the demo's metres meet anybody's pixels,
// and they are a pair on purpose: the drawing goes one way and a click comes
// back the other, and a demo that wrote the inverse out by hand in the click
// path would be one edit away from putting crates somewhere other than under the
// cursor. TestTheScreenAndTheTableAgreeOnWhereAPointIs holds them together.
func project(p m.Vec2d) m.Vec2 {
	return m.Vec2{
		X: float32(screenWidth/2 + p.X*pixelsPerMetre),
		Y: float32(screenHeight/2 - p.Y*pixelsPerMetre),
	}
}

func unproject(p m.Vec2) m.Vec2d {
	return m.Vec2d{
		X: (float64(p.X) - screenWidth/2) / pixelsPerMetre,
		Y: (screenHeight/2 - float64(p.Y)) / pixelsPerMetre,
	}
}

// shapeQuery drives the drawing walk: every Entity carrying a Shape, a Position
// and a colour of the demo's own, Static and Dynamic alike. All three are read,
// which is the whole of this System's lock set — a drawing System that wrote
// anything a solver reads would have to be ordered against Solve for a reason
// other than wanting the settled tick.
type shapeQuery struct {
	Shape ecsphysics2d.Shape
	Place ecsphysics2d.Position
	Look  Piece
}

// draw records the frame: the cloth, the world's outlines and the HUD over
// them. It is ordered After the census and Before canvas's flush, so what it
// draws is the tick the census just read.
func (p *Demo) draw(
	state *ecs.Read[*Table],
	shapes *ecs.Query[shapeQuery],
	polygons *ecs.Get[ecsphysics2d.Polygon],
	queue *ecs.Write[*canvas.OpQueue],
) {
	q := queue.Get()
	q.Clear(layerWorld, clothTint)
	q.SetLayerTransform(layerWorld, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)
	q.SetLayerTransform(layerHUD, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	for e, it := range shapes.All() {
		// Every Shape in this scene carries its vertices inline, but the probe
		// is written out anyway: a ShapePoly is too large to carry them and
		// keeps them in a Polygon Component on the same Entity, and a drawing
		// routine that assumed otherwise would draw such a Shape as nothing.
		polygon, _ := polygons.Of(e)
		p.strokeShape(q, it.Shape, polygon, it.Place, it.Look.Tint)
	}

	drawHUD(q, state.Get().Census)
}

// strokeShape outlines one placed Shape. Which of the five kinds it is says how
// many of its vertices mean anything, so the switch is over Kind and not over a
// count.
func (p *Demo) strokeShape(
	q *canvas.OpQueue,
	shape ecsphysics2d.Shape,
	polygon ecsphysics2d.Polygon,
	place ecsphysics2d.Position,
	tint m.Color,
) {
	rotation := m.ForAngle(place.Angle)
	world := func(local m.Vec2d) m.Vec2d { return place.Current.Add(local.Rotate(rotation)) }
	draw := canvas.ShapeDraw{Color: tint, Thickness: outlineThickness}

	switch shape.Kind {
	case ecsphysics2d.ShapeCircle:
		centre := world(shape.Offset())
		previous := centre.Add(rotation.MulS(shape.Radius))
		// The spoke: a circle with no mark on it looks the same whichever way it
		// is facing, and a ball that is still rolling when the table is
		// supposed to have stopped is a thing worth seeing.
		q.Line(layerWorld, project(centre), project(previous), draw)
		for i := 1; i <= circleSegments; i++ {
			angle := place.Angle + 2*math.Pi*float64(i)/circleSegments
			next := centre.Add(m.ForAngle(angle).MulS(shape.Radius))
			q.Line(layerWorld, project(previous), project(next), draw)
			previous = next
		}
	case ecsphysics2d.ShapeSegment:
		a, b := world(shape.A()), world(shape.B())
		q.Line(layerWorld, project(a), project(b), draw)
		if shape.Radius > 0 {
			// A segment fattened by a Radius is a capsule, and the two
			// parallels are where its surface actually is.
			out := b.Sub(a).Normalize().Perp().MulS(shape.Radius)
			q.Line(layerWorld, project(a.Add(out)), project(b.Add(out)), draw)
			q.Line(layerWorld, project(a.Sub(out)), project(b.Sub(out)), draw)
		}
	default:
		p.verts = ecsphysics2d.PolygonVerts(p.verts[:0], shape, polygon)
		for i, local := range p.verts {
			next := p.verts[(i+1)%len(p.verts)]
			q.Line(layerWorld, project(world(local)), project(world(next)), draw)
		}
	}
}

// drawHUD prints the census beside the sentence each of its lines decides.
func drawHUD(q *canvas.OpQueue, c Census) {
	lines := c.lines()
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudTint)
	}
	text(q, screenHeight-hudFoot,
		"click the cloth to drop a crate; no gravity System is composed, and none is missing", hudDimTint)
}

func text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{Position: m.Vec2{X: hudLeft, Y: y}, Size: hudSize, Color: color})
}
