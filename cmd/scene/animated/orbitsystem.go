package main

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

type orbitQuery struct {
	Place  *m.Transform
	Camera scene.Camera
}

// orbit places the camera on its orbit, looking at the overview's target or the
// focused station's. The Camera Component itself never changes: a camera's
// placement is its Transform.
func orbit(cameras *ecs.Query[orbitQuery], state *ecs.Read[*Demo]) {
	d := state.Get()
	target := d.target()
	eye := eyeAt(target, d.radius(), d.azimuth, d.elevation)
	for _, it := range cameras.All() {
		*it.Place = m.LookAt(eye, target, m.Vec3{Y: 1})
	}
}

// target and radius are the orbit the camera is on, which is the overview's or
// the focused station's.
func (d *Demo) target() m.Vec3 {
	if d.focus == 0 {
		return orbitTarget
	}
	s := &stations[d.focus-1]
	return m.Vec3{X: s.x, Y: s.height}
}

func (d *Demo) radius() float32 {
	if d.focus == 0 {
		return overviewRadius
	}
	return stations[d.focus-1].radius
}

// eyeAt is a camera position on an orbit: azimuth about Y from +Z, elevation
// above the horizontal.
func eyeAt(target m.Vec3, radius, azimuth, elevation float32) m.Vec3 {
	cosElevation := float32(math.Cos(float64(elevation)))
	return target.Add(m.Vec3{
		X: radius * cosElevation * float32(math.Sin(float64(azimuth))),
		Y: radius * float32(math.Sin(float64(elevation))),
		Z: radius * cosElevation * float32(math.Cos(float64(azimuth))),
	})
}
