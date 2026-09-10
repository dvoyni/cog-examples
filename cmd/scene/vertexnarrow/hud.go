package main

// THROWAWAY. The HUD, so the picture and the number that produced it are on
// screen together. A screenshot of this demo has to be arguable six months
// later, and a bare render of a grey sphere is not.

import (
	"fmt"

	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/m"
)

const (
	hudSize = 13
	hudLeft = 18
	hudTop  = 16
	hudLine = hudSize * 7 / 5
	hudFoot = hudTop + hudSize*2/3
)

var (
	backdropColor = m.NewColorSrgb(0.05, 0.055, 0.07, 1)
	hudColor      = m.NewColorSrgb(0.88, 0.90, 0.94, 1)
	hudDimColor   = m.NewColorSrgb(0.45, 0.48, 0.55, 1)
	hudMarkColor  = m.NewColorSrgb(1.00, 0.85, 0.35, 1)
)

func (p *Demo) hud(q *canvas.OpQueue) {
	q.Clear(layerBackdrop, backdropColor)
	q.SetLayerTransform(layerHUD, m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe)

	switch p.station {
	case stationNormals:
		p.hudNormals(q)
	case stationUV:
		p.hudUV(q)
	default:
		p.hudReflect(q)
	}
	p.text(q, screenHeight-hudFoot,
		"tab station   m view   q roughness   1-4 one rung   0 stripes   "+
			"e environment   n normal map   o orbit   arrows orbit/pan   r reset",
		hudDimColor)
}

func (p *Demo) hudReflect(q *canvas.OpQueue) {
	environment := "OFF (one sun, dielectric)"
	if p.envOn {
		environment = "ON (metal, equirect panorama)"
	}
	p.text(q, hudTop, fmt.Sprintf(
		"vertexnarrow  TANGENT FRAME LADDER   view %s   roughness %.2f   orbit %s   fps %.0f",
		reflectModeNames[p.reflectMode], reflectRoughness[p.reflectRough],
		orbitRateNames[p.orbitRate], p.rate.perSecond), hudColor)
	p.text(q, hudTop+hudLine, fmt.Sprintf(
		"environment %s   normal map %s   env mip %.2f of %dx%d",
		environment, mapPresetNames[p.mapPreset], envLod(reflectRoughness[p.reflectRough]),
		envWidth, envHeight), hudDimColor)

	for i, f := range frames {
		colour, mark := hudColor, " "
		if p.solo == i {
			colour, mark = hudMarkColor, ">"
		}
		if i >= stripeFrames {
			mark = "-"
			if p.solo == i {
				mark = ">"
			}
		}
		line := fmt.Sprintf("%s %d  %-22s %2d B frame, %2d B vertex   exact", mark, i+1, f.Name, f.Bytes, f.Stride)
		if f.NBits != 0 {
			line = fmt.Sprintf(
				"%s %d  %-22s %2d B frame, %2d B vertex   n max %.4f deg   t max %.4f deg",
				mark, i+1, f.Name, f.Bytes, f.Stride,
				p.frameStats[i].NormalMax, p.frameStats[i].TangentMax)
		}
		p.text(q, hudTop+float32(i+3)*hudLine, line, colour)
	}
	p.text(q, hudTop+float32(len(frames)+4)*hudLine,
		"a reflection carries twice the normal's angular error; the swim view is on a "+
			"2x ramp so it reads against the error view", hudDimColor)
	p.text(q, hudTop+float32(len(frames)+5)*hudLine,
		"panorama by azimuth: 0-90 bars at 2/4/8/16 deg, 90-180 one hard softbox, "+
			"180-270 a 6 deg checker, 270-360 a gradient and no edge at all (the control)",
		hudDimColor)
	p.text(q, hudTop+float32(len(frames)+6)*hudLine,
		"probe view: four bands from the pole down - oct32 n, oct16 n, oct30 t, oct16 t. "+
			"a band of flat colour is an attribute that arrived as zero", hudDimColor)
}

func (p *Demo) hudNormals(q *canvas.OpQueue) {
	p.text(q, hudTop, fmt.Sprintf(
		"vertexnarrow  NORMAL LADDER   view %s   roughness %.2f   fps %.0f",
		ladderModeNames[p.ladderMode], roughnessPresets[p.roughness], p.rate.perSecond), hudColor)
	p.text(q, hudTop+hudLine, fmt.Sprintf(
		"sphere %d x %d, normals exact before quantisation; stripes run left to right, error over %d vertices",
		sphereSlices, sphereStacks, (sphereSlices+1)*(sphereStacks+1)), hudDimColor)

	// The row reads left to right on screen, so it reads left to right here.
	for i, r := range rungs {
		colour, mark := hudColor, " "
		if p.solo == i {
			colour, mark = hudMarkColor, ">"
		}
		line := fmt.Sprintf("%s %d  %-22s %2d B   exact", mark, i+1, r.Name, r.Bytes)
		if i > 0 {
			line = fmt.Sprintf("%s %d  %-22s %2d B   mean %.4f deg   max %.4f deg",
				mark, i+1, r.Name, r.Bytes, p.octStats[i].Mean, p.octStats[i].Max)
		}
		p.text(q, hudTop+float32(i+3)*hudLine, line, colour)
	}
	p.text(q, hudTop+float32(len(rungs)+4)*hudLine,
		"the four rungs meet at three seams on one surface: a terrace that stops at a seam is the encoding",
		hudDimColor)
}

func (p *Demo) hudUV(q *canvas.OpQueue) {
	c := bandCases[p.bandCase]
	p.text(q, hudTop, fmt.Sprintf(
		"vertexnarrow  TEXTURE COORDINATES   %s   view %s   fps %.0f",
		c.Name, uvModeNames[p.uvMode], p.rate.perSecond), hudColor)
	p.text(q, hudTop+hudLine, fmt.Sprintf(
		"u at frame centre %.4f   zoom %.2f   grid texture %d x %d, nearest, no mips   %d segments",
		p.panU, p.zoom, gridWidth, gridHeight, bandSegments), hudDimColor)

	steps := [len(uvRungs)]float32{
		0,
		texels(unormStep(p.bandRange[p.bandCase].Scale.X)),
		texels(float16Step(p.panU)),
	}
	for i, r := range uvRungs {
		line := fmt.Sprintf("  %-20s %2d B   exact", r.Name, r.Bytes)
		if i > 0 {
			line = fmt.Sprintf("  %-20s %2d B   step %7.3f texels of %d",
				r.Name, r.Bytes, steps[i], gridWidth)
		}
		p.text(q, hudTop+float32(i+3)*hudLine, line, hudColor)
	}
	p.text(q, hudTop+float32(len(uvRungs)+4)*hudLine, fmt.Sprintf(
		"top band is the reference; a line that fails to meet the one above it is the drift. "+
			"half float step doubles at u = 1, 2, 4, 8, 16"), hudDimColor)
}

func (p *Demo) text(q *canvas.OpQueue, y float32, line string, color m.Color) {
	q.Text(layerHUD, "", line, canvas.TextDraw{
		Position: m.Vec2{X: hudLeft, Y: y},
		Size:     hudSize,
		Color:    color,
	})
}
