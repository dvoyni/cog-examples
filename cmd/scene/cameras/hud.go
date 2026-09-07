package main

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// The 2D half of the frame: the backdrop, the two composited panels, the
// nameplates over them, and the HUD.
//
// Nothing here draws with a font of its own. A canvas text op with an empty
// font path draws with the font canvas embeds, so the numbers reach the screen
// without a file, a mount, or any configuration.

// The HUD's layout, in the logical screen's coordinates.
const (
	hudSize  = 13
	hudLeft  = 18
	hudTop   = 14
	hudLine  = hudSize * 7 / 5
	hudFoot  = 26
	plateGap = 6 // how far above the anchor a nameplate's baseline sits
	plateSiz = 12
)

// draw2D composites both panels and draws everything over them.
func (p *Cameras) draw2D(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerViews,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)
	q.SetLayerTransform(layerHUD,
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	panels := p.panels()
	// The panel the pointer is over keeps its full brightness and the other is
	// dimmed, which is one line of proof that a composited camera is an
	// ordinary 2D draw with an ordinary vertex colour on it.
	composite(q, layerViews, mainPanel, p.mainTexture, p.panelTint(panels[0].view))
	composite(q, layerViews, mapPanel, p.mapTexture, p.panelTint(panels[1].view))

	for _, panel := range panels {
		p.nameplates(q, panel)
	}
	p.hud(q)
}

// panelTint dims the panel the pointer is not over.
func (p *Cameras) panelTint(view panel) m.Color {
	if _, over := view.texel(p.pointer); over {
		return m.Color{R: 1, G: 1, B: 1, A: 1}
	}
	return m.Color{R: 0.72, G: 0.72, B: 0.76, A: 1}
}

// nameplates labels every corridor cube in one panel, and is the world-to-screen
// half of this demo.
//
// Three rules, all of them the helper's rather than this function's:
//
//   - The viewport it passes is the panel's own target size, never the screen's
//     and never the other panel's. "Where is this point on screen for a camera
//     that renders two different targets" is an ill-formed question, so the
//     caller names the size it means and the answer comes back in that target's
//     texels. Mapping those texels onto the screen is the panel's job, and the
//     minimap's is not the identity.
//   - ok false means the point is at or behind the eye plane, and a plate is
//     then simply not drawn. Dividing by a negative w yields a plausible,
//     mirrored, confidently wrong point, which is the single classic bug in
//     this helper; refusing to return a coordinate is what makes it impossible
//     to draw one anyway.
//   - Off-screen but in front stays true, and is extrapolated correctly past
//     the target edge - that is what an off-screen indicator arrow needs. This
//     demo does not want one, so it drops a plate outside the panel itself.
//     That is the caller's decision and it is made here, not in the helper.
func (p *Cameras) nameplates(q *canvas.OpQueue, panel composited) {
	for i := range cubes {
		screen, ok := scene.WorldToScreen(panel.camera, panel.view.size, nameplateAnchor(i))
		if !ok {
			continue
		}
		texel := m.Vec2{X: screen.X, Y: screen.Y}
		if !panel.view.contains(texel) {
			continue
		}
		at := panel.view.canvas(texel)
		color := plateColor
		if p.pickedName() == cubes[i].name {
			color = highlightColor
		}
		q.Text(layerHUD, "", cubes[i].name, canvas.TextDraw{
			Position: m.Vec2{X: at.X, Y: at.Y - plateGap - plateSiz},
			Size:     plateSiz,
			Color:    color,
			Align:    canvas.AlignCenter,
		})
	}
}

// hud prints the demo's key numbers.
func (p *Cameras) hud(q *canvas.OpQueue) {
	state := "running"
	if p.paused {
		state = "paused"
	}
	picked := p.pickedName()
	if picked == "" {
		picked = "-"
	}
	lines := [...]string{
		fmt.Sprintf("cameras  step %06d  z %+.2f  fps %.0f  %s",
			p.step, trackZ(p.time()), p.rate.perSecond, state),
		fmt.Sprintf("passes %d  draws %d  culled %d  packed %d  batches %d",
			p.stats.passes, p.stats.recorded, p.stats.culled, p.stats.instances, p.stats.batches),
		fmt.Sprintf("depth pass packed %d  picked %s  ops %d",
			p.stats.depthDraw, picked, p.stats.ops),
	}
	for i, line := range lines {
		p.text(q, hudLeft, hudTop+float32(i)*hudLine, line, hudColor)
	}

	// The minimap's column, under it, where there is room for it and nowhere
	// near the other panel: a HUD that collides with itself is invisible in a
	// low-resolution capture and unmistakable at full size, so it is laid out
	// with the panels rather than beside them.
	column := mapPanel.rect.Y + mapPanel.rect.Height + hudLine
	side := [...]string{
		fmt.Sprintf("main   %.0fx%.0f perspective", mainPanel.size.X, mainPanel.size.Y),
		fmt.Sprintf("minimap %.0fx%.0f orthographic", mapPanel.size.X, mapPanel.size.Y),
		fmt.Sprintf("composited into %.0fx%.0f", mapPanel.rect.Width, mapPanel.rect.Height),
	}
	for i, line := range side {
		p.text(q, mapPanel.rect.X, column+float32(i)*hudLine, line, hudDimColor)
	}

	if p.reports > 0 {
		p.text(q, mapPanel.rect.X, column+float32(len(side)+1)*hudLine,
			fmt.Sprintf("reports %d", p.reports), hudWarnColor)
		p.text(q, mapPanel.rect.X, column+float32(len(side)+2)*hudLine,
			truncate(p.lastReport, 46), hudWarnColor)
	}

	p.text(q, hudLeft, screenHeight-hudFoot,
		"space run/pause   left/right step   r reset   d duplicate camera   click to pick",
		hudDimColor)
}

// text draws one line with its top-left at the given point, so a caller places
// lines by stepping y and never has to know where the baseline sits.
func (p *Cameras) text(q *canvas.OpQueue, x, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: x, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}

// truncate keeps a reported error inside the minimap's column.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit-1] + "…"
}
