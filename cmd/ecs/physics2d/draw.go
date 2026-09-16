package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// The logical screen the demo lays itself out in. The window is fitted to it,
// so the picture keeps its proportions at any window size.
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
	// pixelsPerMetre puts 15 m of world across the screen, which is the floor
	// and a little air at each end.
	pixelsPerMetre = 64.0
	// groundRow is the screen row world y = 0 draws on, which leaves 7.5 m of
	// headroom above the floor and just under a metre below it.
	groundRow = 480.0

	// circleSegments is how many straight pieces a circle is drawn as, and the
	// spoke is what makes a turning circle look like it is turning rather than
	// standing still.
	circleSegments = 24

	outlineThickness = 2
	jointThickness   = 1
)

// The two layers: the world, and the HUD above it.
const (
	layerWorld canvas.Layer = 0
	layerHUD   canvas.Layer = 1
)

// The palette. The stack runs through a ramp of one hue so the boxes are
// countable, and the two crates are coloured by what they do: the one that
// grips is green and the one that slips is amber.
var (
	backgroundTint = m.NewColorSrgb(0.07, 0.08, 0.11, 1)
	floorTint      = m.NewColorSrgb(0.38, 0.42, 0.50, 1)
	rampTint       = m.NewColorSrgb(0.52, 0.56, 0.64, 1)
	gripTint       = m.NewColorSrgb(0.38, 0.76, 0.48, 1)
	slipTint       = m.NewColorSrgb(0.93, 0.70, 0.30, 1)
	torsoTint      = m.NewColorSrgb(0.46, 0.60, 0.88, 1)
	limbTint       = m.NewColorSrgb(0.36, 0.48, 0.74, 1)
	skinTint       = m.NewColorSrgb(0.86, 0.74, 0.60, 1)
	jointTint      = m.NewColorSrgb(0.95, 0.42, 0.44, 1)
	hudTint        = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimTint     = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
)

// stackTint is the i-th box's colour, lightening up the column.
func stackTint(i int) m.Color {
	t := float32(i) / float32(stackHeight-1)
	return m.NewColorSrgb(0.30+0.45*t, 0.55+0.28*t, 0.72+0.20*t, 1)
}

const (
	hudSize = 11
	hudLeft = 14
	hudTop  = 14
	hudLine = hudSize * 7 / 5
	hudFoot = hudTop + hudSize*2/3
)

// project turns a point in the world into a point on the screen. It is the one
// place the demo's metres meet anybody's pixels.
func project(p m.Vec2d) m.Vec2 {
	return m.Vec2{
		X: float32(screenWidth/2 + p.X*pixelsPerMetre),
		Y: float32(groundRow - p.Y*pixelsPerMetre),
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
	Tint  Look
}

// jointQuery drives the hinge overlay: every Joint, read.
type jointQuery struct {
	Joint ecsphysics2d.Joint
}

// draw records the frame: the world's outlines, the hinges over them, and the
// HUD over that. It is ordered After the census and Before canvas's flush, so
// what it draws is the tick the census just read.
func (p *Demo) draw(
	state *ecs.Read[*Scene],
	shapes *ecs.Query[shapeQuery],
	joints *ecs.Query[jointQuery],
	places *ecs.Get[ecsphysics2d.Position],
	polygons *ecs.Get[ecsphysics2d.Polygon],
	queue *ecs.Write[*canvas.OpQueue],
) {
	q := queue.Get()
	q.Clear(layerWorld, backgroundTint)
	q.SetLayerTransform(layerWorld, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)
	q.SetLayerTransform(layerHUD, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	for e, it := range shapes.All() {
		// Every Shape in this scene carries its vertices inline, but the probe
		// is written out anyway: a ShapePoly is too large to carry them and
		// keeps them in a Polygon Component on the same Entity, and a drawing
		// routine that assumed otherwise would draw such a Shape as nothing.
		polygon, _ := polygons.Of(e)
		p.strokeShape(q, it.Shape, polygon, it.Place, it.Tint.Tint)
	}

	for _, it := range joints.All() {
		strokeJoint(q, it.Joint, places)
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
		// The spoke: a circle with no mark on it looks the same whichever way
		// it is facing, and a rolling circle is half of what a ramp shows.
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

// strokeJoint draws the line between a Joint's two world anchors. On a figure
// that is holding together it is a dot; when it is a line, the Joint is being
// stretched, which is the one thing about a jointed figure worth seeing at a
// glance.
func strokeJoint(q *canvas.OpQueue, joint ecsphysics2d.Joint, places *ecs.Get[ecsphysics2d.Position]) {
	if joint.Kind != ecsphysics2d.JointPivot {
		// Only the pivots have anchors in the world; the four rotary limits
		// hold an Angle and have nothing to draw.
		return
	}
	anchorA, anchorB := joint.Anchors()
	worldA, okA := worldPoint(places, joint.A, anchorA)
	worldB, okB := worldPoint(places, joint.B, anchorB)
	if !okA || !okB {
		return
	}
	draw := canvas.ShapeDraw{Color: jointTint, Thickness: jointThickness}
	// A cross at the hinge, so a Joint that is holding is still visible.
	const tick = 0.05
	q.Line(layerWorld, project(worldA.Add(m.Vec2d{X: -tick})), project(worldA.Add(m.Vec2d{X: tick})), draw)
	q.Line(layerWorld, project(worldA.Add(m.Vec2d{Y: -tick})), project(worldA.Add(m.Vec2d{Y: tick})), draw)
	q.Line(layerWorld, project(worldA), project(worldB), draw)
}

// drawHUD prints the census beside the sentence each of its lines decides.
func drawHUD(q *canvas.OpQueue, c Census) {
	lines := c.lines()
	for i, line := range lines {
		text(q, hudTop+float32(i)*hudLine, line, hudTint)
	}
	text(q, screenHeight-hudFoot,
		"no input: step N is the same frame on every machine", hudDimTint)
}

func text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{Position: m.Vec2{X: hudLeft, Y: y}, Size: hudSize, Color: color})
}
