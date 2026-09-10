// Command halo is a prototype of a reusable halo behind anything the canvas
// sprite family draws: a soft outward fade in one colour, so a mark reads
// against whatever art it overlaps.
//
// THROWAWAY. It exists to settle dvoyni/cog#191 - which mechanism produces the
// halo - and to be judged by eye against the hand-painted halos in feuds' unit
// art. It is not a design, and nothing here is API.
//
//	go run ./cmd/canvas/halo
//
// One layer carries the whole cluster and one SetLayerMaterial call haloes all
// of it: a multi-digit number through Text, an icon through Sprite, and a flag,
// a rule and a box through the shape helpers. Every one of those is a sprite
// instance in one instanced atlas draw, which is the observation the general
// effect rests on - "any sprite, including text characters" is ONE material.
//
// What only eyes can judge, and what the ticket asks:
//
//   - the halo reads as a soft outward fade, not a crisp outline
//   - the glyph, the sprite and the fill are indistinguishable at the seam,
//     although two entirely different mechanisms produced them
//   - it reads as the same visual language as the baked halo beside it
//   - it survives more than one scale, and a busy background as well as an empty
//     one
//
// Keys are listed on screen. H is the A/B: press it and the halo is gone.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/storage"
	"github.com/dvoyni/cog/wgpu"

	"github.com/dvoyni/cog-examples/internal/assets"
)

// feuds' own logical screen and score face, so every size on the board below is
// the size it would really be drawn at. A prototype at the wrong scale answers
// wrongly and convincingly.
const (
	screenWidth  = 1280
	screenHeight = 720

	scoreFontSize = 36  // lib.FontSizeFieldScore
	unitIconSize  = 96  // game.UnitIconSize
	unitArtSize   = 512 // the resolution the core roster is authored at
)

// The prototype's art. Copied from dvoyni/feuds-26 for this branch only; see
// assets/halo/README.md.
const (
	fontPath     = "assets/halo/Gabriela-Regular.ttf"
	paperPath    = "assets/halo/paper-bg.jpg"
	anchorPath   = "assets/halo/anchor.png"
	treeAPath    = "assets/halo/trees_1.png"
	treeBPath    = "assets/halo/trees_11.png"
	militiaPath  = "assets/halo/militiaman.png"
	infantryPath = "assets/halo/infantryman.png"
	// The same art with its baked halo unmixed away, so the effect is handed the
	// same silhouette the painter started from. This is the INPUT to the halo,
	// not the thing being judged: the untouched original is the reference beside
	// it. See tmp/strip-halo.py.
	militiaInkPath  = "assets/halo/militiaman-ink.png"
	infantryInkPath = "assets/halo/infantryman-ink.png"
)

// Which layers the material set covers is the point. The reference units on
// layerReference are drawn by the built-in material with their halos already in
// their pixels, and the readout on layerHUD must not be haloed or it could not
// be read against itself.
//
// layerMarks and layerInk are one mark layer or two, depending on the mode - see
// recordMarks. In one-pass mode nothing is recorded on layerInk at all, and
// canvas declares no gfx pass for a layer that neither draws nor clears.
const (
	layerBackdrop  canvas.Layer = 0
	layerReference canvas.Layer = 1
	layerMarks     canvas.Layer = 2
	layerInk       canvas.Layer = 3
	layerHUD       canvas.Layer = 4
)

// InkColor is feuds' mark colour, and haloBaked is the colour measured out of
// the baked halos: 84% of the halo pixels in militiaman.png are exactly this.
var (
	inkColor  = m.NewColorSrgb8(50, 40, 36, 255)
	haloBaked = m.NewColorSrgb8(174, 159, 141, 255)
	haloPaper = m.NewColorSrgb8(244, 238, 226, 255)
	haloInk   = m.NewColorSrgb8(50, 40, 36, 255)
)

// haloColors is what C cycles. The first is the one to match; the other two are
// there because cog is general and feuds is ink on paper - a light-on-dark game
// wants the mark separated from the art the other way round.
var haloColors = [...]struct {
	name  string
	color m.Color
}{
	{"baked #ae9f8d", haloBaked},
	{"paper", haloPaper},
	{"ink", haloInk},
}

// The starting knobs, fitted to the measured falloff of the baked art rather
// than guessed: over the outer band of militiaman.png, alpha holds near 0.82 for
// the first fifth of the band and then falls away almost exactly linearly, out
// to about six world units at a 96-unit icon.
const (
	startReach    = 6.0
	startExponent = 1.0
	startPlateau  = 0.18
	startAlpha    = 0.85
	startSpokes   = 12
	startRings    = 4
)

// The backdrops the halo has to survive, from the one it ships against to the
// two that flatter or punish it most.
var backdrops = [...]string{"paper + art", "paper", "flat dark", "flat light"}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	storageConfig, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	config := map[kernel.PluginName]any{
		storage.Name: storageConfig,
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: halo (prototype)"),
	}

	plugins := []kernel.Plugin{
		storage.New(), input.New(), gfx.New(), canvas.New(), wgpu.New(), New(),
	}

	kernel.New(config).WithPlugins(plugins...).Run(ctx)
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "halo"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// Demo holds nothing but the knobs. There is no simulation: the picture is the
// same every frame unless a key changed a number, which is what makes a
// screenshot of it worth comparing against another screenshot of it.
type Demo struct {
	reach    float32
	exponent float32
	plateau  float32
	alpha    float32
	spokes   int
	rings    int

	color    int
	backdrop int
	twoPass  bool
	off      bool // the A/B's other side
	haloOnly bool
	bounds   bool
	help     bool
}

func New() *Demo {
	return &Demo{
		reach: startReach, exponent: startExponent, plateau: startPlateau,
		alpha: startAlpha, spokes: startSpokes, rings: startRings,
		twoPass: true, help: true,
	}
}

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, storage.Name}
}

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.frame)
	return nil
}

func setViewport() (kernel.Lock, kernel.Observe[app.WindowSizeChangeEvent]) {
	var setDesiredViewport func(kernel.Kernel, app.SetDesiredViewportRequest) (app.SetDesiredViewportResponse, error)
	return func(access kernel.ResourceAccess) {
			setDesiredViewport = access.Uses[app.SetDesiredViewportCmd]()
		}, func(k kernel.Kernel, event app.WindowSizeChangeEvent) error {
			if event.Width <= 0 || event.Height <= 0 {
				return nil
			}
			width, height := float32(screenWidth), float32(screenHeight)
			if event.Height > event.Width {
				width, height = height, width
			}
			_, err := setDesiredViewport(k,
				app.SetDesiredViewportRequest{Mode: app.ViewportFit, Width: width, Height: height})
			return err
		}
}

func (p *Demo) frame() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var inputState kernel.Read[*input.State]
	return func(access kernel.ResourceAccess) {
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			inputState = access.GetRead[*input.State]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			p.advance(inputState.Get())
			q := canvasQueue.Get()
			p.recordBackdrop(q)
			p.recordReference(q)
			p.recordMarks(q)
			p.recordHUD(q)
			return nil
		}
}

// haloMaterial is the material descriptor itself, which never changes: the
// knobs are not on it. It carries no parameters at all, because every one of
// them is named at the scope instead - see params.
var haloMaterial = gfx.MaterialWithState(gfx.ShaderWithText(haloShader), gfx.StateOverlay2D)

// params is the knob state as canvas parameters.
//
// They go on the MaterialSet rather than on the material descriptor, and that is
// not a stylistic choice: a scope's parameters land in `shared` unconditionally
// (canvas/material.go:133), which is the per-BATCH frequency - one value in the
// uniform block for the whole instanced draw - where the same VecParam named at
// a Sprite call would have become a per-sprite storage array instead. Putting
// them here also means a knob change re-fingerprints the set rather than
// rebuilding a material, so twenty presses of R cost nothing.
func (p *Demo) params(haloOnly bool) []gfx.ParameterDescr {
	debug := m.Vec4{}
	if p.off {
		debug.X = 1
	}
	if p.haloOnly || haloOnly {
		debug.Y = 1
	}
	if p.bounds {
		debug.Z = 1
	}
	color := haloColors[p.color].color
	color.A = p.alpha
	return []gfx.ParameterDescr{
		gfx.VecParam("haloShape", m.Vec4{
			X: p.reach, Y: p.exponent, Z: float32(p.spokes), W: float32(p.rings),
		}),
		gfx.VecParam("haloTune", m.Vec4{X: p.plateau}),
		gfx.ColorParam("haloColor", color),
		gfx.VecParam("haloDebug", debug),
	}
}

// recordBackdrop paints what the marks have to be read against. The busiest
// case is first because it is the one the effect exists for: ink on paper with
// tree and mountain art crossing under it.
func (p *Demo) recordBackdrop(q *canvas.OpQueue) {
	q.SetLayerTransform(layerBackdrop, screen(), canvas.AspectInscribe)
	switch backdrops[p.backdrop] {
	case "flat dark":
		q.Clear(layerBackdrop, m.NewColorSrgb(0.09, 0.09, 0.11, 1))
		return
	case "flat light":
		q.Clear(layerBackdrop, m.NewColorSrgb(0.97, 0.97, 0.97, 1))
		return
	}
	q.Clear(layerBackdrop, m.NewColorSrgb(0.85, 0.82, 0.74, 1))
	q.Sprite(layerBackdrop, paperPath, canvas.SpriteTransform{
		Size: m.Vec2{X: screenWidth, Y: screenHeight},
	}, nil)
	if backdrops[p.backdrop] != "paper + art" {
		return
	}
	// Art directly under every row of marks, so nothing is judged against a
	// backdrop chosen to flatter it.
	for _, spot := range [...]struct {
		path string
		at   m.Vec2
		size float32
	}{
		{treeAPath, m.Vec2{X: 250, Y: 210}, 230},
		{treeBPath, m.Vec2{X: 620, Y: 200}, 190},
		{treeAPath, m.Vec2{X: 980, Y: 225}, 260},
		{treeBPath, m.Vec2{X: 300, Y: 430}, 200},
		{treeAPath, m.Vec2{X: 760, Y: 445}, 175},
		{treeBPath, m.Vec2{X: 1090, Y: 420}, 215},
	} {
		q.Sprite(layerBackdrop, spot.path, canvas.SpriteTransform{
			Position: spot.at,
			Size:     m.Vec2{X: spot.size, Y: spot.size},
			Origin:   m.Vec2{X: 0.5, Y: 0.5},
		}, nil)
	}
}

// recordReference draws the unit art with its halo already baked into its
// pixels, by the BUILT-IN material, at the size feuds draws it. This is the
// thing the effect has to match, and it is on its own layer so the material set
// cannot reach it.
func (p *Demo) recordReference(q *canvas.OpQueue) {
	q.SetLayerTransform(layerReference, screen(), canvas.AspectInscribe)
	for i, path := range bakedArt {
		scale := rowScales[i]
		q.Sprite(layerReference, path, canvas.SpriteTransform{
			Position: m.Vec2{X: 1150, Y: rowY(i)},
			Size:     m.Vec2{X: unitIconSize * scale, Y: unitIconSize * scale},
			Origin:   m.Vec2{X: 0.5, Y: 0.5},
		}, nil)
	}
}

// The two halves of the like-for-like comparison, row by row: the untouched
// painting on the reference layer, and its unmixed ink on the haloed one.
var (
	bakedArt = [...]string{militiaPath, infantryPath, militiaPath}
	inkArt   = [...]string{militiaInkPath, infantryInkPath, militiaInkPath}
)

// rowScales is the "more than one scale" the ticket asks for: the size feuds
// actually draws at, then noticeably smaller and noticeably larger. A halo whose
// reach is a constant in world units cannot read the same at all three, and
// whether that matters is exactly what there is to look at.
var rowScales = [...]float32{1.0, 0.55, 1.9}

func rowY(row int) float32 { return 205 + float32(row)*185 }

// recordMarks is the whole of the caller's side of the effect, and the M key
// switches between the two shapes it can take.
//
// ONE PASS. One SetLayerMaterial, then ordinary draws that name nothing. Each
// fragment composites its own halo under its own mark, so the whole cluster
// costs one batch - and a mark drawn later brings its band over the ink of one
// drawn earlier. A StrokeRect is four bars that overlap at four corners, so it
// is where that shows worst: the vertical bar's band cuts a notch out of the
// horizontal bar's ink.
//
// TWO PASSES. The same draws recorded twice, on two layers: the lower one under
// the halo material in halo-only mode, painting every band with no ink at all,
// and the upper one under the built-in material, painting every mark over the
// finished bands. No band can reach any ink, because no ink exists yet when the
// bands are drawn.
//
// The cost is two batches instead of one and the ops recorded twice - and NO cog
// change whatsoever, because it is a pattern a caller can write today with the
// material set that already exists.
func (p *Demo) recordMarks(q *canvas.OpQueue) {
	q.SetLayerTransform(layerMarks, screen(), canvas.AspectInscribe)
	q.SetLayerMaterial(layerMarks, canvas.MaterialSet{
		Sprite: &haloMaterial,
		Params: p.params(p.twoPass),
	})
	p.cluster(q, layerMarks)
	if !p.twoPass {
		return
	}
	// The ink pass names no material, so it is the built-in sprite material: the
	// marks exactly as they would look with no halo at all.
	q.SetLayerTransform(layerInk, screen(), canvas.AspectInscribe)
	p.cluster(q, layerInk)
}

// cluster records every mark once, onto whichever layer it is handed.
func (p *Demo) cluster(q *canvas.OpQueue, layer canvas.Layer) {
	for row, scale := range rowScales {
		y := rowY(row)
		// A glyph, and more than one of them, because adjacent glyphs are the
		// case where one mark's expanded quad reaches over its neighbour's ink.
		p.number(q, layer, "7", m.Vec2{X: 110, Y: y}, scale)
		p.number(q, layer, "18", m.Vec2{X: 215, Y: y}, scale)
		p.number(q, layer, "247", m.Vec2{X: 355, Y: y}, scale)

		// A sprite with a real silhouette, packed in the same atlas as the tree
		// art above, which is what puts a neighbour next to it to bleed from.
		q.Sprite(layer, anchorPath, canvas.SpriteTransform{
			Position: m.Vec2{X: 520, Y: y},
			Size:     m.Vec2{X: 46 * scale, Y: 46 * scale},
			Origin:   m.Vec2{X: 0.5, Y: 0.5},
		}, nil, gfx.ColorParam("tint", inkColor))

		// The fill: feuds' city flag, which is two FillRects and nothing else.
		// Its frame is a degenerate point, so this is the analytic branch.
		flag(q, layer, m.Vec2{X: 640, Y: y}, scale)

		// The other two shape helpers, which are fills too, and are here because
		// a rule one world unit thick is the narrowest thing the analytic branch
		// will ever be asked for.
		q.StrokeRect(layer, m.Rect{
			X: 745, Y: y - 26*scale, Width: 66 * scale, Height: 52 * scale,
		}, canvas.ShapeDraw{Color: inkColor, Thickness: 2 * scale})
		q.Line(layer,
			m.Vec2{X: 850, Y: y + 22*scale}, m.Vec2{X: 950, Y: y - 22*scale},
			canvas.ShapeDraw{Color: inkColor, Thickness: 3 * scale})

		// The same unit art with its baked halo unmixed away, so this effect has
		// to supply one from the same silhouette the painter started from. Side
		// by side with the untouched original at 1150, this is the comparison
		// that settles the ticket.
		q.Sprite(layer, inkArt[row], canvas.SpriteTransform{
			Position: m.Vec2{X: 1030, Y: y},
			Size:     m.Vec2{X: unitIconSize * scale, Y: unitIconSize * scale},
			Origin:   m.Vec2{X: 0.5, Y: 0.5},
		}, nil)
	}
}

// number draws a score exactly as feuds draws one: centred, in the board face,
// at the board size.
func (p *Demo) number(q *canvas.OpQueue, layer canvas.Layer, text string, at m.Vec2, scale float32) {
	size := scoreFontSize * scale
	q.Text(layer, fontPath, text, canvas.TextDraw{
		Position: m.Vec2{X: at.X, Y: at.Y - size/2},
		Size:     size,
		Color:    inkColor,
		Align:    canvas.AlignCenter,
	})
}

// flag is feuds' city flag: a pole and a pennant, two FillRects.
func flag(q *canvas.OpQueue, layer canvas.Layer, at m.Vec2, scale float32) {
	pole := m.Rect{X: at.X, Y: at.Y - 30*scale, Width: 3 * scale, Height: 60 * scale}
	q.FillRect(layer, pole, canvas.ShapeDraw{Color: inkColor})
	q.FillRect(layer, m.Rect{
		X: at.X + 3*scale, Y: at.Y - 30*scale, Width: 34 * scale, Height: 22 * scale,
	}, canvas.ShapeDraw{Color: inkColor})
}

func screen() m.Rect { return m.Rect{Width: screenWidth, Height: screenHeight} }

// recordHUD is the instrumentation: every knob, in the units they are thought
// in, on a layer the material set does not reach.
func (p *Demo) recordHUD(q *canvas.OpQueue) {
	q.SetLayerTransform(layerHUD, screen(), canvas.AspectInscribe)
	ink := m.NewColorSrgb(0.12, 0.11, 0.10, 1)

	state := "ON"
	if p.off {
		state = "OFF  (A/B)"
	}
	pass := "two passes (halo layer under ink layer)"
	if !p.twoPass {
		pass = "one pass (halo composited per mark)"
	}
	line := fmt.Sprintf("halo %s   %s   reach %.1f   plateau %.2f   exp %.2f   alpha %.2f   %d x %d   %s   %s",
		state, pass, p.reach, p.plateau, p.exponent, p.alpha, p.spokes, p.rings,
		haloColors[p.color].name, backdrops[p.backdrop])
	q.Text(layerHUD, fontPath, line, canvas.TextDraw{
		Position: m.Vec2{X: 24, Y: 24}, Size: 21, Color: ink,
	})
	q.Text(layerHUD, fontPath,
		"rows: 1.0x (as feuds draws it) / 0.55x / 1.9x        right pair: this effect | baked art",
		canvas.TextDraw{Position: m.Vec2{X: 24, Y: 52}, Size: 17, Color: ink})

	if !p.help {
		return
	}
	q.Text(layerHUD, fontPath,
		"H halo on/off   M one pass/two   R/F reach   T/G plateau   E/D exponent   A/Z alpha   "+
			"1/2 spokes   3/4 rings   C colour   B backdrop   O halo only   Q quad bounds   P print   ? help",
		canvas.TextDraw{Position: m.Vec2{X: 24, Y: 682}, Size: 17, Color: ink})
}

func (p *Demo) advance(state *input.State) {
	if state == nil {
		return
	}
	// H is the A/B, and it toggles rather than holds so that a capture can sit on
	// either side of it. Judging an image against a memory of the previous image
	// flatters new work; flicking it in and out in place does not.
	if state.JustPressed(input.KeyH) {
		p.off = !p.off
	}

	step := func(down, up input.Key, value *float32, by, lo, hi float32) {
		if state.Pressed(down) {
			*value = m.Clamp(*value+by, lo, hi)
		}
		if state.Pressed(up) {
			*value = m.Clamp(*value-by, lo, hi)
		}
	}
	step(input.KeyR, input.KeyF, &p.reach, 0.08, 0, 40)
	step(input.KeyT, input.KeyG, &p.plateau, 0.004, 0, 0.95)
	step(input.KeyE, input.KeyD, &p.exponent, 0.01, 0.05, 6)
	step(input.KeyA, input.KeyZ, &p.alpha, 0.005, 0, 1)

	if state.JustPressed(input.Key1) && p.spokes > 3 {
		p.spokes--
	}
	if state.JustPressed(input.Key2) && p.spokes < 48 {
		p.spokes++
	}
	if state.JustPressed(input.Key3) && p.rings > 1 {
		p.rings--
	}
	if state.JustPressed(input.Key4) && p.rings < 16 {
		p.rings++
	}
	if state.JustPressed(input.KeyC) {
		p.color = (p.color + 1) % len(haloColors)
	}
	if state.JustPressed(input.KeyB) {
		p.backdrop = (p.backdrop + 1) % len(backdrops)
	}
	if state.JustPressed(input.KeyO) {
		p.haloOnly = !p.haloOnly
	}
	if state.JustPressed(input.KeyM) {
		p.twoPass = !p.twoPass
	}
	if state.JustPressed(input.KeyQ) {
		p.bounds = !p.bounds
	}
	if state.JustPressed(input.KeySlash) {
		p.help = !p.help
	}
	if state.JustPressed(input.KeyBackspace) {
		*p = *New()
	}
	if state.JustPressed(input.KeyP) {
		// The output of a tuning session is the numbers, so print them in the
		// form they would be pasted back in.
		fmt.Printf("reach %.2f  plateau %.3f  exponent %.2f  alpha %.3f  spokes %d  rings %d  colour %s\n",
			p.reach, p.plateau, p.exponent, p.alpha, p.spokes, p.rings, haloColors[p.color].name)
	}
}
