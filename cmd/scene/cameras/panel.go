package main

import "github.com/dvoyni/cog/m"

// A panel is one camera's viewport: a temporary target the camera renders into,
// and the rectangle of the logical screen canvas composites that target onto.
//
// It is the whole reason this demo needs a type at all. Split-screen is spelled
// as one gfx.TemporaryTarget per camera drawn by canvas, because a
// projection-baked sub-rect does not clip - a point at NDC x = 1.5, which the
// clipper would have discarded, is remapped to 0.25 and rasterises into the
// neighbouring camera's half - and gfx exposes no scissor. So there is no
// "viewport rectangle" anywhere in scene, and the mapping between a pixel of a
// camera's target and a point on the screen is the app's to write.
//
// It is two numbers rather than one because they are two different things:
//
//   - size is the target's own resolution in texels, and it is what every
//     coordinate helper takes. WorldToScreen for a texture camera answers in
//     texture pixels; it has no idea where canvas will put them.
//   - rect is where those texels land in the logical screen, which is what
//     canvas and the pointer both speak.
//
// The minimap keeps them apart on purpose: it renders 512 texels into 400
// canvas units, which is the render-scale knob - scene has no RenderScale field
// because a smaller (or larger) temporary target already is one - and it means
// a mapping that forgets the scale is visibly wrong here and invisibly right in
// the main view.
type panel struct {
	rect m.Rect // where canvas composites it, in logical screen coordinates
	size m.Vec2 // the temporary target's size in texels
}

// texel maps a point on the logical screen into this panel's target, and
// reports whether it landed inside the panel at all.
//
// The bool is what a click needs first: the two panels do not overlap and the
// gap between them belongs to neither, so a click there must pick through no
// camera rather than through the nearest one.
func (p panel) texel(screen m.Vec2) (m.Vec2, bool) {
	if p.rect.Width <= 0 || p.rect.Height <= 0 {
		return m.Vec2{}, false
	}
	local := m.Vec2{X: screen.X - p.rect.X, Y: screen.Y - p.rect.Y}
	if local.X < 0 || local.Y < 0 || local.X > p.rect.Width || local.Y > p.rect.Height {
		return m.Vec2{}, false
	}
	return m.Vec2{
		X: local.X * p.size.X / p.rect.Width,
		Y: local.Y * p.size.Y / p.rect.Height,
	}, true
}

// canvas maps a point in this panel's target back onto the logical screen. It
// is texel's inverse, and it is deliberately total: a nameplate for a point
// just off the edge of a viewport is extrapolated rather than refused, which is
// the same rule WorldToScreen follows for a point off the edge of the target.
// Clipping a plate to its panel is the caller's decision, and this demo makes
// it separately.
func (p panel) canvas(texel m.Vec2) m.Vec2 {
	if p.size.X <= 0 || p.size.Y <= 0 {
		return m.Vec2{X: p.rect.X, Y: p.rect.Y}
	}
	return m.Vec2{
		X: p.rect.X + texel.X*p.rect.Width/p.size.X,
		Y: p.rect.Y + texel.Y*p.rect.Height/p.size.Y,
	}
}

// contains reports whether a texel is inside the panel's target, which is what
// decides whether a nameplate is drawn: a label extrapolated past the edge of
// its own viewport would sit over the neighbouring one, or over the HUD.
func (p panel) contains(texel m.Vec2) bool {
	return texel.X >= 0 && texel.Y >= 0 && texel.X <= p.size.X && texel.Y <= p.size.Y
}
