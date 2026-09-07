// Command rendertexture is canvas drawing into a texture and then drawing that
// texture back onto the screen. It is the whole of canvas render-to-texture in
// one file, with no scene, no camera and no asset on disk.
//
// One canvas layer renders a nest of spinning squares into a 256x256 target.
// Two draws on the layer above put that texture on the screen: a rectangle
// through SpriteTexture, and a circle through DrawTexture. Both source the same
// gfx.TextureDescr, because the unit of exchange is a plain gfx texture rather
// than any canvas-owned handle - which is also why the same texture would serve
// a scene material without another line of code.
//
//	go run ./cmd/canvas/rendertexture
//
// What only eyes can judge: the two shapes show the same picture, the circle
// has the panel's corners cut off rather than squashed into it, and the squares
// spin without tearing or flickering. The flicker is the one that matters and
// the one a screenshot cannot catch - it is what a missing barrier between the
// pass that writes the texture and the pass that samples it looks like, and gfx
// places that barrier, not this demo.
package main

import (
	"context"
	"math"
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
)

// Logical viewport the demo draws in. The window is fitted to it, so the layout
// keeps its proportions at any window size.
const (
	screenWidth  = 800
	screenHeight = 600
)

// panelSize is the render target's side in texels. It is square because both
// consumers want it to be: the circle inscribes the whole texture, and a
// non-square one would inscribe as an ellipse.
const panelSize = 256

// The two layers. layerPanel renders into the texture and layerScreen samples
// it, so the order between them is not cosmetic: canvas declares one gfx pass
// per layer at the layer's own order, and the pass that writes the texture has
// to run before the pass that reads it.
const (
	layerPanel  canvas.Layer = 0
	layerScreen canvas.Layer = 1
)

// The nest, outermost first. Each square is smaller than the one around it and
// spins faster, which is what makes the panel obviously live rather than a
// picture baked once at startup.
var panelColors = [...]m.Color{
	m.NewColorSrgb(0.20, 0.29, 0.47, 1),
	m.NewColorSrgb(0.24, 0.53, 0.60, 1),
	m.NewColorSrgb(0.36, 0.72, 0.53, 1),
	m.NewColorSrgb(0.90, 0.76, 0.33, 1),
	m.NewColorSrgb(0.87, 0.42, 0.35, 1),
}

const (
	// panelOuter leaves a margin of the layer's clear colour around the biggest
	// square, so the clear is visible rather than covered. A clear on a
	// texture-targeted layer is worth seeing: a temporary target is drawn from a
	// pool and holds whatever the last frame that borrowed it left behind.
	panelOuter = 0.86
	panelStep  = 0.15
	// squareSpin is radians per second for the first square in; each one further
	// in turns by a further multiple of it.
	squareSpin = 0.35
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	config := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("cog-examples"),
		wgpu.Name:    wgpu.DefaultConfig().WithTitle("cog examples: rendertexture"),
	}

	// The demo is last because it records into the queues the plugins before it
	// declare - canvas's and, to allocate the target, gfx's.
	plugins := []kernel.Plugin{
		storage.New(),
		input.New(),
		gfx.New(),
		canvas.New(),
		wgpu.New(),
		New(),
	}

	kernel.New(config).WithPlugins(plugins...).Run(ctx)
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "rendertexture"

type windowSizeChangeEventHandler kernel.Subscription[app.WindowSizeChangeEvent]
type updateEventHandler kernel.Subscription[app.UpdateEvent]

// Demo is the gameplay plugin. Its only state is the clock the squares spin on
// and the circle it built once, because a circle at a fixed place on the screen
// is not something to rebuild sixty times a second.
type Demo struct {
	elapsed float32
	disc    []canvas.Vertex
}

func New() *Demo {
	return &Demo{disc: disc(m.Vec2{X: 565, Y: 320}, 115)}
}

func (p *Demo) Name() kernel.PluginName { return Name }

func (p *Demo) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.Name, gfx.Name, input.Name, storage.Name}
}

func (p *Demo) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[windowSizeChangeEventHandler](setViewport)
	registrar.Subscribe[updateEventHandler](p.draw)
	return nil
}

// setViewport fits the logical screen inside the window, swapping the axes when
// the window is taller than it is wide.
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

// draw records the whole frame. It holds gfx's queue as well as canvas's,
// because canvas mints nothing: the target is a handle the app allocates and
// hands over, since minting a texture takes the gfx queue and a canvas recorder
// does not hold one.
func (p *Demo) draw() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var canvasQueue kernel.Write[*canvas.OpQueue]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	return func(access kernel.ResourceAccess) {
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.elapsed += float32(event.Dt)
			// TemporaryTarget hands back both handles onto one texture: the target
			// a pass renders into and the texture a later pass samples. Its
			// contents do not survive the frame, which is exactly right here -
			// the panel is redrawn every frame anyway. A panel drawn once and kept
			// would take gfx.ResourceQueue.AllocateRenderTarget instead.
			//
			// Ask for FormatRGBA8Srgb: the atlas is sRGB, the engine blends
			// linear, and gfx keys every pipeline to the frame buffer's colour
			// format whatever the pass target is.
			target, texture := gfxQueue.Get().TemporaryTarget(panelSize, panelSize, gfx.FormatRGBA8Srgb)
			q := canvasQueue.Get()
			p.recordPanel(q, target)
			p.recordScreen(q, texture)
			return nil
		}
}

// recordPanel draws the nest of squares into the render target.
func (p *Demo) recordPanel(q *canvas.OpQueue, target gfx.TargetDescr) {
	q.SetLayerTarget(layerPanel, target)
	// Clear is per layer, so this one lands on the texture and the screen's own
	// clear lands on the screen. One frame-global clear could not say both.
	q.Clear(layerPanel, m.NewColorSrgb(0.10, 0.11, 0.14, 1))
	// A layer with a target measures against the target, not the viewport, so
	// this window is in texels of the texture and the mapping is the identity.
	// The identical call on a screen layer would have mapped to the viewport.
	q.SetLayerTransform(layerPanel,
		m.Rect{Width: panelSize, Height: panelSize}, canvas.AspectStretch)

	centre := m.Vec2{X: panelSize / 2, Y: panelSize / 2}
	for i, color := range panelColors {
		side := panelSize * (panelOuter - float32(i)*panelStep)
		q.Sprite(layerPanel, "", canvas.SpriteTransform{
			Position: centre,
			Size:     m.Vec2{X: side, Y: side},
			// Origin is a fraction of the sprite's own size, so a half on both
			// axes spins the square about its middle.
			Origin:   m.Vec2{X: 0.5, Y: 0.5},
			Rotation: p.elapsed * squareSpin * float32(i),
		}, nil, gfx.ColorParam("tint", color))
	}
}

// recordScreen puts the finished texture on the screen twice.
func (p *Demo) recordScreen(q *canvas.OpQueue, texture gfx.TextureDescr) {
	q.Clear(layerScreen, m.NewColorSrgb(0.06, 0.07, 0.09, 1))
	q.SetLayerTransform(layerScreen,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	label(q, "canvas render to texture", m.Vec2{X: screenWidth / 2, Y: 60}, 26)

	// The rectangle. SpriteTexture is Sprite sourcing a gfx texture instead of a
	// path, and the transform means what it always meant, measured against the
	// texture's own dimensions: leaving Size unset here would draw it at 256x256.
	q.SpriteTexture(layerScreen, texture, canvas.SpriteTransform{
		Position: m.Vec2{X: 235, Y: 320},
		Size:     m.Vec2{X: 230, Y: 230},
		Origin:   m.Vec2{X: 0.5, Y: 0.5},
	}, nil)
	label(q, "SpriteTexture", m.Vec2{X: 235, Y: 470}, 20)

	// The circle. DrawTexture is DrawTriangles sourcing a gfx texture: the shape
	// is this demo's, the uvs in the vertices say which texels it lands on.
	q.DrawTexture(layerScreen, texture, p.disc, nil)
	label(q, "DrawTexture", m.Vec2{X: 565, Y: 470}, 20)

	// Neither call names a material. Both default to canvas.TextureMaterial,
	// which samples the texture and returns it. The sprite and triangle
	// materials must not be used on a render target: both run the key-colour
	// ramp, which turns every dark, low-green texel grey at its own red
	// intensity, and no key colour switches that off.
}

// disc builds the triangle list of a filled circle, uv-mapped so the whole
// texture is inscribed in it. The rim therefore samples the texture's own edge
// and the panel loses its corners, which is the point: the shape is the
// caller's and the texels are the texture's, with nothing in between.
func disc(centre m.Vec2, radius float32) []canvas.Vertex {
	const segments = 72
	white := m.Color{R: 1, G: 1, B: 1, A: 1}
	// The vertex colour multiplies the sampled texel, so white leaves the panel
	// as it was drawn. Tinting the rim would fade the circle out at its edge.
	middle := canvas.Vertex{Position: centre, Color: white, UV: m.Vec2{X: 0.5, Y: 0.5}}
	rim := func(step int) canvas.Vertex {
		angle := 2 * math.Pi * float64(step) / segments
		x, y := float32(math.Cos(angle)), float32(math.Sin(angle))
		return canvas.Vertex{
			Position: m.Vec2{X: centre.X + x*radius, Y: centre.Y + y*radius},
			Color:    white,
			UV:       m.Vec2{X: 0.5 + x*0.5, Y: 0.5 + y*0.5},
		}
	}
	vertices := make([]canvas.Vertex, 0, segments*3)
	for step := range segments {
		vertices = append(vertices, middle, rim(step), rim(step+1))
	}
	return vertices
}

// label centres one line of text at a point, in canvas's built-in font.
func label(q *canvas.OpQueue, text string, at m.Vec2, size float32) {
	q.Text(layerScreen, "", text, canvas.TextDraw{
		Position: at, Size: size, Align: canvas.AlignCenter,
		Color: m.NewColorSrgb(0.85, 0.87, 0.90, 1),
	})
}
