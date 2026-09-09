// Command spritecollapse is a THROWAWAY prototype for
// https://github.com/dvoyni/cog/issues/153. It is not an example, it is not
// documentation, and it will be deleted: it exists to answer one question with
// eyes rather than argument.
//
//	go run ./cmd/canvas/spritecollapse
//
// # The question
//
// canvas has two sprite shaders. builtin/canvas/sprite.wgsl draws one sprite
// from a per-draw uniform block; builtin/canvas/spritebatch.wgsl draws many
// from a storage buffer indexed by @builtin(instance_index). Which one a draw
// takes is decided by whether it carries a material: canvas/plugin.go:331
// routes a sprite with a material to drawEntry (the uniform shader) and every
// other sprite to batchEntry (the instanced one).
//
// The map wants to delete the uniform shader and route everything through the
// instanced one. That is only safe if the two agree pixel for pixel, and
// position, rotation, origin, uv frame, atlas layer, clip, tint and key colour
// all ride different routes in the two shaders. So: draw the same sprite twice,
// once down each route, side by side, and look.
//
// # Reading the screen
//
// Two columns. The left column passes nil and so takes the instanced shader.
// The right column passes canvas.DefaultMaterial() and so takes the uniform
// shader. Every row is the same sprite with the same transform. Any row where
// the two halves differ is a place the collapse would change what is drawn.
//
// The third column appears only on the patched engine (branch
// proto/sprite-collapse of dvoyni/cog), where a material no longer forces the
// uniform path: it carries a custom material whose only difference from the
// built-in instanced one is its fs_main body, multiplying alpha by a uniform
// member it appended. That is the live consumer's case, the feuds-26 fade.
package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"os/signal"
	"testing/fstest"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/storage"
	"github.com/dvoyni/cog/wgpu"
)

const (
	screenWidth  = 1000
	screenHeight = 700
)

// spritePath is the generated test sprite, mounted from memory so the
// prototype needs nothing on disk.
const spritePath = "proto/collapse.png"

// The two column centres, and the third the patched engine adds.
const (
	columnA = 240 // nil material     -> instanced shader
	columnB = 500 // DefaultMaterial() -> uniform shader (or instanced, patched)
	columnC = 760 // custom material  -> instanced shader (patched only)
)

// rows is one case each, drawn identically in every column.
var rows = []struct {
	y     float32
	label string
}{
	{140, "plain"},
	{225, "rotation + origin"},
	{310, "frame inset + flipX"},
	{395, "tint"},
	{480, "key colour ramp"},
	{565, "clip (left half)"},
	{650, "nearest filter"},
}

const (
	spriteSide = 64 // source texels
	drawnSide  = 60 // logical pixels
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		// The generated sprite is mounted over the default config at a higher
		// priority than the executable directory, so nothing is written out.
		storage.Name: storage.DefaultConfig("cog-examples").
			WithReadFS("proto", 100, generatedFS()),
		wgpu.Name: wgpu.DefaultConfig().WithTitle("PROTOTYPE cog#153: sprite collapse"),
	}

	plugins := []kernel.Plugin{
		storage.New(), input.New(), gfx.New(), canvas.New(), wgpu.New(), New(),
	}
	kernel.New(config).WithPlugins(plugins...).Run(ctx)
}

// GeneratedFS builds the in-memory filesystem holding the test sprite. Four
// saturated quadrants make a flip or a uv inset obvious, the dark low-green
// square in the middle is what the key-colour ramp bites on, and the one-texel
// black border shows a frame inset eating into the edge.
func GeneratedFS() fs.FS { return generatedFS() }

func generatedFS() fs.FS {
	img := image.NewNRGBA(image.Rect(0, 0, spriteSide, spriteSide))
	quadrant := [4]color.NRGBA{
		{R: 220, G: 60, B: 60, A: 255},  // top-left     red
		{R: 60, G: 200, B: 90, A: 255},  // top-right    green
		{R: 70, G: 110, B: 230, A: 255}, // bottom-left  blue
		{R: 230, G: 200, B: 70, A: 255}, // bottom-right yellow
	}
	half := spriteSide / 2
	for y := range spriteSide {
		for x := range spriteSide {
			i := 0
			if x >= half {
				i++
			}
			if y >= half {
				i += 2
			}
			c := quadrant[i]
			// The middle square is dark and low-green: the key-colour ramp's
			// input. An unkeyed draw leaves it as it is.
			if x > 22 && x < 42 && y > 22 && y < 42 {
				c = color.NRGBA{R: 90, G: 20, B: 20, A: 255}
			}
			// One-texel black border, so a frame inset is visible eating in.
			if x == 0 || y == 0 || x == spriteSide-1 || y == spriteSide-1 {
				c = color.NRGBA{A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return fstest.MapFS{spritePath: &fstest.MapFile{Data: buf.Bytes()}}
}

// Name is the prototype plugin's name.
const Name kernel.PluginName = "spritecollapse"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// Demo holds only the clock, so the rotation row is visibly live rather than a
// still that could hide a disagreement at one particular angle.
type Demo struct{ elapsed float32 }

// New builds the prototype plugin.
func New() *Demo { return &Demo{} }

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, storage.Name}
}

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.draw)
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

func (p *Demo) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var canvasQueue kernel.Write[*canvas.OpQueue]
	return func(access kernel.ResourceAccess) {
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.elapsed += float32(event.Dt)
			Record(canvasQueue.Get(), p.elapsed)
			return nil
		}
}

// Record draws the whole comparison. It is exported and takes the clock so the
// headless test beside this file records the identical frame and reads the draw
// calls off the fake backend.
func Record(q *canvas.OpQueue, elapsed float32) {
	q.Clear(0, m.NewColorSrgb(0.07, 0.08, 0.10, 1))
	q.SetLayerTransform(0, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	label(q, "PROTOTYPE cog#153 - the same sprite down both routes", m.Vec2{X: screenWidth / 2, Y: 40}, 22)
	label(q, "A: nil (instanced)", m.Vec2{X: columnA, Y: 85}, 16)
	label(q, "B: DefaultMaterial()", m.Vec2{X: columnB, Y: 85}, 16)
	if CustomMaterialWorks {
		label(q, "C: custom fs_main", m.Vec2{X: columnC, Y: 85}, 16)
	}

	for i, row := range rows {
		label(q, row.label, m.Vec2{X: 85, Y: row.y}, 14)
		for _, col := range columns() {
			drawCase(q, i, col.x, row.y, elapsed, col.material, col.params)
		}
	}
}

// column is one route the same sprite is drawn down.
type column struct {
	x        float32
	material *gfx.MaterialDescr
	params   []gfx.ParameterDescr
}

func columns() []column {
	out := []column{
		{x: columnA, material: nil},
		{x: columnB, material: canvas.DefaultMaterial()},
	}
	if CustomMaterialWorks {
		// No draw parameters: fade is a uniform member the shader declared, so
		// it belongs to the material. That is the frequency rule cog#152
		// settled, and putting it here is what makes the column batch.
		out = append(out, column{x: columnC, material: FadeMaterial()})
	}
	return out
}

// drawCase draws one row's sprite at one column, applying whatever that row is
// testing. Everything except the material is identical across the columns.
func drawCase(q *canvas.OpQueue, row int, x, y, elapsed float32, material *gfx.MaterialDescr, extra []gfx.ParameterDescr) {
	t := canvas.SpriteTransform{
		Position: m.Vec2{X: x, Y: y},
		Size:     m.Vec2{X: drawnSide, Y: drawnSide},
		Origin:   m.Vec2{X: 0.5, Y: 0.5},
	}
	params := append([]gfx.ParameterDescr(nil), extra...)

	switch row {
	case 0: // plain
	case 1: // rotation about the middle
		t.Rotation = elapsed * 0.7
	case 2: // uv frame inset, mirrored horizontally
		t.Frame = canvas.SpriteFrame{Left: 10, Top: 6, Right: 4, Bottom: 12}
		t.FlipX = true
	case 3: // tint
		params = append(params, gfx.ColorParam("tint", m.NewColorSrgb(0.4, 0.9, 1.0, 0.75)))
	case 4: // key colour ramp over the dark middle square
		params = append(params, gfx.ColorParam("keyColor", m.NewColorSrgb(0.2, 0.85, 0.35, 1)))
	case 5: // clip cutting the sprite down the middle
		q.SetClip(m.Rect{X: x - drawnSide/2, Y: y - drawnSide/2, Width: drawnSide / 2, Height: drawnSide})
		defer q.RemoveClip()
	case 6: // nearest filter over a magnified crop, so texels are individually visible
		t.Filter = gfx.FilterNearest
		t.Frame = canvas.SpriteFrame{Left: 24, Top: 24, Right: 24, Bottom: 24}
	}
	q.Sprite(0, spritePath, t, material, params...)
}

func label(q *canvas.OpQueue, text string, at m.Vec2, size float32) {
	q.Text(0, "", text, canvas.TextDraw{
		Position: at, Size: size, Align: canvas.AlignCenter,
		Color: m.NewColorSrgb(0.85, 0.87, 0.90, 1),
	})
}
